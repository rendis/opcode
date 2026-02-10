package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rendis/opcode/internal/isolation"
	"github.com/rendis/opcode/pkg/schema"
)

const (
	defaultShellTimeout  = 30 * time.Second
	defaultMaxOutputSize = 10 * 1024 * 1024 // 10MB
)

// ShellConfig configures the shell.exec action.
type ShellConfig struct {
	Isolator       isolation.Isolator
	DefaultTimeout time.Duration
	DefaultLimits  isolation.ResourceLimits
	MaxOutputSize  int64
}

// ShellActions returns all shell-related actions.
func ShellActions(cfg ShellConfig) []Action {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaultShellTimeout
	}
	if cfg.MaxOutputSize <= 0 {
		cfg.MaxOutputSize = defaultMaxOutputSize
	}
	if cfg.Isolator == nil {
		cfg.Isolator = &isolation.FallbackIsolator{}
	}
	return []Action{
		&shellExecAction{cfg: cfg},
	}
}

// --- JSON Schemas ---

const shellExecInputSchema = `{
  "type": "object",
  "properties": {
    "command": {"type": "string"},
    "args": {"type": "array", "items": {"type": "string"}},
    "env": {"type": "object", "additionalProperties": {"type": "string"}},
    "cwd": {"type": "string"},
    "stdin": {"type": "string"},
    "timeout": {"type": "string"},
    "shell": {"type": "boolean", "default": false}
  },
  "required": ["command"]
}`

const shellExecOutputSchema = `{
  "type": "object",
  "properties": {
    "stdout": {"description": "auto-parsed JSON if valid, raw string otherwise"},
    "stdout_raw": {"type": "string", "description": "always the raw string output"},
    "stderr": {"type": "string"},
    "exit_code": {"type": "integer"},
    "duration_ms": {"type": "integer"},
    "killed": {"type": "boolean"}
  }
}`

// --- Param helpers ---

func stringSliceParam(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func stringMapParam(m map[string]any, key string) map[string]string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// --- shellExecAction ---

type shellExecAction struct {
	cfg ShellConfig
}

func (a *shellExecAction) Name() string { return "shell.exec" }

func (a *shellExecAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Execute a system command with process isolation, capturing stdout, stderr, and exit code.",
		InputSchema:  json.RawMessage(shellExecInputSchema),
		OutputSchema: json.RawMessage(shellExecOutputSchema),
	}
}

func (a *shellExecAction) Validate(input map[string]any) error {
	cmd := stringParam(input, "command", "")
	if cmd == "" {
		return schema.NewError(schema.ErrCodeValidation, "shell.exec: missing required param 'command'")
	}
	return nil
}

func (a *shellExecAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	command := stringParam(params, "command", "")
	args := stringSliceParam(params, "args")
	envMap := stringMapParam(params, "env")
	cwd := stringParam(params, "cwd", "")
	stdinStr := stringParam(params, "stdin", "")
	timeoutStr := stringParam(params, "timeout", "")
	shellMode := boolParam(params, "shell", false)

	// Build exec.Cmd.
	var cmd *exec.Cmd
	if shellMode {
		// Join command and args into a single shell string.
		fullCmd := command
		if len(args) > 0 {
			fullCmd = command + " " + strings.Join(args, " ")
		}
		cmd = exec.Command("/bin/sh", "-c", fullCmd)
	} else {
		cmd = exec.Command(command, args...)
	}

	// Set working directory (validate path first).
	if cwd != "" {
		if err := a.cfg.DefaultLimits.ValidatePath(cwd, isolation.PathAccessRead); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodePathDenied, "shell.exec: cwd path denied: %v", err).WithCause(err)
		}
		cmd.Dir = cwd
	}

	// Set environment: inherit current env + user overrides.
	if envMap != nil {
		cmd.Env = os.Environ()
		for k, v := range envMap {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Set stdin.
	if stdinStr != "" {
		cmd.Stdin = strings.NewReader(stdinStr)
	}

	// Parse timeout.
	timeout := a.cfg.DefaultTimeout
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = d
		}
	}

	// Create timeout context so we can detect kills via ctx.Err().
	// Pass Timeout=0 to the isolator since we manage the deadline ourselves.
	execCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	// Build resource limits from defaults (timeout handled by our context).
	limits := a.cfg.DefaultLimits
	limits.Timeout = 0

	// Wrap with isolator.
	wrapped, cleanup, err := a.cfg.Isolator.Wrap(execCtx, cmd, limits)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeIsolation, "shell.exec: isolation wrap failed: %v", err).WithCause(err)
	}
	defer cleanup()

	// Set up stdout/stderr capture with size limits.
	var stdoutBuf, stderrBuf bytes.Buffer
	wrapped.Stdout = &limitedWriter{w: &stdoutBuf, limit: a.cfg.MaxOutputSize}
	wrapped.Stderr = &limitedWriter{w: &stderrBuf, limit: a.cfg.MaxOutputSize}

	// Execute.
	start := time.Now()
	runErr := wrapped.Run()
	durationMs := time.Since(start).Milliseconds()

	// Extract exit code and killed status.
	exitCode := 0
	killed := false
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			// Non-exit error (e.g., command not found).
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "shell.exec: %v", runErr).WithCause(runErr)
		}
		// Detect timeout kill via the context we own.
		if execCtx.Err() == context.DeadlineExceeded {
			killed = true
		}
	}

	// Auto-parse stdout if it's valid JSON, for consistent interpolation
	// (mirrors http.get which auto-parses JSON bodies).
	stdoutStr := stdoutBuf.String()
	var parsedStdout any = stdoutStr
	if stdoutBuf.Len() > 0 && json.Valid(stdoutBuf.Bytes()) {
		var parsed any
		if err := json.Unmarshal(stdoutBuf.Bytes(), &parsed); err == nil {
			parsedStdout = parsed
		}
	}

	// Marshal output.
	result := map[string]any{
		"stdout":      parsedStdout,
		"stdout_raw":  stdoutStr,
		"stderr":      stderrBuf.String(),
		"exit_code":   exitCode,
		"duration_ms": durationMs,
		"killed":      killed,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "shell.exec: failed to marshal output").WithCause(err)
	}
	return &ActionOutput{Data: json.RawMessage(data)}, nil
}

// --- limitedWriter ---

// limitedWriter wraps a writer and silently discards bytes beyond the limit.
// Write always reports the full len(p) consumed to prevent the subprocess from
// blocking on a full pipe.
type limitedWriter struct {
	w       io.Writer
	limit   int64
	written int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	total := len(p) // Capture original length before any truncation.
	remaining := lw.limit - lw.written
	if remaining <= 0 {
		return total, nil
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	if err != nil {
		return total, err
	}
	return total, nil
}
