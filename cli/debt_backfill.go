package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"

	"github.com/samestrin/atcr/internal/localdebt"
)

// newDebtBackfillCmd builds `atcr debt backfill-justifications`: a ONE-OFF repair for
// the justifications already on disk.
//
// It is a separate command rather than a step inside reconcile because the repair and
// the write path have opposite growth characteristics. Record.StampID excludes
// Justification, so a re-detected finding keeps its id and PersistForReconcile skips
// the append — which is exactly what bounds store size by finding count instead of by
// review count. Refreshing excerpts on every reconcile would give that up to catch a
// reviewer rewording its narrative. Running the repair once, deliberately, does not.
func newDebtBackfillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfill-justifications",
		Short: "Replay stored justifications from their source review.md (one-off repair)",
		Long: "atcr debt backfill-justifications re-derives each LIVE record's justification\n" +
			"from the review.md it was originally stamped from, and rewrites the ones that\n" +
			"changed. Live means open or deferred: a resolved or wontfix record is settled,\n" +
			"and its justification may be the operator's --reason rather than a review\n" +
			"excerpt, which nothing can replay.\n\n" +
			"It exists because a record's id excludes its justification: a re-detected\n" +
			"finding hashes to the same id and is deduped away, so an improvement to the\n" +
			"extractor reaches only records persisted after it. Excerpts already in the\n" +
			"store keep their original text — including the marker-free ones that\n" +
			"`debt resolve --status wontfix` accepts as a complete audit trail.\n\n" +
			"A record is repaired only when exactly one surviving review.md anchors it.\n" +
			"source_report paths are review-dir-relative, so several reviews hold the same\n" +
			"relative path; a record whose candidates disagree is left alone rather than\n" +
			"rewritten from a guess. Within a repaired id only the LINES still carrying the\n" +
			"stale excerpt are written, so a resolution trail's --reason is left intact.\n" +
			"Run --dry-run first: it prints the before and after of every line it would\n" +
			"touch. Each line is named by a `<shard>:<line>` locator whose shard name has\n" +
			"had terminal-driving runes stripped and token-breaking ones percent-encoded,\n" +
			"so it may not be the literal filename on disk. Where two names reduce to the\n" +
			"same token, each gets a `#xxxxxx` suffix — the first 6 hex of sha256 over the\n" +
			"raw filename — so the listing never leaves it ambiguous which file would be\n" +
			"rewritten. The suffix is appended only where a collision exists.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtBackfill,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("review-root", "", "directory to search for source review.md files; unset resolves to the repo root")
	cmd.Flags().Bool("dry-run", false, "report what would be rewritten without touching the store")
	return cmd
}

