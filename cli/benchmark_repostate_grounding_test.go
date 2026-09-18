package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MOCK FIDELITY. The mini fixture is structurally EASIER than every shipped case,
// and its own README admits the shortcut: it puts its `outside_diff: true`
// expectation in app/calc.py — the same file the diff changes — while all four
// shipped cases put theirs in a file the diff NEVER touches.
//
// That difference decides whether the epic 14.1 grounding gate runs at all. A
// finding in a CHANGED file reaches the permissive arms (file-level, tolerance,
// evidence match); a finding in an UNTOUCHED file is dropped outright unless
// pre-fetching retrieved the cited span, which is the PrefetchOnly arm at
// internal/fanout/grounding.go:84-86. So every CLI test scored 1.0 out-of-diff on
// a shape that cannot occur in the real suite, and the shipped suite's headline
// metric could be structurally 0 for all reviewers with the whole tree green.
//
// This fixture restores the shipped shape: helper.py is in the base tree, the diff
// never touches it, and the out-of-diff expectation lives there.
func writeUntouchedFileSuite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "untouched-case")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "base", "app"), 0o755))

	// calc.py is the file the diff changes.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base", "app", "calc.py"),
		[]byte("def total(items):\n    s = 0\n    for i in items:\n        s += i\n    return s\n"), 0o600))
	// helper.py is NEVER touched by the diff, and carries the planted defect the
	// out-of-diff expectation names.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "base", "app", "helper.py"),
		[]byte("def average(items):\n    return total(items) / len(items)\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "commit-message.txt"),
		[]byte("feat: add safe_total\n\nBody.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "change.diff"), []byte(
		"diff --git a/app/calc.py b/app/calc.py\n"+
			"index 1111111..2222222 100644\n"+
			"--- a/app/calc.py\n"+
			"+++ b/app/calc.py\n"+
			"@@ -3,3 +3,6 @@ def total(items):\n"+
			"     for i in items:\n"+
			"         s += i\n"+
			"     return s\n"+
			"+\n"+
			"+def safe_total(items):\n"+
			"+    return total(items or [])\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "case.json"), []byte(`{
  "id": "untouched-case",
  "format": "repo-state-v1",
  "base_tree": "base",
  "commit_message": "commit-message.txt",
  "diff": "change.diff",
  "expected_findings": [
    {"id": "helper-divides-by-zero", "file": "app/helper.py", "line_start": 2, "line_end": 2,
     "line_tolerance": 3, "outside_diff": true, "category": "correctness",
     "summary": "average() divides by len(items) with no empty-list guard, in a file the change never touches."},
    {"id": "safe-total-not-none-safe", "file": "app/calc.py", "line_start": 6, "line_end": 6,
     "line_tolerance": 3, "outside_diff": false, "category": "correctness",
     "summary": "safe_total() is settled by an added line."}
  ]
}`), 0o600))

	require.NoError(t, os.WriteFile(filepath.Join(root, "suite.json"),
		[]byte(`{"suite":"repo-state-v1","suite_version":"1.0.0","cases":[{"id":"untouched-case","dir":"untouched-case"}]}`), 0o600))
	return root
}

// stubUntouchedFileCompleter cites BOTH defects, including the one in the file the
// diff never touches. A reviewer that found everything is the only input that can
// distinguish "the gate dropped it" from "the reviewer missed it".
type stubUntouchedFileCompleter struct{}

func (stubUntouchedFileCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return "HIGH|app/helper.py:2|average divides by len(items) with no empty guard|add a guard|correctness|15|return total(items) / len(items)\n" +
		"MEDIUM|app/calc.py:6|safe_total is not None-safe as claimed|handle None explicitly|correctness|15|def safe_total(items):", nil
}

// The pin the row asks for: what ACTUALLY happens when a reviewer correctly cites a
// defect in a file the patch never touched, with pre-fetching not in play.
//
// The answer is that the grounding gate DROPS it, so outside_diff_recall is 0.0
// even though the reviewer found the defect and cited it exactly. That is the
// measurement, not a bug — the tier exists to establish whether pre-fetching lets a
// genuine out-of-diff finding clear the shipped anti-hallucination gate, and this
// is the baseline it is measured against.
//
// Pinned so the gate interaction stops being an untested assumption. If a future
// change makes an untouched-file citation survive by default, this test fails and
// whoever made it has to say so deliberately.
func TestExecuteRepoStateBenchmarkRun_UntouchedFileFindingIsDroppedByTheGroundingGate(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubUntouchedFileCompleter{},
		writeUntouchedFileSuite(t), gen)
	require.NoError(t, err)

	require.Len(t, rr.PositionalRecall, 1)
	p := rr.PositionalRecall[0]

	require.NotNil(t, p.WithinDiffRecall)
	assert.InDelta(t, 1.0, *p.WithinDiffRecall, 1e-9,
		"the in-diff citation lands on an added line and survives the gate")

	require.NotNil(t, p.OutsideDiffRecall)
	assert.InDelta(t, 0.0, *p.OutsideDiffRecall, 1e-9,
		"a correct citation in a file the patch never touched is DISCARDED before scoring "+
			"(internal/fanout/grounding.go: file not in the patch -> ungrounded), so the reviewer "+
			"scores 0 on the half this tier exists to measure — this is the baseline pre-fetching "+
			"has to beat, and the mini fixture could not express it")
}

// The complementary half, and the reason the assertion above means something: the
// SAME citation in the SAME place scores 1.0 when the file IS in the patch. Without
// this, a 0.0 above would be equally consistent with the stub being ignored
// entirely, or with positional matching being broken for helper.py's line numbers.
func TestExecuteRepoStateBenchmarkRun_TheSameCitationScoresWhenItsFileIsInThePatch(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})
	gen := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	rr, err := executeRepoStateBenchmarkRun(context.Background(), cfg, stubLocatedCompleter{},
		repoStateMiniPath, gen)
	require.NoError(t, err)

	require.Len(t, rr.PositionalRecall, 1)
	require.NotNil(t, rr.PositionalRecall[0].OutsideDiffRecall)
	assert.InDelta(t, 1.0, *rr.PositionalRecall[0].OutsideDiffRecall, 1e-9,
		"the mini fixture's out-of-diff expectation sits in the CHANGED file, so it clears the "+
			"gate on a permissive arm — which is exactly why it cannot measure the shipped shape")
}
