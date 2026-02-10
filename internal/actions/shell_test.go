package actions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rendis/opcode/internal/isolation"
	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newShellTestConfig(t *testing.T) ShellConfig {
	t.Helper()
	return ShellConfig{
		Isolator:       &isolation.FallbackIsolator{},
		DefaultTimeout: 10 * time.Second,
		MaxOutputSize:  1024 * 1024,
	}
}

func findShellAction(t *testing.T, cfg ShellConfig) Action {
	t.Helper()
	actions := ShellActions(cfg)
	for _, a := range actions {
		if a.Name() == "shell.exec" {
			return a
		}
	}
	t.Fatal("shell.exec action not found")
	return nil
}

func execShell(t *testing.T, cfg ShellConfig, params map[string]any) (map[string]any, error) {
	t.Helper()
	a := findShellAction(t, cfg)
	out, err := a.Execute(context.Background(), ActionInput{Params: params})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Data, &result))
	return result, nil
}

func execShellCtx(t *testing.T, ctx context.Context, cfg ShellConfig, params map[string]any) (map[string]any, error) {
	t.Helper()
	a := findShellAction(t, cfg)
	out, err := a.Execute(ctx, ActionInput{Params: params})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Data, &result))
	return result, nil
}

// --- Tests ---

func TestShellExec_Name(t *testing.T) {
	cfg := newShellTestConfig(t)
	a := findShellAction(t, cfg)
	assert.Equal(t, "shell.exec", a.Name())
}

func TestShellExec_Schema(t *testing.T) {
	cfg := newShellTestConfig(t)
	a := findShellAction(t, cfg)
	s := a.Schema()
	assert.NotEmpty(t, s.Description)
	assert.NotEmpty(t, s.InputSchema)
	assert.NotEmpty(t, s.OutputSchema)

	// Verify input schema is valid JSON.
	var inputSchema map[string]any
	require.NoError(t, json.Unmarshal(s.InputSchema, &inputSchema))
	assert.Equal(t, "object", inputSchema["type"])

	// Verify output schema is valid JSON.
	var outputSchema map[string]any
	require.NoError(t, json.Unmarshal(s.OutputSchema, &outputSchema))
	assert.Equal(t, "object", outputSchema["type"])
}

func TestShellExec_Validate_MissingCommand(t *testing.T) {
	cfg := newShellTestConfig(t)
	a := findShellAction(t, cfg)

	err := a.Validate(map[string]any{})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestShellExec_Validate_EmptyCommand(t *testing.T) {
	cfg := newShellTestConfig(t)
	a := findShellAction(t, cfg)

	err := a.Validate(map[string]any{"command": ""})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestShellExec_Validate_ValidCommand(t *testing.T) {
	cfg := newShellTestConfig(t)
	a := findShellAction(t, cfg)

	err := a.Validate(map[string]any{"command": "echo"})
	require.NoError(t, err)
}

func TestShellExec_Echo(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", result["stdout"])
	assert.Equal(t, "", result["stderr"])
	assert.Equal(t, float64(0), result["exit_code"])
	assert.Equal(t, false, result["killed"])
}

func TestShellExec_WithArgs(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "echo",
		"args":    []any{"hello", "world"},
	})
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", result["stdout"])
}

func TestShellExec_ExitCode(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "/bin/sh",
		"args":    []any{"-c", "exit 42"},
	})
	require.NoError(t, err)
	assert.Equal(t, float64(42), result["exit_code"])
}

func TestShellExec_Stderr(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "/bin/sh",
		"args":    []any{"-c", "echo error_output >&2"},
	})
	require.NoError(t, err)
	assert.Equal(t, "error_output\n", result["stderr"])
	assert.Equal(t, "", result["stdout"])
}

func TestShellExec_Stdin(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "cat",
		"stdin":   "hello from stdin",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello from stdin", result["stdout"])
}

func TestShellExec_EnvVars(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "printenv",
		"args":    []any{"OPCODE_TEST_VAR"},
		"env":     map[string]any{"OPCODE_TEST_VAR": "test_value_123"},
	})
	require.NoError(t, err)
	assert.Equal(t, "test_value_123\n", result["stdout"])
	assert.Equal(t, float64(0), result["exit_code"])
}

