package localdebt

import (
	"sort"
	"strings"
)

// qualitysignal.go builds the content-free, per-(persona, model) quality signal
// (Sprint 30.0) from the append-only local-debt stream. It reads only the
// Reviewers, Model, and Status fields already present on Record — never code,
// file paths, or problem/fix text — so the aggregated shape is structurally
// incapable of carrying finding content.

// foldTerminalByID folds the append-only record stream by ID down to at most one
// TERMINAL record per id, discarding ids whose effective record is not terminal.
// It reuses FoldRecords for the precedence logic — a suppressing (wontfix) record
// wins unconditionally, otherwise the latest timestamp wins with rank breaking a
// tie (see FoldRecords) — and then keeps only the ids whose effective record is
// terminal, because an unsettled finding is not yet a quality signal.
//
// Since resolution became re-openable, "not terminal" covers two cases: an id
// that never closed, and an id that closed and then was RE-DETECTED. The first is
// always excluded. A re-detected id folds to its newer open record and is
// excluded too, with one exception: when its latest superseded terminal record
// is attempts-exhausted, that record is kept. Re-detection confirms
// attempts-exhausted (the finding is still broken, which is what the status
// says), while it contradicts resolved and unreproducible, so dropping it would
// erase a still-true outcome and trend AttemptsExhaustedCount to zero by
// construction. wontfix never reaches this path: it survives re-detection in the
// fold itself.
//
// The fold is O(n): FoldRecords does a single keyed pass, the donor index below
// adds one more linear pass, and the filter's recovery is an O(1) map lookup —
// no per-id rescan of the whole stream.
func foldTerminalByID(records []Record) []Record {
	effective := FoldRecords(records)

	// Per-id index of the most recent terminal record's Model, built in ONE
	// keyed pass (last-wins on timestamp ties, matching FoldRecords). The
	// recovery below used to rescan the entire slice once per attribution-less
	// terminal id — quadratic on a store near the 100k-record auto-compaction
	// ceiling, where every v1 record and every model-less resolution needs it.
	donorTS := map[string]string{}
	donorModel := map[string]string{}
	donorModelReviewers := map[string][]string{}
	// Per-id terminal records, so a re-detected id can fall back to its latest
	// superseded outcome through the fold's own recency rule (latestItem).
	terminalsByID := map[string][]Record{}
	for _, r := range records {
		if IsClosedStatus(r.Status) {
			terminalsByID[r.ID] = append(terminalsByID[r.ID], r)
		}
		if !IsClosedStatus(r.Status) || strings.TrimSpace(r.Model) == "" {
			continue
		}
		if _, ok := donorModel[r.ID]; !ok || r.Timestamp >= donorTS[r.ID] {
			donorModel[r.ID] = r.Model
			donorTS[r.ID] = r.Timestamp
			// The grafted model covers the DONOR's attributable set, which need
			// not match the effective record's full Reviewers. A pre-
			// ModelReviewers donor has its attributable set in Reviewers
			// already (write-time narrowing), so fall back to that.
			donorModelReviewers[r.ID] = r.ModelReviewers
			if len(donorModelReviewers[r.ID]) == 0 {
				donorModelReviewers[r.ID] = r.Reviewers
			}
		}
	}

	terminal := make([]Record, 0, len(effective))
	for _, r := range effective {
		if !IsClosedStatus(r.Status) {
			prior, ok := terminalsByID[r.ID]
			if !ok {
				continue
			}
			latest := latestItem(prior)
			if normalizeStatus(latest.Status) != StatusAttemptsExhausted {
				continue
			}
			r = latest
		}
		// The effective terminal record can be an attribution-less one even when an
		// earlier same-id terminal carried a real Model — a wontfix that outranks an
		// earlier resolved, or simply a later resolution. AggregateQualitySignal
		// excludes an empty Model, so without this the whole finding — a genuine
		// outcome that DID have model attribution — would be silently dropped.
		// Recover the model from the most recent same-id terminal that carries one
		// before excluding.
		if strings.TrimSpace(r.Model) == "" {
			r.Model = donorModel[r.ID]
			if r.Model != "" {
				// Credit the donor's attributable subset, not the effective
				// record's full Reviewers: a persona on the effective record
				// who never ran on the donor's model must not receive a
				// confirmation/dismissal under it — the same mis-crediting
				// resolveRecordModel refuses at write time.
				r.ModelReviewers = donorModelReviewers[r.ID]
			}
		}
		terminal = append(terminal, r)
	}
	return terminal
}

