package actions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rendis/opcode/internal/isolation"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test helpers ---

func newFSTestConfig(t *testing.T) (FSConfig, string) {
	t.Helper()
	dir := t.TempDir()
	return FSConfig{
		Limits: isolation.ResourceLimits{
			WritablePaths: []string{dir},
			ReadOnlyPaths: []string{dir},
		},
		MaxReadSize: 1024 * 1024, // 1MB for tests
	}, dir
}

func newFSRestrictedConfig(t *testing.T) (FSConfig, string, string) {
	t.Helper()
	allowed := t.TempDir()
	denied := t.TempDir()
	return FSConfig{
		Limits: isolation.ResourceLimits{
			WritablePaths: []string{allowed},
			ReadOnlyPaths: []string{allowed},
			DenyPaths:     []string{denied},
		},
	}, allowed, denied
}

func findFSAction(cfg FSConfig, name string) Action {
	for _, a := range FSActions(cfg) {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

func execFS(t *testing.T, cfg FSConfig, name string, params map[string]any) (map[string]any, error) {
	t.Helper()
	a := findFSAction(cfg, name)
	require.NotNil(t, a, "action %s not found", name)
	out, err := a.Execute(context.Background(), ActionInput{Params: params})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Data, &result))
	return result, nil
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func requireOpcodeError(t *testing.T, err error, expectedCode string) {
	t.Helper()
	require.Error(t, err)
	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr), "expected OpcodeError, got %T: %v", err, err)
	assert.Equal(t, expectedCode, opErr.Code)
}

// --- FSActions factory ---

func TestFSActions_Count(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	actions := FSActions(cfg)
	assert.Len(t, actions, 8)

	names := make([]string, len(actions))
	for i, a := range actions {
		names[i] = a.Name()
	}
	assert.Contains(t, names, "fs.read")
	assert.Contains(t, names, "fs.write")
	assert.Contains(t, names, "fs.append")
	assert.Contains(t, names, "fs.delete")
	assert.Contains(t, names, "fs.list")
	assert.Contains(t, names, "fs.stat")
	assert.Contains(t, names, "fs.copy")
	assert.Contains(t, names, "fs.move")
}

func TestFSActions_DefaultMaxReadSize(t *testing.T) {
	cfg := FSConfig{}
	actions := FSActions(cfg)
	// Verify it doesn't panic with zero MaxReadSize (defaults applied internally).
	assert.Len(t, actions, 8)
}

// --- fs.read tests ---

func TestFSRead_TextFile(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "hello.txt")
	writeTestFile(t, path, "hello world")

	result, err := execFS(t, cfg, "fs.read", map[string]any{"path": path})
	require.NoError(t, err)
	assert.Equal(t, "hello world", result["content"])
	assert.Equal(t, "text", result["encoding"])
	assert.Equal(t, float64(11), result["size"])
	assert.Equal(t, path, result["path"])
}

func TestFSRead_BinaryFile_AutoEncoding(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "data.bin")
	binData := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x0D, 0x0A, 0x1A} // PNG-like with null byte
	require.NoError(t, os.WriteFile(path, binData, 0644))

	result, err := execFS(t, cfg, "fs.read", map[string]any{"path": path})
	require.NoError(t, err)
	assert.Equal(t, "base64", result["encoding"])
	decoded, decErr := base64.StdEncoding.DecodeString(result["content"].(string))
	require.NoError(t, decErr)
	assert.Equal(t, binData, decoded)
}

func TestFSRead_ForceBase64Encoding(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "text.txt")
	writeTestFile(t, path, "plain text")

	result, err := execFS(t, cfg, "fs.read", map[string]any{
		"path":     path,
		"encoding": "base64",
	})
	require.NoError(t, err)
	assert.Equal(t, "base64", result["encoding"])
	decoded, decErr := base64.StdEncoding.DecodeString(result["content"].(string))
	require.NoError(t, decErr)
	assert.Equal(t, "plain text", string(decoded))
}

