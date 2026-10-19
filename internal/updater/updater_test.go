package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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
