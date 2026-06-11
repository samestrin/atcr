package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grayPair returns two PROBLEM texts whose token-set Jaccard lands in the gray
// zone [0.4, 0.7): inter 4 / union 7 ≈ 0.571.
const (
	problemA = "null pointer dereference in handler"
	problemB = "null pointer crash in handler code"
)

// writeGrayReview lays down a review dir with a host finding and a pool finding
// at the same location whose problems are gray-zone similar, so reconcile
// records exactly one ambiguous cluster. Returns the review dir.
func writeGrayReview(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, problem, reviewer string) {
		p := filepath.Join(dir, "sources", rel, "findings.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		body := "# atcr-findings/v1\nHIGH|auth.go:10|" + problem + "|fix|bug|10|evidence|" + reviewer + "\n"
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	write("host", problemA, "host")
	write(filepath.Join("pool", "raw", "agent", "greta"), problemB, "greta")
	return dir
}

func runRecon(t *testing.T, dir string) Result {
	t.Helper()
	res, err := RunReconcile(context.Background(), dir, nil, Options{ReconciledAt: time.Unix(1000, 0)})
	require.NoError(t, err)
	return res
}

func TestAmbiguous_ClusterHasStableID(t *testing.T) {
	dir := writeGrayReview(t)
	res := runRecon(t, dir)
	require.Len(t, res.Ambiguous, 1, "the gray-zone pair is one ambiguous cluster")
	require.Len(t, res.Findings, 2, "ambiguous pairs stay unmerged by default")

	want := AmbiguousID("auth.go", 10, problemA, problemB)
	assert.Equal(t, want, res.Ambiguous[0].ID)
	assert.InDelta(t, 0.571, res.Ambiguous[0].Similarity, 0.01)
}

func TestAmbiguous_AlwaysWrittenEmpty(t *testing.T) {
	// Two clearly-distinct findings → no ambiguous clusters, but ambiguous.json
	// is still written as an empty array (AC 05-04 Edge Case 1).
	dir := t.TempDir()
	p := filepath.Join(dir, "sources", "host", "findings.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte("# atcr-findings/v1\nHIGH|a.go:1|totally unrelated thing|fix|bug|5|e|host\n"), 0o644))
	runRecon(t, dir)
	data, err := os.ReadFile(filepath.Join(dir, "reconciled", AmbiguousJSON))
	require.NoError(t, err)
	var clusters []AmbiguousCluster
	require.NoError(t, json.Unmarshal(data, &clusters))
	assert.Empty(t, clusters)
}

// writeAdjudication writes a decisions file for the gray cluster, copying the
// baseline hash from summary.json the way the Skill does.
func writeAdjudication(t *testing.T, dir, clusterID, decision string) {
	t.Helper()
	sumData, err := os.ReadFile(filepath.Join(dir, "reconciled", SummaryJSON))
	require.NoError(t, err)
	var sum Summary
	require.NoError(t, json.Unmarshal(sumData, &sum))
	adj := Adjudication{BaselineHash: sum.AmbiguousHash, Decisions: []Decision{{
		ClusterID: clusterID, Decision: decision, Rationale: "test", HostModel: "claude-test", Timestamp: "2026-06-11T00:00:00Z",
	}}}
	data, err := json.Marshal(adj)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", AdjudicationJSON), data, 0o644))
}

func TestAdjudication_MergeCollapses(t *testing.T) {
	dir := writeGrayReview(t)
	res := runRecon(t, dir)
	id := res.Ambiguous[0].ID

	writeAdjudication(t, dir, id, DecisionMerge)
	res2 := runRecon(t, dir)

	assert.Len(t, res2.Findings, 1, "merge decision collapses the pair")
	assert.Empty(t, res2.Ambiguous, "the adjudicated pair is no longer ambiguous")
	assert.ElementsMatch(t, []string{"greta", "host"}, res2.Findings[0].Reviewers)
	assert.Equal(t, ConfHigh, res2.Findings[0].Confidence, "two reviewers → HIGH confidence")

	// The pre-adjudication sidecar is preserved for audit.
	orig, err := os.ReadFile(filepath.Join(dir, "reconciled", OriginalAmbiguousJSON))
	require.NoError(t, err)
	var clusters []AmbiguousCluster
	require.NoError(t, json.Unmarshal(orig, &clusters))
	assert.Len(t, clusters, 1, "original ambiguous cluster preserved")
}

// TestAdjudication_Idempotent verifies a persistent adjudication.json re-applies
// cleanly on repeated reconcile runs: validating against the preserved original
// baseline (not the post-merge sidecar) and not clobbering ambiguous.original.json.
func TestAdjudication_Idempotent(t *testing.T) {
	dir := writeGrayReview(t)
	id := runRecon(t, dir).Ambiguous[0].ID
	writeAdjudication(t, dir, id, DecisionMerge)

	// Run 2 (first adjudicated) and run 3 (re-run with the same decisions file).
	res2 := runRecon(t, dir)
	res3 := runRecon(t, dir)

	assert.Len(t, res2.Findings, 1)
	assert.Len(t, res3.Findings, 1, "re-running with the same adjudication is idempotent")
	assert.Empty(t, res3.Ambiguous)

	// The preserved original still holds the pre-merge cluster (not clobbered).
	orig, err := os.ReadFile(filepath.Join(dir, "reconciled", OriginalAmbiguousJSON))
	require.NoError(t, err)
	var clusters []AmbiguousCluster
	require.NoError(t, json.Unmarshal(orig, &clusters))
	require.Len(t, clusters, 1)
	assert.Equal(t, id, clusters[0].ID)
}

// TestPreserveOriginalAmbiguous_AtomicNoSymlinkFollow pins the preservation
// write to writeFileAtomic (temp + rename) rather than a plain os.WriteFile: a
// rename replaces a pre-planted dangling symlink at the destination with a
// regular file, while a plain write would follow the symlink and create its
// target instead (the write-through-symlink class persona resolution refuses),
// and a crash mid-plain-write would leave a truncated baseline.
func TestPreserveOriginalAmbiguous_AtomicNoSymlinkFollow(t *testing.T) {
	reconDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(reconDir, AmbiguousJSON), []byte("[]\n"), 0o644))

	escape := filepath.Join(t.TempDir(), "escape.json")
	require.NoError(t, os.Symlink(escape, filepath.Join(reconDir, OriginalAmbiguousJSON)))

	require.NoError(t, preserveOriginalAmbiguous(reconDir))

	fi, err := os.Lstat(filepath.Join(reconDir, OriginalAmbiguousJSON))
	require.NoError(t, err)
	assert.True(t, fi.Mode().IsRegular(), "baseline must be a regular file written via temp+rename, not a symlink")
	assert.NoFileExists(t, escape, "write must not pass through the planted symlink")

	data, err := os.ReadFile(filepath.Join(reconDir, OriginalAmbiguousJSON))
	require.NoError(t, err)
	assert.Equal(t, "[]\n", string(data))
}

