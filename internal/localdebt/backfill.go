package localdebt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/samestrin/atcr/internal/reconcile"
)

// BackfillResult reports one backfill pass. Four non-Scanned counters — Rewritten,
// Unchanged, Unresolved, Ambiguous — partition the scanned set.
// SkippedRationaleBearing sits
// OUTSIDE that partition: a suppressed record is skipped before Scanned++ runs, so the
// printed "N scanned ... M skipped" does not mean the skipped are among the scanned
// (10 ids with 4 suppressed print "6 scanned ... 4 skipped", and the four partition
// counters sum to 6, not 10). The three partition counters that are not Rewritten
// each mean something the operator may want to act on separately — a pruned review
// tree (Unresolved), a repo holding several reviews that anchor the same finding
// (Ambiguous), or a replayed excerpt that was byte-identical (Unchanged).
type BackfillResult struct {
	Scanned   int // effective records carrying both a source_report path and a justification
	Rewritten int // replayed excerpt differed from the stored one and was written
	Unchanged int // replayed excerpt was byte-identical
	// Unresolved counts records no review.md yielded an excerpt for. It does NOT
	// mean the file is gone. ReExtractJustification returns (ok=false, err=nil) both
	// for "this file is not the one" and for the producer's own POLICY exclusions — a
	// review.md over the size cap, or one that is not a regular file — so a review.md
	// present and readable at the record's own source_report path lands here too. The
	// label an operator reads must therefore describe the OBSERVATION (nothing yielded
	// an excerpt), not infer a cause ("no surviving review.md"), which sent them to
	// restore a file that was already there.
	//
	// PolicyUnrepairable splits the policy half out of this count; the two are
	// subtracted, never added, so this field keeps its historical meaning as the full
	// observation count and no existing consumer loses a row.
	Unresolved int
	// PolicyUnrepairable carves the "the file is there and still cannot help" half out
	// of Unresolved. ReExtractJustification returns ok=false both because no candidate
	// survives at the record's own source_report path and because a candidate WAS there
	// yet yielded no excerpt — the producer's policy exclusions (over the size cap, a
	// symlink, a wholly-salvaged reply, a desynced bin list, a draft anchor line), a
	// namesake whose anchor does not match, or a section that is pure quoted example.
	// Each refusal is right (the replay set may not exceed the stamp set), but the two
	// classes call for opposite operator actions: an absent review.md may be restorable,
	// while restoring a file cannot fix any of the second class — not even with the tree
	// fully intact. Summed into one integer the operator cannot tell which remedy
	// applies, and cli/debt_resolve.go's SCOPE paragraph inherited the same conflation.
	//
	// It counts a record for which the producer's policy PROVABLY declines a matching
	// candidate: a non-regular file (symlink, FIFO, device) at the record's relative
	// path, or one ReviewPolicyDeclinesFile refuses outright — over the size cap, a
	// wholly salvaged reply, a desynced bin list. Those three are FILE-level, so the
	// VERDICT holds wherever the candidate sits — but attributing it to THIS record
	// does not: a refused file in an unrelated review says nothing about whether the
	// record's own review.md would help. So the candidate counts only when its review
	// dir owns the record (see the ownership rule below).
	//
	// It deliberately does NOT count "a candidate was present and nothing matched".
	// That reading was the defect: pathHasSuffix is review-dir-UNSCOPED and
	// SourceReport.Path is review-dir-RELATIVE, so any review in the tree supplies a
	// namesake and a genuinely pruned tree reported as unrepairable — the operator was
	// told not to restore the one file that would have fixed it
	// (TD internal/localdebt/backfill.go:389).
	//
	// Ownership rule: a candidate's review dir is its path with the record's relative
	// source_report path trimmed off, and that dir owns the record when
	// `<reviewDir>/reconciled/summary.json` carries a reconciled_at for which
	// `reconciled_at + "-" + base(reviewDir)` equals the record's RunID — the exact
	// form the run_id was minted in. A dir with a missing or unparseable summary.json
	// owns nothing. The cost is an UNDER-count: a policy refusal in an own tree whose
	// summary.json is gone, and every record-level unrepairable — chiefly an anchor
	// line the producer refused as a draft — land in the absent-tree half instead.
	// That direction is the safe one: it sends the operator to look for a file, which
	// wastes a minute, rather than telling them not to, which loses the repair.
	// `Unresolved - PolicyUnrepairable` is therefore "absent tree, or a refusal this
	// pass cannot attribute to the record".
	PolicyUnrepairable int
	Ambiguous          int // several surviving candidates disagreed, so none was written

	// SkippedRationaleBearing counts effective records the fold filter suppressed
	// because the record may hold an operator-typed rationale this pass must not
	// overwrite (bearsRationale: resolved, wontfix, unreproducible,
	// attempts-exhausted). Without it the suppression is invisible: a store whose
	// ids are all suppressed reports "0 scanned, 0 rewritten", which reads
	// identically to a store that needs no repair.
	//
	// It was SkippedSettled until Story 36.0, and the rename is not cosmetic:
	// `attempts-exhausted` is suppressed here and is NOT settled, so the old name
	// described the wrong set on the one command that rewrites the store in place.
	SkippedRationaleBearing int

	// RewrittenLines counts the SHARD LINES the pass wrote, which Rewritten does
	// not: one id can carry several lines (a re-detection after a resolution
	// appends a fresh record under the same id), so a record count understates the
	// blast radius of a write to an append-only store.
	RewrittenLines int
	// Changes describes each line the pass wrote, or WOULD write on a dry run.
	// --dry-run is documented as the safety step to run first, so it has to be able
	// to show what it would touch; a bare counter cannot.
	Changes []JustificationChange

	// ShardNames is every shard file name the rewrite pass's own walk observed,
	// taken INSIDE the withLock region. The names are a SET, not a sequence:
	// the only consumer (locatorNames' collision disambiguation) keys on
	// membership, so the walk's arrival order is not part of the contract.
	//
	// It exists so a caller rendering Changes describes the same directory snapshot
	// the rewrite was computed against. `atcr debt backfill-justifications --dry-run`
	// disambiguates colliding shard locators, which is a property of the SET of names
	// on disk and so cannot be derived from Changes alone — an unchanged file can
	// collide with a changed one. Resolving that set with a SECOND os.ReadDir after
	// this function returns reads the directory outside the lock, so a concurrent
	// writer removing a colliding shard in that window suppresses the disambiguating
	// suffix and the operator approves a bare locator for a name that was ambiguous
	// when the rewrite was computed.
	//
	// It is populated on every pass that completed its locked listing — including a
	// pass with nothing to rewrite — and is nil in three cases: the store directory
	// does not exist (the "no backlog yet" state ReadAll already tolerates), the
	// directory exists but holds no shard file at all (shardFileNames appends to a
	// nil slice, so an empty result IS nil), or the pass failed before it could
	// list. Do not read nil as "the pass did not run". "Shards present but none
	// needing repair" and "no shards" are still distinguishable — the first yields
	// a non-empty set — and the field's meaning does not depend on Changes being
	// non-empty. No consumer separates nil from empty today: both readers
	// short-circuit on an empty change set.
	ShardNames []string
}

