package localdebt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reclib "github.com/samestrin/atcr/reconcile"

	"github.com/samestrin/atcr/internal/reconcile"
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
		assert.Equal(t, 1, res.SkippedRationaleBearing,
			"the skip has to be REPORTED: a suppressed id is skipped silently, so \"0 scanned\" is otherwise indistinguishable from \"nothing needs repair\"")
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

		assert.Equal(t, 2, res.SkippedRationaleBearing,
			"both rationale-bearing classes are counted: resolved and wontfix")
		assert.Equal(t, 2, res.Scanned,
			"the counter reports the suppression; it does not change what is scanned")
	})

	// The UNSETTLED half of the bearsRationale switch, which a settledness-named
	// gate and a settledness-named counter together hid. `attempts-exhausted` is
	// not settled — it stays closeable, like deferred — but unlike deferred its
	// --reason is MANDATORY, so it always carries operator text that exists
	// nowhere else. Gating on settledness would replay a review excerpt straight
	// over it: irreversible, in an append-only store, on the one command that
	// rewrites shard lines in place.
	t.Run("suppresses the unsettled rationale-bearing status", func(t *testing.T) {
		store, reviewRoot := setup(t)
		const reason = "three fix attempts each regressed the retry tests"
		writeTerminal(t, store, "cccc3333", StatusAttemptsExhausted, reason)

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		assert.Equal(t, 1, res.SkippedRationaleBearing,
			"attempts-exhausted carries a mandatory operator --reason and must be suppressed")
		assert.Equal(t, reason, findByID(t, store, "2026-09", "cccc3333")["justification"],
			"the operator's attempt trail exists nowhere else and must not be replayed over")
	})

	// deferred is the control: terminal, unsettled, and NOT reason-gated, so its
	// stale excerpt is exactly what this pass exists to repair. It must stay
	// scannable — the widened gate must not have swept it in.
	t.Run("still scans deferred, which carries no operator rationale", func(t *testing.T) {
		store, reviewRoot := setup(t)
		writeTerminal(t, store, "cccc3333", StatusDeferred, "stale review excerpt")

		res, err := BackfillJustifications(store, reviewRoot, false)
		require.NoError(t, err)

		assert.Zero(t, res.SkippedRationaleBearing,
			"deferred records no rationale, so the repair must still reach it")
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
	// ordinary open one, but the resolved line's justification is the operator's
	// typed rationale — which exists nowhere else in the tree — so the ID gate
	// scopes the whole id out of the pass. The line-scoped text inequality used to
	// spare the reason only INCIDENTALLY (where the reason differs from rep.from);
	// where they coincide, the replay overwrote it, irreversibly.
	t.Run("skips an id whose trail carries a resolution's reason, sparing every line on it", func(t *testing.T) {
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

		// The resolved@t2 record bears rationale, so the ID gate scopes the whole id
		// out: its stale excerpts stay unread (the safe over-broad direction), and
		// the reason is spared STRUCTURALLY, not by a text inequality that breaks
		// where the reason coincides with rep.from.
		assert.Equal(t, 1, res.SkippedRationaleBearing,
			"the id is counted through the same visibility counter as an effective rationale-bearing record")
		assert.Equal(t, 1, res.Rewritten, "the fixture's own stale excerpt is still repaired — only the rationale-trail id is out of scope")
		assert.Equal(t, 2, res.Scanned, "the fixture's own two ids; the rationale-trail id never reaches the replay")

		got := shardLines(t, store, "2026-09")
		require.Len(t, got, 3)
		assert.Equal(t, operatorReason, got[1]["justification"],
			"the resolved line's justification is the operator's --reason, which exists nowhere else in the tree")
		assert.Equal(t, staleText, got[0]["justification"],
			"the excerpt line stays stale too — an unrepaired excerpt is live stale text the next pass reports, while a replayed-over --reason cannot be recovered")
		assert.Equal(t, staleText, got[2]["justification"],
			"including the re-detection that copied it")

		var trail []JustificationChange
		for _, c := range res.Changes {
			if c.ID == "eeee5555" {
				trail = append(trail, c)
			}
		}
		require.Empty(t, trail,
			"the whole id is out of the pass's scope, so no change names it")
		assert.Equal(t, 1, res.RewrittenLines, "the fixture's own stale excerpt only — the rationale-trail id contributes nothing")
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

	// pathHasSuffix carries two arms, and every subtest above reaches only the
	// separator-anchored one: they all build reviewRoot from t.TempDir(), which is
	// absolute, so the walked p is absolute and rel never equals it. The exact-match
	// arm needs a RELATIVE review root — filepath.WalkDir joins each entry onto its
	// root and filepath.Join cleans a "." root away, so p IS the review-dir-relative
	// path and equals rel outright. That is not a synthetic shape:
	// cli/debt_backfill.go resolves the repo root by default, and a caller passing
	// --review-root . lands here.
	t.Run("a relative review root is matched on the exact-match arm", func(t *testing.T) {
		root := t.TempDir()
		store := filepath.Join(root, "debt")
		require.NoError(t, os.MkdirAll(store, 0o750))
		reviewRoot := filepath.Join(root, "reviews")
		// Placed DIRECTLY under reviewRoot, with no sprint/multi-agent prefix, so the
		// walked path equals source_report.path exactly rather than merely ending
		// with it — the separator-anchored arm cannot carry this one.
		rd := filepath.Join(reviewRoot, "sources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(danglingReview), 0o600))
		writeShard(t, store, "2026-08",
			`{"schema_version":3,"id":"cccc3333","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
				`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",`+
				`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
				`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",`+
				`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`)

		// The store path is absolute, so it survives the move; only reviewRoot is
		// relative, which is the whole point of the case.
		t.Chdir(reviewRoot)

		res, err := BackfillJustifications(store, ".", false)
		require.NoError(t, err)

		assert.Equal(t, 1, res.Rewritten,
			"the record is REPAIRED through the exact-match arm, not left for a human")
		assert.Zero(t, res.Unresolved,
			"an unresolved count here would mean the walked path never matched rel at all")
		assert.Zero(t, res.Ambiguous)

		got := shardLines(t, store, "2026-08")
		require.Len(t, got, 1)
		assert.Contains(t, got[0]["justification"], "```",
			"the replayed excerpt carries the synthetic dangling-fence marker the stored one lacked")
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

	changes, _, err := rewriteJustifications(store, map[string]replacement{
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

		_, _, err := rewriteJustifications(dir, want, false)
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

		_, _, err := rewriteJustifications(dir, want, false)
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

		changes, _, err := rewriteJustifications(dir, want, true)
		require.NoError(t, err)
		require.Len(t, changes, 1)
		assert.Equal(t, stale, changes[0].Before)
	})
}

// The backfill dry-run LISTING was hardened against terminal-controlling runes, but the
// same command's ERROR paths reach the same terminal. Two untrusted values ride them: a
// shard basename, which os.ReadDir returns verbatim from a world-appendable store
// directory and basePathErr reduces without escaping, and the record id, which is read
// straight out of a store line. A U+202E in either reorders the operator's own report —
// the surface they consult to decide whether to let an in-place rewrite proceed.
func TestRewriteJustifications_ErrorPathsDoNotLeakRawUntrustedNames(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not deny access")
	}

	t.Run("shard basename is escaped, not passed through raw", func(t *testing.T) {
		dir := t.TempDir()
		// A hostile shard name. Made unreadable so the ReadFile wrap fires on it.
		hostile := "2026-08\u202Egnol.jsonl"
		path := filepath.Join(dir, hostile)
		require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
		require.NoError(t, os.Chmod(path, 0o000))
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

		_, _, err := rewriteJustifications(dir, map[string]replacement{"x": {from: "a", to: "b"}}, false)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "\u202E",
			"a raw bidi override in an error reorders the report the operator reads")
	})

	// The 'publishing backfilled shard' wrap is the one the ReadFile case above does
	// NOT cover, and it is the only wrap on this path whose error is an *os.LinkError
	// rather than an *os.PathError. basePathErr matches only *os.PathError, so
	// quotedPathErr fell straight through to `return err` and the raw LinkError reached
	// the terminal — carrying BOTH absolute paths (which store.go's SECURITY contract
	// says can contain a username in ordinary operation) and the shard name unescaped.
	//
	// The LinkError here is produced by a REAL failing os.Rename, not hand-built, so the
	// error shape under test is exactly the one the publish step raises.
	t.Run("a rename failure escapes the shard name and discloses no absolute path", func(t *testing.T) {
		dir := t.TempDir()
		hostile := "2026-08\u202Egnol.jsonl"

		raw := os.Rename(filepath.Join(dir, "absent-source"), filepath.Join(dir, hostile))
		require.Error(t, raw)
		var le *os.LinkError
		require.ErrorAs(t, raw, &le, "the publish step's error shape is *os.LinkError, not *os.PathError")
		require.Contains(t, raw.Error(), "\u202E",
			"guard the guard: the raw LinkError really does pass the rune through, which is what the fix must change")
		require.Contains(t, raw.Error(), dir,
			"guard the guard: the raw LinkError really does disclose the store root")

		got := quotedPathErr(raw).Error()
		require.NotContains(t, got, "\u202E",
			"a raw bidi override in the publish error reorders the report the operator reads")
		require.NotContains(t, got, dir,
			"basePathErr reduces a path to its base name for privacy; the publish wrap must not disclose the store root")
	})

	t.Run("record id is escaped, not passed through raw", func(t *testing.T) {
		id := "aaa\u202E111"
		got := fmt.Errorf("re-encoding backfilled record %s: %w", id, errUnencodable).Error()
		require.Contains(t, got, "\u202E",
			"guard the guard: %s really does pass the rune through, which is what the fix must change")
		require.NotContains(t, reencodeErr(id, errUnencodable).Error(), "\u202E",
			"the re-encode wrap must escape the untrusted record id")
	})
}

var errUnencodable = errors.New("json: unsupported value")

// quotedPathErr has three branches and the LAST one — pass the error through untouched —
// was the branch that let a raw *os.LinkError reach the terminal from the publish wrap.
// It is a silent branch by construction: taking it produces no visible marker, so nothing
// downstream can tell "reduced and escaped" from "handed straight back". Pinning it is
// what makes the LinkError arm above a decision rather than an accident.
func TestQuotedPathErr_BranchesOverTheThreeErrorShapes(t *testing.T) {
	t.Run("a plain error is returned unchanged", func(t *testing.T) {
		base := errors.New("disk went away")
		got := quotedPathErr(base)
		require.Same(t, base, got,
			"an error carrying no path has nothing to reduce or escape; wrapping it would only "+
				"add a layer errors.Is/As callers have to unwrap")
	})

	t.Run("a wrapped plain error keeps its chain", func(t *testing.T) {
		base := errors.New("disk went away")
		got := quotedPathErr(fmt.Errorf("context: %w", base))
		require.ErrorIs(t, got, base, "the fallback must not sever the error chain")
	})

	// The shape the 'writing backfilled shard' and 'creating temp file' wraps raise.
	t.Run("a PathError is reduced to a quoted base name", func(t *testing.T) {
		dir := t.TempDir()
		hostile := "2026-08\u202Egnol.jsonl"

		raw := &os.PathError{Op: "write", Path: filepath.Join(dir, hostile), Err: errors.New("no space left on device")}
		require.Contains(t, raw.Error(), "\u202E",
			"guard the guard: the raw PathError really does pass the rune through")
		require.Contains(t, raw.Error(), dir,
			"guard the guard: the raw PathError really does disclose the store root")

		got := quotedPathErr(raw).Error()
		require.NotContains(t, got, "\u202E",
			"a raw bidi override reorders the report the operator reads")
		require.NotContains(t, got, dir,
			"basePathErr reduces a path to its base name for privacy; the store root must not leak")
		require.Contains(t, got, strconv.Quote(hostile),
			"the base name must survive, escaped, so the operator can still identify the shard")
	})

	// The shape os.Rename raises, which basePathErr alone does not match.
	t.Run("a LinkError has both of its paths reduced and quoted", func(t *testing.T) {
		dir := t.TempDir()
		oldName := "\u202Etmp-source"
		newName := "2026-08\u202Egnol.jsonl"

		raw := os.Rename(filepath.Join(dir, oldName), filepath.Join(dir, newName))
		var le *os.LinkError
		require.ErrorAs(t, raw, &le)

		got := quotedPathErr(raw).Error()
		require.NotContains(t, got, "\u202E")
		require.NotContains(t, got, dir)
		require.Contains(t, got, strconv.Quote(oldName), "the source name is reduced and escaped, not dropped")
		require.Contains(t, got, strconv.Quote(newName), "the target name is reduced and escaped, not dropped")
	})
}

// The 'writing backfilled shard' and 'publishing backfilled shard' wraps are the two
// error paths a caller sees when an in-place rewrite fails halfway, so their operator-
// visible contract is asserted here on the exact error shapes they raise (above), not
// only through rewriteJustifications.
//
// Driving those two CALL SITES from a test is not portable: reaching them needs a store
// directory that is writable when os.CreateTemp runs and unwritable a few statements
// later, which no in-process hook provides. Every unwritable-directory arrangement fails
// earlier, at 'creating temp file for backfill' (pinned in
// TestRewriteJustifications_WrapsItsIOErrors), and a shard whose path is a directory
// cannot be reached at all — rewriteJustifications skips directory entries. What IS
// reachable is asserted: the temp file is removed on either failure, so a failed rewrite
// leaves no debris beside the shard it could not replace.
func TestRewriteJustifications_LeavesNoTempDebrisOnASuccessfulRewrite(t *testing.T) {
	const stale = "the stale excerpt"
	dir := t.TempDir()
	writeShard(t, dir, "2026-08",
		`{"schema_version":3,"id":"aaaa1111","run_id":"r","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f",`+
			`"category":"correctness","est_minutes":10,"evidence":"e","reviewers":["dax"],`+
			`"confidence":"HIGH","justification":`+strconv.Quote(stale)+`}`)

	changes, _, err := rewriteJustifications(dir, map[string]replacement{
		"aaaa1111": {from: stale, to: "the replayed excerpt"},
	}, false)
	require.NoError(t, err)
	require.Len(t, changes, 1)

	entries, rerr := os.ReadDir(dir)
	require.NoError(t, rerr)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), "."),
			"the publish step renames its temp file into place; a leftover .tmp-* is debris in the store: %s", e.Name())
	}
	require.Len(t, entries, 1, "the store must hold exactly the one shard it started with")
}

