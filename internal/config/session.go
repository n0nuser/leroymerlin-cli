package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
)

// CookieJarEnv names a cookie export (any format `auth login` reads) to use
// instead of the stored session. It is read on every run and never copied to
// disk, cookie rotations included.
const CookieJarEnv = "LEROYMERLIN_COOKIEJAR"

// Session sources.
const (
	SourceFile = "file"
	SourceEnv  = "env"
)

// StoredSession is one profile's session file.
type StoredSession struct {
	leroymerlin.Session
	ImportedAt   time.Time `json:"imported_at"`
	ImportFormat string    `json:"import_format"`

	// Source says where this session came from at runtime. Env sessions are
	// never saved: SaveSession refuses them.
	Source string `json:"-"`
	Path   string `json:"-"`
}

var profileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// ValidProfile rejects names that would escape the sessions directory.
func ValidProfile(name string) error {
	if !profileName.MatchString(name) {
		return fmt.Errorf("profile %q: use letters, digits, - and _ (max 64)", name)
	}
	return nil
}

// SessionPath is where a profile's session lives.
func (p Paths) SessionPath(profile string) string {
	return filepath.Join(p.SessionsDir, profile+".json")
}

// LoadSession returns the session for profile: LEROYMERLIN_COOKIEJAR when
// set, else the profile's file. (nil, nil) means there is none.
func LoadSession(p Paths, profile string) (*StoredSession, error) {
	if path := os.Getenv(CookieJarEnv); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", CookieJarEnv, err)
		}
		res, err := leroymerlin.ParseExport(raw, leroymerlin.FormatAuto)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", CookieJarEnv, err)
		}
		return &StoredSession{Session: res.Session, ImportFormat: res.Format, Source: SourceEnv, Path: path}, nil
	}
	if err := ValidProfile(profile); err != nil {
		return nil, err
	}
	path := p.SessionPath(profile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s StoredSession
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s is corrupt (%v). Run `leroymerlin auth login` again", path, err)
	}
	s.Source, s.Path = SourceFile, path
	return &s, nil
}

// SaveSession writes the profile's session file, 0600, atomically.
func SaveSession(p Paths, profile string, s *StoredSession) error {
	if s.Source == SourceEnv {
		return fmt.Errorf("refusing to write a %s session to disk", CookieJarEnv)
	}
	if err := ValidProfile(profile); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := WriteAtomic(p.SessionPath(profile), raw, 0o600); err != nil {
		return err
	}
	s.Source, s.Path = SourceFile, p.SessionPath(profile)
	return nil
}

// RemoveSession deletes the profile's session file. Missing is not an error.
func RemoveSession(p Paths, profile string) (bool, error) {
	if err := ValidProfile(profile); err != nil {
		return false, err
	}
	err := os.Remove(p.SessionPath(profile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Profiles lists the profiles that have a session file.
func Profiles(p Paths) ([]string, error) {
	entries, err := os.ReadDir(p.SessionsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && ValidProfile(name) == nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}
