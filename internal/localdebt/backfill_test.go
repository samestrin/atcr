package localdebt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// danglingReview is a reviewer narrative whose fence is never closed. extractSection
// releases the quoted tail as prose and emits a synthetic ``` at the head of the
// block, which is the marker cli/debt_resolve.go's wontfix gate keys on. A
// justification stamped BEFORE that emission existed carries no marker, so the gate
// accepts it as a permanent dismissal's whole audit trail.
const danglingReview = "## Findings\n" +
	"\n" +
	"Some preamble.\n" +
	"\n" +
	"```\n" +
	"- internal/thing.go:42 quoted example row\n" +
	"\n" +
	"- **internal/thing.go:42** the real narrative explaining the defect.\n"

func shardLines(t *testing.T, dir, month string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, month+".jsonl"))
	require.NoError(t, err)
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		m := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		out = append(out, m)
	}
	return out
}

// The dangling-fence guarantee is not retroactive: StampID excludes Justification, so
// a re-detected finding keeps its id and PersistForReconcile skips the append. A
// one-off backfill is the only way the excerpts already in the store get corrected.
func TestBackfillJustifications(t *testing.T) {
	setup := func(t *testing.T) (store, reviewRoot string) {
		t.Helper()
		root := t.TempDir()
		store = filepath.Join(root, "debt")
		require.NoError(t, os.MkdirAll(store, 0o750))
		reviewRoot = filepath.Join(root, "reviews")
		rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(danglingReview), 0o600))

		stale := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
			`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
			`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
		// Its source_report names a review.md no surviving directory holds.
		orphan := `{"schema_version":3,"id":"bbbb2222","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
			`"severity":"LOW","file":"internal/other.go","line":7,"problem":"p2","fix":"f2","category":"correctness",` +
			`"est_minutes":5,"evidence":"e","reviewers":["dax"],"confidence":"LOW",` +
			`"justification":"stale text","future_field":"must survive",` +
			`"source_report":{"path":"sources/pool/raw/agent/gone/review.md","line":3}}`
		writeShard(t, store, "2026-08", stale, orphan)
		return store, reviewRoot
	}

	t.Run("rewrites a resolvable stale excerpt and leaves an orphan alone", func(t *testing.T) {
		store, reviewRoot := setup(t)
		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		assert.Equal(t, 2, res.Scanned)
		assert.Equal(t, 1, res.Rewritten, "the resolvable record's excerpt is replayed from source")
		assert.Equal(t, 1, res.Unresolved, "the orphan has no surviving review.md and must be reported, not guessed at")

		got := shardLines(t, store, "2026-08")
		require.Len(t, got, 2)
		assert.Contains(t, got[0]["justification"], "```",
			"the replayed excerpt carries the synthetic dangling-fence marker the stored one lacked")
		assert.Equal(t, "stale text", got[1]["justification"], "an unresolved record is never blanked or guessed")
		assert.Equal(t, "must survive", got[1]["future_field"],
			"a line this backfill does not change passes through byte-for-byte, so a field this binary does not declare is not dropped")
	})

	t.Run("dry run reports the same counts and writes nothing", func(t *testing.T) {
		store, reviewRoot := setup(t)
		before, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)

		res, err := BackfillJustifications(store, reviewRoot, true)
		require.NoError(t, err)
		assert.Equal(t, 1, res.Rewritten, "a dry run reports what it WOULD rewrite")

		after, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "a dry run must not touch the store")
	})

	t.Run("is idempotent: a second pass rewrites nothing", func(t *testing.T) {
		store, reviewRoot := setup(t)
		_, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)
		assert.Zero(t, res.Rewritten, "the replay is deterministic, so a settled store is a no-op")
		assert.Equal(t, 1, res.Unchanged)
	})

	t.Run("declines an ambiguous record rather than picking one", func(t *testing.T) {
		store, reviewRoot := setup(t)
		// A second review directory holding the SAME relative path whose anchor line
		// also references the finding. SourceReport.Path is review-dir-relative, so
		// this is the normal shape of a repo with many reviews — the backfill must
		// not rewrite from a coin flip.
		rd := filepath.Join(reviewRoot, "sprint-b", "multi-agent", "sources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
			[]byte(strings.Replace(danglingReview, "the real narrative explaining the defect.", "a DIFFERENT narrative for the same anchor.", 1)), 0o600))

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)
		assert.Equal(t, 1, res.Ambiguous)
		assert.Zero(t, res.Rewritten, "two candidates disagree, so neither may be written")
	})
}
