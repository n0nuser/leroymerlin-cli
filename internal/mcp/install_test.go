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
