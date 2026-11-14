package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/tddedupe"
)

// The two fixture streams sit in directories named for their reviewer, so the
// default tag (the parent directory's name) is the reviewer.
var (
	dedupeClaudeStream = filepath.Join("testdata", "dedupe_cmd", "claude", "td-stream.txt")
	dedupeGPTStream    = filepath.Join("testdata", "dedupe_cmd", "gpt", "td-stream.txt")
	dedupeStreams      = dedupeClaudeStream + "," + dedupeGPTStream
)

func runDedupeJSON(t *testing.T, args ...string) tddedupe.Result {
	t.Helper()
	code, stdout, stderr := execCmdSplit(t, append([]string{"dedupe"}, args...)...)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	var res tddedupe.Result
	require.NoError(t, json.Unmarshal([]byte(stdout), &res), stdout)
	return res
}

func dedupeRowAt(t *testing.T, res tddedupe.Result, fileLine string) tddedupe.Row {
	t.Helper()
	for _, r := range res.Merged {
		if r.FileLine == fileLine {
			return r
		}
	}
	t.Fatalf("no merged row at %s in %+v", fileLine, res.Merged)
	return tddedupe.Row{}
}

func TestDedupe_TwoReviewersOnOneLineMergeForReview(t *testing.T) {
	res := runDedupeJSON(t, "--streams", dedupeStreams)

	r := dedupeRowAt(t, res, "internal/store/store.go:42")
	assert.Equal(t, "HIGH", r.Severity)
	assert.Equal(t, "HIGH", r.Confidence)
	assert.True(t, r.NeedsReview)
	assert.Len(t, r.Members, 2)
	assert.Equal(t, "LOW vs HIGH", r.Disagreement)
	assert.Equal(t, "claude,gpt", r.Reviewers, "an empty REVIEWER takes the parent directory's name")
	assert.Equal(t, "claude", r.Source)
	assert.Equal(t, "initialize the map in New // make the map before use", r.Fix, "both remedies are kept, in line order")

	single := dedupeRowAt(t, res, "cli/main.go:10")
	assert.Equal(t, "MEDIUM", single.Confidence)
	assert.False(t, single.NeedsReview)
	assert.Empty(t, single.Members)

	assert.Equal(t, tddedupe.Summary{InputRows: 3, Sources: 2, Clusters: 2, MergedRows: 2, NeedsReviewCount: 1}, res.Summary)
}

// The JSON keeps EVIDENCE, so the merged stream the skill writes from it still
// has the column: the merged row leads with the highest-severity member's quote,
// and each member keeps its own.
func TestDedupe_KeepsEvidence(t *testing.T) {
	res := runDedupeJSON(t, "--streams", dedupeStreams)

	r := dedupeRowAt(t, res, "internal/store/store.go:42")
	assert.Equal(t, "store.go:42 writes s.items[k] before make / store.go:43 assigns into a nil map", r.Evidence)
	require.Len(t, r.Members, 2)
	assert.Equal(t, "store.go:42 writes s.items[k] before make", r.Members[0].Evidence)
	assert.Equal(t, "store.go:43 assigns into a nil map", r.Members[1].Evidence)

	assert.Equal(t, "main.go:10 imports os", dedupeRowAt(t, res, "cli/main.go:10").Evidence)
}

func TestDedupe_UntrustedSourceAloneIsLow(t *testing.T) {
	res := runDedupeJSON(t, "--streams", dedupeStreams, "--untrusted", "claude")
	assert.Equal(t, "LOW", dedupeRowAt(t, res, "cli/main.go:10").Confidence)
	assert.Equal(t, "HIGH", dedupeRowAt(t, res, "internal/store/store.go:42").Confidence)
}

func TestDedupe_SourceTagsNameTheStreams(t *testing.T) {
	res := runDedupeJSON(t, "--streams", dedupeStreams, "--source-tags", "a,b")
	r := dedupeRowAt(t, res, "internal/store/store.go:42")
	assert.Equal(t, "a,b", r.Reviewers)
	assert.Equal(t, "a", r.Source)
}

