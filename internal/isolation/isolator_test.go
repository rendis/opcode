package isolation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ValidatePath tests
// ---------------------------------------------------------------------------

func TestValidatePath_EmptyLists_Unrestricted(t *testing.T) {
	rl := ResourceLimits{}
	assert.NoError(t, rl.ValidatePath("/any/path", PathAccessRead))
	assert.NoError(t, rl.ValidatePath("/any/path", PathAccessWrite))
}

func TestValidatePath_DenyPaths_BlocksRead(t *testing.T) {
	rl := ResourceLimits{DenyPaths: []string{"/secret"}}
	err := rl.ValidatePath("/secret/file.txt", PathAccessRead)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_DenyPaths_BlocksWrite(t *testing.T) {
	rl := ResourceLimits{DenyPaths: []string{"/secret"}}
	err := rl.ValidatePath("/secret/file.txt", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_DenyPaths_ExactMatch(t *testing.T) {
	rl := ResourceLimits{DenyPaths: []string{"/secret"}}
	err := rl.ValidatePath("/secret", PathAccessRead)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_DenyPaths_TrumpsWritable(t *testing.T) {
	rl := ResourceLimits{
		WritablePaths: []string{"/data"},
		DenyPaths:     []string{"/data/private"},
	}
	// /data/public allowed
	assert.NoError(t, rl.ValidatePath("/data/public/file.txt", PathAccessWrite))
	// /data/private denied despite /data being writable
	err := rl.ValidatePath("/data/private/file.txt", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_WritablePaths_GrantsWrite(t *testing.T) {
	rl := ResourceLimits{WritablePaths: []string{"/tmp/workspace"}}
	assert.NoError(t, rl.ValidatePath("/tmp/workspace/output.txt", PathAccessWrite))
}

func TestValidatePath_WritablePaths_GrantsRead(t *testing.T) {
	// Writable implies readable.
	rl := ResourceLimits{WritablePaths: []string{"/tmp/workspace"}}
	assert.NoError(t, rl.ValidatePath("/tmp/workspace/data.txt", PathAccessRead))
}

func TestValidatePath_ReadOnlyPaths_GrantsRead(t *testing.T) {
	rl := ResourceLimits{ReadOnlyPaths: []string{"/config"}}
	assert.NoError(t, rl.ValidatePath("/config/settings.json", PathAccessRead))
}

func TestValidatePath_ReadOnlyPaths_DeniesWrite(t *testing.T) {
	rl := ResourceLimits{ReadOnlyPaths: []string{"/config"}}
	err := rl.ValidatePath("/config/settings.json", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_PathNotUnderAnyList_Denied(t *testing.T) {
	rl := ResourceLimits{
		ReadOnlyPaths: []string{"/allowed/read"},
		WritablePaths: []string{"/allowed/write"},
	}
	err := rl.ValidatePath("/other/file.txt", PathAccessRead)
	require.Error(t, err)
	assertPathDenied(t, err)

	err = rl.ValidatePath("/other/file.txt", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_PathTraversal_Caught(t *testing.T) {
	rl := ResourceLimits{WritablePaths: []string{"/allowed"}}
	// /../denied resolves to /denied after Clean
	err := rl.ValidatePath("/allowed/../denied/secret", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_PartialDirName_NotConfused(t *testing.T) {
	rl := ResourceLimits{WritablePaths: []string{"/tmp"}}
	// /tmpevil is NOT under /tmp
	err := rl.ValidatePath("/tmpevil/file.txt", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_NestedPath_Allowed(t *testing.T) {
	rl := ResourceLimits{WritablePaths: []string{"/data"}}
	assert.NoError(t, rl.ValidatePath("/data/a/b/c/deep.txt", PathAccessWrite))
}

func TestValidatePath_OnlyReadOnly_WriteDenied(t *testing.T) {
	// ReadOnlyPaths set, no WritablePaths — all writes denied.
	rl := ResourceLimits{ReadOnlyPaths: []string{"/data"}}
	err := rl.ValidatePath("/data/file.txt", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_InvalidDenyPath_FailsClosed(t *testing.T) {
	// An invalid deny path should cause access to be denied (fail-closed).
	rl := ResourceLimits{DenyPaths: []string{string([]byte{0x00})}} // null byte = invalid path
	err := rl.ValidatePath("/any/path", PathAccessRead)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_SymlinkedParent(t *testing.T) {
	// Create a temp dir with a symlink to test symlink resolution.
	tmp := t.TempDir()
	real := filepath.Join(tmp, "real")
	require.NoError(t, os.MkdirAll(real, 0o755))
	link := filepath.Join(tmp, "link")
	require.NoError(t, os.Symlink(real, link))

	rl := ResourceLimits{WritablePaths: []string{real}}
	// Access via symlink should resolve to real and be allowed.
	assert.NoError(t, rl.ValidatePath(filepath.Join(link, "file.txt"), PathAccessWrite))
}

func TestValidatePath_EmptyPath_Error(t *testing.T) {
	// Empty string resolves to CWD via filepath.Abs — must be denied when lists are configured.
	rl := ResourceLimits{WritablePaths: []string{"/allowed"}}
	err := rl.ValidatePath("", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_RootDeny_BlocksEverything(t *testing.T) {
	rl := ResourceLimits{DenyPaths: []string{"/"}}
	err := rl.ValidatePath("/any/path/at/all", PathAccessRead)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_RootWritable_AllowsAll(t *testing.T) {
	rl := ResourceLimits{WritablePaths: []string{"/"}}
	assert.NoError(t, rl.ValidatePath("/any/path", PathAccessWrite))
	assert.NoError(t, rl.ValidatePath("/any/path", PathAccessRead))
}

func TestValidatePath_RelativePath_ResolvesToCwd(t *testing.T) {
	// Relative paths resolve via filepath.Abs (CWD-dependent).
	// With WritablePaths set, the resolved absolute path must be under an allowed path.
	cwd, err := os.Getwd()
	require.NoError(t, err)
	rl := ResourceLimits{WritablePaths: []string{cwd}}
	assert.NoError(t, rl.ValidatePath("relative/file.txt", PathAccessWrite))
}

func TestValidatePath_UnicodeAndSpaces(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "path with spaces", "日本語")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	rl := ResourceLimits{WritablePaths: []string{tmp}}
	assert.NoError(t, rl.ValidatePath(filepath.Join(dir, "file.txt"), PathAccessWrite))
}

func TestValidatePath_MultipleDenyRules(t *testing.T) {
	rl := ResourceLimits{
		WritablePaths: []string{"/data"},
		DenyPaths:     []string{"/data/secret", "/data/private"},
	}
	assert.NoError(t, rl.ValidatePath("/data/public/file.txt", PathAccessWrite))
	err := rl.ValidatePath("/data/secret/file.txt", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
	err = rl.ValidatePath("/data/private/file.txt", PathAccessWrite)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_NullByteMiddle(t *testing.T) {
	rl := ResourceLimits{}
	err := rl.ValidatePath("/path/with\x00null/file.txt", PathAccessRead)
	require.Error(t, err)
	assertPathDenied(t, err)
}

func TestValidatePath_DotPaths(t *testing.T) {
	// "." resolves to CWD, ".." resolves to parent of CWD.
	cwd, err := os.Getwd()
	require.NoError(t, err)

	rl := ResourceLimits{ReadOnlyPaths: []string{cwd}}
	assert.NoError(t, rl.ValidatePath(".", PathAccessRead))

	// ".." resolves to parent — should be denied if parent isn't in allow list.
	parent := filepath.Dir(cwd)
	if parent != cwd { // not at root
		rl2 := ResourceLimits{ReadOnlyPaths: []string{cwd}}
		err = rl2.ValidatePath("..", PathAccessRead)
		require.Error(t, err)
		assertPathDenied(t, err)
	}
}

func TestValidatePath_ConcurrentAccess(t *testing.T) {
	tmp := t.TempDir()
	rl := ResourceLimits{
		WritablePaths: []string{tmp},
		DenyPaths:     []string{filepath.Join(tmp, "denied")},
	}

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := filepath.Join(tmp, fmt.Sprintf("file_%d.txt", n))
			assert.NoError(t, rl.ValidatePath(path, PathAccessWrite))

			denied := filepath.Join(tmp, "denied", fmt.Sprintf("file_%d.txt", n))
			err := rl.ValidatePath(denied, PathAccessWrite)
			assert.Error(t, err)
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// isUnderPath tests
// ---------------------------------------------------------------------------

func TestIsUnderPath_ExactMatch(t *testing.T) {
	assert.True(t, isUnderPath("/tmp", "/tmp"))
}

func TestIsUnderPath_Child(t *testing.T) {
	assert.True(t, isUnderPath("/tmp/foo/bar", "/tmp"))
}

func TestIsUnderPath_NotChild(t *testing.T) {
	assert.False(t, isUnderPath("/var/log", "/tmp"))
}

func TestIsUnderPath_PartialName(t *testing.T) {
	assert.False(t, isUnderPath("/tmpevil", "/tmp"))
}

func TestIsUnderPath_BothEmpty(t *testing.T) {
	assert.True(t, isUnderPath("", ""))
}

func TestIsUnderPath_EmptyBase(t *testing.T) {
	// filepath.Rel("", "/foo") behavior — should not match.
	result := isUnderPath("/foo", "")
	_ = result // Just verify no panic.
}

func TestIsUnderPath_Root(t *testing.T) {
	assert.True(t, isUnderPath("/etc/passwd", "/"))
	assert.True(t, isUnderPath("/", "/"))
}

// ---------------------------------------------------------------------------
// resolveAncestor tests
// ---------------------------------------------------------------------------

func TestResolveAncestor_NonExistentDeepPath(t *testing.T) {
	// Should resolve without infinite loop even for deeply non-existent path.
	result := resolveAncestor("/nonexistent/deep/path/file.txt")
	assert.NotEmpty(t, result)
}

func TestResolveAncestor_RootPath(t *testing.T) {
	result := resolveAncestor("/")
	assert.Equal(t, "/", result)
}

// ---------------------------------------------------------------------------
// FallbackIsolator tests
// ---------------------------------------------------------------------------

func TestFallbackIsolator_Capabilities_AllFalse(t *testing.T) {
	iso := NewFallbackIsolator()
	caps := iso.Capabilities()
	assert.False(t, caps.CanLimitMemory)
	assert.False(t, caps.CanLimitCPU)
	assert.False(t, caps.CanLimitNetwork)
	assert.False(t, caps.CanIsolateFS)
	assert.False(t, caps.CanIsolatePID)
}

func TestFallbackIsolator_Wrap_PreservesFields(t *testing.T) {
	iso := NewFallbackIsolator()
	original := exec.Command("echo", "hello")
	original.Dir = "/tmp"
	original.Env = []string{"FOO=bar"}
	var buf bytes.Buffer
	original.Stdout = &buf

	wrapped, cleanup, err := iso.Wrap(context.Background(), original, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	assert.Equal(t, original.Path, wrapped.Path)
	assert.Equal(t, original.Args, wrapped.Args)
	assert.Equal(t, "/tmp", wrapped.Dir)
	assert.Equal(t, []string{"FOO=bar"}, wrapped.Env)
	assert.Equal(t, &buf, wrapped.Stdout)
}

func TestFallbackIsolator_Wrap_ZeroTimeout_UsesParentCtx(t *testing.T) {
	iso := NewFallbackIsolator()
	cmd := exec.Command("echo", "hello")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	// Should be runnable.
	require.NoError(t, wrapped.Run())
}

func TestFallbackIsolator_Wrap_CancelledCtx_ReturnsError(t *testing.T) {
	iso := NewFallbackIsolator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := exec.Command("echo", "hello")
	_, _, err := iso.Wrap(ctx, cmd, ResourceLimits{})
	require.Error(t, err)
}

func TestFallbackIsolator_Wrap_Timeout_KillsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep command not available on windows")
	}

	iso := NewFallbackIsolator()
	cmd := exec.Command("sleep", "60")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		Timeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer cleanup()

	start := time.Now()
	err = wrapped.Run()
	elapsed := time.Since(start)

	// Process should have been killed by timeout.
	require.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second, "process should be killed well before 2s")
}

func TestFallbackIsolator_Cleanup_Idempotent(t *testing.T) {
	iso := NewFallbackIsolator()
	cmd := exec.Command("echo", "hello")

	_, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		Timeout: time.Second,
	})
	require.NoError(t, err)

	// Calling cleanup multiple times should not panic.
	cleanup()
	cleanup()
}

func TestFallbackIsolator_Wrap_CapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("echo behavior differs on windows")
	}

	iso := NewFallbackIsolator()
	cmd := exec.Command("echo", "hello world")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	assert.Equal(t, "hello world\n", stdout.String())
}

func TestFallbackIsolator_Wrap_NegativeTimeout_NoEnforcement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("echo behavior differs on windows")
	}

	iso := NewFallbackIsolator()
	cmd := exec.Command("echo", "hello")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Negative timeout should be treated as no timeout (< 0 fails the > 0 check).
	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		Timeout: -1 * time.Second,
	})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	assert.Equal(t, "hello\n", stdout.String())
}

func TestFallbackIsolator_Wrap_StderrCaptured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell redirect differs on windows")
	}

	iso := NewFallbackIsolator()
	cmd := exec.Command("sh", "-c", "echo error >&2")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	assert.Equal(t, "error\n", stderr.String())
}

// ---------------------------------------------------------------------------
// Factory tests
// ---------------------------------------------------------------------------

func TestNewIsolator_ReturnsNonNil(t *testing.T) {
	iso, err := NewIsolator()
	require.NoError(t, err)
	require.NotNil(t, iso)
}

func TestNewIsolator_ReturnsFallbackOnNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test verifies non-Linux factory path")
	}
	iso, err := NewIsolator()
	require.NoError(t, err)
	_, ok := iso.(*FallbackIsolator)
	assert.True(t, ok, "expected *FallbackIsolator on %s", runtime.GOOS)
}

func TestNewIsolator_SatisfiesInterface(t *testing.T) {
	iso, err := NewIsolator()
	require.NoError(t, err)
	// Verify interface is satisfied at runtime.
	var _ Isolator = iso
}

func TestNewIsolator_NonLinux_LogsWarning(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("this test verifies non-Linux factory warning")
	}

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	orig := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(orig)

	_, err := NewIsolator()
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "fallback")
	assert.Contains(t, buf.String(), "WARN")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertPathDenied(t *testing.T, err error) {
	t.Helper()
	var opcodeErr *schema.OpcodeError
	require.ErrorAs(t, err, &opcodeErr)
	assert.Equal(t, schema.ErrCodePathDenied, opcodeErr.Code)
	assert.False(t, opcodeErr.IsRetryable(), "PATH_DENIED should not be retryable")
}