func TestFSRead_ForceTextEncoding(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "text.txt")
	writeTestFile(t, path, "hello")

	result, err := execFS(t, cfg, "fs.read", map[string]any{
		"path":     path,
		"encoding": "text",
	})
	require.NoError(t, err)
	assert.Equal(t, "text", result["encoding"])
	assert.Equal(t, "hello", result["content"])
}

func TestFSRead_EmptyFile(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "empty.txt")
	writeTestFile(t, path, "")

	result, err := execFS(t, cfg, "fs.read", map[string]any{"path": path})
	require.NoError(t, err)
	assert.Equal(t, "", result["content"])
	assert.Equal(t, "text", result["encoding"])
	assert.Equal(t, float64(0), result["size"])
}

func TestFSRead_Validate_MissingPath(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.read", map[string]any{})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSRead_Validate_InvalidEncoding(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "hello.txt")
	writeTestFile(t, path, "hello")

	_, err := execFS(t, cfg, "fs.read", map[string]any{
		"path":     path,
		"encoding": "gzip",
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSRead_FileNotFound(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.read", map[string]any{
		"path": filepath.Join(dir, "nonexistent.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSRead_PathDenied(t *testing.T) {
	cfg, _, denied := newFSRestrictedConfig(t)
	path := filepath.Join(denied, "secret.txt")
	writeTestFile(t, path, "secret")

	_, err := execFS(t, cfg, "fs.read", map[string]any{"path": path})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

func TestFSRead_NilParams(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	a := findFSAction(cfg, "fs.read")
	require.NotNil(t, a)

	_, err := a.Execute(context.Background(), ActionInput{Params: nil})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSRead_Schema(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	a := findFSAction(cfg, "fs.read")
	require.NotNil(t, a)

	s := a.Schema()
	assert.NotEmpty(t, s.Description)
	assert.NotEmpty(t, s.InputSchema)
	assert.NotEmpty(t, s.OutputSchema)
}

// --- fs.write tests ---

func TestFSWrite_NewFile(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "output.txt")

	result, err := execFS(t, cfg, "fs.write", map[string]any{
		"path":    path,
		"content": "hello write",
	})
	require.NoError(t, err)
	assert.Equal(t, path, result["path"])
	assert.Equal(t, float64(11), result["size"])

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "hello write", string(data))
}

func TestFSWrite_OverwriteExisting(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "overwrite.txt")
	writeTestFile(t, path, "original")

	result, err := execFS(t, cfg, "fs.write", map[string]any{
		"path":    path,
		"content": "replaced",
	})
	require.NoError(t, err)
	assert.Equal(t, float64(8), result["size"])

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "replaced", string(data))
}

func TestFSWrite_CreateDirs(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "sub", "deep", "file.txt")

	result, err := execFS(t, cfg, "fs.write", map[string]any{
		"path":        path,
		"content":     "nested",
		"create_dirs": true,
	})
	require.NoError(t, err)
	assert.Equal(t, float64(6), result["size"])

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "nested", string(data))
}

func TestFSWrite_CreateDirs_False_MissingDir(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "missing", "file.txt")

	_, err := execFS(t, cfg, "fs.write", map[string]any{
		"path":    path,
		"content": "fail",
	})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSWrite_EmptyContent(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "empty.txt")

	result, err := execFS(t, cfg, "fs.write", map[string]any{
		"path":    path,
		"content": "",
	})
	require.NoError(t, err)
	assert.Equal(t, float64(0), result["size"])

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Empty(t, data)
}

