package localdebt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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

	// The fold filter decides which stored excerpts the replay may touch. Two record
	// classes sit on opposite sides of it and both were wrong before this test:
	// `wontfix` carries the operator's --reason (cli/debt_resolve.go replaces the
	// justification with it), which exists nowhere else in the tree and must never be
	// replayed over; `deferred` is live, closeable debt whose stale excerpt is exactly
	// what the repair exists to fix.
	writeTerminal := func(t *testing.T, store, id, status, justification string) {
		t.Helper()
		writeShard(t, store, "2026-09",
			`{"schema_version":3,"id":"`+id+`","run_id":"2026-09-01T00:00:00Z-`+status+`","ts":"2026-09-01T00:00:00Z",`+
				`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p3","fix":"f3","category":"correctness",`+
				`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
				`"status":"`+status+`","resolved_at":"2026-09-01T00:00:00Z",`+
				`"justification":`+strconv.Quote(justification)+`,`+
				`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`)
	}

	findByID := func(t *testing.T, store, month, id string) map[string]any {
		t.Helper()
		for _, m := range shardLines(t, store, month) {
			if m["id"] == id {
				return m
			}
		}
		t.Fatalf("no record %s in shard %s", id, month)
		return nil
	}

	t.Run("never replays over a wontfix record's operator reason", func(t *testing.T) {
		store, reviewRoot := setup(t)
		const reason = "intentional per ADR-12; the alloc is hoisted deliberately"
		writeTerminal(t, store, "cccc3333", StatusWontfix, reason)

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		assert.Equal(t, 2, res.Scanned,
			"a wontfix record is settled: its justification is the operator's --reason, not a review excerpt, so it must not even be scanned")
		assert.Equal(t, reason, findByID(t, store, "2026-09", "cccc3333")["justification"],
			"the human-typed --reason exists nowhere else in the tree and the store is append-only, so replaying over it is irreversible loss")
	})

	t.Run("repairs a deferred record's stale excerpt", func(t *testing.T) {
		store, reviewRoot := setup(t)
		writeTerminal(t, store, "dddd4444", StatusDeferred,
			"- **internal/thing.go:42** the real narrative explaining the defect.")

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		assert.Equal(t, 3, res.Scanned,
			"deferred means \"not now\", not \"done\" — it is live, closeable debt whose excerpt still gates the wontfix path")
		assert.Contains(t, findByID(t, store, "2026-09", "dddd4444")["justification"], "```",
			"the one status class that is both stale and still wontfix-able must be the one the repair reaches")
	})

	// A store where one id carries a resolution trail: detected, resolved with an
	// operator --reason, then RE-DETECTED (FoldRecords rule 2 — a non-suppressing
	// terminal record is displaced by a later open one). The effective record is the
	// ordinary open one, so the id is in scope for the replay — but the resolved
	// line's justification is the operator's typed rationale, not a review excerpt.
	t.Run("rewrites only the lines carrying the stale excerpt, sparing a resolution trail's reason", func(t *testing.T) {
		store, reviewRoot := setup(t)
		const staleText = "- **internal/thing.go:42** the real narrative explaining the defect."
		const operatorReason = "fixed in PR #900 - hoisted the alloc"
		line := func(ts, status, justification string) string {
			st := ""
			if status != "" {
				st = `"status":"` + status + `","resolved_at":"` + ts + `",`
			}
			return `{"schema_version":3,"id":"eeee5555","run_id":"` + ts + `-multi-agent","ts":"` + ts + `",` +
				`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p5","fix":"f5","category":"correctness",` +
				`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` + st +
				`"justification":` + strconv.Quote(justification) + `,` +
				`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
		}
		writeShard(t, store, "2026-09",
			line("2026-09-01T00:00:00Z", "", staleText),
			line("2026-09-02T00:00:00Z", StatusResolved, operatorReason),
			line("2026-09-03T00:00:00Z", "", staleText))

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		got := shardLines(t, store, "2026-09")
		require.Len(t, got, 3)
		assert.Equal(t, operatorReason, got[1]["justification"],
			"the resolved line's justification is the operator's --reason, which exists nowhere else in the tree")
		assert.Contains(t, got[0]["justification"], "```", "a line carrying the stale excerpt is repaired")
		assert.Contains(t, got[2]["justification"], "```", "including the re-detection that copied it")

		var trail []JustificationChange
		for _, c := range res.Changes {
			if c.ID == "eeee5555" {
				trail = append(trail, c)
			}
		}
		require.Len(t, trail, 2,
			"the counter must report LINES: this id's record count says 1 and understates the write to an append-only store")
		assert.Equal(t, 3, res.RewrittenLines, "2 lines for this id plus the fixture's own 1")
		assert.Equal(t, staleText, trail[0].Before)
		assert.Contains(t, trail[0].After, "```")
		assert.Equal(t, "2026-09.jsonl", trail[0].Shard)
		assert.Equal(t, 1, trail[0].Line, "line numbers are 1-based within the shard")
		assert.Equal(t, 3, trail[1].Line)
	})

	t.Run("dry run reports the lines it would touch without writing them", func(t *testing.T) {
		store, reviewRoot := setup(t)
		before, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)

		res, err := BackfillJustifications(store, reviewRoot, true)
		require.NoError(t, err)
		assert.Equal(t, 1, res.RewrittenLines)
		require.Len(t, res.Changes, 1,
			"the documented safety step must be able to show the before/after, not just a count")
		assert.NotEqual(t, res.Changes[0].Before, res.Changes[0].After)

		after, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "a dry run must not touch the store")
	})
}
