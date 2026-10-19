package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// ServerName is the key the server is registered under in every harness.
const ServerName = "leroymerlin"

// Harnesses that `leroymerlin mcp install` knows.
var Harnesses = []string{"claude-code", "codex", "cursor"}

// Change reports what an install did to which file.
type Change struct {
	Harness string   `json:"harness"`
	Path    string   `json:"path"`
	Action  string   `json:"action"` // added, updated, unchanged
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Install registers command+args as the leroymerlin MCP server in harness's
// config. Other servers and other keys in our entry (env, timeouts) are kept.
func Install(harness, command string, args []string) (Change, error) {
	ch := Change{Harness: harness, Command: command, Args: args}
	home, err := os.UserHomeDir()
	if err != nil {
		return ch, err
	}
	switch harness {
	case "claude-code":
		dir := home
		if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
			dir = v
		}
		ch.Path = filepath.Join(dir, ".claude.json")
		ch.Action, err = installJSON(ch.Path, command, args)
	case "cursor":
		ch.Path = filepath.Join(home, ".cursor", "mcp.json")
		ch.Action, err = installJSON(ch.Path, command, args)
	case "codex":
		dir := filepath.Join(home, ".codex")
		if v := os.Getenv("CODEX_HOME"); v != "" {
			dir = v
		}
		ch.Path = filepath.Join(dir, "config.toml")
		ch.Action, err = installTOML(ch.Path, command, args)
	default:
		return ch, fmt.Errorf("unknown harness %q (want %s)", harness, strings.Join(Harnesses, ", "))
	}
	return ch, err
}

// installJSON edits {"mcpServers": {"leroymerlin": {...}}}.
func installJSON(path, command string, args []string) (string, error) {
	doc := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return "", fmt.Errorf("%s is not valid JSON, so it was left alone: %w", path, err)
		}
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if doc["mcpServers"] != nil && !ok {
		return "", fmt.Errorf("%s: mcpServers is not an object", path)
	}
	if servers == nil {
		servers = map[string]any{}
		doc["mcpServers"] = servers
	}
	entry, _ := servers[ServerName].(map[string]any)
	action := "updated"
	if entry == nil {
		entry, action = map[string]any{}, "added"
	}
	argList := make([]any, len(args))
	for i, a := range args {
		argList[i] = a
	}
	if action == "updated" && entry["command"] == command && reflect.DeepEqual(entry["args"], argList) {
		return "unchanged", nil
	}
	entry["command"], entry["args"] = command, argList
	servers[ServerName] = entry
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return action, writeKeepingMode(path, append(out, '\n'))
}

// installTOML appends an [mcp_servers.leroymerlin] table to codex's config.
// The file is edited as text so comments and order survive; an existing
// table is replaced in place.
func installTOML(path, command string, args []string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = strconv.Quote(a)
	}
	header := "[mcp_servers." + ServerName + "]"
	block := header + "\ncommand = " + strconv.Quote(command) + "\nargs = [" + strings.Join(quoted, ", ") + "]\n"
	body := string(raw)
	start := strings.Index(body, header+"\n")
	if start < 0 {
		if strings.Contains(body, "mcp_servers."+ServerName) || strings.Contains(body, `mcp_servers."`+ServerName+`"`) {
			return "", fmt.Errorf("%s defines the %s server in a form this installer does not edit; add this by hand:\n%s", path, ServerName, block)
		}
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if body != "" {
			body += "\n"
		}
		return "added", writeKeepingMode(path, []byte(body+block))
	}
	end := len(body)
	if next := strings.Index(body[start+len(header):], "\n["); next >= 0 {
		end = start + len(header) + next + 1
	}
	old := body[start:end]
	// Keep keys we do not own (env, timeouts) from the old table.
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(old, "\n"), "\n")[1:] {
		k := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		if k != "command" && k != "args" && strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	next := block
	if len(kept) > 0 {
		next += strings.Join(kept, "\n") + "\n"
	}
	if strings.TrimRight(old, "\n") == strings.TrimRight(next, "\n") {
		return "unchanged", nil
	}
	if end < len(body) {
		next += "\n"
	}
	return "updated", writeKeepingMode(path, []byte(body[:start]+next+body[end:]))
}

// writeKeepingMode writes atomically, following a symlinked config (dotfile
// managers) and keeping the file's mode.
func writeKeepingMode(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
