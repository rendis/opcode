package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// sha256Hex computes the SHA-256 hex digest of r.
func sha256Hex(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sha256File computes the SHA-256 hex digest of a file.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sha256Hex(f)
}

// parseChecksumFile parses a standard checksums file (e.g. shasum -a 256 output).
// Each line: "<hex>  <filename>" or "<hex> <filename>".
// Returns map[filename]hex. Malformed lines are skipped.
func parseChecksumFile(r io.Reader) (map[string]string, error) {
	result := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "hash  filename" (two spaces) or "hash filename" (one space).
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		hash := parts[0]
		name := parts[len(parts)-1]
		if len(hash) != 64 { // SHA-256 hex is 64 chars
			continue
		}
		result[name] = hash
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading checksums: %w", err)
	}
	return result, nil
}

// downloadToTempFile downloads url to a temporary file in dir.
// Returns the temp file path. Caller must remove.
func downloadToTempFile(url, dir string, client httpGetter) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	f, err := os.CreateTemp(dir, "download-*")
	if err != nil {
		return "", err
	}
	path := f.Name()

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// httpGetter is satisfied by *http.Client.
type httpGetter interface {
	Get(url string) (*http.Response, error)
}
