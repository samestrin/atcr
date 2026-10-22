package fanout

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveScopeConstraint maps a ReviewRequest's SprintPlanPath to the formatted
// SCOPE CONSTRAINT block plus an optional stderr warning, covering the three
// dispositions in Epic 12.2: no plan (silent), unreadable (warn, proceed), and
// oversized (cap, warn).
func TestResolveScopeConstraint(t *testing.T) {
	// No plan: empty path → no constraint, no warning (AC2).
	if c, w := resolveScopeConstraint(ReviewRequest{}, registry.DefaultMaxSprintPlanBytes); c != "" || w != "" {
		t.Fatalf("empty path = (%q, %q), want (\"\", \"\")", c, w)
	}

	// Missing file is ignored silently (AC2).
	missing := filepath.Join(t.TempDir(), "nope.md")
	if c, w := resolveScopeConstraint(ReviewRequest{SprintPlanPath: missing}, registry.DefaultMaxSprintPlanBytes); c != "" || w != "" {
		t.Fatalf("missing file = (%q, %q), want (\"\", \"\")", c, w)
	}

	// Valid plan: constraint carries the block + content, no warning.
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Tasks\n- only auth changes\n"), 0o644))
	c, w := resolveScopeConstraint(ReviewRequest{SprintPlanPath: planPath}, registry.DefaultMaxSprintPlanBytes)
	require.Contains(t, c, "SCOPE CONSTRAINT")
	require.Contains(t, c, "only auth changes")
	require.Empty(t, w, "a valid plan produces no warning")

	// Unreadable plan (a directory): no constraint, but a warning so the review
	// proceeds diff-wide without crashing (AC3).
	c, w = resolveScopeConstraint(ReviewRequest{SprintPlanPath: dir}, registry.DefaultMaxSprintPlanBytes)
	require.Empty(t, c, "unreadable plan yields no constraint")
	require.NotEmpty(t, w, "unreadable plan must warn on stderr")

	// Oversized plan: capped constraint plus a truncation warning (AC6).
	big := filepath.Join(dir, "big.md")
	require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("x"), int(registry.DefaultMaxSprintPlanBytes)+1000), 0o644))
	c, w = resolveScopeConstraint(ReviewRequest{SprintPlanPath: big}, registry.DefaultMaxSprintPlanBytes)
	require.Contains(t, c, "SCOPE CONSTRAINT")
	require.NotEmpty(t, w, "an oversized plan must warn that it was truncated")
	// Tightened: verify the embedded plan length is exactly at the byte cap.
	// ASCII-only input means no rune-boundary adjustment, so the count is exact.
	beginIdx := strings.Index(c, "----- BEGIN SPRINT PLAN -----\n")
	endIdx := strings.Index(c, "\n----- END SPRINT PLAN -----")
	require.GreaterOrEqual(t, beginIdx, 0, "constraint must have BEGIN marker")
	require.GreaterOrEqual(t, endIdx, 0, "constraint must have END marker")
	embeddedLen := endIdx - (beginIdx + len("----- BEGIN SPRINT PLAN -----\n"))
	require.Equal(t, int(registry.DefaultMaxSprintPlanBytes), embeddedLen,
		"embedded plan must be exactly MaxSprintPlanBytes bytes for ASCII-only input")
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
	require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("x"), int(registry.DefaultMaxSprintPlanBytes)+1000), 0o644))
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
		bytes.Repeat([]byte("x"), int(registry.DefaultMaxSprintPlanBytes)+1000), 0o644))
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

// TestPrepareReviewFromDiff_WritesConstraintArtifact asserts that
// PrepareReviewFromDiff writes the resolved SCOPE CONSTRAINT block to
// payload/scope-constraint.txt so the on-disk artifact reflects what each
// reviewer actually received (Epic 12.2 AC5 provenance).
func TestPrepareReviewFromDiff_WritesConstraintArtifact(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)
	planPath := filepath.Join(t.TempDir(), "sprint.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Sprint\n- only auth changes\n"), 0o644))
	req.SprintPlanPath = planPath

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(prep.Dir, "payload", "scope-constraint.txt"))
	require.NoError(t, err, "payload/scope-constraint.txt must be written for a scoped review")
	require.Contains(t, string(data), "SCOPE CONSTRAINT")
	require.Contains(t, string(data), "only auth changes")
}

// TestPrepareResume_PreservesSprintPlanConstraint asserts that PrepareResume
// re-injects the SCOPE CONSTRAINT into every resumed slot so reviewers that
// pick up from an interrupted run still receive the sprint plan scope
// (Epic 12.2 AC4.3 / resume.go regression: line 287 passed "" for scopeConstraint).
func TestPrepareResume_PreservesSprintPlanConstraint(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")
	planPath := filepath.Join(t.TempDir(), "sprint.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Sprint\n- only auth changes\n"), 0o644))
	req := reviewReq(repo, repo, base, head)
	req.SprintPlanPath = planPath

	prep, err := PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, req)
	require.NoError(t, err)
	require.NotEmpty(t, rprep.Slots)

	for _, s := range rprep.Slots {
		require.Contains(t, s.Primary.Prompt, "SCOPE CONSTRAINT",
			"resumed slot must carry the injected scope constraint")
		require.Contains(t, s.Primary.Prompt, "only auth changes")
	}
}

