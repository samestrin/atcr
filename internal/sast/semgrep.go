package sast

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// DefaultSemgrepConfig is the rule set Semgrep runs when Semgrep.Config is
// empty. "auto" is not used: it requires metrics, which the runner turns off.
const DefaultSemgrepConfig = "p/default"

// semgrepTimeout bounds each semgrep invocation. Fixed, not configurable; a
// var only so tests can shrink it.
var semgrepTimeout = 2 * time.Minute

// ErrUnavailable is matched (errors.Is) by every *UnavailableError.
var ErrUnavailable = errors.New("sast: scanner unavailable")

// UnavailableError reports that a scanner could not produce findings: its
// binary is missing, it exited non-zero, it timed out, or its output was not
// SARIF. Callers fail open on it. Reason names only the failure mode; scanner
// stderr can echo repo content, so it is never included.
type UnavailableError struct {
	Scanner string
	Reason  string
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("sast: %s unavailable: %s", e.Scanner, e.Reason)
}

// Is makes errors.Is(err, ErrUnavailable) hold for any *UnavailableError.
func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

// Semgrep runs the semgrep binary resolved on PATH and reads its SARIF
// output. It is optional: when semgrep is absent Run returns an
// *UnavailableError rather than failing the review.
type Semgrep struct {
	// Config is the --config value (registry pack or rules path);
	// empty means DefaultSemgrepConfig.
	Config string
}

// Name implements Runner.
func (Semgrep) Name() string { return "semgrep" }

// Run implements Runner. Semgrep runs with root as its working directory over
// files (root-relative), or over the whole root when files is empty, under a
// fixed timeout. A bad root or a cancelled ctx is returned as a plain error;
// every scanner-side failure is an *UnavailableError.
func (s Semgrep) Run(ctx context.Context, root string, files []string) ([]Finding, error) {
	if root == "" {
		return nil, errors.New("sast: scan root is required")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("sast: resolve scan root: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bin, err := exec.LookPath("semgrep")
	if err != nil {
		return nil, s.unavailable("binary not found on PATH")
	}

	config := s.Config
	if config == "" {
		config = DefaultSemgrepConfig
	}
	args := []string{"scan", "--config", config, "--sarif", "--quiet",
		"--metrics=off", "--disable-version-check", "--"}
	if len(files) == 0 {
		args = append(args, ".")
	} else {
		args = append(args, files...)
	}

	runCtx, cancel := context.WithTimeout(ctx, semgrepTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Dir = realRoot
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Bound Wait if a killed semgrep left a child holding the pipes open.
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr // the caller cancelled: not a scanner outage
		}
		if runCtx.Err() != nil {
			return nil, s.unavailable(fmt.Sprintf("timed out after %v", semgrepTimeout))
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, s.unavailable(fmt.Sprintf("exited with status %d", exitErr.ExitCode()))
		}
		return nil, s.unavailable("could not run")
	}

	findings, _, err := ParseSARIF(&stdout, realRoot)
	if err != nil {
		return nil, s.unavailable("output is not SARIF 2.1.0")
	}
	return findings, nil
}

func (s Semgrep) unavailable(reason string) error {
	return &UnavailableError{Scanner: s.Name(), Reason: reason}
}
