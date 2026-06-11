package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlugifyBranch(t *testing.T) {
	cases := map[string]string{
		"feature/JIRA-123-add-auth": "JIRA-123-add-auth",
		"fix/bug":                   "bug",
		"feature/1.0_atcr_core":     "1.0_atcr_core",
		"feature/a//b  c":           "a-b-c",
		"main":                      "main",
		"!!!":                       "",
		"  feature/x  ":             "x",
	}
	for in, want := range cases {
		assert.Equal(t, want, slugifyBranch(in), "slug of %q", in)
	}
}

func TestReviewID_DefaultScheme(t *testing.T) {
	id, err := ReviewID("", "feature/add-auth", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_add-auth", id)
}

func TestReviewID_DetachedAndEmptySlugFallbacks(t *testing.T) {
	id, err := ReviewID("", "", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_detached", id, "empty branch (detached HEAD) → detached")

	id, err = ReviewID("", "feature/!!!", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_review", id, "branch that sanitizes to nothing → review")
}

func TestReviewID_CollisionAppendsSuffix(t *testing.T) {
	exists := func(id string) bool { return id == "2026-06-10_add-auth" }
	id, err := ReviewID("", "feature/add-auth", "2026-06-10", "143022", exists)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_add-auth-143022", id)
}

func TestReviewID_OverrideWinsVerbatim(t *testing.T) {
	id, err := ReviewID("custom-review-id", "feature/ignored", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "custom-review-id", id)
}

func TestReviewID_RejectsTraversalOverride(t *testing.T) {
	// Includes ".", leading-dash (flag injection), and Windows separators.
	for _, bad := range []string{"../escape", "a/b", "..", ".", "/abs", `a\b`, "-rf", "--id"} {
		_, err := ReviewID(bad, "feature/x", "2026-06-10", "143022", nil)
		require.Error(t, err, "override %q must be rejected", bad)
		assert.Contains(t, err.Error(), "invalid review id")
	}
}

func TestReviewID_DotSlugBranchFallsBackToReview(t *testing.T) {
	// A branch that slugifies to dots must not produce an unsafe component.
	id, err := ReviewID("", "feature/..", "2026-06-10", "143022", nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_review", id)
}

func TestReviewID_CollisionReprobeAvoidsClobber(t *testing.T) {
	// Both the base id AND the first suffixed id already exist → counter appended.
	taken := map[string]bool{
		"2026-06-10_x":        true,
		"2026-06-10_x-143022": true,
	}
	id, err := ReviewID("", "feature/x", "2026-06-10", "143022", func(s string) bool { return taken[s] })
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_x-143022-2", id)
}

func TestScaffoldReviewDir_CreatesLayout(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_feature")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, ".atcr", "reviews", "2026-06-10_feature"), dir)
	for _, sub := range []string{"payload", "sources", "reconciled"} {
		assert.DirExists(t, filepath.Join(dir, sub))
	}
	// Per-agent dir is NOT scaffolded here (fan-out creates it later).
	assert.NoDirExists(t, filepath.Join(dir, "sources", "pool"))
}

func TestReviewExists_AndCollisionProbe(t *testing.T) {
	root := t.TempDir()
	assert.False(t, ReviewExists(root, "2026-06-10_x"))
	_, err := ScaffoldReviewDir(root, "2026-06-10_x")
	require.NoError(t, err)
	assert.True(t, ReviewExists(root, "2026-06-10_x"))
}

// Two concurrent claims of the same derived id must land in distinct
// directories: directory creation itself is the atomic claim, so the
// Stat-probe TOCTOU window (both probes pass, both scaffold one dir) is gone.
func TestClaimReviewDir_ConcurrentSameIDGetDistinctDirs(t *testing.T) {
	root := t.TempDir()
	const id = "2026-06-10_race"

	type claim struct {
		id  string
		dir string
		err error
	}
	results := make(chan claim, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			cid, dir, err := claimReviewDir(root, id, "143022")
			results <- claim{cid, dir, err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	assert.NotEqual(t, a.dir, b.dir, "concurrent claims of one id must yield distinct review dirs")
	for _, c := range []claim{a, b} {
		for _, sub := range []string{"payload", "sources", "reconciled"} {
			assert.DirExists(t, filepath.Join(c.dir, sub))
		}
	}
}

// Sequential claims follow the same candidate sequence the probe-based
// resolver used: base id, then id-suffix, then id-suffix-2.
func TestClaimReviewDir_SecondClaimAppendsSuffix(t *testing.T) {
	root := t.TempDir()
	id1, _, err := claimReviewDir(root, "2026-06-10_x", "143022")
	require.NoError(t, err)
	id2, _, err := claimReviewDir(root, "2026-06-10_x", "143022")
	require.NoError(t, err)
	id3, _, err := claimReviewDir(root, "2026-06-10_x", "143022")
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_x", id1)
	assert.Equal(t, "2026-06-10_x-143022", id2)
	assert.Equal(t, "2026-06-10_x-143022-2", id3)
}

func TestWriteAndReadLatest(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, WriteLatest(root, "2026-06-10_feature"))

	data, err := os.ReadFile(filepath.Join(root, ".atcr", "latest"))
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_feature\n", string(data))

	got, err := ReadLatest(root)
	require.NoError(t, err)
	assert.Equal(t, "2026-06-10_feature", got)
}

// A WriteManifest failure after WritePool leaves manifest.json saying
// partial:false while summary.json (the completion signal) says true.
// ReadManifestPartial must agree with the summary — the same source of truth
// ReadReviewStatus uses — not the stale manifest.
func TestReadManifestPartial_SummaryIsSourceOfTruth(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_partial")
	require.NoError(t, err)

	// Stale manifest: finalization failed, Partial still false.
	require.NoError(t, WriteManifest(dir, &payload.Manifest{Partial: false}))

	// Completed summary: the run was partial.
	poolDir := filepath.Join(dir, "sources", "pool")
	require.NoError(t, os.MkdirAll(poolDir, 0o755))
	sum, err := json.Marshal(PoolSummary{Total: 2, Succeeded: 1, Failed: 1, Partial: true})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(poolDir, "summary.json"), sum, 0o644))

	assert.True(t, ReadManifestPartial(dir), "summary.json says partial:true; stale manifest must not override it")
}

// Without a summary (fan-out still running, or a hand-assembled review) the
// manifest remains the only available signal and is still honored.
func TestReadManifestPartial_FallsBackToManifestWhenNoSummary(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_fallback")
	require.NoError(t, err)
	require.NoError(t, WriteManifest(dir, &payload.Manifest{Partial: true}))
	assert.True(t, ReadManifestPartial(dir))
}

func TestWriteManifest_AtReviewRootWithFields(t *testing.T) {
	root := t.TempDir()
	dir, err := ScaffoldReviewDir(root, "2026-06-10_feature")
	require.NoError(t, err)

	m := &payload.Manifest{
		Base:            "abc123",
		Head:            "def456",
		DetectionMode:   "explicit",
		CommitCount:     3,
		PayloadMode:     "blocks",
		PerAgentPayload: map[string]string{"greta": "blocks", "kai": "diff"},
		Roster:          []string{"greta", "kai"},
		StartedAt:       time.Unix(1000, 0).UTC(),
		Partial:         false,
	}
	require.NoError(t, WriteManifest(dir, m))

	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var got payload.Manifest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "abc123", got.Base)
	assert.Equal(t, "def456", got.Head)
	assert.Equal(t, "explicit", got.DetectionMode)
	assert.Equal(t, "blocks", got.PerAgentPayload["greta"])
	assert.Equal(t, []string{"greta", "kai"}, got.Roster)
	assert.False(t, got.Partial)
}
