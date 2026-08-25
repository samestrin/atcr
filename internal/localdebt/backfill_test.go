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
		assert.Equal(t, 1, res.Unresolved, "the orphan has no review.md anywhere in the tree and must be reported, not guessed at")

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
		assert.Equal(t, 1, res.SkippedSettled,
			"the skip has to be REPORTED: a settled id is suppressed silently, so \"0 scanned\" is otherwise indistinguishable from \"nothing needs repair\"")
	})

	// The skip is per-record inside the fold, so ONE settled record makes the whole
	// id unreachable to the repair. That is the safe direction and stays - but an
	// operator reading "0 scanned, 0 rewritten" over a store that is entirely settled
	// has no way to tell a suppressed scan from an empty one. The counter is the
	// difference.
	t.Run("reports the settled records it suppressed", func(t *testing.T) {
		store, reviewRoot := setup(t)
		// Two settled ids of DIFFERENT classes. writeShard overwrites the month it
		// writes, so they go to separate shards rather than two calls to one.
		writeTerminal(t, store, "cccc3333", StatusWontfix, "intentional per ADR-12")
		writeShard(t, store, "2026-10",
			`{"schema_version":3,"id":"eeee5555","run_id":"2026-10-01T00:00:00Z-resolved","ts":"2026-10-01T00:00:00Z",`+
				`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p4","fix":"f4","category":"correctness",`+
				`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
				`"status":"`+StatusResolved+`","resolved_at":"2026-10-01T00:00:00Z",`+
				`"justification":"fixed in PR #900",`+
				`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`)

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		assert.Equal(t, 2, res.SkippedSettled,
			"both settled classes are counted: resolved and wontfix")
		assert.Equal(t, 2, res.Scanned,
			"the counter reports the suppression; it does not change what is scanned")
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

	// replayCandidates matches candidate files by suffix against the review-dir-
	// RELATIVE source_report.path. A plain strings.HasSuffix is not path-segment
	// aware, so a sibling directory whose name merely ENDS with the first segment
	// matches too — and a second, unrelated narrative turns a repairable record into
	// `ambiguous`: a silently declined repair the operator is told to investigate as
	// a real disagreement.
	t.Run("a sibling directory that merely ends with the first path segment is not a candidate", func(t *testing.T) {
		store, reviewRoot := setup(t)
		rd := filepath.Join(reviewRoot, "sprint-b", "multi-agent", "xsources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
			[]byte(strings.Replace(danglingReview, "the real narrative explaining the defect.", "an UNRELATED narrative from xsources.", 1)), 0o600))

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)
		assert.Zero(t, res.Ambiguous,
			"`xsources/` is not `sources/`; treating it as one declines a repair that has exactly one answer")
		assert.Equal(t, 1, res.Rewritten)
	})

	// The distinct-candidate dedupe: two copies of the same review (a re-run, a
	// backup) agree, and calling that ambiguity would decline a repair with one
	// answer. Nothing pinned this rule before.
	t.Run("two byte-identical copies of one review are one answer, not a disagreement", func(t *testing.T) {
		store, reviewRoot := setup(t)
		rd := filepath.Join(reviewRoot, "sprint-b", "multi-agent", "sources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(danglingReview), 0o600))

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)
		assert.Zero(t, res.Ambiguous, "identical candidates agree")
		assert.Equal(t, 1, res.Rewritten)
	})

	// The producer refuses to stamp from a non-regular file or one over 1 MiB
	// (collectReviewNarratives). A file the producer would never have stamped from
	// must not become an authoritative candidate for the REPLAY either, or the
	// replay set exceeds the stamp set.
	t.Run("a symlinked review.md is not a candidate", func(t *testing.T) {
		store, reviewRoot := setup(t)
		real := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax", "review.md")
		rd := filepath.Join(reviewRoot, "sprint-b", "multi-agent", "sources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		// Points at a DIFFERENT narrative, so if the link is followed the record
		// becomes ambiguous rather than repaired.
		other := filepath.Join(reviewRoot, "other.md")
		require.NoError(t, os.WriteFile(other,
			[]byte(strings.Replace(danglingReview, "the real narrative explaining the defect.", "a narrative reached only through a symlink.", 1)), 0o600))
		require.NoError(t, os.Symlink(other, filepath.Join(rd, "review.md")))
		require.FileExists(t, real)

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)
		assert.Zero(t, res.Ambiguous, "the producer excludes symlinks, so the replayer must too")
		assert.Equal(t, 1, res.Rewritten)
	})

	t.Run("an oversized review.md is not a candidate", func(t *testing.T) {
		store, reviewRoot := setup(t)
		rd := filepath.Join(reviewRoot, "sprint-b", "multi-agent", "sources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		big := strings.Replace(danglingReview, "the real narrative explaining the defect.",
			"an oversized narrative the producer would never have stamped from.", 1) +
			strings.Repeat("\npadding", 1<<18)
		require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(big), 0o600))

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)
		assert.Zero(t, res.Ambiguous, "the producer skips a review.md over 1 MiB, so the replayer must too")
		assert.Equal(t, 1, res.Rewritten)
	})

	// The `sr == nil || sr.Path == "" || r.Justification == ""` precondition weakens
	// to `sr == nil` with the whole tree green. The `sr.Path == ""` arm is the
	// load-bearing one: without it replayCandidates computes rel = "" and matches on
	// an empty suffix, so every review.md under reviewRoot — which DEFAULTS to the
	// whole repo root — becomes a candidate for a record that names no source. In a
	// tree holding one review that resolves, that is a WRONG rewrite on the
	// destructive path; in a larger tree it silently inflates Ambiguous.
	t.Run("a record with no usable source or justification is never scanned", func(t *testing.T) {
		store, reviewRoot := setup(t)
		lineFor := func(id, srPath, justification string) string {
			sr := `"source_report":{"path":` + strconv.Quote(srPath) + `,"line":8}`
			return `{"schema_version":3,"id":"` + id + `","run_id":"2026-09-01T00:00:00Z-multi-agent","ts":"2026-09-01T00:00:00Z",` +
				`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p-` + id + `","fix":"f","category":"correctness",` +
				`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
				`"justification":` + strconv.Quote(justification) + `,` + sr + `}`
		}
		// One record per missing precondition, each otherwise resolvable: same
		// file:line as the surviving review.md, so ONLY the precondition can be
		// keeping them out of the scanned set.
		writeShard(t, store, "2026-09",
			lineFor("ffff6666", "", "- **internal/thing.go:42** the real narrative explaining the defect."),
			lineFor("99990000", "sources/pool/raw/agent/dax/review.md", ""))
		before := shardLines(t, store, "2026-09")

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		assert.Equal(t, 2, res.Scanned,
			"a record naming no source, and one carrying no justification, have nothing to replay — the fixture's own two records are the whole scanned set")
		assert.Zero(t, res.Ambiguous,
			"an empty source_report.path must not match every review.md in the tree")

		after := shardLines(t, store, "2026-09")
		require.Len(t, after, 2)
		for i := range after {
			assert.Equal(t, before[i]["justification"], after[i]["justification"],
				"record %v must be left byte-for-byte alone", after[i]["id"])
		}
	})
}

// The line-scoped guard in rewriteJustifications carries ONE live predicate:
// `cur != rep.from`. It is what makes the rewrite line-scoped rather than id-scoped,
// and every other shape a line can take is a consequence of it, given how `want` is
// built:
//
//   - rep.from is never "" (BackfillJustifications skips a record with an empty
//     justification before it can reach want), so a line whose justification is ""
//     already differs from rep.from.
//   - rep.to is never rep.from (want is populated only where the replayed text
//     DIFFERS from the stored one), so a line already carrying rep.to differs too.
//
// Both invariants live in the want-construction loop, not here, so this pins them from
// the consumer's side: if a future edit lets an empty or already-replayed line reach
// this guard, the rewrite must still decline it.
func TestRewriteJustifications_RewritesOnlyLinesCarryingTheStaleText(t *testing.T) {
	store := t.TempDir()
	const stale = "the stale excerpt"
	const fresh = "```\nthe replayed excerpt"

	line := func(id, justification string) string {
		return `{"schema_version":3,"id":"` + id + `","run_id":"r","ts":"2026-08-01T00:00:00Z",` +
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
			`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
			`"justification":` + strconv.Quote(justification) + `}`
	}
	writeShard(t, store, "2026-08",
		line("aaaa1111", stale), // carries the stale text: the one line to rewrite
		line("aaaa1111", ""),    // empty: differs from rep.from, so it is not the line
		line("aaaa1111", fresh), // already the replayed text: likewise
		line("aaaa1111", "an operator's typed --reason from a resolution trail"),
	)

	changes, err := rewriteJustifications(store, map[string]replacement{
		"aaaa1111": {from: stale, to: fresh},
	}, false)
	require.NoError(t, err)

	require.Len(t, changes, 1, "only the line carrying the stale text may be rewritten")
	assert.Equal(t, 1, changes[0].Line)
	assert.Equal(t, stale, changes[0].Before)
	assert.Equal(t, fresh, changes[0].After)

	got := shardLines(t, store, "2026-08")
	require.Len(t, got, 4)
	assert.Equal(t, fresh, got[0]["justification"])
	assert.Equal(t, "", got[1]["justification"], "an empty justification is left alone, not filled in")
	assert.Equal(t, fresh, got[2]["justification"], "a line already carrying the replacement is not re-marshaled into a change")
	assert.Equal(t, "an operator's typed --reason from a resolution trail", got[3]["justification"],
		"a human-typed rationale on the same id must survive: the store is append-only, so replaying over it is irreversible")
}

// The six IO error wraps in rewriteJustifications are the return-path half of the
// function's signature change from `error` to `([]JustificationChange, error)`, so
// every one of them was rewritten without a test reaching it. Two are reachable
// portably, through directory permissions rather than a fake filesystem: the ReadDir
// wrap on a store that cannot be listed, and the CreateTemp wrap on a store that can
// be read but not written.
//
// The remaining four (ReadFile, json.Marshal, the temp write, and the rename) are
// left uncovered deliberately — reaching them needs either a fake filesystem or a
// value json.Marshal rejects, and each is a single fmt.Errorf wrap over an os error
// whose worst outcome is a less precise message. That is documented on the function
// rather than pinned with a fake, the same stance the repoRoot error arm takes.
func TestRewriteJustifications_WrapsItsIOErrors(t *testing.T) {
	// Permission bits do not constrain root, so this whole class of test is
	// meaningless there — skip rather than assert something false.
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not deny access")
	}

	const stale = "the stale excerpt"
	want := map[string]replacement{"aaaa1111": {from: stale, to: "the replayed excerpt"}}

	t.Run("wraps a store it cannot list", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "unlistable")
		require.NoError(t, os.Mkdir(dir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		_, err := rewriteJustifications(dir, want, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading localdebt dir for backfill",
			"the wrap names the operation; a bare os error reads as if it came from elsewhere in the debt namespace")
	})

	t.Run("wraps a store it cannot write", func(t *testing.T) {
		dir := t.TempDir()
		writeShard(t, dir, "2026-08",
			`{"schema_version":3,"id":"aaaa1111","run_id":"r","ts":"2026-08-01T00:00:00Z",`+
				`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f",`+
				`"category":"correctness","est_minutes":10,"evidence":"e","reviewers":["dax"],`+
				`"confidence":"HIGH","justification":`+strconv.Quote(stale)+`}`)
		// Readable and traversable, but not writable: the shard is read and edited in
		// memory, and the failure lands on the temp file the rewrite publishes through.
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		_, err := rewriteJustifications(dir, want, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "creating temp file for backfill")
	})

	// A dry run reaches neither write path, so the same unwritable store succeeds —
	// which is what makes --dry-run safe to recommend as the first step.
	t.Run("a dry run over an unwritable store still reports what it would change", func(t *testing.T) {
		dir := t.TempDir()
		writeShard(t, dir, "2026-08",
			`{"schema_version":3,"id":"aaaa1111","run_id":"r","ts":"2026-08-01T00:00:00Z",`+
				`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f",`+
				`"category":"correctness","est_minutes":10,"evidence":"e","reviewers":["dax"],`+
				`"confidence":"HIGH","justification":`+strconv.Quote(stale)+`}`)
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		changes, err := rewriteJustifications(dir, want, true)
		require.NoError(t, err)
		require.Len(t, changes, 1)
		assert.Equal(t, stale, changes[0].Before)
	})
}
