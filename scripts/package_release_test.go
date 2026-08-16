package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageLibraryCreatesRootOnlyCPAArchive(t *testing.T) {
	dir := t.TempDir()
	library := filepath.Join(dir, "quota-center.dylib")
	if err := os.WriteFile(library, []byte("library"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "quota-center_0.2.0_darwin_arm64.zip")
	checksum := filepath.Join(dir, "quota-center.zip.sha256")
	if err := packageLibrary(library, archive); err != nil {
		t.Fatalf("packageLibrary() error = %v", err)
	}
	if err := writeChecksum(archive, checksum); err != nil {
		t.Fatalf("writeChecksum() error = %v", err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 1 || reader.File[0].Name != "quota-center.dylib" {
		t.Fatalf("entries = %#v", reader.File)
	}
	got, err := os.ReadFile(checksum)
	if err != nil {
		t.Fatal(err)
	}
	archiveData, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archiveData)
	want := hex.EncodeToString(sum[:]) + "  quota-center_0.2.0_darwin_arm64.zip\n"
	if string(got) != want || strings.Count(string(got), "\n") != 1 {
		t.Fatalf("checksum = %q, want %q", got, want)
	}
}
