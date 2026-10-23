package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
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
	}, nil, nil))
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

// TD (sprint 35.16.11.2): the findings-write wrap in writeAgentArtifacts
// ("writing findings for '<agent>'") had no test. A failing findings.toon/txt
// write must surface as a wrapped, agent-attributed error, not a bare write
// error.
func TestWriteAgentArtifacts_FindingsWriteFailureIsWrapped(t *testing.T) {
	dir := t.TempDir()
	orig := writeFindingsFileFn
	writeFindingsFileFn = func(path string, data []byte) error {
		return errors.New("injected findings write failure")
	}
	t.Cleanup(func() { writeFindingsFileFn = orig })

	err := writeAgentArtifacts(dir, "greta", Result{Agent: "greta"}, findingsResult{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing findings for 'greta'")
	assert.Contains(t, err.Error(), "injected findings write failure")
}

// TestTruncatedZeroRemedy_MatchesDocThinkingLever pins truncatedZeroRemedy to
// docs/registry.md's "Thinking and max_tokens" paragraph, which makes `thinking: off`
// the FIRST remedy for a reviewer that truncates with zero findings. The warning in
// artifacts.go is the only runtime place that remedy is stated, so the two must name
// the same lever in the same order — if the doc's advice changes, this test fails
// until the runtime remedy follows it (and vice versa).
func TestTruncatedZeroRemedy_MatchesDocThinkingLever(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "registry.md"))
	require.NoError(t, err)
	docPara := ""
	for _, para := range strings.Split(string(doc), "\n\n") {
		// Anchor on the note's own lead-in: other paragraphs (e.g. the thinking
		// row) cross-reference "Thinking and `max_tokens`" by name, and matching
		// on containment alone would pin the wrong paragraph.
		if strings.HasPrefix(para, "**Thinking and `max_tokens`.**") {
			docPara = para
			break
		}
	}
	require.NotEmpty(t, docPara, "docs/registry.md must keep a 'Thinking and max_tokens' paragraph")

	// Doc-side anchor: the paragraph must still lead with the thinking lever.
	docOff := strings.Index(docPara, "`thinking: off`")
	docCap := strings.Index(docPara, "max_tokens` alone")
	require.GreaterOrEqual(t, docOff, 0, "doc paragraph must state the thinking: off remedy")
	require.Less(t, docOff, docCap, "doc paragraph must lead with the thinking lever, not the cap raise")

	remedy := truncatedZeroRemedy

	// The doc's measured advice: a lower level can silence the model instead of
	// fixing it, so the runtime remedy must never prescribe thinking_level: low
	// and must carry the doc's repoint-the-agent advice (epic 35.16.11.2.2.2).
	require.NotContains(t, remedy, "use thinking_level: low",
		"truncatedZeroRemedy must not prescribe thinking_level: low — epic 35.16.11.2.2.2 measured it silencing nemotron-3-super-120b (NO FINDINGS)")
	require.Contains(t, docPara, "repoint", "doc paragraph must carry the repoint-the-agent advice")
	require.Contains(t, remedy, "repoint", "truncatedZeroRemedy must agree with the doc's repoint-the-agent advice")

	// Runtime side: the operator-facing remedy must agree with the doc.
	off := strings.Index(remedy, "thinking: off")
	capIdx := strings.Index(remedy, "--max-tokens")
	require.GreaterOrEqual(t, off, 0, "truncatedZeroRemedy must mention thinking: off (docs' first fix)")
	require.GreaterOrEqual(t, capIdx, 0, "truncatedZeroRemedy must keep the cap-raise advice")
	require.Less(t, off, capIdx,
		"truncatedZeroRemedy must LEAD with the thinking lever: docs/registry.md calls thinking: off the first fix, and raising max_tokens alone gives a runaway thinker more room")

	// The existing context-window tradeoff text must be retained.
	for _, token := range []string{"same context window", "4096", "context_window_tokens"} {
		require.Contains(t, remedy, token, "truncatedZeroRemedy must keep its context-window tradeoff text (%s)", token)
	}
}

// earlyWriteCompleter is personaSpeedCompleter plus m-fail, which always errors,
// and any bin carrying failFile, which errors too.
type earlyWriteCompleter struct {
	personaSpeedCompleter
	failFile string
}

func (c earlyWriteCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	if inv.Model == "m-fail" {
		return "", errors.New("simulated provider failure")
	}
	if c.failFile != "" && strings.Contains(inv.Prompt, "// FILE:"+c.failFile+"\n") {
		return "", fmt.Errorf("simulated bin failure for %s", c.failFile)
	}
	return c.personaSpeedCompleter.Complete(ctx, inv)
}

