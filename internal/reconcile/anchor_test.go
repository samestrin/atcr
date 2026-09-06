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
			// The "." case of isQualifiedIdentRune, reachable ONLY through a CALL
			// shape: a backtick span never consults it, so `stream.ValidatePath`
			// above leaves this branch untouched. Without it the backwards scan
			// stops at the dot and records the receiver segment instead.
			name:    "package-qualified call keeps the trailing segment",
			problem: "stream.ValidatePath() swallows the permission error",
			want:    []string{"ValidatePath"},
		},
		{
			// The "_" case of isQualifiedIdentRune, likewise call-shape only.
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
		{
			// A non-ASCII call name must be captured WHOLE or not at all: the
			// backwards scan decodes runes, so it never truncates at a
			// multibyte rune's continuation byte. An ASCII byte class turned
			// `parseGrößeValue()` into the fragment "eValue" — a different,
			// potentially declared, name. Escape-spelled so an editor's
			// normalisation cannot rewrite the fixture.
			name:    "non-ASCII call shape yields the full name, never a fragment",
			problem: "parseGr\u00f6\u00dfeValue() is called twice",
			want:    []string{"parseGr\u00f6\u00dfeValue"},
		},
		{
			name:    "diaeresis call shape yields the full name, never a fragment",
			problem: "na\u00efveParser() drops the error",
			want:    []string{"na\u00efveParser"},
		},
		{
			// The Mn/Mc arm of isQualifiedIdentRune, which the two cases
			// above do NOT reach: their o-umlaut and i-diaeresis are
			// precomposed LETTERS, so they exercise only the unicode.IsLetter
			// arm. A COMBINING mark is what the arm exists for - an
			// NFD-spelled name (cafe + U+0301) is what a macOS- or
			// git-normalised tree carries. Delete the arm and the backwards
			// scan stops at the mark, leaving the span "Bar", which carries no
			// identifier signal and yields NO anchor at all.
			name:    "NFD call shape yields the whole NFC-folded name",
			problem: "cafe\u0301Bar() drops the error",
			want:    []string{"caf\u00e9Bar"},
		},
		{
			// The Mc half of the same arm, and the case where losing it is
			// WORSE than losing the anchor: delete the arm and the scan stops
			// at the U+093E vowel sign, yielding a two-rune fragment of the
			// name - still identifier-shaped, still signal-carrying (it keeps
			// the underscore), and a name that may be declared somewhere else
			// entirely, which validate.go would then stamp as a confident
			// PathSuggestion at the wrong file.
			name:    "Devanagari call shape yields the whole name, never a fragment",
			problem: "\u0928\u093e\u092e_load() drops the error",
			want:    []string{"\u0928\u093e\u092e_load"},
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

// TestExtractAnchors_SpacelessScriptBoundary pins the CRITICAL regression the
// rune-based backwards scan introduced: isQualifiedIdentRune admits every
// unicode.IsLetter rune, and in a script that writes without inter-word spaces
// (Han, Kana, Hangul, Thai) that class never terminates at the word boundary —
// so the reviewer's own prose is glued onto the call name and BECOMES the
// anchor. `在配置中调用ParseConfig()` then yields the pseudo-token
// `在配置中调用ParseConfig`, which is identifier-shaped and carries a signal
// (the interior e->C transition), is in neither present nor byName, and so
// resolves to tier4NoMatch — deleting a real finding and durably charging the
// reviewer a phantom. atcr's own registry runs CJK-emitting models, so this is
// the ordinary path for them.
//
// The last case is the companion direction and is load-bearing: a script that
// DOES separate words (Devanagari) must keep contributing to the name, or the
// boundary rule over-truncates `नाम_load` to the fragment `_load` — the same
// class of defect pointed the other way. Literals are escape-spelled so an
// editor's Unicode normalisation cannot silently rewrite the fixture.
func TestExtractAnchors_SpacelessScriptBoundary(t *testing.T) {
	cases := []struct {
		name    string
		problem string
		want    []string
	}{
		{
			name:    "Han prose glued to a call name stops at the script boundary",
			problem: "\u5728\u914d\u7f6e\u4e2d\u8c03\u7528ParseConfig() \u65f6\u8d85\u65f6\u672a\u5904\u7406",
			want:    []string{"ParseConfig"},
		},
		{
			name:    "Kana prose glued to a call name stops at the script boundary",
			problem: "\u30ab\u30bf\u30ab\u30ca\u3067ParseConfig() \u3092\u547c\u3076",
			want:    []string{"ParseConfig"},
		},
		{
			name:    "Hangul prose glued to a call name stops at the script boundary",
			problem: "\ucf54\ub4dc\uc5d0\uc11cParseConfig() \ud638\ucd9c",
			want:    []string{"ParseConfig"},
		},
		{
			name:    "Thai prose glued to a call name stops at the script boundary",
			problem: "\u0e41\u0e25\u0e49\u0e27\u0e40\u0e23\u0e35\u0e22\u0e01ParseConfig() \u0e44\u0e21\u0e48\u0e08\u0e31\u0e14\u0e01\u0e32\u0e23",
			want:    []string{"ParseConfig"},
		},
		{
			name:    "a space-separating script stays part of the call name",
			problem: "\u0928\u093e\u092e_load() drops the error",
			want:    []string{"\u0928\u093e\u092e_load"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mergeAnchorsForTest(tc.problem, ""))
		})
	}
}

// TestHasIdentifierSignal_DigitCarry pins the OTHER uncased class the signal
// predicate carries a lowercase run across: digits. Its combining-mark sibling
// is pinned by TestHasIdentifierSignal_NormalizationAgreement, but the digit
// half is the one that changes behaviour in a pure-ASCII tree — base64Encode,
// sha256Sum, x509Cert and v2Config all move false->true, so every finding
// citing such a name now contributes an anchor it did not before, feeding
// locate, PathSuggestion, the anchor cap and namedInDocs. Without this table
// the carry can be reverted (digits reset the tracker) or over-widened (any
// uppercase counts, so a leading capital suffices) with the suite still green.
//
// The all-lowercase rows are the over-widening guard: a digit must CARRY a
// lowercase run into a later uppercase, never manufacture a signal on its own.
func TestHasIdentifierSignal_DigitCarry(t *testing.T) {
	cases := []struct {
		tok  string
		want bool
	}{
		{"base64Encode", true},
		{"sha256Sum", true},
		{"utf8Reader", true},
		{"x509Cert", true},
		{"v2Config", true},
		{"base64encode", false},
		{"sha256sum", false},
		{"Base64", false}, // leading capital is not an INTERNAL transition
	}
	for _, tc := range cases {
		t.Run(tc.tok, func(t *testing.T) {
			assert.Equal(t, tc.want, hasIdentifierSignal(tc.tok))
		})
	}
}

// TestExtractAnchors_SnakeCaseSpacelessNameSurvivesBoundary pins the CRITICAL
// regression the spaceless-script boundary rule introduced in the OTHER
// direction: the break fires INSIDE a single real identifier whenever a
// snake_case name written in a spaceless script carries an internal script
// transition, and the fragment it leaves is still signal-carrying because the
// underscore is consumed BEFORE the break is detected on the next rune, so
// hasIdentifierSignal's strings.Contains(tok, "_") short-circuit admits it.
//
// Measured old-vs-new: `データ_解析()` yielded the whole name before the rule
// and yields `_解析` after it. The fragment is in neither present nor byName
// (collectSourceIdentifiers treats '_' as a word byte, so the declaration is
// ONE token), so resolve returns tier4NoMatch — gate.go deletes a real finding
// and scorecard.go durably charges the reviewer a phantom for 180 days. That is
// strictly worse than either predecessor: the pre-rune ASCII byte scan stopped
// at the first multibyte byte and produced NO anchor, which is inconclusive and
// safe.
//
// TWO different runes could fire the old break, and the repair needed both
// halves. Replaying the pre-diff predicate rune by rune:
//
//   - `データ_解析` broke at タ. The backwards run reads 析(Han) 解(Han) then the
//     underscore (skipped, not a letter) then タ(Katakana), and Katakana != Han
//     ends it. U+30FC is two runes further back and is never reached. That is
//     why the spaceless/spaceless exemption in isWordBoundary is load-bearing
//     here, not the Script=Common change.
//
//   - `ユーザー_取得` broke at U+30FC, the katakana-hiragana prolonged sound
//     mark: 得(Han) 取(Han) then the underscore then ー, which is Script=Common
//     and which the original rule classified as scriptSpacing — splitting the
//     name at its own vowel mark. Measured old anchor: `_取得`. That mark is in
//     most everyday Japanese loanword identifiers (データ, ユーザー, サーバー,
//     ロード, パーサー), so the case is ordinary rather than exotic.
//     spacelessScriptOf now returns scriptNeutral for it.
//
// Both halves are still pinned by the `データ_解析` row: the exemption keeps the
// run past タ, and only then does it reach ー, where the Script=Common rule
// keeps it going. Deleting either one turns the row red (measured — the
// Script=Common mutant yields the fragment `タ_解析`).
//
// An earlier revision of this paragraph attributed the `データ_解析` break to
// U+30FC "with no real script change anywhere in it". That is false in both
// halves and was never measured.
//
// The last two rows are the load-bearing counter-direction: prose glued to a
// call name must still terminate at the boundary. An underscore inside the
// PROSE (rather than inside the name) must not license the glue, or this test
// would pin the very defect the boundary rule exists to remove.
func TestExtractAnchors_SnakeCaseSpacelessNameSurvivesBoundary(t *testing.T) {
	cases := []struct {
		name    string
		problem string
		want    []string
	}{
		{
			// Katakana + U+30FC (Common) + Han: no script change at all, and
			// still broken before the fix.
			name:    "Katakana-with-prolonged-mark plus Han snake_case name stays whole",
			problem: "データ_解析() drops the error",
			want:    []string{"データ_解析"},
		},
		{
			name:    "Katakana plus Han snake_case name stays whole",
			problem: "サバ_接続() drops the error",
			want:    []string{"サバ_接続"},
		},
		{
			name:    "Han plus Katakana snake_case name stays whole",
			problem: "解析_データ() drops the error",
			want:    []string{"解析_データ"},
		},
		{
			// Counter-direction: the prose carries the underscore, not the
			// name. The boundary must still fire.
			name:    "Han prose with an underscore still stops at the call name",
			problem: "在_配置中调用ParseConfig() 时超时",
			want:    []string{"ParseConfig"},
		},
		{
			// The undecidable case, and the one the rule must REFUSE to
			// answer: an underscore straddling a spaceless/spacing boundary
			// is either prose glued onto `_ParseConfig` or the tail of one
			// snake_case name `调用_ParseConfig`, and nothing in the text
			// distinguishes them. Both available answers are wrong in one
			// reading, so the run contributes NO anchor - resolve then reads
			// the finding as tier4Inconclusive and KEEPS it, instead of
			// deleting it or stamping a PathSuggestion at whatever declares
			// the fragment.
			name:    "an underscore straddling the boundary yields no anchor at all",
			problem: "调用_ParseConfig() 失败",
			want:    nil,
		},
		{
			// Same shape with a genuinely mixed Han+Latin name. Also
			// undecidable, also answered with silence rather than the
			// fragment `_loadFile`.
			name:    "a mixed Han-Latin snake_case name yields no anchor rather than a fragment",
			problem: "設定_loadFile() drops the error",
			want:    nil,
		},
		{
			// MUTATION GUARD for isWordBoundary's `next == scriptSpacing`
			// clause. Every other row above fires the boundary through
			// `run == scriptSpacing` (Latin or Han prose reached first, a
			// spaceless name after it). This is the only direction where the
			// SPACELESS side is the run and the space-separating side is what
			// the run reaches, so it is the only row the second clause decides.
			//
			// Measured with the clause deleted: the run never stops, and
			// `parse_解析()` yields the glued anchor `parse_解析` where HEAD
			// yields none - the name-final-spaceless direction of the boundary
			// was entirely unpinned before this row.
			name:    "a name-final spaceless run still stops at the boundary",
			problem: "parse_解析() drops the error",
			want:    nil,
		},
		{
			// MUTATION GUARD for spacelessScriptOf's `!unicode.IsLetter(r)`
			// clause. That clause is redundant for every ASCII non-letter the
			// identifier class admits - '_', '.' and the digits are all
			// Script=Common, so the SECOND clause already returns neutral for
			// them - and its only real job is COMBINING MARKS, which are
			// Script=Inherited and reach neither clause any other way.
			//
			// Its job is to make a mark TRANSPARENT to the backwards run, so a
			// name is read across it rather than broken at it. Delete the clause
			// and U+0301 classifies as scriptSpacing: the run, already in Han,
			// treats the mark as a space-separating letter and stops dead.
			// Measured with the clause deleted:
			//
			//   HEAD    ["解析_処理́"]   the name is read across the mark
			//   mutant  []              the name is broken at the mark and lost
			//
			// The mark is placed at the end of a Han name rather than inside an
			// ordinary word because that is the ONLY position where the clause
			// decides anything. An Inherited mark surrounded by Latin classifies
			// as scriptSpacing either way (Latin IS scriptSpacing), and a Thai
			// tone mark inside a Thai name classifies as Thai either way - both
			// measured, both identical under the mutant. The clause is only
			// observable where an Inherited mark borders a spaceless run
			// directly, which is what this row is.
			name:    "a combining mark inside a spaceless name is read across, not broken at",
			problem: "解析_処理" + string(rune(0x0301)) + "() drops the error",
			want:    []string{"解析_処理" + string(rune(0x0301))},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mergeAnchorsForTest(tc.problem, ""))
		})
	}
}

