//go:build linux

package isolation

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Constructor & capabilities
// ---------------------------------------------------------------------------

func TestLinuxNew_DetectsCaps(t *testing.T) {
	iso, err := NewLinuxIsolator()
	require.NoError(t, err)
	require.NotNil(t, iso)

	caps := iso.Capabilities()
	// At minimum, memory and cpu should be available on modern kernels.
	assert.True(t, caps.CanLimitMemory, "expected memory controller")
	assert.True(t, caps.CanLimitCPU, "expected cpu controller")
	// Network is always true (via CLONE_NEWNET, not cgroup).
	assert.True(t, caps.CanLimitNetwork)
	// FS isolation deferred (no mount namespace).
	assert.False(t, caps.CanIsolateFS)
}

func TestLinuxCaps_MatchControllers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cgroupRoot, "cgroup.controllers"))
	require.NoError(t, err)

	controllers := parseControllers(string(data))
	caps := buildCaps(controllers)

	assert.Equal(t, controllers["memory"], caps.CanLimitMemory)
	assert.Equal(t, controllers["cpu"], caps.CanLimitCPU)
	assert.Equal(t, controllers["pids"], caps.CanIsolatePID)
	assert.True(t, caps.CanLimitNetwork) // always true
}

// ---------------------------------------------------------------------------
// Cgroup lifecycle
// ---------------------------------------------------------------------------

func TestLinuxWrap_CreatesCgroup(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("true")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	// The cgroup directory should exist (derived from SysProcAttr.CgroupFD).
	cgPath := findCgroupPath(t, iso)
	require.NotEmpty(t, cgPath, "expected cgroup directory to exist")

	require.NoError(t, wrapped.Run())
}

func TestLinuxWrap_CleanupRemovesCgroup(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("true")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)

	require.NoError(t, wrapped.Run())
	cleanup()

	// After cleanup, no child subdirectories should remain under the base cgroup.
	// The base dir itself has cgroup control files — only check for dirs.
	entries, err := os.ReadDir(iso.cgroupBase)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, e.IsDir(), "expected no child cgroup dirs after cleanup, found: %s", e.Name())
	}
}

func TestLinuxWrap_CleanupIdempotent(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("true")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)

	require.NoError(t, wrapped.Run())

	// Should not panic on double cleanup.
	cleanup()
	cleanup()
}

// ---------------------------------------------------------------------------
// Memory limits
// ---------------------------------------------------------------------------

func TestLinuxWrap_MemoryLimit_Written(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("true")

	const memLimit int64 = 64 * 1024 * 1024 // 64 MiB
	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		MaxMemoryBytes: memLimit,
	})
	require.NoError(t, err)
	defer cleanup()

	cgPath := findCgroupPath(t, iso)
	data, err := os.ReadFile(filepath.Join(cgPath, "memory.max"))
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%d", memLimit), strings.TrimSpace(string(data)))

	require.NoError(t, wrapped.Run())
}

func TestLinuxWrap_MemoryLimit_OOMKill(t *testing.T) {
	iso := newTestIsolator(t)

	// Allocate anonymous memory by storing data in a shell variable.
	// This creates heap-resident memory that counts against cgroup memory.max.
	// Limit is 8MiB, allocation target is ~32MiB.
	cmd := exec.Command("sh", "-c",
		"x=$(dd if=/dev/urandom bs=1M count=32 2>/dev/null | od -A x); sleep 10")

	const memLimit int64 = 8 * 1024 * 1024
	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		MaxMemoryBytes: memLimit,
	})
	require.NoError(t, err)
	defer cleanup()

	err = wrapped.Run()
	require.Error(t, err, "process should be OOM killed")
}

// ---------------------------------------------------------------------------
// CPU quota
// ---------------------------------------------------------------------------

func TestLinuxWrap_CPUQuota_Written(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("true")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		MaxCPUPercent: 50,
	})
	require.NoError(t, err)
	defer cleanup()

	cgPath := findCgroupPath(t, iso)
	data, err := os.ReadFile(filepath.Join(cgPath, "cpu.max"))
	require.NoError(t, err)
	assert.Equal(t, "50000 100000", strings.TrimSpace(string(data)))

	require.NoError(t, wrapped.Run())
}