// JustificationChange is one shard line the backfill rewrote or would rewrite.
type JustificationChange struct {
	ID     string // the record id the line carries
	Shard  string // shard file name, e.g. "2026-08.jsonl"
	Line   int    // 1-based line number within the shard
	Before string // the stored justification
	After  string // the replayed excerpt that replaces it
}

// BackfillJustifications repairs the justifications ALREADY in the store by replaying
// each one from its originating review.md.
//
// It exists because the store's own identity rule makes the repair unreachable any
// other way: Record.StampID hashes file\x00line\x00problem and deliberately excludes
// Justification, so a re-detected finding hashes to the same id; PersistForReconcile
// then seeds seen[id] for every open and suppressing id and skips the append. A
// change to extractSection — the dangling-fence marker emission being the case this
// was written for — therefore governs only records first persisted after it, while
// every excerpt already on disk keeps its original text forever. Those stale excerpts
// are what cli/debt_resolve.go's wontfix gate reads, so the inconsistency is not
// cosmetic: a marker-free excerpt is accepted as a permanent dismissal's whole audit
// trail.
//
// It is a ONE-OFF, not a hook. Refreshing on every reconcile was considered and
// rejected: an unconditional enrichment append would re-add a record whenever a
// reviewer reworded its narrative, and store growth would stop being bounded by
// finding count — which is the whole reason PersistForReconcile dedupes.
//
// reviewRoot is searched for each record's SourceReport.Path. That search is
// necessary, not defensive: SourceReport.Path is review-dir-RELATIVE
// (`sources/pool/raw/agent/dax/review.md`) and every review directory holds the same
// relative paths, so the path alone selects dozens of namesakes. RunID alone cannot
// break the tie either — its suffix is filepath.Base(reviewDir), which is
// `multi-agent` for nearly every review. Paired with the review dir's own
// reconciled/summary.json it CAN name the owning dir (reviewDirOwnsRun), but the
// replay does not select on that: a review dir whose summary.json is gone would then
// decline a repair the anchor re-score makes safely, so ownership only gates the
// PolicyUnrepairable attribution. Re-scoring the anchor line against the finding's
// own file:line distinguishes candidates, and a record whose candidates disagree is
// left ALONE.
// Guessing there would rewrite a permanent dismissal's audit trail out of an
// unrelated review, which is strictly worse than the stale text it replaces.
//
// A rewritten line is re-marshaled from a generic map, so a field this binary does
// not declare survives the edit; its key ORDER is not preserved (Go marshals map keys
// sorted), which is cosmetic in JSONL. Every line the pass does not change is copied
// through byte-for-byte and is not re-marshaled at all.
func BackfillJustifications(dir, reviewRoot string, dryRun bool) (BackfillResult, error) {
	var res BackfillResult
	err := withLock(dir, "backfill-justifications", func() error {
		recs, err := ReadAll(dir, ReadOpts{})
		if err != nil {
			return err
		}
		// Fold first: the gate reads the EFFECTIVE record, so that is the excerpt
		// whose staleness matters. An id's other rows are updated alongside it below,
		// because they carry the same stamp.
		// id -> the replacement AND the stale text it replaces. The stale text is
		// what makes the rewrite LINE-scoped instead of id-scoped: see
		// rewriteJustifications.
		want := map[string]replacement{}
		// ID gate, built before the fold: an id is skipped if ANY of its records
		// satisfies bearsRationale, not just the effective one. The effective
		// record alone is not the right scope: a regressed id folds to its LATEST
		// open record (open@t1 -> attempts-exhausted@t2 -> open@t3), so a gate on
		// the effective record does not fire, the id is scanned, and the superseded
		// rationale-bearing trail line is protected only by the incidental text
		// inequality in rewriteJustifications. Where the regression record carries
		// the operator's --reason verbatim (re-detection copies the effective
		// record) and the replayed excerpt differs, rep.from matches the trail
		// line's text and the operator's typed reason is replayed over — the exact
		// irreversible loss this gate exists to prevent. The reason coincides with
		// the excerpt where an operator typed a dismissal citing it, which is
		// reachable, not hypothetical.
		//
		// Skipping the WHOLE id is the safe over-broad direction: an id whose trail
		// carries a human-typed rationale keeps its stale excerpts unread — one
		// unrepaired excerpt is reported as live stale text by the next pass, while
		// a replayed-over --reason cannot be recovered from anything in the tree.
		// FoldRecords emits one record per id, so the gate below runs once per id.
		rationaleIDs := make(map[string]bool)
		for _, rec := range recs {
			if bearsRationale(rec.Status) {
				rationaleIDs[rec.ID] = true
			}
		}
		for _, r := range FoldRecords(recs) {
			if rationaleIDs[r.ID] {
				// RATIONALE-BEARING, not merely closed — the distinction record.go
				// draws between the predicates decides both directions here.
				// `resolved` and `wontfix` are done: a resolved id is settled
				// history whose excerpt gates nothing, and a wontfix id's
				// justification MAY be the operator's --reason rather than a review
				// excerpt — which exists nowhere else in the tree, so replaying over
				// it in an append-only store is irreversible loss. `deferred`
				// carries a terminal marker but means "not now": it is live,
				// closeable debt whose stale excerpt is exactly what this pass exists
				// to repair, so it must NOT be skipped.
				//
				// The gate reads bearsRationale rather than IsSettledStatus because
				// Story 36.0 split the two apart. `attempts-exhausted` is UNSETTLED
				// (it stays closeable, like deferred) but its `--reason` is
				// MANDATORY, so unlike deferred it always holds operator-typed text
				// that exists nowhere else. Gating on settledness would replay a
				// review excerpt straight over it. That is the same irreversible loss
				// the wontfix skip exists to prevent, arriving by the one route the
				// old proxy could not see — and here it is DOES, not MAY.
				//
				// MAY, not DOES: --reason is OPTIONAL for wontfix. cli/debt_resolve.go
				// permits an empty --reason whenever isRecordedRationale holds of the
				// justification already stored, and an empty reason preserves that
				// text rather than replacing it — which a legacy marker-free excerpt
				// satisfies. So a wontfix record routinely DOES carry the stale review
				// excerpt this pass repairs, and skipping it is over-broad.
				//
				// The gate is ID-scoped, not per-record inside the fold: ONE
				// rationale-bearing record anywhere in the id's trail makes the whole
				// id unreachable. It is the safe direction: the alternative failure is
				// overwriting a human-typed rationale in an append-only store, and the
				// line-scoped `cur != rep.from` predicate in rewriteJustifications
				// cannot separate the two on its own — rep.from IS the effective
				// record's justification, so a trail line carrying the same text (a
				// re-detection that copied the --reason verbatim) matches it. What the
				// skip owes instead is VISIBILITY: counted below, so "0 scanned" is
				// distinguishable from a scan that was suppressed.
				res.SkippedRationaleBearing++
				continue
			}
			sr := r.SourceReport
			if sr == nil || sr.Path == "" || r.Justification == "" {
				continue
			}
			res.Scanned++
			cands, err := replayCandidates(reviewRoot, r)
			if err != nil {
				return err
			}
			texts := cands.texts
			switch {
			case len(texts) == 0:
				// See BackfillResult.Unresolved: this covers a pruned review tree AND
				// a review.md the replay declined by policy. The COUNT is split so the
				// operator can tell which remedy applies; only the policy half is
				// unrepairable with the tree intact.
				res.Unresolved++
				if cands.policyRefused {
					res.PolicyUnrepairable++
				}
			case len(texts) > 1:
				res.Ambiguous++
			case texts[0] == r.Justification:
				res.Unchanged++
			default:
				res.Rewritten++
				want[r.ID] = replacement{from: r.Justification, to: texts[0]}
			}
		}
		if len(want) == 0 {
			// Nothing needs a rewrite, but ShardNames still owes the caller the locked
			// walk's observation: leaving it nil here would make "the store holds no
			// shards" indistinguishable from "the store holds shards, none needing
			// repair", and would leave the field's meaning silently coupled to Changes
			// being non-empty. An unchanged colliding shard is exactly the collision
			// the change set cannot see, so the snapshot must not depend on there
			// being changes at all.
			entries, derr := os.ReadDir(dir)
			if derr != nil && !os.IsNotExist(derr) {
				// A missing store directory is the legal "no backlog yet" state ReadAll
				// already tolerates above; any other listing failure is as fatal here
				// as it is in rewriteJustifications.
				//
				// This arm is a BACKSTOP and is currently unreachable through
				// BackfillJustifications: ReadAll lists the same directory first and
				// returns any non-ENOENT failure itself, so control cannot arrive here
				// with a listing problem. Its 0-hit coverage — and its survival under
				// mutation — is therefore structural, not a missing test. The ordering
				// that makes it dead is pinned by
				// TestBackfillJustifications_NoRewriteSnapshotListingArms, so a change
				// that makes ReadAll tolerant fails there and says this guard has gone
				// live. Keep it: the cost is one branch, and the alternative is a
				// silent nil snapshot on a store that could not be read.
				return fmt.Errorf("reading localdebt dir for backfill: %w", quotedPathErr(derr))
			}
			res.ShardNames = shardFileNames(entries)
			return nil
		}
		changes, shards, rerr := rewriteJustifications(dir, want, dryRun)
		// Assigned BEFORE the error check: on a non-dry run each shard is renamed into
		// place as the walk reaches it, so a failure on a later shard leaves the earlier
		// ones already rewritten. changes carries exactly the ones that were published.
		res.Changes = changes
		res.ShardNames = shards
		res.RewrittenLines = len(changes)
		if rerr != nil {
			return rerr
		}
		return nil
	})
	if err != nil {
		// Not BackfillResult{}: the store may already have been mutated, and a zero
		// result reads as "nothing happened" — the one reading an operator must not
		// take away from a half-completed rewrite of an append-only store.
		//
		// Only the fields describing WORK DONE survive. The scan counters (Scanned,
		// Rewritten, Unresolved, Ambiguous, Unchanged, SkippedRationaleBearing) describe a pass
		// that completed, and this one did not, so carrying them would report a
		// partition of a scan whose writes were never finished.
		return BackfillResult{
			Changes:        res.Changes,
			RewrittenLines: len(res.Changes),
			ShardNames:     res.ShardNames,
		}, err
	}
	return res, nil
}

