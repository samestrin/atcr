package localdebt

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/samestrin/atcr/internal/history"
	"github.com/samestrin/atcr/internal/reconcile"
)

// oneFindingResult builds a minimal reconcile.Result carrying a single finding, so
// a persistence test states only the thing it is testing.
func oneFindingResult(file string, line int, problem string) reconcile.Result {
	return reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{
				Severity: "HIGH", File: file, Line: line, Problem: problem,
				Fix: "fix it", Category: "correctness", EstMinutes: 10,
			}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-08-04T10:00:00Z"},
	}
}

// chdir moves the process into dir for the duration of the test. Persistence tests
// need a CWD that is deliberately NOT the store root — that divergence is the whole
// defect this bridge exists to fix, and a test running with CWD == root would pass
// against the CWD-relative code it replaced.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// TestPersistForReconcile_WritesUnderRootNotCWD is the core regression this task
// exists to prevent (AC7/AC8): the store is resolved from opts.Root, and the process
// CWD — which for the MCP server is whatever launched it — contributes nothing.
//
// The negative half matters as much as the positive: asserting only that the record
// landed under root would still pass if the code ALSO created a store under the CWD.
func TestPersistForReconcile_WritesUnderRootNotCWD(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	chdir(t, cwd)

	var diag bytes.Buffer
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Root: root, Diag: &diag})

	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1, "the finding must land in the store under opts.Root")
	assert.Equal(t, "a.go", recs[0].File)

	_, err = os.Stat(filepath.Join(cwd, ".atcr"))
	assert.True(t, os.IsNotExist(err), "nothing may be created under the process CWD: %v", err)
}

// TestPersistForReconcile_CancelledContextStopsBeforeWriting closes TD
// internal/mcp/handlers.go:406. The bridge does a full streaming store read, one
// lock-protected batch append (waiting up to lockWait under contention) and a
// synchronous whole-store rewrite via MaybeCompact — none of it cancellable, so an
// MCP client that timed out or disconnected left the handler running to completion
// and the server draining it on shutdown. A CLI run at least has a human with
// Ctrl-C; a serve-mode handler has neither.
//
// Context is optional and nil-safe (every other test here omits it), so the guard
// cannot become a trap for a caller that never supplied one.
func TestPersistForReconcile_CancelledContextStopsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var diag bytes.Buffer
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"),
		PersistOpts{Root: root, Diag: &diag, Context: ctx})

	_, err := os.Stat(filepath.Join(root, ".atcr"))
	assert.True(t, os.IsNotExist(err), "a cancelled persist must not create the store: %v", err)
	assert.Contains(t, diag.String(), "cancelled",
		"the abandoned side effect must be reported, not silent")
}

// TestPersistForReconcile_NilContextPersistsNormally pins the nil-safety half: the
// three CLI call sites pass no Context, and a guard that treated the zero value as
// "already cancelled" would silently disable persistence for all of them.
func TestPersistForReconcile_NilContextPersistsNormally(t *testing.T) {
	root := t.TempDir()

	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Root: root})

	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, recs, 1, "an unset Context must mean 'not cancellable', never 'cancelled'")
}

// TestPersistForReconcile_EmptyResultDoesNoIO pins the zero-finding no-op: no store
// directory is created, so a reconcile that found nothing leaves no trace at all.
func TestPersistForReconcile_EmptyResultDoesNoIO(t *testing.T) {
	root := t.TempDir()

	PersistForReconcile("review", reconcile.Result{Summary: reclib.Summary{ReconciledAt: "2026-08-04T10:00:00Z"}}, PersistOpts{Root: root})

	_, err := os.Stat(filepath.Join(root, ".atcr"))
	assert.True(t, os.IsNotExist(err), "a zero-finding run must create no store directory")
}

// TestPersistForReconcile_NilDiagDoesNotPanic covers the best-effort contract at the
// bridge itself: the MCP handler injects a writer, but a caller that omits one must
// not crash a reconcile over a diagnostics sink.
func TestPersistForReconcile_NilDiagDoesNotPanic(t *testing.T) {
	root := t.TempDir()

	assert.NotPanics(t, func() {
		PersistForReconcile("review", oneFindingResult("a.go", 1, "no sink"), PersistOpts{Root: root})
	})

	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, recs, 1, "an unset diag sink must not suppress the write itself")
}

