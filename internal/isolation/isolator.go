package isolation

import (
	"context"
	"os/exec"
	"time"
)

// ResourceLimits specifies constraints for isolated process execution.
type ResourceLimits struct {
	MaxMemoryBytes int64         `json:"max_memory_bytes,omitempty"`
	MaxCPUPercent  int           `json:"max_cpu_percent,omitempty"`
	MaxDiskIOBPS   int64         `json:"max_disk_io_bps,omitempty"`
	Timeout        time.Duration `json:"timeout,omitempty"`
	AllowNetwork   bool          `json:"allow_network"`
	ReadOnlyPaths  []string      `json:"read_only_paths,omitempty"`
	WritablePaths  []string      `json:"writable_paths,omitempty"`
	DenyPaths      []string      `json:"deny_paths,omitempty"`
}

// IsolatorCaps describes what a platform's isolator can enforce.
type IsolatorCaps struct {
	CanLimitMemory  bool `json:"can_limit_memory"`
	CanLimitCPU     bool `json:"can_limit_cpu"`
	CanLimitNetwork bool `json:"can_limit_network"`
	CanIsolateFS    bool `json:"can_isolate_fs"`
	CanIsolatePID   bool `json:"can_isolate_pid"`
}

// Isolator wraps a command with platform-specific process isolation.
// Implementations are auto-detected at startup: Linux → Cgroups v2, Fallback → os/exec + timeout.
type Isolator interface {
	Wrap(ctx context.Context, cmd *exec.Cmd, limits ResourceLimits) (*exec.Cmd, func(), error)
	Capabilities() IsolatorCaps
}