// replayResult reports what one record's replay search found. `texts` is the
// DISTINCT set of excerpts the surviving candidates yielded. `policyRefused` is set
// when the record's OWN source_report path holds a present, regular review.md that
// the producer's policy declined — the one signal that separates an unrepairable
// refusal from a pruned tree, and the one an operator needs to pick a remedy
// (TD cli/debt_resolve.go:81).
type replayResult struct {
	texts         []string
	policyRefused bool
}

// replayCandidates returns the DISTINCT excerpts every surviving review.md under
// reviewRoot yields for rec's anchor. Distinct rather than one-per-file: two copies
// of the same review (a re-run, a backup) agree, and treating that as ambiguity would
// decline a repair that has only one answer.
func replayCandidates(reviewRoot string, rec Record) (replayResult, error) {
	rel := filepath.FromSlash(rec.SourceReport.Path)
	var out []string
	seen := map[string]bool{}
	policyRefused := false
	// owns reports whether candidate p sits in the record's own review dir. Asked
	// only once a candidate is already refused, so the summary.json read is paid on
	// the rare path, not for every namesake.
	owns := func(p string) bool {
		return reviewDirOwnsRun(reviewDirOf(p, rel), rec.RunID)
	}
	err := filepath.WalkDir(reviewRoot, func(p string, d fs.DirEntry, walkErr error) error {
		// An unreadable file or subtree is SKIPPED, not fatal: reviewRoot is an open
		// tree — the same resilience stance collectReviewNarratives takes over
		// sources/ — and one bad directory must not abort a repair pass over the
		// whole store. Propagating walkErr here would do exactly that.
		if walkErr != nil {
			return nil
		}
		// IsRegular, not merely !IsDir: internal/reconcile's collectReviewNarratives
		// deliberately excludes symlinks, FIFOs and devices named review.md, and
		// ReExtractJustification's os.ReadFile would FOLLOW a link. A file the
		// producer would never have stamped from must not become an authoritative
		// candidate for the replay — the replay set may not exceed the stamp set.
		if !pathHasSuffix(p, rel) {
			return nil
		}
		// The stamp set is keyed on a REGULAR file: the producer refuses a symlink,
		// FIFO or device named review.md, so all three are policy refusals rather
		// than an absent tree and must not be filed under the "restore the file"
		// remedy either. Recorded before ReExtractJustification so the presence is
		// the walk's observation, independent of whatever the replay then decides.
		if !d.Type().IsRegular() {
			if owns(p) {
				policyRefused = true
			}
			return nil
		}
		text, _, ok, rerr := reconcile.ReExtractJustification(p, rec.File, rec.Line, rec.SourceReport.Line)
		if rerr != nil || !ok {
			// rerr here is "this candidate is unreadable", not "the backfill
			// failed" — another candidate may still resolve the record.
			//
			// Ask the policy why, rather than inferring it from presence. A refusal
			// the producer's FILE-LEVEL policy explains is unrepairable wherever the
			// file sits; a candidate that merely fails to carry this record's anchor
			// is a namesake and says nothing about the record's own tree. Only the
			// former may set policyRefused — see ReviewPolicyDeclinesFile — and only
			// from a candidate whose review dir owns the record. A probe error is not
			// evidence either way, so it leaves the flag alone.
			if declined, perr := reconcile.ReviewPolicyDeclinesFile(p); perr == nil && declined && owns(p) {
				policyRefused = true
			}
			return nil
		}
		if !seen[text] {
			seen[text] = true
			out = append(out, text)
		}
		return nil
	})
	if err != nil {
		return replayResult{}, fmt.Errorf("searching %s for review narratives: %w", reviewRoot, err)
	}
	// `policyRefused` is "the producer's policy would refuse this candidate at all",
	// never "a file was present and nothing matched". The weaker reading was the
	// defect: pathHasSuffix is review-dir-UNSCOPED and SourceReport.Path is
	// review-dir-RELATIVE, so ANY review in the tree supplies a namesake and a pruned
	// tree read as unrepairable — telling the operator not to restore the one file
	// that would have fixed it (TD internal/localdebt/backfill.go:389).
	//
	// What remains is a non-regular file at a matching path, and the file-level arms
	// ReviewPolicyDeclinesFile names — and each counts only from a candidate whose
	// review dir OWNS the record (reviewDirOwnsRun). The verdict on such a file is
	// path-independent; attributing it to this record is not, and an unscoped walk
	// otherwise lets a symlink or over-cap namesake in an unrelated review speak for a
	// record whose own tree is pruned (TD internal/localdebt/backfill.go:382).
	return replayResult{texts: out, policyRefused: policyRefused}, nil
}

