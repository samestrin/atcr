package reconcile

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxAnchorsPerFinding bounds how many anchors one finding contributes to a
// Tier 4 lookup. A finding naming more identifiers than this is almost
// certainly prose about a whole subsystem rather than a citation of one
// construct, and searching every token would trade a precise lookup for an
// index sweep. Truncation is applied AFTER the sort, so it drops the lexically-
// last anchors deterministically rather than whichever ones the scanner
// happened to reach first (AC2).
const maxAnchorsPerFinding = 8

// minAnchorLen is the shortest token accepted as an anchor. Two-character
// identifiers (id, ok, fn) are too common across a tree to localize a finding,
// and a spurious single match on one would produce a confident wrong
// PathSuggestion — the exact failure Tier 1-3 were tuned to avoid.
const minAnchorLen = 3

// extractAnchorSet is the Tier 4 (Epic 35.16.6.5 T1) deterministic anchor
// extractor: given ONE body of finding prose it returns the identifier-shaped
// tokens that text appears to be talking about, which the repo-wide symbol index
// (T2) is then searched for.
//
// PROBLEM and FIX are extracted SEPARATELY and are not interchangeable. A FIX
// routinely names a construct the reviewer wants CREATED ("extract the retry
// loop into `splitRetryHelper`"), which is absent from the tree by definition —
// so a FIX anchor can never be evidence that a finding is fabricated. Only
// PROBLEM anchors may authorize a no-match verdict; FIX anchors may still
// contribute a resolution when they name existing code. Merging them into one
// set, as an earlier revision did, made the proposed remedy's own name the
// evidence for discarding the finding that proposed it.
//
// truncated reports that more identifiers were named than maxAnchorsPerFinding
// admits, so the returned set is a PREFIX of what the text actually named. A
// caller must never reach a no-match verdict on a truncated set: the one anchor
// that would have matched may be among the dropped ones.
//
// It is a pure function of its inputs — no model call, no filesystem, no clock,
// no map-iteration order — so the same finding text always yields the same
// anchor set (AC2). A summarizing or interpreting pass here is exactly where a
// fabricated finding could be paraphrased into something that matches, which is
// why the extraction path is deliberately mechanical (mirroring 35.16.7's
// claim-ledger precedent).
//
// Three span kinds are scanned, in the order a reviewer is most likely to have
// marked an identifier deliberately:
//
//   - backtick spans   — `foo`, the conventional code span in reviewer prose
//   - quoted spans     — "foo" and 'foo'
//   - call shapes      — foo(, an identifier immediately followed by an open paren
//
// Each raw span is reduced to its trailing segment (stream.ValidatePath ->
// ValidatePath) because the symbol index keys on the declared name, not the
// qualified call site. The result is kept only if it is identifier-shaped AND
// carries an identifier signal (see hasIdentifierSignal): prose in quotes ("file
// not found") and bare lowercase English words (`handler`) are rejected, so a
// finding with no identifier-shaped text yields ZERO anchors rather than a noisy
// guess. Returning nothing is the correct answer there — the caller treats "no
// anchors" as "could not check", never as "checked and found nothing".
//
// The returned slice is deduped and lexically sorted; nil when nothing
// qualifies.
func extractAnchorSet(text string) (anchors []string, truncated bool) {
	seen := make(map[string]struct{})
	for _, d := range anchorDelimiters {
		collectDelimitedAnchors(text, byte(d), seen)
	}
	collectCallAnchors(text, seen)
	if len(seen) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(seen))
	for tok := range seen {
		out = append(out, tok)
	}
	sort.Strings(out)
	if len(out) > maxAnchorsPerFinding {
		return out[:maxAnchorsPerFinding], true
	}
	return out, false
}

// anchorDelimiters are the paired characters a reviewer uses to mark a literal
// identifier in prose. Each is its own closer, and each is scanned in its OWN
// pass over the text (see extractAnchors) rather than in one interleaved pass.
//
// The separate passes are load-bearing, not stylistic. Apostrophes are pervasive
// in ordinary review prose ("the parser's cache", "doesn't handle nil"), so a
// single interleaved scan mis-pairs them constantly, and each mis-paired span
// swallows every delimiter it spans — including the backtick spans that carry
// the real identifiers. Per-delimiter passes contain that damage to the quote
// pass alone: a mis-paired span there always contains whitespace, so it fails
// isIdentifierShaped and contributes nothing.
const anchorDelimiters = "`\"'"

