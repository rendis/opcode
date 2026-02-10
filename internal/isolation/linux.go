//go:build linux

package isolation

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

const (
	cgroupRoot    = "/sys/fs/cgroup"
	opcodePrefix  = "opcode"
	cgroupPeriod  = 100000 // 100ms in microseconds (standard cpu.max period)
	cleanupDelay  = 50 * time.Millisecond
	cleanupRetries = 10
)

// Compile-time interface check.
var _ Isolator = (*LinuxIsolator)(nil)

// LinuxIsolator provides kernel-level process isolation using cgroups v2
// and Linux namespaces (PID, network).
type LinuxIsolator struct {
	cgroupBase string       // e.g. /sys/fs/cgroup/opcode
	caps       IsolatorCaps // detected from available controllers
}

// NewLinuxIsolator creates a LinuxIsolator backed by cgroups v2.
// Returns error if cgroups v2 is not available.
func NewLinuxIsolator() (*LinuxIsolator, error) {
	controllersPath := filepath.Join(cgroupRoot, "cgroup.controllers")
	data, err := os.ReadFile(controllersPath)
	if err != nil {
		return nil, fmt.Errorf("cgroups v2 not available: %w", err)
	}

	available := parseControllers(string(data))
	caps := buildCaps(available)

	base := filepath.Join(cgroupRoot, opcodePrefix)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, fmt.Errorf("create cgroup base %s: %w", base, err)
	}

	if err := enableControllers(base, available); err != nil {
		return nil, fmt.Errorf("enable cgroup controllers: %w", err)
	}

	return &LinuxIsolator{cgroupBase: base, caps: caps}, nil
}

// Capabilities returns the detected isolation capabilities.
func (l *LinuxIsolator) Capabilities() IsolatorCaps {
	return l.caps
}

// Wrap creates an isolated execution environment for cmd using cgroups v2 and namespaces.
// The returned cleanup function must always be called after process completion.
// The caller must use the returned *exec.Cmd, not the original.
func (l *LinuxIsolator) Wrap(ctx context.Context, cmd *exec.Cmd, limits ResourceLimits) (*exec.Cmd, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	id := uuid.New().String()
	cgPath := filepath.Join(l.cgroupBase, id)

	if err := os.Mkdir(cgPath, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create cgroup %s: %w", cgPath, err)
	}

	// Open cgroup directory FD early so the deferred rollback can close it.
	cgFD := -1
	success := false
	defer func() {
		if !success {
			if cgFD >= 0 {
				syscall.Close(cgFD)
			}
			removeCgroup(cgPath)
		}
	}()

	if err := l.writeLimits(cgPath, limits); err != nil {
		return nil, nil, err
	}

	// Open cgroup directory as FD for SysProcAttr.CgroupFD.
	var err error
	cgFD, err = syscall.Open(cgPath, syscall.O_DIRECTORY|syscall.O_RDONLY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open cgroup fd: %w", err)
	}

	execCtx := ctx
	var timeoutCancel context.CancelFunc

	if limits.Timeout > 0 {
		execCtx, timeoutCancel = context.WithTimeout(ctx, limits.Timeout)
	}

	// Clone cmd onto exec.CommandContext (same pattern as FallbackIsolator).
	wrapped := exec.CommandContext(execCtx, cmd.Path, cmd.Args[1:]...)
	wrapped.Args = cmd.Args
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr

	wrapped.Cancel = func() error {
		if wrapped.Process != nil {
			return wrapped.Process.Kill()
		}
		return nil
	}
	wrapped.WaitDelay = 5 * time.Second

	// Configure SysProcAttr with cgroup FD and namespace flags.
	cloneflags := uintptr(0)
	if l.caps.CanIsolatePID {
		cloneflags |= syscall.CLONE_NEWPID
	}
	if !limits.AllowNetwork && l.caps.CanLimitNetwork {
		cloneflags |= syscall.CLONE_NEWNET
	}

	wrapped.SysProcAttr = &syscall.SysProcAttr{
		UseCgroupFD: true,
		CgroupFD:    cgFD,
		Cloneflags:  cloneflags,
	}

	cleanup := l.buildCleanup(cgFD, cgPath, timeoutCancel)
	success = true
	return wrapped, cleanup, nil
}