// producesQualitySignal reports whether an effective record with this status
// contributes a row to AggregateQualitySignal — i.e. whether the status is one
// of the four COUNTED terminal outcomes, as opposed to merely terminal.
//
// `deferred` is the one terminal status that is not counted: it records that a
// decision was postponed, which says nothing about whether the finding was real
// and so is not a quality signal about the reviewer that raised it.
//
// This has NO production caller. It is kept purely as the documented vocabulary
// of counted outcomes — the four terminal statuses a quality-signal row can be
// built from, spelled once and pinned by
// TestProducesQualitySignal_MatchesTheAggregationSwitch, which asserts the set
// against the aggregation switch below directly.
//
// The caller history matters because two removed call sites left comments that
// still name this predicate, and both named it for a question it no longer
// answers:
//
//   - retainForCompaction used to consult it as the proxy for "losing this
//     attribution deletes a signal row", gated on settledness until Story 36.0
//     made `attempts-exhausted` both unsettled AND counted. The branches have
//     since merged and the donor is called unconditionally (modelDonorIndex),
//     with the old gate explicitly retired — "do not add such a gate back"
//     (store.go, beside the donor call). Retention now asks bearsRationale,
//     the rationale-certainty question, not the counting question.
//   - The aggregation switch keeps its arms in step with this predicate by
//     convention, but nothing consults the predicate at runtime; a status
//     counted in one and not the other is drift this pin surfaces at the next
//     test run, not a lost signal row.
func producesQualitySignal(status string) bool {
	switch normalizeStatus(status) {
	case StatusWontfix, StatusResolved, StatusUnreproducible, StatusAttemptsExhausted:
		return true
	default:
		return false
	}
}

// QualityRow is one aggregated per-(persona, model) quality-signal row: how many
// findings that persona+model raised were later dismissed (status wontfix) versus
// confirmed (status resolved). It is the internal aggregation shape — a fixed,
// content-free set of fields (never code, path, or finding text) — that the
// outbound telemetry.QualitySignal payload and the maintainer report are built
// from.
type QualityRow struct {
	Persona        string
	Model          string
	DismissedCount int
	ConfirmedCount int
	// UnreproducibleCount and AttemptsExhaustedCount are the Story 36.0
	// ground-truth outcomes, counted SEPARATELY rather than folded into the two
	// counters above. Merging them would destroy the distinction the durable
	// lens score is built on: "was fixed", "was never real" and "could not be
	// fixed" say different things about the reviewer that raised the finding,
	// and only the first is a confirmation.
	//
	// These two stay internal to this package for now. The outbound
	// telemetry.QualitySignal payload and the local maintainer report are
	// field-by-field allowlists and are deliberately NOT extended here; growing
	// them is a separate, deliberate edit.
	UnreproducibleCount    int
	AttemptsExhaustedCount int

	// TerminalOutcomes is the number of terminal records that produced this row:
	// the explicit denominator for any rate built over the four counters above.
	//
	// It is exactly their sum, and therefore carries no information they do not
	// — it is a convenience and a pinned invariant, not a new signal. Say so
	// plainly, because the obvious richer claim is FALSE and a consumer acting
	// on it would be wrong: this field does NOT distinguish a measured zero from
	// an unmeasured axis. A pre-36.0 store and a post-36.0 store whose reviewer
	// simply never produced an `unreproducible` outcome emit byte-identical
	// rows. Nothing in this shape can tell them apart; doing so would need the
	// store's observed status horizon, which is not recorded anywhere.
	//
	// What it does give a consumer is the sample size without re-summing, so
	// "is this measurable yet?" — the question DefaultTrustMinRuns answers for
	// trust priors — can be asked directly of the row.
	//
	// These counters are CURRENT FOLD STATE, not cumulative history.
	// AggregateQualitySignal folds to one terminal record per finding id, so an
	// id that was attempts-exhausted and later resolved contributes one
	// confirmation and no exhausted attempt. That is the correct answer to "what
	// happened in the end", and the wrong answer to "how often did this
	// reviewer's findings resist a fix" — do not use these as the latter.
	//
	// A re-detection (a fresh open record after a terminal one) is part of that
	// fold state too. It drops the id's outcome when the latest terminal record
	// was resolved or unreproducible, because re-detection contradicts both. It
	// KEEPS an attempts-exhausted outcome, because re-detection confirms it —
	// see foldTerminalByID.
	TerminalOutcomes int
}