// The dry-run listing in cli/ resolves locator collisions, and it must describe the
// SAME directory snapshot the rewrite was computed against. Before this, `cli` ran its
// own os.ReadDir after BackfillJustifications had already returned — outside the
// withLock region rewriteJustifications' walk ran inside — so a concurrent writer that
// removed a colliding shard in that window suppressed the "#hash" suffix the listing
// exists to add, and the operator approved a bare token for a name that WAS ambiguous
// when the rewrite was computed. atcr's own CLAUDE.md notes concurrent sessions share
// this tree, so that writer is not hypothetical.
//
// ShardNames closes the window by carrying the locked walk's own observation out to the
// caller, so there is exactly one listing and one filter rather than two that must be
// kept in step.
func TestBackfillJustifications_ReturnsTheShardNamesObservedUnderTheLock(t *testing.T) {
	dir := t.TempDir()
	reviewRoot := t.TempDir()
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
		[]byte("## Findings\n\n- **internal/thing.go:42** the real narrative explaining the defect.\n"), 0o600))

	rec := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"stale text that the replay will replace",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-08.jsonl"), []byte(rec+"\n"), 0o600))
	// An unchanged shard: it produces no JustificationChange, so the change set alone
	// cannot see it — only the directory snapshot can. That is the whole reason the
	// snapshot has to travel out rather than being reconstructed from Changes.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-07.jsonl"), []byte(""), 0o600))
	// Non-shard entries, one for each half of the walk's filter. Neither may appear in
	// the snapshot, or the caller's collision map gains names that are not shards.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a shard\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "2026-06.jsonl"), 0o750))

	res, err := BackfillJustifications(dir, reviewRoot, true)
	require.NoError(t, err)
	require.NotEmpty(t, res.Changes, "the fixture must produce a change, or the snapshot proves nothing")

	assert.ElementsMatch(t, []string{"2026-07.jsonl", "2026-08.jsonl"}, res.ShardNames,
		"the snapshot must carry every shard the locked walk saw — including one with "+
			"nothing to repair — and nothing that is not a shard")
}

// ShardNames' doc historically said it "is nil when no rewrite was needed", which
// made "the store holds no shards" and "the store holds shards, none needing repair"
// the same value — and made the field's meaning silently coupled to Changes being
// non-empty. cli/debt_backfill.go's locator rendering leans on the field describing
// the locked walk's observation, and an unchanged colliding shard is exactly the
// collision the change set cannot see. A pass with nothing to rewrite still walked
// nothing, so the observation was never recorded; this pins that the early return
// records it too.
func TestBackfillJustifications_ReportsShardNamesEvenWhenNothingNeedsRewrite(t *testing.T) {
	dir := t.TempDir()
	reviewRoot := t.TempDir()

	// An empty shard: holds no records, so it can never produce a change.
	writeShard(t, dir, "2026-07")
	// A shard whose only record is SETTLED: skipped in the fold before any replay,
	// so `want` stays empty and the pass takes the no-rewrite early return.
	rec := `{"schema_version":3,"id":"bbbb2222","run_id":"r","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f",` +
		`"category":"correctness","est_minutes":10,"evidence":"e","reviewers":["dax"],` +
		`"confidence":"HIGH","status":"resolved","justification":"already settled",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`
	writeShard(t, dir, "2026-08", rec)
	// Both halves of the shard filter: a non-.jsonl file and a directory whose name
	// ends in .jsonl. Neither may reach the snapshot.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a shard\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "2026-06.jsonl"), 0o750))

	res, err := BackfillJustifications(dir, reviewRoot, true)
	require.NoError(t, err)
	require.Empty(t, res.Changes, "the fixture must produce no change, or this is not the no-rewrite path")

	assert.ElementsMatch(t, []string{"2026-07.jsonl", "2026-08.jsonl"}, res.ShardNames,
		"a pass with nothing to rewrite must still report every shard the locked walk "+
			"saw — nil here conflates 'no shards' with 'shards, none needing repair'")
}

