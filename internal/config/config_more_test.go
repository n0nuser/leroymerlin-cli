package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPathsAndOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-data")
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	t.Setenv("LEROYMERLIN_CONFIG", "")
	p := DefaultPaths()
	if p.ConfigFile != "/tmp/xdg-config/leroymerlin-cli/config.toml" ||
		p.SessionsDir != "/tmp/xdg-config/leroymerlin-cli/sessions" ||
		p.StateDB != "/tmp/xdg-data/leroymerlin-cli/state.db" ||
		p.DataDir != "/tmp/xdg-data/leroymerlin-cli" ||
		p.ChromeDir != "/tmp/xdg-data/leroymerlin-cli/chrome" ||
		p.StateDir != "/tmp/xdg-state/leroymerlin-cli" {
		t.Fatalf("paths = %+v", p)
	}
	t.Setenv("LEROYMERLIN_CONFIG", "/tmp/special.toml")
	if got := DefaultPaths().ConfigFile; got != "/tmp/special.toml" {
		t.Fatalf("override = %q", got)
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if c, err := Load(path); err != nil || c.DefaultProfile != "" {
		t.Fatalf("missing file = %+v %v", c, err)
	}
	cfg := Config{DefaultProfile: "madrid", Watch: WatchConfig{Interval: "1m"}, Sinks: map[string]SinkConfig{"shop": {Type: "ntfy", URL: "https://ntfy.sh", Topic: "t"}}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil || back.DefaultProfile != "madrid" || back.Watch.Interval != "1m" || back.Sinks["shop"].Topic != "t" {
		t.Fatalf("round trip = %+v %v", back, err)
	}
	// A bad TOML value is an error.
	os.WriteFile(path, []byte("default_profile = [oops\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("broken toml accepted")
	}
}

func TestWriteAtomicCreatesPrivateDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "file.txt")
	if err := WriteAtomic(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file %v %v", fi, err)
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", di.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "hi" {
		t.Fatalf("contents %q", raw)
	}
	// Overwriting keeps the new contents.
	if err := WriteAtomic(path, []byte("bye"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "bye" {
		t.Fatalf("contents after overwrite %q", raw)
	}
}

func TestLoadSessionEnvErrors(t *testing.T) {
	p := Paths{SessionsDir: t.TempDir()}
	t.Setenv(CookieJarEnv, filepath.Join(t.TempDir(), "missing.json"))
	if _, err := LoadSession(p, "default"); err == nil || !strings.Contains(err.Error(), CookieJarEnv) {
		t.Fatalf("missing env export: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("not cookies"), 0o600)
	t.Setenv(CookieJarEnv, bad)
	if _, err := LoadSession(p, "default"); err == nil {
		t.Fatal("a non-cookie env export was accepted")
	}
}

func TestLoadSessionCorruptFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "default.json"), []byte("{"), 0o600)
	if _, err := LoadSession(Paths{SessionsDir: dir}, "default"); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt session: %v", err)
	}
}

func TestSaveSessionRefusesBadProfiles(t *testing.T) {
	p := Paths{SessionsDir: t.TempDir()}
	s := &StoredSession{}
	for _, bad := range []string{"../x", ""} {
		if err := SaveSession(p, bad, s); err == nil {
			t.Errorf("profile %q accepted", bad)
		}
	}
}

func TestRemoveSession(t *testing.T) {
	p := Paths{SessionsDir: t.TempDir()}
	if removed, err := RemoveSession(p, "default"); err != nil || removed {
		t.Fatalf("missing = %v %v", removed, err)
	}
	if err := SaveSession(p, "work", &StoredSession{}); err != nil {
		t.Fatal(err)
	}
	if removed, err := RemoveSession(p, "work"); err != nil || !removed {
		t.Fatalf("remove = %v %v", removed, err)
	}
	if _, err := RemoveSession(p, "../escape"); err == nil {
		t.Fatal("bad profile accepted")
	}
}

func TestProfilesListsSessionNames(t *testing.T) {
	p := Paths{SessionsDir: t.TempDir()}
	if got, err := Profiles(p); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v %v", got, err)
	}
	for _, name := range []string{"b-work", "a-home"} {
		os.WriteFile(filepath.Join(p.SessionsDir, name+".json"), []byte("{}"), 0o600)
	}
	os.WriteFile(filepath.Join(p.SessionsDir, "notes.txt"), []byte("x"), 0o600)
	os.Mkdir(filepath.Join(p.SessionsDir, "dir.json"), 0o700)
	got, err := Profiles(p)
	if err != nil || strings.Join(got, ",") != "a-home,b-work" {
		t.Fatalf("profiles = %v %v", got, err)
	}
}

func TestValidProfileAccepted(t *testing.T) {
	for _, ok := range []string{"a", "A1", "a-b_c", strings.Repeat("x", 64)} {
		if err := ValidProfile(ok); err != nil {
			t.Errorf("ValidProfile(%q) = %v", ok, err)
		}
	}
	if err := ValidProfile(strings.Repeat("x", 65)); err == nil {
		t.Fatal("a 65-character profile was accepted")
	}
}

func TestResolveProfilePrecedence(t *testing.T) {
	t.Setenv("LEROYMERLIN_PROFILE", "")
	if got := ResolveProfile("flag", Config{DefaultProfile: "cfg"}); got != "flag" {
		t.Fatalf("flag lost: %q", got)
	}
	t.Setenv("LEROYMERLIN_PROFILE", "env")
	if got := ResolveProfile("", Config{DefaultProfile: "cfg"}); got != "env" {
		t.Fatalf("env lost: %q", got)
	}
	t.Setenv("LEROYMERLIN_PROFILE", "")
	if got := ResolveProfile("", Config{DefaultProfile: "cfg"}); got != "cfg" {
		t.Fatalf("config lost: %q", got)
	}
	if got := ResolveProfile("", Config{}); got != "default" {
		t.Fatalf("default = %q", got)
	}
}

func TestSaveConfigFailsOnAnUnwritablePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := Save(filepath.Join(dir, "sub", "config.toml"), Config{}); err == nil {
		t.Fatal("saving under a read-only directory succeeded")
	}
}

func TestLoadUnreadableConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("default_profile = \"x\"\n"), 0o000)
	if _, err := Load(path); err == nil {
		t.Fatal("an unreadable config was accepted")
	}
}

func TestWriteAtomicFailsWhenTheParentIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("x"), 0o600)
	if err := WriteAtomic(filepath.Join(file, "child"), []byte("y"), 0o600); err == nil {
		t.Fatal("a path under a regular file was written")
	}
}

func TestLoadSessionUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "default.json"), []byte("{}"), 0o000)
	if _, err := LoadSession(Paths{SessionsDir: dir}, "default"); err == nil {
		t.Fatal("an unreadable session was accepted")
	}
}

func TestSaveSessionFailsOnAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := SaveSession(Paths{SessionsDir: dir}, "default", &StoredSession{}); err == nil {
		t.Fatal("a session was saved into a read-only directory")
	}
}

func TestProfilesFailsWhenTheDirectoryIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sessions")
	os.WriteFile(file, []byte("x"), 0o600)
	if _, err := Profiles(Paths{SessionsDir: file}); err == nil {
		t.Fatal("a sessions path that is a file was accepted")
	}
}