func runDebtBackfill(cmd *cobra.Command, _ []string) error {
	dir := debtStoreDir(cmd)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	reviewRoot, _ := cmd.Flags().GetString("review-root")
	if reviewRoot == "" {
		root, err := repoRoot()
		if err != nil {
			return fmt.Errorf("backfill-justifications: no --review-root given and the repo root could not be resolved: %w", err)
		}
		reviewRoot = root
	}

	res, err := localdebt.BackfillJustifications(dir, reviewRoot, dryRun)
	if err != nil {
		return fmt.Errorf("backfill-justifications: %w", err)
	}

	prefix := ""
	if dryRun {
		prefix = "dry run: "
	}
	// Every counter is printed, including the zeros. Unresolved and Ambiguous are
	// the two the operator may need to act on — a pruned review tree, or a repo
	// holding several reviews that anchor one finding — and a counter that appears
	// only when non-zero reads as "not checked" rather than "checked, none found".
	// The rewritten counter names LINES as well as records. They differ whenever an
	// id carries a resolution trail, and the line count is the one that describes
	// what was written to an append-only store.
	// The settled counter is printed for the same reason: the fold suppresses a
	// settled id per-record, so a store whose ids are all settled reports "0 scanned,
	// 0 rewritten" — byte-identical to a store that needs no repair. Naming the
	// suppression is what lets an operator tell those apart.
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"%s%d scanned, %d rewritten (%d %s), %d unchanged, %d unresolved (no review.md yielded the excerpt), %d ambiguous (candidates disagreed), %d skipped (settled: resolved or wontfix)\n",
		prefix, res.Scanned, res.Rewritten, res.RewrittenLines, pluralLines(res.RewrittenLines),
		res.Unchanged, res.Unresolved, res.Ambiguous, res.SkippedSettled)

	// A dry run shows the text, not just the count. It is documented as the step to
	// run FIRST on the one subcommand that rewrites the store in place, and a bare
	// counter cannot reveal WHICH line would change — the difference between a stale
	// review excerpt and an operator's typed --reason is only visible in the text.
	// %q keeps a multi-line excerpt on one line and, more importantly, escapes it:
	// the store is world-appendable, so its text is untrusted input that must never
	// be echoed verbatim to a terminal.
	//
	// That rule covers EVERY field of the line, not just the excerpt. The id reaches
	// JustificationChange as an unvalidated `m["id"].(string)` (internal/localdebt/
	// backfill.go), and the shard is a store filename, so both are untrusted for the
	// same reason. Printing them under %s let an ANSI CSI or a bidi override through to
	// the terminal on the one surface an operator consults to decide whether to let the
	// in-place rewrite proceed - where reordering WHICH line is named is the whole
	// attack. The sibling listing `atcr debt list` already strips the ANSI CSI / C0 / C1
	// half of that (cli/debt.go -> cell -> sanitizeCell); it shares the Cf gap, passing a
	// bidi override through unchanged exactly as recorded in the next paragraph.
	//
	// The two get different treatment on purpose. The id takes %q, which escapes the
	// FORMAT runes (Cf) sanitizeCell deliberately keeps - a bidi override is not a C0/C1
	// control, and it is the rune that reorders which line appears to be named. The
	// shard cannot take %q: that would put the line number outside the quoted name and
	// break `<shard>:<line>` as one copy-pasteable token. It takes sanitizeLocator
	// instead, which removes the terminal-controlling categories AND percent-encodes the
	// colon and whitespace that would otherwise break that one-token property from
	// inside the name - the property is asserted here, so it has to be enforced there.
	if dryRun {
		// Resolved once over the whole store directory: a collision is a property of the
		// SET of shard names on disk, so it cannot be detected one row at a time — nor
		// from the change set alone, which cannot see an unchanged colliding file.
		locators := locatorNames(res.ShardNames, res.Changes)

		// The suffix needs a legend, or it does not do its job. Unannotated, "#a1b2c3"
		// reads as part of the filename — which is also the documented residual case, a
		// real file named that way — and the operator cannot map it back to a file by
		// eye, because it hashes raw bytes they are not shown. The mechanism's whole
		// security value is the operator understanding that a suffix means "this is not
		// the plain name you think it is", and nothing else on this surface says so.
		//
		// A locator counts as suffixed when it differs from the bare sanitized name,
		// rather than by searching for a "#" — a shard genuinely named with a "#" would
		// otherwise summon a legend explaining a suffix that was never appended.
		//
		// Emitted only when at least one locator carries a suffix, matching the suffix's
		// own conditional appearance: an unconditional legend on every ordinary run is a
		// line the operator learns to skip, and it is most needed on the runs that are
		// not ordinary.
		for _, c := range res.Changes {
			if locators[c.Shard] != sanitizeLocator(c.Shard) {
				_, _ = fmt.Fprint(cmd.OutOrStdout(),
					"  note: some shard names collide once unprintable runes are stripped; "+
						"#xxxxxx is the first 6 hex of sha256 over the RAW filename, appended only "+
						"to tell colliding names apart — it is not part of the file's name\n")
				break
			}
		}

		for _, c := range res.Changes {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s:%d %q\n    before: %q\n    after:  %q\n",
				locators[c.Shard], c.Line, c.ID, c.Before, c.After)
		}
	}
	return nil
}