// Cumulative-review correction: the no-rewrite snapshot's tolerance for a MISSING
// store directory (the legal "no backlog yet" state ReadAll already tolerates) is
// documented on the field but was pinned by no test — a future edit could start
// surfacing ENOENT as a hard error on an empty backlog and no test would notice.
// (The store directory itself existing after the call is withLock's own
// ensureStoreDir precondition — lock_unix.go, lock_other.go — not this pass's behavior, so it is not
// asserted here.)
func TestBackfillJustifications_MissingStoreDirIsTheNoBacklogState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	res, err := BackfillJustifications(dir, t.TempDir(), true)
	require.NoError(t, err, "a missing store directory is the no-backlog state, not a failure")
	assert.Nil(t, res.ShardNames, "no shards were observed, so the snapshot must be nil")
}

// The no-rewrite snapshot's os.ReadDir carries a two-armed guard: ENOENT is the
// legal "no backlog yet" state, any other listing failure is fatal. The fatal arm
// showed 0 coverage and survived mutation (adding `false &&` left the package
// green), which reads as a missing test — it is not. It is UNREACHABLE through
// this entry point, and that is worth pinning rather than papering over: ReadAll
// lists the same directory first and returns any non-ENOENT failure itself, so
// control never arrives at the snapshot with a listable-directory problem.
//
// This table pins BOTH arms and the ordering that makes the second one dead. If a
// later change makes ReadAll tolerant of a listing failure, the second case starts
// failing here and says so — which is the signal that the backfill guard has
// become live and now owes its own coverage.
func TestBackfillJustifications_NoRewriteSnapshotListingArms(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not deny access")
	}

	t.Run("a missing store directory is the no-backlog state", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "does-not-exist")
		res, err := BackfillJustifications(dir, t.TempDir(), true)
		require.NoError(t, err, "ENOENT is the legal empty-backlog state, not a failure")
		assert.Nil(t, res.ShardNames, "no shards were observed, so the snapshot must be nil")
		assert.Empty(t, res.Changes)
	})

	t.Run("an unlistable store fails in ReadAll, before the snapshot guard", func(t *testing.T) {
		// Write + execute, NOT read: withLock can still create its lock file by
		// name, while listing the directory is denied. That is the only shape in
		// which a listing failure can reach this pass at all.
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o300))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		_, err := BackfillJustifications(dir, t.TempDir(), true)
		require.Error(t, err, "a non-ENOENT listing failure must be fatal somewhere")
		assert.Contains(t, err.Error(), "reading localdebt dir:",
			"ReadAll owns this failure")
		assert.NotContains(t, err.Error(), "reading localdebt dir for backfill:",
			"the snapshot guard is unreachable while ReadAll lists first and fails; "+
				"if this assertion breaks, that guard is now live and needs its own test")
	})
}

// A non-dry run renames each shard into place AS IT WALKS, so a failure on a later
// shard leaves the earlier ones already rewritten in an append-only store. Returning
// nil there reported "nothing happened" over a store that had in fact been mutated,
// and the operator had no way to learn which shards to reconcile.
func TestRewriteJustifications_ReturnsWhatItAlreadyWroteWhenALaterShardFails(t *testing.T) {
	const stale = "the stale excerpt"
	dir := t.TempDir()
	writeShard(t, dir, "2026-08",
		`{"schema_version":3,"id":"aaaa1111","run_id":"r","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f",`+
			`"category":"correctness","est_minutes":10,"evidence":"e","reviewers":["dax"],`+
			`"confidence":"HIGH","justification":`+strconv.Quote(stale)+`}`)
	// A DANGLING symlink named like a shard. os.ReadDir lists it and IsShardEntry
	// accepts it (not a directory, .jsonl suffix), so the walk reaches it only AFTER
	// 2026-08 has been renamed into place — and os.ReadFile fails there. This is the
	// mid-pass failure the row is about, reached without a fake filesystem.
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.jsonl"), filepath.Join(dir, "2026-09.jsonl")))

	changes, shards, err := rewriteJustifications(dir, map[string]replacement{
		"aaaa1111": {from: stale, to: "the replayed excerpt"},
	}, false)
	require.Error(t, err)
	require.Len(t, changes, 1,
		"the shard already renamed into place must travel out with the error, not be discarded")
	assert.Equal(t, "2026-08.jsonl", changes[0].Shard)
	assert.Equal(t, stale, changes[0].Before)
	assert.Contains(t, shards, "2026-08.jsonl",
		"the snapshot is taken before the walk, so it survives a mid-pass failure too")

	b, rerr := os.ReadFile(filepath.Join(dir, "2026-08.jsonl"))
	require.NoError(t, rerr)
	assert.Contains(t, string(b), "the replayed excerpt",
		"the reported write must really be on disk: reporting one that is not is the inverse error")
}

// The partial write has to survive BackfillJustifications too. It discarded the
// closure's result wholesale with `return BackfillResult{}, err`, so even once
// rewriteJustifications reports what it wrote, the operator saw zero counters.
func TestBackfillJustifications_ReportsThePartialWriteWhenALaterShardFails(t *testing.T) {
	dir := t.TempDir()
	reviewRoot := t.TempDir()
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
		[]byte("## Findings\n\n- **internal/thing.go:42** the real narrative explaining the defect.\n"), 0o600))

	rec := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"stale text that the replay will replace",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`
	writeShard(t, dir, "2026-08", rec)
	// ReadAll skips an ENOENT shard (a dangling link reads as missing), so the scan
	// phase succeeds and the failure lands where the row says it does: in the rewrite
	// walk, after the first shard was published.
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.jsonl"), filepath.Join(dir, "2026-09.jsonl")))

	res, err := BackfillJustifications(dir, reviewRoot, false)
	require.Error(t, err)
	require.Len(t, res.Changes, 1,
		"a zero result over a store that was already mutated is the defect: the written shard must be named")
	assert.Equal(t, "2026-08.jsonl", res.Changes[0].Shard)
	assert.Equal(t, 1, res.RewrittenLines)
	assert.Contains(t, res.ShardNames, "2026-08.jsonl")
}

// A regressed id (open@t1 -> attempts-exhausted@t2 -> open@t3) folds to the LATEST
// open record, so the per-effective-record rationale gate does not fire and the id is
// scanned. The superseded attempts-exhausted line is then protected only by the text
// inequality in rewriteJustifications — and where the regression record carries the
// operator's --reason verbatim (re-detection copies the effective record), the trail
// line matches rep.from and is replayed over: the operator's typed reason is
// overwritten with a review excerpt, irreversibly. The gate must be ID-scoped: an id
// with ANY rationale-bearing record is skipped outright.
func TestBackfillJustifications_IdGateProtectsRationaleTrailLineOnRegression(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	require.NoError(t, os.MkdirAll(store, 0o750))
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(danglingReview), 0o600))

	const reason = "operator reason"
	base := `{"schema_version":3,"id":"dddd4444","run_id":"2026-08-01T00:00:00Z-multi-agent",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`
	writeShard(t, store, "2026-08",
		// open@t1: the originally stamped review excerpt.
		base+`"ts":"2026-08-01T00:00:00Z","status":"`+"open"+`",`+
			`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",`+
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`,
		// attempts-exhausted@t2: the operator's MANDATORY --reason, which exists
		// nowhere else in the tree.
		base+`"ts":"2026-08-02T00:00:00Z","status":"`+StatusAttemptsExhausted+`",`+
			`"justification":"`+reason+`",`+
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`,
		// open@t3: the regression record — effective for the fold, and carrying the
		// reason text verbatim. The replayed excerpt differs from it, so want IS
		// built, and the trail line's text coincides with rep.from.
		base+`"ts":"2026-08-03T00:00:00Z","status":"`+"open"+`",`+
			`"justification":"`+reason+`",`+
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`)

	res, err := BackfillJustifications(store, reviewRoot, false)
	require.NoError(t, err)

	assert.Equal(t, 0, res.Scanned,
		"an id with a rationale-bearing record anywhere in its trail is skipped, not scanned")
	assert.Equal(t, 1, res.SkippedRationaleBearing,
		"the id-gate skip is counted through the same visibility counter")
	assert.Zero(t, res.Rewritten, "the id's lines must never be replayed over")

	got := shardLines(t, store, "2026-08")
	require.Len(t, got, 3)
	assert.Equal(t, reason, got[1]["justification"],
		"the superseded attempts-exhausted line keeps the operator's --reason; only the text inequality protected it before")
	assert.Equal(t, reason, got[2]["justification"],
		"the effective open line keeps its stored text too — the whole id is out of the pass's scope")
}

// TD cli/debt_resolve.go:81 — a policy refusal must be distinguishable from a pruned
// review tree.
//
// ReExtractJustification returns ok=false, err=nil for BOTH "no review.md survives at
// this path" and "the producer's policy excludes this file" (over the 1 MiB cap, a
// symlink, a wholly-salvaged reply, a desynced bin list, a draft anchor line). Both
// landed in Unresolved, which this result's own doc says a caller must read as an
// OBSERVATION rather than a cause — but the CLI label and the debt_resolve SCOPE
// paragraph both read it as "restore the file", which is impossible for the policy
// half. The replay set may not exceed the stamp set, so the refusals stay; only the
// REPORTING splits.
//
// The observable is a distinct counter, not the wording: a caller cannot recover the
// split from an integer that already summed the two.
func TestBackfillJustifications_SeparatesPolicyRefusalsFromMissingTrees(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	// Two records, one per class, each otherwise identical in every column the pass
	// reads — so the counter they land in is the ONLY thing that differs.
	rec := func(id, srPath string) string {
		return `{"schema_version":3,"id":"` + id + `","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p-` + id + `","fix":"f","category":"correctness",` +
			`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
			`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",` +
			`"source_report":{"path":"` + srPath + `","line":8}}`
	}

	// MISSING: the source_report names a review.md no directory holds.
	// POLICY: the review.md IS present and readable at its own path, over the size cap
	// so the producer's policy excludes it. A whole-file arm of the cap, so the
	// narrative body never matters.
	// The review dir carries the reconciled_at both records' run_id was minted from,
	// so the over-cap file is provably the record's OWN file, not a namesake.
	ownDir := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
	writeReconciledSummary(t, ownDir, "2026-08-01T00:00:00Z")
	rd := filepath.Join(ownDir, "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
		[]byte(strings.Repeat("padding to clear the producer's 1 MiB cap\n", 1<<16)), 0o600))

	writeShard(t, store, "2026-08",
		rec("aaaa0001", "sources/pool/raw/agent/gone/review.md"),
		rec("aaaa0002", "sources/pool/raw/agent/dax/review.md"))

	res, err := BackfillJustifications(store, reviewRoot, false)
	require.NoError(t, err)

	assert.Equal(t, 2, res.Scanned)
	assert.Equal(t, 2, res.Unresolved,
		"both records yielded no excerpt, so the observation count is unchanged")
	assert.Equal(t, 1, res.PolicyUnrepairable,
		"the over-cap review.md is PRESENT at its own path — its refusal is by policy, not absence")
	assert.Equal(t, 1, res.Unresolved-res.PolicyUnrepairable,
		"the orphan's review.md survives nowhere, so it alone is the missing-tree class")
	assert.Zero(t, res.Rewritten, "neither class may be rewritten")
	assert.Zero(t, res.Ambiguous)
}

