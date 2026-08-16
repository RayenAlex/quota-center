package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	libraryPath := flag.String("library", "", "compiled plugin library")
	archivePath := flag.String("archive", "", "output CPA plugin archive")
	checksumPath := flag.String("checksum", "", "output archive checksum")
	flag.Parse()
	if *libraryPath == "" || *archivePath == "" || *checksumPath == "" {
		fatal("library, archive, and checksum are required")
	}
	if err := packageLibrary(*libraryPath, *archivePath); err != nil {
		fatal("package: %v", err)
	}
	if err := writeChecksum(*archivePath, *checksumPath); err != nil {
		fatal("checksum: %v", err)
	}
}

func packageLibrary(libraryPath, archivePath string) error {
	library, err := os.Open(libraryPath)
	if err != nil {
		return err
	}
	defer library.Close()
	info, err := library.Stat()
	if err != nil {
		return err
	}
	archive, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	writer := zip.NewWriter(archive)
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.Base(libraryPath)
	header.Method = zip.Deflate
	header.SetMode(0o755)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(entry, library); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return archive.Close()
}

func writeChecksum(archivePath, checksumPath string) error {
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(archivePath))
	return os.WriteFile(checksumPath, []byte(line), 0o644)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