// TestExtractAnchorSet_ImpreciseSpanMarksTruncated pins the three release-blockers
// that `--post` round 4 measured against the boundary-rule repair. All three have
// ONE consequence and ONE mechanism.
//
// The consequence is always the same: extractAnchorSet returns an anchor set that
// is not a faithful reading of what the text named, validate.go:143 sees
// `!problemTruncated`, and a REAL finding is routed to the unresolved sidecar —
// gate.go deletes it and scorecard.go durably charges the reviewer a phantom.
//
// The mechanism is the `truncated` contract at extractAnchorSet's doc: a caller
// may never reach a no-match verdict on a set that is a PREFIX of what the text
// named. Before this test the cap at anchor.go:84-87 was the ONLY thing that could
// set that flag, and the call scan had acquired two more ways to lose fidelity:
//
//  1. GLUED (the anchor.go:158 blocker). Two adjacent spaceless scripts no longer
//     break the run, so ordinary prose in such a script is re-glued onto a
//     snake_case call name — `設定を解析_処理()` yields the glued
//     `設定を解析_処理` where the tree declares `解析_処理`. Measured old-vs-new:
//     resolve went tier4Resolved -> tier4NoMatch, i.e. the BEST outcome was
//     replaced by the WORST one.
//
//  2. SILENCED (the anchor.go:169 and :171 blockers). The undecidable-underscore
//     suppression contributes no anchor for that span. Its safety argument is
//     per-FINDING ("no anchor keeps the finding") but the suppression is
//     per-SPAN, so when a co-cited anchor exists and is absent from the tree,
//     silencing the one span that WOULD have matched flips the whole finding to
//     no-match. The same silence also shrinks the set below the cap, flipping
//     `truncated` from true to false on an otherwise identical anchor list.
//
// Neither case may be answered by dropping the anchor: `データ_解析` and
// `解析_処理` are indistinguishable by any rule the text supports, and suppressing
// the glued form would take the round-3 counter-direction (`データ_解析()` yields
// the WHOLE name) back out. The answer is to keep the anchor and mark the
// EXTRACTION imprecise, which blocks the no-match verdict while leaving the
// resolution direction intact.
func TestExtractAnchorSet_ImpreciseSpanMarksTruncated(t *testing.T) {
	han := string([]rune{0x89E3, 0x6790})                                // 解析
	proseHan := string([]rune{0x8A2D, 0x5B9A, 0x3092})                   // 設定を
	suffix := string([]rune{0x51E6, 0x7406})                             // 処理
	kata := string([]rune{0x30C7, 0x30FC, 0x30BF})                       // データ
	setteiUnd := string([]rune{0x8A2D, 0x5B9A, 0x005F})                  // 設定_
	prolonged := string(rune(0x30FC))                                    // ー (Script=Common, neutral)
	katamodule := string([]rune{0x30E2, 0x30B8, 0x30E5, 0x30FC, 0x30EB}) // モジュール
	settei := string([]rune{0x8A2D, 0x5B9A})                             // 設定

	cases := []struct {
		name          string
		text          string
		wantAnchors   []string
		wantTruncated bool
	}{
		{
			// GLUED: the anchor survives (the tree may well declare it), but the
			// set is no longer a faithful reading, so it may not ground a no-match.
			name:          "spaceless prose glued to a snake_case name marks the set imprecise",
			text:          proseHan + han + "_" + suffix + "() drops the error",
			wantAnchors:   []string{proseHan + han + "_" + suffix},
			wantTruncated: true,
		},
		{
			// Same shape, and the reason the glued case may NOT be suppressed:
			// this one is a single real identifier and must keep yielding whole.
			name:          "a genuine spaceless snake_case name is whole and also imprecise",
			text:          kata + "_" + han + "() drops the error",
			wantAnchors:   []string{kata + "_" + han},
			wantTruncated: true,
		},
		{
			// SILENCED: the undecidable underscore contributes no anchor, and that
			// loss must be reported so a co-cited absent anchor cannot route the
			// finding out on its own.
			name:          "a silenced undecidable span marks the set imprecise",
			text:          setteiUnd + "loadFile() ignores the deadline set by `retryOnce`",
			wantAnchors:   []string{"retryOnce"},
			wantTruncated: true,
		},
		{
			// Counter-direction: an ordinary ASCII call scan loses nothing, so the
			// flag must stay false or every finding becomes unroutable.
			name:          "an ordinary call scan is not imprecise",
			text:          "ParseConfig() drops the error",
			wantAnchors:   []string{"ParseConfig"},
			wantTruncated: false,
		},
		{
			// Pins the CROSSING half of the glued predicate, and it is the
			// load-bearing direction: snake_case is the most ordinary
			// identifier style there is, so marking every underscore-bearing
			// span imprecise would make no-match routing unreachable for most
			// findings in most repositories. The underscore only means
			// "undecidable" when a spaceless-script crossing put it there.
			name:          "an ASCII snake_case call is not imprecise",
			text:          "read_tree() is invoked once per finding",
			wantAnchors:   []string{"read_tree"},
			wantTruncated: false,
		},
		{
			// Pins the UNDERSCORE half of the glued predicate. A spaceless
			// crossing with no underscore cannot carry a signal through a
			// caseless run, so hasIdentifierSignal rejects the whole span and
			// nothing is contributed - no loss to report. Marking this
			// imprecise would let a span that produced no evidence suppress a
			// no-match verdict for the rest of the finding.
			name:          "a spaceless crossing with no underscore is not imprecise",
			text:          string([]rune{0x8A2D, 0x5B9A, 0x3092, 0x89E3, 0x6790, 0x51E6, 0x7406}) + "() drops the error",
			wantAnchors:   nil,
			wantTruncated: false,
		},
		{
			// Pins the empty-set return: a finding whose ONLY span was silenced
			// still lost that span, and must say so. The anchor list and the
			// flag are independent - an empty list is not evidence of a
			// complete search.
			name:          "a silenced span is imprecise even when it was the only one",
			text:          string([]rune{0x8C03, 0x7528, 0x005F}) + "ParseConfig() failed",
			wantAnchors:   nil,
			wantTruncated: true,
		},
		{
			// Counter-direction: prose glued across a spaceless/spacing boundary
			// still terminates cleanly and loses nothing.
			name:          "a clean spaceless/spacing boundary is not imprecise",
			text:          string([]rune{0x5728, 0x005F, 0x914D, 0x7F6E, 0x4E2D, 0x8C03, 0x7528}) + "ParseConfig() timed out",
			wantAnchors:   []string{"ParseConfig"},
			wantTruncated: false,
		},
		{
			// The suppression must read the anchor that is actually RECORDED,
			// not the first byte of the raw span. U+30FC is script-neutral, so
			// the break lands ON it and text[start] is its lead byte rather than
			// the underscore one rune further in. Measured before the fix:
			// the fragment `ー_解析` escaped as a confident anchor.
			name:          "a script-neutral rune before the underscore does not let the fragment escape",
			text:          "parse" + prolonged + "_" + han + "() drops the error",
			wantAnchors:   nil,
			wantTruncated: true,
		},
		{
			// Same predicate one level down: the raw span begins with the
			// qualifier separator, so the raw-byte test misses, but
			// trailingSegment strips it and the RECORDED anchor is the leading-
			// underscore fragment `_解析`. Measured before the fix: it escaped.
			name:          "a stripped qualifier before the underscore does not let the fragment escape",
			text:          "parse._" + han + "() drops the error",
			wantAnchors:   nil,
			wantTruncated: true,
		},
		{
			// The glued guard must also read the RECORDED anchor. Here the
			// underscore lives only in the qualifier trailingSegment strips, and
			// the remaining `解析` carries no identifier signal, so the span
			// contributes nothing at all. Marking the set imprecise on a span
			// that contributed nothing refuses a no-match verdict for a set whose
			// every member is a faithful reading - which keeps a fabricated
			// finding out of the sidecar and out of the scorecard denominator.
			name:          "an underscore only in the stripped qualifier is not imprecise",
			text:          kata + "_" + katamodule + "." + han + "() is wrong, see `retryOnce`",
			wantAnchors:   []string{"retryOnce"},
			wantTruncated: false,
		},
		{
			// The silence's flags must also ask whether the silenced fragment
			// could ever have contributed an anchor. `_解` is 2 runes, so
			// isIdentifierShaped rejects it (minAnchorLen) no matter what the
			// reviewer meant - silencing it lost nothing, and reporting the set
			// unfaithful would refuse a no-match verdict on a set whose every
			// member IS a faithful reading.
			name:          "a silenced fragment that could never qualify is not a loss",
			text:          "`retryOnce` `parseTree` then parse_" + string(rune(0x89E3)) + "() drops the error",
			wantAnchors:   []string{"parseTree", "retryOnce"},
			wantTruncated: false,
		},
		{
			// The row above asks the question of the FRAGMENT, and that is only
			// the right question when the fragment is what the break could have
			// cost. Here it is not: the break split a SPACELESS-script prefix
			// off a 1-2 rune tail, so the fragment `_a` fails minAnchorLen while
			// the full span `設定_a` is identifier-shaped, carries an underscore
			// signal, and is perfectly searchable. Silencing it deletes a name
			// the tree may well declare, and reporting the set faithful lets a
			// co-cited absent anchor route the whole finding out.
			//
			// The prefix's script is what separates this from the row above.
			// `parse_解` breaks the other way (a SPACING-script prefix), which
			// is the ordinary "a Latin word ran into a name" reading the
			// fragment question answers correctly.
			name:          "a spaceless prefix split off a short tail is a loss",
			text:          "the " + settei + "_a() helper drops the error that `retryOnce` returns",
			wantAnchors:   []string{"retryOnce"},
			wantTruncated: true,
		},
		{
			// Same shape in Hangul, and with the tail on the other side of the
			// minAnchorLen boundary: the fragment `_i` is two runes, the full
			// span `버퍼_i` is four.
			name:          "a Hangul prefix split off a short tail is a loss",
			text:          string([]rune{0xBC84, 0xD37C}) + "_i() drops the error that `retryOnce` returns",
			wantAnchors:   []string{"retryOnce"},
			wantTruncated: true,
		},
		{
			// The suppression's other direction, and the one it must NOT take:
			// the raw span begins with '_' but the RECORDED anchor does not,
			// because a qualifier follows the underscore. Both readings of
			// `設定_pkg.loadFile()` — prose plus `_pkg.loadFile`, or the single
			// name `設定_pkg` qualifying `loadFile` — reduce to the same trailing
			// segment, so `loadFile` is a faithful reading either way and there
			// is nothing to report. Measured against the raw-span test: the span
			// was silenced and the set marked imprecise, losing a faithful anchor
			// to a guard aimed at fragments.
			name:          "a qualifier after the underscore leaves a faithful anchor",
			text:          settei + "_pkg.loadFile() drops the error",
			wantAnchors:   []string{"loadFile"},
			wantTruncated: false,
		},
		{
			// An NFD combining mark sitting where the break lands. Before the
			// recorded-anchor repair this span was NOT suppressed by the guard
			// at all: the break landed on U+0301, text[start] was its lead byte,
			// and the fragment was saved from escaping only by isIdentifierShaped
			// rejecting a leading Mn - a different predicate, reached by luck.
			// The set was therefore reported as a CLEAN read of a span it had
			// silently lost, which is the shape of the three blockers the parent
			// branch closed.
			//
			// Measured 12ffd73 -> HEAD: (nil, false) -> (nil, true). The anchor
			// list is unchanged; the honesty of the flag is the whole delta, so
			// that is what this row asserts.
			//
			// NOT a mutation guard for spacelessScriptOf's !IsLetter clause:
			// under that mutant the break lands one rune LATER (on the
			// underscore, since the mark is no longer transparent), the guard
			// fires by the ordinary leading-underscore path, and the result is
			// identical. Measured. The clause is pinned in
			// TestExtractAnchors_SnakeCaseSpacelessNameSurvivesBoundary instead.
			name:          "a combining mark where the break lands is a loss, and says so",
			text:          "cafe" + string(rune(0x0301)) + "_" + han + "() drops the error",
			wantAnchors:   nil,
			wantTruncated: true,
		},
		{
			// The QUALIFIED spelling of the row above ("a spaceless prefix split
			// off a short tail is a loss"). isQualifiedIdentRune admits '.', so
			// fullRunStart walks straight back THROUGH the qualifier and the raw
			// full run is `pkg.設定_a` — which isIdentifierShaped rejects on the
			// '.', clearing the loss and reporting a set it silently truncated.
			// The full-run question must be asked of the same reduction the
			// fragment path one branch up already asks of (recordedAnchorForm),
			// so the two halves of one guard agree on which string they mean.
			//
			// Measured 12ffd732 (merge-base) -> HEAD: truncated TRUE -> FALSE,
			// i.e. the bare-form repair did not reach the qualified form.
			name:          "a qualified spaceless prefix split off a short tail is a loss",
			text:          "the pkg." + settei + "_a() helper drops the error that `retryOnce` returns",
			wantAnchors:   []string{"retryOnce"},
			wantTruncated: true,
		},
		{
			// Same, with the qualifier in the SAME script as the prefix, so
			// nothing about the boundary rule can be credited for the repair.
			name:          "a same-script qualifier before a spaceless prefix is still a loss",
			text:          "the " + settei + "." + settei + "_a() helper drops the error that `retryOnce` returns",
			wantAnchors:   []string{"retryOnce"},
			wantTruncated: true,
		},
		{
			// A receiver-qualified spelling with a different Han name, pinning
			// that the repair is the qualifier strip and not a property of the
			// particular identifier.
			name:          "a receiver-qualified spaceless prefix split off a short tail is a loss",
			text:          "obj." + han + "_x() drops the error that `retryOnce` returns",
			wantAnchors:   []string{"retryOnce"},
			wantTruncated: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := extractAnchorSet(tc.text)
			assert.Equal(t, tc.wantAnchors, got)
			assert.Equal(t, tc.wantTruncated, truncated, "truncated flag")
		})
	}
}