// A NAMESAKE in an unrelated review must not be read as the record's own file.
//
// PolicyUnrepairable's whole contract is "the file is there and still cannot help, so
// restoring one will not fix it" — and cli/debt_resolve.go's SCOPE paragraph tells the
// operator exactly that. Deriving it from "some candidate at this relative path was
// present and nothing matched" cannot support the claim: SourceReport.Path is
// review-dir-RELATIVE and pathHasSuffix is review-dir-UNSCOPED, so every review
// directory in the tree holds a same-named candidate (BackfillJustifications' own doc,
// backfill.go:141-146). With .atcr/reviews/ holding many reviews for the same agent,
// that made the pruned-tree count read near zero and sent the operator away from the
// one remedy that would have worked (TD internal/localdebt/backfill.go:389).
//
// The provable claim is narrower and path-independent: a candidate the producer's
// FILE-LEVEL policy would refuse to stamp from at all — over the size cap, a wholly
// salvaged reply, a desynced bin list — is unrepairable wherever it sits. A namesake
// that is an ordinary, in-cap review.md whose anchor simply does not match is a
// MISMATCH, and says nothing about whether the record's own tree survives.
func TestBackfillJustifications_NamesakeInAnotherReviewIsNotAPolicyRefusal(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	rec := `{"schema_version":3,"id":"aaaa0001","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`

	// The record's OWN review is gone. An UNRELATED review (sprint-b) happens to hold
	// the same relative path, with an ordinary in-cap review.md that does not carry
	// this record's anchor — a mismatch, not a policy refusal.
	other := filepath.Join(reviewRoot, "sprint-b", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(other, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(other, "review.md"),
		[]byte("# some other review\n\n- **internal/other.go:9** an unrelated narrative.\n"), 0o600))

	writeShard(t, store, "2026-08", rec)

	res, err := BackfillJustifications(store, reviewRoot, false)
	require.NoError(t, err)

	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 1, res.Unresolved, "nothing yielded an excerpt, so the observation count stands")
	assert.Zero(t, res.PolicyUnrepairable,
		"the only candidate is a namesake in an unrelated review that the producer's policy would "+
			"have stamped from quite happily — it simply does not carry this record's anchor, so it is "+
			"no evidence that restoring the record's own review.md cannot help")
	assert.Equal(t, 1, res.Unresolved-res.PolicyUnrepairable,
		"the record's own tree is pruned, so it belongs wholly to the missing-tree class")
}

// A NON-REGULAR candidate is a policy refusal, and this is the test the guard never
// had. internal/reconcile's collectReviewNarratives deliberately excludes symlinks,
// FIFOs and devices named review.md — and ReExtractJustification's os.ReadFile would
// FOLLOW a link, so a file the producer would never have stamped from must not become
// an authoritative candidate: the replay set may not exceed the stamp set.
//
// Deleting the `policyRefused = true` line left ./internal/localdebt/... and ./cli/...
// fully green, so nothing pinned the one behaviour the guard exists for. A later edit
// could silently route all three non-regular kinds back under the "restore the file"
// remedy with the suite still passing (TD internal/localdebt/backfill.go:366).
//
// The symlink stands for the class. It is the only one of the three that is portable
// to create in a test and the only one reachable by ordinary means (a FIFO needs
// mkfifo, a device node needs root), and all three take the identical code path —
// d.Type().IsRegular() is false for every one of them.
func TestBackfillJustifications_SymlinkCandidateIsAPolicyRefusalNotAnAbsentTree(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	rec := `{"schema_version":3,"id":"aaaa0001","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`

	// The target is a REAL review.md that carries the record's anchor, so the only
	// thing standing between the replay and a successful excerpt is the symlink — if
	// the guard were absent, os.ReadFile would follow it and the record would resolve.
	// That is what makes this a guard test rather than a coincidence.
	target := filepath.Join(root, "elsewhere", "review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target,
		[]byte("# review\n\n- **internal/thing.go:42** the narrative a follow would have stamped.\n"), 0o600))

	// The symlink sits in the record's OWN review dir — its summary.json carries the
	// reconciled_at the run_id was minted from — so the refusal is attributable.
	ownDir := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
	writeReconciledSummary(t, ownDir, "2026-08-01T00:00:00Z")
	rd := filepath.Join(ownDir, "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))

	writeShard(t, store, "2026-08", rec)

	res, err := BackfillJustifications(store, reviewRoot, false)
	require.NoError(t, err)

	assert.Equal(t, 1, res.Scanned)
	assert.Zero(t, res.Rewritten,
		"the symlink must never yield an authoritative excerpt — following it would let the replay "+
			"stamp from a file the producer refused, so the replay set would exceed the stamp set")
	assert.Equal(t, 1, res.Unresolved, "nothing yielded an excerpt, so the observation count stands")
	assert.Equal(t, 1, res.PolicyUnrepairable,
		"a symlink named review.md in the record's own review dir is refused by policy, so restoring "+
			"a file cannot fix it and the operator must not be sent looking for one")
}

// TD internal/localdebt/backfill.go:533 — replayCandidates writes through its
// ownershipCache, and a nil map panics on write. BackfillJustifications always hands
// it one, but a future caller (or a test) passing the zero value would reach the
// write only on the rare policy-refused path, so the panic would hide until then.
// A nil cache is defaulted rather than trusted: the refusal is still attributed.
func TestReplayCandidates_NilOwnershipCacheDoesNotPanic(t *testing.T) {
	root := t.TempDir()
	reviewRoot := filepath.Join(root, "reviews")

	target := filepath.Join(root, "elsewhere", "review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target,
		[]byte("# review\n\n- **internal/thing.go:42** the narrative a follow would have stamped.\n"), 0o600))

	ownDir := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
	writeReconciledSummary(t, ownDir, "2026-08-01T00:00:00Z")
	rd := filepath.Join(ownDir, "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))

	rec := Record{
		RunID:        "2026-08-01T00:00:00Z-multi-agent",
		File:         "internal/thing.go",
		Line:         42,
		SourceReport: &SourceReport{Path: "sources/pool/raw/agent/dax/review.md", Line: 3},
	}

	var res replayResult
	var err error
	require.NotPanics(t, func() { res, err = replayCandidates(newReviewTree(reviewRoot, []Record{rec}), rec, nil) })
	require.NoError(t, err)
	assert.Empty(t, res.texts, "the symlink must never yield an excerpt")
	assert.True(t, res.policyRefused,
		"the refusal sits in the record's own review dir, so a defaulted cache still attributes it")
	assert.False(t, res.policyUnattributed,
		"an attributed refusal is not also an unattributed one: the two are exclusive")
}

