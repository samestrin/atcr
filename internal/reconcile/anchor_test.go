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
// `データ_解析` was broken by U+30FC, the katakana-hiragana prolonged sound
// mark, which is Script=Common and which the original rule classified as
// scriptSpacing — splitting the name at its own vowel mark, with no real script
// change anywhere in it. That mark appears in most everyday Japanese loanword
// identifiers (データ, ユーザー, サーバー, ロード, パーサー), so the case is
// ordinary rather than exotic. spacelessScriptOf now returns scriptNeutral for
// it.
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
	han := string([]rune{0x89E3, 0x6790})               // 解析
	proseHan := string([]rune{0x8A2D, 0x5B9A, 0x3092})  // 設定を
	suffix := string([]rune{0x51E6, 0x7406})            // 処理
	kata := string([]rune{0x30C7, 0x30FC, 0x30BF})      // データ
	setteiUnd := string([]rune{0x8A2D, 0x5B9A, 0x005F}) // 設定_

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
			// Counter-direction: prose glued across a spaceless/spacing boundary
			// still terminates cleanly and loses nothing.
			name:          "a clean spaceless/spacing boundary is not imprecise",
			text:          string([]rune{0x5728, 0x005F, 0x914D, 0x7F6E, 0x4E2D, 0x8C03, 0x7528}) + "ParseConfig() timed out",
			wantAnchors:   []string{"ParseConfig"},
			wantTruncated: false,
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