// stripTerminalDrivers removes exactly the rune categories that can drive a terminal:
// sanitizeCell's C0/ESC/DEL and C1, plus U+2028/U+2029, plus category Cf. It is the
// shared first half of sanitizeLocator (which then percent-encodes for token safety)
// and collisionKey (which then folds for equivalence). Splitting it out is what lets
// the fold see the ORIGINAL runes: NFKC over an already-encoded token cannot map
// "%C2%A0" back to "%20", so folding after encoding would silently do nothing for the
// no-break-space case it exists to catch.
func stripTerminalDrivers(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, sanitizeCell(s))
}

// sanitizeLocator is sanitizeCell plus category Cf, plus a percent-encoding of the
// runes that would break the token from inside, for a field rendered UNQUOTED on a
// terminal surface.
//
// sanitizeCell keeps Cf on purpose: it feeds table cells whose neighbours are quoted,
// and a legitimate identity may carry a joiner. That trade does not hold for the
// `<shard>:<line>` locator in the backfill dry run. The shard is a store filename from
// os.ReadDir over a world-appendable directory, it is printed with %s so nothing
// escapes it downstream, and it is the field that literally NAMES the line the
// in-place rewrite would overwrite — the one the surrounding comment identifies as the
// whole attack. A U+202E there reorders which line appears to be named, on the surface
// an operator consults to decide whether to let the rewrite proceed.
//
// Cf is STRIPPED rather than escaped so the locator stays one token a reader can copy
// whole; quoting it would push the line number outside the name.
//
// This is NOT equivalent to the %q applied to the id beside it. %q escapes everything
// strconv.IsPrint rejects, which includes Co private-use runes this function leaves
// alone. What is REMOVED here is exactly the set that can drive a terminal: C0/ESC/DEL,
// C1, U+2028/U+2029 and Cf.
//
// A second, separate pass then percent-encodes `%`, `:` and every unicode.IsSpace rune
// - not because they drive a terminal, but because they break the one-token property
// this whole design is built on. A colon inside the name gives `<shard>:<line>` two
// candidate splits; whitespace ends the token, so half the name is what a copy-paste
// picks up. `%` is encoded first, so the escape stays reversible and a file genuinely
// named "2026%3A08.jsonl" cannot render as the encoded form of "2026:08.jsonl".
//
// Stripping also means the printed locator is not guaranteed to be the literal
// filename on disk — and, because it is lossy, that two DIFFERENT filenames can reduce
// to the same token. Callers rendering a SET of locators must therefore go through
// locatorNames, which appends a per-file suffix where that collision actually happens;
// this function alone sees one name and cannot detect it.
//
// It is deliberately not a widening of sanitizeCell: `debt list` and
// `leaderboard --table` share that helper, and Cf pass-through there is the documented
// behavior, not an oversight.
func sanitizeLocator(s string) string {
	stripped := stripTerminalDrivers(s)

	// Percent-encode the three things that would break `<shard>:<line>` as ONE
	// unambiguously parseable, copy-pasteable token — which is the entire reason the
	// shard is stripped instead of quoted (see the header above and the id/shard
	// contrast at the call site).
	//
	// A colon inside the shard name gives the token two candidate splits: printed raw,
	// "2026:08.jsonl:1" is as readable as shard "2026" line "08.jsonl:1". Whitespace
	// ends the token at a terminal, so "2026 08.jsonl:1" is two tokens, not one, and a
	// copy-paste picks up half a name. Neither is exotic: both are ordinary POSIX
	// filenames, and this directory is world-appendable.
	//
	// `%` is encoded FIRST (as %25) so the encoding is reversible — otherwise a file
	// genuinely named "2026%3A08.jsonl" would render identically to the encoded form of
	// "2026:08.jsonl", which is the same which-file-is-it ambiguity locatorNames exists
	// to remove, reintroduced by the escape itself.
	var b strings.Builder
	for _, r := range stripped {
		if r == '%' || r == ':' || unicode.IsSpace(r) {
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", by)
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func pluralLines(n int) string {
	if n == 1 {
		return "line"
	}
	return "lines"
}

// locatorNames maps each RAW shard name in changes to the token the dry run prints for
// it, disambiguating names that collide once sanitizeLocator strips their Cf runes.
//
// The strip is lossy by design (it keeps the locator one copy-pasteable token instead
// of quoting it), and lossy means two DIFFERENT store filenames can reduce to the same
// string. On this surface that is not cosmetic: the listing is what an operator reads
// to decide whether to let an in-place rewrite proceed, and two identical locators make
// it ambiguous WHICH file would be rewritten. Marking the row "name sanitized" does not
// help — both rows carry the same mark and still read alike; only a per-file suffix
// tells them apart.
//
// The suffix is derived from the raw name, not from an index, so the DERIVATION is
// stable across runs and independent of directory order. The token itself is not:
// the suffix is appended only where a collision actually exists, so whether a file
// prints bare or suffixed varies with whether its colliding sibling is present in
// the store listing on that run — an operator comparing
// two dry runs can see the same file print bare in one and suffixed in the other.
// The ordinary single-shard listing is unchanged: no collision, no suffix.
//
// The collision is resolved against every shard the store DIRECTORY holds, not against
// the change set alone. The ambiguity being removed is between the printed token and a
// real file on disk, and the dangerous shape is exactly the one a change-set-only map
// cannot see: a genuine 2026-08.jsonl with nothing to repair sits beside a planted
// 2026-08<U+200B>.jsonl that carries the rewrite, the listing prints "2026-08.jsonl:1"
// bare, and the operator approves believing their August shard is the one being
// repaired.
//
// `shards` is that directory listing, and it arrives from the caller rather than being
// read here. This function used to run its own os.ReadDir, which executed AFTER
// localdebt.BackfillJustifications had returned — outside the withLock region
// rewriteJustifications' own walk ran inside. A concurrent writer removing a colliding
// shard in that window suppressed the "#hash" suffix this function exists to add, so
// the dry run printed a bare token for a name that WAS ambiguous when the rewrite was
// computed, on the surface an operator approves an in-place rewrite from. atcr's own
// CLAUDE.md notes concurrent sessions share this tree, so that writer is not
// hypothetical. Taking the locked pass's own observation instead means the printed
// locators and the computed rewrite describe ONE snapshot, and it leaves exactly one
// shard filter in the tree (localdebt's) rather than two copies to keep in step.
//
// Residual case, accepted rather than overlooked: a store file literally named like an
// already-disambiguated token ("2026-08.jsonl#a1b2c3d4e5f6") would print the same as the
// disambiguated form of some other file. Reaching it needs an attacker to guess a
// SHA-256 prefix of a filename they do not control, and the outcome is the same
// ambiguity that exists today rather than a worse one.
//
// That dismissal covers ONLY the guess-a-hash-you-do-not-control shape. It does not
// cover the attacker who plants BOTH names — the one the threat model here already
// assumes, since they can write to the store directory. They pick both preimages and
// pad either with Cf runes that sanitize away, so finding two names that share a
// truncated digest is an offline birthday search over a space of their choosing, not a
// guess. That is what locatorSuffixHexLen is sized against.
func locatorNames(shards []string, changes []localdebt.JustificationChange) map[string]string {
	rawByToken := map[string]map[string]bool{}
	add := func(shard string) {
		t := collisionKey(shard)
		if rawByToken[t] == nil {
			rawByToken[t] = map[string]bool{}
		}
		rawByToken[t][shard] = true
	}

	for _, shard := range shards {
		add(shard)
	}
	// A TOTALITY invariant, not a live fallback — and the distinction is stated because
	// this function used to read the directory itself, when the loop genuinely rescued a
	// change whose shard the failed listing had missed. It cannot fire for the one caller
	// that exists today: every JustificationChange.Shard is an os.ReadDir entry name from
	// the same locked walk that produced `shards`, so the snapshot is a superset of the
	// change set by construction. It is kept so locatorNames stays TOTAL over `changes`
	// for any caller — a change must never print without its own name considered, and a
	// caller passing a partial snapshot would otherwise silently lose collisions rather
	// than fail. Do not read it as a guard against a state this caller can reach.
	for _, c := range changes {
		add(c.Shard)
	}

	// Sized by SHARD cardinality, which is what this map is keyed by — not by
	// len(changes), which counts changed LINES. A repair touching tens of thousands of
	// lines across a dozen month shards would otherwise pre-allocate two to four orders
	// of magnitude more buckets than the map can ever hold. rawByToken is fully
	// populated by this point (both add loops are above), and its length is a tight
	// upper bound: one entry per distinct sanitized token over the snapshot and the
	// change set together. A size hint cannot affect correctness, only allocation.
	out := make(map[string]string, len(rawByToken))
	for _, c := range changes {
		// Keyed on the folded form, PRINTED as the plain sanitized token: the operator
		// should still see the name as close to the file as this surface can render it,
		// with the suffix — not a normalized spelling — doing the telling-apart.
		t := sanitizeLocator(c.Shard)
		if len(rawByToken[collisionKey(c.Shard)]) > 1 {
			sum := sha256.Sum256([]byte(c.Shard))
			t += "#" + hex.EncodeToString(sum[:])[:locatorSuffixHexLen]
		}
		out[c.Shard] = t
	}
	return out
}

// collisionKey is the value locatorNames groups shard names by: the shard name with
// terminal-driving runes stripped, then folded through NFKC.
//
// It folds the STRIPPED name, not the printed token. sanitizeLocator percent-encodes
// after stripping, and "%20" and "%C2%A0" are both plain ASCII by then — so a fold
// applied to the token could no longer see that a space and a no-break space were ever
// equivalent.
//
// Keying on the printed token's raw BYTES would detect only the ambiguity the Cf strip
// itself introduces. Two names that RENDER identically but differ in bytes would get
// distinct keys and no suffix at all — and two rows carrying what looks like the same
// filename is the exact which-file-would-be-rewritten ambiguity this whole mechanism
// exists to remove, arrived at by a different route. NFKC closes the
// compatibility-equivalent half of it: NFC e-acute beside NFD e+U+0301, or U+00A0
// beside a space.
//
// It is a coarse net, and deliberately so: over-grouping costs a redundant suffix on a
// pair that would have read distinctly, which is noise. Under-grouping costs the
// operator an unmarked ambiguity on the surface they approve an in-place rewrite from.
//
// KNOWN LIMIT, stated so the guarantee is not overstated: NFKC folds compatibility
// equivalents, not visual confusables. U+2011 NON-BREAKING HYPHEN maps to U+2010, not
// to ASCII U+002D, so "2026-08.jsonl" and "2026\u2011 08.jsonl" keep distinct keys and
// both print bare. Closing that needs a confusables table rather than a normalizer, and
// the residual is the same ambiguity that exists today rather than a worse one — the
// same footing as the already-disambiguated-name case above. It is pinned by
// TestLocatorNames_DoesNotFoldVisualConfusablesThatAreNotCompatibilityEquivalent so the
// boundary is a decision on record, not a gap nobody noticed.
func collisionKey(shard string) string {
	return norm.NFKC.String(stripTerminalDrivers(shard))
}

// locatorSuffixHexLen is how many hex characters of the raw name's SHA-256 the
// disambiguating suffix carries: 12, for 48 bits.
//
// It is sized against the attacker the surrounding comments already assume — one who
// can write to the store directory, and therefore controls BOTH planted filenames and
// can pad either with Cf runes that sanitize away. That is not the guess-a-hash-of-a-
// name-you-do-not-control case the residual paragraph dismisses; it is an offline
// birthday search over a space the attacker chooses, which needs about 2^(bits/2)
// trials. At the previous 6 characters that was 24 bits, so roughly 4096 trials — under
// a second of scripting, demonstrated by the fixture in
// TestLocatorNames_SuffixSurvivesAForcedShortPrefixCollision, whose two names really do
// share the prefix "2a0450". Both rows then printed one identical locator, which is the
// exact invariant the listing exists to uphold.
//
// 48 bits puts that search at roughly 2^24 hashes for a pair, and the token stays short
// enough to read. A per-run ordinal would also work and is shorter still, but it would
// give up the property the header calls out: the suffix is derived from the raw name,
// so the DERIVATION is stable across runs and independent of directory order.
const locatorSuffixHexLen = 12