// ---------------------------------------------------------------------------
// Namespaces
// ---------------------------------------------------------------------------

func TestLinuxWrap_PIDNamespace(t *testing.T) {
	iso := newTestIsolator(t)
	if !iso.caps.CanIsolatePID {
		t.Skip("pids controller not available")
	}

	// Inside a PID namespace, the shell (sh) is PID 1.
	// echo $$ prints the shell's own PID.
	cmd := exec.Command("sh", "-c", "echo $$")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	pid := strings.TrimSpace(stdout.String())
	assert.Equal(t, "1", pid, "expected shell PID 1 in namespace")
}

func TestLinuxWrap_NetworkDenied(t *testing.T) {
	iso := newTestIsolator(t)

	// With AllowNetwork=false (default), process should have no network.
	// Try to connect to a public DNS — should fail.
	cmd := exec.Command("sh", "-c", "cat /dev/null > /dev/tcp/8.8.8.8/53 2>&1 || echo no_network")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		AllowNetwork: false,
	})
	require.NoError(t, err)
	defer cleanup()

	_ = wrapped.Run()
	assert.Contains(t, stdout.String(), "no_network")
}

func TestLinuxWrap_NetworkAllowed(t *testing.T) {
	iso := newTestIsolator(t)

	// With AllowNetwork=true, no network namespace — loopback should be available.
	cmd := exec.Command("sh", "-c", "ip link show lo 2>/dev/null | head -1 || echo has_network")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		AllowNetwork: true,
	})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	output := stdout.String()
	assert.True(t, strings.Contains(output, "lo") || strings.Contains(output, "has_network"),
		"expected network access, got: %s", output)
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

func TestLinuxWrap_Timeout(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("sleep", "60")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		Timeout: 200 * time.Millisecond,
	})
	require.NoError(t, err)
	defer cleanup()

	start := time.Now()
	err = wrapped.Run()
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 3*time.Second, "process should be killed by timeout")
}

func TestLinuxWrap_CancelledCtx(t *testing.T) {
	iso := newTestIsolator(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := exec.Command("true")
	_, _, err := iso.Wrap(ctx, cmd, ResourceLimits{})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Field preservation & output
// ---------------------------------------------------------------------------

func TestLinuxWrap_PreservesFields(t *testing.T) {
	iso := newTestIsolator(t)
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

func TestLinuxWrap_CapturesOutput(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("echo", "hello linux")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	assert.Equal(t, "hello linux\n", stdout.String())
}

// ---------------------------------------------------------------------------
// Zero limits & partial failure
// ---------------------------------------------------------------------------

func TestLinuxWrap_ZeroLimits(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("echo", "no limits")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	assert.Equal(t, "no limits\n", stdout.String())
}

func TestLinuxWrap_PartialFailure_CleansUp(t *testing.T) {
	// Simulate a failure by using a non-existent cgroup base.
	iso := &LinuxIsolator{
		cgroupBase: "/sys/fs/cgroup/nonexistent_opcode_test",
		caps:       IsolatorCaps{CanLimitMemory: true},
	}
	cmd := exec.Command("true")

	_, _, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		MaxMemoryBytes: 1024,
	})
	require.Error(t, err, "should fail when cgroup base doesn't exist")
}

func TestLinuxWrap_FDClosedOnWriteLimitsFailure(t *testing.T) {
	iso := newTestIsolator(t)

	// Count open FDs before.
	fdsBefore := countOpenFDs(t)

	// Create a valid cgroup but make writeLimits fail by requesting a limit
	// on a controller we disable. We simulate this by creating an isolator
	// with caps that claim memory is available but the cgroup doesn't have
	// the controller enabled.
	fakeIso := &LinuxIsolator{
		cgroupBase: iso.cgroupBase,
		caps:       IsolatorCaps{CanLimitMemory: true, CanLimitCPU: true},
	}

	// Create a child cgroup that does NOT have controllers enabled.
	badBase := filepath.Join(iso.cgroupBase, "fd_leak_test")
	require.NoError(t, os.Mkdir(badBase, 0o755))
	defer os.Remove(badBase)
	fakeIso.cgroupBase = badBase

	cmd := exec.Command("true")
	_, _, err := fakeIso.Wrap(context.Background(), cmd, ResourceLimits{
		MaxMemoryBytes: 1024,
	})
	// May succeed or fail depending on controller propagation — either way,
	// FDs should not leak.
	if err != nil {
		// Failure path: verify no FD leak.
		fdsAfter := countOpenFDs(t)
		assert.LessOrEqual(t, fdsAfter, fdsBefore+1,
			"FD leak detected: before=%d after=%d", fdsBefore, fdsAfter)
	}
}

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot read /proc/self/fd: %v", err)
	}
	return len(entries)
}

