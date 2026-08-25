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
		text, _, ok, err := ReExtractJustification(path, "internal/thing.go", 42, 9)
		require.NoError(t, err)
		require.True(t, ok, "line 9 anchors internal/thing.go:42, so the replay must produce an excerpt")
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
		_, _, _, err := ReExtractJustification(filepath.Join(dir, "gone.md"), "internal/thing.go", 42, 9)
		require.Error(t, err, "a caller must be able to tell 'source pruned' from 'anchor did not match'")
	})

	t.Run("an out-of-range anchor line is no-match, not a panic", func(t *testing.T) {
		_, _, ok, err := ReExtractJustification(path, "internal/thing.go", 42, 9999)
		require.NoError(t, err)
		assert.False(t, ok)
	})
}
