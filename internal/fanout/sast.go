package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// sastScan is one pre-filter scan's result: the runner's name, its findings,
// and the secret_guard policy the sidecar copy is written under. The zero value
// means no scan produced findings.
type sastScan struct {
	scanner  string
	findings []sast.Finding
	policy   string
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
	return payload.SASTBlock(out.findings, sentinel), sastScan{scanner: runner.Name(), findings: out.findings, policy: secretGuardPolicy(cfg)}
}

// writeSASTSidecar hands the scan to reconcile through reviewDir's sast.json,
// which mergeSAST reads into the reconciled report the Fixer works from. A scan
// with no findings removes any sidecar already in the dir, so a reused dir
// never reports an earlier run's findings. It fails open like the scan: a write
// or remove error prints the notice and the review continues.
//
// The Fixer is an LLM and reads each finding's message and rule through the
// reconciled report, so the findings pass the secret guard first, under the
// review's policy (guardSASTFindings).
func writeSASTSidecar(reviewDir string, scan sastScan) {
	if len(scan.findings) > 0 {
		scan.findings = guardSASTFindings(scan.policy, scan.findings)
	}
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

// guardSASTFindings applies the secret_guard policy to the message and rule of
// every finding bound for the sidecar, in one scan: off and warn return them
// unchanged (warn prints a warning), redact replaces each hit with
// payload.SecretPlaceholder, and block drops every finding that holds one, or
// all of them when the scan could not be trusted. The sidecar has every finding
// in full, unlike the reviewer block, which lists 25 and cuts each message, so
// a credential past that cut is caught here. No output holds secret material.
func guardSASTFindings(policy string, findings []sast.Finding) []sast.Finding {
	if strings.TrimSpace(policy) == "" {
		policy = secretGuardWarn
	}
	if policy == secretGuardOff {
		return findings
	}
	// Each field is a [start, end) span of one text; the newline between
	// fields keeps a hit in one from running into the next unnoticed.
	type span struct{ start, end int }
	var b strings.Builder
	fields := make([][2]span, len(findings))
	for i, f := range findings {
		for j, text := range []string{f.Message, f.Rule} {
			start := b.Len()
			b.WriteString(text)
			fields[i][j] = span{start, b.Len()}
			b.WriteByte('\n')
		}
	}
	hits, scanErr := scanSecrets(context.Background(), b.String(), nil)
	if scanErr != nil {
		if policy == secretGuardBlock {
			fmt.Fprintf(os.Stderr, "atcr: warning: SAST findings: the secret scan failed (%v), so %s is not written (secret_guard: block)\n", scanErr, sast.ReportFile)
			return nil
		}
		fmt.Fprintf(os.Stderr, "atcr: warning: SAST findings: secret scan: %v; using the built-in ruleset\n", scanErr)
	}
	// local returns the hits inside s, clipped to it and shifted to its start.
	local := func(s span) []payload.SecretHit {
		var out []payload.SecretHit
		for _, h := range hits {
			if h.End <= s.start || h.Start >= s.end {
				continue
			}
			out = append(out, payload.SecretHit{Kind: h.Kind, Start: max(h.Start, s.start) - s.start, End: min(h.End, s.end) - s.start})
		}
		return out
	}
	var (
		found   []secretFinding
		out     []sast.Finding
		dropped int
	)
	for i, f := range findings {
		msgHits, ruleHits := local(fields[i][0]), local(fields[i][1])
		if len(msgHits)+len(ruleHits) == 0 {
			out = append(out, f)
			continue
		}
		for _, h := range append(msgHits, ruleHits...) {
			found = append(found, secretFinding{kind: h.Kind, file: f.File})
		}
		switch policy {
		case secretGuardBlock:
			dropped++
			continue
		case secretGuardRedact:
			f.Message = payload.RedactSecrets(f.Message, msgHits)
			f.Rule = payload.RedactSecrets(f.Rule, ruleHits)
		}
		out = append(out, f)
	}
	if len(found) == 0 {
		return out
	}
	switch policy {
	case secretGuardBlock:
		fmt.Fprintf(os.Stderr, "atcr: warning: dropped %d SAST finding(s) holding %s from %s (secret_guard: block)\n", dropped, describeFindings(found), sast.ReportFile)
	case secretGuardRedact:
		fmt.Fprintf(os.Stderr, "atcr: warning: redacted %s from the SAST findings saved to %s (secret_guard: redact)\n", describeFindings(found), sast.ReportFile)
	default:
		fmt.Fprintf(os.Stderr, "atcr: warning: the SAST findings hold %s and are saved unchanged to %s (secret_guard: warn); set secret_guard: redact or block to stop it\n", describeFindings(found), sast.ReportFile)
	}
	return out
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
