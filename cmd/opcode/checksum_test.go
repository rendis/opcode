package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSha256Hex(t *testing.T) {
	input := "hello world\n"
	r := strings.NewReader(input)
	got, err := sha256Hex(r)
	require.NoError(t, err)

	h := sha256.Sum256([]byte(input))
	want := hex.EncodeToString(h[:])
	assert.Equal(t, want, got)
}

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	data := []byte("opcode test data")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	got, err := sha256File(path)
	require.NoError(t, err)

	h := sha256.Sum256(data)
	want := hex.EncodeToString(h[:])
	assert.Equal(t, want, got)
}

func TestSha256File_NotFound(t *testing.T) {
	_, err := sha256File("/nonexistent/file")
	assert.Error(t, err)
}

func TestParseChecksumFile(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "standard two-space format",
			input: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd  opcode_Darwin_arm64.tar.gz\n" +
				"fedcba98fedcba98fedcba98fedcba98fedcba98fedcba98fedcba98fedcba98  opcode_Linux_x86_64.tar.gz\n",
			want: map[string]string{
				"opcode_Darwin_arm64.tar.gz":  "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
				"opcode_Linux_x86_64.tar.gz": "fedcba98fedcba98fedcba98fedcba98fedcba98fedcba98fedcba98fedcba98",
			},
		},
		{
			name:  "empty input",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "blank lines and whitespace",
			input: "\n  \n\n",
			want:  map[string]string{},
		},
		{
			name:  "malformed line (no filename)",
			input: "abc123\n",
			want:  map[string]string{},
		},
		{
			name:  "short hash skipped",
			input: "abc123  file.tar.gz\n",
			want:  map[string]string{},
		},
		{
			name: "single space separator",
			input: "abc123def456abc123def456abc123def456abc123def456abc123def456abcd file.tar.gz\n",
			want: map[string]string{
				"file.tar.gz": "abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChecksumFile(strings.NewReader(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindChecksumAsset(t *testing.T) {
	release := &githubRelease{
		TagName: "v1.0.0",
		Assets: []githubAsset{
			{Name: "opcode_Darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/a"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
			{Name: "opcode_Linux_x86_64.tar.gz", BrowserDownloadURL: "https://example.com/b"},
		},
	}

	asset := findChecksumAsset(release)
	require.NotNil(t, asset)
	assert.Equal(t, "checksums.txt", asset.Name)
	assert.Equal(t, "https://example.com/checksums", asset.BrowserDownloadURL)
}

func TestFindChecksumAsset_Missing(t *testing.T) {
	release := &githubRelease{
		TagName: "v0.9.0",
		Assets: []githubAsset{
			{Name: "opcode_Darwin_arm64.tar.gz"},
		},
	}

	assert.Nil(t, findChecksumAsset(release))
}
