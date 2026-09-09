package payload

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// DefaultMaxClaimBytes is the default size cap for the commit-message text read
// into the claim ledger, in the style of DefaultMaxDiffBytes: a squashed branch
// or an imported history can carry a very long message, and an uncapped read
// would let it displace the diff it is supposed to be checked against. 64 KiB
// holds every realistic branch's messages; a maxBytes <= 0 means unlimited.
const DefaultMaxClaimBytes int64 = 64 * 1024

// commitRecordSep separates commit messages in the `git log -z` output. NUL is
// git's own record separator and is the one byte a commit message cannot carry,
// so message content cannot forge a boundary and split one commit's claims into
// two — which any printable sentinel, including ASCII RS, would allow an
// attacker-influenced message to do.
const commitRecordSep = "\x00"

// commitMessages returns the commit messages of base..head, oldest first, with
// merge commits excluded. truncated reports whether the byte cap shed anything;
// maxBytes <= 0 means unlimited.
//
// Merges are excluded because a merge commit's message is git's own boilerplate
// ("Merge branch 'x'"), not an assertion its author made about the diff. Every
// claim manufactured from one would be UNSUPPORTED by construction, which is
// the exact verdict the ledger exists to make meaningful.
//
// The cap sheds the OLDEST commits first: the assertion a reviewer most needs to
// adjudicate is the one the branch tip just made, so a cap that dropped from the
// tip would discard the claims most likely to matter. A single message larger
// than the whole cap is capped on a rune boundary rather than dropped, because
// an empty ledger would be indistinguishable from a branch that claimed nothing.
//
// Errors are returned rather than swallowed; the ledger seam
// (RangeBuilder.claimLedger) is what degrades an unreadable range to an empty
// ledger, so this function stays usable by a caller that wants the failure.
func (g *gitRunner) commitMessages(base, head string, maxBytes int64) (msgs []string, truncated bool, err error) {
	// --end-of-options blocks option injection via a ref beginning with '-',
	// matching verifyRef. %B is the raw subject+body, unwrapped and unreformatted,
	// so the claim the author wrote is the claim the panel adjudicates.
	out, err := g.output("log", "-z", "--no-merges", "--format=%B", "--end-of-options", base+".."+head)
	if err != nil {
		return nil, false, fmt.Errorf("reading commit messages for %s..%s: %w", base, head, err)
	}

	// git log is newest-first; records are collected in that order so the cap
	// sheds from the tail (oldest), then reversed for output.
	var newestFirst []string
	for _, rec := range strings.Split(string(out), commitRecordSep) {
		if m := strings.TrimSpace(rec); m != "" {
			newestFirst = append(newestFirst, m)
		}
	}

	kept := newestFirst
	if maxBytes > 0 {
		kept = nil
		var used int64
		for i, m := range newestFirst {
			if used+int64(len(m)) <= maxBytes {
				kept = append(kept, m)
				used += int64(len(m))
				continue
			}
			// The newest message alone overruns the cap: keep its opening bytes
			// rather than returning nothing at all.
			if i == 0 {
				capped, _ := capUTF8(m, int(maxBytes))
				kept = append(kept, capped)
			}
			truncated = true
			break
		}
	}

	// Reverse in place to chronological order: claims read as the branch made
	// them, and the ledger's indices are stable across runs of the same range.
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept, truncated, nil
}

// Claim-splitting shape recognizers. All are anchored and case-sensitive where
// git's own conventions are, so the split is a pure function of the bytes.
var (
	// A bullet: "- ", "* ", "+ ", "1. ", "2) ".
	bulletRe = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)
	// A git trailer whose token is hyphenated (Signed-off-by, Co-authored-by,
	// Change-Id, Reviewed-by, Claude-Session). Requiring the hyphen keeps
	// ordinary prose that opens with a colonized word ("Note: ...") a claim.
	hyphenTrailerRe = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)+:\s`)
	// A single-word reference trailer. Enumerated rather than pattern-matched
	// because the pattern that would catch them also catches prose.
	wordTrailerRe = regexp.MustCompile(`(?i)^(refs?|fixes|closes?|resolves?|cc|bug|issue|see|link|pr):\s`)
	// A line that is nothing but a URL.
	bareURLRe = regexp.MustCompile(`^https?://\S+$`)
)

