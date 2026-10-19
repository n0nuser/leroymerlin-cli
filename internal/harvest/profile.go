package harvest

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// A real Chrome profile keeps the cookies in one of two places depending on
// the version, and the key that decrypts them in "Local State". Only these
// files are copied; the rest of the profile never leaves the user's directory.
const localStateFile = "Local State"

var profileFiles = []string{
	"Default/Cookies",
	"Default/Cookies-journal",
	"Default/Cookies-wal",
	"Default/Cookies-shm",
	"Default/Network/Cookies",
	"Default/Network/Cookies-journal",
	"Default/Network/Cookies-wal",
	"Default/Network/Cookies-shm",
	"Default/Preferences",
}

// DetectProfile finds the Chrome profile to copy: the override, or the first
// well-known profile that has a cookie store.
func DetectProfile(override string) (string, error) {
	return detectProfile(profileCandidates(), override)
}

func detectProfile(candidates []string, override string) (string, error) {
	if override != "" {
		root := expandHome(override)
		if err := checkProfile(root); err != nil {
			return "", err
		}
		return root, nil
	}
	for _, root := range candidates {
		if checkProfile(root) == nil {
			return root, nil
		}
	}
	return "", errors.New("no Chrome profile with cookies found. Set --profile or LEROYMERLIN_CHROME_PROFILE to your Chrome profile directory")
}

// checkProfile reports whether root looks like a Chrome profile with a cookie
// store.
func checkProfile(root string) error {
	if root == "" {
		return errors.New("empty Chrome profile directory")
	}
	if hasCookieStore(root) {
		return nil
	}
	return fmt.Errorf("%q has no Default/Cookies or Default/Network/Cookies. Set --profile or LEROYMERLIN_CHROME_PROFILE to the Chrome profile directory", root)
}

func hasCookieStore(root string) bool {
	for _, rel := range []string{"Default/Cookies", "Default/Network/Cookies"} {
		if isFile(filepath.Join(root, rel)) {
			return true
		}
	}
	return false
}

// profileCandidates lists the usual Chrome profile roots, most common first.
func profileCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{
			filepath.Join(home, "Library/Application Support/Google/Chrome"),
			filepath.Join(home, "Library/Application Support/Chromium"),
			filepath.Join(home, "Library/Application Support/BraveSoftware/Brave-Browser"),
		}
	case "windows":
		var out []string
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			out = append(out,
				filepath.Join(base, `Google\Chrome\User Data`),
				filepath.Join(base, `Chromium\User Data`),
			)
		}
		return out
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(cfg, "google-chrome"),
		filepath.Join(cfg, "chromium"),
		filepath.Join(cfg, "google-chrome-beta"),
		filepath.Join(cfg, "chromium-browser"),
	}
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return p
}

// PlanProfileCopy lists the relative files to copy from a profile: Local
// State, the cookie store and its journals, and Preferences when present.
func PlanProfileCopy(root string) ([]string, error) {
	if !hasCookieStore(root) {
		return nil, fmt.Errorf("%q has no Default/Cookies or Default/Network/Cookies. Set --profile to the Chrome profile directory", root)
	}
	if !isFile(filepath.Join(root, localStateFile)) {
		return nil, fmt.Errorf("%q has no Local State, which holds the cookie key. Open Chrome once with that profile, then try again", root)
	}
	files := []string{localStateFile}
	for _, rel := range profileFiles {
		if isFile(filepath.Join(root, rel)) {
			files = append(files, rel)
		}
	}
	return files, nil
}

// PrepareProfile copies a real profile's cookie store into a fresh 0700 temp
// directory and returns it with a cleanup that removes it. The caller must
// always run cleanup: the copy holds the user's cookies.
func PrepareProfile(root, parent string) (string, func() error, error) {
	files, err := PlanProfileCopy(root)
	if err != nil {
		return "", nil, err
	}
	if parent == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", nil, fmt.Errorf("finding a cache directory for the temporary profile: %w", err)
		}
		parent = filepath.Join(cache, "leroymerlin-cli")
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(parent, "harvest-profile-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() error { return removeAllRetry(dir) }
	for _, rel := range files {
		if err := copyFile(filepath.Join(root, rel), filepath.Join(dir, rel)); err != nil {
			_ = cleanup()
			return "", nil, fmt.Errorf("copying %s: %w", rel, err)
		}
	}
	return dir, cleanup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// removeAllRetry removes a directory like os.RemoveAll, retrying briefly
// because Chrome's helper processes can still hold files just after exit.
func removeAllRetry(dir string) error {
	var err error
	for range 25 {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return err
}

// FreePort reserves an unused TCP port on 127.0.0.1 for Chrome's DevTools
// endpoint. There is a small race between closing the listener and Chrome
// binding it; a retry by the caller covers that.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("could not read the reserved port")
	}
	return addr.Port, nil
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
