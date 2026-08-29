package reconcile

import (
	"sort"
	"strings"
	"unicode"
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

// extractAnchors is the Tier 4 (Epic 35.16.6.5 T1) deterministic anchor
// extractor: given a finding's PROBLEM and FIX prose it returns the
// identifier-shaped tokens the finding appears to be talking about, which the
// repo-wide symbol index (T2) is then searched for.
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
func extractAnchors(problem, fix string) []string {
	seen := make(map[string]struct{})
	for _, text := range [...]string{problem, fix} {
		for _, d := range anchorDelimiters {
			collectDelimitedAnchors(text, byte(d), seen)
		}
		collectCallAnchors(text, seen)
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
// qualifying anchor from each. An unterminated opener contributes nothing and
// does NOT abort the scan — a lone apostrophe mid-sentence must not suppress
// the well-formed spans after it.
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
		for start > 0 && isQualifiedIdentByte(text[start-1]) {
			start--
		}
		if start == i {
			continue // "(" with no identifier before it
		}
		addAnchor(text[start:i], seen)
	}
}

// addAnchor normalizes one raw span and records it if it qualifies.
func addAnchor(raw string, seen map[string]struct{}) {
	tok := trailingSegment(strings.TrimSpace(raw))
	if !isIdentifierShaped(tok) || !hasIdentifierSignal(tok) {
		return
	}
	seen[tok] = struct{}{}
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

// isQualifiedIdentByte reports whether b may appear in a qualified identifier
// run scanned backwards from a call paren. '.' is included so a package- or
// receiver-qualified call is captured whole and reduced by trailingSegment.
func isQualifiedIdentByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '_', b == '.':
		return true
	}
	return false
}

// isIdentifierShaped reports whether tok is a bare identifier of usable length:
// an ASCII letter or underscore followed by letters, digits, or underscores.
// A span containing whitespace, punctuation, or any other character (a quoted
// English phrase, a sentence fragment, a path) fails here.
func isIdentifierShaped(tok string) bool {
	if len(tok) < minAnchorLen {
		return false
	}
	for i := 0; i < len(tok); i++ {
		b := tok[i]
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b == '_':
		case b >= '0' && b <= '9':
			if i == 0 {
				return false // an identifier never starts with a digit
			}
		default:
			return false
		}
	}
	return true
}

// hasIdentifierSignal reports whether an identifier-shaped token actually looks
// like code rather than an ordinary English word that happened to be quoted.
// The signal is a case transition (camelCase, PascalCase) or an underscore
// (snake_case) — the two conventions that separate a declared name from prose.
//
// A single all-lowercase word (`handler`, `error`, `root`) carries no signal and
// is rejected even inside backticks. That is deliberately conservative: such a
// word matches a symbol somewhere in almost any tree, and a spurious lone match
// would promote to a confident wrong PathSuggestion. The cost is a missed
// Tier 4 resolution for a finding that names only a lowercase symbol, which
// degrades to today's Tier 1-3 behavior — never to a wrong answer.
func hasIdentifierSignal(tok string) bool {
	if strings.Contains(tok, "_") {
		return true
	}
	hasUpper, hasLower := false, false
	for _, r := range tok {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		}
	}
	return hasUpper && hasLower
}