// TestPrepareResume_RecoversScopeFromArtifact asserts that PrepareResume recovers
// the SCOPE CONSTRAINT from the persisted payload/scope-constraint.txt artifact —
// NOT from req.SprintPlanPath — so a resume launched by `atcr review --resume`
// (whose ReviewRequest carries no SprintPlanPath) still injects the same scope the
// completed agents reviewed under. This is the production regression the inert
// resolveScopeConstraint(req) call masked: the resume entry point (cmd/atcr/
// resume.go) never threads SprintPlanPath, so scope must be locked from on-disk
// review state like the range and roster (Epic 12.2 AC4.3).
func TestPrepareResume_RecoversScopeFromArtifact(t *testing.T) {
	repo, base, head := initRepo(t)
	cfg := twoAgentConfig("http://unused")
	planPath := filepath.Join(t.TempDir(), "sprint.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Sprint\n- only auth changes\n"), 0o644))

	scopedReq := reviewReq(repo, repo, base, head)
	scopedReq.SprintPlanPath = planPath
	prep, err := PrepareReview(context.Background(), cfg, scopedReq)
	require.NoError(t, err)

	// The resume entry point builds its ReviewRequest without a SprintPlanPath, so
	// the scope can only come from the persisted artifact.
	resumeReq := reviewReq(repo, repo, base, head)
	require.Empty(t, resumeReq.SprintPlanPath, "resume request must not carry a sprint plan path")

	rprep, _, err := PrepareResume(context.Background(), cfg, prep.Dir, resumeReq)
	require.NoError(t, err)
	require.NotEmpty(t, rprep.Slots)

	for _, s := range rprep.Slots {
		require.Contains(t, s.Primary.Prompt, "SCOPE CONSTRAINT",
			"resumed slot must recover the scope constraint from the artifact")
		require.Contains(t, s.Primary.Prompt, "only auth changes")
	}
}

// The artifact is written from a SECOND, uncapped resolveScopeConstraint call,
// independent of the run-level cap buildSlots applies to the block it actually
// ships — so "the on-disk artifact reflects what each reviewer received" is false
// exactly where the run-level cap does any work. With payload_byte_budget set, the
// plan body every reviewer receives is capped to budget/8 (the sibling test above
// pins that on the prompt), while the artifact keeps the full untruncated plan.
//
// Asserted as a relation to the shipped prompt, not against a literal, so the two
// cannot drift apart in either direction.
func TestPrepareReviewFromDiff_ConstraintArtifactMatchesWhatShipped(t *testing.T) {
	const budget int64 = 4096
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = budget
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)
	planPath := filepath.Join(t.TempDir(), "sprint.md")
	require.NoError(t, os.WriteFile(planPath,
		bytes.Repeat([]byte("x"), int(registry.DefaultMaxSprintPlanBytes)+1000), 0o644))
	req.SprintPlanPath = planPath

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.NoError(t, err)
	require.NotEmpty(t, prep.Slots)

	data, err := os.ReadFile(filepath.Join(prep.Dir, "payload", "scope-constraint.txt"))
	require.NoError(t, err, "a scoped review still writes the artifact")

	const beginMark = "----- BEGIN SPRINT PLAN -----\n"
	const endMark = "\n----- END SPRINT PLAN -----"
	planBody := func(s string) string {
		start := strings.Index(s, beginMark)
		end := strings.Index(s, endMark)
		require.GreaterOrEqual(t, start, 0)
		require.GreaterOrEqual(t, end, 0)
		return s[start+len(beginMark) : end]
	}

	shipped := planBody(prep.Slots[0].Primary.Prompt)
	require.LessOrEqual(t, len(shipped), int(budget/8),
		"precondition: the run-level cap really did truncate what the reviewers received")

	assert.Equal(t, shipped, planBody(string(data)),
		"the artifact documents what each reviewer received, so it must carry the block that was actually sent — not the uncapped one no reviewer saw")
}