// earlyWriteSlots is greta chunked over a.go/b.go/c.go plus single-slot personas
// with the given name -> model pairs, in order.
func earlyWriteSlots(t *testing.T, single ...string) []Slot {
	t.Helper()
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.PayloadByteBudget = 100
	entries := []payload.FileEntry{baselineEntry("a.go", 100), baselineEntry("b.go", 100), baselineEntry("c.go", 100)}
	slots, _, err := buildSlots(cfg, baselinePayloads(entries), ReviewRange{}, string(payload.ModeFiles), "", false, true)
	require.NoError(t, err)
	require.Len(t, slots, 3, "greta plans one bin per file")
	for i := 0; i+1 < len(single); i += 2 {
		slots = append(slots, Slot{Primary: Agent{Name: single[i], Invocation: llmclient.Invocation{Model: single[i+1]}}})
	}
	return slots
}

func agentFile(poolDir, agent, name string) string {
	return filepath.Join(poolDir, poolRawAgentDir, agent, name)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Epic 35.16.14.1 T2 success criterion: a persona's status.json is on disk, with
// its findings already present, while a slower persona still runs; a failed
// persona's status.json says failed at once; and status.json is always the last
// of the persona's files to land.
func TestRunEngine_EarlyWrite_PersonaOnDiskWhileSlowerRuns(t *testing.T) {
	slots := earlyWriteSlots(t, "slow", "m-slow", "mid", "m-mid", "bad", "m-fail")
	poolDir := t.TempDir()

	orig := writeStatusFn
	t.Cleanup(func() { writeStatusFn = orig })
	var statusOrderErrs []string
	var smu sync.Mutex
	writeStatusFn = func(path string, s *AgentStatus) error {
		dir := filepath.Dir(path)
		for _, f := range []string{reviewFile, findingsToonFile, findingsFile} {
			if !fileExists(filepath.Join(dir, f)) {
				smu.Lock()
				statusOrderErrs = append(statusOrderErrs, s.Agent+": status.json before "+f)
				smu.Unlock()
			}
		}
		return orig(path, s)
	}

	release := make(chan struct{})
	var mu sync.Mutex
	seen := map[string]bool{}
	type onDisk struct {
		status, findings, slowStatus bool
		st                           AgentStatus
	}
	got := map[string]onDisk{}
	p := &PreparedReview{Slots: slots}
	p.onPersonaDone = func(agent string, _ []int, _ []Result) {
		d := onDisk{
			status:     fileExists(agentFile(poolDir, agent, statusFile)),
			findings:   fileExists(agentFile(poolDir, agent, findingsFile)),
			slowStatus: fileExists(agentFile(poolDir, "slow", statusFile)),
		}
		if data, err := os.ReadFile(agentFile(poolDir, agent, statusFile)); err == nil {
			_ = json.Unmarshal(data, &d.st)
		}
		mu.Lock()
		defer mu.Unlock()
		got[agent] = d
		seen[agent] = true
		if seen["greta"] && seen["mid"] && seen["bad"] && !seen["slow"] {
			close(release)
		}
	}

	ret := make(chan struct{})
	go func() {
		defer close(ret)
		runEngine(context.Background(), earlyWriteCompleter{personaSpeedCompleter: personaSpeedCompleter{release: release}}, p, poolDir)
	}()
	select {
	case <-ret:
	case <-time.After(10 * time.Second):
		t.Fatal("run hung: the fast personas did not report done while the slow persona ran")
	}

	mu.Lock()
	defer mu.Unlock()
	for _, agent := range []string{"greta", "mid", "bad"} {
		d := got[agent]
		assert.True(t, d.status, "%s: status.json on disk the moment it finished", agent)
		assert.True(t, d.findings, "%s: findings.txt on disk beside its status.json", agent)
		assert.False(t, d.slowStatus, "%s finished while the slow persona still ran, with no status.json for it", agent)
	}
	assert.Equal(t, StatusOK, got["greta"].st.Status)
	assert.Equal(t, 3, got["greta"].st.FindingsCount, "greta's status.json is the merge of all three bins")
	assert.Equal(t, StatusOK, got["mid"].st.Status)
	assert.Equal(t, StatusFailed, got["bad"].st.Status, "a failed persona's status.json says failed at once")
	assert.Contains(t, got["bad"].st.Error, "simulated provider failure")
	assert.True(t, got["slow"].status, "the slow persona is written when it finishes too")
	smu.Lock()
	assert.Empty(t, statusOrderErrs, "status.json is written last")
	smu.Unlock()
}

// A run killed between a persona's writes leaves no status.json without its
// findings: whichever write the kill lands on, the persona has either both or
// no status.json, and a resume (CompletedAgents) never takes it for finished.
func TestRunEngine_EarlyWrite_KillBetweenWritesLeavesNoStatusWithoutFindings(t *testing.T) {
	for _, kill := range []string{findingsToonFile, findingsFile, statusFile} {
		t.Run("killed at "+kill, func(t *testing.T) {
			origF, origS := writeFindingsFileFn, writeStatusFn
			t.Cleanup(func() { writeFindingsFileFn, writeStatusFn = origF, origS })
			killed := errors.New("killed")
			die := func(path string) bool {
				return filepath.Base(filepath.Dir(path)) == "mid" && filepath.Base(path) == kill
			}
			writeFindingsFileFn = func(path string, data []byte) error {
				if die(path) {
					return killed
				}
				return origF(path, data)
			}
			writeStatusFn = func(path string, s *AgentStatus) error {
				if die(path) {
					return killed
				}
				return origS(path, s)
			}

			reviewDir := t.TempDir()
			poolDir := filepath.Join(reviewDir, "sources", "pool")
			p := &PreparedReview{Slots: earlyWriteSlots(t, "mid", "m-mid", "kai", "m-mid")}
			// No WritePool after: the process died before the join.
			runEngine(context.Background(), earlyWriteCompleter{}, p, poolDir)

			entries, err := os.ReadDir(filepath.Join(poolDir, poolRawAgentDir))
			require.NoError(t, err)
			for _, e := range entries {
				if fileExists(agentFile(poolDir, e.Name(), statusFile)) {
					assert.True(t, fileExists(agentFile(poolDir, e.Name(), findingsFile)), "%s: status.json without findings.txt", e.Name())
				}
			}
			assert.False(t, fileExists(agentFile(poolDir, "mid", statusFile)), "the killed persona has no status.json")
			for _, agent := range []string{"greta", "kai"} {
				assert.True(t, fileExists(agentFile(poolDir, agent, statusFile)), "%s, untouched by the kill, is written", agent)
			}
			done, err := CompletedAgents(reviewDir)
			require.NoError(t, err)
			assert.NotContains(t, done, "mid", "a resume reruns the killed persona")
		})
	}
}

// The early per-persona files are the same merge WritePool writes after the
// join, and the final pool is byte for byte what WritePool alone writes from
// the same results, including a chunked persona's file gap.
func TestRunEngine_EarlyWrite_FinalPoolByteIdentical(t *testing.T) {
	poolDir := t.TempDir()
	p := &PreparedReview{Slots: earlyWriteSlots(t, "kai", "m-mid", "bad", "m-fail")}
	results, _ := runEngine(context.Background(), earlyWriteCompleter{failFile: "b.go"}, p, poolDir)

	agentFiles := []string{reviewFile, findingsFile, findingsToonFile, statusFile}
	early := map[string]string{}
	for _, agent := range []string{"greta", "kai", "bad"} {
		for _, f := range agentFiles {
			data, err := os.ReadFile(agentFile(poolDir, agent, f))
			require.NoError(t, err, "%s/%s written early", agent, f)
			early[agent+"/"+f] = string(data)
		}
	}
	assert.Equal(t, 1, readAgentStatus(t, poolDir, "greta").UnreviewedFiles, "the early status carries greta's file gap")

	_, err := WritePool(poolDir, results, nil)
	require.NoError(t, err, "WritePool rewrites the files the hook already wrote")
	todayPool := t.TempDir()
	_, err = WritePool(todayPool, results, nil)
	require.NoError(t, err)

	for k, want := range early {
		data, err := os.ReadFile(filepath.Join(poolDir, poolRawAgentDir, k))
		require.NoError(t, err)
		assert.Equal(t, want, string(data), "%s: the early write is the final file", k)
	}
	require.NoError(t, filepath.WalkDir(todayPool, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(todayPool, path)
		want, _ := os.ReadFile(path)
		got, rerr := os.ReadFile(filepath.Join(poolDir, rel))
		require.NoError(t, rerr, rel)
		assert.Equal(t, string(want), string(got), "%s matches today's pool byte for byte", rel)
		return nil
	}))
}

