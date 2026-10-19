package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewerAndStable(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "0.1.9", true},
		{"v0.10.0", "0.9.0", true}, // numeric, not lexical
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "dev", false},
		{"v1.0.0-rc1", "0.9.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q,%q) = %v", c.latest, c.current, got)
		}
	}
	if Stable("dev") || Stable("0.1.0-rc1") || !Stable("0.1.0") {
		t.Error("Stable misclassifies")
	}
}

func TestVerifyExtractReplace(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	payload := []byte("#!/bin/sh\necho new\n")
	tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: binaryName, Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg})
	tw.Write(payload)
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	name := ArchiveName("v1.2.3", "linux", "amd64")
	if name != "leroymerlin_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("archive name %s", name)
	}
	sum := sha256.Sum256(archive)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	if err := Verify(archive, sums, name); err != nil {
		t.Fatal(err)
	}
	if err := Verify(append(archive, 0), sums, name); err == nil {
		t.Fatal("tampered archive verified")
	}
	bin, err := Extract(archive, false)
	if err != nil || !bytes.Equal(bin, payload) {
		t.Fatalf("extract: %q %v", bin, err)
	}
	exe := filepath.Join(t.TempDir(), binaryName)
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := Replace(exe, bin); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	fi, _ := os.Stat(exe)
	if !bytes.Equal(got, payload) || fi.Mode().Perm() != 0o755 {
		t.Fatalf("replaced binary %q mode %v", got, fi.Mode())
	}
}

func tarball(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func withEndpoints(t *testing.T, latest, download string) {
	t.Helper()
	oldLatest, oldDownload := latestURL, downloadURL
	latestURL, downloadURL = latest, download
	t.Cleanup(func() { latestURL, downloadURL = oldLatest, oldDownload })
}

func TestLatestReadsOnlyStableReleases(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
		code int
	}{
		{"stable", `{"tag_name":"v1.2.3"}`, true, 200},
		{"draft", `{"tag_name":"v1.2.3","draft":true}`, false, 200},
		{"prerelease", `{"tag_name":"v1.2.3","prerelease":true}`, false, 200},
		{"not semver", `{"tag_name":"nightly"}`, false, 200},
		{"broken json", `{`, false, 200},
		{"server error", `{}`, false, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			withEndpoints(t, srv.URL, "")
			rel, err := Latest(context.Background(), srv.Client())
			if tc.ok {
				if err != nil || rel.Tag != "v1.2.3" {
					t.Fatalf("Latest = %+v, %v", rel, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Latest accepted %s", tc.body)
			}
		})
	}
}

func TestInstallDownloadsVerifiesAndReplaces(t *testing.T) {
	payload := []byte("#!/bin/sh\necho v2\n")
	name := ArchiveName("v2.0.0", runtime.GOOS, runtime.GOARCH)
	archive := tarball(t, binaryName, payload)
	sum := sha256.Sum256(archive)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			w.Write(sums)
		case strings.HasSuffix(r.URL.Path, "/"+name):
			w.Write(archive)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	withEndpoints(t, "", srv.URL+"/")
	exe := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), srv.Client(), Release{Tag: "v2.0.0"}, exe); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary = %q", got)
	}
}

func TestInstallRejectsABadChecksum(t *testing.T) {
	name := ArchiveName("v2.0.0", runtime.GOOS, runtime.GOARCH)
	archive := tarball(t, binaryName, []byte("x"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
			w.Write([]byte(strings.Repeat("0", 64) + "  " + name + "\n"))
			return
		}
		w.Write(archive)
	}))
	defer srv.Close()
	withEndpoints(t, "", srv.URL+"/")
	exe := filepath.Join(t.TempDir(), binaryName)
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := Install(context.Background(), srv.Client(), Release{Tag: "v2.0.0"}, exe); err == nil {
		t.Fatal("a mismatched checksum was accepted")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("binary changed to %q", got)
	}
}

func TestInstallRejectsAMissingArchive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
			w.Write([]byte(strings.Repeat("0", 64) + "  other.tar.gz\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	withEndpoints(t, "", srv.URL+"/")
	if err := Install(context.Background(), srv.Client(), Release{Tag: "v2.0.0"}, filepath.Join(t.TempDir(), "exe")); err == nil {
		t.Fatal("a missing archive was accepted")
	}
}

func TestFetchRejectsOversizedBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("a"), 100))
	}))
	defer srv.Close()
	if _, err := fetch(context.Background(), srv.Client(), srv.URL, 10); err == nil {
		t.Fatal("an oversized body was accepted")
	}
	if _, err := fetch(context.Background(), srv.Client(), srv.URL, 1000); err != nil {
		t.Fatalf("a body within the limit failed: %v", err)
	}
}

func TestExtractZip(t *testing.T) {
	payload := []byte("windows binary")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("leroymerlin.exe")
	f.Write(payload)
	zw.Close()
	got, err := Extract(buf.Bytes(), true)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("zip extract = %q %v", got, err)
	}
	if _, err := Extract(buf.Bytes(), false); err == nil {
		t.Fatal("a zip parsed as gzip")
	}
	empty := zipWith(t, "other.exe", payload)
	if _, err := Extract(empty, true); err == nil {
		t.Fatal("a zip without the binary was accepted")
	}
}

func zipWith(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create(name)
	f.Write(payload)
	zw.Close()
	return buf.Bytes()
}

func TestExtractErrors(t *testing.T) {
	if _, err := Extract([]byte("not gzip"), false); err == nil {
		t.Fatal("broken gzip accepted")
	}
	archive := tarball(t, "README.md", []byte("hi"))
	if _, err := Extract(archive, false); err == nil {
		t.Fatal("an archive without the binary was accepted")
	}
}

func TestVerifyUnlistedArchive(t *testing.T) {
	if err := Verify([]byte("x"), []byte(strings.Repeat("0", 64)+"  other.tar.gz\n"), "leroymerlin_1.0.0_linux_amd64.tar.gz"); err == nil {
		t.Fatal("an unlisted archive verified")
	}
	if err := Verify([]byte("x"), []byte("short line\n"), "x"); err == nil {
		t.Fatal("a malformed checksum line verified")
	}
}

func TestReplaceErrors(t *testing.T) {
	if err := Replace(filepath.Join(t.TempDir(), "exe"), nil); err == nil {
		t.Fatal("an empty binary was written")
	}
	if err := Replace(filepath.Join(t.TempDir(), "missing"), []byte("x")); err == nil {
		t.Fatal("a missing target was accepted")
	}
}

func TestArchiveNameWindows(t *testing.T) {
	if got := ArchiveName("v1.0.0", "windows", "amd64"); got != "leroymerlin_1.0.0_windows_amd64.zip" {
		t.Fatalf("windows archive = %q", got)
	}
}
