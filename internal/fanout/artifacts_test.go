package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func okResult(agent, content string) Result {
	return Result{Agent: agent, Content: content, Status: StatusOK, DurationMS: 100, PayloadMode: "blocks"}
}

const findingsBody = `CRITICAL|auth.go:42|Token never expires|Check expiry|security|15|expiresAt unread
HIGH|main.go:88|Goroutine leak|Add WaitGroup|concurrency|30|no wg.Wait`

func TestWritePool_PerAgentArtifactsWritten(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "sources", "pool")
	results := []Result{okResult("greta", findingsBody)}

	sum, err := WritePool(pool, results, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, sum.Succeeded)

	agentDir := filepath.Join(pool, "raw", "agent", "greta")
	assert.FileExists(t, filepath.Join(agentDir, "review.md"))
	assert.FileExists(t, filepath.Join(agentDir, "findings.txt"))
	assert.FileExists(t, filepath.Join(agentDir, "status.json"))

	review, _ := os.ReadFile(filepath.Join(agentDir, "review.md"))
	assert.Equal(t, findingsBody, string(review), "review.md is the raw model content verbatim")
}

func TestWritePool_EngineSetsReviewerFromAgentName(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	// Model tries to self-attribute via an 8th column; engine must override it.
	content := `HIGH|a.go:1|prob|fix|security|10|ev|forged-name`
	_, err := WritePool(pool, []Result{okResult("greta", content)}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "findings.txt"))
	require.NoError(t, err)
	res, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	assert.Equal(t, "greta", res.Findings[0].Reviewer, "REVIEWER is the agent name, not the model's forged value")
}

func TestWritePool_StatusJSONRecordsOutcome(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	r := okResult("greta", findingsBody)
	r.Truncation = payload.Truncation{Truncated: true, FilesDropped: []string{"big.go"}}
	_, err := WritePool(pool, []Result{r}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(data, &st))

	assert.Equal(t, "greta", st.Agent)
	assert.Equal(t, StatusOK, st.Status)
	assert.Equal(t, 2, st.FindingsCount)
	assert.Equal(t, int64(100), st.DurationMS)
	assert.Equal(t, "blocks", st.PayloadMode)
	assert.True(t, st.Truncated)
	assert.Equal(t, []string{"big.go"}, st.FilesDropped)
}

func TestWritePool_FailedAgentStillWritesStatus(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		{Agent: "greta", Status: StatusFailed, Err: assertErr("connection refused"), PayloadMode: "blocks"},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err, "a failed agent must not abort artifact writing")

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var st AgentStatus
	require.NoError(t, json.Unmarshal(data, &st))
	assert.Equal(t, StatusFailed, st.Status)
	assert.Equal(t, 0, st.FindingsCount)
	assert.Contains(t, st.Error, "connection refused")
}

func TestWritePool_MergedFindingsAndSummary(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		okResult("greta", `CRITICAL|auth.go:42|Token never expires|Fix|security|15|ev`),
		okResult("kai", `HIGH|main.go:88|Goroutine leak|Add wg|concurrency|30|ev`),
		{Agent: "mira", Status: StatusFailed, Err: assertErr("timeout"), PayloadMode: "diff"},
	}
	sum, err := WritePool(pool, results, nil)
	require.NoError(t, err)
	assert.True(t, sum.Partial, "one failure among successes is partial")

	// Merged findings.txt holds both reviewers' rows with REVIEWER attribution.
	data, err := os.ReadFile(filepath.Join(pool, "findings.txt"))
	require.NoError(t, err)
	res, err := stream.ParseSource(data)
	require.NoError(t, err)
	require.Len(t, res.Findings, 2)
	reviewers := []string{res.Findings[0].Reviewer, res.Findings[1].Reviewer}
	assert.ElementsMatch(t, []string{"greta", "kai"}, reviewers)

	// summary.json records the run tally.
	sdata, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(sdata, &ps))
	assert.Equal(t, 3, ps.Total)
	assert.Equal(t, 2, ps.Succeeded)
	assert.Equal(t, 1, ps.Failed)
	assert.True(t, ps.Partial)
	assert.Equal(t, 2, ps.TotalFindings)
	assert.Len(t, ps.Agents, 3)
	assert.False(t, ps.FailureMarker, "a normal WritePool summary is a real run record, not a failure marker")
}