func TestFSWrite_Validate_MissingPath(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.write", map[string]any{
		"content": "data",
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSWrite_Validate_MissingContent(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.write", map[string]any{
		"path": filepath.Join(dir, "file.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSWrite_PathDenied(t *testing.T) {
	cfg, _, denied := newFSRestrictedConfig(t)
	_, err := execFS(t, cfg, "fs.write", map[string]any{
		"path":    filepath.Join(denied, "bad.txt"),
		"content": "nope",
	})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

// --- fs.append tests ---

func TestFSAppend_NewFile(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "append.txt")

	result, err := execFS(t, cfg, "fs.append", map[string]any{
		"path":    path,
		"content": "first line\n",
	})
	require.NoError(t, err)
	assert.Equal(t, path, result["path"])

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "first line\n", string(data))
}

func TestFSAppend_ExistingFile(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "append.txt")
	writeTestFile(t, path, "line1\n")

	_, err := execFS(t, cfg, "fs.append", map[string]any{
		"path":    path,
		"content": "line2\n",
	})
	require.NoError(t, err)

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "line1\nline2\n", string(data))
}

func TestFSAppend_MultipleAppends(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "multi.txt")

	for i := 0; i < 3; i++ {
		_, err := execFS(t, cfg, "fs.append", map[string]any{
			"path":    path,
			"content": "x",
		})
		require.NoError(t, err)
	}

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "xxx", string(data))
}

func TestFSAppend_Validate_MissingPath(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.append", map[string]any{
		"content": "data",
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSAppend_Validate_MissingContent(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.append", map[string]any{
		"path": filepath.Join(dir, "file.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSAppend_PathDenied(t *testing.T) {
	cfg, _, denied := newFSRestrictedConfig(t)
	_, err := execFS(t, cfg, "fs.append", map[string]any{
		"path":    filepath.Join(denied, "nope.txt"),
		"content": "fail",
	})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

// --- fs.delete tests ---

func TestFSDelete_File(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "delete_me.txt")
	writeTestFile(t, path, "bye")

	result, err := execFS(t, cfg, "fs.delete", map[string]any{"path": path})
	require.NoError(t, err)
	assert.Equal(t, true, result["deleted"])
	assert.Equal(t, path, result["path"])

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestFSDelete_EmptyDir(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	sub := filepath.Join(dir, "emptydir")
	require.NoError(t, os.Mkdir(sub, 0755))

	result, err := execFS(t, cfg, "fs.delete", map[string]any{"path": sub})
	require.NoError(t, err)
	assert.Equal(t, true, result["deleted"])
}

func TestFSDelete_NonEmptyDir_NotRecursive(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	sub := filepath.Join(dir, "notempty")
	require.NoError(t, os.Mkdir(sub, 0755))
	writeTestFile(t, filepath.Join(sub, "file.txt"), "data")

	_, err := execFS(t, cfg, "fs.delete", map[string]any{"path": sub})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSDelete_Recursive(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	sub := filepath.Join(dir, "tree")
	require.NoError(t, os.MkdirAll(filepath.Join(sub, "deep"), 0755))
	writeTestFile(t, filepath.Join(sub, "a.txt"), "a")
	writeTestFile(t, filepath.Join(sub, "deep", "b.txt"), "b")

	result, err := execFS(t, cfg, "fs.delete", map[string]any{
		"path":      sub,
		"recursive": true,
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["deleted"])

	_, statErr := os.Stat(sub)
	assert.True(t, os.IsNotExist(statErr))
}

func TestFSDelete_NonExistent(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.delete", map[string]any{
		"path": filepath.Join(dir, "ghost.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSDelete_Validate_MissingPath(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.delete", map[string]any{})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSDelete_PathDenied(t *testing.T) {
	cfg, _, denied := newFSRestrictedConfig(t)
	path := filepath.Join(denied, "secret.txt")
	writeTestFile(t, path, "secret")

	_, err := execFS(t, cfg, "fs.delete", map[string]any{"path": path})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

// --- fs.list tests ---

func TestFSList_Basic(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "a")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "b")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0755))

	result, err := execFS(t, cfg, "fs.list", map[string]any{"path": dir})
	require.NoError(t, err)

	entries, ok := result["entries"].([]any)
	require.True(t, ok)
	assert.Len(t, entries, 3) // a.txt, b.txt, sub

	// Verify entry structure.
	entry := entries[0].(map[string]any)
	assert.Contains(t, entry, "name")
	assert.Contains(t, entry, "path")
	assert.Contains(t, entry, "size")
	assert.Contains(t, entry, "modified_at")
	assert.Contains(t, entry, "is_dir")
}

func TestFSList_WithPattern(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	writeTestFile(t, filepath.Join(dir, "file.txt"), "t")
	writeTestFile(t, filepath.Join(dir, "file.go"), "g")
	writeTestFile(t, filepath.Join(dir, "file.rs"), "r")

	result, err := execFS(t, cfg, "fs.list", map[string]any{
		"path":    dir,
		"pattern": "*.go",
	})
	require.NoError(t, err)

	entries, ok := result["entries"].([]any)
	require.True(t, ok)
	assert.Len(t, entries, 1)
	assert.Equal(t, "file.go", entries[0].(map[string]any)["name"])
}

func TestFSList_Recursive(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0755))
	writeTestFile(t, filepath.Join(dir, "top.txt"), "top")
	writeTestFile(t, filepath.Join(sub, "nested.txt"), "nested")

	result, err := execFS(t, cfg, "fs.list", map[string]any{
		"path":      dir,
		"recursive": true,
	})
	require.NoError(t, err)

	entries, ok := result["entries"].([]any)
	require.True(t, ok)
	assert.Len(t, entries, 3) // sub/, top.txt, sub/nested.txt
}

func TestFSList_RecursiveWithPattern(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0755))
	writeTestFile(t, filepath.Join(dir, "a.txt"), "a")
	writeTestFile(t, filepath.Join(dir, "a.go"), "a")
	writeTestFile(t, filepath.Join(sub, "b.txt"), "b")
	writeTestFile(t, filepath.Join(sub, "b.go"), "b")

	result, err := execFS(t, cfg, "fs.list", map[string]any{
		"path":      dir,
		"pattern":   "*.go",
		"recursive": true,
	})
	require.NoError(t, err)

	entries, ok := result["entries"].([]any)
	require.True(t, ok)
	assert.Len(t, entries, 2) // a.go, sub/b.go
}

