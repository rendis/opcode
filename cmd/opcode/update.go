package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func runUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	skipVerify := fs.Bool("skip-verify", false, "skip SHA-256 checksum verification")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	fmt.Printf("Current version: %s\n", version)

	// 1. Check GitHub releases.
	release, err := fetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot check GitHub releases: %v\n", err)
		fallbackGoInstall()
		return
	}

	if release == nil {
		fmt.Println("No GitHub releases found")
		fallbackGoInstall()
		return
	}

	// 2. Compare versions.
	if version != "dev" && !isNewer(release.TagName, version) {
		fmt.Printf("Already up to date (%s)\n", version)
		return
	}

	fmt.Printf("New version available: %s\n", release.TagName)

	// 3. Find matching asset for this platform.
	asset := findAsset(release)
	if asset == nil {
		fmt.Fprintf(os.Stderr, "No binary for %s/%s — ", runtime.GOOS, runtime.GOARCH)
		fallbackGoInstall()
		return
	}

	// 4. Load expected checksum (if available).
	var expectedHash string
	if !*skipVerify {
		expectedHash = loadExpectedChecksum(release, asset.Name)
	}

	// 5. Resolve current binary path.
	selfPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}
	selfPath, err = filepath.EvalSymlinks(selfPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot resolve executable path: %v\n", err)
		os.Exit(1)
	}

	// 6. Download, verify, and extract.
	fmt.Printf("Downloading %s...\n", asset.Name)
	binPath, tmpDir, err := downloadVerifyAndExtract(asset.BrowserDownloadURL, expectedHash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: download failed: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	// 7. Replace current binary.
	if err := replaceBinary(selfPath, binPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot replace binary: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Updated to %s\n", release.TagName)

	// 8. Stop running server if any.
	stopIfRunning()
}

// --- GitHub API types ---

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func fetchLatestRelease() (*githubRelease, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/rendis/opcode/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

// --- Version comparison ---

func isNewer(remote, local string) bool {
	if local == "dev" {
		return true
	}
	// If local contains git-describe suffix (e.g., "v1.0.0-3-gabcdef"), always update.
	localClean := strings.TrimPrefix(local, "v")
	if strings.Count(localClean, "-") > 0 {
		return true
	}
	return compareSemver(remote, local) > 0
}

func compareSemver(a, b string) int {
	ap := semverParts(a)
	bp := semverParts(b)
	for i := 0; i < 3; i++ {
		if ap[i] > bp[i] {
			return 1
		}
		if ap[i] < bp[i] {
			return -1
		}
	}
	return 0
}

func semverParts(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		p, _, _ = strings.Cut(p, "-")
		result[i], _ = strconv.Atoi(p)
	}
	return result
}

// --- Asset matching ---

func findAsset(release *githubRelease) *githubAsset {
	name, err := opcodeAssetName()
	if err != nil {
		return nil
	}
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i]
		}
	}
	return nil
}

func opcodeAssetName() (string, error) {
	osName := ""
	switch runtime.GOOS {
	case "darwin":
		osName = "Darwin"
	case "linux":
		osName = "Linux"
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	archName := ""
	switch runtime.GOARCH {
	case "amd64":
		archName = "x86_64"
	case "arm64":
		archName = "arm64"
	default:
		return "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}

	return fmt.Sprintf("opcode_%s_%s.tar.gz", osName, archName), nil
}

// --- Checksum helpers ---

// loadExpectedChecksum fetches checksums.txt from the release and returns the
// expected SHA-256 for assetName. Returns "" if checksums.txt is missing or
// does not contain an entry (backward-compat with old releases).
func loadExpectedChecksum(release *githubRelease, assetName string) string {
	csAsset := findChecksumAsset(release)
	if csAsset == nil {
		fmt.Fprintln(os.Stderr, "Warning: release has no checksums.txt — skipping verification")
		return ""
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(csAsset.BrowserDownloadURL) //nolint:gosec // trusted GitHub release URL
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot download checksums.txt: %v — skipping verification\n", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Warning: checksums.txt returned %d — skipping verification\n", resp.StatusCode)
		return ""
	}

	checksums, err := parseChecksumFile(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot parse checksums.txt: %v — skipping verification\n", err)
		return ""
	}

	hash, ok := checksums[assetName]
	if !ok {
		fmt.Fprintf(os.Stderr, "Warning: no checksum for %s in checksums.txt — skipping verification\n", assetName)
		return ""
	}
	return hash
}

func findChecksumAsset(release *githubRelease) *githubAsset {
	for i := range release.Assets {
		if release.Assets[i].Name == "checksums.txt" {
			return &release.Assets[i]
		}
	}
	return nil
}

// --- Download + Verify + Replace ---

func downloadVerifyAndExtract(url, expectedHash string) (binPath, tmpDir string, err error) {
	tmpDir, err = os.MkdirTemp("", "opcode-update-*")
	if err != nil {
		return "", "", err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	archivePath, err := downloadToTempFile(url, tmpDir, client)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}

	// Verify checksum if expected hash is available.
	if expectedHash != "" {
		actual, err := sha256File(archivePath)
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("computing checksum: %w", err)
		}
		if actual != expectedHash {
			os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actual)
		}
		fmt.Println("Checksum verified")
	}

	// Extract binary from verified archive.
	f, err := os.Open(archivePath)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}
	defer f.Close()

	if err := extractTarGz(f, tmpDir, "opcode"); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}

	binPath = filepath.Join(tmpDir, "opcode")
	if err := os.Chmod(binPath, 0o755); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}

	return binPath, tmpDir, nil
}

func replaceBinary(selfPath, newPath string) error {
	// Atomic replace via rename (works on Unix even if binary is running).
	if err := os.Rename(newPath, selfPath); err == nil {
		return nil
	}
	// Cross-filesystem fallback: copy over.
	src, err := os.Open(newPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(selfPath, os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// --- Restart / fallback ---

func stopIfRunning() {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		fmt.Println("Run `opcode serve` to start the server")
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		fmt.Println("Run `opcode serve` to start the server")
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("Run `opcode serve` to start the server")
		return
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		fmt.Println("Run `opcode serve` to start the server")
		return
	}

	fmt.Printf("Stopping running server (PID %d)...\n", pid)
	_ = proc.Signal(syscall.SIGTERM)

	// Wait for exit (up to 10s).
	for i := 0; i < 100; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break
		}
	}

	fmt.Println("Run `opcode serve` to start the updated server")
}

func fallbackGoInstall() {
	goPath, err := exec.LookPath("go")
	if err != nil {
		fmt.Fprintln(os.Stderr, "No GitHub releases and `go` not in PATH — cannot update")
		os.Exit(1)
	}

	fmt.Println("Falling back to: go install github.com/rendis/opcode/cmd/opcode@latest")
	cmd := exec.Command(goPath, "install", "github.com/rendis/opcode/cmd/opcode@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: go install failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Updated via go install")
	stopIfRunning()
}