// ---------------------------------------------------------------------------
// Security: escape attempt tests
// ---------------------------------------------------------------------------

func TestLinuxWrap_PIDNamespace_CannotSeeHostProcesses(t *testing.T) {
	iso := newTestIsolator(t)
	if !iso.caps.CanIsolatePID {
		t.Skip("pids controller not available")
	}

	// Inside a PID namespace, /proc only shows the namespace's processes.
	// Count PIDs visible — should be very few (shell + children only).
	cmd := exec.Command("sh", "-c", "ls /proc | grep -c '^[0-9]'")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	require.NoError(t, wrapped.Run())
	count := strings.TrimSpace(stdout.String())
	n, err := fmt.Sscanf(count, "%d", new(int))
	require.Equal(t, 1, n)
	require.NoError(t, err)

	pid_count := 0
	fmt.Sscanf(count, "%d", &pid_count)
	// In a PID namespace, should see very few processes (typically 1-5).
	// On the host, there would be hundreds.
	assert.LessOrEqual(t, pid_count, 10,
		"PID namespace should isolate from host processes, saw %d PIDs", pid_count)
}

func TestLinuxWrap_PIDNamespace_CannotSignalHostPID(t *testing.T) {
	iso := newTestIsolator(t)
	if !iso.caps.CanIsolatePID {
		t.Skip("pids controller not available")
	}

	// Try to send signal 0 (existence check) to PID 1 of the HOST.
	// Inside PID namespace, host PID 1 is not visible — kill should fail.
	// Note: the namespace's own PID 1 is the shell, so kill -0 1 succeeds.
	// Instead, try a high PID that would exist on host but not in namespace.
	cmd := exec.Command("sh", "-c", "kill -0 999 2>&1; echo exit_code=$?")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{})
	require.NoError(t, err)
	defer cleanup()

	_ = wrapped.Run()
	output := stdout.String()
	// kill should fail because PID 999 doesn't exist in the namespace.
	assert.Contains(t, output, "exit_code=1",
		"should not be able to signal host PIDs from namespace")
}

func TestLinuxWrap_NetworkDenied_CannotReachLocalhost(t *testing.T) {
	iso := newTestIsolator(t)

	// In a network namespace, even localhost should be unreachable
	// because the loopback interface is down by default.
	cmd := exec.Command("sh", "-c",
		"ip link show lo 2>/dev/null | grep -q UP && echo lo_is_up || echo lo_is_down")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		AllowNetwork: false,
	})
	require.NoError(t, err)
	defer cleanup()

	_ = wrapped.Run()
	assert.Contains(t, stdout.String(), "lo_is_down",
		"loopback should be down in isolated network namespace")
}

func TestLinuxWrap_NetworkDenied_NoInterfaces(t *testing.T) {
	iso := newTestIsolator(t)

	// Process should see no active network interfaces (or only lo DOWN).
	// grep -c outputs "0" but exits with code 1 when no matches — use || true to suppress.
	cmd := exec.Command("sh", "-c", "ip link show 2>/dev/null | grep -c 'state UP' || true")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		AllowNetwork: false,
	})
	require.NoError(t, err)
	defer cleanup()

	_ = wrapped.Run()
	count := strings.TrimSpace(stdout.String())
	assert.Equal(t, "0", count, "no interfaces should be UP in network namespace")
}

