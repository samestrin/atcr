package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/spf13/cobra"
)

// newScorecardCmd builds `atcr scorecard [id-or-path]`: render the per-reviewer
// eval table for a single reconcile run, resolved either by run_id or by the
// review directory that produced it.
func newScorecardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scorecard [id-or-path]",
		Short: "Display the per-reviewer scorecard for a single reconcile run",
		Long: "Display the per-reviewer scorecard for a single reconcile run.\n\n" +
			"The argument is either a run_id (e.g. 2026-06-14T10:00:00Z-abc123) or the\n" +
			"path to a review directory; a path is resolved to its run_id via\n" +
			"reconciled/summary.json. Records are read from the local monthly JSONL\n" +
			"store (~/.config/atcr/scorecard/). Read-only.",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runScorecard,
	}
}

func runScorecard(cmd *cobra.Command, args []string) error {
	arg := ""
	if len(args) == 1 {
		arg = strings.TrimSpace(args[0])
	}
	if arg == "" {
		return usageError(errors.New("requires a run_id or path argument"))
	}

	runID, err := resolveScorecardRunID(arg)
	if err != nil {
		return err // already exit-coded by the resolver
	}

	dir, err := scorecard.DefaultDir()
	if err != nil {
		return fmt.Errorf("cannot determine scorecard store path: %w", err)
	}

	recs, err := scorecard.FindByRunID(dir, runID)
	if err != nil {
		// The run_id is already validated upstream (resolveScorecardRunID), so an
		// error here is a real store read failure (e.g. an unreadable month file),
		// not a usage error — surface it as a failure (exit 1).
		return fmt.Errorf("failed to read scorecard store: %w", err)
	}

	reviewers := make([]scorecard.Record, 0, len(recs))
	for _, r := range recs {
		if r.RecordType == scorecard.RecordTypeReviewer {
			reviewers = append(reviewers, r)
		}
	}
	if len(reviewers) == 0 {
		// No matching records is a real failure (exit 1), not a usage error.
		return fmt.Errorf("no scorecard records found for run %s: run 'atcr reconcile' to generate data", runID)
	}

	return renderScorecard(cmd.OutOrStdout(), reviewers)
}

// resolveScorecardRunID maps the id-or-path argument to a run_id. A path (mirrors
// the anchorDir contract: absolute, contains a separator, or ".") is resolved
// through its reconciled/summary.json; a bare argument must already be a
// well-formed run_id, so a typo fails fast as a usage error rather than a silent
// empty table.
func resolveScorecardRunID(arg string) (string, error) {
	if looksLikePath(arg) {
		runID, err := runIDFromReviewDir(arg)
		// A slash-bearing arg that is also a well-formed run_id is ambiguous:
		// looksLikePath sent it down the review-dir branch, but if that resolution
		// fails, retry it as a run_id rather than surfacing the confusing "no
		// reconciled/summary.json" error. A real review-dir path is never run_id
		// shaped, so this never masks a genuine path-resolution failure.
		if err != nil && scorecard.IsRunID(arg) {
			return arg, nil
		}
		return runID, err
	}
	if !scorecard.IsRunID(arg) {
		return "", usageError(fmt.Errorf("invalid run_id %q: expected a timestamp-prefixed id like 2026-06-14T10:00:00Z-abc123, or a review directory path", arg))
	}
	return arg, nil
}

