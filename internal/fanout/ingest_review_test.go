package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// looseDiff is a two-file loose unified diff (the suite-fixture format, no
// `diff --git` header) used across the ingestion tests.
const looseDiff = "--- a/pay.go\n" +
	"+++ b/pay.go\n" +
	"@@ -1,2 +1,2 @@\n" +
	" func total(items []*Item) int {\n" +
	"-\treturn items[0].Price\n" +
	"+\treturn sum(items)\n" +
	"--- a/query.go\n" +
	"+++ b/query.go\n" +
	"@@ -1,1 +1,1 @@\n" +
	"-safe\n" +
	"+unsafe\n"

// diffReq builds a range-less ReviewRequest writing to an explicit OutputDir, so
// the scaffold does not depend on a repo root having an .atcr tree.
func diffReq(root, out string) ReviewRequest {
	return ReviewRequest{
		Root:       root,
		OutputDir:  out,
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}
}

// PrepareReviewFromDiff must scaffold a runnable PreparedReview from a loose diff
// and force every agent to diff mode — even when the project's configured mode is
// "blocks" — writing a single payload/diff.txt and a manifest whose per-agent map
// records "diff" for all agents. The manifest's range fields are empty (no git
// range), and the payload content is the ingested diff verbatim.
func TestPrepareReviewFromDiff_ScaffoldsAndForcesDiffMode(t *testing.T) {
	cfg := twoAgentConfig("http://unused") // PayloadMode: "blocks"
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.NoError(t, err)
	require.NotNil(t, prep)
	assert.Equal(t, out, prep.Dir)
	assert.Equal(t, 2, prep.AgentCount(), "both roster agents become slots")

	// Only the diff payload artifact is written (no blocks/files).
	assert.FileExists(t, filepath.Join(out, "payload", "diff.txt"))
	assert.NoFileExists(t, filepath.Join(out, "payload", "blocks.txt"))
	diffBytes, err := os.ReadFile(filepath.Join(out, "payload", "diff.txt"))
	require.NoError(t, err)
	assert.Equal(t, looseDiff, string(diffBytes), "payload is the ingested diff verbatim")

	// Manifest: diff mode forced for every agent, range fields empty.
	mdata, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, "diff", m.PayloadMode)
	assert.Equal(t, "diff", m.PerAgentPayload["greta"], "configured blocks mode is overridden to diff")
	assert.Equal(t, "diff", m.PerAgentPayload["kai"])
	assert.Empty(t, m.Base, "a range-less diff has no base")
	assert.Empty(t, m.Head, "a range-less diff has no head")
	assert.ElementsMatch(t, []string{"greta", "kai"}, m.Roster)
}

// An empty diff is rejected before any scaffold, mapped to ErrNoReviewableContent.
func TestPrepareReviewFromDiff_EmptyDiffRejected(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, "   \n")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoReviewableContent)
	assert.NoDirExists(t, out, "no scaffold for an empty diff")
}

// A byte budget below every file's size drops all files and must surface
// ErrPayloadFullyDropped rather than forwarding an empty payload.
func TestPrepareReviewFromDiff_BudgetDropsAllErrors(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 1
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPayloadFullyDropped)
}

// A diff larger than payload.DefaultMaxDiffBytes is rejected before parsing or
// scaffolding, so a hostile multi-GB diff cannot exhaust memory at the exported
// ingestion boundary (the epic's MaxDiffBytes mitigation).
func TestPrepareReviewFromDiff_SizeCapRejectsOversized(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	oversized := strings.Repeat("a", int(payload.DefaultMaxDiffBytes)+1)
	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, oversized)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	assert.NoDirExists(t, out, "an oversized diff must not scaffold a review")
}

// An empty roster short-circuits before scaffolding, like PrepareReview.
func TestPrepareReviewFromDiff_EmptyRosterRejected(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project.Agents = nil
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, looseDiff)
	require.ErrorIs(t, err, ErrEmptyRoster)
}

// A malformed (non-diff) input must surface the primitive's parse error through
// the entry rather than scaffolding a vacuous review.
func TestPrepareReviewFromDiff_MalformedDiffPropagates(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	out := filepath.Join(t.TempDir(), "ext-review")
	req := diffReq(t.TempDir(), out)

	_, err := PrepareReviewFromDiff(context.Background(), cfg, req, "not a diff at all\n")
	require.Error(t, err)
	assert.NoDirExists(t, out, "a malformed diff must not scaffold a review")
}