// TestPersistForReconcile_SeedIsSuppressingOrOpenOnly re-asserts AC3(d) against the
// EXTRACTED bridge. Task 03's coupled decision — only suppressing-or-open ids are
// seeded, so a re-detected `resolved` id re-appends and returns to the open backlog —
// has to survive the move, and the move is exactly the kind of mechanical edit that
// silently widens a seed back to every id.
func TestPersistForReconcile_SeedIsSuppressingOrOpenOnly(t *testing.T) {
	root := t.TempDir()
	dir := DefaultDir(root)

	resolved := Record{
		SchemaVersion: SchemaVersion,
		RunID:         "2026-08-01T00:00:00Z-seed",
		Timestamp:     "2026-08-01T00:00:00Z",
		Severity:      "HIGH",
		File:          "a.go",
		Line:          1,
		Problem:       "leaks a handle",
		Status:        "resolved",
		ResolvedAt:    "2026-08-01T00:00:00Z",
	}
	resolved.StampID()
	require.NoError(t, Append(dir, resolved))

	dismissed := Record{
		SchemaVersion: SchemaVersion,
		RunID:         "2026-08-01T00:00:00Z-seed",
		Timestamp:     "2026-08-01T00:00:00Z",
		Severity:      "HIGH",
		File:          "b.go",
		Line:          2,
		Problem:       "false positive",
		Status:        "wontfix",
	}
	dismissed.StampID()
	require.NoError(t, Append(dir, dismissed))

	// Re-detect BOTH at their original locations (same file/line/problem → same id).
	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "leaks a handle", Fix: "fix it", Category: "correctness", EstMinutes: 10}},
			{Finding: reclib.Finding{Severity: "HIGH", File: "b.go", Line: 2, Problem: "false positive", Fix: "n/a", Category: "correctness", EstMinutes: 5}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-08-04T10:00:00Z"},
	}
	PersistForReconcile("review", res, PersistOpts{Root: root})

	byID := map[string]Record{}
	all, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	for _, r := range FoldRecords(all) {
		byID[r.ID] = r
	}

	assert.Equal(t, "", byID[resolved.ID].Status,
		"a re-detected resolved id must re-append as open — the regression is the signal")
	assert.Equal(t, "wontfix", byID[dismissed.ID].Status,
		"a wontfix id must stay suppressed; suppression is that status's entire purpose")
}

// TestPersistForReconcile_AutoCompactPolicyIsHonored proves PersistOpts.AutoCompact
// actually reaches MaybeCompact rather than being hard-coded during the extraction.
// Hard-coding CompactPolicy{} here would leave cli's autoCompactPolicy test seam
// inert while every cli test still passed, so the seam is pinned at the bridge.
func TestPersistForReconcile_AutoCompactPolicyIsHonored(t *testing.T) {
	root := t.TempDir()
	dir := DefaultDir(root)

	// Two records for one id: an open detection and its resolution. A compaction
	// folds them, which is observable in the diagnostics.
	for i, st := range []string{"", "resolved"} {
		rec := Record{
			SchemaVersion: SchemaVersion,
			RunID:         "2026-08-01T00:00:00Z-seed",
			Timestamp:     "2026-08-0" + string(rune('1'+i)) + "T00:00:00Z",
			Severity:      "HIGH",
			File:          "old.go",
			Line:          9,
			Problem:       "already handled",
			Status:        st,
		}
		if st != "" {
			rec.ResolvedAt = rec.Timestamp
		}
		rec.StampID()
		require.NoError(t, Append(dir, rec))
	}

	var diag bytes.Buffer
	// MaxRecords: 1 is far below the store's size, so an honored policy triggers.
	PersistForReconcile("review", oneFindingResult("new.go", 1, "fresh finding"),
		PersistOpts{Root: root, Diag: &diag, AutoCompact: CompactPolicy{MaxRecords: 1}})

	assert.Contains(t, diag.String(), "compacted",
		"a shrunken PersistOpts.AutoCompact must reach MaybeCompact; a hard-coded policy would never trigger here")
}

