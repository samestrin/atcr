package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

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
			"touch.",
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
	// instead, which removes the terminal-controlling categories.
	if dryRun {
		// Resolved once over the whole store directory: a collision is a property of the
		// SET of shard names on disk, so it cannot be detected one row at a time — nor
		// from the change set alone, which cannot see an unchanged colliding file.
		locators := locatorNames(dir, res.Changes)
		for _, c := range res.Changes {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s:%d %q\n    before: %q\n    after:  %q\n",
				locators[c.Shard], c.Line, c.ID, c.Before, c.After)
		}
	}
	return nil
}

// sanitizeLocator is sanitizeCell plus category Cf, for a field rendered UNQUOTED on
// a terminal surface.
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
// strconv.IsPrint rejects, which includes Zs runes (U+00A0, U+2000-U+200A) and Co
// private-use runes that neither sanitizeCell nor this strip touches. What is removed
// here is exactly the set that can drive a terminal: C0/ESC/DEL, C1, U+2028/U+2029 and
// Cf. Stripping also means the printed locator is not guaranteed to be the literal
// filename on disk — and, because it is lossy, that two DIFFERENT filenames can reduce
// to the same token. Callers rendering a SET of locators must therefore go through
// locatorNames, which appends a per-file suffix where that collision actually happens;
// this function alone sees one name and cannot detect it.
//
// It is deliberately not a widening of sanitizeCell: `debt list` and
// `leaderboard --table` share that helper, and Cf pass-through there is the documented
// behavior, not an oversight.
func sanitizeLocator(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, sanitizeCell(s))
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
// the store listing (or the change-set fallback) on that run — an operator comparing
// two dry runs can see the same file print bare in one and suffixed in the other.
// The ordinary single-shard listing is unchanged: no collision, no suffix.
//
// The collision is resolved against every shard the store DIRECTORY holds, not against
// the change set alone. The ambiguity being removed is between the printed token and a
// real file on disk, and the dangerous shape is exactly the one a change-set-only map
// cannot see: a genuine 2026-08.jsonl with nothing to repair sits beside a planted
// 2026-08<U+200B>.jsonl that carries the rewrite, the listing prints "2026-08.jsonl:1"
// bare, and the operator approves believing their August shard is the one being
// repaired. The listing filter matches rewriteJustifications' own walk — non-directory
// entries ending in ".jsonl" — so the two agree on what a shard is.
//
// A listing error is not fatal here: the rewrite the operator is about to approve has
// already been computed, so the dry run falls back to the change set (the pre-existing
// behavior) rather than failing on a directory it could read moments earlier.
//
// Residual case, accepted rather than overlooked: a store file literally named like an
// already-disambiguated token ("2026-08.jsonl#a1b2c3") would print the same as the
// disambiguated form of some other file. Reaching it needs an attacker to guess a
// SHA-256 prefix of a filename they do not control, and the outcome is the same
// ambiguity that exists today rather than a worse one.
func locatorNames(dir string, changes []localdebt.JustificationChange) map[string]string {
	rawByToken := map[string]map[string]bool{}
	add := func(shard string) {
		t := sanitizeLocator(shard)
		if rawByToken[t] == nil {
			rawByToken[t] = map[string]bool{}
		}
		rawByToken[t][shard] = true
	}

	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			add(e.Name())
		}
	}
	// Unconditional, not an else: a changed shard must be in the map even if the
	// listing missed it (unreadable directory, or a file removed between the rewrite
	// pass and this one), so a change can never print without its own name considered.
	for _, c := range changes {
		add(c.Shard)
	}

	out := make(map[string]string, len(changes))
	for _, c := range changes {
		t := sanitizeLocator(c.Shard)
		if len(rawByToken[t]) > 1 {
			sum := sha256.Sum256([]byte(c.Shard))
			t += "#" + hex.EncodeToString(sum[:])[:6]
		}
		out[c.Shard] = t
	}
	return out
}