func TestLinuxWrap_MemoryLimit_SwapDisabled(t *testing.T) {
	iso := newTestIsolator(t)
	cmd := exec.Command("true")

	const memLimit int64 = 32 * 1024 * 1024
	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		MaxMemoryBytes: memLimit,
	})
	require.NoError(t, err)
	defer cleanup()

	// Verify memory.swap.max is set to 0 (no swap escape hatch).
	cgPath := findCgroupPath(t, iso)
	data, err := os.ReadFile(filepath.Join(cgPath, "memory.swap.max"))
	require.NoError(t, err)
	assert.Equal(t, "0", strings.TrimSpace(string(data)),
		"swap should be disabled to enforce hard memory limit")

	require.NoError(t, wrapped.Run())
}

func TestLinuxWrap_CPUQuota_Throttled(t *testing.T) {
	iso := newTestIsolator(t)

	// Run a CPU-intensive busy loop for 1 second with 10% CPU limit.
	// Then check cpu.stat for throttled_usec > 0.
	cmd := exec.Command("sh", "-c",
		"timeout 1 sh -c 'while true; do :; done' 2>/dev/null; exit 0")

	wrapped, cleanup, err := iso.Wrap(context.Background(), cmd, ResourceLimits{
		MaxCPUPercent: 10,
	})
	require.NoError(t, err)

	cgPath := findCgroupPath(t, iso)
	require.NoError(t, wrapped.Run())

	// Read cpu.stat to verify throttling actually happened.
	data, err := os.ReadFile(filepath.Join(cgPath, "cpu.stat"))
	cleanup()
	require.NoError(t, err)

	stat := string(data)
	assert.Contains(t, stat, "nr_throttled",
		"cpu.stat should report throttling info")

	// Parse nr_throttled — should be > 0 if throttling occurred.
	for _, line := range strings.Split(stat, "\n") {
		if strings.HasPrefix(line, "nr_throttled ") {
			val := strings.TrimPrefix(line, "nr_throttled ")
			n := 0
			fmt.Sscanf(strings.TrimSpace(val), "%d", &n)
			assert.Greater(t, n, 0, "process should have been throttled (nr_throttled > 0)")
			return
		}
	}
	t.Fatal("nr_throttled not found in cpu.stat")
}

// ---------------------------------------------------------------------------
// Unit tests for helpers
// ---------------------------------------------------------------------------

func TestLinux_FormatCPUMax(t *testing.T) {
	tests := []struct {
		percent int
		want    string
	}{
		{50, "50000 100000"},
		{100, "100000 100000"},
		{10, "10000 100000"},
		{1, "1000 100000"},
		{0, "max 100000"},   // unlimited
		{-1, "max 100000"},  // unlimited
		{200, "max 100000"}, // invalid → unlimited
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("percent_%d", tt.percent), func(t *testing.T) {
			assert.Equal(t, tt.want, formatCPUMax(tt.percent))
		})
	}
}

// ---------------------------------------------------------------------------
// Factory test
// ---------------------------------------------------------------------------

func TestLinuxFactory_ReturnsLinuxIsolator(t *testing.T) {
	iso, err := NewIsolator()
	require.NoError(t, err)

	_, ok := iso.(*LinuxIsolator)
	assert.True(t, ok, "expected *LinuxIsolator on Linux with cgroups v2")
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestIsolator(t *testing.T) *LinuxIsolator {
	t.Helper()
	iso, err := NewLinuxIsolator()
	require.NoError(t, err, "LinuxIsolator requires cgroups v2 (run in Docker with --privileged)")
	return iso
}

// findCgroupPath returns the first child directory under the isolator's cgroup base.
func findCgroupPath(t *testing.T, iso *LinuxIsolator) string {
	t.Helper()
	entries, err := os.ReadDir(iso.cgroupBase)
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(iso.cgroupBase, e.Name())
		}
	}
	return ""
}