// On a resume that replays a persona's reviewed bins, the hook merges the
// persona once and the final writeResumedAgents does not merge it again, so
// review.md and findings hold every bin exactly once.
func TestExecuteResume_EarlyWrite_CarriedPersonaMergedOnce(t *testing.T) {
	entries := sixtyFourFiles()
	dir := originalRun(t, entries, 4)
	poolDir := filepath.Join(dir, "sources", "pool")
	resumeGreta(t, dir, gretaBaselineSlots(t, entries[4:]), baselineChunkFindingCompleter{})

	review, err := os.ReadFile(agentFile(poolDir, "greta", reviewFile))
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, 1, strings.Count(string(review), "problem "+e.Path+"|"), "%s once in review.md", e.Path)
	}
	assert.Len(t, agentFindingFiles(t, poolDir, "greta"), 64)
}

// The resume hook keeps writeResumedAgents' rule: a slot the engine never ran
// (a synthesized cancellation) does not overwrite the prior run's record, while
// a fresh review writes it at once.
func TestWriteEarlyAgent_SynthesizedCancelOnlySkippedOnResume(t *testing.T) {
	synth := Result{Agent: "greta", Status: StatusTimeout, Err: context.Canceled}

	poolDir := t.TempDir()
	require.NoError(t, writeEarlyAgent(poolDir, synth, nil, nil, true))
	assert.False(t, fileExists(agentFile(poolDir, "greta", statusFile)), "a resume keeps the prior record")

	require.NoError(t, writeEarlyAgent(poolDir, synth, nil, nil, false))
	assert.Equal(t, StatusTimeout, readAgentStatus(t, poolDir, "greta").Status, "a fresh review records the timeout at once")
}

