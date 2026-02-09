package isolation

import (
	"context"
	"os/exec"
	"time"
)

// Compile-time interface check.
var _ Isolator = (*FallbackIsolator)(nil)

// FallbackIsolator provides minimal process isolation using os/exec + timeout.
// Used on platforms where kernel-level isolation is unavailable.
// Only timeout enforcement is provided; all capabilities report false.
type FallbackIsolator struct{}

// NewFallbackIsolator creates a FallbackIsolator.
func NewFallbackIsolator() *FallbackIsolator {
	return &FallbackIsolator{}
}

// Wrap clones the command onto a context-aware exec.Cmd with timeout enforcement.
// The returned cleanup function must always be called after process completion.
// The caller must use the returned *exec.Cmd, not the original.
func (f *FallbackIsolator) Wrap(ctx context.Context, cmd *exec.Cmd, limits ResourceLimits) (*exec.Cmd, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	execCtx := ctx
	var cancel context.CancelFunc

	if limits.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, limits.Timeout)
	}

	// Clone cmd onto exec.CommandContext to guarantee context cancellation works.
	// exec.Cmd.Cancel is only honored for cmds created via exec.CommandContext.
	wrapped := exec.CommandContext(execCtx, cmd.Path, cmd.Args[1:]...)
	wrapped.Args = cmd.Args // Preserve original Args (CommandContext resolves Args[0] differently).
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr

	// Kill process on context cancellation and allow 5s for pipe drain.
	wrapped.Cancel = func() error {
		if wrapped.Process != nil {
			return wrapped.Process.Kill()
		}
		return nil
	}
	wrapped.WaitDelay = 5 * time.Second

	cleanup := func() {
		if cancel != nil {
			cancel()
		}
	}

	return wrapped, cleanup, nil
}

// Capabilities returns all-false caps (FallbackIsolator only enforces timeout).
func (f *FallbackIsolator) Capabilities() IsolatorCaps {
	return IsolatorCaps{}
}
