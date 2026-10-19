package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The command refuses before doing anything when an env session would shadow
// the one it is about to store.
func TestHarvestRefusesWhileCookieJarSet(t *testing.T) {
	e := newEnv(t)
	t.Setenv("LEROYMERLIN_COOKIEJAR", e.cookieFile())
	_, errOut, code := e.run("auth", "harvest")
	if code != 2 {
		t.Fatalf("exit %d, want 2: %s", code, errOut)
	}
	if !strings.Contains(errOut, "LEROYMERLIN_COOKIEJAR") {
		t.Fatalf("refusal does not name the env var: %s", errOut)
	}
}

func TestHarvestCommandSurface(t *testing.T) {
	e := newEnv(t)
	out, errOut, code := e.run("auth", "--help")
	if code != 0 {
		t.Fatalf("auth --help exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "harvest") {
		t.Fatalf("auth help has no harvest command:\n%s", out)
	}
	out, errOut, code = e.run("auth", "harvest", "--help")
	if code != 0 {
		t.Fatalf("harvest --help exit %d: %s", code, errOut)
	}
	for _, flag := range []string{"--browser-url", "--chrome", "--user-agent", "--timeout", "LEROYMERLIN_CHROME_URL", "LEROYMERLIN_CHROME"} {
		if !strings.Contains(out, flag) {
			t.Errorf("harvest help does not mention %s:\n%s", flag, out)
		}
	}
	if _, _, code := e.run("auth", "harvest", "extra"); code == 0 {
		t.Fatal("harvest accepted an argument")
	}
}

func TestResolveChromeEnv(t *testing.T) {
	t.Setenv(chromeURLEnv, "http://env:9222")
	t.Setenv(chromePathEnv, "/env/chrome")
	if u, c := resolveChromeEnv("http://flag:9333", "/flag/chrome"); u != "http://flag:9333" || c != "/flag/chrome" {
		t.Fatalf("flag did not win: %q %q", u, c)
	}
	if u, c := resolveChromeEnv("", ""); u != "http://env:9222" || c != "/env/chrome" {
		t.Fatalf("env not used: %q %q", u, c)
	}
	t.Setenv(chromeURLEnv, "")
	t.Setenv(chromePathEnv, "")
	if u, c := resolveChromeEnv("", ""); u != "" || c != "" {
		t.Fatalf("empty env produced %q %q", u, c)
	}
}

// The harvest profile is a browser profile under the data directory, not the
// session directory, and `config path` says where it is.
func TestHarvestProfilePath(t *testing.T) {
	e := newEnv(t)
	out, _, code := e.run("config", "path")
	if code != 0 {
		t.Fatalf("config path exit %d", code)
	}
	want := filepath.Join(e.home, "data", "leroymerlin-cli", "chrome")
	if !strings.Contains(out, want) {
		t.Fatalf("config path = %s, want %s", out, want)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatal("config path created the profile")
	}
}