// TestExtractAnchorSet_SilencedSpanKeepsTruncatedAtCap pins the anchor.go:171
// blocker on its own axis: the silence must not be able to shrink a set THROUGH
// the maxAnchorsPerFinding cap and clear the flag on the way down.
//
// Measured before the fix: nine named identifiers where the ninth is an ordinary
// ASCII call gives 8 anchors with truncated=TRUE; the SAME nine where the ninth
// is a silenced span gives the SAME 8 anchors with truncated=FALSE. Identical
// anchor lists, opposite routing decisions.
func TestExtractAnchorSet_SilencedSpanKeepsTruncatedAtCap(t *testing.T) {
	base := "`aOne` `bTwo` `cThree` `dFour` `eFive` `fSix` `gSeven` `hEight` "
	eight := []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight"}
	setteiUnd := string([]rune{0x8A2D, 0x5B9A, 0x005F}) // 設定_

	ordinary, ordinaryTruncated := extractAnchorSet(base + "loadFile() also")
	silenced, silencedTruncated := extractAnchorSet(base + setteiUnd + "loadFile() also")

	assert.Equal(t, eight, ordinary)
	assert.True(t, ordinaryTruncated, "cap alone must report truncation")
	assert.Equal(t, eight, silenced)
	assert.True(t, silencedTruncated, "a silenced ninth anchor is still a loss, not a smaller set")
}