// reviewDirOf trims the review-dir-relative rel off candidate p, leaving the review
// dir the candidate sits in. p is known to end with rel on a segment boundary
// (pathHasSuffix), so the trim cannot cut a segment in half.
func reviewDirOf(p, rel string) string {
	return filepath.Clean(strings.TrimSuffix(p, rel))
}

// reviewDirOwnsRun reports whether reviewDir is the review that minted runID.
//
// A record's RunID is `<reconciled_at>-<base(reviewDir)>` (internal/localdebt/
// reconcile.go), and the same reconciled_at is stored in
// `<reviewDir>/reconciled/summary.json`, so ownership is checkable from data already
// on disk. The base alone cannot decide it — it is `multi-agent` for nearly every
// review — but the base together with the reconciliation timestamp can: two reviews
// collide only if they share a dir base AND were reconciled in the same second.
//
// A missing, unreadable or unparseable summary.json, or one with no reconciled_at,
// owns NOTHING. That is the safe under-count: the record lands in the pruned-tree
// half, which sends the operator to look for a file rather than telling them not to.
func reviewDirOwnsRun(reviewDir, runID string) bool {
	data, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", "summary.json"))
	if err != nil {
		return false
	}
	var s struct {
		ReconciledAt string `json:"reconciled_at"`
	}
	if json.Unmarshal(data, &s) != nil || s.ReconciledAt == "" {
		return false
	}
	return s.ReconciledAt+"-"+filepath.Base(reviewDir) == runID
}