// buildCleanup creates the cleanup function for a wrapped process.
func (l *LinuxIsolator) buildCleanup(cgFD int, cgPath string, timeoutCancel context.CancelFunc) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			syscall.Close(cgFD)

			if timeoutCancel != nil {
				timeoutCancel()
			}

			// Kill all processes in the cgroup.
			killPath := filepath.Join(cgPath, "cgroup.kill")
			if err := os.WriteFile(killPath, []byte("1"), 0o644); err != nil {
				killCgroupProcesses(cgPath)
			}

			// Retry removal (cgroup must be empty of processes).
			for range cleanupRetries {
				if err := os.Remove(cgPath); err == nil {
					return
				}
				time.Sleep(cleanupDelay)
			}
			slog.Warn("isolation: failed to remove cgroup after retries", "path", cgPath)
		})
	}
}

// writeLimits writes resource limit files to the cgroup directory.
func (l *LinuxIsolator) writeLimits(cgPath string, limits ResourceLimits) error {
	if limits.MaxMemoryBytes > 0 && l.caps.CanLimitMemory {
		val := strconv.FormatInt(limits.MaxMemoryBytes, 10)
		if err := writeLimit(cgPath, "memory.max", val); err != nil {
			return fmt.Errorf("set memory.max: %w", err)
		}
		// Disable swap to enforce a hard memory ceiling.
		// Without this, the process can spill into swap and avoid OOM.
		_ = writeLimit(cgPath, "memory.swap.max", "0")
	}

	if limits.MaxCPUPercent > 0 && l.caps.CanLimitCPU {
		val := formatCPUMax(limits.MaxCPUPercent)
		if err := writeLimit(cgPath, "cpu.max", val); err != nil {
			return fmt.Errorf("set cpu.max: %w", err)
		}
	}

	return nil
}

// writeLimit writes a value to a cgroup control file.
func writeLimit(cgPath, file, value string) error {
	path := filepath.Join(cgPath, file)
	return os.WriteFile(path, []byte(value), 0o644)
}

// formatCPUMax converts a CPU percentage (1-100) to cgroups v2 cpu.max format "QUOTA PERIOD".
func formatCPUMax(percent int) string {
	if percent <= 0 || percent > 100 {
		return fmt.Sprintf("max %d", cgroupPeriod)
	}
	quota := cgroupPeriod * percent / 100
	return fmt.Sprintf("%d %d", quota, cgroupPeriod)
}

// removeCgroup kills all processes in the cgroup and removes the directory.
func removeCgroup(cgPath string) {
	killPath := filepath.Join(cgPath, "cgroup.kill")
	if err := os.WriteFile(killPath, []byte("1"), 0o644); err != nil {
		killCgroupProcesses(cgPath)
	}
	for range cleanupRetries {
		if err := os.Remove(cgPath); err == nil {
			return
		}
		time.Sleep(cleanupDelay)
	}
}

// killCgroupProcesses reads cgroup.procs and sends SIGKILL to each PID.
func killCgroupProcesses(cgPath string) {
	procsPath := filepath.Join(cgPath, "cgroup.procs")
	f, err := os.Open(procsPath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		pid, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
		if err != nil || pid <= 0 {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			slog.Warn("isolation: failed to kill process in cgroup", "pid", pid, "error", err)
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("isolation: error reading cgroup.procs", "path", procsPath, "error", err)
	}
}

// parseControllers parses space-separated controller names from cgroup.controllers.
func parseControllers(data string) map[string]bool {
	m := make(map[string]bool)
	for _, c := range strings.Fields(strings.TrimSpace(data)) {
		m[c] = true
	}
	return m
}

// buildCaps maps available cgroup controllers to IsolatorCaps.
func buildCaps(controllers map[string]bool) IsolatorCaps {
	return IsolatorCaps{
		CanLimitMemory:  controllers["memory"],
		CanLimitCPU:     controllers["cpu"],
		CanLimitNetwork: true, // via CLONE_NEWNET, not a cgroup controller
		CanIsolateFS:    false, // mount namespace deferred
		CanIsolatePID:   controllers["pids"],
	}
}

// enableControllers writes +controller entries to cgroup.subtree_control
// so child cgroups can use those controllers.
func enableControllers(basePath string, controllers map[string]bool) error {
	wanted := []string{"memory", "cpu", "pids"}
	var enable []string
	for _, c := range wanted {
		if controllers[c] {
			enable = append(enable, "+"+c)
		}
	}
	if len(enable) == 0 {
		return nil
	}
	controlPath := filepath.Join(basePath, "cgroup.subtree_control")
	return os.WriteFile(controlPath, []byte(strings.Join(enable, " ")), 0o644)
}