// TD internal/localdebt/backfill.go:382 — the namesake narrowing closed the ordinary
// in-cap arm (TestBackfillJustifications_NamesakeInAnotherReviewIsNotAPolicyRefusal)
// but left the symlink and over-cap arms speaking for a record they cannot be shown
// to belong to. The policy VERDICT on those files is path-independent; attributing it
// to this record is not. With the record's own tree pruned and a same-named file the
// producer would refuse in an UNRELATED review, the operator was told "0 review tree
// pruned, 1 declined by policy" — sent away from the one restore that would work.
//
// Ownership is checkable from disk: a record's run_id is
// `<reconciled_at>-<base(reviewDir)>` (internal/localdebt/reconcile.go), and the same
// reconciled_at lives in `<reviewDir>/reconciled/summary.json`. The unrelated review
// here carries a summary.json with a DIFFERENT reconciled_at, so it is a real review
// that provably does not own the record — not merely one missing its summary.
func TestBackfillJustifications_PolicyRefusingNamesakeInAnotherReviewIsNotAttributed(t *testing.T) {
	rec := `{"schema_version":3,"id":"aaaa0001","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`

	cases := []struct {
		name  string
		plant func(t *testing.T, root, reviewMD string)
	}{
		{"symlink namesake", func(t *testing.T, root, reviewMD string) {
			target := filepath.Join(root, "elsewhere", "review.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
			require.NoError(t, os.WriteFile(target,
				[]byte("# review\n\n- **internal/thing.go:42** the narrative a follow would have stamped.\n"), 0o600))
			require.NoError(t, os.Symlink(target, reviewMD))
		}},
		{"over-cap namesake", func(t *testing.T, _, reviewMD string) {
			require.NoError(t, os.WriteFile(reviewMD,
				[]byte(strings.Repeat("padding to clear the producer's 1 MiB cap\n", 1<<16)), 0o600))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := filepath.Join(root, "debt")
			reviewRoot := filepath.Join(root, "reviews")
			require.NoError(t, os.MkdirAll(store, 0o750))

			// The record's OWN review (2026-08-01) is gone. sprint-b is a different
			// review — same base, different reconciled_at — holding a same-named file
			// the producer's policy would refuse.
			otherDir := filepath.Join(reviewRoot, "sprint-b", "multi-agent")
			writeReconciledSummary(t, otherDir, "2026-08-09T00:00:00Z")
			rd := filepath.Join(otherDir, "sources", "pool", "raw", "agent", "dax")
			require.NoError(t, os.MkdirAll(rd, 0o750))
			tc.plant(t, root, filepath.Join(rd, "review.md"))

			writeShard(t, store, "2026-08", rec)

			res, err := BackfillJustifications(store, reviewRoot, false)
			require.NoError(t, err)

			assert.Equal(t, 1, res.Scanned)
			assert.Zero(t, res.Rewritten)
			assert.Equal(t, 1, res.Unresolved, "nothing yielded an excerpt, so the observation count stands")
			assert.Zero(t, res.PolicyUnrepairable,
				"the refused file belongs to an unrelated review, so it is no evidence that restoring "+
					"the record's own review.md cannot help")
			assert.Equal(t, 1, res.Unresolved-res.PolicyUnrepairable,
				"the record's own tree is pruned, so it belongs wholly to the missing-tree class")
		})
	}
}

// writeReconciledSummary stamps a minimal `<reviewDir>/reconciled/summary.json`
// carrying reconciledAt — the one field replayCandidates' ownership check reads.
func writeReconciledSummary(t *testing.T, reviewDir, reconciledAt string) {
	t.Helper()
	dir := filepath.Join(reviewDir, "reconciled")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "summary.json"),
		[]byte(`{"reconciled_at":"`+reconciledAt+`"}`), 0o600))
}

// TestReviewDirOwnsRun pins every outcome of the ownership check, not just the
// match and mismatch the replay tests reach. The "owns NOTHING" arms are the safe
// under-count the doc promises: a regression that returned true on a missing,
// unreadable or unparseable summary.json would attribute a policy refusal to a
// record whose own review proves nothing, and every replay test would stay green.
func TestReviewDirOwnsRun(t *testing.T) {
	const at = "2026-08-04T10:00:00Z"
	cases := []struct {
		name    string
		base    string                       // the review dir's base name
		summary func(t *testing.T, d string) // stamps <reviewDir>/reconciled/summary.json; nil leaves it absent
		runID   string
		chdir   bool   // run from inside the review dir and pass it as "."
		link    string // with chdir, enter the review dir through a symlink of this name
		want    bool
	}{
		{
			name:  "no summary.json owns nothing",
			base:  "multi-agent",
			runID: at + "-multi-agent",
		},
		{
			name: "unreadable summary.json owns nothing",
			base: "multi-agent",
			summary: func(t *testing.T, d string) {
				// A directory in the file's place fails os.ReadFile even as root,
				// where a chmod 000 would not.
				require.NoError(t, os.MkdirAll(filepath.Join(d, "reconciled", "summary.json"), 0o750))
			},
			runID: at + "-multi-agent",
		},
		{
			name:    "invalid JSON owns nothing",
			base:    "multi-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"`),
			runID:   at + "-multi-agent",
		},
		{
			name:    "empty object (no reconciled_at) owns nothing",
			base:    "multi-agent",
			summary: rawSummary(`{}`),
			// The run_id an empty reconciled_at would mint: a check that compared
			// without the emptiness guard would match it.
			runID: "-multi-agent",
		},
		{
			name:    "matching reconciled_at and base owns the run",
			base:    "multi-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"}`),
			runID:   at + "-multi-agent",
			want:    true,
		},
		{
			name:    "mismatching reconciled_at does not own the run",
			base:    "multi-agent",
			summary: rawSummary(`{"reconciled_at":"2026-08-04T10:00:01Z"}`),
			runID:   at + "-multi-agent",
		},
		{
			name:    "matching reconciled_at under a different dir base does not own the run",
			base:    "single-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"}`),
			runID:   at + "-multi-agent",
		},
		{
			// Valid JSON whose reconciled_at WOULD match, padded past the cap with
			// trailing whitespace. Any prefix that ends inside the padding still parses,
			// so the read's truncation alone cannot refuse it: only a size check
			// can, and a read that dropped it would own the run.
			name: "oversized summary.json owns nothing",
			base: "multi-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"}` +
				strings.Repeat(" ", maxSummaryBytes)),
			runID: at + "-multi-agent",
		},
		{
			// A symlink in the file's place, to a summary whose reconciled_at WOULD
			// match: only the regular-file check refuses it, and a read that followed
			// the link would own the run.
			name: "symlinked summary.json owns nothing",
			base: "multi-agent",
			summary: func(t *testing.T, d string) {
				target := filepath.Join(t.TempDir(), "summary.json")
				require.NoError(t, os.WriteFile(target, []byte(`{"reconciled_at":"`+at+`"}`), 0o600))
				require.NoError(t, os.MkdirAll(filepath.Join(d, "reconciled"), 0o750))
				require.NoError(t, os.Symlink(target, filepath.Join(d, "reconciled", "summary.json")))
			},
			runID: at + "-multi-agent",
		},
		{
			// `--review-root .` from inside the review dir: reviewDirOf hands back
			// ".", whose filepath.Base is ".", not the dir's name. The base must come
			// from the absolute path or a same-dir record is never attributed.
			name:    "review dir given as \".\" owns the run by its real name",
			base:    "multi-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"}`),
			runID:   at + "-multi-agent",
			chdir:   true,
			want:    true,
		},
		{
			// `--review-root .` from inside a review dir reached through a symlink.
			// The mint takes the base of the path as typed, so a run minted through
			// the link carries the link's name; the absolute path of "." keeps it.
			name:    "symlinked review dir given as \".\" owns a run minted from the link's name",
			base:    "multi-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"}`),
			runID:   at + "-latest",
			chdir:   true,
			link:    "latest",
			want:    true,
		},
		{
			// The same dir's run minted through its real path carries the target's
			// name, which only resolving the link recovers.
			name:    "symlinked review dir given as \".\" owns a run minted from the target's name",
			base:    "multi-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"}`),
			runID:   at + "-multi-agent",
			chdir:   true,
			link:    "latest",
			want:    true,
		},
		{
			name:    "symlinked review dir given as \".\" does not own a run minted from neither name",
			base:    "multi-agent",
			summary: rawSummary(`{"reconciled_at":"` + at + `"}`),
			runID:   at + "-single-agent",
			chdir:   true,
			link:    "latest",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reviewDir := filepath.Join(t.TempDir(), tc.base)
			require.NoError(t, os.MkdirAll(reviewDir, 0o750))
			if tc.summary != nil {
				tc.summary(t, reviewDir)
			}
			arg := reviewDir
			if tc.chdir {
				dir := reviewDir
				if tc.link != "" {
					dir = filepath.Join(t.TempDir(), tc.link)
					require.NoError(t, os.Symlink(reviewDir, dir))
				}
				t.Chdir(dir)
				arg = "."
			}
			assert.Equal(t, tc.want, reviewDirOwnsRun(arg, tc.runID))
		})
	}
}

// TD internal/localdebt/backfill.go:376 — the ownership check re-read summary.json
// for every policy-refused candidate. One backfill pass must read a review dir's
// summary.json at most once, however many records' refusals land in that dir.
//
// Three records, each pointing at its own symlinked review.md (a policy refusal) in
// ONE owning review dir, so each record's replay asks the same dir whether it owns
// the run. Every refusal must still be attributed, from a single open.
func TestBackfillJustifications_ReadsEachReviewDirSummaryOnce(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	opens := map[string]int{}
	seams := defaultBackfillSeams()
	seams.open = func(name string) (*os.File, error) {
		opens[name]++
		return os.Open(name)
	}

	target := filepath.Join(root, "elsewhere", "review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target,
		[]byte("# review\n\n- **internal/thing.go:42** the narrative a follow would have stamped.\n"), 0o600))

	ownDir := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
	writeReconciledSummary(t, ownDir, "2026-08-01T00:00:00Z")

	agents := []string{"dax", "kira", "odo"}
	lines := make([]string, 0, len(agents))
	for i, agent := range agents {
		rd := filepath.Join(ownDir, "sources", "pool", "raw", "agent", agent)
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))
		id := fmt.Sprintf("aaaa000%d", i+1)
		lines = append(lines, `{"schema_version":3,"id":"`+id+`","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p-`+id+`","fix":"f","category":"correctness",`+
			`"est_minutes":10,"evidence":"e","reviewers":["`+agent+`"],"confidence":"HIGH",`+
			`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",`+
			`"source_report":{"path":"sources/pool/raw/agent/`+agent+`/review.md","line":3}}`)
	}
	writeShard(t, store, "2026-08", lines...)

	res, err := backfillJustifications(store, reviewRoot, false, seams)
	require.NoError(t, err)

	assert.Equal(t, len(agents), res.PolicyUnrepairable,
		"every refusal sits in the record's own review dir, so each is still attributed")
	assert.Equal(t, map[string]int{filepath.Join(ownDir, "reconciled", "summary.json"): 1}, opens,
		"the owning dir's summary.json is opened once for the whole pass, not once per refused candidate")
}

