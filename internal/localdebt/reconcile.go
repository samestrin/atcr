package localdebt

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
)

// PersistOpts parameterizes PersistForReconcile for the entry point calling it.
//
// AutoCompact carries the caller's threshold policy rather than being read from a
// package var, so the cli-side test seam stays live across this package boundary: a
// var in cli is unreachable from here, and hard-coding a policy would leave that
// seam inert while every test still passed.
//
// Disabling automatic compaction is deliberately NOT expressible. CompactPolicy's
// convention is "zero means the production defaults" (100k records / 100 MiB), which
// both callers rely on — cli's seam only ever SHRINKS the thresholds for tests, and
// the MCP path passes a zero value precisely to get the defaults. AC5 treats
// automatic compaction as mandatory, so no off sentinel is reserved; should a caller
// ever need one, this struct is internal with two in-module callers and can gain it
// without breaking anything outside the module.
type PersistOpts struct {
	Root        string        // resolved repo root; the store is DefaultDir(Root)
	Diag        io.Writer     // best-effort diagnostics sink
	AutoCompact CompactPolicy // automatic-compaction threshold; zero value = production defaults
}

// PersistForReconcile appends a reconcile run's findings to the .atcr/-scoped local
// TD store under DefaultDir(opts.Root) (Epic 20.1 Story 2; Plan 35.13 T6). It is a
// best-effort, non-fatal side effect modeled on scorecard.EmitForReconcile: every
// failure is logged to Diag and the function always returns cleanly, so a caller's
// return value, exit code, or tool result is never affected by a persistence
// problem.
//
// It is the SINGLE persistence implementation, shared by `atcr reconcile` and the
// MCP atcr_reconcile handler. That is the point of it living here rather than in
// cli: the two entry points cannot diverge, so the MCP path inherits the status-
// aware dedup seeding, the minimal-decode streaming read, and the automatic
// compaction trigger without restating any of them. Copying this body into
// internal/mcp would satisfy the parity requirement on the day it shipped and freeze
// the MCP path at that day's behavior forever after.
//
// It reads from res.JSONFindings() (NOT res.Findings) so the Epic 18.3
// Justification/SourceReport enrichment is carried through — those fields live
// only on the cached JSONFinding layer. A zero-finding run does no I/O (no store
// directory is created).
//
// Dedup is write-time by finding id (history.FindingID(file,line,problem), via
// Record.StampID): a single streaming pass over the store seeds the set of ids
// already in it, and each new record is appended only if its id is unseen — the
// same set also collapses in-run duplicates (two findings that hash to one id
// write once). A dedup-read failure fails OPEN toward append (log + append without
// dedup) rather than silently dropping the whole run's backlog.
//
// The seed decodes Summary values ({id, status, severity, ts, has-file}) rather
// than whole Records: it needs an id's effective STATUS and nothing else, and a
// minimal decode keeps peak memory an order of magnitude below the full-record
// path on a large store (Plan 35.13 AC6; streaming.go).
//
// The fail-open path is all-or-nothing by design: a partial read is NOT used as a
// smaller seed, because the seed keys on an id's EFFECTIVE status and the fold can
// only compute that from the id's complete history. See the read-error branch for
// the failure this prevents.
//
// The seed is SCOPED, not the whole store: only ids whose effective (folded)
// record suppresses (wontfix) or is still open are seeded. A re-detected
// resolved/deferred id is therefore appended as a fresh open record, and
// FoldRecords — which is recency-aware — returns it to the open backlog. This is
// the write half of one coupled decision; the read half is FoldRecords' precedence
// rule, and its doc comment carries the same warning. Seeding every id instead
// would make a regression never re-append and silently restore permanent closure
// with both sides' tests still green.
func PersistForReconcile(reviewDir string, res reconcile.Result, opts PersistOpts) {
	diag := opts.Diag
	if diag == nil {
		diag = io.Discard
	}
	findings := res.JSONFindings()
	if len(findings) == 0 {
		return // no-op on an empty result; never a zero-length write
	}

	// An empty Root would make DefaultDir resolve .atcr/debt relative to the
	// process CWD — the silent wrong-store write ResolveStoreRoot's whole
	// precedence design exists to prevent (root.go). Both entry points gate on
	// ResolveStoreRoot's ok before calling, but the bridge is the single
	// persistence implementation, so the invariant holds HERE rather than only
	// in caller-side checks a third caller could forget.
	if strings.TrimSpace(opts.Root) == "" {
		_, _ = fmt.Fprintf(diag, "localdebt: no repo root supplied; skipping local debt persistence\n")
		return
	}

	dir := DefaultDir(opts.Root)
	seen := make(map[string]bool)
	// Records to persist, built first so the whole run appends under ONE lock
	// acquisition (appendBatch) instead of a withLock cycle per finding.
	var batch []Record
	if sums, err := ReadSummaries(dir, ReadOpts{Writer: diag}); err != nil {
		// Fail open: append anyway so a corrupt/unreadable store does not drop this
		// run's findings from the backlog. The error is already path-scrubbed
		// (basePathErr). The seed's suppression role is an OPTIMIZATION, not a
		// correctness mechanism: foldByID's rule 1 selects a suppressing (wontfix)
		// record unconditionally, so a re-appended open record never displaces a
		// dismissal. The REAL cost of an empty seed is duplicates — every
		// reconcile re-appends the entire finding set until the read error is
		// fixed, and MaybeCompact cannot recover because readAllPreserving hits
		// the same unreadable shard. The warning names that, not a dismissal
		// risk that does not exist.
		//
		// The PARTIAL summaries ReadSummaries returns alongside the error are
		// deliberately DISCARDED rather than used as a smaller seed. Seeding from a
		// partial read looks strictly safer and is not: the seed's input is an id's
		// EFFECTIVE status, which the fold can only compute from that id's COMPLETE
		// history. Drop the shard holding an id's resolution and the id folds to
		// `open`, gets seeded as still-outstanding, and the re-detection that should
		// have re-opened it is skipped instead — silently suppressing exactly the
		// regression this store exists to surface. All-or-nothing is the only
		// fold-safe fail-open.
		_, _ = fmt.Fprintf(diag, "localdebt: dedup read failed, appending without dedup; duplicate records will be appended and store growth is unbounded until the read error is fixed: %v\n", err)
	} else {
		// Fold FIRST, then seed from the effective record. A per-summary
		// seen[s.ID] = true would mark every id ever written, including the
		// resolved/deferred ones that must re-open on re-detection — silently
		// restoring terminal-forever dedup with every allocation assertion still
		// green, because those measure bytes and this is behavior.
		for _, s := range FoldSummaries(sums) {
			if IsSuppressingStatus(s.Status) || !IsClosedStatus(s.Status) {
				seen[s.ID] = true
			}
		}
	}

	// run_id mirrors scorecard.EmitForReconcile verbatim so a finding's shard and
	// provenance line up across the two ledgers; the ts is the same reconcile
	// timestamp (deterministic, no second clock read).
	runID := res.Summary.ReconciledAt + "-" + filepath.Base(reviewDir)

	// Validate the derived month ONCE, here: every record in this run shares the
	// run_id, so a month monthFromRunID rejects (an empty or non-RFC3339
	// ReconciledAt) would make EVERY Append fail identically — N "append failed"
	// lines, zero persisted records, and a clean return with no summary. One
	// diagnostic naming the bad run_id is the whole story. This is the call-site
	// half of the precondition ManualRunID enforces internally for `debt add`.
	if _, err := monthFromRunID(runID); err != nil {
		_, _ = fmt.Fprintf(diag, "localdebt: %v; skipping local debt persistence\n", err)
		return
	}

	// Resolve each finding's model from the fan-out pool summary's per-agent
	// AgentStatus.Model (schema v2, Sprint 30.0), mirroring EmitForReconcile's
	// reviewer->model mapping. Best-effort: a missing/unreadable summary leaves the
	// map empty and records persist with Model == "" (attribution-incomplete), which
	// the quality-signal aggregation excludes from per-model rows rather than
	// mis-bucketing under an empty model.
	modelByReviewer := map[string]string{}
	if ps, err := fanout.ReadPoolSummary(reviewDir); err == nil {
		for _, a := range ps.Agents {
			if a.Model != "" {
				modelByReviewer[a.Agent] = a.Model
			}
		}
	}

	for _, f := range findings {
		// Apply the same exclusions the gate uses (internal/reconcile/gate.go
		// IsFailing): out-of-scope findings and refuted verdicts never persist,
		// so the local TD backlog matches what the gate considers real.
		// Path-warned findings are also skipped: a file that did not resolve under
		// the repo root is treated as a hallucinated path (Epic 5.0).
		if strings.ToLower(strings.TrimSpace(f.Category)) == reclib.CategoryOutOfScope {
			continue
		}
		if f.Verification != nil && strings.ToLower(strings.TrimSpace(f.Verification.Verdict)) == reclib.VerdictRefuted {
			continue
		}
		if f.PathWarning != "" {
			continue
		}

		// Attribution is recorded WITHOUT narrowing: the record keeps the
		// finding's FULL reviewer list. The store is the only persistent copy of
		// the finding, so stripping a sibling persona here would permanently
		// destroy the fact that they reported it — and with it their resolve-time
		// credit (cli/debt_resolve.go unions Reviewers across the id's live
		// records to attribute the outcome). The model-attributable subset goes
		// to ModelReviewers instead: AggregateQualitySignal credits THAT set
		// under the record's single Model, so a persona with no recorded model
		// is not credited under a model it never ran on. When no model resolves
		// (none recorded, or a cross-model merge) ModelReviewers stays empty and
		// the empty Model excludes the record from per-model rows regardless.
		model := resolveRecordModel(f.Reviewers, modelByReviewer)
		rec := Record{
			SchemaVersion: SchemaVersion,
			RunID:         runID,
			Timestamp:     res.Summary.ReconciledAt,
			Severity:      f.Severity,
			File:          f.File,
			Line:          f.Line,
			Problem:       f.Problem,
			Fix:           f.Fix,
			Category:      f.Category,
			EstMinutes:    f.EstMinutes,
			Evidence:      f.Evidence,
			Reviewers:     f.Reviewers,
			Confidence:    f.Confidence,
			Model:         model,
			Justification: f.Justification,
		}
		if model != "" {
			rec.ModelReviewers = attributableReviewers(f.Reviewers, modelByReviewer, model)
		}
		if f.SourceReport != nil {
			rec.SourceReport = &SourceReport{
				Path:    f.SourceReport.Path,
				Line:    f.SourceReport.Line,
				Section: f.SourceReport.Section,
			}
		}
		rec.StampID()
		if seen[rec.ID] {
			continue // already persisted (cross-run) or an in-run duplicate id
		}
		seen[rec.ID] = true
		batch = append(batch, rec)
	}

	// A run that added nothing cannot have pushed the store over a threshold, so
	// a fully-deduped, fully-excluded, or fully-failed reconcile pays ZERO extra
	// I/O — not even the stat walk.
	if len(batch) == 0 {
		return
	}
	// One lock cycle for the whole run (TD internal/localdebt/reconcile.go:204):
	// per-record Append would take and release withLock for every finding, and
	// under contention each of N findings could independently spin up to
	// lockWait. Per-record failures remain non-fatal — appendBatch mirrors the
	// old loop's continue-on-error semantics and reports them joined.
	written, err := appendBatch(dir, batch)
	if err != nil {
		_, _ = fmt.Fprintf(diag, "localdebt: append failed: %v\n", err)
	}
	if written == 0 {
		return
	}
	switch res, triggered, err := MaybeCompact(dir, opts.AutoCompact, ReadOpts{Writer: diag}); {
	case err != nil:
		// Best-effort, exactly like the appends above: compaction is store hygiene,
		// never a reason to change the reconcile's return value or the gate's exit
		// code. The store is append-only and Compact is atomic per shard, so a failed
		// compaction leaves the findings this run just wrote fully intact.
		_, _ = fmt.Fprintf(diag, "localdebt: automatic compaction failed: %v\n", err)
	case triggered:
		_, _ = fmt.Fprintf(diag, "localdebt: compacted %d records to %d (%d superseded dropped, %d preserved)\n",
			res.RecordsBefore, res.RecordsAfter, res.Dropped, res.Preserved)
	}
}