// splitClaims turns commit messages into an ordered list of discrete claims:
// one per bullet, else one per sentence, with the subject line always its own
// claim. It is a pure function of its input — no model is in this path, because
// a summarizing pass between the author's assertion and the panel's
// adjudication is exactly where "the fix is described but absent" softens into
// "the fix is described", which the diff then satisfies.
//
// Exact duplicates are collapsed to their first occurrence: a squashed or
// cherry-picked branch repeats the same subject across commits, and enumerating
// it twice pads the ledger without adding an assertion.
func splitClaims(msgs []string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(c string) {
		c = strings.TrimSpace(c)
		if !isClaimBearing(c) {
			return
		}
		if _, dup := seen[c]; dup {
			return
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}

	for _, msg := range msgs {
		lines := strings.Split(strings.TrimSpace(msg), "\n")
		if len(lines) == 0 {
			continue
		}
		// The subject is one assertion by construction — it is the one-line
		// summary the author chose — so it is never sentence-split.
		add(lines[0])

		var para []string
		flush := func() {
			if len(para) == 0 {
				return
			}
			for _, s := range splitSentences(strings.Join(para, " ")) {
				add(s)
			}
			para = nil
		}
		inFence := false
		for _, line := range lines[1:] {
			trimmed := strings.TrimSpace(line)
			// A fenced block holds code, not assertions about code.
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				flush()
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			if trimmed == "" {
				flush()
				continue
			}
			if hyphenTrailerRe.MatchString(trimmed) || wordTrailerRe.MatchString(trimmed) || bareURLRe.MatchString(trimmed) {
				flush()
				continue
			}
			if m := bulletRe.FindString(line); m != "" {
				// A bullet is already one discrete claim; splitting it further
				// would fragment a single assertion across several verdicts.
				flush()
				add(line[len(m):])
				continue
			}
			para = append(para, trimmed)
		}
		flush()
	}
	return out
}

// isClaimBearing rejects candidates that assert nothing: a bare token ("wip",
// "fixup"), a punctuation run, or an empty string. The bar is deliberately low
// — two words — because the cost of dropping a real claim (the panel never
// adjudicates it) is far higher than the cost of carrying a weak one.
func isClaimBearing(s string) bool {
	if len(strings.Fields(s)) < 2 {
		return false
	}
	return strings.IndexFunc(s, unicode.IsLetter) >= 0
}

// splitSentences splits prose on '.', '!', or '?' that genuinely ends a
// sentence: the punctuation must be followed by end-of-text, or by whitespace
// and then an uppercase letter. That keeps dotted tokens ("v1.2.3", "e.g.")
// intact, which a naive split on ". " would shred into claims asserting
// nothing.
func splitSentences(s string) []string {
	var out []string
	start := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '.' && runes[i] != '!' && runes[i] != '?' {
			continue
		}
		j := i + 1
		for j < len(runes) && unicode.IsSpace(runes[j]) {
			j++
		}
		atEnd := j >= len(runes)
		// Require the gap: "v1.2" has no whitespace after the dot and is not a
		// boundary. Require the capital: a lowercase continuation is mid-sentence.
		nextStartsSentence := j > i+1 && j < len(runes) && unicode.IsUpper(runes[j])
		if !atEnd && !nextStartsSentence {
			continue
		}
		out = append(out, strings.TrimSpace(string(runes[start:i+1])))
		start = j
		i = j - 1
	}
	if start < len(runes) {
		out = append(out, strings.TrimSpace(string(runes[start:])))
	}
	return out
}

// ClaimLedgerPath is the sentinel Path of the claim-ledger FileEntry.
//
// STUB — replaced in GREEN.
const ClaimLedgerPath = "<claims>"

// claimLedgerSection renders the claim ledger payload section.
//
// STUB — replaced in GREEN.
func claimLedgerSection(claims []string, truncated bool) string {
	return ""
}