// replacement pairs the stale justification a record carries with the excerpt
// replayed from its review.md.
type replacement struct {
	from string // the stored text, as the EFFECTIVE record carries it
	to   string // the replayed excerpt
}

// pathHasSuffix reports whether p ends with the path-relative rel on a SEGMENT
// boundary.
//
// A plain strings.HasSuffix is not segment-aware, and the difference is not
// theoretical: rel is review-dir-relative (`sources/pool/raw/agent/dax/review.md`),
// so a sibling directory named `xsources/` or `my-sources/` at the same depth
// matched it. That produced a second, unrelated candidate and turned a repairable
// record into `ambiguous` — a silently declined repair the operator is then told to
// investigate as a real disagreement between reviews.
func pathHasSuffix(p, rel string) bool {
	if p == rel {
		return true
	}
	return strings.HasSuffix(p, string(os.PathSeparator)+rel)
}

// rewriteJustifications edits the justification field of every line whose id is in
// want AND whose stored justification is still the stale text want names.
//
// The second half of that predicate is what keeps the pass LINE-scoped rather than
// id-scoped, and it is load-bearing rather than an optimisation. One id can carry
// several lines — a resolution appended by `debt resolve` copies the effective
// record verbatim, and FoldRecords rule 2 lets a later re-detection displace it, so
// open@t1 -> resolved@t2 -> open@t3 is an ordinary shape. The resolved line's
// justification is then the operator's --reason (cli/debt_resolve.go replaces it),
// which exists nowhere else in the tree and cannot be replayed from anything. An
// id-scoped rewrite overwrites it with a review excerpt, irreversibly.
//
// Matching on the stale TEXT rather than on the presence of a status field is
// deliberate: a `deferred` line is a resolution-trail line too, but when it was
// filed without a --reason it merely COPIED the stale excerpt, and repairing it is
// the whole point of the pass for the one status class that is both stale and still
// closeable. Text provenance separates the two cases; a status check cannot.
//
// When dryRun is set the changes are computed and returned but no shard is written.
//
// It works at the JSON-object level rather than through Record + stageShard on
// purpose: stageShard re-marshals a decoded Record, which cannot round-trip a field
// this binary does not declare — the exact loss Compact's preserved-lines mechanism
// exists to prevent. Editing a map touches one key and carries the rest.
//
// Each shard is staged to a temp beside itself and renamed, so a failure mid-pass
// leaves whole shards, never a half-written one.
//
// # Coverage of the IO error paths
//
// Six wraps here return an os error with the operation named. Two are pinned through
// directory permissions — the ReadDir wrap on an unlistable store and the CreateTemp
// wrap on an unwritable one (TestRewriteJustifications_WrapsItsIOErrors). The other
// four — ReadFile, json.Marshal, the temp write, and the rename — are left UNCOVERED
// deliberately: reaching them needs a fake filesystem or a value json.Marshal rejects,
// and each is a single fmt.Errorf over an os error whose worst outcome is a less
// precise message, never a wrong write. Said here rather than pinned with a fake, the
// same stance repoRoot's error arm takes in cli/debt_backfill.go.
// shardFileNames is the shard filter both walks apply — the rewrite pass and the
// no-rewrite snapshot — extracted so the two copies cannot drift apart: a filter
// change on one walk and not the other would make the snapshot describe a different
// directory than the rewrite was computed against.
func shardFileNames(entries []os.DirEntry) []string {
	var names []string
	for _, e := range entries {
		if IsShardEntry(e) {
			names = append(names, e.Name())
		}
	}
	return names
}