// resolveRecordModel picks the single model to attribute to a persisted
// local-debt record from its reviewers and the fan-out's reviewer->model map
// (Sprint 30.0, schema v2). A record carries one Model field, so a model can only
// be attributed when it is unambiguous: it returns the shared model when the
// record's resolvable reviewers all agree on one model (a single reviewer, or a
// multi-persona merge that ran on the same model). It returns "" —
// attribution-incomplete, which AggregateQualitySignal excludes from per-model
// rows — when no reviewer has a resolvable model, OR when reviewers span two or
// more DISTINCT models (a cross-model merged finding). Returning the first
// reviewer's model in the cross-model case would mis-credit the other personas'
// dismissal signal to a model they never ran on, so it is deliberately excluded
// rather than guessed.
func resolveRecordModel(reviewers []string, modelByReviewer map[string]string) string {
	model := ""
	for _, rev := range reviewers {
		m := modelByReviewer[rev]
		if m == "" {
			continue
		}
		if model == "" {
			model = m
		} else if m != model {
			return "" // reviewers disagree on model → attribution-incomplete
		}
	}
	return model
}

// attributableReviewers returns the subset of reviewers whose recorded pool
// model IS the record's resolved model, preserving reviewer order. It feeds
// Record.ModelReviewers: AggregateQualitySignal credits that subset under the
// record's single Model, so a sibling with no recorded model keeps its place
// in the record's full Reviewers (resolve-time credit survives) without being
// credited under a model it never ran on. Callers invoke it only with a
// resolved (non-empty) model, so every reviewer with an unrecorded model fails
// the equality and is excluded; a reviewer that ran on the resolved model is
// kept.
func attributableReviewers(reviewers []string, modelByReviewer map[string]string, model string) []string {
	out := make([]string, 0, len(reviewers))
	for _, rev := range reviewers {
		if modelByReviewer[rev] == model {
			out = append(out, rev)
		}
	}
	return out
}