// TestExtractFixAnchors_DropsOnlyTheImpreciseMembers pins the consumer half of
// the imprecise seam: the per-anchor narrowing extractFixAnchors performs. The
// full cap-vs-per-span-vs-unaccounted argument (and the measured examples) lives
// in extractFixAnchors' doc — these rows pin that the code honors it, they do
// not restate it.
func TestExtractFixAnchors_DropsOnlyTheImpreciseMembers(t *testing.T) {
	han := string([]rune{0x89E3, 0x6790})               // 解析
	kata := string([]rune{0x30C7, 0x30FC, 0x30BF})      // データ
	setteiUnd := string([]rune{0x8A2D, 0x5B9A, 0x005F}) // 設定_
	genuine := kata + "_" + han                         // データ_解析

	t.Run("a glued member is dropped and the precise members survive", func(t *testing.T) {
		got := extractFixAnchors("call `parseTree` instead of " + genuine + "()")
		assert.Equal(t, []string{"parseTree"}, got)

		all, truncated := extractAnchorSet("call `parseTree` instead of " + genuine + "()")
		require.Equal(t, []string{"parseTree", genuine}, all, "both anchors are still extracted")
		require.True(t, truncated, "and the SET is still imprecise - only the FIX consumer narrows")
	})

	t.Run("a silenced span abandons the set, like the cap", func(t *testing.T) {
		text := setteiUnd + "loadFile() - see `retryOnce` and `parseTree`"

		all, truncated := extractAnchorSet(text)
		require.Equal(t, []string{"parseTree", "retryOnce"}, all,
			"the silenced span contributes no member, so every anchor present IS faithful")
		require.True(t, truncated)

		assert.Nil(t, extractFixAnchors(text),
			"faithful is not the same as complete: locate refuses when two precise anchors "+
				"disagree, so its verdict needs the whole set, and the silenced span is exactly "+
				"the one whose answer is unknowable")
	})

	t.Run("a glued span whose token fails the shape test abandons the set, like a silence", func(t *testing.T) {
		// A leading combining mark makes the recorded token unshaped, so the
		// glued span contributes NO member - the one arm that discards the
		// whole FIX set without a member to drop.
		text := string(rune(0x0301)) + string([]rune{0x8A2D, 0x5B9A, 0x3092, 0x89E3, 0x6790, 0x005F, 0x51E6, 0x7406}) + "() drops the error - see `parseTree`"

		all, truncated := extractAnchorSet(text)
		require.Equal(t, []string{"parseTree"}, all, "the co-cited backticked anchor survives")
		require.True(t, truncated, "the glued span still marks the set imprecise")

		assert.Nil(t, extractFixAnchors(text),
			"the glued token failed the shape test, so no member records the loss - same standing as a silence")
	})

	t.Run("a backticked citation survives an incidental glued call of the same name", func(t *testing.T) {
		text := "call `" + genuine + "` here, as in " + genuine + "() below"
		assert.Equal(t, []string{genuine}, extractFixAnchors(text),
			"the backticked citation is a clean contribution: an incidental glued call of the same name must not take it down")
	})

	t.Run("a dropped member is kept as evidence, not discarded", func(t *testing.T) {
		usable, scan := scanFixAnchors("call `parseTree` instead of " + genuine + "()")
		require.Equal(t, []string{"parseTree"}, usable)
		assert.Equal(t, []string{genuine}, scan.droppedFixAnchors(),
			"a member dropped from the usable set is still part of what the FIX named: "+
				"locate refuses when two precise anchors disagree, so its verdict needs "+
				"the dropped name too - the same completeness the unaccounted arm nils the set for")
	})

	t.Run("a member the cap dropped is not reported as a per-anchor drop", func(t *testing.T) {
		// imprecise is keyed on what the scan recorded, so it can name a token
		// the cap later removed from anchors. Such a token is not a per-anchor
		// drop - the whole set was already abandoned - and reporting it would
		// hand the disagreement check a name the usable set never excluded.
		text := "`aOne` `bTwo` `cThree` `dFour` `eFive` `fSix` `gSeven` `hEight` " + genuine + "()"
		usable, scan := scanFixAnchors(text)
		require.Nil(t, usable, "the cap abandons the set whole")
		require.True(t, scan.capped, "the fixture must actually cap")
		assert.Nil(t, scan.droppedFixAnchors(),
			"nothing was narrowed away: the set was abandoned, so there is no per-anchor drop to report")
	})

	t.Run("an ordinary FIX keeps its whole set", func(t *testing.T) {
		assert.Equal(t, []string{"parseTree", "readTree"},
			extractFixAnchors("call `readTree` then `parseTree`"))
	})

	t.Run("the cap abandons the set whole", func(t *testing.T) {
		text := "`aOne` `bTwo` `cThree` `dFour` `eFive` `fSix` `gSeven` `hEight` `iNine`"
		require.Len(t, mustAnchorsOnly(t, text), maxAnchorsPerFinding, "the fixture must actually cap")
		assert.Nil(t, extractFixAnchors(text),
			"a prefix of what the FIX named cannot ground a suggestion: the dropped anchors are unknown")
	})

	t.Run("a FIX whose every member is imprecise yields nothing", func(t *testing.T) {
		assert.Nil(t, extractFixAnchors(genuine+"()"))
	})
}