func TestShellExec_CWD(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newShellTestConfig(t)

	result, err := execShell(t, cfg, map[string]any{
		"command": "pwd",
		"cwd":    tmpDir,
	})
	require.NoError(t, err)

	// Resolve both to handle macOS /tmp → /private/tmp symlink.
	expectedDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	actualDir := strings.TrimSpace(result["stdout"].(string))
	resolvedActual, err := filepath.EvalSymlinks(actualDir)
	require.NoError(t, err)

	assert.Equal(t, expectedDir, resolvedActual)
}

func TestShellExec_CWD_PathDenied(t *testing.T) {
	cfg := newShellTestConfig(t)
	cfg.DefaultLimits = isolation.ResourceLimits{
		DenyPaths: []string{"/etc"},
	}

	_, err := execShell(t, cfg, map[string]any{
		"command": "pwd",
		"cwd":    "/etc",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodePathDenied, opErr.Code)
}

func TestShellExec_ShellMode(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "echo $((1+2))",
		"shell":   true,
	})
	require.NoError(t, err)
	// "3\n" is valid JSON (number), so stdout is auto-parsed to float64.
	assert.Equal(t, float64(3), result["stdout"])
	assert.Equal(t, "3\n", result["stdout_raw"])
}

func TestShellExec_ShellMode_WithArgs(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "echo",
		"args":    []any{"hello", "world"},
		"shell":   true,
	})
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", result["stdout"])
}

func TestShellExec_Timeout(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "sleep",
		"args":    []any{"60"},
		"timeout": "100ms",
	})
	// sleep gets killed, so we get a non-zero exit or killed status.
	// The command should not return an execution error — it should return output with killed=true.
	require.NoError(t, err)
	assert.Equal(t, true, result["killed"])
	assert.NotEqual(t, float64(0), result["exit_code"])
}

func TestShellExec_CommandNotFound(t *testing.T) {
	cfg := newShellTestConfig(t)
	_, err := execShell(t, cfg, map[string]any{
		"command": "nonexistent_binary_xyz_opcode_test",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeExecution, opErr.Code)
}

func TestShellExec_DurationMs(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "/bin/sh",
		"args":    []any{"-c", "sleep 0.05"},
	})
	require.NoError(t, err)

	durationMs, ok := result["duration_ms"].(float64)
	require.True(t, ok, "duration_ms should be a number")
	assert.GreaterOrEqual(t, durationMs, float64(0))
}

func TestShellExec_MaxOutputSize(t *testing.T) {
	cfg := newShellTestConfig(t)
	cfg.MaxOutputSize = 64 // Very small limit.

	// Generate output larger than limit.
	result, err := execShell(t, cfg, map[string]any{
		"command": "/bin/sh",
		"args":    []any{"-c", "dd if=/dev/zero bs=1024 count=1 2>/dev/null | tr '\\0' 'A'"},
	})
	require.NoError(t, err)

	stdout := result["stdout"].(string)
	assert.LessOrEqual(t, int64(len(stdout)), cfg.MaxOutputSize)
	assert.Equal(t, float64(0), result["exit_code"])
}

func TestShellExec_ContextCancelled(t *testing.T) {
	cfg := newShellTestConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := execShellCtx(t, ctx, cfg, map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	})
	// Cancelled context should cause an error from the isolator wrap or command run.
	require.Error(t, err)
}

func TestShellExec_DefaultTimeout(t *testing.T) {
	cfg := ShellConfig{
		Isolator: &isolation.FallbackIsolator{},
		// DefaultTimeout not set — should fall back to defaultShellTimeout.
	}
	actions := ShellActions(cfg)
	require.Len(t, actions, 1)

	// Verify that the action was created (defaults applied internally).
	assert.Equal(t, "shell.exec", actions[0].Name())
}

func TestShellExec_DefaultIsolator(t *testing.T) {
	cfg := ShellConfig{
		// Isolator not set — should use FallbackIsolator.
	}
	actions := ShellActions(cfg)
	require.Len(t, actions, 1)

	// Verify it works (FallbackIsolator was applied).
	a := actions[0]
	result, err := a.Execute(context.Background(), ActionInput{
		Params: map[string]any{"command": "echo", "args": []any{"ok"}},
	})
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(result.Data, &out))
	assert.Equal(t, "ok\n", out["stdout"])
}