// TD internal/localdebt/backfill.go:464 — an oversized summary.json owned nothing
// silently. The skip is logged at debug with the path and the size, as the
// producer's oversized review.md is (internal/reconcile/justification.go).
// The logger is injected, so the test leaves the process-wide default alone.
func TestReadReconciledAt_OversizedLogsAtDebug(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	reviewDir := t.TempDir()
	body := `{"reconciled_at":"2026-08-04T10:00:00Z"}` + strings.Repeat(" ", maxSummaryBytes)
	rawSummary(body)(t, reviewDir)

	assert.Empty(t, readReconciledAt(reviewDir, os.Open, log))
	out := buf.String()
	assert.Contains(t, out, "level=DEBUG")
	assert.Contains(t, out, filepath.Join(reviewDir, "reconciled", "summary.json"))
	assert.Contains(t, out, "size="+strconv.Itoa(len(body)))
}

// TD internal/localdebt/backfill.go:440 — a candidate whose path IS rel (the walk
// rooted at the review dir itself, `--review-root .`) sits in the review dir ".".
// Pinned because "." is still a usable path for reading summary.json; its Base is
// not the dir's name, which reviewDirOwnsRun answers by taking the base from the
// absolute path (the "review dir given as \".\"" case in TestReviewDirOwnsRun).
func TestReviewDirOf_RelEqualsPath(t *testing.T) {
	rel := filepath.FromSlash("a/b.md")
	assert.Equal(t, ".", reviewDirOf(rel, rel))
}

// rawSummary returns a setup that writes body verbatim as the review's
// reconciled/summary.json.
func rawSummary(body string) func(t *testing.T, reviewDir string) {
	return func(t *testing.T, reviewDir string) {
		t.Helper()
		dir := filepath.Join(reviewDir, "reconciled")
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "summary.json"), []byte(body), 0o600))
	}
}

// TestReviewDirOwnsRun_RealRunIDAndSummaryAgree pins the pair every other ownership
// fixture hand-writes on both sides: the run_id PersistForReconcile mints and the
// summary.json reconcile.RunReconcile emits, for the same review dir. If either
// side changes its format (a different timestamp layout, a different separator, a
// renamed field) the hand-written fixtures keep agreeing with each other while the
// real pair stops matching, and every policy refusal silently falls into the
// pruned-tree half.
func TestReviewDirOwnsRun_RealRunIDAndSummaryAgree(t *testing.T) {
	root := t.TempDir()
	reviewDir := filepath.Join(t.TempDir(), "multi-agent")
	sources := filepath.Join(reviewDir, "sources", "host")
	require.NoError(t, os.MkdirAll(sources, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sources, "findings.txt"),
		[]byte("# atcr-findings/v1\nHIGH|a.go:1|the issue|fix it|security|10|ev|host\n"), 0o600))

	// A wall-clock-shaped instant: sub-second precision in a non-UTC zone, as the
	// CLI's time.Now() hands it over, so the test does not lean on a pre-normalized
	// value both sides would trivially agree on.
	at := time.Date(2026, 8, 4, 12, 0, 0, 123456789, time.FixedZone("CEST", 2*60*60))
	res, err := reconcile.RunReconcile(context.Background(), reviewDir, nil, reclib.Options{ReconciledAt: at})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(reviewDir, "reconciled", "summary.json"),
		"the real summary writer must have stamped the review")

	PersistForReconcile(reviewDir, res, PersistOpts{Root: root})

	recs, err := ReadAll(DefaultDir(root), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 1, "the real mint must have persisted the one finding")
	assert.True(t, reviewDirOwnsRun(reviewDir, recs[0].RunID),
		"run_id %q was minted for this review dir, so its own summary.json must own it", recs[0].RunID)
}

// countWalks returns seams whose walk counts the walks it starts and runs the real
// one. The counter is the caller's own, so tests using it may run in parallel.
func countWalks() (backfillSeams, *int) {
	walks := 0
	seams := defaultBackfillSeams()
	seams.walk = func(root string, fn fs.WalkDirFunc) error {
		walks++
		return filepath.WalkDir(root, fn)
	}
	return seams, &walks
}

// TD internal/localdebt/backfill.go:382 — replayCandidates walked the whole review
// root once per record, so a pass was O(records x review files). One pass walks it
// once however many records replay, and a pass with no replayable record walks it
// not at all.
func TestBackfillJustifications_WalksTheReviewRootOncePerPass(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	agents := []string{"dax", "kira", "odo"}
	lines := make([]string, 0, len(agents))
	for i, agent := range agents {
		rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", agent)
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
			[]byte("# review\n\n- **internal/thing.go:42** the narrative "+agent+" wrote.\n"), 0o600))
		id := fmt.Sprintf("bbbb000%d", i+1)
		lines = append(lines, `{"schema_version":3,"id":"`+id+`","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p-`+id+`","fix":"f","category":"correctness",`+
			`"est_minutes":10,"evidence":"e","reviewers":["`+agent+`"],"confidence":"HIGH",`+
			`"justification":"- **internal/thing.go:42** a stale excerpt.",`+
			`"source_report":{"path":"sources/pool/raw/agent/`+agent+`/review.md","line":3}}`)
	}
	writeShard(t, store, "2026-08", lines...)

	seams, walks := countWalks()
	res, err := backfillJustifications(store, reviewRoot, true, seams)
	require.NoError(t, err)
	assert.Equal(t, len(agents), res.Scanned)
	assert.Equal(t, len(agents), res.Rewritten, "each record still finds its own review.md in the shared walk")
	assert.Equal(t, 1, *walks, "one walk for the whole pass, not one per record")
}