// TestPersistForReconcile_NoAppendSkipsAutoCompact is the mirror: a run that appends
// nothing must not pay for the threshold check at all, however aggressive the policy.
func TestPersistForReconcile_NoAppendSkipsAutoCompact(t *testing.T) {
	root := t.TempDir()

	var diag bytes.Buffer
	// Every finding is gate-excluded, so nothing is appended.
	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "out of scope", Fix: "n/a", Category: reclib.CategoryOutOfScope, EstMinutes: 5}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-08-04T10:00:00Z"},
	}
	PersistForReconcile("review", res, PersistOpts{Root: root, Diag: &diag, AutoCompact: CompactPolicy{MaxRecords: 1}})

	assert.NotContains(t, diag.String(), "compacted", "a no-append run must skip the threshold check entirely")
}

// TestResolveRecordModel covers Sprint 30.0 AC 01-02/01-03 and the Phase 1 gate
// fix: a single Record.Model can only be attributed when unambiguous. A
// single-reviewer or same-model merge resolves to that model; a cross-model
// merged finding (reviewers on 2+ distinct models) resolves to "" —
// attribution-incomplete — so the aggregation excludes it rather than
// mis-crediting one persona's model to another persona that never ran it.
//
// Moved here from cli/reconcile_test.go with the function itself (Plan 35.13 T6).
func TestResolveRecordModel(t *testing.T) {
	byRev := map[string]string{
		"security-reviewer": "claude-sonnet-4-6",
		"perf-reviewer":     "gpt-5.1",
		"style-reviewer":    "claude-sonnet-4-6",
	}
	cases := []struct {
		name      string
		reviewers []string
		want      string
	}{
		{"single reviewer", []string{"security-reviewer"}, "claude-sonnet-4-6"},
		{"two reviewers same model", []string{"security-reviewer", "style-reviewer"}, "claude-sonnet-4-6"},
		{"two reviewers different models excluded", []string{"security-reviewer", "perf-reviewer"}, ""},
		{"reviewer absent from map", []string{"unknown-reviewer"}, ""},
		{"empty reviewers", nil, ""},
		{"known + unknown resolves to known", []string{"unknown-reviewer", "perf-reviewer"}, "gpt-5.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveRecordModel(tc.reviewers, byRev))
		})
	}
}

// TestAttributableReviewers pins the narrowing half of record attribution: only the
// reviewers whose recorded model IS the record's resolved model stay on the record,
// in their original order. A sibling with no recorded model is dropped rather than
// credited under a model it never ran on.
func TestAttributableReviewers(t *testing.T) {
	byRev := map[string]string{
		"security-reviewer": "claude-sonnet-4-6",
		"style-reviewer":    "claude-sonnet-4-6",
		"perf-reviewer":     "gpt-5.1",
	}

	assert.Equal(t, []string{"security-reviewer", "style-reviewer"},
		attributableReviewers([]string{"security-reviewer", "unknown", "style-reviewer"}, byRev, "claude-sonnet-4-6"),
		"unrecorded reviewers are excluded and the surviving order is preserved")
	assert.Empty(t, attributableReviewers([]string{"perf-reviewer"}, byRev, "claude-sonnet-4-6"))
}

// TestPersistForReconcile_ManualEntryWinsIDCollision pins the current collision
// rule the Deduplication contract documents (doc.go): StampID hashes
// file/line/problem only — origin is deliberately excluded — so a manual
// `debt add` record and a reconcile finding at the same location/problem share
// one id, and write-time dedup silently drops whichever arrives SECOND. Here
// the manual entry arrives first and the finding is skipped: the manual record
// stands. Whether a review finding MAY supersede a manual entry is the open
// T2/T3 semantics question (sprint-plan TD-003) — this pins the status quo so
// the drop is deliberate, not accidental.
func TestPersistForReconcile_ManualEntryWinsIDCollision(t *testing.T) {
	root := t.TempDir()
	dir := DefaultDir(root)

	manual := Record{
		SchemaVersion: SchemaVersion,
		RunID:         "2026-08-01T00:00:00Z-manual",
		Timestamp:     "2026-08-01T00:00:00Z",
		Severity:      "MEDIUM",
		File:          "a.go",
		Line:          1,
		Problem:       "leaks a handle",
		Fix:           "manual fix",
		Category:      "correctness",
		Origin:        OriginManual,
	}
	manual.StampID()
	require.NoError(t, Append(dir, manual))

	// A reconcile finding at the SAME file/line/problem hashes to the same id.
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Root: root})

	all, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, all, 1,
		"the colliding reconcile finding must be dedup-dropped — the manual entry stands")
	assert.Equal(t, OriginManual, all[0].Origin)
	assert.Equal(t, "manual fix", all[0].Fix, "the surviving record is the manual one, fields intact")
}

