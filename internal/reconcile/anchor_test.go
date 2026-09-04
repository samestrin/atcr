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
		{
			// collectCallAnchors scans BACKWARDS from every "(". A paren with no
			// identifier run before it yields a zero-length span, which the
			// `start == i` early-out skips. Parenthesised prose is common in review
			// text, so this is the ordinary case, not a pathological one.
			//
			// NOT A MUTATION GUARD, deliberately: that early-out is provably
			// redundant. start == i means the span is empty, and addAnchor already
			// rejects an empty span on isIdentifierShaped's minAnchorLen test, so
			// deleting the early-out changes no output for any input. This case
			// pins the OUTPUT (parenthesised prose contributes nothing) and
			// documents the early-out as belt-and-braces. Do not "strengthen" it
			// into a claim that the branch is pinned — it cannot be.
			name:    "bare paren with no identifier before it",
			problem: "the guard (x) is applied before `readTree` runs",
			want:    []string{"readTree"},
		},
		{
			name:    "paren opening a clause contributes nothing",
			problem: "the retry path (see the comment above) drops the error",
			want:    nil,
		},
		{
			// The "." case of isQualifiedIdentByte, reachable ONLY through a CALL
			// shape: a backtick span never consults it, so `stream.ValidatePath`
			// above leaves this branch untouched. Without it the backwards scan
			// stops at the dot and records the receiver segment instead.
			name:    "package-qualified call keeps the trailing segment",
			problem: "stream.ValidatePath() swallows the permission error",
			want:    []string{"ValidatePath"},
		},
		{
			// The "_" case of isQualifiedIdentByte, likewise call-shape only.
			name:    "snake_case call shape",
			problem: "read_tree() is invoked once per finding",
			want:    []string{"read_tree"},
		},
		{
			name:    "receiver-qualified snake_case call",
			problem: "idx.by_fold() is consulted before the parser runs",
			want:    []string{"by_fold"},
		},
		{
			// isIdentifierShaped rejects a digit-leading token: no language admits
			// one as an identifier, so it is a version string or a numeric literal
			// the reviewer quoted, never a construct to search the tree for.
			//
			// The token must carry an identifier SIGNAL (the interior case change
			// in `9abcDef`), or hasIdentifierSignal rejects it first and this case
			// passes whether or not the digit-leading test exists. A plain `9abc`
			// looks like the obvious fixture and is exactly that dead assertion.
			name:    "digit-leading token is not an identifier",
			problem: "the `9abcDef` marker is emitted twice",
			want:    nil,
		},
		{
			name:    "digit-leading call shape is not an identifier",
			problem: "9abcDef() appears in the generated table",
			want:    nil,
		},
		{
			// The companion direction: a digit INSIDE an identifier is fine, so the
			// rejection above must be keyed on position, not on digits at all.
			name:    "interior digit is allowed",
			problem: "the `readTree2` helper duplicates `readTree`",
			want:    []string{"readTree", "readTree2"},
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

// TestHasIdentifierSignal_NormalizationAgreement pins the signal predicate to
// the same normalization-independence the shape predicate already has: a
// combining mark is uncased, so it must not RESET the lower->upper transition
// tracker — the NFD spelling of a camelCase name (cafe + U+0301 + Bar) carries
// the same signal as its NFC spelling, and a caseless-script name keeps
// carrying no signal under either spelling. Literals are escape-spelled so an
// editor's Unicode normalisation cannot silently rewrite the fixture.
func TestHasIdentifierSignal_NormalizationAgreement(t *testing.T) {
	cases := []struct {
		name string
		nfc  string
		nfd  string
		want bool
	}{
		{"camelCase across a combining mark", "caf\u00e9Bar", "cafe\u0301Bar", true},
		{"single-case word with a mark", "caf\u00e9", "cafe\u0301", false},
		{"caseless script", "\u0928\u093e\u092e", "\u0928\u093e\u092e", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasIdentifierSignal(tc.nfc), "NFC spelling")
			assert.Equal(t, tc.want, hasIdentifierSignal(tc.nfd),
				"NFD spelling must agree with NFC")
		})
	}
}

// TestIsIdentifierShaped_CombiningMarks pins the mark rule on the harvest
// filter: a combining mark (Mn or Mc) INSIDE a token leaves it
// identifier-shaped — `export const नाम = 1` (नाम carries the Mc vowel sign
// ा) and an NFD-spelled `café` (e + U+0301) are real declarations whose names
// must reach presentInSource — while a LEADING mark or an enclosing mark (Me,
// outside ID_Continue) still fails. This mirrors isDeclNameRune's class
// (symbolindex.go): the two must agree, or a grammar-admitted declaration
// harvests without its name and a finding anchored on it is routed out as
// fabricated. Literals are escape-spelled so an editor's Unicode
// normalisation cannot silently rewrite the fixture.
func TestIsIdentifierShaped_CombiningMarks(t *testing.T) {
	assert.True(t, isIdentifierShaped("\u0928\u093e\u092e"),
		"Devanagari name with an Mc vowel sign is a real identifier")
	assert.True(t, isIdentifierShaped("cafe\u0301"),
		"NFD spelling (e + U+0301 combining acute) is the same declaration as precomposed café")
	assert.False(t, isIdentifierShaped("\u0301abc"),
		"a combining mark cannot BEGIN an identifier")
	assert.False(t, isIdentifierShaped("a\u20ddbc"),
		"U+20DD is category Me, outside ID_Continue — node rejects `const a⃝b = 1`")
}