func rewriteJustifications(dir string, want map[string]replacement, dryRun bool) ([]JustificationChange, []string, error) {
	var changes []JustificationChange
	// published is how much of changes is already ON DISK. A rename that completed is
	// not undone by a later shard failing, so every error return below hands back
	// changes[:published] — the prefix that was actually written.
	//
	// The truncation matters in BOTH directions. Returning nil claims a store that was
	// mutated is untouched; returning the whole of changes claims lines the failed
	// shard never received. Only the published prefix is true, and this is an
	// append-only store where the operator acts on that answer.
	//
	// It stays 0 on a dry run, which is correct: nothing is written, so a dry run that
	// fails part-way wrote nothing.
	published := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("reading localdebt dir for backfill: %w", quotedPathErr(err))
	}
	// Recorded for EVERY shard, before the want-lookup below can skip the file:
	// the caller disambiguates locators against the set of names on disk, and an
	// unchanged shard is exactly the collision its change set cannot see.
	shards := shardFileNames(entries)
	for _, e := range entries {
		if !IsShardEntry(e) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		// path is dir + an entry name os.ReadDir just returned, inside the store
		// directory this pass already holds the lock on — not caller input.
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return changes[:published], shards, fmt.Errorf("reading shard for backfill: %w", quotedPathErr(rerr))
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		changed := false
		for i, l := range lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			var m map[string]any
			if json.Unmarshal([]byte(l), &m) != nil {
				continue // a forward-incompatible or corrupt line is carried through untouched
			}
			id, _ := m["id"].(string)
			rep, wanted := want[id]
			if !wanted {
				continue
			}
			// ONE predicate, and it is what makes the rewrite LINE-scoped rather than
			// id-scoped: only a line still carrying the stale text is replayed over, so
			// a resolution trail's operator --reason on the same id survives.
			//
			// `cur == ""` and `cur == rep.to` used to sit here as extra disjuncts and
			// were unreachable, given how want is built: rep.from is never "" (a record
			// with an empty justification is skipped before it can reach want) and
			// rep.to is never rep.from (want is populated only where the replayed text
			// DIFFERS), so either shape already satisfies `cur != rep.from`. They read
			// as live guards while protecting nothing, which is how a future edit to the
			// want-construction loses a protection it appears to have.
			// TestRewriteJustifications_RewritesOnlyLinesCarryingTheStaleText pins both
			// shapes from this side, so removing them cannot go unnoticed.
			cur, ok := m["justification"].(string)
			if !ok || cur != rep.from {
				continue
			}
			m["justification"] = rep.to
			enc, merr := json.Marshal(m)
			if merr != nil {
				// BACKSTOP, unreachable by construction: m was produced by
				// json.Unmarshal, so every value in it is a marshalable JSON type
				// (string, float64, bool, nil, []any, map[string]any) and the one key
				// this loop writes is a string. json.Marshal has nothing here it can
				// fail on — JSON has no NaN or Inf literal for Unmarshal to have
				// produced. Its 0-hit coverage is therefore structural, not a missing
				// test. Kept because "cannot fail" is a property of today's decode
				// path, and a future change that hands this loop a hand-built map
				// would make it live.
				return changes[:published], shards, reencodeErr(id, merr)
			}
			lines[i] = string(enc)
			changed = true
			changes = append(changes, JustificationChange{
				ID: id, Shard: e.Name(), Line: i + 1, Before: cur, After: rep.to,
			})
		}
		if !changed || dryRun {
			continue
		}
		tmp, terr := os.CreateTemp(dir, "."+e.Name()+".tmp-*")
		if terr != nil {
			return changes[:published], shards, fmt.Errorf("creating temp file for backfill: %w", quotedPathErr(terr))
		}
		// The write and rename arms below are BACKSTOPS with 0-hit coverage, and that
		// is a testability limit rather than a test gap: POSIX grants "create a file
		// here" and "rename a file here" on the SAME directory write+execute bit, so
		// no permission trick can deny one while allowing the other. os.CreateTemp
		// runs first and would fail instead, which is the arm already covered
		// (TestRewriteJustifications_WrapsItsIOErrors). Reaching either one needs
		// fault injection — a package-level var wrapping os.Rename — and this package
		// deliberately uses real permission tricks rather than injection seams, so
		// introducing one for these two branches was judged the worse trade.
		//
		// Both uphold the same published-prefix contract the covered arms prove
		// (changes[:published], shards), and that contract IS exercised through the
		// read and CreateTemp arms — so the shape is pinned once even though these
		// two paths are unexecuted.
		_, werr := tmp.WriteString(strings.Join(lines, "\n") + "\n")
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(tmp.Name())
			return changes[:published], shards, fmt.Errorf("writing backfilled shard: %w", quotedPathErr(werr))
		}
		if rnerr := os.Rename(tmp.Name(), path); rnerr != nil {
			_ = os.Remove(tmp.Name())
			return changes[:published], shards, fmt.Errorf("publishing backfilled shard: %w", quotedPathErr(rnerr))
		}
		// The shard is on disk. Everything computed up to here is now published.
		published = len(changes)
	}
	return changes, shards, nil
}

