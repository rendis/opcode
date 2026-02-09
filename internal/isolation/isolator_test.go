package isolation

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