// mustAnchorsOnly is mustAnchors minus the flag: it returns the anchor set and
// DELIBERATELY drops `truncated`. Callers that assert on the flag must call
// extractAnchorSet themselves - the `Only` suffix exists so a future fixture
// edit cannot silently degrade the cap this helper's require.Len guards.
func mustAnchorsOnly(t *testing.T, text string) []string {
	t.Helper()
	a, _ := extractAnchorSet(text)
	return a
}

// TestLeadsWithUnderscore pins the predicate directly: the loop-exhaustion
// `return false` is reachable in production (a qualified span whose trailing
// segment is all script-neutral runes, e.g. a digit run) and was previously
// unpinned - flipping it to `return true` left the suite green.
func TestLeadsWithUnderscore(t *testing.T) {
	cases := []struct {
		tok  string
		want bool
	}{
		{"", false},
		{"9", false},     // all script-neutral, no underscore: loop exhausts
		{"_", true},      //
		{"ー_load", true}, // U+30FC is script-neutral: skipped, then '_'
		{"́_x", true},    // a combining mark is neutral too
		{"x_", false},    // a scripted rune first: a real name, not an orphan '_'
		{"_x", true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, leadsWithUnderscore(tc.tok), "%q", tc.tok)
	}
}

// TestDroppedFixAnchors_AbandonedArmsWithhold pins droppedFixAnchors' `s.capped
// || s.unaccounted` disjuncts, which survive mutation: removing both and leaving
// only `len(s.imprecise) == 0` left `go test ./internal/reconcile/` green.
//
// They survive because scanFixAnchors nils the anchors on both arms, so
// locate(secondary) at symbolindex.go:264 can never succeed and there is nothing
// for the veto to withhold — the disjuncts are a defensive assertion with no
// reachable consumer today. That is exactly why no test can reach them through
// the public seam, and exactly why one has to be built at the unit level: a
// later change that let a capped scan keep its anchors would silently start
// feeding cap-era dropped names to the veto with no test objecting.
//
// The literals below are therefore deliberate: an anchorScan carrying BOTH
// anchors and a populated imprecise map alongside capped/unaccounted is a state
// scanFixAnchors does not produce, and constructing it is the point.
func TestDroppedFixAnchors_AbandonedArmsWithhold(t *testing.T) {
	populated := func(capped, unaccounted bool) anchorScan {
		return anchorScan{
			anchors:     []string{"dataParse", "treeWalk"},
			capped:      capped,
			unaccounted: unaccounted,
			imprecise:   map[string]struct{}{"dataParse": {}},
		}
	}

	t.Run("a narrowed scan reports its dropped members", func(t *testing.T) {
		assert.Equal(t, []string{"dataParse"}, populated(false, false).droppedFixAnchors(),
			"the baseline the two arms below must differ from, or the test proves nothing")
	})

	t.Run("a capped scan withholds them", func(t *testing.T) {
		assert.Nil(t, populated(true, false).droppedFixAnchors(),
			"the cap abandons the set whole: nothing was narrowed away, and there is no located file to contradict")
	})

	t.Run("an unaccounted scan withholds them", func(t *testing.T) {
		assert.Nil(t, populated(false, true).droppedFixAnchors(),
			"a member-less loss abandons the set whole, with the cap's standing")
	})

	t.Run("both at once withholds them", func(t *testing.T) {
		assert.Nil(t, populated(true, true).droppedFixAnchors(),
			"the two losses are independent and either one alone is sufficient")
	})

	t.Run("an empty imprecise map has nothing to report", func(t *testing.T) {
		assert.Nil(t, anchorScan{anchors: []string{"treeWalk"}}.droppedFixAnchors(),
			"the third disjunct: no member was narrowed out")
	})
}

