package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const mermaidASCIIVersion = "1.1.0"

func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	listenAddr := fs.String("listen-addr", ":4100", "TCP listen address")
	baseURL := fs.String("base-url", "", "public base URL (derived from listen-addr if empty)")
	dbPath := fs.String("db-path", "", "database path (default: ~/.opcode/opcode.db)")
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, error")
	poolSize := fs.Int("pool-size", 10, "worker pool size")
	vaultKey := fs.String("vault-key", "", "vault passphrase (memory only, not persisted to disk)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	dir := opcodeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot create %s: %v\n", dir, err)
		os.Exit(1)
	}

	cfg := Config{
		ListenAddr: *listenAddr,
		BaseURL:    *baseURL,
		LogLevel:   *logLevel,
		PoolSize:   *poolSize,
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	} else {
		cfg.DBPath = filepath.Join(dir, "opcode.db")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost" + cfg.ListenAddr
	}

	data, _ := json.MarshalIndent(cfg, "", "  ")
	path := settingsPath()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("Config written to %s\n", path)

	// Download external tools.
	installMermaidASCII(filepath.Join(dir, "bin"))

	// Set vault key in env (memory only) if provided.
	if *vaultKey != "" {
		os.Setenv("OPCODE_VAULT_KEY", *vaultKey)
	}

	// Start the server.
	runServe()
}

// installMermaidASCII downloads the mermaid-ascii binary to binDir.
// Non-fatal: logs a warning and continues if the download fails.
func installMermaidASCII(binDir string) {
	destPath := filepath.Join(binDir, "mermaid-ascii")

	// Skip if already installed.
	if _, err := os.Stat(destPath); err == nil {
		fmt.Printf("mermaid-ascii already installed at %s\n", destPath)
		return
	}

	assetName, err := mermaidASCIIAssetName()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v — ASCII diagrams will use fallback renderer\n", err)
		return
	}

	url := fmt.Sprintf("https://github.com/AlexanderGrooff/mermaid-ascii/releases/download/%s/%s",
		mermaidASCIIVersion, assetName)

	fmt.Printf("Downloading mermaid-ascii %s...\n", mermaidASCIIVersion)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot create %s: %v\n", binDir, err)
		return
	}

	resp, err := http.Get(url) //nolint:gosec // trusted URL, pinned version
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: download failed: %v — ASCII diagrams will use fallback renderer\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Warning: download returned %d — ASCII diagrams will use fallback renderer\n", resp.StatusCode)
		return
	}

	if strings.HasSuffix(assetName, ".tar.gz") {
		err = extractTarGz(resp.Body, binDir, "mermaid-ascii")
	} else {
		fmt.Fprintf(os.Stderr, "Warning: unsupported archive format: %s\n", assetName)
		return
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: extraction failed: %v — ASCII diagrams will use fallback renderer\n", err)
		_ = os.Remove(destPath)
		return
	}

	if err := os.Chmod(destPath, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: chmod failed: %v\n", err)
	}

	fmt.Printf("mermaid-ascii installed to %s\n", destPath)
}

// mermaidASCIIAssetName returns the GitHub release asset name for the current platform.
func mermaidASCIIAssetName() (string, error) {
	osName := ""
	switch runtime.GOOS {
	case "darwin":
		osName = "Darwin"
	case "linux":
		osName = "Linux"
	default:
		return "", fmt.Errorf("mermaid-ascii: unsupported OS %q", runtime.GOOS)
	}

	archName := ""
	switch runtime.GOARCH {
	case "amd64":
		archName = "x86_64"
	case "arm64":
		archName = "arm64"
	case "386":
		archName = "i386"
	default:
		return "", fmt.Errorf("mermaid-ascii: unsupported architecture %q", runtime.GOARCH)
	}

	return fmt.Sprintf("mermaid-ascii_%s_%s.tar.gz", osName, archName), nil
}

// extractTarGz extracts a specific file from a tar.gz archive into destDir.
func extractTarGz(r io.Reader, destDir, targetName string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("file %q not found in archive", targetName)
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		// Match by base name (archive may include directory prefix).
		if filepath.Base(hdr.Name) != targetName {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		destPath := filepath.Join(destDir, targetName)
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("create %s: %w", destPath, err)
		}
		if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // bounded by tar header size
			f.Close()
			return fmt.Errorf("write %s: %w", destPath, err)
		}
		return f.Close()
	}
}