func TestFSList_EmptyDir(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	empty := filepath.Join(dir, "empty")
	require.NoError(t, os.Mkdir(empty, 0755))

	result, err := execFS(t, cfg, "fs.list", map[string]any{"path": empty})
	require.NoError(t, err)

	entries, ok := result["entries"].([]any)
	require.True(t, ok)
	assert.Len(t, entries, 0)
}

func TestFSList_NoMatchPattern(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	writeTestFile(t, filepath.Join(dir, "file.txt"), "x")

	result, err := execFS(t, cfg, "fs.list", map[string]any{
		"path":    dir,
		"pattern": "*.zzz",
	})
	require.NoError(t, err)

	entries, ok := result["entries"].([]any)
	require.True(t, ok)
	assert.Len(t, entries, 0)
}

func TestFSList_NonExistentDir(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.list", map[string]any{
		"path": filepath.Join(dir, "ghost"),
	})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSList_Validate_MissingPath(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.list", map[string]any{})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSList_PathDenied(t *testing.T) {
	cfg, _, denied := newFSRestrictedConfig(t)
	_, err := execFS(t, cfg, "fs.list", map[string]any{"path": denied})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

// --- fs.stat tests ---

func TestFSStat_File(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	path := filepath.Join(dir, "stat.txt")
	writeTestFile(t, path, "twelve chars")

	result, err := execFS(t, cfg, "fs.stat", map[string]any{"path": path})
	require.NoError(t, err)
	assert.Equal(t, path, result["path"])
	assert.Equal(t, float64(12), result["size"])
	assert.Equal(t, false, result["is_dir"])
	assert.NotEmpty(t, result["modified_at"])
	assert.NotEmpty(t, result["permissions"])
}

func TestFSStat_Directory(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	sub := filepath.Join(dir, "statdir")
	require.NoError(t, os.Mkdir(sub, 0755))

	result, err := execFS(t, cfg, "fs.stat", map[string]any{"path": sub})
	require.NoError(t, err)
	assert.Equal(t, true, result["is_dir"])
}

func TestFSStat_FileNotFound(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.stat", map[string]any{
		"path": filepath.Join(dir, "nope.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSStat_Validate_MissingPath(t *testing.T) {
	cfg, _ := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.stat", map[string]any{})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSStat_PathDenied(t *testing.T) {
	cfg, _, denied := newFSRestrictedConfig(t)
	_, err := execFS(t, cfg, "fs.stat", map[string]any{"path": denied})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

// --- fs.copy tests ---

func TestFSCopy_File(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	src := filepath.Join(dir, "original.txt")
	dst := filepath.Join(dir, "copy.txt")
	writeTestFile(t, src, "copy me")

	result, err := execFS(t, cfg, "fs.copy", map[string]any{
		"src": src,
		"dst": dst,
	})
	require.NoError(t, err)
	assert.Equal(t, src, result["src"])
	assert.Equal(t, dst, result["dst"])
	assert.Equal(t, float64(7), result["size"])
	assert.Equal(t, false, result["is_dir"])

	data, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "copy me", string(data))

	// Source still exists.
	_, statErr := os.Stat(src)
	require.NoError(t, statErr)
}

func TestFSCopy_Directory(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	src := filepath.Join(dir, "srcdir")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "sub"), 0755))
	writeTestFile(t, filepath.Join(src, "a.txt"), "aaa")
	writeTestFile(t, filepath.Join(src, "sub", "b.txt"), "bb")

	dst := filepath.Join(dir, "dstdir")

	result, err := execFS(t, cfg, "fs.copy", map[string]any{
		"src": src,
		"dst": dst,
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["is_dir"])

	data, readErr := os.ReadFile(filepath.Join(dst, "a.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "aaa", string(data))

	data, readErr = os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "bb", string(data))
}

func TestFSCopy_CreateDirs(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	src := filepath.Join(dir, "source.txt")
	writeTestFile(t, src, "content")
	dst := filepath.Join(dir, "new", "nested", "copy.txt")

	result, err := execFS(t, cfg, "fs.copy", map[string]any{
		"src":         src,
		"dst":         dst,
		"create_dirs": true,
	})
	require.NoError(t, err)
	assert.Equal(t, float64(7), result["size"])

	data, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "content", string(data))
}

