package harvest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// syntheticProfile builds a fake Chrome profile root with the given relative
// files (contents are the file names).
func syntheticProfile(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDetectProfileOrder(t *testing.T) {
	a := syntheticProfile(t, "Default/Cookies")
	b := syntheticProfile(t, "Default/Network/Cookies")
	c := syntheticProfile(t, "Default/Cookies")
	got, err := detectProfile([]string{a, b, c}, "")
	if err != nil || got != a {
		t.Fatalf("first valid profile = %q %v", got, err)
	}
	// A profile without a cookie store is skipped.
	empty := t.TempDir()
	got, err = detectProfile([]string{empty, b}, "")
	if err != nil || got != b {
		t.Fatalf("skipped profile = %q %v", got, err)
	}
	if _, err := detectProfile([]string{empty}, ""); err == nil {
		t.Fatal("no profile did not error")
	}
	// An explicit profile wins, and an invalid one is an error rather than a
	// silent fall back to a fresh browser.
	if got, err := detectProfile([]string{a}, b); err != nil || got != b {
		t.Fatalf("override = %q %v", got, err)
	}
	if _, err := detectProfile([]string{a}, empty); err == nil {
		t.Fatal("a bad override was accepted")
	}
}

func TestProfileCandidatesLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux path order")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir())
	got := profileCandidates()
	want := []string{
		filepath.Join(cfg, "google-chrome"),
		filepath.Join(cfg, "chromium"),
		filepath.Join(cfg, "google-chrome-beta"),
		filepath.Join(cfg, "chromium-browser"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("candidates = %v", got)
	}
}

func TestPlanProfileCopy(t *testing.T) {
	root := syntheticProfile(t,
		"Local State",
		"Default/Cookies",
		"Default/Cookies-journal",
		"Default/Network/Cookies",
		"Default/Preferences",
		"Default/History",
		"Default/Other file",
	)
	files, err := PlanProfileCopy(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Local State",
		"Default/Cookies",
		"Default/Cookies-journal",
		"Default/Network/Cookies",
		"Default/Preferences",
	}
	if strings.Join(files, "|") != strings.Join(want, "|") {
		t.Fatalf("plan = %v", files)
	}
	for _, f := range files {
		if strings.Contains(f, "History") || strings.Contains(f, "Other") {
			t.Fatalf("plan copies more than the cookie store: %v", files)
		}
	}
}

func TestPlanProfileCopyNeedsLocalState(t *testing.T) {
	root := syntheticProfile(t, "Default/Cookies")
	if _, err := PlanProfileCopy(root); err == nil {
		t.Fatal("a profile without Local State was accepted")
	}
}

func TestPrepareProfileCopiesAndCleansUp(t *testing.T) {
	root := syntheticProfile(t,
		"Local State",
		"Default/Cookies",
		"Default/Cookies-journal",
		"Default/Network/Cookies",
		"Default/Preferences",
		"Default/History",
	)
	parent := t.TempDir()
	dir, cleanup, err := PrepareProfile(root, parent)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("temp dir %v %v, want 0700", fi, err)
	}
	for _, rel := range []string{"Local State", "Default/Cookies", "Default/Cookies-journal", "Default/Network/Cookies", "Default/Preferences"} {
		p := filepath.Join(dir, rel)
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s not copied: %v", rel, err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v, want 0600", rel, fi.Mode().Perm())
		}
		raw, _ := os.ReadFile(p)
		if string(raw) != rel {
			t.Fatalf("%s contents %q", rel, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Default/History")); !os.IsNotExist(err) {
		t.Fatal("the plan copied History")
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("cleanup left the temporary profile")
	}
}

func TestPrepareProfileFailsWithoutLocalStateAndLeavesNothing(t *testing.T) {
	root := syntheticProfile(t, "Default/Cookies")
	parent := t.TempDir()
	if _, _, err := PrepareProfile(root, parent); err == nil {
		t.Fatal("expected an error")
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 0 {
		t.Fatalf("a failed copy left %d entries", len(entries))
	}
}

func TestFreePort(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if port < 1 || port > 65535 {
		t.Fatalf("port = %d", port)
	}
	if port2, err := FreePort(); err != nil || port2 < 1 {
		t.Fatalf("second port = %d %v", port2, err)
	}
}

func TestNormalizeDebugURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:9222":                  "http://127.0.0.1:9222",
		"http://127.0.0.1:9222":                  "http://127.0.0.1:9222",
		"ws://localhost:9222/devtools/browser/x": "ws://127.0.0.1:9222/devtools/browser/x",
		"http://chrome.local:9222":               "http://chrome.local:9222",
	} {
		if got := NormalizeDebugURL(in); got != want {
			t.Errorf("NormalizeDebugURL(%q) = %q, want %q", in, got, want)
		}
	}
}