func TestAdjudication_DistinctLeavesUnmerged(t *testing.T) {
	dir := writeGrayReview(t)
	id := runRecon(t, dir).Ambiguous[0].ID
	writeAdjudication(t, dir, id, DecisionDistinct)
	res := runRecon(t, dir)
	assert.Len(t, res.Findings, 2, "distinct decision keeps both findings")
}

func TestSummary_CarriesAmbiguousHash(t *testing.T) {
	// summary.json must carry ambiguous_hash = sha256 over the exact bytes of
	// the emitted ambiguous.json, so the Skill can copy it verbatim into
	// adjudication.json as baseline_hash (TD-024: bind decisions to the
	// ambiguous.json generation they were authored against).
	dir := writeGrayReview(t)
	runRecon(t, dir)
	sumData, err := os.ReadFile(filepath.Join(dir, "reconciled", SummaryJSON))
	require.NoError(t, err)
	var sum Summary
	require.NoError(t, json.Unmarshal(sumData, &sum))
	ambData, err := os.ReadFile(filepath.Join(dir, "reconciled", AmbiguousJSON))
	require.NoError(t, err)
	want := sha256.Sum256(ambData)
	assert.Equal(t, "sha256:"+hex.EncodeToString(want[:]), sum.AmbiguousHash)
}

func TestAdjudication_MissingBaselineHashRejected(t *testing.T) {
	// v1 is unreleased: a decisions file without the baseline binding is
	// rejected outright — tolerating it would preserve the exact stale-file
	// vulnerability the binding exists to close.
	dir := writeGrayReview(t)
	id := runRecon(t, dir).Ambiguous[0].ID
	raw := `{"decisions":[{"cluster_id":"` + id + `","decision":"merge"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", AdjudicationJSON), []byte(raw), 0o644))
	_, err := RunReconcile(context.Background(), dir, nil, Options{ReconciledAt: time.Unix(1000, 0)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing baseline_hash")
}

func TestAdjudication_StaleBaselineHashRejected(t *testing.T) {
	// A decisions file authored against a different ambiguous.json generation
	// (content-addressed ids may still match!) must refuse to apply.
	dir := writeGrayReview(t)
	id := runRecon(t, dir).Ambiguous[0].ID
	raw := `{"baseline_hash":"sha256:` + strings.Repeat("0", 64) + `","decisions":[{"cluster_id":"` + id + `","decision":"merge"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", AdjudicationJSON), []byte(raw), 0o644))
	_, err := RunReconcile(context.Background(), dir, nil, Options{ReconciledAt: time.Unix(1000, 0)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different ambiguous.json generation")
}

func TestAdjudication_NoClustersToAdjudicate(t *testing.T) {
	// adjudication.json present but the baseline has zero gray clusters: a
	// misfire that must hard-error explicitly, not pass silently or surface as
	// a confusing unknown-id error.
	dir := t.TempDir()
	p := filepath.Join(dir, "sources", "host", "findings.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte("# atcr-findings/v1\nHIGH|a.go:1|p|f|bug|5|e|host\n"), 0o644))
	runRecon(t, dir)
	raw := `{"baseline_hash":"sha256:` + strings.Repeat("0", 64) + `","decisions":[]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", AdjudicationJSON), []byte(raw), 0o644))
	_, err := RunReconcile(context.Background(), dir, nil, Options{ReconciledAt: time.Unix(1000, 0)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no clusters to adjudicate")
}

func TestAdjudication_UnknownClusterIDRejected(t *testing.T) {
	dir := writeGrayReview(t)
	runRecon(t, dir)
	writeAdjudication(t, dir, "amb-deadbeef", DecisionMerge)
	_, err := RunReconcile(context.Background(), dir, nil, Options{ReconciledAt: time.Unix(1000, 0)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown cluster id")
}

func TestAdjudication_MalformedRejected(t *testing.T) {
	dir := writeGrayReview(t)
	runRecon(t, dir)
	// Zero-byte adjudication.json is malformed.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reconciled", AdjudicationJSON), nil, 0o644))
	_, err := RunReconcile(context.Background(), dir, nil, Options{ReconciledAt: time.Unix(1000, 0)})
	require.Error(t, err)
}

func TestAdjudication_InvalidDecisionVerb(t *testing.T) {
	dir := writeGrayReview(t)
	id := runRecon(t, dir).Ambiguous[0].ID
	writeAdjudication(t, dir, id, "frobnicate")
	_, err := RunReconcile(context.Background(), dir, nil, Options{ReconciledAt: time.Unix(1000, 0)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid adjudication decision")
}
