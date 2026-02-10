//go:build linux

package isolation

import "log/slog"

// NewIsolator returns the platform-appropriate Isolator.
// On Linux, attempts LinuxIsolator (cgroups v2). Falls back to FallbackIsolator
// if cgroups v2 is unavailable.
func NewIsolator() (Isolator, error) {
	iso, err := NewLinuxIsolator()
	if err != nil {
		slog.Warn("isolation: cgroups v2 unavailable, using fallback (timeout only)", "error", err)
		return NewFallbackIsolator(), nil
	}
	return iso, nil
}
