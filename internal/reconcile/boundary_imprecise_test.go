package reconcile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/stream"
)

// TestScanAnchors_BoundaryTruncatedCallAnchorIsImprecise pins epic 35.16.6.8.2 T1
// at the unit level: a call-shape run the spaceless-script word boundary cut short
// contributes its surviving fragment as an IMPRECISE anchor, not a clean one.
//
// The fragment may be the real name (`ParseConfig` really is declared somewhere)
// or it may be the Latin tail of a mixed name the reviewer wrote whole
// (`配置ParseConfig`), and nothing in the text separates the two — which is the
// same undecidability the GLUED span is already marked imprecise for, reached by
// the boundary rather than by an underscore.
//
// `truncated` is deliberately NOT asserted true here. That flag feeds the
// no-match direction, and this epic changes only the PathSuggestion direction;
// extractAnchorSet's doc records the false reading as accepted for this shape.
func TestScanAnchors_BoundaryTruncatedCallAnchorIsImprecise(t *testing.T) {
	cases := []struct {
		name          string
		text          string
		wantAnchors   []string
		wantImprecise []string
		wantTruncated bool
	}{
		{
			name:          "latin tail after a spaceless prefix is imprecise",
			text:          "配置ParseConfig() ignores the returned error",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: []string{"ParseConfig"},
		},
		{
			name:          "a clean citation of the same name retracts the imprecision",
			text:          "配置ParseConfig() ignores the error returned by `ParseConfig`",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: nil,
		},
		{
			name:          "an ordinary ASCII call shape stays precise",
			text:          "BuildFileIndex() is called once per finding",
			wantAnchors:   []string{"BuildFileIndex"},
			wantImprecise: nil,
		},
		{
			name:          "a spaced-out prefix is not a boundary break",
			text:          "the helper ParseConfig() ignores the returned error",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: nil,
		},
		{
			name:          "a qualified call after a spaceless prefix is imprecise on its trailing segment",
			text:          "配置cfg.ParseConfig() ignores the returned error",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: []string{"ParseConfig"},
		},
		{
			name: "a silenced boundary span still contributes nothing",
			// `_ParseConfig` leads with an underscore once the break lands, so
			// collectCallAnchors silences it outright — the imprecision producer
			// added here must not resurrect it as an anchor.
			text:          "配置_ParseConfig() ignores the returned error",
			wantAnchors:   nil,
			wantImprecise: nil,
			wantTruncated: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := scanAnchors(tc.text)
			assert.Equal(t, tc.wantAnchors, s.anchors)

			got := make([]string, 0, len(s.imprecise))
			for tok := range s.imprecise {
				got = append(got, tok)
			}
			assert.ElementsMatch(t, tc.wantImprecise, got)
			assert.Equal(t, tc.wantTruncated, s.truncated(),
				"this epic changes the PathSuggestion direction only, never the no-match one")
		})
	}
}

// TestRunReconcile_BoundaryTruncatedAnchorWithholdsSuggestionEndToEnd is epic
// 35.16.6.8.2's AC1 acceptance test, run against the real pipeline: a real git
// repo, the real `git ls-files` candidate index, and the real embedded parser.
//
// The tree declares BOTH halves of the ambiguity — `配置ParseConfig` in
// internal/zh/mix.go (what the reviewer actually wrote) and `ParseConfig` in
// internal/cfg/parse.go (the Latin tail the boundary rule leaves behind). Before
// this epic the tail located internal/cfg/parse.go in exactly one file and
// validate.go stamped a CONFIDENT PathSuggestion there — a wrong answer at the one
// seam symbolindex.go states nothing downstream can undo.
//
// The finding must be KEPT either way. Withholding the suggestion costs a
// "did you mean" clause; routing the finding out would delete a real finding and
// durably charge the reviewer a phantom, so the outcome asserted here is
// tier4Inconclusive, never tier4NoMatch.
func TestRunReconcile_BoundaryTruncatedAnchorWithholdsSuggestionEndToEnd(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/zh/mix.go": "package zh\n\n" +
			"func 配置ParseConfig() error {\n\treturn nil\n}\n",
		"internal/cfg/parse.go": "package cfg\n\n" +
			"func ParseConfig() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/tokens/renewal.go:31|配置ParseConfig() ignores the returned error|check the error|correctness|20|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1, "the finding is KEPT — withholding a suggestion never routes one out")

	got := res.JSONFindings()[0]
	assert.Equal(t, "internal/tokens/renewal.go", got.File, "suggest-only: File is never rewritten")
	assert.False(t, got.PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, got.PathWarning)
	assert.Empty(t, got.PathSuggestion,
		"the boundary-truncated tail may not source a confident suggestion at the file declaring only that tail")

	unresolved, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	assert.Empty(t, unresolved, "an inconclusive Tier 4 verdict is never sidecar-routed")
	assert.Zero(t, res.Summary.UnresolvedFiltered)
}