// looksLikePath reports whether arg is an explicit filesystem path rather than a
// bare run_id. Both separators are checked so the contract is platform-uniform.
func looksLikePath(arg string) bool {
	return filepath.IsAbs(arg) || strings.ContainsAny(arg, `/\`) || arg == "."
}

// runIDFromReviewDir reconstructs the run_id the emitter wrote for a review
// directory: reconciled_at (from reconciled/summary.json) + "-" + base(dir),
// matching scorecard.EmitForReconcile. A missing summary.json is a usage error
// (exit 2, "run reconcile first"); a present-but-unreadable/corrupt one is a real
// failure (exit 1). Error messages echo the user-provided path, not the resolved
// absolute path, so no internal path leaks. An arbitrary review-dir path is
// accepted by design — this is a local, read-only CLI running with the user's own
// permissions (mirroring anchorDir's verbatim-path contract), so there is no
// trusted root to confine the read to and reaching any user-readable file grants
// no escalation; no bounds check is enforced.
func runIDFromReviewDir(arg string) (string, error) {
	clean := filepath.Clean(arg)
	summaryPath := filepath.Join(clean, "reconciled", "summary.json")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", usageError(fmt.Errorf("no reconciled/summary.json in %s: run 'atcr reconcile' first", arg))
		}
		// Strip the PathError wrapper (which carries the resolved absolute path)
		// so only the OS reason is exposed to the user.
		sanitized := err
		if inner := errors.Unwrap(err); inner != nil {
			sanitized = inner
		}
		return "", fmt.Errorf("failed to read summary.json in %s: %s", arg, sanitized)
	}
	var s struct {
		ReconciledAt string `json:"reconciled_at"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return "", fmt.Errorf("failed to parse summary.json in %s: %w", arg, err)
	}
	if s.ReconciledAt == "" {
		return "", fmt.Errorf("summary.json in %s has no reconciled_at timestamp", arg)
	}
	return s.ReconciledAt + "-" + filepath.Base(clean), nil
}

// renderScorecard writes the per-reviewer table to w via text/tabwriter. The
// verification columns (VERIFIED/REFUTED/SURV%) appear only when at least one
// record carries verification data; reviewers without it show "-". Duplicate
// reviewer rows (a retried emit) collapse last-write-wins. The whole table is
// built in a buffer and written once so a flush error cannot emit a half table.
// The single stdout write's error is propagated so a broken pipe is not silently
// reported as success.
func renderScorecard(w io.Writer, recs []scorecard.Record) error {
	byRev := make(map[string]scorecard.Record, len(recs))
	for _, r := range recs {
		byRev[r.Reviewer] = r // last-write-wins (AC 02-01 EC3)
	}
	names := make([]string, 0, len(byRev))
	for n := range byRev {
		names = append(names, n)
	}
	sort.Strings(names)

	hasVer := false
	for _, r := range byRev {
		if r.FindingsVerified != nil {
			hasVer = true
			break
		}
	}

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	header := "REVIEWER\tMODEL\tRAISED\tCORROBORATED\tSOLO\tCORR%\tCOST\tLATENCY"
	if hasVer {
		header += "\tVERIFIED\tREFUTED\tSURV%"
	}
	_, _ = fmt.Fprintln(tw, header)
	for _, n := range names {
		r := byRev[n]
		row := fmt.Sprintf("%s\t%s\t%d\t%d\t%d\t%s\t$%.4f\t%dms",
			sanitizeCell(r.Reviewer), sanitizeCell(r.Model),
			r.FindingsRaised, r.FindingsCorroborated, r.FindingsSolo,
			formatPercent(r.CorroborationRate), r.CostUSD, r.LatencyMS)
		if hasVer {
			if r.FindingsVerified != nil {
				survived := 0.0
				if r.SurvivedSkepticRate != nil {
					survived = *r.SurvivedSkepticRate
				}
				refuted := 0
				if r.FindingsRefuted != nil {
					refuted = *r.FindingsRefuted
				}
				row += fmt.Sprintf("\t%d\t%d\t%s", *r.FindingsVerified, refuted, formatPercent(survived))
			} else {
				row += "\t-\t-\t-"
			}
		}
		_, _ = fmt.Fprintln(tw, row)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// formatPercent renders a 0..1 rate as a rounded integer percentage ("58%"). The
// rate is clamped to [0,1] first so a corrupt stored value outside that range
// renders a sane bound rather than nonsense (e.g. 500%) or a negative percent,
// matching the export path's clampRate (internal/scorecard/export.go).
func formatPercent(rate float64) string {
	if rate < 0 {
		rate = 0
	} else if rate > 1 {
		rate = 1
	}
	return fmt.Sprintf("%d%%", int(rate*100+0.5))
}

// sanitizeCell strips control characters from a JSONL string field before it is
// rendered, so a crafted reviewer/model value cannot inject terminal control
// sequences (ANSI/C1) or row-fracturing line separators into the table output.
func sanitizeCell(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f: // C0 controls (incl. ESC) + DEL
			return -1
		case r >= 0x80 && r <= 0x9f: // C1 controls
			return -1
		case r == '\u2028' || r == '\u2029': // line/paragraph separators
			return -1
		}
		return r
	}, s)
}