// TestScanAnchors_SilencedSpanReconciledAgainstClean pins the PROBLEM-side half
// of the completeness argument that scanAnchors already makes for `imprecise`.
//
// `unaccounted` means "a fidelity loss left NO member behind, so what that span
// would have named is unknowable". That claim is false the moment the SAME text
// cites the destroyed name faithfully somewhere else: the name is not unknowable,
// it is sitting in `scan.anchors`, and the counters and the withheld
// PathSuggestion built on the flag inherit the error.
//
// scanAnchors already resolves exactly this conflict for `imprecise` — a token a
// clean span also contributed is not imprecise (the delete-loop below the call
// scan). This is that loop's counterpart for `unaccounted`, and it is written the
// same way and in the same place for the same reason: `clean` is still being
// filled WHILE collectCallAnchors runs, so a decision taken inline would depend
// on whether the clean citation happened to appear before or after the silenced
// span in the text.
//
// Measured at the parent branch's HEAD, both rows below reported
// `unaccounted=true`; only the second one should.
func TestScanAnchors_SilencedSpanReconciledAgainstClean(t *testing.T) {
	settei := string([]rune{0x8A2D, 0x5B9A}) // 設定
	han := string([]rune{0x89E3, 0x6790})    // 解析
	name := settei + "_a"                    // 設定_a
	// 設定を解析_処理 — spaceless prose welded to a snake_case call name.
	glued := string([]rune{0x8A2D, 0x5B9A, 0x3092, 0x89E3, 0x6790, 0x005F, 0x51E6, 0x7406})

	cases := []struct {
		name            string
		text            string
		wantUnaccounted bool
		wantAnchor      string
		why             string
	}{
		{
			name:            "backticked before the qualified call",
			text:            "`" + name + "` is broken; pkg." + name + "() returns nil",
			wantUnaccounted: false,
			wantAnchor:      name,
			why: "the destroyed name was cited cleanly two words earlier and is " +
				"already in the anchor set: nothing about it is unknowable",
		},
		{
			name:            "backticked AFTER the qualified call",
			text:            "pkg." + name + "() returns nil; see `" + name + "`",
			wantUnaccounted: false,
			wantAnchor:      name,
			why: "order must not decide it — the reconciliation runs after the " +
				"scan, not inline where `clean` is still being filled",
		},
		{
			name:            "cited cleanly by a QUOTED span, not a backticked one",
			text:            `pkg.` + name + `() returns nil, see "` + name + `"`,
			wantUnaccounted: false,
			wantAnchor:      name,
			why: "every delimiter collectDelimitedAnchors scans contributes to `clean`, " +
				"so the reconciliation must key on the set and not on the backtick",
		},
		{
			name:            "the same name called again, still spaceless-broken",
			text:            "pkg." + name + "() returns nil, unlike " + name + "()",
			wantUnaccounted: true,
			wantAnchor:      "",
			why: "a bare call of the same name is silenced by the SAME boundary rule, " +
				"so it is not a clean citation and cannot vouch for the loss " +
				"(measured: anchors=[] for this text)",
		},
		{
			name:            "no clean citation anywhere in the text",
			text:            "pkg." + name + "() returns nil",
			wantUnaccounted: true,
			wantAnchor:      "",
			why: "nothing contradicts the loss, so it stands: this is the row the " +
				"silence guard exists for",
		},
		{
			name:            "a DIFFERENT name is cited cleanly",
			text:            "pkg." + name + "() returns nil, see `retryOnce`",
			wantUnaccounted: true,
			wantAnchor:      "retryOnce",
			why: "reconciliation is per-TOKEN, not per-scan: a clean citation of " +
				"some other name says nothing about what the break destroyed",
		},
		{
			name:            "the FRAGMENT is cited cleanly, not the destroyed name",
			text:            "`_a` is odd; pkg." + name + "() returns nil",
			wantUnaccounted: true,
			wantAnchor:      "",
			why: "the record is keyed on the FULL run's trailing segment, which is " +
				"what the break destroyed — vouching for the boundary-reduced " +
				"fragment vouches for nothing",
		},
		{
			name:            "the QUALIFIER is cited cleanly, not the destroyed name",
			text:            "`pkg` is odd; pkg." + name + "() returns nil",
			wantUnaccounted: true,
			wantAnchor:      "",
			why: "trailingSegment strips the qualifier before the record is keyed, so " +
				"a clean citation of `pkg` cannot vouch for " + name,
		},
		{
			name: "the vouching token is dropped by the anchor cap",
			text: "`aOne` `bTwo` `cThree` `dFour` `eFive` `fSix` `gSeven` `hEight` " +
				"`" + name + "` broken; pkg." + name + "() returns nil",
			wantUnaccounted: true,
			wantAnchor:      "aOne",
			why: "the citation must vouch from the set locate is actually GIVEN. " +
				"maxAnchorsPerFinding slices the sorted set to 8 and " + name +
				" sorts after every ASCII name, so it is cited cleanly and then " +
				"thrown away - suppressing on it would stamp a suggestion sourced " +
				"from the survivors alone, which is the wrong-file answer the " +
				"complete set refused",
		},
		{
			name:            "the FRAGMENT the predicate judged is the one cited cleanly",
			text:            "`_" + han + "` is broken; parse_" + han + "() fails",
			wantUnaccounted: false,
			wantAnchor:      "_" + han,
			why: "this span is silenced by the FRAGMENT disjunct, which judges `_解析` " +
				"and not the full run - so `_解析` is what the loss destroyed, it is " +
				"cited in backticks, and the epic's success criterion (a name the " +
				"reviewer cited cleanly is never an unknowable loss) applies to it",
		},
		{
			name:            "the surviving anchor is GLUED, so it vouches for nothing",
			text:            glued + "() is wrong and pkg." + name + "() returns nil",
			wantUnaccounted: true,
			wantAnchor:      glued,
			why: "membership in the anchor set is necessary but not sufficient: a glued " +
				"span's token may be an unfaithful reading of what the reviewer " +
				"wrote, so it is in `anchors` and `imprecise` but never in `clean` " +
				"and may not retract anyone's loss - least of all its own",
		},
		{
			name:            "two silenced spans, only one of them vouched for",
			text:            "`" + name + "` broken; pkg." + name + "() and parse._" + han + "()",
			wantUnaccounted: true,
			wantAnchor:      name,
			why: "the record is a SET, so subtracting `clean` clears only the vouched " +
				"token; the second span's loss is still unknowable and the flag " +
				"must still stand — a per-scan boolean would have collapsed here",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := scanAnchors(tc.text)

			assert.Equal(t, tc.wantUnaccounted, s.unaccounted, tc.why)

			// lostSpan is deliberately NOT reconciled. `truncated` is built from
			// it and the no-match direction reads that, so clearing it here
			// would make tier4NoMatch reachable where it was not before — the
			// one thing this repair must never do.
			assert.True(t, s.lostSpan,
				"the span really did lose fidelity; only the UNKNOWABILITY claim is retracted")

			if tc.wantAnchor != "" {
				assert.Contains(t, s.anchors, tc.wantAnchor)
			}
		})
	}
}

// TestScanFixAnchors_SilencedSpanReconciledAgainstClean is the FIX-side
// consequence of the reconciliation above, and it needs no separate repair:
// scanFixAnchors abandons the whole set on `s.unaccounted`, so a PROBLEM-side
// correction of that flag reaches the FIX side through the same field.
//
// Measured at the parent branch's HEAD this text yielded `fixAnchors=[]` while
// `fixScan.anchors` held the intact precise anchor — an anchor discarded, and
// atcr_tier4_fix_set_unaccounted_total incremented, for a name the text spelled
// out faithfully.
func TestScanFixAnchors_SilencedSpanReconciledAgainstClean(t *testing.T) {
	settei := string([]rune{0x8A2D, 0x5B9A}) // 設定
	name := settei + "_a"
	text := "`" + name + "` is broken; pkg." + name + "() returns nil"

	fixAnchors, s := scanFixAnchors(text)

	require.False(t, s.unaccounted, "precondition: the loss is reconciled away")
	assert.Equal(t, []string{name}, fixAnchors,
		"the set is no longer abandoned whole, so the intact precise anchor survives")
}
