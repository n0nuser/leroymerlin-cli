package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallJSONKeepsOtherEntriesAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"x"},"leroymerlin":{"command":"old","env":{"A":"1"}}},"theme":"dark"}`), 0o600)
	act, err := installJSON(path, "/bin/leroymerlin", []string{"mcp"})
	if err != nil || act != "updated" {
		t.Fatalf("%s %v", act, err)
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{`"other"`, `"theme": "dark"`, `"A": "1"`, `"/bin/leroymerlin"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
	if act, _ := installJSON(path, "/bin/leroymerlin", []string{"mcp"}); act != "unchanged" {
		t.Fatalf("second run: %s", act)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %v", fi.Mode().Perm())
	}
}

func TestInstallTOMLEditsAsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("# my settings\nmodel = \"x\"\n\n[mcp_servers.leroymerlin]\ncommand = \"old\"\nstartup_timeout_sec = 20\n\n[other]\nk = 1\n"), 0o644)
	act, err := installTOML(path, "/bin/leroymerlin", []string{"mcp", "--profile", "madrid"})
	if err != nil || act != "updated" {
		t.Fatalf("%s %v", act, err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	for _, want := range []string{"# my settings", `command = "/bin/leroymerlin"`, `args = ["mcp", "--profile", "madrid"]`, "startup_timeout_sec = 20", "[other]\nk = 1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"old"`) {
		t.Fatalf("old command kept:\n%s", s)
	}
	if act, _ := installTOML(path, "/bin/leroymerlin", []string{"mcp", "--profile", "madrid"}); act != "unchanged" {
		t.Fatalf("second run: %s\n%s", act, s)
	}
	fresh := filepath.Join(t.TempDir(), "new.toml")
	if act, err := installTOML(fresh, "lm", []string{"mcp"}); err != nil || act != "added" {
		t.Fatalf("fresh: %s %v", act, err)
	}
}

func TestToolArgvValidation(t *testing.T) {
	tool, _ := lookup("category_list")
	argv, err := tool.argv(map[string]any{"parent": "construccion", "depth": float64(1)})
	if err != nil || strings.Join(argv, " ") != "category list --parent=construccion --depth=1" {
		t.Fatalf("%v %v", argv, err)
	}
	// "--parent=-x" keeps a dash-led value from being read as a flag.
	if argv, _ := tool.argv(map[string]any{"parent": "--format=xml"}); argv[2] != "--parent=--format=xml" {
		t.Fatalf("value not bound to its flag: %v", argv)
	}
	for _, bad := range []map[string]any{{"depth": 1.5}, {"depth": "2"}, {"nope": true}} {
		if _, err := tool.argv(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestInstallJSONRefusesAMalformedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	original := []byte(`{"mcpServers":{"leroymerlin":"oops"}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := installJSON(path, "/bin/leroymerlin", []string{"mcp"}); err == nil {
		t.Fatal("a non-object entry was overwritten")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != string(original) {
		t.Fatalf("file changed:\n%s", raw)
	}
}

func TestInstallHarnessesAndErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	if _, err := Install("nope", "lm", []string{"mcp"}); err == nil {
		t.Fatal("an unknown harness was accepted")
	}

	ch, err := Install("claude-code", "/bin/lm", []string{"mcp"})
	if err != nil || ch.Action != "added" || ch.Path != filepath.Join(home, ".claude.json") {
		t.Fatalf("claude-code: %+v %v", ch, err)
	}
	if ch, err := Install("cursor", "/bin/lm", []string{"mcp"}); err != nil || ch.Action != "added" {
		t.Fatalf("cursor: %+v %v", ch, err)
	}
	if ch, err := Install("codex", "/bin/lm", []string{"mcp"}); err != nil || ch.Action != "added" {
		t.Fatalf("codex: %+v %v", ch, err)
	}

	// The env overrides the config directory.
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	ch, err = Install("claude-code", "/bin/lm", []string{"mcp"})
	if err != nil || ch.Path != filepath.Join(claudeDir, ".claude.json") {
		t.Fatalf("CLAUDE_CONFIG_DIR: %+v %v", ch, err)
	}
	codexDir := t.TempDir()
	t.Setenv("CODEX_HOME", codexDir)
	ch, err = Install("codex", "/bin/lm", []string{"mcp"})
	if err != nil || ch.Path != filepath.Join(codexDir, "config.toml") {
		t.Fatalf("CODEX_HOME: %+v %v", ch, err)
	}
}

func TestInstallJSONCases(t *testing.T) {
	dir := t.TempDir()
	// Invalid JSON is left alone.
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{"), 0o600)
	if _, err := installJSON(bad, "lm", nil); err == nil {
		t.Fatal("invalid json was accepted")
	}
	// mcpServers must be an object.
	notObject := filepath.Join(dir, "servers.json")
	os.WriteFile(notObject, []byte(`{"mcpServers":[]}`), 0o600)
	if _, err := installJSON(notObject, "lm", nil); err == nil {
		t.Fatal("a non-object mcpServers was accepted")
	}
	// A fresh file is created with the entry.
	fresh := filepath.Join(dir, "fresh.json")
	act, err := installJSON(fresh, "lm", []string{"mcp"})
	if err != nil || act != "added" {
		t.Fatalf("fresh: %s %v", act, err)
	}
	raw, _ := os.ReadFile(fresh)
	if !strings.Contains(string(raw), `"mcpServers"`) || !strings.Contains(string(raw), `"leroymerlin"`) {
		t.Fatalf("fresh file: %s", raw)
	}
	// An existing mode is kept.
	mode := filepath.Join(dir, "mode.json")
	os.WriteFile(mode, []byte("{}"), 0o644)
	if _, err := installJSON(mode, "lm", nil); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(mode); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode changed to %v", fi.Mode().Perm())
	}
}

func TestInstallTOMLRefusesAFormItCannotEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte(`mcp_servers.leroymerlin = { command = "x" }`+"\n"), 0o600)
	if _, err := installTOML(path, "lm", nil); err == nil {
		t.Fatal("an inline table was overwritten")
	}
	// A file without a trailing newline still gets a clean block.
	plain := filepath.Join(t.TempDir(), "plain.toml")
	os.WriteFile(plain, []byte("model = \"x\""), 0o600)
	act, err := installTOML(plain, "lm", nil)
	if err != nil || act != "added" {
		t.Fatalf("plain: %s %v", act, err)
	}
	raw, _ := os.ReadFile(plain)
	if !strings.HasSuffix(string(raw), "args = []\n") || !strings.Contains(string(raw), "model = \"x\"\n") {
		t.Fatalf("plain file:\n%s", raw)
	}
}

func TestWriteKeepingModeFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "link.json")
	os.WriteFile(real, []byte("{}"), 0o600)
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := writeKeepingMode(link, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(real)
	if err != nil || !strings.Contains(string(raw), `"a":1`) {
		t.Fatalf("real file = %q %v", raw, err)
	}
}
