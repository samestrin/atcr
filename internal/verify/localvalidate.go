package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// localvalidate.go is the post-apply, language-independent validation gate for
// the --auto-fix flow (Sprint 17.0, Story 2). It is a sibling to
// syntaxguard.go's pre-apply, Go-only validateGoFixSyntax: where that parses Fix
// content with go/parser before it touches disk, this runs a real operator-
// supplied command against the working tree AFTER a patch has been applied, and
// reports a conservative pass/fail. Exit code is the sole signal — no
// stdout/stderr content heuristics — so behavior is fully owned by whoever
// configures the command. This code performs no file mutation and never imports
// internal/ghaction: GitHub orchestration happens strictly after this gate passes.

// maxValidationOutputBytes bounds how much stdout/stderr is retained per stream,
// so a pathological validation command cannot exhaust memory. Output past the cap
// is discarded and flagged via the result's *Truncated field; the child process
// is never blocked, since the capturing writer always reports a full consume.
const maxValidationOutputBytes = 1 << 20 // 1 MiB per stream

// validationWaitGrace bounds how long Run may block AFTER the timeout fires
// waiting for the command's I/O pipes to close. On unix, configureProcessGroup
// makes the timeout SIGKILL the whole process group, so a grandchild spawned by a
// shell (e.g. `sh -c "... sleep 60"`) is reaped directly rather than left holding
// the stdout pipe open. cmd.WaitDelay remains a platform-independent backstop: if
// any process still holds a pipe open past this grace (a non-unix target, or a
// grandchild that escaped its group), the pipes are force-closed so a hanging
// validation command can never stall --auto-fix indefinitely. It only ever applies
// on the cancel/timeout path; a normally-exiting command closes its pipes and
// returns immediately, unaffected.
const validationWaitGrace = 2 * time.Second

// defaultValidationTimeout is the bound applied when a caller passes a zero
// timeout, so a missing configuration value does not immediately fail every
// validation with DeadlineExceeded.
const defaultValidationTimeout = 2 * time.Minute

// ValidationResult captures the outcome of one post-apply validation command run.
// Stdout/Stderr hold the raw captured bytes as-is (never sanitized or mutated —
// display-time sanitization is a reporting-boundary concern), so non-UTF8 output
// is preserved without panic. TimedOut and StartError are distinct fields rather
// than folded into a fabricated exit code: a timeout and a command that could not
// start are different failures than a command that ran and exited non-zero.
type ValidationResult struct {
	ExitCode        int
	Stdout          string
	Stderr          string
	Duration        time.Duration
	TimedOut        bool
	StartError      error
	StdoutTruncated bool
	StderrTruncated bool
}

// Passed is the sole conservative pass/fail gate: a run passes only when it
// started cleanly, did not time out, and exited zero. Any non-zero exit or a
// timeout is a failure, with no inspection of stdout/stderr content — closing off
// any injection vector via crafted validation-command output (AC 02-03).
func (r ValidationResult) Passed() bool {
	return r.StartError == nil && !r.TimedOut && r.ExitCode == 0
}

// RunConfiguredValidation runs argv (an explicit argument list, never a shell
// string) in dir with a bounded timeout, capturing exit code, stdout, stderr, and
// wall-clock duration into a ValidationResult. It returns a non-nil error ONLY
// when the command could not start (or argv is empty) — the StartError case; a
// command that runs and exits non-zero, or that times out, is a completed result
// with a nil error and Passed()==false, so the caller distinguishes "cannot even
// validate" from "validation failed". The command is sourced entirely from argv;
// no shell interprets it, so no configured or injected value is expanded.
func RunConfiguredValidation(ctx context.Context, argv []string, dir string, timeout time.Duration) (ValidationResult, error) {
	if timeout <= 0 {
		timeout = defaultValidationTimeout
	}

	if len(argv) == 0 {
		err := errors.New("auto-fix validation command not found or not executable: no command configured")
		return ValidationResult{StartError: err}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			startErr := fmt.Errorf("validation working directory does not exist: %s: %w", dir, err)
			return ValidationResult{StartError: startErr}, startErr
		}
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.WaitDelay = validationWaitGrace
	// On unix, place the command in its own process group and override the default
	// cancel so a timeout SIGKILLs the whole group — reaping grandchildren spawned
	// by shells like `sh -c ...` instead of leaving them orphaned. No-op elsewhere.
	configureProcessGroup(cmd)
	stdout := &cappedBuffer{cap: maxValidationOutputBytes}
	stderr := &cappedBuffer{cap: maxValidationOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	start := time.Now()
	runErr := cmd.Run()
	res := ValidationResult{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		Duration:        time.Since(start),
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
	}

	// A deadline hit OR a parent-context cancellation ends the run before it could
	// complete cleanly: a hard failure regardless of what exit the killed process
	// reported. Both are folded into TimedOut (the only cancellation-class signal
	// on the result) so Passed()==false without a spurious command-not-found label.
	// Partial output captured before the kill is retained above.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
		res.TimedOut = true
		return res, nil
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(runErr, &exitErr):
			// Ran to completion but exited non-zero: a validation failure, not a
			// Go error. Passed()==false via the non-zero ExitCode.
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		case errors.Is(runErr, exec.ErrWaitDelay):
			// The command exited but a lingering child held its I/O pipes open
			// past cmd.WaitDelay, so the run completed uncleanly. That is a hard
			// failure, not a start failure: mark TimedOut (the wait grace elapsed)
			// so it fails closed without being mislabeled command-not-found.
			res.TimedOut = true
			return res, nil
		case errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, os.ErrPermission):
			// Genuinely could not start (binary missing / not executable): the
			// only case that is a true StartError.
			res.StartError = fmt.Errorf("auto-fix validation command not found or not executable: %s: %w", argv[0], runErr)
			return res, res.StartError
		default:
			// Any other failure to run (e.g. an I/O setup error): fail closed as a
			// start error rather than silently passing.
			res.StartError = fmt.Errorf("auto-fix validation could not run: %s: %w", argv[0], runErr)
			return res, res.StartError
		}
	}

	res.ExitCode = 0
	return res, nil
}

// ResolveValidateCommand picks the effective validation argv: an operator-
// configured command always wins; otherwise the single built-in convenience
// default ["go", "build", "./..."] applies ONLY when a go.mod exists at repoRoot
// (the sole project-type signal — there is no multi-language default table).
// Any other project with no configured command is a hard refusal, so --auto-fix
// never skips validation silently. The returned argv is derived solely from
// configured or the Go default; no PR/diff/model-derived value can reach it.
func ResolveValidateCommand(configured []string, repoRoot string) ([]string, error) {
	if len(configured) > 0 {
		return configured, nil
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
		return []string{"go", "build", "./..."}, nil
	}
	return nil, errors.New("auto-fix requires a configured validate_command: no default validation command for this project type")
}

// cappedBuffer accumulates up to cap bytes and drops the rest, flagging
// truncation, while always reporting a full write so the child process's pipe
// never blocks on a capture that has stopped storing.
type cappedBuffer struct {
	buf       []byte
	cap       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := c.cap - len(c.buf); remaining > 0 {
		if len(p) <= remaining {
			c.buf = append(c.buf, p...)
		} else {
			c.buf = append(c.buf, p[:remaining]...)
			c.truncated = true
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return string(c.buf) }