// A missing --source-tags entry falls back to the parent directory's name.
func TestDedupe_ShortSourceTagsFallBackToTheDirectory(t *testing.T) {
	res := runDedupeJSON(t, "--streams", dedupeStreams, "--source-tags", "a")
	assert.Equal(t, "a,gpt", dedupeRowAt(t, res, "internal/store/store.go:42").Reviewers)
}

// An empty --source-tags entry keeps its position: the later tag stays on its
// own stream.
func TestDedupe_EmptySourceTagKeepsItsPosition(t *testing.T) {
	res := runDedupeJSON(t, "--streams", dedupeStreams, "--source-tags", ",b")
	r := dedupeRowAt(t, res, "internal/store/store.go:42")
	assert.Equal(t, "b,claude", r.Reviewers)
	assert.Equal(t, "claude", r.Source)
}

func TestDedupe_ToleranceZeroSplitsAdjacentLines(t *testing.T) {
	res := runDedupeJSON(t, "--streams", dedupeStreams, "--tolerance", "0")
	assert.Len(t, res.Merged, 3)
	assert.Zero(t, res.Summary.NeedsReviewCount)
}

func TestDedupe_PrintsTdDedupeKeys(t *testing.T) {
	code, stdout, stderr := execCmdSplit(t, "dedupe", "--streams", dedupeStreams)
	require.Equal(t, 0, code, stderr)
	var doc struct {
		Merged  []map[string]json.RawMessage `json:"merged"`
		Summary map[string]json.RawMessage   `json:"summary"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	row := dedupeRowKeys(t, doc.Merged, "internal/store/store.go:42")
	assert.Equal(t, sortedKeys(row), []string{"category", "cluster_size", "confidence", "disagreement", "est_minutes", "evidence", "file_line", "fix", "members", "needs_review", "problem", "reviewers", "severity", "source"})
	var members []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(row["members"], &members))
	assert.Equal(t, sortedKeys(members[0]), []string{"category", "est_minutes", "evidence", "file_line", "fix", "problem", "reviewer", "severity"})
	// warnings is omitted when every stream has a version header, as td-dedupe omits it.
	assert.Equal(t, sortedKeys(doc.Summary), []string{"clusters", "input_rows", "merged_rows", "needs_review_count", "skipped", "sources"})
}

func dedupeRowKeys(t *testing.T, rows []map[string]json.RawMessage, fileLine string) map[string]json.RawMessage {
	t.Helper()
	for _, r := range rows {
		var fl string
		require.NoError(t, json.Unmarshal(r["file_line"], &fl))
		if fl == fileLine {
			return r
		}
	}
	t.Fatalf("no row at %s", fileLine)
	return nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDedupe_TextOutput(t *testing.T) {
	code, stdout, stderr := execCmdSplit(t, "dedupe", "--streams", dedupeStreams, "--json=false")
	require.Equal(t, 0, code, stderr)
	assert.Equal(t, "3 finding(s) → 2 merged (1 need review)\n", stdout)
}

func TestDedupe_UsageErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent", "td-stream.txt")
	for name, args := range map[string][]string{
		"no --streams":       {},
		"empty --streams":    {"--streams", " , "},
		"unreadable path":    {"--streams", dedupeClaudeStream + "," + missing},
		"positional args":    {"--streams", dedupeStreams, "extra"},
		"non-integer window": {"--streams", dedupeStreams, "--tolerance", "x"},
	} {
		code, stdout, _ := execCmdSplit(t, append([]string{"dedupe"}, args...)...)
		assert.Equal(t, exitUsage, code, name)
		assert.Empty(t, stdout, name)
	}
}

// An unsupported stream version is bad data, not a bad invocation: exit 1.
func TestDedupe_UnsupportedVersionFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "td-stream.txt")
	require.NoError(t, os.WriteFile(p, []byte("# atcr-findings/v9\nHIGH|a.go:1|p|f|c|1|e|r\n"), 0o600))
	code, stdout, _ := execCmdSplit(t, "dedupe", "--streams", p)
	assert.Equal(t, exitFailure, code)
	assert.Empty(t, stdout)
}