// TestPersistForReconcile_EmptyRootIsNoPersist pins the bridge-side guard (TD
// internal/localdebt/reconcile.go:90): an empty opts.Root must never fall
// through to a CWD-relative .atcr/debt — the exact wrong-store write the
// security profile forbids. Both current callers gate on ResolveStoreRoot's ok,
// but the bridge is the single persistence implementation: the invariant must
// hold here, not only in caller-side checks a third caller could forget.
func TestPersistForReconcile_EmptyRootIsNoPersist(t *testing.T) {
	cwd := t.TempDir()
	chdir(t, cwd)

	var diag bytes.Buffer
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Diag: &diag})

	assert.Contains(t, diag.String(), "skipping local debt persistence",
		"an empty Root must warn, not silently write into whatever directory the process is in")
	_, err := os.Stat(filepath.Join(cwd, ".atcr"))
	assert.True(t, os.IsNotExist(err), "an empty Root must not create a CWD-relative store: %v", err)
}

// TestPersistForReconcile_VanishedRootIsNotResurrected pins the write-time half of
// root validation (TD internal/localdebt/root.go:123). ResolveStoreRoot stats the
// root ONCE and returns a string; everything after that is path-based, and both
// withLock and appendLocked called os.MkdirAll, which creates every missing parent
// INCLUDING the repo root. A root that passed validation and was then deleted or
// renamed was silently recreated as an empty tree at that absolute path, with the
// marker check never re-run — turning a stale-root stop signal into a write to a
// directory nobody asked for.
func TestPersistForReconcile_VanishedRootIsNotResurrected(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	chdir(t, t.TempDir())

	// Resolve while the root is valid, exactly as an entry point does...
	resolvedRoot, ok := ResolveStoreRoot(RootOpts{Explicit: root, AllowCWD: false})
	require.True(t, ok)

	// ...then the root goes away before the write lands.
	require.NoError(t, os.RemoveAll(root))

	var diag bytes.Buffer
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Root: resolvedRoot, Diag: &diag})

	_, err := os.Stat(root)
	assert.True(t, os.IsNotExist(err), "a vanished root must not be recreated by the write path: %v", err)
	assert.Contains(t, diag.String(), "skipping local debt persistence",
		"the vanished root must be reported, not silently swallowed")
}

// TestAppend_VanishedRootIsAnErrorNotAResurrection pins the same rule one layer
// down, where the MkdirAll chain actually lived: Append must create the store
// directory under an EXISTING root, never conjure the root itself.
func TestAppend_VanishedRootIsAnErrorNotAResurrection(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "gone-repo")

	err := Append(DefaultDir(root), Record{
		SchemaVersion: SchemaVersion,
		RunID:         "2026-08-04T10:00:00Z-review",
		ID:            "abc123",
		Timestamp:     "2026-08-04T10:00:00Z",
	})

	require.Error(t, err, "appending under a nonexistent root must fail, not create it")
	_, staterr := os.Stat(root)
	assert.True(t, os.IsNotExist(staterr), "the missing root must not be created: %v", staterr)
}

// TestPersistForReconcile_FailOpenWarningNamesDuplicateGrowth pins the REAL
// effect of the fail-open path (TD internal/localdebt/reconcile.go:112). The
// old warning claimed "previously dismissed/wontfix findings may be
// re-surfaced" — impossible: foldByID rule 1 selects a suppressing record
// unconditionally, so a re-appended open record never displaces a wontfix
// (pinned by TestFoldRecords_WontfixSurvivesFailOpenDuplicate below). The
// actual hazard is unbounded duplicate growth, and MaybeCompact cannot recover
// because readAllPreserving hits the same unreadable shard.
func TestPersistForReconcile_FailOpenWarningNamesDuplicateGrowth(t *testing.T) {
	root := t.TempDir()
	// A store path that is a regular FILE makes the dedup read fail (ENOTDIR) —
	// no permissions games, so the trigger holds under any test user.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atcr"), 0o755))
	require.NoError(t, os.WriteFile(DefaultDir(root), []byte("not a store"), 0o600))

	var diag bytes.Buffer
	PersistForReconcile("review", oneFindingResult("b.go", 2, "false positive"), PersistOpts{Root: root, Diag: &diag})

	warn := diag.String()
	assert.Contains(t, warn, "dedup read failed", "the fail-open path must still announce itself")
	assert.Contains(t, warn, "duplicate",
		"the warning must name the real effect: duplicate appends, unbounded until the read error is fixed")
	assert.NotContains(t, warn, "re-surfaced",
		"the dismissal claim is false — the fold's suppressing rule makes wontfix survive any re-detection")
}

