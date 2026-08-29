package reconcile

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractAnchors_IdentifierShapes covers the deterministic extraction rules
// (Epic 35.16.6.5 T1): backtick spans, quoted spans, and call shapes yield
// identifier-shaped anchors; prose that carries no identifier signal yields
// none. No model is involved, so the mapping is a pure function of the text.
func TestExtractAnchors_IdentifierShapes(t *testing.T) {
	cases := []struct {
		name    string
		problem string
		fix     string
		want    []string
	}{
		{
			name:    "backtick span",
			problem: "the `validateFindingPaths` helper never checks the root",
			want:    []string{"validateFindingPaths"},
		},
		{
			name:    "call shape without backticks",
			problem: "BuildFileIndex() is called once per finding instead of once per run",
			want:    []string{"BuildFileIndex"},
		},
		{
			name:    "double-quoted span",
			problem: `the "MissingSuggestion" routine returns the cited path back to itself`,
			want:    []string{"MissingSuggestion"},
		},
		{
			name:    "single-quoted span",
			problem: "'stampSymbolAnchors' double-stamps on a re-reconcile",
			want:    []string{"stampSymbolAnchors"},
		},
		{
			name:    "qualified name keeps the trailing segment",
			problem: "`stream.ValidatePath` swallows the permission error",
			want:    []string{"ValidatePath"},
		},
		{
			name:    "snake_case counts as an identifier signal",
			problem: "the `path_suggestion` field is emitted even when empty",
			want:    []string{"path_suggestion"},
		},
		{
			name:    "problem and fix are both scanned, result is sorted and deduped",
			problem: "`readTree` leaks a file handle; `readTree` is also called twice",
			fix:     "close the handle in `openTree` before returning",
			want:    []string{"openTree", "readTree"},
		},
		{
			name:    "plain prose yields no anchors",
			problem: "this function is too long and hard to read",
			fix:     "split it up",
			want:    nil,
		},
		{
			name:    "quoted english phrase is not an identifier",
			problem: `the warning text "file not found" should be capitalized`,
			want:    nil,
		},
		{
			name:    "lowercase single word carries no identifier signal",
			problem: "the `handler` is wrong",
			want:    nil,
		},
		{
			name:    "empty text yields no anchors",
			problem: "",
			fix:     "",
			want:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeAnchorsForTest(tc.problem, tc.fix)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestExtractAnchors_Deterministic pins AC2: identical input yields a
// byte-identical anchor slice across repeated calls, with no ordering drift
// from map iteration.
func TestExtractAnchors_Deterministic(t *testing.T) {
	problem := "`zebraCheck` and `alphaCheck` disagree; see `middleCheck()` and `zebraCheck`"
	fix := "make `alphaCheck` authoritative"

	first := mergeAnchorsForTest(problem, fix)
	require.Equal(t, []string{"alphaCheck", "middleCheck", "zebraCheck"}, first,
		"anchors are deduped and lexically sorted, never map-iteration ordered")

	for i := 0; i < 25; i++ {
		assert.Equal(t, first, mergeAnchorsForTest(problem, fix), "run %d drifted", i)
	}
}

// TestExtractAnchors_Capped bounds the work Tier 4 does per finding: a finding
// whose prose names an unreasonable number of identifiers contributes a capped
// anchor set rather than an unbounded index sweep.
func TestExtractAnchors_Capped(t *testing.T) {
	problem := ""
	for _, n := range []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight", "iNine", "jTen", "kEleven", "lTwelve"} {
		problem += "`" + n + "` "
	}
	got := mergeAnchorsForTest(problem, "")
	assert.Len(t, got, maxAnchorsPerFinding)
	assert.Equal(t, []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight"}, got,
		"the cap keeps the lexically-first anchors so the truncation is deterministic too")
}

// TestExtractAnchors_ApostropheProse pins the per-delimiter scan: an apostrophe
// used as English punctuation must not swallow the backtick spans that follow
// it. A single interleaved scan mis-pairs "parser's" with "doesn't" and loses
// every identifier in between.
func TestExtractAnchors_ApostropheProse(t *testing.T) {
	got := mergeAnchorsForTest(
		"the parser's cache is stale so `readTree` doesn't refresh `openTree`",
		"the caller's fix is to invalidate in `dropCache`")
	assert.Equal(t, []string{"dropCache", "openTree", "readTree"}, got)
}

// TestExtractAnchors_UnterminatedDelimiter pins that a lone opener contributes
// nothing and never panics on the slice bounds.
func TestExtractAnchors_UnterminatedDelimiter(t *testing.T) {
	assert.Nil(t, mergeAnchorsForTest("a stray backtick ` at the very end", ""))
	assert.Equal(t, []string{"realName"}, mergeAnchorsForTest("`realName` then a stray ` tail", ""))
}

// mergeAnchorsForTest is the pre-split extraction shape, retained ONLY so the
// original T1 extraction table keeps exercising the tokenizer against both
// fields at once. Production no longer merges the two: see extractAnchorSet's
// doc for why a FIX anchor may never be no-match evidence.
func mergeAnchorsForTest(problem, fix string) []string {
	seen := map[string]struct{}{}
	for _, text := range []string{problem, fix} {
		a, _ := extractAnchorSet(text)
		for _, tok := range a {
			seen[tok] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for tok := range seen {
		out = append(out, tok)
	}
	sort.Strings(out)
	if len(out) > maxAnchorsPerFinding {
		out = out[:maxAnchorsPerFinding]
	}
	return out
}