// ungroundedBody is one grounded finding (auth.go:42, inside groundedChanged)
// and one ungrounded one (auth.go:999), so the grounding gate drops exactly one.
const ungroundedBody = "MEDIUM|auth.go:42|real bug|f|correctness|10|token := parseToken(r)\n" +
	"HIGH|auth.go:999|hallucinated|f|correctness|10|invented unrelated snippet\n"

var groundedChanged = payload.ChangedLines{
	"auth.go": {
		Ranges:      []payload.LineRange{{Start: 40, End: 45}},
		ChangedText: []string{"token := parseToken(r)"},
	},
}

const ungroundedWarning = `atcr: warning: agent "greta": dropped 1 ungrounded finding(s) not present in the patch`

// Epic 78 T2 (row artifacts.go:700): a persona written early and then again by
// the end-of-run writer is graded twice, but its ungrounded-finding warning is
// printed once — on a fresh run, on a resume, and on a resume whose replayed
// bins the early write already merged.
func TestEarlyWrite_UngroundedWarningPrintedOnce(t *testing.T) {
	r := okResult("greta", ungroundedBody)

	t.Run("fresh run", func(t *testing.T) {
		poolDir := t.TempDir()
		out := captureStderr(t, func() {
			require.NoError(t, writeEarlyAgent(poolDir, r, groundedChanged, nil, false))
			_, err := writePool(context.Background(), poolDir, []Result{r}, groundedChanged, "")
			require.NoError(t, err)
		})
		assert.Equal(t, 1, strings.Count(out, ungroundedWarning), "stderr:\n%s", out)
	})

	t.Run("resume", func(t *testing.T) {
		poolDir := t.TempDir()
		out := captureStderr(t, func() {
			require.NoError(t, writeEarlyAgent(poolDir, r, groundedChanged, nil, true))
			require.NoError(t, writeResumedAgents(poolDir, []Result{r}, groundedChanged, nil))
		})
		assert.Equal(t, 1, strings.Count(out, ungroundedWarning), "stderr:\n%s", out)
	})

	t.Run("resume with replayed bins", func(t *testing.T) {
		poolDir := t.TempDir()
		prior := []stream.Finding{{Severity: "LOW", File: "main.go", Line: 3, Problem: "p", Fix: "f", Category: "style", EstMinutes: 5, Reviewer: "greta"}}
		require.NoError(t, writeAgentFiles(poolDir, "greta", "greta", "prior review", prior, &AgentStatus{Agent: "greta", Status: StatusOK}))
		l := &chunkLedger{
			records: map[string]*chunkRecord{"greta": {Bins: []chunkRecordBin{
				{Files: []string{"main.go"}, Outcome: StatusOK},
				{Files: []string{"auth.go"}, Outcome: StatusOK},
			}}},
			carried: map[string]map[string]bool{"greta": {"main.go": true}},
		}
		out := captureStderr(t, func() {
			require.NoError(t, writeEarlyAgent(poolDir, r, groundedChanged, l, true))
			require.NoError(t, writeResumedAgents(poolDir, []Result{r}, groundedChanged, l))
		})
		assert.Equal(t, 1, strings.Count(out, ungroundedWarning), "stderr:\n%s", out)
	})
}