// TestFoldRecords_WontfixSurvivesFailOpenDuplicate documents the seed's role as
// an OPTIMIZATION rather than a suppression mechanism: when a failed dedup read
// lets a wontfix id's re-detection append as a fresh open record, the fold
// still selects the wontfix — suppression was never the seed's to give.
func TestFoldRecords_WontfixSurvivesFailOpenDuplicate(t *testing.T) {
	wontfix := Record{File: "b.go", Line: 2, Problem: "false positive",
		Timestamp: "2026-08-01T00:00:00Z", Status: "wontfix"}
	wontfix.StampID()
	redetection := Record{File: "b.go", Line: 2, Problem: "false positive",
		Timestamp: "2026-08-04T10:00:00Z"}
	redetection.StampID()
	require.Equal(t, wontfix.ID, redetection.ID, "same file/line/problem must share an id for this scenario")

	folded := FoldRecords([]Record{wontfix, redetection})
	require.Len(t, folded, 1)
	assert.Equal(t, "wontfix", folded[0].Status,
		"a fail-open duplicate never displaces the wontfix record — suppression is the fold's job, not the seed's")
}

// TestPersistForReconcile_InvalidRunIDWarnsOnceAndWritesNothing pins call-site
// validation of the run_id month precondition (TD
// internal/localdebt/reconcile.go:129) — the same contract ManualRunID enforces
// for `debt add`. An empty ReconciledAt yields a run_id monthFromRunID rejects,
// and every finding shares that run_id, so without a pre-loop check EVERY
// Append rejects identically: N identical "append failed" lines, zero persisted
// records, and a clean return with no summary of what happened.
func TestPersistForReconcile_InvalidRunIDWarnsOnceAndWritesNothing(t *testing.T) {
	root := t.TempDir()

	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "p1", Fix: "f", Category: "correctness", EstMinutes: 5}},
			{Finding: reclib.Finding{Severity: "LOW", File: "b.go", Line: 2, Problem: "p2", Fix: "f", Category: "hygiene", EstMinutes: 5}},
		},
		Summary: reclib.Summary{ReconciledAt: ""},
	}
	var diag bytes.Buffer
	PersistForReconcile("review", res, PersistOpts{Root: root, Diag: &diag})

	warn := diag.String()
	assert.Contains(t, warn, "cannot derive month from run_id", "the single diagnostic must name the bad run_id")
	assert.Equal(t, 1, strings.Count(warn, "\n"),
		"exactly one warning line, not one identical append failure per finding")
	_, err := os.Stat(DefaultDir(root))
	assert.True(t, os.IsNotExist(err), "an unpersistable run must create no store directory: %v", err)
}

// writePoolSummary stamps a minimal sources/pool/summary.json fixture so the
// bridge's reviewer->model resolution has something to read.
func writePoolSummary(t *testing.T, reviewDir, jsonText string) {
	t.Helper()
	dir := filepath.Join(reviewDir, "sources", "pool")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "summary.json"), []byte(jsonText), 0o644))
}

// TestPersistForReconcile_PreservesFullReviewersWithModelSubset locks the
// clarified attribution contract (TD internal/localdebt/reconcile.go:173): the
// record keeps the finding's FULL reviewer list — the store is the only
// persistent copy, and cli/debt_resolve.go's resolve-time credit unions it —
// while the model-attributable subset lives on ModelReviewers. Narrowing
// Reviewers at write time used to destroy both.
func TestPersistForReconcile_PreservesFullReviewersWithModelSubset(t *testing.T) {
	root := t.TempDir()
	reviewDir := t.TempDir()
	writePoolSummary(t, reviewDir,
		`{"agents":[{"agent":"security-reviewer","model":"claude-sonnet-4-6"},{"agent":"style-reviewer"}],"total":2,"succeeded":2}`)

	res := reconcile.Result{
		Findings: []reclib.Merged{
			{Finding: reclib.Finding{
				Severity: "HIGH", File: "a.go", Line: 1, Problem: "leaks a handle",
				Fix: "fix it", Category: "correctness", EstMinutes: 10,
				Reviewers: []string{"security-reviewer", "style-reviewer"},
			}},
		},
		Summary: reclib.Summary{ReconciledAt: "2026-08-04T10:00:00Z"},
	}
	PersistForReconcile(reviewDir, res, PersistOpts{Root: root})

	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, []string{"security-reviewer", "style-reviewer"}, recs[0].Reviewers,
		"the persisted record retains every reviewer the finding carried — resolve-time credit recovers them from this list")
	assert.Equal(t, "claude-sonnet-4-6", recs[0].Model)
	assert.Equal(t, []string{"security-reviewer"}, recs[0].ModelReviewers,
		"the model-attributable subset is recorded WITHOUT narrowing Reviewers")
}