// The SCOPE CONSTRAINT contract runs both ways (Epic 36.2 AC2): besides
// suppressing findings unrelated to the plan, it tells the reviewer to report
// each plan work item the diff does not implement, under the marker and
// category the contract names, as a file-level finding on a changed file so the
// grounding gate keeps it.
func TestResolveScopeConstraint_ReportsUnmetPlanItems(t *testing.T) {
	planPath := filepath.Join(t.TempDir(), "plan.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Tasks\n- add rate limiting to login\n"), 0o644))

	c, w := resolveScopeConstraint(ReviewRequest{SprintPlanPath: planPath}, registry.DefaultMaxSprintPlanBytes)
	require.Empty(t, w)
	assert.Contains(t, c, "Suppress findings for unrelated changes", "the suppression half of the contract is kept")
	assert.Contains(t, c, payload.UnmetPlanItemPrefix, "the contract names the unmet-plan-item marker")
	assert.Contains(t, c, "CATEGORY `"+payload.UnmetPlanItemCategory+"`", "the contract names the unmet-plan-item category")
	assert.Contains(t, c, "does not implement", "the contract asks for plan items the diff does not implement")
	assert.Contains(t, c, "file-level", "the contract anchors the finding file-level on a changed file so grounding keeps it")
}

// A missing or empty sprint-plan file is still ignored (Epic 36.2 AC3): no
// constraint, no warning, and the review prepares diff-wide with no error and
// no unmet-plan-item instruction for the reviewer to act on.
func TestSprintPlan_EmptyOrMissingPlanIgnored(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.md")
	require.NoError(t, os.WriteFile(empty, nil, 0o644))
	blank := filepath.Join(dir, "blank.md")
	require.NoError(t, os.WriteFile(blank, []byte(" \n\t\n"), 0o644))
	missing := filepath.Join(dir, "missing.md")

	for _, p := range []string{empty, blank, missing} {
		c, w := resolveScopeConstraint(ReviewRequest{SprintPlanPath: p}, registry.DefaultMaxSprintPlanBytes)
		require.Emptyf(t, c, "%s: no constraint", filepath.Base(p))
		require.Emptyf(t, w, "%s: no warning", filepath.Base(p))

		cfg := twoAgentConfig("http://unused")
		req := diffReq(t.TempDir(), filepath.Join(t.TempDir(), "ext-review"))
		req.SprintPlanPath = p
		prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
		require.NoErrorf(t, err, "%s: the review proceeds", filepath.Base(p))
		require.NotEmpty(t, prep.Slots)
		for _, s := range prep.Slots {
			assert.NotContainsf(t, s.Primary.Prompt, "SCOPE CONSTRAINT", "%s: diff-wide review", filepath.Base(p))
			assert.NotContainsf(t, s.Primary.Prompt, payload.UnmetPlanItemPrefix, "%s: no unmet-plan-item instruction", filepath.Base(p))
		}
	}
}

// planFollowingCompleter stands in for a reviewer that follows the SCOPE
// CONSTRAINT contract it is given: when the prompt asks for unmet plan items it
// reports the one plan item the fixture diff does not implement, exactly as the
// contract shapes it; otherwise it reports nothing.
type planFollowingCompleter struct{ item string }

func (c planFollowingCompleter) Complete(_ context.Context, inv llmclient.Invocation) (string, error) {
	if !strings.Contains(inv.Prompt, payload.UnmetPlanItemPrefix) {
		return "NO FINDINGS", nil
	}
	return "MEDIUM|auth.go|" + payload.UnmetPlanItemPrefix + " " + c.item +
		" — the diff never touches login|Implement the plan item or drop it from the plan|" +
		payload.UnmetPlanItemCategory + "|30|no rate limiter appears in the change", nil
}

// `atcr review --sprint-plan <path>` on a diff that does not implement a plan
// work item produces an unmet-plan-item finding naming the item (Epic 36.2 AC2).
// The run is a real git-range review, so the finding must also clear the
// grounding gate to reach the agent's findings file.
func TestRunReview_SprintPlanReportsUnmetPlanItem(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	resetBreakers(t)
	repo, base, head := initRepo(t) // the diff wires b() into a() in auth.go
	const unmet = "Add rate limiting to login"
	planPath := filepath.Join(t.TempDir(), "sprint.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Tasks\n- Wire b() into a()\n- "+unmet+"\n"), 0o644))
	req := reviewReq(repo, repo, base, head)
	req.SprintPlanPath = planPath

	res, err := RunReview(context.Background(), planFollowingCompleter{item: unmet}, twoAgentConfig("http://unused"), req)
	require.NoError(t, err)
	require.Equal(t, 2, res.Summary.Succeeded)

	for _, agent := range []string{"greta", "kai"} {
		data, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", agent, "findings.txt"))
		require.NoErrorf(t, err, "missing %s findings", agent)
		parsed, err := stream.ParseSource(data)
		require.NoError(t, err)
		require.Lenf(t, parsed.Findings, 1, "%s: the unmet-plan-item finding survives grounding", agent)
		f := parsed.Findings[0]
		assert.Contains(t, f.Problem, payload.UnmetPlanItemPrefix)
		assert.Contains(t, f.Problem, unmet, "the finding names the unmet plan item")
		assert.Equal(t, payload.UnmetPlanItemCategory, f.Category)
	}
}