func TestWritePool_SanitizesAgentDirName(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	// A traversal-shaped name must be reduced to a base name; nothing escapes pool.
	_, err := WritePool(pool, []Result{okResult("../escape", findingsBody)}, nil)
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(pool, "raw", "agent", "escape"))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(pool), "escape"))
}

func TestWritePool_RejectsTraversalAgentNames(t *testing.T) {
	for _, name := range []string{"..", ".", ""} {
		pool := filepath.Join(t.TempDir(), "pool")
		_, err := WritePool(pool, []Result{okResult(name, findingsBody)}, nil)
		require.Error(t, err, "agent name %q must be rejected", name)
		assert.Contains(t, err.Error(), "invalid agent name")
	}
}

func TestWritePool_RejectsDuplicateAgentDirs(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	// Distinct names that collapse to the same base must not silently clobber.
	_, err := WritePool(pool, []Result{okResult("a/greta", findingsBody), okResult("b/greta", findingsBody)}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate agent directory")
}

func TestWritePool_ArtifactFileModeIs0644(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	_, err := WritePool(pool, []Result{okResult("greta", findingsBody)}, nil)
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "AC 01-03 mandates 0644 artifact files")
}

// TestWriteFailureSummary_PreservesRealCounts verifies the failure marker
// reflects what actually happened (some agents ok, some failed) rather than
// hard-coding all-failed.
func TestWriteFailureSummary_PreservesRealCounts(t *testing.T) {
	pool := t.TempDir()
	results := []Result{
		{Agent: "greta", Status: StatusOK, DurationMS: 100, PayloadMode: "blocks"},
		{Agent: "kai", Status: StatusFailed},
		{Agent: "mira", Status: StatusFailed},
	}
	writeFailureSummary(pool, results)

	data, err := os.ReadFile(filepath.Join(pool, summaryFile))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))
	assert.Equal(t, 3, ps.Total)
	assert.Equal(t, 1, ps.Succeeded, "partial success must be recorded, not fabricated as all-failed")
	assert.Equal(t, 2, ps.Failed)
	assert.True(t, ps.Partial)
	assert.True(t, ps.FailureMarker, "writeFailureSummary must stamp the best-effort marker so readers know this is not a real run record")
}

// TestWriteFailureSummary_AllFailed verifies the all-failed case still records
// correctly when every agent truly failed.
func TestWriteFailureSummary_AllFailed(t *testing.T) {
	pool := t.TempDir()
	results := []Result{
		{Agent: "greta", Status: StatusFailed},
		{Agent: "kai", Status: StatusFailed},
	}
	writeFailureSummary(pool, results)

	data, err := os.ReadFile(filepath.Join(pool, summaryFile))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))
	assert.Equal(t, 2, ps.Total)
	assert.Equal(t, 0, ps.Succeeded)
	assert.Equal(t, 2, ps.Failed)
	assert.False(t, ps.Partial)
	assert.True(t, ps.FailureMarker, "the marker flags the failure-path write regardless of partial; Succeeded==0 keeps it from forcing partial downstream")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

// TestWritePool_ConstrainedAgentPersistsDroppedCounts checks that
// dropped_by_min_severity and truncated_by_max_findings are written to
// status.json so volume reduction is observable after the run, not just in
// transient stderr.
func TestWritePool_ConstrainedAgentPersistsDroppedCounts(t *testing.T) {
	content := "HIGH|a.go:1|bug|fix|correctness|5|ev\n" +
		"LOW|b.go:2|nit|fix|style|5|ev\n" + // below MEDIUM floor
		"MEDIUM|c.go:3|gap|fix|correctness|5|ev\n"
	pool := filepath.Join(t.TempDir(), "pool")
	r := Result{Agent: "greta", Content: content, Status: StatusOK,
		MinSeverity: "MEDIUM", DurationMS: 100, PayloadMode: "blocks"}
	r.Truncation = payload.Truncation{FilesDropped: []string{}}
	_, err := WritePool(pool, []Result{r}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "raw", "agent", "greta", "status.json"))
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, float64(1), raw["dropped_by_min_severity"],
		"LOW finding dropped below MEDIUM floor must be recorded in status.json")
	assert.Equal(t, float64(0), raw["truncated_by_max_findings"],
		"truncated_by_max_findings must be zero when no max_findings cap applied")
}