// gitRepoWithFiles commits files into a fresh git repo and returns its root, so
// RunReconcile's path validation has a tracked file set and its grouper has real
// source to anchor symbols against.
func gitRepoWithFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := gitexec.FixtureCommandContext(context.Background(), root, args...)
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	for rel, src := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(src), 0o644))
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return root
}

// TestPersistForReconcile_StoredIDEqualsStreamID pins Epic 35.16.11.3 T4: the id a
// finding carries on the wire (res.JSONFindings(), which findings.json emits) is
// the id of its .atcr/debt/ record. Both sides hash history.FindingID over the
// same post-anchor record, but each stamps it on its own (stampFindingIDs and
// Record.StampID), so nothing but this test keeps them from drifting.
//
// The fixture runs the real RunReconcile so the symbol-anchored and path-warned
// cases come from the pipeline, not from hand-built records. The anchored finding
// is the one that catches a divergence of INPUTS: persisting from res.Findings
// instead of res.JSONFindings() would hash the reviewer's bare problem. The
// duplicate is two records that survive merge as separate findings (they differ
// only in category) yet hash to one id, so they share one row. The out-of-scope,
// refuted and path-warned findings are still emitted with an id, and the store
// writes no row for it: the id is an address, not a promise of a stored row.
// The sibling's problem ends in the in-scope text and sorts (so is emitted) before
// it, so find matches the whole problem: a suffix match would return the sibling.
func TestPersistForReconcile_StoredIDEqualsStreamID(t *testing.T) {
	root := gitRepoWithFiles(t, map[string]string{
		"internal/tok/renew.go": "package tok\n\nfunc RenewToken() error {\n\tx := 1\n\t_ = x\n\treturn nil\n}\n",
	})
	reviewDir := t.TempDir()
	writeSource := func(rel, body string) {
		p := filepath.Join(reviewDir, "sources", filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("# atcr-findings/v1\n"+body), 0o644))
	}
	const (
		inScope    = "package tok has no doc comment"
		anchored   = "RenewToken drops the refresh error"
		outOfScope = "vendored import sits outside the diff"
		refuted    = "package tok exports no constructor"
		pathWarned = "renw.go never closes the token file"
		sibling    = "godoc: " + inScope
	)
	writeSource("greta/findings.txt",
		"LOW|internal/tok/renew.go:1|"+sibling+"|add one|style|2|ev|greta\n"+
			"LOW|internal/tok/renew.go:1|"+inScope+"|add one|style|2|ev|greta\n"+
			"LOW|internal/tok/renew.go:1|"+inScope+"|add one|maintainability|2|ev|greta\n"+
			"HIGH|internal/tok/renew.go:4|"+anchored+"|check it|correctness|20|ev|greta\n"+
			"LOW|internal/tok/renew.go:1|"+outOfScope+"|ignore|out-of-scope|2|ev|greta\n"+
			"MEDIUM|internal/tok/renew.go:1|"+refuted+"|add one|design|5|ev|greta\n"+
			"MEDIUM|internal/tok/renw.go:1|"+pathWarned+"|close it|correctness|5|ev|greta\n")

	res, err := reconcile.RunReconcile(context.Background(), reviewDir, nil, reconcile.Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	emitted := res.JSONFindings()
	require.Len(t, emitted, 7, "every fixture finding must survive merge as its own record")
	find := func(problem, category string) int {
		t.Helper()
		for i, f := range emitted {
			if f.Problem == problem && (category == "" || f.Category == category) {
				return i
			}
		}
		require.Failf(t, "fixture finding not emitted", "%q (%s)", problem, category)
		return -1
	}
	require.Equal(t, inScope, emitted[find(inScope, "style")].Problem,
		"find must not return the sibling whose problem merely ends in the wanted text")
	// Refutation is stamped by the verify stage on the very records
	// res.JSONFindings() returns, so the fixture sets it there too.
	emitted[find(refuted, "")].Verification = &reclib.Verification{Verdict: reclib.VerdictRefuted, Skeptic: "sam"}

	iAnchored := find("(RenewToken) "+anchored, "")
	require.Equal(t, "(RenewToken) "+anchored, emitted[iAnchored].Problem,
		"the fixture must anchor this finding, or it proves nothing about the inputs diverging")
	require.NotEmpty(t, emitted[find(pathWarned, "")].PathWarning,
		"the fixture must path-warn this finding")
	dupA, dupB := emitted[find(inScope, "style")], emitted[find(inScope, "maintainability")]
	require.Equal(t, dupA.ID, dupB.ID, "the duplicate pair must hash to one id")

	for _, f := range emitted {
		require.NotEmpty(t, f.ID, "every emitted finding carries an id: %+v", f)
		assert.Equal(t, history.FindingID(f.File, f.Line, f.Problem), f.ID,
			"the stream id is history.FindingID over the emitted (file, line, problem)")
	}

	PersistForReconcile(reviewDir, res, PersistOpts{Root: root})
	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)

	stored := map[string]Record{}
	for _, r := range recs {
		assert.Equal(t, history.FindingID(r.File, r.Line, r.Problem), r.ID,
			"the stored id is history.FindingID over the persisted (file, line, problem)")
		_, dup := stored[r.ID]
		assert.False(t, dup, "one row per id: %s", r.ID)
		stored[r.ID] = r
	}

	persisted := map[string]bool{
		emitted[find(inScope, "style")].ID: true,
		emitted[iAnchored].ID:              true,
		emitted[find(sibling, "")].ID:      true,
	}
	for _, f := range emitted {
		r, ok := stored[f.ID]
		if !persisted[f.ID] {
			assert.False(t, ok, "%q is emitted with id %s but must have no stored row", f.Problem, f.ID)
			continue
		}
		require.True(t, ok, "%q is emitted with id %s but no record carries it", f.Problem, f.ID)
		assert.Equal(t, f.File, r.File)
		assert.Equal(t, f.Line, r.Line)
		assert.Equal(t, f.Problem, r.Problem, "the stored row is the emitted finding, anchor included")
	}
	assert.Len(t, recs, len(persisted), "the store holds exactly the in-scope, sibling, anchored and deduplicated rows")
}

