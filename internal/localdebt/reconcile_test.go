package localdebt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reclib "github.com/samestrin/atcr/reconcile"

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
