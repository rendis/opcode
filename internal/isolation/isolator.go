package isolation

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rendis/opcode/pkg/schema"
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

// PathAccessMode indicates the type of filesystem access being requested.
type PathAccessMode int

const (
	PathAccessRead PathAccessMode = iota
	PathAccessWrite
)

// ValidatePath checks whether the given path is permitted under these limits.
// Empty path lists mean unrestricted access (permissive default for dev/fallback).
// DenyPaths always takes precedence over allow lists.
func (r ResourceLimits) ValidatePath(path string, mode PathAccessMode) error {
	clean, err := resolveCleanPath(path)
	if err != nil {
		return schema.NewErrorf(schema.ErrCodePathDenied, "invalid path %q: %v", path, err)
	}

	// Deny list always wins. Fail-closed: invalid deny path → deny access.
	for _, deny := range r.DenyPaths {
		base, err := resolveCleanPath(deny)
		if err != nil {
			return schema.NewErrorf(schema.ErrCodePathDenied,
				"path %q denied: invalid deny rule %q: %v", path, deny, err)
		}
		if isUnderPath(clean, base) {
			return schema.NewErrorf(schema.ErrCodePathDenied, "path %q is denied", path)
		}
	}

	hasReadOnly := len(r.ReadOnlyPaths) > 0
	hasWritable := len(r.WritablePaths) > 0

	// No restrictions configured — unrestricted access.
	if !hasReadOnly && !hasWritable {
		return nil
	}

	// Resolve allow lists. Invalid entries are skipped (fail-open for allow is safe
	// because the path must still match at least one valid entry to be allowed).
	switch mode {
	case PathAccessWrite:
		if !hasWritable {
			return schema.NewErrorf(schema.ErrCodePathDenied, "write access to %q denied: no writable paths configured", path)
		}
		for _, w := range r.WritablePaths {
			base, err := resolveCleanPath(w)
			if err != nil {
				continue // Invalid allow entry cannot grant access — safe to skip.
			}
			if isUnderPath(clean, base) {
				return nil
			}
		}
		return schema.NewErrorf(schema.ErrCodePathDenied, "write access to %q denied: not under any writable path", path)

	case PathAccessRead:
		for _, ro := range r.ReadOnlyPaths {
			base, err := resolveCleanPath(ro)
			if err != nil {
				continue
			}
			if isUnderPath(clean, base) {
				return nil
			}
		}
		for _, w := range r.WritablePaths {
			base, err := resolveCleanPath(w)
			if err != nil {
				continue
			}
			if isUnderPath(clean, base) {
				return nil
			}
		}
		return schema.NewErrorf(schema.ErrCodePathDenied, "read access to %q denied: not under any allowed path", path)
	}

	return nil
}

// resolveCleanPath cleans and resolves a path to absolute.
// Walks up ancestors to resolve symlinks on the longest existing prefix,
// ensuring consistent resolution even for non-existent paths (e.g. new files).
func resolveCleanPath(path string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path contains null byte")
	}

	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}

	// Try full path first (fast path when target exists).
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}

	// Walk up to find the longest existing ancestor and resolve its symlinks.
	return resolveAncestor(abs), nil
}

// resolveAncestor walks up from path until it finds an existing directory,
// resolves symlinks on that ancestor, and re-appends the unresolved suffix.
func resolveAncestor(path string) string {
	dir := path
	for range 256 { // Defensive depth limit.
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			rel, err := filepath.Rel(parent, path)
			if err != nil {
				return path
			}
			return filepath.Join(resolved, rel)
		}
		dir = parent
	}
	return path
}

// isUnderPath returns true if path is under (or equal to) the base directory.
// Uses filepath.Rel to avoid string-prefix false positives (e.g. /tmp vs /tmpevil).
func isUnderPath(path, base string) bool {
	if path == base {
		return true
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	// rel must not escape base (no leading "..")
	return !strings.HasPrefix(rel, "..")
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
