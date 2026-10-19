package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
)

func testPaths(t *testing.T) Paths {
	dir := t.TempDir()
	return Paths{ConfigFile: filepath.Join(dir, "config.toml"), SessionsDir: filepath.Join(dir, "sessions")}
}

func TestSessionFileIsPrivate(t *testing.T) {
	p := testPaths(t)
	s := &StoredSession{Session: leroymerlin.Session{Cookies: []leroymerlin.Cookie{{Name: "datadome", Value: "v", Domain: ".leroymerlin.es"}}}}
	if err := SaveSession(p, "default", s); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p.SessionPath("default"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("session mode %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(p.SessionsDir)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("sessions dir mode %v, want 0700", di.Mode().Perm())
	}
	back, err := LoadSession(p, "default")
	if err != nil || back == nil || back.Source != SourceFile {
		t.Fatalf("load: %+v %v", back, err)
	}
	if c, _ := back.Cookie("datadome"); c.Value != "v" {
		t.Fatalf("cookie lost: %+v", back.Cookies)
	}
}

// Secrets from the environment must never land on disk, rotations included.
func TestEnvSessionIsNeverSaved(t *testing.T) {
	p := testPaths(t)
	jar := filepath.Join(t.TempDir(), "jar.txt")
	os.WriteFile(jar, []byte("datadome=fromenv"), 0o600)
	t.Setenv(CookieJarEnv, jar)
	s, err := LoadSession(p, "default")
	if err != nil || s == nil || s.Source != SourceEnv {
		t.Fatalf("env session: %+v %v", s, err)
	}
	if err := SaveSession(p, "default", s); err == nil {
		t.Fatal("an env session was written")
	}
	if _, err := os.Stat(p.SessionPath("default")); !os.IsNotExist(err) {
		t.Fatal("session file exists after refusing an env session")
	}
}

func TestProfileNamesCannotEscape(t *testing.T) {
	p := testPaths(t)
	for _, bad := range []string{"../x", "a/b", "", ".hidden", "x y"} {
		if _, err := LoadSession(p, bad); err == nil {
			t.Errorf("profile %q accepted", bad)
		}
	}
}

func TestConfigRejectsUnknownKeys(t *testing.T) {
	p := testPaths(t)
	os.WriteFile(p.ConfigFile, []byte("defualt_profile = \"x\"\n"), 0o644)
	if _, err := Load(p.ConfigFile); err == nil {
		t.Fatal("typo key accepted")
	}
	if err := Save(p.ConfigFile, Config{DefaultProfile: "work"}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p.ConfigFile)
	if err != nil || c.DefaultProfile != "work" {
		t.Fatalf("%+v %v", c, err)
	}
}