func TestBackfillJustifications_NothingToReplayWalksNothing(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))
	require.NoError(t, os.MkdirAll(reviewRoot, 0o750))

	// No source_report: skipped before replayCandidates.
	writeShard(t, store, "2026-08",
		`{"schema_version":3,"id":"cccc0001","run_id":"r","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",`+
			`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
			`"justification":"- **internal/thing.go:42** an excerpt."}`)

	seams, walks := countWalks()
	res, err := backfillJustifications(store, reviewRoot, true, seams)
	require.NoError(t, err)
	assert.Zero(t, res.Scanned)
	assert.Zero(t, *walks, "a pass with nothing to replay must not walk the review root")
}

// TD internal/localdebt/backfill.go:272 — ownershipCache defaults a nil open and log,
// but the walk seam was copied over the tree's filepath.WalkDir unconditionally, so a
// zero-value backfillSeams panicked on the first replayable record. A nil walk now
// keeps the default, as the other two seams do.
func TestBackfillJustifications_ZeroValueSeamsDoNotPanic(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
		[]byte("# review\n\n- **internal/thing.go:42** the narrative dax wrote.\n"), 0o600))
	writeShard(t, store, "2026-08",
		`{"schema_version":3,"id":"dddd0001","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",`+
			`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
			`"justification":"- **internal/thing.go:42** a stale excerpt.",`+
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`)

	var res BackfillResult
	var err error
	require.NotPanics(t, func() { res, err = backfillJustifications(store, reviewRoot, true, backfillSeams{}) })
	require.NoError(t, err)
	assert.Equal(t, 1, res.Scanned)
	assert.Equal(t, 1, res.Rewritten, "a nil walk seam falls back to filepath.WalkDir and still finds review.md")
}

// TD internal/localdebt/backfill.go:673 — readSummary's io.ReadAll error arm was
// never reached. The summary here is a regular file whose reconciled_at WOULD own
// the run, so only the read error stands between it and ownership: an open that
// hands back the directory makes the read fail, and the summary must read as absent.
func TestReadSummary_ReadErrorIsTreatedAsAbsent(t *testing.T) {
	reviewDir := filepath.Join(t.TempDir(), "multi-agent")
	writeReconciledSummary(t, reviewDir, "2026-08-01T00:00:00Z")
	openDir := func(name string) (*os.File, error) { return os.Open(filepath.Dir(name)) }

	at, oversized := readSummary(reviewDir, openDir, slog.Default())
	assert.Empty(t, at, "a summary that cannot be read carries no reconciled_at")
	assert.False(t, oversized, "a read error is not an oversized skip")

	c := &ownershipCache{open: openDir}
	assert.False(t, c.owns(reviewDir, "2026-08-01T00:00:00Z-multi-agent"),
		"an unreadable summary owns nothing, though its contents would have owned the run")
	assert.Zero(t, c.oversized)
	assert.True(t, (&ownershipCache{}).owns(reviewDir, "2026-08-01T00:00:00Z-multi-agent"),
		"control: the same summary read through os.Open does own the run")
}

// TD internal/localdebt/backfill.go:453 — the walk and the summary open were package
// vars, so two tests counting through them corrupted each other's counts the moment
// either ran in parallel. Each pass now carries its own seams: these two run side by
// side (and under -race) and each sees only its own pass.
func TestBackfillJustifications_SeamsAreCarriedPerPass(t *testing.T) {
	t.Run("walks", func(t *testing.T) {
		t.Parallel()
		for range 5 {
			root := t.TempDir()
			store := filepath.Join(root, "debt")
			rd := filepath.Join(root, "reviews", "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
			require.NoError(t, os.MkdirAll(store, 0o750))
			require.NoError(t, os.MkdirAll(rd, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"),
				[]byte("# review\n\n- **internal/thing.go:42** the narrative dax wrote.\n"), 0o600))
			writeShard(t, store, "2026-08",
				`{"schema_version":3,"id":"dddd0001","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
					`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",`+
					`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
					`"justification":"- **internal/thing.go:42** a stale excerpt.",`+
					`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`)

			seams, walks := countWalks()
			res, err := backfillJustifications(store, filepath.Join(root, "reviews"), true, seams)
			require.NoError(t, err)
			assert.Equal(t, 1, res.Rewritten)
			assert.Equal(t, 1, *walks, "this pass walks once, whatever a parallel pass does")
		}
	})
	t.Run("opens", func(t *testing.T) {
		t.Parallel()
		for range 5 {
			root := t.TempDir()
			store := filepath.Join(root, "debt")
			require.NoError(t, os.MkdirAll(store, 0o750))
			target := filepath.Join(root, "elsewhere", "review.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
			require.NoError(t, os.WriteFile(target,
				[]byte("# review\n\n- **internal/thing.go:42** the narrative a follow would have stamped.\n"), 0o600))
			ownDir := filepath.Join(root, "reviews", "sprint-a", "multi-agent")
			writeReconciledSummary(t, ownDir, "2026-08-01T00:00:00Z")
			rd := filepath.Join(ownDir, "sources", "pool", "raw", "agent", "dax")
			require.NoError(t, os.MkdirAll(rd, 0o750))
			require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))
			writeShard(t, store, "2026-08",
				`{"schema_version":3,"id":"eeee0001","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
					`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",`+
					`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
					`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",`+
					`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`)

			opens := 0
			seams := defaultBackfillSeams()
			seams.open = func(name string) (*os.File, error) {
				opens++
				return os.Open(name)
			}
			res, err := backfillJustifications(store, filepath.Join(root, "reviews"), false, seams)
			require.NoError(t, err)
			assert.Equal(t, 1, res.PolicyUnrepairable)
			assert.Equal(t, 1, opens, "this pass opens its summary once, whatever a parallel pass does")
		}
	})
}

// TD internal/localdebt/backfill.go:496 — reviewTree.walk keeps every path whose
// base is some record's source_report base, a superset of what any one record may
// replay from; replayCandidates' pathHasSuffix is what narrows it to the record's
// own rel. Nothing pinned that split: relaxing the per-record filter to a base-name
// match left the suite green.
//
// Three files sit under one review dir: the record's own review.md (kept and
// matched), a same-base review.md at another agent's rel (kept by walk, not matched)
// and a summary.md (dropped by walk). The namesake carries the record's anchor at the
// same source line with a different excerpt, so a relaxed match would add a second
// distinct text and turn this Rewritten record into an Ambiguous one.
func TestReviewTree_WalkKeepsTheBaseAndReplayMatchesOnlyTheRecordsRel(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	agentDir := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent")
	own := filepath.Join(agentDir, "dax", "review.md")
	namesake := filepath.Join(agentDir, "kira", "review.md")
	otherBase := filepath.Join(agentDir, "dax", "summary.md")
	for p, body := range map[string]string{
		own:       "# review\n\n- **internal/thing.go:42** the narrative dax wrote.\n",
		namesake:  "# review\n\n- **internal/thing.go:42** the narrative kira wrote.\n",
		otherBase: "# review\n\n- **internal/thing.go:42** a summary nobody stamped from.\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}

	rec := Record{
		RunID:        "2026-08-01T00:00:00Z-multi-agent",
		File:         "internal/thing.go",
		Line:         42,
		SourceReport: &SourceReport{Path: "sources/pool/raw/agent/dax/review.md", Line: 3},
	}

	tree := newReviewTree(reviewRoot, []Record{rec})
	paths, err := tree.walk()
	require.NoError(t, err)
	assert.Equal(t, []string{own, namesake}, paths,
		"walk keeps every path with the record's base, its own rel and the namesake alike, "+
			"and drops the file with another base")

	res, err := replayCandidates(tree, rec, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"- **internal/thing.go:42** the narrative dax wrote."}, res.texts,
		"only the file at the record's own rel is matched; the namesake's excerpt is not a candidate")
	assert.False(t, res.policyRefused)

	writeShard(t, store, "2026-08",
		`{"schema_version":3,"id":"ffff0001","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",`+
			`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
			`"justification":"- **internal/thing.go:42** a stale excerpt.",`+
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`)

	pass, err := BackfillJustifications(store, reviewRoot, false)
	require.NoError(t, err)
	assert.Equal(t, 1, pass.Scanned)
	assert.Equal(t, 1, pass.Rewritten, "the record's own review.md is its one candidate")
	assert.Zero(t, pass.Ambiguous,
		"the namesake at another rel must not become a second, disagreeing candidate")
}

// TD internal/localdebt/backfill.go:596 — an oversized summary.json was skipped at
// slog.Debug only, which the default Info logger drops, so the record whose own review
// dir it sits in quietly lost its policy attribution. The pass now counts the skip.
//
// Two records refuse a symlinked review.md in ONE review dir whose summary.json is
// over the cap, so the pass asks that dir who owns the run twice; the cache reads
// the file once, and the count is per summary file per pass, not per record.
func TestBackfillJustifications_CountsAnOversizedSummaryOncePerFile(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	target := filepath.Join(root, "elsewhere", "review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target,
		[]byte("# review\n\n- **internal/thing.go:42** the narrative a follow would have stamped.\n"), 0o600))

	// The summary carries the run's own reconciled_at, so only the size stands
	// between this dir and owning the records.
	ownDir := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
	rawSummary(`{"reconciled_at":"2026-08-01T00:00:00Z"}`+strings.Repeat(" ", maxSummaryBytes))(t, ownDir)

	agents := []string{"dax", "kira"}
	lines := make([]string, 0, len(agents))
	for i, agent := range agents {
		rd := filepath.Join(ownDir, "sources", "pool", "raw", "agent", agent)
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))
		id := fmt.Sprintf("cccc000%d", i+1)
		lines = append(lines, `{"schema_version":3,"id":"`+id+`","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p-`+id+`","fix":"f","category":"correctness",`+
			`"est_minutes":10,"evidence":"e","reviewers":["`+agent+`"],"confidence":"HIGH",`+
			`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",`+
			`"source_report":{"path":"sources/pool/raw/agent/`+agent+`/review.md","line":3}}`)
	}
	writeShard(t, store, "2026-08", lines...)

	res, err := BackfillJustifications(store, reviewRoot, true)
	require.NoError(t, err)

	assert.Equal(t, 2, res.Unresolved, "premise: neither symlink yields an excerpt")
	assert.Zero(t, res.PolicyUnrepairable, "premise: an oversized summary.json owns nothing")
	assert.Equal(t, 1, res.OversizedSummaries,
		"one summary.json over the cap was ignored, however many records asked its dir")
}

