package fanout

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

// resolveScopeConstraint maps a ReviewRequest's SprintPlanPath to the formatted
// SCOPE CONSTRAINT block plus an optional stderr warning, covering the three
// dispositions in Epic 12.2: no plan (silent), unreadable (warn, proceed), and
// oversized (cap, warn).
func TestResolveScopeConstraint(t *testing.T) {
	// No plan: empty path → no constraint, no warning (AC2).
	if c, w := resolveScopeConstraint(ReviewRequest{}); c != "" || w != "" {
		t.Fatalf("empty path = (%q, %q), want (\"\", \"\")", c, w)
	}

	// Missing file is ignored silently (AC2).
	missing := filepath.Join(t.TempDir(), "nope.md")
	if c, w := resolveScopeConstraint(ReviewRequest{SprintPlanPath: missing}); c != "" || w != "" {
		t.Fatalf("missing file = (%q, %q), want (\"\", \"\")", c, w)
	}

	// Valid plan: constraint carries the block + content, no warning.
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Tasks\n- only auth changes\n"), 0o644))
	c, w := resolveScopeConstraint(ReviewRequest{SprintPlanPath: planPath})
	require.Contains(t, c, "SCOPE CONSTRAINT")
	require.Contains(t, c, "only auth changes")
	require.Empty(t, w, "a valid plan produces no warning")

	// Unreadable plan (a directory): no constraint, but a warning so the review
	// proceeds diff-wide without crashing (AC3).
	c, w = resolveScopeConstraint(ReviewRequest{SprintPlanPath: dir})
	require.Empty(t, c, "unreadable plan yields no constraint")
	require.NotEmpty(t, w, "unreadable plan must warn on stderr")

	// Oversized plan: capped constraint plus a truncation warning (AC6).
	big := filepath.Join(dir, "big.md")
	require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("x"), int(payload.MaxSprintPlanBytes)+1000), 0o644))
	c, w = resolveScopeConstraint(ReviewRequest{SprintPlanPath: big})
	require.Contains(t, c, "SCOPE CONSTRAINT")
	require.NotEmpty(t, w, "an oversized plan must warn that it was truncated")
}

// End-to-end: a ReviewRequest carrying a SprintPlanPath must make PrepareReviewFromDiff
// inject the SCOPE CONSTRAINT into every reviewer slot, immediately before the diff
// payload (Epic 12.2 AC4).
func TestPrepareReviewFromDiff_InjectsSprintPlanConstraint(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)
	planPath := filepath.Join(t.TempDir(), "sprint.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Sprint\n- only auth changes\n"), 0o644))
	req.SprintPlanPath = planPath

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.NoError(t, err)
	require.NotEmpty(t, prep.Slots)

	for _, s := range prep.Slots {
		p := s.Primary.Prompt
		require.Contains(t, p, "SCOPE CONSTRAINT", "every slot must carry the injected constraint")
		require.Contains(t, p, "only auth changes")
		ci := strings.Index(p, "SCOPE CONSTRAINT")
		di := strings.Index(p, "func total") // appears in looseDiff
		require.GreaterOrEqual(t, ci, 0)
		require.GreaterOrEqual(t, di, 0)
		require.Less(t, ci, di, "constraint must precede the diff payload")
	}

	// Without a plan, slots carry no constraint (diff-wide default).
	reqNoPlan := diffReq(t.TempDir(), filepath.Join(t.TempDir(), "ext-review-2"))
	prepNoPlan, err := PrepareReviewFromDiff(context.Background(), cfg, reqNoPlan, looseDiff)
	require.NoError(t, err)
	require.NotEmpty(t, prepNoPlan.Slots)
	require.NotContains(t, prepNoPlan.Slots[0].Primary.Prompt, "SCOPE CONSTRAINT")
}

// PrepareReviewFromDiff must route sprint-plan warnings through the contextual
// logger (not the global os.Stderr) so callers can capture them and so the
// warning is consistent with the diff-truncation warning.
func TestPrepareReviewFromDiff_LogsScopeWarningToContextLogger(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	big := filepath.Join(t.TempDir(), "big.md")
	require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("x"), int(payload.MaxSprintPlanBytes)+1000), 0o644))
	req.SprintPlanPath = big

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := log.NewContext(context.Background(), logger)

	prep, err := PrepareReviewFromDiff(ctx, cfg, req, looseDiff)
	require.NoError(t, err)
	require.NotEmpty(t, prep.Slots)

	logs := buf.String()
	require.Contains(t, logs, "scope constraint warning", "context logger should carry the warning")
	require.Contains(t, logs, "truncated", "warning should mention truncation")
}

// TestPrepareReviewFromDiff_ConstraintRespectsByteBudget asserts that the plan
// body embedded in the SCOPE CONSTRAINT is capped to budget/8 bytes when a
// PayloadByteBudget is configured, preventing the prepended constraint from
// inflating the rendered prompt well past the configured budget (Epic 12.2 AC4.2).
func TestPrepareReviewFromDiff_ConstraintRespectsByteBudget(t *testing.T) {
	const budget int64 = 4096
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = budget
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)
	planPath := filepath.Join(t.TempDir(), "sprint.md")
	// Plan exceeds MaxSprintPlanBytes so the existing cap fires first; the
	// budget/8 cap must then reduce it further to ≤ budget/8 bytes.
	require.NoError(t, os.WriteFile(planPath,
		bytes.Repeat([]byte("x"), int(payload.MaxSprintPlanBytes)+1000), 0o644))
	req.SprintPlanPath = planPath

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.NoError(t, err)
	require.NotEmpty(t, prep.Slots)

	const beginMark = "----- BEGIN SPRINT PLAN -----\n"
	const endMark = "\n----- END SPRINT PLAN -----"
	for _, s := range prep.Slots {
		p := s.Primary.Prompt
		require.Contains(t, p, "SCOPE CONSTRAINT")
		start := strings.Index(p, beginMark)
		end := strings.Index(p, endMark)
		require.GreaterOrEqual(t, start, 0, "prompt must contain BEGIN marker")
		require.GreaterOrEqual(t, end, 0, "prompt must contain END marker")
		embedded := p[start+len(beginMark) : end]
		require.LessOrEqual(t, len(embedded), int(budget/8),
			"scope constraint plan content must not exceed budget/8 bytes")
	}
}