// discountedFindingResult is oneFindingResult with the panel-wide confidence
// discount reconcile records on a correlated invariant cluster (Epic 35.16.17).
func discountedFindingResult(file string, line int, problem string) reconcile.Result {
	res := oneFindingResult(file, line, problem)
	res.Findings[0].Category = reclib.CategoryInvariant
	res.Findings[0].Confidence = reclib.ConfMedium
	res.Findings[0].ConfidenceDiscount = reconcile.PanelWideReason
	return res
}

// Epic 35.16.17 T4: the discount reason reaches the TD store and the TD row, so a
// reader can tell a discounted MEDIUM from one a single reviewer earned.
func TestPersistForReconcile_CarriesTheConfidenceDiscountIntoTheTDRow(t *testing.T) {
	root := t.TempDir()
	PersistForReconcile("review", discountedFindingResult("key.go", 42, "equal() omits Name"), PersistOpts{Root: root})

	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, reclib.ConfMedium, recs[0].Confidence)
	assert.Equal(t, reconcile.PanelWideReason, recs[0].ConfidenceDiscount)
	assert.Equal(t, "equal() omits Name (confidence discounted: "+reconcile.PanelWideReason+")",
		recs[0].RowProblem(), "the TD row states the confidence was discounted and why")

	data := readStoreBytes(t, root)
	assert.Contains(t, data, `"confidence_discount":"panel-wide instruction: `)
}