// A summary.json within the cap is not counted, and neither is one the pass never
// reads: the count moves only when a refusal makes the pass ask who owns the record.
func TestBackfillJustifications_DoesNotCountASummaryWithinTheCap(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	require.NoError(t, os.MkdirAll(store, 0o750))

	target := filepath.Join(root, "elsewhere", "review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target, []byte("# review\n"), 0o600))

	ownDir := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
	writeReconciledSummary(t, ownDir, "2026-08-01T00:00:00Z")
	rd := filepath.Join(ownDir, "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))
	writeShard(t, store, "2026-08", `{"schema_version":3,"id":"cccc0003","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",`+
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",`+
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",`+
		`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",`+
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`)

	res, err := BackfillJustifications(store, reviewRoot, true)
	require.NoError(t, err)

	assert.Equal(t, 1, res.PolicyUnrepairable, "premise: the summary was read and owns the record")
	assert.Zero(t, res.OversizedSummaries)
}

// readReconciledAt has two oversized arms: the Lstat size check and the post-read
// length check for a file that grew past the cap after the Lstat. Both are counted.
func TestOwnershipCache_CountsAFileThatGrewPastTheCapAfterTheLstat(t *testing.T) {
	reviewDir := t.TempDir()
	writeReconciledSummary(t, reviewDir, "2026-08-01T00:00:00Z")
	path := filepath.Join(reviewDir, "reconciled", "summary.json")
	c := &ownershipCache{open: func(name string) (*os.File, error) {
		// Grow the file between the Lstat and the read.
		if err := os.WriteFile(name, []byte(strings.Repeat(" ", maxSummaryBytes+1)), 0o600); err != nil {
			return nil, err
		}
		return os.Open(name)
	}}

	assert.False(t, c.owns(reviewDir, "2026-08-01T00:00:00Z-"+filepath.Base(reviewDir)))
	assert.Equal(t, 1, c.oversized, "the post-read length arm is an oversized skip too: %s", path)
}

// TD internal/localdebt/backfill.go:372 — a policy refusal the pass cannot tie to the
// record (its review dir provably owns another run, or its summary.json is missing or
// oversized, so it owns nothing) fell into the pruned-tree half of Unresolved, so the
// operator read "review tree pruned" for a record whose tree held a refused file.
// PolicyUnattributed names it without claiming the file is the record's own, so it
// still does not tell the operator to leave a file alone. It is exclusive with
// PolicyUnrepairable, which wins when both hold, and the three counts sum to
// Unresolved.
func TestBackfillJustifications_CountsAnUnattributedPolicyRefusalApart(t *testing.T) {
	const runID = "2026-08-01T00:00:00Z-multi-agent"
	rec := `{"schema_version":3,"id":"eeee0001","run_id":"` + runID + `","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`

	// plantSymlink puts a symlinked review.md — a file-level policy refusal — at the
	// record's relative path inside reviewDir.
	plantSymlink := func(t *testing.T, root, reviewDir string) {
		t.Helper()
		target := filepath.Join(root, "elsewhere", "review.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
		require.NoError(t, os.WriteFile(target,
			[]byte("# review\n\n- **internal/thing.go:42** the narrative a follow would have stamped.\n"), 0o600))
		rd := filepath.Join(reviewDir, "sources", "pool", "raw", "agent", "dax")
		require.NoError(t, os.MkdirAll(rd, 0o750))
		require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))
	}

	cases := []struct {
		name                       string
		plant                      func(t *testing.T, root, reviewRoot string)
		pruned, owned, unattribute int
	}{
		{"refusal in an unrelated review", func(t *testing.T, root, reviewRoot string) {
			other := filepath.Join(reviewRoot, "sprint-b", "multi-agent")
			writeReconciledSummary(t, other, "2026-08-09T00:00:00Z")
			plantSymlink(t, root, other)
		}, 0, 0, 1},
		{"refusal in a dir whose summary.json is gone", func(t *testing.T, root, reviewRoot string) {
			plantSymlink(t, root, filepath.Join(reviewRoot, "sprint-a", "multi-agent"))
		}, 0, 0, 1},
		{"refusal in a dir whose summary.json is oversized", func(t *testing.T, root, reviewRoot string) {
			own := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
			rawSummary(`{"reconciled_at":"2026-08-01T00:00:00Z"}`+strings.Repeat(" ", maxSummaryBytes))(t, own)
			plantSymlink(t, root, own)
		}, 0, 0, 1},
		{"an owned refusal wins over an unattributed one", func(t *testing.T, root, reviewRoot string) {
			own := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
			writeReconciledSummary(t, own, "2026-08-01T00:00:00Z")
			plantSymlink(t, root, own)
			other := filepath.Join(reviewRoot, "sprint-b", "multi-agent")
			writeReconciledSummary(t, other, "2026-08-09T00:00:00Z")
			plantSymlink(t, root, other)
		}, 0, 1, 0},
		{"no candidate at all is the pruned tree", func(*testing.T, string, string) {}, 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := filepath.Join(root, "debt")
			reviewRoot := filepath.Join(root, "reviews")
			require.NoError(t, os.MkdirAll(store, 0o750))
			require.NoError(t, os.MkdirAll(reviewRoot, 0o750))
			tc.plant(t, root, reviewRoot)
			writeShard(t, store, "2026-08", rec)

			res, err := BackfillJustifications(store, reviewRoot, true)
			require.NoError(t, err)

			assert.Equal(t, 1, res.Unresolved, "premise: nothing yielded an excerpt")
			assert.Equal(t, tc.owned, res.PolicyUnrepairable)
			assert.Equal(t, tc.unattribute, res.PolicyUnattributed)
			assert.Equal(t, tc.pruned, res.Unresolved-res.PolicyUnrepairable-res.PolicyUnattributed,
				"the pruned-tree figure the CLI prints")
			assert.Equal(t, res.Unresolved, tc.pruned+res.PolicyUnrepairable+res.PolicyUnattributed,
				"the three counts sum to Unresolved")
		})
	}
}

// TD internal/localdebt/backfill.go:484 (epic 88 T4) pins the decision to KEEP the
// PolicyUnattributed definition. The record's own review tree is absent, and an
// unrelated run's review holds a policy-refused namesake at the record's relative
// path. The record counts under PolicyUnattributed, so the "review tree pruned"
// figure reads 0 for a record whose tree really was pruned. The pass finds the own
// review dir only through its reconciled/summary.json, so it cannot tell this case
// from an own tree whose summary.json is gone and whose refused file IS the record's
// own; the unattributed clause is the honest reading of both. The control subtest
// shows the namesake alone moves the record: without it the same store and the same
// absent tree read as pruned.
func TestBackfillJustifications_AbsentOwnTreeWithUnrelatedRefusedNamesakeIsUnattributed(t *testing.T) {
	const runID = "2026-08-01T00:00:00Z-multi-agent"
	rec := `{"schema_version":3,"id":"eeee0002","run_id":"` + runID + `","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** a stale excerpt nothing can replay.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":3}}`

	for _, tc := range []struct {
		name                string
		namesake            bool
		pruned, unattribute int
	}{
		{"an unrelated refused namesake reads as unattributed, not pruned", true, 0, 1},
		{"control: without the namesake the absent tree reads as pruned", false, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := filepath.Join(root, "debt")
			reviewRoot := filepath.Join(root, "reviews")
			require.NoError(t, os.MkdirAll(store, 0o750))
			require.NoError(t, os.MkdirAll(reviewRoot, 0o750))

			if tc.namesake {
				// Another run's review, which provably owns that run and not this
				// record, holds a symlinked review.md (a file-level policy refusal)
				// at the record's relative source_report path.
				other := filepath.Join(reviewRoot, "sprint-b", "multi-agent")
				writeReconciledSummary(t, other, "2026-08-09T00:00:00Z")
				target := filepath.Join(root, "elsewhere", "review.md")
				require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
				require.NoError(t, os.WriteFile(target,
					[]byte("# review\n\n- **internal/thing.go:42** an unrelated run's narrative.\n"), 0o600))
				rd := filepath.Join(other, "sources", "pool", "raw", "agent", "dax")
				require.NoError(t, os.MkdirAll(rd, 0o750))
				require.NoError(t, os.Symlink(target, filepath.Join(rd, "review.md")))
			}
			writeShard(t, store, "2026-08", rec)

			own := filepath.Join(reviewRoot, "sprint-a", "multi-agent")
			_, statErr := os.Stat(own)
			require.ErrorIs(t, statErr, fs.ErrNotExist, "premise: the record's own review tree is absent")

			res, err := BackfillJustifications(store, reviewRoot, true)
			require.NoError(t, err)

			assert.Equal(t, 1, res.Unresolved, "premise: nothing yielded an excerpt")
			assert.Equal(t, 0, res.PolicyUnrepairable, "no refusal sits in a dir that owns the record")
			assert.Equal(t, tc.unattribute, res.PolicyUnattributed)
			assert.Equal(t, tc.pruned, res.Unresolved-res.PolicyUnrepairable-res.PolicyUnattributed,
				"the pruned-tree figure the CLI prints")
		})
	}
}