func TestFSCopy_SrcNotFound(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.copy", map[string]any{
		"src": filepath.Join(dir, "ghost.txt"),
		"dst": filepath.Join(dir, "copy.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSCopy_Validate_MissingSrc(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.copy", map[string]any{
		"dst": filepath.Join(dir, "copy.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSCopy_Validate_MissingDst(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.copy", map[string]any{
		"src": filepath.Join(dir, "file.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSCopy_PathDenied_Src(t *testing.T) {
	cfg, allowed, denied := newFSRestrictedConfig(t)
	src := filepath.Join(denied, "secret.txt")
	writeTestFile(t, src, "secret")

	_, err := execFS(t, cfg, "fs.copy", map[string]any{
		"src": src,
		"dst": filepath.Join(allowed, "copy.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

func TestFSCopy_PathDenied_Dst(t *testing.T) {
	cfg, allowed, denied := newFSRestrictedConfig(t)
	src := filepath.Join(allowed, "file.txt")
	writeTestFile(t, src, "data")

	_, err := execFS(t, cfg, "fs.copy", map[string]any{
		"src": src,
		"dst": filepath.Join(denied, "copy.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

// --- fs.move tests ---

func TestFSMove_File(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	src := filepath.Join(dir, "moveme.txt")
	dst := filepath.Join(dir, "moved.txt")
	writeTestFile(t, src, "moving")

	result, err := execFS(t, cfg, "fs.move", map[string]any{
		"src": src,
		"dst": dst,
	})
	require.NoError(t, err)
	assert.Equal(t, src, result["src"])
	assert.Equal(t, dst, result["dst"])
	assert.Equal(t, false, result["is_dir"])

	// Source gone.
	_, statErr := os.Stat(src)
	assert.True(t, os.IsNotExist(statErr))

	// Destination has content.
	data, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "moving", string(data))
}

func TestFSMove_Directory(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	src := filepath.Join(dir, "srcdir")
	require.NoError(t, os.Mkdir(src, 0755))
	writeTestFile(t, filepath.Join(src, "file.txt"), "data")

	dst := filepath.Join(dir, "dstdir")

	result, err := execFS(t, cfg, "fs.move", map[string]any{
		"src": src,
		"dst": dst,
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["is_dir"])

	_, statErr := os.Stat(src)
	assert.True(t, os.IsNotExist(statErr))

	data, readErr := os.ReadFile(filepath.Join(dst, "file.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, "data", string(data))
}

func TestFSMove_CreateDirs(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	src := filepath.Join(dir, "source.txt")
	writeTestFile(t, src, "content")
	dst := filepath.Join(dir, "new", "dir", "moved.txt")

	result, err := execFS(t, cfg, "fs.move", map[string]any{
		"src":         src,
		"dst":         dst,
		"create_dirs": true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result["size"])

	data, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "content", string(data))
}

func TestFSMove_SrcNotFound(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.move", map[string]any{
		"src": filepath.Join(dir, "ghost.txt"),
		"dst": filepath.Join(dir, "moved.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeExecution)
}

func TestFSMove_Validate_MissingSrc(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.move", map[string]any{
		"dst": filepath.Join(dir, "moved.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSMove_Validate_MissingDst(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_, err := execFS(t, cfg, "fs.move", map[string]any{
		"src": filepath.Join(dir, "file.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodeValidation)
}

func TestFSMove_PathDenied_Src(t *testing.T) {
	cfg, allowed, denied := newFSRestrictedConfig(t)
	src := filepath.Join(denied, "secret.txt")
	writeTestFile(t, src, "secret")

	_, err := execFS(t, cfg, "fs.move", map[string]any{
		"src": src,
		"dst": filepath.Join(allowed, "moved.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

func TestFSMove_PathDenied_Dst(t *testing.T) {
	cfg, allowed, denied := newFSRestrictedConfig(t)
	src := filepath.Join(allowed, "file.txt")
	writeTestFile(t, src, "data")

	_, err := execFS(t, cfg, "fs.move", map[string]any{
		"src": src,
		"dst": filepath.Join(denied, "moved.txt"),
	})
	requireOpcodeError(t, err, schema.ErrCodePathDenied)
}

// --- helper function tests ---

func TestIsBinary_TextData(t *testing.T) {
	assert.False(t, isBinary([]byte("Hello, World!")))
}

func TestIsBinary_BinaryData(t *testing.T) {
	assert.True(t, isBinary([]byte{0x89, 0x50, 0x4E, 0x47, 0x00}))
}

func TestIsBinary_EmptyData(t *testing.T) {
	assert.False(t, isBinary([]byte{}))
}

func TestFileInfoMap_Fields(t *testing.T) {
	cfg, dir := newFSTestConfig(t)
	_ = cfg
	path := filepath.Join(dir, "info.txt")
	writeTestFile(t, path, "test")

	info, err := os.Stat(path)
	require.NoError(t, err)

	m := fileInfoMap(path, info)
	assert.Equal(t, path, m["path"])
	assert.Equal(t, int64(4), m["size"])
	assert.Equal(t, false, m["is_dir"])
	assert.NotEmpty(t, m["modified_at"])
	assert.NotEmpty(t, m["permissions"])
}

func TestMarshalOutput_Success(t *testing.T) {
	out, err := marshalOutput(map[string]any{"key": "value"})
	require.NoError(t, err)
	assert.Contains(t, string(out.Data), `"key"`)
}
