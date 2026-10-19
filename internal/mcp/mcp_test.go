package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeRunner answers with the output a CLI command would print.
func fakeRunner(stdout, errJSON string) Runner {
	return func(ctx context.Context, argv []string) Result {
		return Result{Stdout: stdout, ErrorJSON: errJSON}
	}
}

func serve(t *testing.T, in string, run Runner) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	s := &Server{Version: "9.9.9", Run: run}
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("reply %q: %v", line, err)
		}
		replies = append(replies, r)
	}
	return replies
}

func TestServeProtocol(t *testing.T) {
	in := strings.Join([]string{
		`not json`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":true,"method":"ping"}`,
		`{"jsonrpc":"1.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
		`{"jsonrpc":"2.0","id":5,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"doctor"}}`,
		`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"category_list","arguments":{"depth":"one"}}}`,
		`{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"product_show","arguments":{}}}`,
		"",
	}, "\n")
	replies := serve(t, in, fakeRunner(`{"ok":true}`, ""))
	if len(replies) != 13 { // one reply per request with an id
		t.Fatalf("%d replies:\n%v", len(replies), replies)
	}
	code := func(i int) float64 {
		e, _ := replies[i]["error"].(map[string]any)
		c, _ := e["code"].(float64)
		return c
	}
	if code(0) != errParse {
		t.Errorf("parse error: %v", replies[0])
	}
	if code(1) != errInvalidRequest {
		t.Errorf("boolean id: %v", replies[1])
	}
	if code(2) != errInvalidRequest || code(3) != errInvalidRequest {
		t.Errorf("bad jsonrpc/method: %v %v", replies[2], replies[3])
	}
	if code(4) != errNoMethod {
		t.Errorf("unknown method: %v", replies[4])
	}
	init1, _ := replies[5]["result"].(map[string]any)
	if init1["protocolVersion"] != "2025-03-26" {
		t.Errorf("initialize echo: %v", replies[5])
	}
	init2, _ := replies[6]["result"].(map[string]any)
	if init2["protocolVersion"] != protocolVersions[len(protocolVersions)-1] {
		t.Errorf("initialize default: %v", replies[6])
	}
	if _, ok := replies[7]["result"]; !ok {
		t.Errorf("ping: %v", replies[7])
	}
	list, _ := replies[8]["result"].(map[string]any)
	if _, ok := list["tools"]; !ok {
		t.Errorf("tools/list: %v", replies[8])
	}
	if _, err := replies[9]["error"]; err {
		t.Errorf("valid tool call failed: %v", replies[9])
	}
	if code(10) != errInvalidParams || code(11) != errInvalidParams || code(12) != errInvalidParams {
		t.Errorf("tool errors: %v %v %v", replies[10], replies[11], replies[12])
	}
}

func TestCallReportsOutputAndErrors(t *testing.T) {
	ok := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor"}}`+"\n", fakeRunner(`{"a":1}`+"\n", ""))
	res, _ := ok[0]["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("ok call: %v", res)
	}
	content, _ := res["content"].([]any)
	first, _ := content[0].(map[string]any)
	if first["text"] != `{"a":1}` {
		t.Fatalf("content: %v", res)
	}

	failed := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor"}}`+"\n", fakeRunner("", `{"code":3}`))
	res, _ = failed[0]["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("failed call: %v", res)
	}

	empty := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor"}}`+"\n", fakeRunner("", ""))
	res, _ = empty[0]["result"].(map[string]any)
	content, _ = res["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != "" {
		t.Fatalf("empty content: %v", res)
	}
}

func TestServeStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Server{Version: "1", Run: fakeRunner("", "")}
	if err := s.Serve(ctx, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("a cancelled context did not stop the server")
	}
}

func TestToolSchemaAndLookup(t *testing.T) {
	raw, err := json.Marshal(Tools[0])
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["name"] != "store_list" || schema["inputSchema"] == nil {
		t.Fatalf("schema: %s", raw)
	}
	if _, ok := lookup("doctor"); !ok {
		t.Fatal("doctor not found")
	}
	if _, ok := lookup("nope"); ok {
		t.Fatal("unknown tool found")
	}
	if !strings.Contains(Describe(), "store_list") || len(ToolNames()) != len(Tools) {
		t.Fatalf("describe/names: %q %v", Describe(), ToolNames())
	}
}

func TestToolArgvAllTypes(t *testing.T) {
	store, _ := lookup("store_list")
	argv, err := store.argv(map[string]any{"query": "madrid", "compact": true})
	if err != nil || strings.Join(argv, " ") != "store list --query=madrid --compact" {
		t.Fatalf("argv: %v %v", argv, err)
	}
	// A false boolean is dropped, not passed as =false.
	if argv, _ := store.argv(map[string]any{"compact": false}); strings.Join(argv, " ") != "store list" {
		t.Fatalf("false boolean: %v", argv)
	}
	product, _ := lookup("product_show")
	if _, err := product.argv(map[string]any{}); err == nil {
		t.Fatal("a missing required argument was accepted")
	}
	if argv, err := product.argv(map[string]any{"ref": "81866477"}); err != nil || strings.Join(argv, " ") != "product show -- 81866477" {
		t.Fatalf("positional: %v %v", argv, err)
	}
	if _, err := store.argv(map[string]any{"depth": 1.5}); err == nil {
		t.Fatal("a fractional integer was accepted")
	}
	if _, err := store.argv(map[string]any{"query": 1}); err == nil {
		t.Fatal("a non-string was accepted")
	}
	if _, err := store.argv(map[string]any{"compact": "yes"}); err == nil {
		t.Fatal("a non-boolean was accepted")
	}
}
