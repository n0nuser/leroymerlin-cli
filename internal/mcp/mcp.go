// Package mcp serves the CLI's read commands over the Model Context Protocol,
// stdio transport: newline-delimited JSON-RPC 2.0 on stdin/stdout.
//
// Each tool is a CLI argv run in-process, so a tool's output is exactly the
// JSON the command prints and the two cannot drift apart.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Versions this server speaks; a client asking for another gets the newest.
var protocolVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18"}

// Result is one command's outcome: what it printed, and the --error-format
// json envelope when it failed.
type Result struct {
	Stdout    string
	ErrorJSON string
}

// Runner runs one CLI invocation.
type Runner func(ctx context.Context, argv []string) Result

// Server answers MCP on one stdio pair.
type Server struct {
	Version string
	Run     Runner
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errNoMethod       = -32601
	errInvalidParams  = -32602
)

// Serve handles requests until in closes or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	lines := make(chan string)
	done := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
		done <- sc.Err()
		close(lines)
	}()
	enc := json.NewEncoder(out)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return <-done
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			if resp, reply := s.handle(ctx, []byte(line)); reply {
				if err := enc.Encode(resp); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) handle(ctx context.Context, line []byte) (response, bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return fail(nil, errParse, "invalid json"), true
	}
	if len(req.ID) == 0 {
		return response{}, false // notification: never answered
	}
	if c := req.ID[0]; c == '{' || c == '[' || c == 't' || c == 'f' {
		return fail(nil, errInvalidRequest, "id must be a string, number or null"), true
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return fail(req.ID, errInvalidRequest, `want "jsonrpc":"2.0" and a method`), true
	}
	ok := func(v any) (response, bool) { return response{JSONRPC: "2.0", ID: req.ID, Result: v}, true }
	switch req.Method {
	case "initialize":
		return ok(s.initialize(req.Params))
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": Tools})
	case "tools/call":
		res, rerr := s.call(ctx, req.Params)
		if rerr != nil {
			return response{JSONRPC: "2.0", ID: req.ID, Error: rerr}, true
		}
		return ok(res)
	}
	return fail(req.ID, errNoMethod, "unknown method "+req.Method), true
}

func fail(id json.RawMessage, code int, msg string) response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	v := protocolVersions[len(protocolVersions)-1]
	for _, known := range protocolVersions {
		if known == p.ProtocolVersion {
			v = known
		}
	}
	return map[string]any{
		"protocolVersion": v,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "leroymerlin", "version": s.Version},
		"instructions": "leroymerlin.es through the leroymerlin CLI, read-only. Tool output is the JSON the matching command prints; " +
			"a failed command returns its error envelope with the exit code (5 means the site's bot protection challenged the session: " +
			"tell the user to refresh it, do not retry in a loop).",
	}
}

func (s *Server) call(ctx context.Context, params json.RawMessage) (map[string]any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "invalid params"}
	}
	t, found := lookup(p.Name)
	if !found {
		return nil, &rpcError{Code: errInvalidParams, Message: fmt.Sprintf("unknown tool %q. Available: %s", p.Name, strings.Join(ToolNames(), ", "))}
	}
	argv, err := t.argv(p.Arguments)
	if err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: err.Error()}
	}
	r := s.Run(ctx, argv)
	var content []map[string]string
	if text := strings.TrimRight(r.Stdout, "\n"); text != "" {
		content = append(content, map[string]string{"type": "text", "text": text})
	}
	if r.ErrorJSON != "" {
		content = append(content, map[string]string{"type": "text", "text": strings.TrimRight(r.ErrorJSON, "\n")})
	}
	if content == nil {
		content = []map[string]string{{"type": "text", "text": ""}}
	}
	return map[string]any{"content": content, "isError": r.ErrorJSON != ""}, nil
}
