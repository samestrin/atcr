package payload

import (
	"fmt"
	"strings"
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