// AggregateQualitySignal folds the append-only debt stream by ID to its terminal
// records, then groups those by (persona, model) and sums dismissed (wontfix),
// confirmed (resolved), unreproducible and attempts-exhausted counts, returning
// one row per distinct pair sorted persona ascending then model ascending. The
// last two are the Story 36.0 ground-truth outcomes and are counted on their own
// axes — see QualityRow for why they are not folded into the first two. It
// mirrors internal/scorecard/aggregate.go's
// Aggregate() grouping/sort idiom (map-of-key + insertion-order slice +
// sort.SliceStable tie-break).
//
// Exclusion rules (all content-free, reading only Reviewers/Model/Status):
//   - Records with an empty Model (v1, or v2 with unresolved attribution) are
//     excluded from every per-model row rather than bucketed under "" (AC 01-02).
//   - A terminal status outside the four counted outcomes (i.e. deferred)
//     contributes to no counter and creates no group, so a deferred-only pair
//     emits no row (AC 01-01 EC2). `unreproducible` and `attempts-exhausted`
//     deliberately do NOT take this arm: each is a measured outcome, so a pair
//     whose only outcome is one of them still emits a row.
//   - Every listed persona receives the outcome, deduplicated per-record with
//     empty entries skipped (AC 01-03); an empty reviewer list contributes to
//     no group. The list read is ModelReviewers (the subset the record's Model
//     covers), falling back to Reviewers on pre-field records, whose Reviewers
//     were already narrowed to the attributable set at write time.
//
// It is a total, pure function: nil input yields a non-nil empty slice, and
// repeated calls on the same input are byte-for-byte identical (no shared mutable
// state). Complexity is O(n) fold + O(sum reviewers) group + O(k log k) sort.
func AggregateQualitySignal(records []Record) []QualityRow {
	type key struct{ persona, model string }
	groups := map[key]*QualityRow{}
	order := []key{}

	for _, rec := range foldTerminalByID(records) {
		// Trim attribution fields the same way Status is normalized below, so a
		// whitespace-only model or persona is treated as empty/excluded rather than
		// forming its own spurious group (adversarial 1.8.A). Model slugs and persona
		// names are catalog-controlled today, but the exclusion contract is enforced
		// structurally, not left to input hygiene.
		model := strings.TrimSpace(rec.Model)
		if model == "" {
			continue // attribution-incomplete: excluded from per-model rows
		}
		var dismissed, confirmed, unreproducible, attemptsExhausted int
		switch normalizeStatus(rec.Status) {
		case StatusWontfix:
			dismissed = 1
		case StatusResolved:
			confirmed = 1
		case StatusUnreproducible:
			unreproducible = 1
		case StatusAttemptsExhausted:
			attemptsExhausted = 1
		default:
			continue // deferred (or any other terminal) is neither a signal nor a group
		}
		// Keep the arms above and producesQualitySignal in step. The predicate has
		// no production caller: store.go's compaction gates on bearsRationale and
		// deliberately does NOT consult this predicate ("do not add such a gate
		// back", beside modelDonorIndex). The pairing is documentation, not a
		// runtime contract — it keeps the counted-outcome vocabulary spelled once,
		// and TestProducesQualitySignal_MatchesTheAggregationSwitch surfaces any
		// drift between the two at the next test run.

		seen := map[string]bool{}
		// ModelReviewers is the attributable subset for the record's Model. An
		// empty ModelReviewers on a model-carrying record means a pre-field
		// record, whose Reviewers were already narrowed to exactly that set at
		// write time — falling back to them preserves the pre-field behavior
		// for the existing store.
		reviewers := rec.ModelReviewers
		if len(reviewers) == 0 {
			reviewers = rec.Reviewers
		}
		for _, raw := range reviewers {
			persona := strings.TrimSpace(raw)
			if persona == "" || seen[persona] {
				continue
			}
			seen[persona] = true
			k := key{persona, model}
			row, ok := groups[k]
			if !ok {
				row = &QualityRow{Persona: persona, Model: model}
				groups[k] = row
				order = append(order, k)
			}
			row.DismissedCount += dismissed
			row.ConfirmedCount += confirmed
			row.UnreproducibleCount += unreproducible
			row.AttemptsExhaustedCount += attemptsExhausted
			row.TerminalOutcomes++
		}
	}

	rows := make([]QualityRow, 0, len(order))
	for _, k := range order {
		rows = append(rows, *groups[k])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Persona != rows[j].Persona {
			return rows[i].Persona < rows[j].Persona
		}
		return rows[i].Model < rows[j].Model
	})
	return rows
}