func TestShellExec_NilParams(t *testing.T) {
	cfg := newShellTestConfig(t)
	a := findShellAction(t, cfg)

	_, err := a.Execute(context.Background(), ActionInput{Params: nil})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestShellExec_StdoutAndStderr(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "/bin/sh",
		"args":    []any{"-c", "echo out_msg && echo err_msg >&2"},
	})
	require.NoError(t, err)
	assert.Equal(t, "out_msg\n", result["stdout"])
	assert.Equal(t, "err_msg\n", result["stderr"])
	assert.Equal(t, float64(0), result["exit_code"])
}

func TestShellExec_CWD_ReadOnlyPaths(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := newShellTestConfig(t)
	cfg.DefaultLimits = isolation.ResourceLimits{
		ReadOnlyPaths: []string{tmpDir},
	}

	result, err := execShell(t, cfg, map[string]any{
		"command": "pwd",
		"cwd":    tmpDir,
	})
	require.NoError(t, err)

	expectedDir, _ := filepath.EvalSymlinks(tmpDir)
	actualDir := strings.TrimSpace(result["stdout"].(string))
	resolvedActual, _ := filepath.EvalSymlinks(actualDir)
	assert.Equal(t, expectedDir, resolvedActual)
}

func TestShellExec_CWD_NotInAllowedPaths(t *testing.T) {
	cfg := newShellTestConfig(t)
	cfg.DefaultLimits = isolation.ResourceLimits{
		ReadOnlyPaths: []string{"/nonexistent_allowed_path_opcode"},
	}

	_, err := execShell(t, cfg, map[string]any{
		"command": "pwd",
		"cwd":    os.TempDir(),
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodePathDenied, opErr.Code)
}

// --- limitedWriter tests ---

func TestLimitedWriter_UnderLimit(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 100}

	n, err := lw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", buf.String())
	assert.Equal(t, int64(5), lw.written)
}

func TestLimitedWriter_ExactLimit(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 5}

	n, err := lw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", buf.String())
}

func TestLimitedWriter_OverLimit(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 3}

	// First write: only 3 bytes should be written to buf.
	n, err := lw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n) // Reports full len consumed.
	assert.Equal(t, "hel", buf.String())

	// Second write: all discarded.
	n, err = lw.Write([]byte("world"))
	require.NoError(t, err)
	assert.Equal(t, 5, n) // Reports full len consumed.
	assert.Equal(t, "hel", buf.String())
}

func TestLimitedWriter_MultipleWrites(t *testing.T) {
	var buf strings.Builder
	lw := &limitedWriter{w: &buf, limit: 8}

	n, err := lw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	n, err = lw.Write([]byte("world"))
	require.NoError(t, err)
	assert.Equal(t, 5, n) // Reports full len even though only 3 written.
	assert.Equal(t, "hellowor", buf.String())
}

// --- Auto-parse JSON stdout tests ---

func TestShellExec_JSONStdout(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "echo",
		"args":    []any{`{"name":"alice","age":30}`},
	})
	require.NoError(t, err)

	// stdout should be auto-parsed into a map.
	stdout, ok := result["stdout"].(map[string]any)
	require.True(t, ok, "stdout should be parsed map, got %T", result["stdout"])
	assert.Equal(t, "alice", stdout["name"])
	assert.Equal(t, float64(30), stdout["age"])
}

func TestShellExec_PlainStdout(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "echo",
		"args":    []any{"hello world"},
	})
	require.NoError(t, err)

	// Non-JSON stdout stays as string.
	stdout, ok := result["stdout"].(string)
	require.True(t, ok, "stdout should be string, got %T", result["stdout"])
	assert.Equal(t, "hello world\n", stdout)
}

func TestShellExec_StdoutRaw(t *testing.T) {
	cfg := newShellTestConfig(t)
	result, err := execShell(t, cfg, map[string]any{
		"command": "echo",
		"args":    []any{`{"x":1}`},
	})
	require.NoError(t, err)

	// stdout_raw is always the raw string, even when stdout is parsed.
	raw, ok := result["stdout_raw"].(string)
	require.True(t, ok, "stdout_raw should be string, got %T", result["stdout_raw"])
	assert.Equal(t, "{\"x\":1}\n", raw)

	// stdout should be parsed.
	parsed, ok := result["stdout"].(map[string]any)
	require.True(t, ok, "stdout should be parsed map")
	assert.Equal(t, float64(1), parsed["x"])
}
