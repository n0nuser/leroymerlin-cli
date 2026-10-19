// Package config owns what the CLI keeps on disk outside the state database:
// XDG paths, config.toml (hand-edited, may be shared) and one session file per
// profile (cookies, 0600). They live apart so a shared config never carries a
// session.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/pelletier/go-toml/v2"
)

const appDir = "leroymerlin-cli"

// Paths says where each file lives. LEROYMERLIN_CONFIG moves config.toml only;
// sessions and state follow XDG, so a relocated config cannot point the CLI at
// another person's cookies.
type Paths struct {
	ConfigFile  string
	SessionsDir string
	StateDB     string
	StateDir    string
}

// DefaultPaths resolves the paths from the current environment.
func DefaultPaths() Paths {
	xdg.Reload() // honour XDG_* set after process start (tests, wrappers)
	p := Paths{
		ConfigFile:  filepath.Join(xdg.ConfigHome, appDir, "config.toml"),
		SessionsDir: filepath.Join(xdg.ConfigHome, appDir, "sessions"),
		StateDB:     filepath.Join(xdg.DataHome, appDir, "state.db"),
		StateDir:    filepath.Join(xdg.StateHome, appDir),
	}
	if v := os.Getenv("LEROYMERLIN_CONFIG"); v != "" {
		p.ConfigFile = v
	}
	return p
}

// Config is config.toml. Unknown keys are errors, so a typo never silently
// does nothing.
type Config struct {
	DefaultProfile string `toml:"default_profile,omitempty" json:"default_profile,omitempty"`
}

// Load reads config.toml; a missing file is an empty config.
func Load(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Save writes config.toml atomically.
func Save(path string, c Config) error {
	raw, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return WriteAtomic(path, raw, 0o644)
}

// ResolveProfile applies --profile > LEROYMERLIN_PROFILE > config default > "default".
func ResolveProfile(flag string, c Config) string {
	switch {
	case flag != "":
		return flag
	case os.Getenv("LEROYMERLIN_PROFILE") != "":
		return os.Getenv("LEROYMERLIN_PROFILE")
	case c.DefaultProfile != "":
		return c.DefaultProfile
	}
	return "default"
}

// WriteAtomic writes data to a temp file beside path and renames it over
// path, so a crash never leaves half a file. The parent directory is created
// 0700 and the mode is applied with Chmod as well, since a relaxed umask would
// otherwise widen it.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