// TestFindingsFor_UsesCachedCount verifies that findingsFor reuses a cached zero
// parsed-finding count rather than re-parsing Content (TD-019).
func TestFindingsFor_UsesCachedCount(t *testing.T) {
	// Content has one parseable finding, but the cached count says zero.
	r := Result{
		Agent:                 "bruce",
		Content:               "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce",
		parsedFindingCount:    0,
		parsedFindingCountSet: true,
	}
	fr := findingsFor(r, nil)
	assert.Empty(t, fr.Findings, "findingsFor should trust the cached zero count")
}

// TestStatusFor_DiagnosabilityPassThrough verifies statusFor copies the five
// Epic 19.10 F8 diagnosability values from Result to AgentStatus verbatim, with no
// recomputation, and leaves them zero for a Result that never went through
// per-model sizing.
func TestStatusFor_DiagnosabilityPassThrough(t *testing.T) {
	sized := Result{
		Agent: "dax", Status: StatusOK, PayloadMode: "diff",
		EffectiveBudget: 114688, ResolvedWindow: 32768, ReservedOutputTokens: 8192,
		ChunkCount: 6, DegradationAction: "chunk",
	}
	st := statusFor(sized, findingsResult{})
	assert.Equal(t, int64(114688), st.EffectiveBudget)
	assert.Equal(t, 32768, st.ResolvedWindow)
	assert.Equal(t, 8192, st.ReservedOutputTokens)
	assert.Equal(t, 6, st.ChunkCount)
	assert.Equal(t, "chunk", st.DegradationAction)

	// An unsized result leaves all five at their zero values (omitempty then fires).
	bare := statusFor(Result{Agent: "greta", Status: StatusOK}, findingsResult{})
	assert.Zero(t, bare.EffectiveBudget)
	assert.Zero(t, bare.ResolvedWindow)
	assert.Zero(t, bare.ReservedOutputTokens)
	assert.Zero(t, bare.ChunkCount)
	assert.Empty(t, bare.DegradationAction)
}

// TestWritePool_DiagnosabilityFieldsInSummary is the AC8 end-to-end proof: a run
// over a roster with a chunked agent and a truncate-degraded agent produces a
// summary.json whose agents[] entries carry the per-agent effective budget,
// resolved window, reserved output tokens, chunk count, and degradation action —
// while an unsized agent's entry omits all five.
func TestWritePool_DiagnosabilityFieldsInSummary(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")

	chunked := okResult("dax", findingsBody)
	chunked.EffectiveBudget, chunked.ResolvedWindow, chunked.ReservedOutputTokens = 114688, 32768, 8192
	chunked.ChunkCount, chunked.DegradationAction = 6, "chunk"

	truncated := okResult("otto", findingsBody)
	truncated.EffectiveBudget, truncated.ResolvedWindow, truncated.ReservedOutputTokens = 507904, 144941, 8192
	truncated.DegradationAction = "truncate"

	unsized := okResult("legacy", findingsBody) // e.g. a cache-hit/bare result

	_, err := WritePool(pool, []Result{chunked, truncated, unsized}, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))

	byAgent := map[string]AgentStatus{}
	for _, a := range ps.Agents {
		byAgent[a.Agent] = a
	}

	assert.Equal(t, 6, byAgent["dax"].ChunkCount)
	assert.Equal(t, "chunk", byAgent["dax"].DegradationAction)
	assert.Equal(t, int64(114688), byAgent["dax"].EffectiveBudget)
	assert.Equal(t, 32768, byAgent["dax"].ResolvedWindow)
	assert.Equal(t, 8192, byAgent["dax"].ReservedOutputTokens)

	assert.Equal(t, "truncate", byAgent["otto"].DegradationAction)
	assert.Equal(t, int64(507904), byAgent["otto"].EffectiveBudget)
	assert.Zero(t, byAgent["otto"].ChunkCount, "a bulk (unchunked) degraded agent has no chunk count")

	// The unsized agent's entry omits every diagnosability field entirely.
	assert.Zero(t, byAgent["legacy"].EffectiveBudget)
	assert.Zero(t, byAgent["legacy"].ResolvedWindow)
	assert.Empty(t, byAgent["legacy"].DegradationAction)
}

