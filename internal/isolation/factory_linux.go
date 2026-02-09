//go:build linux

package isolation

// NewIsolator returns the platform-appropriate Isolator.
// On Linux, this will return LinuxIsolator (cgroups v2) once story 023 is implemented.
// For now, falls back to FallbackIsolator.
func NewIsolator() (Isolator, error) {
	return NewFallbackIsolator(), nil
}
