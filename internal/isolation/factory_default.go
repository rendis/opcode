//go:build !linux

package isolation

// NewIsolator returns the platform-appropriate Isolator.
// On non-Linux platforms, returns FallbackIsolator (timeout-only enforcement).
func NewIsolator() (Isolator, error) {
	return NewFallbackIsolator(), nil
}