// collectDelimitedAnchors scans text for spans delimited by d and records the
// qualifying anchor from each. It stops at an opener with no closer after it,
// because no further pair of d can exist past that point.
//
// That early stop is contained to THIS delimiter's pass. A lone apostrophe
// mid-sentence ends only the apostrophe pass; the backtick pass over the same
// text is unaffected and still finds the identifiers after it. That containment
// is the whole reason extractAnchorSet runs one pass per delimiter.
func collectDelimitedAnchors(text string, d byte, seen map[string]struct{}) {
	for i := 0; i < len(text); i++ {
		if text[i] != d {
			continue
		}
		close := strings.IndexByte(text[i+1:], d)
		if close < 0 {
			return // no closer remains anywhere after i: nothing left to pair
		}
		addAnchor(text[i+1:i+1+close], seen)
		i += close + 1 // resume after the closer, never inside the span
	}
}

// collectCallAnchors scans text for an identifier immediately followed by '(',
// the call shape a reviewer writes when naming a function without marking it up
// (BuildFileIndex() is called once per finding). The identifier run is read
// backwards from the paren, so a qualified call (x.Parse() ) still yields its
// trailing segment via addAnchor.
func collectCallAnchors(text string, seen map[string]struct{}) {
	for i := 0; i < len(text); i++ {
		if text[i] != '(' {
			continue
		}
		start := i
		for start > 0 {
			r, size := utf8.DecodeLastRuneInString(text[:start])
			if !isQualifiedIdentRune(r) {
				break
			}
			start -= size
		}
		if start == i {
			continue // "(" with no identifier before it
		}
		addAnchor(text[start:i], seen)
	}
}

// addAnchor normalizes one raw span and records it if it qualifies.
func addAnchor(raw string, seen map[string]struct{}) {
	tok := foldAnchorForm(trailingSegment(strings.TrimSpace(raw)))
	if !isIdentifierShaped(tok) || !hasIdentifierSignal(tok) {
		return
	}
	seen[tok] = struct{}{}
}

// foldAnchorForm puts one token in NFC, the single normalization form every
// anchor and every symbol-index key is stored under.
//
// It exists because a Tier 4 lookup is a byte-exact Go map lookup: `módulo` typed
// as o+U+0301 and `módulo` typed as U+00F3 are the SAME identifier to a compiler
// and to a human, and two different keys to a map. Source files carry whichever
// form their author's editor produced; reviewer models emit NFC. Left unfolded,
// an NFD-spelled name misses an NFC-built index, resolve returns tier4NoMatch for
// a construct plainly in the tree, and a real finding is deleted and charged to
// the reviewer as a phantom — an ordinary case in any Spanish, Portuguese,
// French, or Vietnamese codebase.
//
// NFC (never NFD) because it is what Go source is conventionally written in and
// what the models emit, so the common path is a no-op. Both sides must call this;
// folding either alone leaves exactly the mismatch it is meant to close.
//
// It is deliberately NOT a case fold: the anchor alphabet is case-sensitive
// (isIdentifierShaped and hasIdentifierSignal both read case), and folding case
// here would collide distinct declarations.
func foldAnchorForm(tok string) string {
	if norm.NFC.IsNormalString(tok) {
		return tok // overwhelmingly the common path: no allocation
	}
	return norm.NFC.String(tok)
}

// trailingSegment reduces a qualified reference to the declared name the symbol
// index keys on: "stream.ValidatePath" -> "ValidatePath", "Host::Parser" ->
// "Parser", "obj->run" -> "run". Separators are stripped left-to-right so the
// LAST segment wins regardless of which qualifier style the reviewer used.
func trailingSegment(s string) string {
	for _, sep := range [...]string{"->", "::", ".", ":", "#", "/"} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			s = s[i+len(sep):]
		}
	}
	return s
}

