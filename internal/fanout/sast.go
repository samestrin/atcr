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

// errNoSASTBackend reports a runner constructor that returned no runner; it
// surfaces as the fail-open notice.
var errNoSASTBackend = errors.New("no SAST scanner backend is available")

// newSASTRunner resolves the scanner the pre-filter runs: the Semgrep wrapper,
// which reports a missing binary or a failed scan as sast.ErrUnavailable. A
// variable so tests inject a fake runner instead of a real Semgrep.
var newSASTRunner = func() (sast.Runner, error) { return sast.Semgrep{}, nil }

// secretSASTLabel names the SAST block in a secret-guard warning or error.
const secretSASTLabel = "SAST findings"

// sastScan is one pre-filter scan's result: the runner's name and its
// findings. The zero value means no scan produced findings.
type sastScan struct {
	scanner  string
	findings []sast.Finding
}

// runSASTPreFilter runs the optional deterministic scanner over the review's
// changed files and returns the fenced untrusted block every reviewer prompt
// carries, or "", plus the scan itself for the reconcile sidecar (Epic 50.0).
// It is off unless the project config sets `sast.enabled: true`.
//
// It fails OPEN: a missing runner, a runner error or panic, and a scan that
// outlives sastScanTimeout each print one notice to stderr and return "" and a
// zero sastScan, so the review proceeds exactly as with the scanner off. The scan runs on its own
// goroutine so a runner that ignores ctx still cannot hold the review past the
// timeout; such a runner's goroutine is abandoned, not waited on.
func runSASTPreFilter(ctx context.Context, cfg *ReviewConfig, repo string, payloads map[string]modePayload) (string, sastScan) {
	if cfg.Project == nil || !cfg.Project.SAST.IsEnabled() {
		return "", sastScan{}
	}
	files := sastTargets(repo, payloads)
	if len(files) == 0 {
		return "", sastScan{}
	}
	runner, err := newSASTRunner()
	if err == nil && runner == nil {
		err = errNoSASTBackend
	}
	if err != nil {
		sastNotice(err)
		return "", sastScan{}
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
		return "", sastScan{}
	}
	if len(out.findings) == 0 {
		return "", sastScan{}
	}
	sentinel, err := payload.NewSASTSentinel()
	if err != nil {
		sastNotice(err)
		return "", sastScan{}
	}
	return payload.SASTBlock(out.findings, sentinel), sastScan{scanner: runner.Name(), findings: out.findings}
}

// writeSASTSidecar hands the scan to reconcile through reviewDir's sast.json,
// which mergeSAST reads into the reconciled report the Fixer works from. A scan
// with no findings removes any sidecar already in the dir, so a reused dir
// never reports an earlier run's findings. It fails open like the scan: a write
// or remove error prints the notice and the review continues.
func writeSASTSidecar(reviewDir string, scan sastScan) {
	if len(scan.findings) == 0 {
		if err := os.Remove(filepath.Join(reviewDir, sast.ReportFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
			sastNotice(err)
		}
		return
	}
	if err := sast.WriteReport(reviewDir, sast.Report{Scanner: scan.scanner, Findings: scan.findings}); err != nil {
		sastNotice(err)
	}
}

// handOff writes the scan's sidecar into a freshly prepared review's dir, which
// does not exist until finalizePreparedReview scaffolds it. A failed prepare
// passes through untouched.
func (s sastScan) handOff(prep *PreparedReview, err error) (*PreparedReview, error) {
	if err != nil {
		return nil, err
	}
	writeSASTSidecar(prep.Dir, s)
	return prep, nil
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
