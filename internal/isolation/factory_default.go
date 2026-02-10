//go:build !linux

package isolation

import "log/slog"

// NewIsolator returns the platform-appropriate Isolator.
// On non-Linux platforms, returns FallbackIsolator (timeout-only enforcement).
func NewIsolator() (Isolator, error) {
	slog.Warn("isolation: no kernel isolation available, using fallback (timeout only)")
	return NewFallbackIsolator(), nil
}