// An undiscounted finding carries no confidence_discount key, so its JSONL line
// and its TD row are byte-identical to what they were before the field existed.
func TestPersistForReconcile_UndiscountedRecordIsByteIdentical(t *testing.T) {
	root := t.TempDir()
	PersistForReconcile("review", oneFindingResult("a.go", 1, "leaks a handle"), PersistOpts{Root: root})

	data := readStoreBytes(t, root)
	assert.NotContains(t, data, "confidence_discount")
	assert.Equal(t, `{"schema_version":3,"id":"`+history.FindingID("a.go", 1, "leaks a handle")+
		`","run_id":"2026-08-04T10:00:00Z-review","ts":"2026-08-04T10:00:00Z","severity":"HIGH","file":"a.go","line":1,`+
		`"problem":"leaks a handle","fix":"fix it","category":"correctness","est_minutes":10,"evidence":"","reviewers":null,"confidence":""}`+"\n",
		data, "the pre-35.16.17 wire shape, byte for byte")

	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "leaks a handle", recs[0].RowProblem())
}

// readStoreBytes returns the concatenated contents of every shard under root's
// store, so a test can assert on the exact JSONL a run wrote.
func readStoreBytes(t *testing.T, root string) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(DefaultDir(root), "*.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	var b strings.Builder
	for _, p := range paths {
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		b.Write(data)
	}
	return b.String()
}

// TestPersistForReconcile_SiblingWarningDoesNotDropFinding pins Epic 35.16.15: a
// sibling-not-found warning only clears the cross-branch marker, so a finding
// whose anchored File resolves is still persisted. A finding whose anchored File
// does not resolve is still dropped, with or without a sibling warning beside it.
// This bridge is the one both entry points call (the CLI reconcile and the MCP
// atcr_reconcile handler), so it covers both.
//
// The real RunReconcile stamps the anchored warnings. The sibling warning is
// added onto its cached records afterwards, the way the existing tests stamp a
// verdict, because no source parser stamps the marker for reconcile yet. Its text
// is the shape TestAnchoredPathWarning pins against validateFindingPaths.
func TestPersistForReconcile_SiblingWarningDoesNotDropFinding(t *testing.T) {
	root := gitRepoWithFiles(t, map[string]string{
		"internal/tok/renew.go": "package tok\n\nfunc RenewToken() error {\n\treturn nil\n}\n",
	})
	reviewDir := t.TempDir()
	p := filepath.Join(reviewDir, "sources", "greta", "findings.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	const (
		realBadSibling = "token cache is never invalidated"
		badBadSibling  = "token file handle leaks"
		badNoSibling   = "token file is world-readable"
		siblingWarning = "sibling file not found: internal/tok/invented.go:9"
	)
	require.NoError(t, os.WriteFile(p, []byte("# atcr-findings/v1\n"+
		"MEDIUM|internal/tok/renew.go:3|"+realBadSibling+"|invalidate it|correctness|5|ev|greta\n"+
		"MEDIUM|internal/tok/renw.go:3|"+badBadSibling+"|close it|correctness|5|ev|greta\n"+
		"MEDIUM|internal/tok/renw.go:4|"+badNoSibling+"|chmod it|security|5|ev|greta\n"), 0o644))

	res, err := reconcile.RunReconcile(context.Background(), reviewDir, nil, reconcile.Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	emitted := res.JSONFindings()
	require.Len(t, emitted, 3, "every fixture finding must survive merge as its own record")
	// The finding inside RenewToken is symbol-anchored, so its problem gains a
	// "(RenewToken) " prefix: match on the reviewer's text, and require all three.
	seen := 0
	for i := range emitted {
		seen++
		switch p := emitted[i].Problem; {
		case strings.HasSuffix(p, realBadSibling):
			require.Empty(t, emitted[i].PathWarning, "the fixture's anchored File must resolve")
			emitted[i].PathWarning = siblingWarning
		case strings.HasSuffix(p, badBadSibling):
			require.NotEmpty(t, emitted[i].PathWarning, "the fixture must path-warn the bad anchored File")
			emitted[i].PathWarning += "; " + siblingWarning
		case strings.HasSuffix(p, badNoSibling):
			require.NotEmpty(t, emitted[i].PathWarning, "the fixture must path-warn the bad anchored File")
		default:
			seen--
		}
	}
	require.Equal(t, 3, seen, "every fixture finding must be matched and stamped")

	PersistForReconcile(reviewDir, res, PersistOpts{Root: root})
	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)

	require.Len(t, recs, 1, "only the finding with a real anchored File may persist")
	assert.True(t, strings.HasSuffix(recs[0].Problem, realBadSibling), "persisted %q", recs[0].Problem)
	assert.Equal(t, "internal/tok/renew.go", recs[0].File)
	assert.Equal(t, 3, recs[0].Line)
}