// reencodeErr wraps a json.Marshal failure with the record id ESCAPED. The id is read
// verbatim out of a store line in a world-appendable directory, and this error reaches
// the same terminal the backfill dry-run listing does — the surface an operator consults
// to decide whether to let the in-place rewrite proceed. Under %s a bidi override in the
// id reorders that report; %q escapes the format (Cf) and control runes, matching the
// treatment the listing already gives the id.
func reencodeErr(id string, err error) error {
	return fmt.Errorf("re-encoding backfilled record %q: %w", id, err)
}

// quotedPathErr is basePathErr for the wraps that surface a SHARD NAME on the operator's
// terminal. basePathErr reduces the path to its base name for privacy — it never escapes
// it — and os.ReadDir returns that name verbatim from a world-appendable store directory,
// so a C0/C1/Cf rune in a shard filename rides an *os.PathError straight to the terminal.
//
// It layers on top of basePathErr rather than replacing it: the privacy reduction is the
// package's SECURITY contract with 27 call sites (see RedactPathErr), and quoting all of
// them is a wider change than the backfill command's own error paths.
//
// **os.Rename raises an *os.LinkError, not an *os.PathError**, and it carries TWO paths.
// basePathErr matches only *os.PathError, so it passes a LinkError through untouched —
// meaning the 'publishing backfilled shard' wrap disclosed both absolute paths (the store
// dir can contain a username; see the SECURITY note in store.go) and the shard name
// unescaped. That wrap is on the one path this helper exists for, so the LinkError arm is
// handled here rather than left to the caller.
func quotedPathErr(err error) error {
	var le *os.LinkError
	if errors.As(err, &le) {
		clone := *le
		clone.Old = strconv.Quote(filepath.Base(le.Old))
		clone.New = strconv.Quote(filepath.Base(le.New))
		return &clone
	}
	var pe *os.PathError
	if errors.As(basePathErr(err), &pe) {
		clone := *pe
		clone.Path = strconv.Quote(pe.Path)
		return &clone
	}
	return err
}
