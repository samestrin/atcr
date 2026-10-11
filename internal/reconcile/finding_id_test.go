package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunReconcile_StampsFindingIDOverAnchoredProblem pins Epic 35.16.11.3 T1 on
// the real pipeline: every record in the emitted findings.json carries an id equal
// to history.FindingID of its emitted File, Line and symbol-anchored Problem. The
// anchored finding is the case that matters: an ID stamped before
// stampSymbolAnchors would hash the reviewer's bare text and name no stored row.
func TestRunReconcile_StampsFindingIDOverAnchoredProblem(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/tok/renew.go": "package tok\n\nfunc RenewToken() error {\n\tx := 1\n\t_ = x\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/tok/renew.go:4|RenewToken drops the refresh error|check it|correctness|20|ev|greta\n"+
			"LOW|internal/tok/renew.go:1|package tok has no doc comment|add one|style|2|ev|greta\n")

	_, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	got, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, got, 2)

	anchored := 0
	for _, f := range got {
		require.NotEmpty(t, f.ID, "every emitted record carries an id: %+v", f)
		assert.Equal(t, history.FindingID(f.File, f.Line, f.Problem), f.ID,
			"id is FindingID over the emitted (file, line, problem)")
		if bare, ok := strings.CutPrefix(f.Problem, "(RenewToken) "); ok {
			anchored++
			assert.NotEqual(t, history.FindingID(f.File, f.Line, bare), f.ID,
				"the id hashes the anchored text, not the reviewer's bare problem")
		}
	}
	require.Equal(t, 1, anchored, "the fixture must anchor exactly one finding, or the anchored-text check proves nothing")
}

// TestJSONFindings_DerivedPathStampsID covers the no-cache path of
// Result.JSONFindings(): a Result built directly (no RunReconcile, no anchoring)
// still emits an id over its merged (file, line, problem).
func TestJSONFindings_DerivedPathStampsID(t *testing.T) {
	res := goldenCorpusResult()
	jf := res.JSONFindings()
	require.NotEmpty(t, jf)
	for _, f := range jf {
		assert.Equal(t, history.FindingID(f.File, f.Line, f.Problem), f.ID)
	}
}

// TestJSONFinding_NoIDRemarshalsByteIdentical pins the omitempty half of T1: a
// findings.json written before the id field existed decodes and re-marshals
// through the new struct to the same bytes, so a pre-epic file is unchanged by a
// round trip and gains no empty "id" key.
func TestJSONFinding_NoIDRemarshalsByteIdentical(t *testing.T) {
	legacy := `[
  {
    "severity": "HIGH",
    "file": "auth.go",
    "line": 42,
    "problem": "(Check) token never expires here",
    "fix": "guard it",
    "category": "security",
    "est_minutes": 15,
    "evidence": "pool saw it",
    "reviewers": [
      "greta",
      "host"
    ],
    "confidence": "HIGH",
    "disagreement": "severity",
    "path_warning": "missing",
    "justification": "narrative",
    "source_report": {
      "path": "sources/host/review.md",
      "line": 3
    }
  },
  {
    "severity": "LOW",
    "file": "util.go",
    "line": 5,
    "problem": "unused import",
    "fix": "remove it",
    "category": "style",
    "est_minutes": 2,
    "evidence": "lint",
    "reviewers": [
      "greta"
    ],
    "confidence": "MEDIUM"
  }
]
`
	var findings []JSONFinding
	require.NoError(t, json.Unmarshal([]byte(legacy), &findings))
	for _, f := range findings {
		require.Empty(t, f.ID)
	}

	var buf bytes.Buffer
	require.NoError(t, renderIndentedJSON(&buf, findings))
	assert.Equal(t, legacy, buf.String())
}

// TestJSONFinding_ReaderWithoutIDDecodesOtherFields pins the middle leg of the
// name-mapped guarantee: a consumer whose struct predates the id field decodes
// every other v1 field of a findings.json that carries id, because
// encoding/json maps keys by name and ignores the one it does not know.
func TestJSONFinding_ReaderWithoutIDDecodesOtherFields(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/tok/renew.go": "package tok\n\nfunc RenewToken() error {\n\tx := 1\n\t_ = x\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/tok/renew.go:4|RenewToken drops the refresh error|check it|correctness|20|ev|greta\n"+
			"LOW|internal/tok/renew.go:1|package tok has no doc comment|add one|style|2|ev|greta\n")

	_, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	want, err := ReadReconciledFindings(reviewDir)
	require.NoError(t, err)
	require.Len(t, want, 2)
	withID := 0
	for _, f := range want {
		if f.ID != "" {
			withID++
		}
	}
	require.NotZero(t, withID, "the fixture must carry an id, or decoding past it proves nothing")

	type legacyReader struct {
		Severity   string   `json:"severity"`
		File       string   `json:"file"`
		Line       int      `json:"line"`
		Problem    string   `json:"problem"`
		Fix        string   `json:"fix"`
		Category   string   `json:"category"`
		EstMinutes int      `json:"est_minutes"`
		Evidence   string   `json:"evidence"`
		Reviewers  []string `json:"reviewers"`
		Confidence string   `json:"confidence"`
	}
	data, err := os.ReadFile(filepath.Join(reviewDir, reconciledSubdir, FindingsJSON))
	require.NoError(t, err)
	require.Contains(t, string(data), `"id":`, "the file on disk carries the id key")

	var got []legacyReader
	require.NoError(t, json.Unmarshal(data, &got))
	require.Len(t, got, len(want))
	for i, w := range want {
		assert.Equal(t, legacyReader{
			Severity:   w.Severity,
			File:       w.File,
			Line:       w.Line,
			Problem:    w.Problem,
			Fix:        w.Fix,
			Category:   w.Category,
			EstMinutes: w.EstMinutes,
			Evidence:   w.Evidence,
			Reviewers:  w.Reviewers,
			Confidence: w.Confidence,
		}, got[i], "record %d decodes every non-id field by name", i)
	}
}
