package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/sast"
)

// sastScanTimeout is the fixed ceiling on one pre-filter scan (Epic 50.0). A
// variable only so tests can shorten it.
var sastScanTimeout = 2 * time.Minute

// errNoSASTBackend is what newSASTRunner reports until a scanner backend is
// wired in; with `sast.enabled: true` it surfaces as the fail-open notice.
var errNoSASTBackend = errors.New("no SAST scanner backend is available in this build")

// newSASTRunner resolves the scanner the pre-filter runs. A variable so tests
// inject a fake runner instead of a real Semgrep.
var newSASTRunner = func() (sast.Runner, error) { return nil, errNoSASTBackend }

// secretSASTLabel names the SAST block in a secret-guard warning or error.
const secretSASTLabel = "SAST findings"

// runSASTPreFilter runs the optional deterministic scanner over the review's
// changed files and returns the fenced untrusted block every reviewer prompt
// carries, or "" (Epic 50.0). It is off unless the project config sets
// `sast.enabled: true`.
//
// It fails OPEN: a missing runner, a runner error or panic, and a scan that
// outlives sastScanTimeout each print one notice to stderr and return "", so
// the review proceeds exactly as with the scanner off. The scan runs on its own
// goroutine so a runner that ignores ctx still cannot hold the review past the
// timeout; such a runner's goroutine is abandoned, not waited on.
func runSASTPreFilter(ctx context.Context, cfg *ReviewConfig, repo string, payloads map[string]modePayload) string {
	if cfg.Project == nil || !cfg.Project.SAST.IsEnabled() {
		return ""
	}
	files := sastTargets(repo, payloads)
	if len(files) == 0 {
		return ""
	}
	runner, err := newSASTRunner()
	if err == nil && runner == nil {
		err = errNoSASTBackend
	}
	if err != nil {
		sastNotice(err)
		return ""
	}

	sctx, cancel := context.WithTimeout(ctx, sastScanTimeout)
	defer cancel()
	type outcome struct {
		findings []sast.Finding
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("scanner %q panicked: %v", runner.Name(), r)}
			}
		}()
		f, err := runner.Run(sctx, repo, files)
		done <- outcome{findings: f, err: err}
	}()

	var out outcome
	select {
	case out = <-done:
	case <-sctx.Done():
		out.err = fmt.Errorf("scanner %q did not finish within %s: %w", runner.Name(), sastScanTimeout, sctx.Err())
	}
	if out.err != nil {
		sastNotice(out.err)
		return ""
	}
	if len(out.findings) == 0 {
		return ""
	}
	sentinel, err := payload.NewSASTSentinel()
	if err != nil {
		sastNotice(err)
		return ""
	}
	return payload.SASTBlock(out.findings, sentinel)
}

// sastNotice prints the visible fail-open notice.
func sastNotice(err error) {
	fmt.Fprintf(os.Stderr, "atcr: notice: SAST pre-filter skipped (%v); the review continues without deterministic findings\n", err)
}

// sastTargets lists the changed files to scan: every payload entry path, across
// modes, that names a regular file under root, sorted and deduplicated. A
// deleted file, a symlink, and an engine-rendered section (claim ledger,
// Context Definitions) have no regular file at that path and are left out.
func sastTargets(root string, payloads map[string]modePayload) []string {
	seen := map[string]struct{}{}
	var files []string
	for _, mp := range payloads {
		for _, e := range mp.Entries {
			if _, dup := seen[e.Path]; dup || e.Path == "" {
				continue
			}
			seen[e.Path] = struct{}{}
			info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(e.Path)))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			files = append(files, e.Path)
		}
	}
	sort.Strings(files)
	return files
}
