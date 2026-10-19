// Package updater finds the latest stable GitHub release and swaps the
// running binary for it after checking the release's SHA-256 checksums.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// latestURL and downloadURL are variables so tests can point the updater at a
// local server; production always uses the GitHub release.
var (
	latestURL   = "https://api.github.com/repos/n0nuser/leroymerlin-cli/releases/latest"
	downloadURL = "https://github.com/n0nuser/leroymerlin-cli/releases/download/"
)

const (
	binaryName  = "leroymerlin"
	maxDownload = 64 << 20
)

var semver = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Release is the part of the GitHub release object the updater reads.
type Release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Stable reports whether v is a plain X.Y.Z release (not dev, not a prerelease).
func Stable(v string) bool { return semver.MatchString(v) }

// Newer reports whether latest is a higher X.Y.Z than current.
func Newer(latest, current string) bool {
	l, c := semver.FindStringSubmatch(latest), semver.FindStringSubmatch(current)
	if l == nil || c == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		a, _ := strconv.Atoi(l[i])
		b, _ := strconv.Atoi(c[i])
		if a != b {
			return a > b
		}
	}
	return false
}

// Client is the HTTP client for GitHub; stdlib TLS, since GitHub is not the
// storefront.
func Client() *http.Client { return &http.Client{Timeout: 60 * time.Second} }

func fetch(ctx context.Context, c *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "leroymerlin-cli-updater")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("download larger than expected")
	}
	return b, err
}

// Latest returns the newest stable release.
func Latest(ctx context.Context, c *http.Client) (Release, error) {
	raw, err := fetch(ctx, c, latestURL, 1<<20)
	if err != nil {
		return Release{}, err
	}
	var r Release
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	if r.Draft || r.Prerelease || !Stable(r.Tag) {
		return r, fmt.Errorf("latest release %q is not a stable version", r.Tag)
	}
	return r, nil
}

// ArchiveName is the goreleaser archive for this platform.
func ArchiveName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", binaryName, strings.TrimPrefix(tag, "v"), goos, goarch, ext)
}

// Install downloads r for this platform, verifies it and replaces exe.
func Install(ctx context.Context, c *http.Client, r Release, exe string) error {
	name := ArchiveName(r.Tag, runtime.GOOS, runtime.GOARCH)
	sums, err := fetch(ctx, c, downloadURL+r.Tag+"/checksums.txt", 1<<20)
	if err != nil {
		return err
	}
	archive, err := fetch(ctx, c, downloadURL+r.Tag+"/"+name, maxDownload)
	if err != nil {
		return err
	}
	if err := Verify(archive, sums, name); err != nil {
		return err
	}
	bin, err := Extract(archive, strings.HasSuffix(name, ".zip"))
	if err != nil {
		return err
	}
	return Replace(exe, bin)
}

// Verify checks data against its line in a goreleaser checksums.txt.
func Verify(data, sums []byte, name string) error {
	sum := sha256.Sum256(data)
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if f[0] != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("checksum mismatch for %s", name)
			}
			return nil
		}
	}
	return fmt.Errorf("%s is not listed in checksums.txt", name)
}

// Extract returns the executable from the archive. Only that one entry is
// read; nothing in the archive is written to disk by name.
func Extract(archive []byte, isZip bool) ([]byte, error) {
	want := binaryName
	if isZip {
		want += ".exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name == want && f.Mode().IsRegular() {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, maxDownload))
			}
		}
		return nil, fmt.Errorf("archive has no %s", want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("archive has no %s", want)
		}
		if err != nil {
			return nil, err
		}
		if h.Name == want && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
}

// Replace swaps exe for bin atomically, keeping exe's permissions. Windows
// cannot overwrite a running binary, so the old one is moved aside first.
func Replace(exe string, bin []byte) error {
	if len(bin) == 0 {
		return errors.New("empty executable")
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".leroymerlin-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", exe, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := tmp.Name() + ".old"
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(tmp.Name(), exe); err != nil {
			_ = os.Rename(old, exe)
			return err
		}
		_ = os.Remove(old) // fails while the old binary runs; harmless
		return nil
	}
	return os.Rename(tmp.Name(), exe)
}