// dualWriteFindings is a fixture whose fields v1 cannot carry: a literal pipe
// and a multi-line EVIDENCE. findings.toon must keep them; findings.txt keeps
// today's lossy bytes.
var dualWriteFindings = []stream.Finding{
	{Severity: "HIGH", File: "flags.go", Line: 7, Problem: "mode is O_CREATE | O_WRONLY", Fix: "use a | b", Category: "correctness", EstMinutes: 5, Evidence: "line one\n    line two", Reviewer: "greta"},
	{Severity: "LOW", File: "x.go", Line: 1, Problem: "plain", Fix: "plain fix", Category: "style", EstMinutes: 1, Evidence: "ev", Reviewer: "kai"},
}

func v1Bytes(t *testing.T, findings []stream.Finding) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, stream.WriteSource(&b, findings))
	return b.Bytes()
}

func parseFile(t *testing.T, path string) []stream.Finding {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	res, err := stream.ParseSource(data)
	require.NoError(t, err)
	return res.Findings
}

// stubWriteFindingsFile swaps the atomic write writeFindings uses, recording the
// order of written paths and failing the write whose base name is failOn.
func stubWriteFindingsFile(t *testing.T, failOn string) *[]string {
	t.Helper()
	var order []string
	orig := writeFindingsFileFn
	writeFindingsFileFn = func(path string, data []byte) error {
		order = append(order, filepath.Base(path))
		if filepath.Base(path) == failOn {
			return errors.New("injected write failure")
		}
		return orig(path, data)
	}
	t.Cleanup(func() { writeFindingsFileFn = orig })
	return &order
}

func TestWriteFindings_DualWritesTxtAndToon(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeFindings(filepath.Join(dir, findingsFile), dualWriteFindings))

	txt, err := os.ReadFile(filepath.Join(dir, findingsFile))
	require.NoError(t, err)
	// Literal bytes captured from main's writer before this change, so the
	// check does not compare stream.WriteSource with itself.
	const wantV1 = "# atcr-findings/v1\n" +
		"HIGH|flags.go:7|mode is O_CREATE / O_WRONLY|use a / b|correctness|5|line one     line two|greta\n" +
		"LOW|x.go:1|plain|plain fix|style|1|ev|kai\n"
	assert.Equal(t, wantV1, string(txt), "findings.txt must stay byte-identical v1")

	assert.Equal(t, dualWriteFindings, parseFile(t, filepath.Join(dir, findingsToonFile)),
		"findings.toon must carry every field losslessly")
}

func TestWriteFindings_ToonAndTxtAgreeOnPlainFindings(t *testing.T) {
	dir := t.TempDir()
	plain := dualWriteFindings[1:]
	require.NoError(t, writeFindings(filepath.Join(dir, findingsFile), plain))
	assert.Equal(t, parseFile(t, filepath.Join(dir, findingsFile)), parseFile(t, filepath.Join(dir, findingsToonFile)))
}

func TestWriteFindings_EmptyFindingsWritesBothHeaders(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeFindings(filepath.Join(dir, findingsFile), nil))
	assert.Empty(t, parseFile(t, filepath.Join(dir, findingsFile)))
	assert.Empty(t, parseFile(t, filepath.Join(dir, findingsToonFile)))
}

func TestWriteFindings_WritesToonBeforeTxt(t *testing.T) {
	order := stubWriteFindingsFile(t, "")
	require.NoError(t, writeFindings(filepath.Join(t.TempDir(), findingsFile), dualWriteFindings))
	assert.Equal(t, []string{findingsToonFile, findingsFile}, *order)
}