// isQualifiedIdentRune reports whether r may appear in a qualified identifier
// run scanned backwards from a call paren. The class is rune-based — letters,
// digits, combining marks (the same alphabet isIdentifierShaped admits) plus
// '_' — so a non-ASCII call name is captured WHOLE rather than truncated at a
// multibyte rune's continuation byte into a fragment that names something
// else. '.' is included so a package- or receiver-qualified call is captured
// whole and reduced by trailingSegment.
func isQualifiedIdentRune(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r):
		return true
	case unicode.In(r, unicode.Mn, unicode.Mc):
		return true
	case r == '_', r == '.':
		return true
	}
	return false
}

// isIdentifierShaped reports whether tok is a bare identifier of usable length:
// a letter or underscore followed by letters, digits, underscores, or combining
// marks. A span containing whitespace, punctuation, or any other character (a
// quoted English phrase, a sentence fragment, a path) fails here.
//
// Letters are tested with unicode.IsLetter, not an ASCII range. Go, Python and
// TypeScript all admit non-ASCII identifiers, and an ASCII-only test silently
// dropped them — which is worse than it sounds here, because a finding whose
// real subject was never extracted can still be judged "checked and found
// nothing" on whatever co-cited ASCII anchor happened to miss.
//
// Combining marks (Mn and Mc) are admitted at non-initial positions for the
// same reason: a macOS- or git-normalised file spells café as e + U+0301, and
// Devanagari names carry vowel signs (नाम is न + ा + म), so a mark-rejecting
// filter drops the name of a declaration the grammar (isDeclNameRune,
// symbolindex.go) admits — the two must agree, or a grammar-admitted
// declaration never reaches presentInSource. Admission here is only the SHAPE
// half, though: a caseless-script name still carries no identifier signal
// (hasIdentifierSignal), so it reaches present from the harvest but anchors a
// finding only when it also carries an underscore. Me (enclosing marks) stays
// rejected: it is outside ECMAScript ID_Continue, and a leading mark is never
// legal, exactly as a leading digit is not.
func isIdentifierShaped(tok string) bool {
	if utf8.RuneCountInString(tok) < minAnchorLen {
		return false
	}
	for i, r := range tok {
		switch {
		case unicode.IsLetter(r), r == '_':
		case unicode.IsDigit(r), unicode.In(r, unicode.Mn, unicode.Mc):
			if i == 0 {
				return false // an identifier never starts with a digit or combining mark
			}
		default:
			return false
		}
	}
	return true
}

// hasIdentifierSignal reports whether an identifier-shaped token actually looks
// like code rather than an ordinary English word that happened to be quoted.
// The signal is an INTERNAL case transition (camelCase, PascalCase past the
// first letter) or an underscore (snake_case) — the two conventions that
// separate a declared name from prose.
//
// The transition must be internal, at index > 0. A merely-capitalized single
// word — `Timeout`, `Error`, `Cannot`, `Unresolved` — is how reviewers quote
// user-facing strings and sentence-initial prose, and admitting it was doubly
// harmful: it manufactured no-match "evidence" out of English, and a spurious
// unique hit on a same-named symbol became a confidently wrong PathSuggestion.
//
// A single-case word (`handler`, `Close`, `Parse`) carries no signal and is
// rejected even inside backticks. That is deliberately conservative: such a word
// matches a symbol somewhere in almost any tree. The cost is a missed Tier 4
// resolution for a finding that names only a single-word symbol, which degrades
// to today's Tier 1-3 behavior — never to a wrong answer.
func hasIdentifierSignal(tok string) bool {
	if strings.Contains(tok, "_") {
		return true
	}
	prevLower := false
	for i, r := range tok {
		if i > 0 && prevLower && unicode.IsUpper(r) {
			return true
		}
		// Only a cased rune moves the transition tracker: an uncased rune
		// (a digit or a combining mark) carries prevLower across unchanged,
		// so the NFD spelling of a camelCase name (cafe + U+0301 + Bar) keeps
		// the signal its NFC spelling has.
		switch {
		case unicode.IsLower(r):
			prevLower = true
		case unicode.IsUpper(r):
			prevLower = false
		}
	}
	return false
}