// When the byte budget drops a NON-LAST file from a multi-file diff (independent
// review MEDIUM), the elision must be observable downstream: the agents' status
// records the dropped file, and the materialized payload contains only the kept
// files — the same truncation contract the git-range path honors.
func TestPrepareReviewFromDiff_MidFileBudgetDropSurfacesTruncation(t *testing.T) {
	// Three files; the MIDDLE one is by far the largest, so largest-first byte
	// budgeting drops it specifically.
	fileA := "--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	fileMid := "--- a/mid.go\n+++ b/mid.go\n@@ -1,1 +1,1 @@\n-" + strings.Repeat("x", 2000) + "\n+" + strings.Repeat("y", 2000) + "\n"
	fileZ := "--- a/z.go\n+++ b/z.go\n@@ -1,1 +1,1 @@\n-y\n+z\n"
	diff := fileA + fileMid + fileZ

	cfg := twoAgentConfig("http://unused")
	cfg.Settings.PayloadByteBudget = 256 // fits a.go + z.go, not mid.go
	root := t.TempDir()
	req := ReviewRequest{
		Root: root, Branch: "feature/test", Date: "2026-06-10",
		TimeSuffix: "120000", StartedAt: time.Unix(1000, 0).UTC(),
	}

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, diff)
	require.NoError(t, err)

	// The materialized payload contains only the kept files, mid.go elided.
	diffBytes, err := os.ReadFile(filepath.Join(prep.Dir, "payload", "diff.txt"))
	require.NoError(t, err)
	assert.Equal(t, fileA+fileZ, string(diffBytes), "dropped middle file must be elided from the payload")

	res, err := ExecuteReview(context.Background(), newFake(), prep)
	require.NoError(t, err)

	for _, agent := range []string{"greta", "kai"} {
		data, err := os.ReadFile(filepath.Join(res.Dir, "sources", "pool", "raw", "agent", agent, "status.json"))
		require.NoError(t, err)
		var st AgentStatus
		require.NoError(t, json.Unmarshal(data, &st))
		assert.True(t, st.Truncated, "agent %s must record truncation", agent)
		assert.Contains(t, st.FilesDropped, "mid.go", "agent %s must list the dropped middle file", agent)
	}
}

// readFixture loads a suite diff fixture relative to the fanout package dir.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "benchmark", "testdata", "suite-valid", name))
	require.NoError(t, err)
	return string(data)
}

// AC4 + AC5: the suite fixture case-01.diff, fed through PrepareReviewFromDiff on
// the default (non-output-dir) path, must scaffold a review whose diff payload is
// the fixture bytes verbatim, repoint .atcr/latest, and be accepted unchanged by
// ExecuteReview running against a stub Completer (no network) — proving an
// ingested diff reviews end to end on the production modePayload path.
func TestPrepareReviewFromDiff_FixtureEndToEnd(t *testing.T) {
	fixture := readFixture(t, "case-01.diff")
	cfg := twoAgentConfig("http://unused") // completer is the fake; URL unused
	root := t.TempDir()
	req := ReviewRequest{
		Root:       root,
		Range:      ReviewRange{}, // range-less: a standalone diff
		Branch:     "feature/test",
		Date:       "2026-06-10",
		TimeSuffix: "120000",
		StartedAt:  time.Unix(1000, 0).UTC(),
	}

	prep, err := PrepareReviewFromDiff(context.Background(), cfg, req, fixture)
	require.NoError(t, err)
	require.Equal(t, 2, prep.AgentCount())

	// The ingested diff is the payload, verbatim.
	diffBytes, err := os.ReadFile(filepath.Join(prep.Dir, "payload", "diff.txt"))
	require.NoError(t, err)
	assert.Equal(t, fixture, string(diffBytes), "AC4: ingested payload reproduces the fixture bytes")

	// ExecuteReview accepts the prepared review unchanged and fans out with no
	// network (the fake Completer succeeds for every agent).
	res, err := ExecuteReview(context.Background(), newFake(), prep)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 2, res.Summary.Succeeded, "AC5: both agents reviewed the ingested diff")
	assert.False(t, res.Summary.Partial)

	// .atcr/latest was repointed (default path, no --output-dir).
	latest, err := ReadLatest(root)
	require.NoError(t, err)
	assert.Equal(t, res.ID, latest)

	// Manifest records diff mode for every agent, with empty range provenance.
	mdata, err := os.ReadFile(filepath.Join(prep.Dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(mdata, &m))
	assert.Equal(t, "diff", m.PayloadMode)
	assert.Empty(t, m.Base)
	assert.Empty(t, m.Head)
}
