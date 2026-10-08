package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ReExtractJustification is the replay entry point a one-off store backfill needs:
// Record.StampID hashes only file\x00line\x00problem, so a re-detected finding keeps
// its id and PersistForReconcile skips the append — every justification already in
// the store therefore keeps the text extractSection produced at the time it was
// written, including the marker-free excerpts that predate the dangling-fence
// emission. Re-deriving one requires the ORIGINAL review.md, not the stored text:
// whether a block needed a synthetic opener is a property of where it began in the
// source document, which the excerpt alone cannot report.
func TestReExtractJustification(t *testing.T) {
	dir := t.TempDir()
	// A DANGLING opener: the fence is never closed, so extractSection releases the
	// quoted tail as prose and must emit a synthetic ``` at the head of the block —
	// the marker isRecordedRationale keys on.
	body := "## Findings\n" +
		"\n" +
		"Some preamble.\n" +
		"\n" +
		"```\n" +
		"- internal/thing.go:42 quoted example row\n" +
		"\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	path := filepath.Join(dir, "review.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	t.Run("replays the excerpt for a verified anchor", func(t *testing.T) {
		text, _, ok, err := ReExtractJustification(path, "internal/thing.go", 42, 8)
		require.NoError(t, err)
		require.True(t, ok, "line 8 anchors internal/thing.go:42, so the replay must produce an excerpt")
		assert.NotEmpty(t, text)
		var marked bool
		for _, l := range strings.Split(text, "\n") {
			if isFenceMarker(l) {
				marked = true
				break
			}
		}
		assert.True(t, marked,
			"a block opened inside a released tail carries the synthetic marker; that emission is the whole point of replaying from source rather than re-reading the stored excerpt")
	})

	t.Run("refuses a line that does not anchor the finding", func(t *testing.T) {
		// Line 3 is prose that never mentions the file: accepting it would let a
		// backfill rewrite a record from an unrelated section of a same-named
		// review.md in a different review directory.
		_, _, ok, err := ReExtractJustification(path, "internal/thing.go", 42, 3)
		require.NoError(t, err)
		assert.False(t, ok, "an unanchored line must not yield a replacement excerpt")
	})

	t.Run("reports a missing review.md as an error, never as no-match", func(t *testing.T) {
		_, _, _, err := ReExtractJustification(filepath.Join(dir, "gone.md"), "internal/thing.go", 42, 8)
		require.Error(t, err, "a caller must be able to tell 'source pruned' from 'anchor did not match'")
		require.ErrorContains(t, err, "stat review narrative",
			"the error must come from the stat arm, not a later read failing on the same missing file")
	})

	t.Run("an out-of-range anchor line is no-match, not a panic", func(t *testing.T) {
		_, _, ok, err := ReExtractJustification(path, "internal/thing.go", 42, 9999)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	// A VERIFIED anchor whose whole block is quoted example text. extractSection
	// elides a terminated fence to a placeholder and returns "" — matchAllElided, not
	// a match. It is the one path that reaches the anchor check and still has no
	// narrative to give, and it is the sole defence against ok=true with an empty
	// text: localdebt.BackfillJustifications would write that "" over a stored
	// excerpt, on an append-only store, irreversibly.
	t.Run("an anchor whose section is entirely quoted example is no-match, never an empty rewrite", func(t *testing.T) {
		elided := "## Review\n" +
			"\n" +
			"```\n" +
			"internal/thing.go:42 HIGH the token is never rotated\n" +
			"```\n"
		p := filepath.Join(dir, "elided.md")
		require.NoError(t, os.WriteFile(p, []byte(elided), 0o600))

		text, _, ok, err := ReExtractJustification(p, "internal/thing.go", 42, 4)
		require.NoError(t, err)
		assert.False(t, ok,
			"an all-elided section carries no reviewer content, so it must not authorise a rewrite")
		assert.Empty(t, text,
			"returning ok=true here would blank a stored justification that no later reconcile can replace")
	})
}

// The replay path must apply the SAME exclusions the producer does, or the replay
// set can exceed the stamp set: a file the producer would never have stamped from
// would become an authoritative candidate for the replay (localdebt/backfill.go's
// stated invariant). Two are missing today — the salvaged-source skip and the
// draftLineSet exclusion — so a pre-existing forged justification survives a
// backfill as Unchanged and the operator is told the store is clean.
func TestReExtractJustification_AppliesTheProducerExclusions(t *testing.T) {
	op := "\x3cthink\x3e"
	cl := "\x3c/think\x3e"
	dir := t.TempDir()

	anchored := "\n- **internal/thing.go:42** the real narrative explaining the defect.\n"

	t.Run("a salvaged source with no bin index yields no replay excerpt", func(t *testing.T) {
		p := filepath.Join(dir, "salvaged.md")
		require.NoError(t, os.WriteFile(p, []byte("## Findings\n"+anchored), 0o600))
		// No salvaged_chunks: an unchunked persona whose whole reply is promoted
		// reasoning. collectReviewNarratives withholds it entirely, so it is not a
		// document the stamp could have come from.
		require.NoError(t, os.WriteFile(filepath.Join(dir, statusFileName),
			[]byte(`{"salvaged":true}`), 0o600))

		// Line 3 is the anchored narrative line, so the only reason to refuse is the
		// salvaged status the producer honours.
		text, _, ok, err := ReExtractJustification(p, "internal/thing.go", 42, 3)
		require.NoError(t, err)
		assert.False(t, ok, "a source the producer refuses must not authorise a replay rewrite")
		assert.Empty(t, text)
	})

	// The third exclusion, and the one that shipped unexercised: a status.json naming
	// bins the review.md cannot account for. Mutation gives no evidence here —
	// removing the arm leaves `desynced` unused and fails the BUILD, not a test — so
	// the arm needs a reachable input or nothing pins it at all.
	t.Run("a salvaged bin index the content cannot account for yields no replay excerpt", func(t *testing.T) {
		p := filepath.Join(dir, "desynced.md")
		require.NoError(t, os.WriteFile(p, []byte("## Findings\n"+anchored), 0o600))
		// One segment (no boundary marker), so bin 3 names nothing. fanout never
		// writes such a pair, which is exactly why it is evidence the review.md and
		// the status.json came from different states. The bin list is non-empty, so
		// the whole-file salvaged arm above does NOT fire — this reaches the desync
		// arm specifically.
		require.NoError(t, os.WriteFile(filepath.Join(dir, statusFileName),
			[]byte(`{"salvaged":true,"salvaged_chunks":[3]}`), 0o600))

		// Line 3 is the anchored narrative line, so the only reason to refuse is the
		// desync. Withhold, mirroring the producer — and withhold as ok=false, not as
		// an error: err is reserved for "the source is gone or unreadable".
		text, section, ok, err := ReExtractJustification(p, "internal/thing.go", 42, 3)
		require.NoError(t, err,
			"a desynced pair is a mismatch, not an unreadable source — collapsing the two "+
				"would make a pruned review dir indistinguishable from this")
		assert.False(t, ok, "a bin list the content cannot account for must not authorise a replay rewrite")
		assert.Empty(t, text)
		assert.Empty(t, section)
	})

	t.Run("a draft citation inside a leading think run is not an anchor", func(t *testing.T) {
		p := filepath.Join(dir, "draft.md")
		body := op + "considering internal/thing.go:42\nstill drafting\n" + cl + "\n" +
			"- internal/thing.go:43 something else entirely\n"
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		require.NoError(t, os.Remove(filepath.Join(dir, statusFileName)))

		// Line 1 carries the anchor, but it lives inside the leading run the
		// findings parser refused — the model DISCARDED it. Publishing it as the
		// finding's provenance is the damage draftLineSet exists to prevent.
		text, _, ok, err := ReExtractJustification(p, "internal/thing.go", 42, 1)
		require.NoError(t, err)
		assert.False(t, ok, "a draft-run line must not authorise a replay rewrite")
		assert.Empty(t, text)
	})
}

// collectReviewNarratives refuses a symlink named review.md (justification.go's
// IsRegular check), so the exported predicate must too — otherwise it is incomplete
// against its own doc, and a caller that does not repeat the IsRegular check itself
// is told an in-cap link is a stampable file. os.Stat would follow the link and size
// the TARGET, which is in-cap, so only an Lstat-based arm can see this.
func TestReviewPolicyDeclinesFile_RefusesASymlinkToAnInCapReview(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	require.NoError(t, os.WriteFile(target,
		[]byte("## Findings\n\n- **internal/thing.go:42** the real narrative explaining the defect.\n"), 0o600))
	link := filepath.Join(dir, "review.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Control: the target itself is a regular, in-cap, unsalvaged file the policy
	// admits, so the only difference the link makes is that it is a link.
	declined, err := ReviewPolicyDeclinesFile(target)
	require.NoError(t, err)
	require.False(t, declined, "the regular target must be admitted, or this test proves nothing about the link")

	declined, err = ReviewPolicyDeclinesFile(link)
	require.NoError(t, err, "a refused link is a policy verdict, not an unreadable source")
	assert.True(t, declined, "the producer never stamps from a symlink, so the predicate must decline it")

	// And the replay gate reads the same arm: the anchored line 3 matches, so the
	// link is the only reason to refuse.
	text, _, ok, err := ReExtractJustification(link, "internal/thing.go", 42, 3)
	require.NoError(t, err)
	assert.False(t, ok, "a symlink must not authorise a replay rewrite")
	assert.Empty(t, text)
}