// A v2 encode failure must leave the directory untouched: both buffers are
// encoded before either write.
func TestWriteFindings_V2EncodeFailureWritesNeitherFile(t *testing.T) {
	orig := encodeFindingsV2Fn
	encodeFindingsV2Fn = func(io.Writer, []stream.Finding) error { return errors.New("injected encode failure") }
	t.Cleanup(func() { encodeFindingsV2Fn = orig })

	dir := t.TempDir()
	err := writeFindings(filepath.Join(dir, findingsFile), dualWriteFindings)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encoding findings (v2)")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, []string{findingsFile, findingsToonFile}, e.Name())
	}
}

// Decision A (AC 04-01 Error Scenario 3): a failed findings.toon write on a
// rewrite leaves the previous pair intact, so no reader sees a stale .toon
// beside a fresh .txt.
func TestWriteFindings_ToonWriteFailureKeepsPreviousPair(t *testing.T) {
	dir := t.TempDir()
	old := dualWriteFindings[1:]
	require.NoError(t, writeFindings(filepath.Join(dir, findingsFile), old))
	oldTxt, err := os.ReadFile(filepath.Join(dir, findingsFile))
	require.NoError(t, err)
	oldToon, err := os.ReadFile(filepath.Join(dir, findingsToonFile))
	require.NoError(t, err)

	stubWriteFindingsFile(t, findingsToonFile)
	require.Error(t, writeFindings(filepath.Join(dir, findingsFile), dualWriteFindings))

	gotTxt, err := os.ReadFile(filepath.Join(dir, findingsFile))
	require.NoError(t, err)
	gotToon, err := os.ReadFile(filepath.Join(dir, findingsToonFile))
	require.NoError(t, err)
	assert.Equal(t, oldTxt, gotTxt, "findings.txt must not be written after a failed .toon write")
	assert.Equal(t, oldToon, gotToon)

	sel, err := stream.SelectFindingsFile(dir)
	require.NoError(t, err)
	assert.Equal(t, old, parseFile(t, sel), "readers still see the previous consistent findings")
}

func TestWriteFindings_TxtWriteFailureKeepsFreshToonAndErrors(t *testing.T) {
	dir := t.TempDir()
	stubWriteFindingsFile(t, findingsFile)
	require.Error(t, writeFindings(filepath.Join(dir, findingsFile), dualWriteFindings))
	assert.Equal(t, dualWriteFindings, parseFile(t, filepath.Join(dir, findingsToonFile)))
}

// Every writeFindings call site dual-writes: per-agent (writeAgentArtifacts),
// merged pool (writePool), and the resume rewrite (RebuildPool).
func TestWritePool_DualWritesPerAgentAndPool(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	content := "HIGH|a.go:1|p|f|security|10|ev"
	_, err := WritePool(pool, []Result{okResult("greta", content), okResult("kai", content)}, nil)
	require.NoError(t, err)

	for _, dir := range []string{
		filepath.Join(pool, poolRawAgentDir, "greta"),
		filepath.Join(pool, poolRawAgentDir, "kai"),
		pool,
	} {
		txt := parseFile(t, filepath.Join(dir, findingsFile))
		toon := parseFile(t, filepath.Join(dir, findingsToonFile))
		assert.NotEmpty(t, toon, dir)
		assert.Equal(t, txt, toon, "%s: both files hold the same findings in the same order", dir)
	}
}

func TestRebuildPool_DualWritesPool(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		okResult("zeta", "CRITICAL|z.go:1|z|fz|security|15|ez"),
		okResult("alpha", "HIGH|a.go:1|a|fa|security|15|ea"),
	}, nil))
	// writeResumedAgents writes only per-agent artifacts, so the merged pool
	// files come from the rebuild alone.
	require.NoFileExists(t, filepath.Join(poolDir, findingsToonFile))

	_, _, err := RebuildPool(context.Background(), poolDir, []string{"zeta", "alpha"})
	require.NoError(t, err)

	txt := parseFile(t, filepath.Join(poolDir, findingsFile))
	toon := parseFile(t, filepath.Join(poolDir, findingsToonFile))
	require.Len(t, toon, 2)
	assert.Equal(t, txt, toon)
	assert.Equal(t, "z.go", toon[0].File, "roster order")
}
