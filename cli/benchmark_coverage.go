package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/scorecard"
)

// coverageKey is the identity the gate joins on: the SCRUBBED (model, persona).
// Publication re-scrubs every reviewer via scorecard.ScrubPublicRecord as a
// defense-in-depth backstop, so the board keys on the scrubbed value — and a
// check keyed on the pre-transform string is bypassable by construction: two raw
// identities differing only by a scrub-stripped token would be distinct to the
// duplicate check yet identical on the public board.
func coverageKey(model, persona string) reviewerKey {
	s := scorecard.ScrubPublicRecord(scorecard.PublicRecord{Model: model, Persona: persona})
	return reviewerKey{model: s.Model, persona: s.Persona}
}

// duplicateIdentityError is the ONE duplicate-identity rejection the coverage and
// reviewer arrays share, so the two cannot answer the same condition differently.
// `what` is the object phrase ("coverage for" / "reviewer"); `key` is the published
// identity the two rows collided on; prev* are the previously-seen row's RAW identity
// (kept by the caller precisely so this branch can name the collision partner), cur*
// the current row's raw identity, and model/persona the current row's STRIPPED values,
// computed once per row for the caller's other diagnostics.
//
// Two IDENTICAL raw rows are malformed outright. Two DIFFERENT raw identities
// colliding on one published identity are not NECESSARILY tampering, and flatly
// saying "malformed" would send the operator hunting for it: scrubField iterates to
// a fixed point (scorecard/export.go), so strictly more distinct raw values collapse
// to one published identity than when the PRODUCER's own collision check ran
// (cli/benchmark_run.go) under single-pass scrubbing — a run-result written by an
// earlier atcr can therefore have been well-formed when written and fail here purely
// on version skew.
//
// The message names that possibility WITHOUT ranking it, because this branch cannot
// tell it from hand-assembly. The discriminator is only "these are two different raw
// strings", which both causes satisfy. The two sharper tests that suggest themselves
// do not work: "stable under one pass but not under the fixed point" is the EMPTY
// SET (scrubField halts the moment one pass is stable), and "a single pass still
// rewrites it" is not evidence of hand-assembly either, since a pre-fixed-point
// producer emitted exactly such values — benchmark_run.go records
// "bedrock@us-east-1/claude" scrubbing once to "/claude" and twice to "". Separating
// the causes needs image-membership under scrubOnce, which a seven-regex sequential
// pipeline does not invert cheaply, and the blast radius here (two path- or
// email-shaped ids colliding) does not justify building it.
//
// The rejection stands either way: two rows cannot share one published identity on
// the board, whatever wrote them.
func duplicateIdentityError(path, what string, key reviewerKey, prevModel, prevPersona, curModel, curPersona, model, persona string) error {
	if prevModel != curModel || prevPersona != curPersona {
		// %q on BOTH raw halves, NOT stripTerminalControlRunes — the same rule
		// anchorSuiteDenominator states at length for the suite-identity gate, and for
		// the same reason. This message's entire job is to show a DIFFERENCE, and
		// stripping sanitizes by deletion: unicode.IsControl covers \r, \n and \t,
		// which are exactly the runes scrubOnce collapses via strings.Fields, so the
		// pair that most naturally reaches this branch ("m" vs "m\r") rendered as two
		// copies of one name. The operator was shown identical text, told the two
		// differ, and told to rename one — an instruction unfollowable from what is
		// displayed. %q gives the same terminal safety (a control rune becomes a
		// visible escape) while keeping the difference legible.
		//
		// The published identity is named too. The message asserts the two collapsed
		// onto one value; without showing it, the operator cannot tell which of the two
		// names survived the scrub, and that is the name they have to rename away from.
		// Remedy ORDER is load-bearing. Re-running was listed first, and for the
		// version-skew cause the same sentence names it provably cannot work: the
		// producer's own collision check (buildRunResult in benchmark_run.go) uses the same
		// fixed-point ScrubPublicRecord this key does, and scrubField(scrubOnce(x))
		// has the same fixed point as scrubField(x) — so a file that collides at
		// export implies the raw ids collide at fixed point too, which means
		// buildRunResult refuses and writes no run-result at all. Worse, that check
		// runs AFTER every case has executed, so the suggested remedy costs a full
		// panel run and then fails with a second collision error. Renaming is the
		// remedy that terminates, so it leads.
		return fmt.Errorf("run-result %s records %s %q/%q and %q/%q, which are distinct raw identities "+
			"that scrub to the same published identity %q/%q; this file was either written by an older atcr under an "+
			"earlier, single-pass privacy scrub (version skew) or hand-assembled. Rename the model/persona ids so "+
			"they stay distinct after scrubbing; re-running `atcr benchmark run` helps only if the file was "+
			"hand-assembled, since this version's producer rejects a genuine identity collision itself",
			path, what, prevModel, prevPersona, curModel, curPersona, key.model, key.persona)
	}
	return fmt.Errorf("run-result %s records %s %s/%s more than once; "+
		"a reviewer identity has exactly one covered case set, so this file is malformed",
		path, what, model, persona)
}

// maxNamedMissingCases caps how many missing case ids the shortfall message spells
// out per row. The message exists to make the shortfall ACTIONABLE, and a row short
// by fifty cases is diagnosed by the first few plus the count; printing all of them
// buries the other short rows below it. It is a fixed limit, not a configuration
// knob — no flag or setting surfaces it.
//
// The cap applies to the INNER list (case ids within one row) and deliberately NOT
// to the OUTER one (the short rows themselves): each short row is a distinct
// reviewer identity whose re-runs the operator must schedule, so dropping one
// behind an overflow count would hide WHO owes work — the actionable unit of this
// message. Missing case ids within a row are interchangeable by contrast; the
// first few plus the count diagnose the row fully. Pinned by
// TestCheckCoverage_EveryShortRowIsNamed and TestRunBFixture_RejectedByExportGate.
//
// The bound is PER HALF of a row's shortfall, not per row. describeMissing splits a
// row into genuinely-missing, case-level-unmeasured and slot-level-unshown cases and
// caps each independently, so a row carrying all three can name up to
// 3*maxNamedMissingCases ids. That is deliberate: the halves call for different
// responses (re-run the suite vs. investigate a case fault vs. investigate one
// reviewer's provider), and capping them jointly would let one half's overflow hide
// another half's existence — the same argument that keeps the outer list uncapped,
// applied one level in.
const maxNamedMissingCases = 3

// halfSeparator divides describeMissing's halves. It is deliberately NOT the
// "; " checkCoverage uses between distinct short rows: the row list is the outer
// nesting level, and a message that nests both must keep the two delimiters
// distinguishable — otherwise a reader, or anything downstream that splits on it,
// reads one reviewer row as two.
const halfSeparator = " / "

// checkCoverage is the publication gate: no reviewer row may reach the public board
// having been scored over less than the suite the run-result declares.
//
// SCOPE — read this before quoting the gate as a suite-coverage guarantee. The
// denominator (rr.SuiteCaseIDs) is supplied by the same file the gate validates, so
// on its own this is an INTERNAL-CONSISTENCY check: it proves every reviewer row
// covers the declared case list, not that the declared case list is the suite's. A
// caller who truncates suite_case_ids and every row's case_ids to the same subset
// passes every check below. anchorSuiteDenominator closes that gap by comparing the
// declared list against the suite manifest, and `atcr benchmark export --suite-path`
// is what promotes this gate to the full guarantee. Without that flag, what follows
// is the weaker claim.
//
// The gate exists because `runs` is a count and case difficulty on this suite varies
// enormously — two rows built from different subsets are not comparable, and reading
// them side by side on a leaderboard silently compares different measurements.
// Splitting rows by realized model makes uneven coverage the NORMAL result of a
// quota-limited run, so this is the routine case rather than the exotic one.
//
// Coverage is compared as a SET, never as a count: a row carrying the right NUMBER of
// case ids but not THE right ids is short, and a length check would publish it.
//
// It deliberately chooses COVERAGE, not "degradation", as the gate. Marking any
// failover degraded would flag most real runs and train operators to pass the opt-out
// reflexively, which is how a safety flag becomes a no-op.
//
// UNMEASURED IS NOT SHORT. A run-result with no coverage data — any file written
// before the field existed — cannot be checked at all, so it warns and exports rather
// than failing. This mirrors the same file's existing treatment of a nil
// out_of_vocabulary_rate ("nil stays legal — it means unmeasured"): the tool reports
// what it does not know instead of inventing a violation. Failing closed here would
// reject every pre-existing run-result over a field they had no way to write.
func checkCoverage(w io.Writer, rr benchmark.RunResult, path string, allowPartial bool) error {
	// UNMEASURED is keyed on the DENOMINATOR being absent, not on the coverage rows
	// being absent. A file that records suite_case_ids but no coverage rows is not a
	// pre-epic artifact — it is one whose coverage was removed, and treating that as
	// "unmeasured" would make deleting the whole array a cheaper way past this gate
	// than any of the tampering shapes below. The reverse shape is malformed for the
	// same reason: the producer writes suite_case_ids and reviewer_coverage together,
	// so a coverage array with a stripped denominator is a demonstrably post-epic file
	// missing exactly one key — deleting it must not be a cheaper bypass than the
	// --allow-partial-coverage opt-out. Only a file with NEITHER field is genuinely
	// unmeasurable, and keeps the warn-only path below.
	if len(rr.SuiteCaseIDs) == 0 {
		if len(rr.Coverage) > 0 {
			return fmt.Errorf("run-result %s records reviewer coverage but no suite_case_ids; "+
				"`atcr benchmark run` writes the two together, so this file is malformed", path)
		}
		_, _ = fmt.Fprintf(w,
			"warning: run-result %s carries no case coverage, so it cannot be verified against the full suite — "+
				"publishing it asserts nothing about how many cases each reviewer actually scored. "+
				"Re-run `atcr benchmark run` to produce a run-result that records coverage.\n", path)
		return nil
	}
	suite := make(map[string]bool, len(rr.SuiteCaseIDs))
	for _, id := range rr.SuiteCaseIDs {
		// The denominator comes from the same untrusted file it validates, so it
		// must be a set by construction: a repeated id would shrink the required
		// set while inflating the reported suite size (["case-01"] x3 would read as
		// full coverage of a "3-case suite"). The producer writes the manifest's
		// case list, which cannot repeat — same malformed-file rule as a repeated
		// covered id below. This runs BEFORE the empty-coverage check so every
		// later diagnostic can use len(suite) — the distinct denominator — rather
		// than the raw count a repeated id would inflate.
		if suite[id] {
			return fmt.Errorf("run-result %s lists suite case %q more than once; "+
				"the producer writes the manifest's case list, which cannot repeat, so this file is malformed",
				path, id)
		}
		suite[id] = true
	}
	if len(rr.Coverage) == 0 {
		return fmt.Errorf("run-result %s lists a %d-case suite but records no reviewer coverage; "+
			"`atcr benchmark run` writes the two together, so this file is malformed",
			path, len(suite))
	}

	// Index coverage by the (model, persona) identity the reviewer rows carry, so a
	// reviewer row with NO coverage row is caught rather than skipped. An
	// unverifiable row is exactly what a hand-supplied run-result would use to slip
	// past this gate, and export is where hand-supplied files first enter the tool.
	byIdentity := make(map[reviewerKey]benchmark.ReviewerCoverage, len(rr.Coverage))
	for _, c := range rr.Coverage {
		key := coverageKey(c.Model, c.Persona)
		// Stripped ONCE per row and used at every interpolation site below. Every
		// rejection here is operator-facing — cobra prints it to the same terminal as
		// the `short` warnings further down — and every one of them returns EARLY,
		// before `short` is ever built. Sanitizing only the warnings would leave the
		// paths a hostile run-result reaches FIRST able to erase the operator's line.
		model, persona := stripTerminalControlRunes(c.Model), stripTerminalControlRunes(c.Persona)
		// A DUPLICATE identity is rejected outright rather than resolved. The
		// producer emits one row per unique identity by construction, so a duplicate
		// means the file was hand-assembled — and last-write-wins would let a full
		// row mask a short one carrying the same identity, which is the cheapest
		// possible way to walk a partial run past this gate.
		if prev, dup := byIdentity[key]; dup {
			return duplicateIdentityError(path, "coverage for", key, prev.Model, prev.Persona, c.Model, c.Persona, model, persona)
		}
		// The outcomes tally is untrusted input here, exactly like
		// out_of_vocabulary_rate at the load boundary: the producer writes one
		// closed-vocabulary outcome per case, so a negative count or a key outside
		// the vocabulary can only come from a hand-assembled file. The sum check
		// below cannot catch either shape — a -1 paired with an inflated positive
		// still sums to the covered-set size. The "unknown" TALLY label is a legal
		// key (OutcomeTallyKey spells OutcomeUnknown that way); every other key is
		// a stored wire value, so ValidOutcome is the allowlist.
		for k, n := range c.Outcomes {
			if n < 0 {
				return fmt.Errorf("run-result %s records a negative outcome tally for %s/%s (%q: %d); "+
					"the producer counts one outcome per case, so this file is malformed",
					path, model, persona, k, n)
			}
			// Both causes are named, like duplicateIdentityError above. The vocabulary
			// GROWS — repo-state-v1 added "ungrounded" — so the commonest way to reach
			// this branch is an older atcr exporting a newer run-result, not a hand-
			// assembled file. Reporting only hand-assembly sends that operator auditing
			// a file nobody edited. Version skew leads because it is both the likelier
			// cause and the one with a remedy that terminates; re-running under this
			// build cannot help, since a producer of this version writes only values
			// this version knows.
			// An EMPTY key is rejected before the allowlist: ValidOutcome("") is
			// true — the empty string is OutcomeUnknown's stored wire value — so
			// the check below would admit {"": 17}, the exact legal-but-awful
			// shape OutcomeUnknownLabel exists to prevent. No producer version
			// can emit it: a tally is written through benchmark.OutcomeTallyKey,
			// which spells the unknown outcome "unknown", so hand-assembly is
			// the only cause and version skew does not apply.
			if k == "" {
				return fmt.Errorf("run-result %s records an empty outcome tally key for %s/%s; "+
					"a producer writes tally keys through benchmark.OutcomeTallyKey, which spells "+
					"the unknown outcome %q, so this file is hand-assembled",
					path, model, persona, benchmark.OutcomeUnknownLabel)
			}
			if k != benchmark.OutcomeUnknownLabel && !benchmark.ValidOutcome(k) {
				return fmt.Errorf("run-result %s records outcome tally key %q for %s/%s, outside the outcome vocabulary "+
					"this build knows; the file was either written by a NEWER atcr whose vocabulary added the value "+
					"(version skew — upgrade atcr and re-export) or hand-assembled, since a producer of this version "+
					"writes only benchmark.Outcome* values",
					path, k, model, persona)
			}
		}
		// grounding_enabled is published verbatim into the public envelope and is the
		// tag saying which population corroboration_rate was computed over, so it gets
		// the same untrusted-input treatment as the tally above. The producer
		// guarantees exactly one implication for free: reviewerOutcome reaches
		// OutcomeUngrounded only via AgentStatus.DroppedByGrounding > 0, which the gate
		// cannot produce when it is off. A row claiming both is self-contradictory.
		//
		// Only an EXPLICIT false is rejected. nil means unmeasured — a rebuilt summary,
		// or a row folded across a mix of gated and ungated cases — which is
		// uninformative rather than contradictory, and rejecting it would make a
		// legitimate paid run unexportable at the one boundary with no remedy. That is
		// narrower than "must be true", deliberately: this gate's job is to catch a
		// claim the producer cannot make, not to require one it may not have.
		//
		// The message names the PRODUCER alongside hand-assembly, the way
		// duplicateIdentityError names version skew: a row folded across a mix of gated
		// and ungated cases USED TO AND to false rather than to nil, so this pair is
		// reachable from a legitimate paid run written by such a build. It is no longer
		// reachable from a CURRENT one — foldGroundingEnabled
		// (cli/benchmark_repostate.go:947) requires unanimity and yields nil for a mixed
		// row — which is why the error text below is past tense and names an upgrade as
		// the remedy. Reporting only "hand-assembled" would send that operator hunting an
		// edit nobody made.
		if c.GroundingEnabled != nil && !*c.GroundingEnabled && c.Outcomes[benchmark.OutcomeUngrounded] > 0 {
			return fmt.Errorf("run-result %s records %d %q outcome(s) for %s/%s while claiming grounding_enabled=false; "+
				"that outcome is reached only when the grounding gate dropped a finding, so the two cannot both be true — "+
				"either the file was hand-assembled, or it was written by a build whose multi-case fold reported a mixed "+
				"run as ungated rather than as unmeasured (upgrade atcr and re-run)",
				path, c.Outcomes[benchmark.OutcomeUngrounded], benchmark.OutcomeUngrounded, model, persona)
		}
		byIdentity[key] = c
	}

	// The infra-failure index behind every shortfall diagnostic below. A case in
	// here was never measured, so "re-run the missing cases" is the wrong
	// instruction for it — the case did not run, and whether a re-run helps depends
	// on the reason, which is why the reason is what gets printed.
	//
	// An entry whose reason is outside the vocabulary is DROPPED rather than
	// printed, so that case falls back to reading as plainly missing. runBenchmarkExport
	// already rejects such a file through validateCaseFailures before calling this,
	// but the safety of an operator-facing diagnostic must not rest on the order two
	// functions happen to be called in: a second caller would otherwise interpolate
	// an arbitrary attacker-chosen string into the terminal.
	//
	// The drop is ANNOUNCED, not silent. Without this line the case simply reads as
	// plainly missing, so a caller reaching checkCoverage without the export command's
	// gate in front of it is told to re-run a case the file claims was unmeasured, with
	// no hint that the file said anything about it. The entry is named by case id and
	// the reason is reported stripped and under %q — the same treatment
	// validateCaseFailures' own rejection gives it — so the operator learns the entry
	// existed without the reason's arbitrary prose being interpolated as an explanation.
	failed := make(map[string]string, len(rr.CaseFailures))
	for _, f := range rr.CaseFailures {
		if !benchmark.ValidCaseFailureReason(f.Reason) {
			_, _ = fmt.Fprintf(w,
				"warning: run-result %s records a case_failures entry for %s with reason %q, outside the failure vocabulary; "+
					"it explains nothing and is ignored, so that case is reported as plainly missing.\n",
				path, stripTerminalControlRunes(f.CaseID), stripTerminalControlRunes(f.Reason))
			continue
		}
		failed[f.CaseID] = f.Reason
	}

	// The slot-failure index behind the third shortfall label. Keyed by IDENTITY
	// first, because a slot failure is a statement about one reviewer: the case ran,
	// and only this row is short by it. A flat case-keyed map would attach one
	// reviewer's excuse to every other row that happens to be short of the same case
	// — an unearned explanation, which is the shape validateSlotFailures exists to
	// keep out of the file in the first place.
	//
	// The same defence-in-depth drop as the case index above, and for the same
	// reason: runBenchmarkExport rejects an out-of-vocabulary reason through
	// validateSlotFailures before reaching here, but a second caller must not be able
	// to interpolate an arbitrary string into the terminal, and the drop is ANNOUNCED
	// so the case does not silently fall back to "missing" with no hint the file said
	// anything about it.
	slotFailed := map[reviewerKey]map[string]string{}
	for _, sf := range rr.SlotFailures {
		if !benchmark.ValidSlotFailureReason(sf.Reason) {
			_, _ = fmt.Fprintf(w,
				"warning: run-result %s records a slot_failures entry for %s/%s on %s with reason %q, outside the "+
					"failure vocabulary; it explains nothing and is ignored, so that case is reported as plainly missing.\n",
				path, stripTerminalControlRunes(sf.Model), stripTerminalControlRunes(sf.Persona),
				stripTerminalControlRunes(sf.CaseID), stripTerminalControlRunes(sf.Reason))
			continue
		}
		k := coverageKey(sf.Model, sf.Persona)
		if slotFailed[k] == nil {
			slotFailed[k] = map[string]string{}
		}
		slotFailed[k][sf.CaseID] = sf.Reason
	}

	var short []string
	// The identities behind `short`, kept in the same order so the remedy below can
	// ask which short rows lost a slot. `short` itself is pre-formatted display text
	// and cannot be matched back to an identity without re-parsing it.
	var shortKeys []reviewerKey
	consumed := make(map[reviewerKey]scorecard.PublicRecord, len(rr.Reviewers))
	for _, rev := range rr.Reviewers {
		key := coverageKey(rev.Model, rev.Persona)
		// Stripped once per row, for the same reason as the coverage loop above: the
		// rejections below reach the terminal before any sanitized warning does.
		model, persona := stripTerminalControlRunes(rev.Model), stripTerminalControlRunes(rev.Persona)
		// The reviewer array gets the same duplicate-identity rule as the coverage
		// array above: two identical reviewer rows both join the single coverage row
		// and both publish, putting two different metric sets on the board under one
		// identity. The rejection rationale — a reviewer identity has exactly one
		// covered case set — applies verbatim here, and the previously-seen row is
		// kept so a scrub collision can name BOTH raw identities, like the coverage
		// branch does. The consumed set doubles as the seen set: every joined
		// identity is recorded exactly once.
		if prev, dup := consumed[key]; dup {
			return duplicateIdentityError(path, "reviewer", key, prev.Model, prev.Persona, rev.Model, rev.Persona, model, persona)
		}
		consumed[key] = rev
		cov, ok := byIdentity[key]
		if !ok {
			// The producer appends reviewers and coverage from the same accumulator
			// loop, so a reviewer row with no coverage row cannot come from it — this
			// is the one shape that is hand-assembly by construction. It is also the
			// one shape the documented shortfall join cannot describe: there is no
			// reviewer_coverage row whose case_ids a consumer could compare against
			// suite_case_ids, and the runs/covered-set sanity check below has nothing
			// to run against. Reject rather than mark short, in both gate modes — the
			// opt-out publishes a shortfall the submission makes visible, and this
			// row would be invisible.
			return fmt.Errorf("run-result %s: reviewer %s/%s has no coverage row; "+
				"`atcr benchmark run` writes the two arrays from the same accumulator, so this file is malformed",
				path, model, persona)
		}
		// `runs` and the covered set are appended together by the producer, so they
		// are equal by construction and a mismatch can only come from editing. Left
		// unchecked, a row could publish `runs` measured over two cases while
		// presenting a seventeen-case coverage list as its provenance.
		if rev.Runs != len(cov.CaseIDs) {
			return fmt.Errorf("run-result %s: reviewer %s/%s reports runs=%d but records %d covered case(s); "+
				"the two are written together by `atcr benchmark run`, so this file is malformed",
				path, model, persona, rev.Runs, len(cov.CaseIDs))
		}
		// The outcomes tally is written together with the covered set by the same
		// fold (one outcome per case), so its values must sum to len(cov.CaseIDs) —
		// the same tamper family as the runs/coverage pair, one field over. An
		// absent tally stays legal (omitempty): pre-field files have none.
		if cov.Outcomes != nil {
			tally := 0
			for _, n := range cov.Outcomes {
				tally += n
			}
			if tally != len(cov.CaseIDs) {
				return fmt.Errorf("run-result %s: reviewer %s/%s records outcomes summing to %d over %d covered case(s); "+
					"the two are written together by `atcr benchmark run`, so this file is malformed",
					path, model, persona, tally, len(cov.CaseIDs))
			}
		}
		// A covered set must be a set OF THE SUITE. Checking only for missing ids
		// lets ["case-01","case-02","case-03","case-01"] satisfy a 3-case suite while
		// reporting runs=4 — full marks for a row that scored one case twice and
		// carries a bigger denominator than the suite has cases.
		missing, verr := validateCoveredSet(suite, cov.CaseIDs, path, model, persona)
		if verr != nil {
			return verr
		}
		if len(missing) == 0 {
			continue
		}
		// covered is len(cov.CaseIDs) unconditionally: validateCoveredSet just
		// rejected any repeat and any non-member, so every id in the row is a
		// distinct suite member. A defensive recount would describe a state the
		// checks above have already made unreachable.
		short = append(short, fmt.Sprintf("%s/%s (%d/%d cases, %s)",
			model, persona, len(cov.CaseIDs), len(suite), describeMissing(missing, failed, slotFailed[key])))
		// The RAW key, because slotFailed is indexed by it. Stripping happens where the
		// pair is rendered below, not here — a stripped key would silently miss its
		// slotFailed entry for any identity carrying a control rune, which is exactly
		// the case where a wrong answer matters.
		shortKeys = append(shortKeys, key)
	}

	// The join is checked in BOTH directions: a coverage row no reviewer row
	// consumed is silently discarded by the loop above, so a row citing cases the
	// suite never saw would export at exit 0 with no warning. A file whose two
	// arrays disagree on which reviewers exist is malformed by the same argument
	// as the forward direction — and rejecting leftovers before the short-coverage
	// diagnostics below means every later message can trust the join.
	for _, c := range rr.Coverage {
		key := coverageKey(c.Model, c.Persona)
		if _, ok := consumed[key]; !ok {
			return fmt.Errorf("run-result %s records coverage for %s/%s with no matching reviewer row; "+
				"the producer writes the two arrays from the same accumulator, so this file is malformed",
				path, stripTerminalControlRunes(c.Model), stripTerminalControlRunes(c.Persona))
		}
	}

	if len(short) == 0 {
		return nil
	}

	// The SLOT caveat, computed once for both exits below. It applies only when a row
	// is short because a reviewer was not shown a case — not when the whole panel lost
	// one. That distinction is the point: a case-level shortfall hits every row
	// equally, so the rows stay comparable to EACH OTHER and only the suite is short,
	// while a slot-level one makes one row's denominator differ from its peers'.
	var slotShortRows []string
	for _, k := range shortKeys {
		if len(slotFailed[k]) > 0 {
			slotShortRows = append(slotShortRows,
				stripTerminalControlRunes(k.model)+"/"+stripTerminalControlRunes(k.persona))
		}
	}

	if allowPartial {
		msg := fmt.Sprintf(
			"warning: publishing %s with partial coverage (--allow-partial-coverage): %s — "+
				partialCoverageVisibilityAdvisory+
				", but the rows still are not comparable to fully-covered ones.\n",
			path, strings.Join(short, "; "))
		// What the override is about to publish, stated plainly. corroboration_rate is
		// a mean over the cases a reviewer was SHOWN (score.go's ratedCases counts
		// r.Cases, which the slot skip already removed them from), so a slot-short row
		// is not penalised for what it missed and reads higher than a peer scored over
		// the full suite. That is the correct measurement of what the reviewer saw and
		// the wrong number to rank it by, and the envelope has no field that can say so
		// — slot_failures is run-result-only. An operator overriding the gate is owed
		// that sentence before the figure reaches a board.
		//
		// "Reads higher" is the direction, not a guarantee, and the note must not claim
		// otherwise: this branch fires on any non-empty slotFailed entry, including the
		// row that lost EVERY slot. Score returns early on len(r.Cases) == 0
		// (internal/benchmark/score.go:110), leaving corroboration_rate at its 0.00 zero
		// value — the floor, not an inflated figure. Telling that operator to discount
		// the row as flattering would be exactly backwards, so both ends are named.
		// The rate alone distinguishes nothing (a full-suite reviewer that matched
		// nothing also publishes 0.00), but runs and case_ids do: the row that lost
		// every slot is the only one with runs 0 and an empty covered set, and the
		// closing sentence points the operator at that shape rather than asserting
		// nothing on the submission carries it.
		if len(slotShortRows) > 0 {
			msg += fmt.Sprintf(
				"  note: %s lost individual reviewer slots, so each one's corroboration_rate is "+
					"averaged over only the cases that reviewer was shown and is not penalised for the rest. "+
					"It is not comparable to a row scored over the full suite: it reads higher where the "+
					"reviewer was shown some cases, and 0.00 where every slot failed and it was shown none. "+
					"The all-slots-lost row is distinguishable by its shape, not by the rate: runs 0 with an empty "+
					"case_ids array (runs is always published and a covered set is always an array).\n",
				strings.Join(slotShortRows, ", "))
		}
		_, _ = fmt.Fprint(w, msg)
		return nil
	}

	// The remedy is TIER-AWARE, because "re-run the missing or unmeasured cases"
	// presumes per-case re-running and repo-state-v1 has none: checkRepoStateFlags
	// refuses --checkpoint there, so the only re-run on offer is the entire paid
	// panel. An operator weighing that against --allow-partial-coverage has to know
	// which they are choosing between.
	remedy := "re-run the missing or unmeasured cases, or pass --allow-partial-coverage to publish the shortfall explicitly"
	if strings.EqualFold(rr.Suite, benchmark.FormatRepoStateV1) {
		remedy = "on " + benchmark.FormatRepoStateV1 + " there is no per-case resume (--checkpoint is refused for this tier), " +
			"so re-running re-pays the whole suite; weigh that against --allow-partial-coverage, which publishes the " +
			"shortfall explicitly"
	}
	if len(slotShortRows) > 0 {
		// A re-run is the wrong instruction for the slot half whatever the tier: the
		// case ran and the other reviewers scored it, so what needs investigating is
		// that reviewer's provider, not the suite.
		remedy += ". Re-running will not help the `unshown` cases — those ran and the rest of the panel scored them; " +
			"investigate the provider behind " + strings.Join(slotShortRows, ", ") + " instead"
	}
	return fmt.Errorf("run-result %s has reviewer row(s) scored over less than the full %d-case suite: %s; %s",
		path, len(suite), strings.Join(short, "; "), remedy)
}

// suiteAnchor is the only part of a suite manifest the denominator anchor consults:
// the identity pair and the case-id list. Both tiers have exactly these, which is
// what lets one anchor serve both without either loader's type crossing into this
// file.
type suiteAnchor struct {
	Suite        string
	SuiteVersion string
	CaseIDs      []string
}

// loadSuiteAnchor loads either tier's manifest, routing on the suite's own
// discriminator the way `benchmark run` and `benchmark verify` do.
//
// Anchoring used to call benchmark.Load unconditionally, which hard-rejects a
// repo-state suite — so `benchmark export --suite-path` could not anchor the tier
// export otherwise accepts. The new tier could publish to the same public board as
// standard-v1 while being permanently held to the weaker self-consistency gate, and
// the operator's only way past the error was to drop the flag, which downgrades the
// check silently. A denominator gate that one tier can never satisfy is not a gate.
func loadSuiteAnchor(suitePath string) (suiteAnchor, error) {
	format, err := benchmark.DetectSuiteFormat(suitePath)
	if err != nil {
		return suiteAnchor{}, err
	}
	if strings.EqualFold(format, benchmark.FormatRepoStateV1) {
		m, lerr := benchmark.LoadRepoState(suitePath)
		if lerr != nil {
			return suiteAnchor{}, lerr
		}
		a := suiteAnchor{Suite: m.Suite, SuiteVersion: m.SuiteVersion}
		for _, c := range m.Cases {
			a.CaseIDs = append(a.CaseIDs, c.ID)
		}
		return a, nil
	}
	m, lerr := benchmark.Load(suitePath)
	if lerr != nil {
		return suiteAnchor{}, lerr
	}
	a := suiteAnchor{Suite: m.Suite, SuiteVersion: m.SuiteVersion}
	for _, c := range m.Cases {
		a.CaseIDs = append(a.CaseIDs, c.ID)
	}
	return a, nil
}

// anchorSuiteDenominator ties the run-result's declared denominator to the suite
// manifest at suitePath, turning checkCoverage's internal-consistency check into the
// suite-coverage guarantee its doc describes.
//
// It is opt-in (`--suite-path`) rather than mandatory because export must keep
// accepting a run-result whose suite is not on the exporting machine — the file is
// portable and the suite tree is not.
//
// The three checks run in this order deliberately: suite identity first, so anchoring
// to the wrong suite reports the wrong suite rather than reporting every case as
// missing; then the presence of a denominator, since an unmeasured file has nothing to
// anchor and passing the flag must not read as a check that silently did nothing; then
//
// THAT FIRST GUARANTEE HOLDS ON standard-v1 ONLY. A repo-state suite's name is fixed
// by the format discriminator rather than chosen by its author, so the identity check
// is a tautology on that tier and a wrong-suite anchor falls through to the case-set
// check. The case-set diagnostics below carry a caveat naming that second possible
// cause rather than asserting the run was truncated; see the comment beside them.
// the case list itself, compared as a SET in both directions — a missing id is the
// truncation this exists to catch, and an EXTRA id is a denominator inflated past what
// the suite can support.
func anchorSuiteDenominator(rr benchmark.RunResult, suitePath, path string) error {
	m, err := loadSuiteAnchor(suitePath)
	if err != nil {
		return fmt.Errorf("loading suite %s to anchor %s: %w", suitePath, path, err)
	}
	if m.Suite != rr.Suite || m.SuiteVersion != rr.SuiteVersion {
		// rr.Suite and rr.SuiteVersion are untrusted for the same reason the case ids
		// and the reviewer identities are — they are read straight from the
		// operator-supplied run-result, and export is where a hand-supplied file first
		// enters the tool.
		//
		// %q, NOT stripTerminalControlRunes: this message's entire job is to show a
		// DIFFERENCE, and stripping sanitizes by deletion. The gate above is a raw !=
		// with no trimming, and unicode.IsControl covers \r and \n as well as ESC, so a
		// CRLF-mangled run-result whose suite genuinely differs would render both sides
		// as the same text — a self-contradicting message that reads as a spurious
		// failure. %q gives the same terminal safety (a control rune becomes a visible
		// escape) while keeping the difference legible, which is exactly why the id
		// sites in validateCoveredSet already use it.
		//
		// BOTH halves take %q. Rendering the untrusted side under one rule and the
		// manifest side under another is what let the difference disappear; quoting the
		// manifest value also disambiguates a suite name with leading/trailing spaces.
		return fmt.Errorf("run-result %s is for suite %q/%q but the manifest at %s is %q/%q; "+
			"anchoring a run-result to a different suite compares two unrelated case lists",
			path, rr.Suite, rr.SuiteVersion,
			suitePath, m.Suite, m.SuiteVersion)
	}
	if len(rr.SuiteCaseIDs) == 0 {
		return fmt.Errorf("run-result %s records no suite_case_ids, so there is nothing to anchor "+
			"against the suite manifest at %s; drop --suite-path to publish it as unmeasured, "+
			"or re-run `atcr benchmark run` to produce a run-result that records coverage",
			path, suitePath)
	}

	// Both directions are compared, and the declared list is deduped on the way in so
	// a repeated id cannot pad the set — checkCoverage rejects that shape too, but this
	// function runs first and must not be the looser of the two.
	declared := make(map[string]bool, len(rr.SuiteCaseIDs))
	for _, id := range rr.SuiteCaseIDs {
		declared[id] = true
	}
	manifestIDs := make(map[string]bool, len(m.CaseIDs))
	var missing []string
	for _, id := range m.CaseIDs {
		manifestIDs[id] = true
		if !declared[id] {
			missing = append(missing, id)
		}
	}
	var extra []string
	for id := range declared {
		if !manifestIDs[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	// On repo-state-v1 the identity check above cannot have told these two suites
	// apart. LoadRepoState requires the manifest to declare exactly
	// FormatRepoStateV1 and then stamps that constant onto what it returns, so every
	// repo-state manifest and run-result carries the same literal — unlike
	// standard-v1, whose suite name is author-chosen and therefore distinguishing.
	// The documented ordering guarantee ("anchoring to the wrong suite reports the
	// wrong suite rather than reporting every case as missing") does not hold here,
	// so a case-set difference on this tier has TWO possible causes and the message
	// must not pick one. Blaming the denominator asserts the run was truncated, which
	// the file gives no evidence for; the likelier cause is a run-result anchored to
	// a different repo-state suite that happens to share its suite_version.
	//
	// validateSuiteIdentityForPublication names the same hazard one level up: two
	// different suites publishing a byte-identical (suite, suite_version) merge into
	// one comparability bucket on the public board.
	tierCaveat := ""
	if strings.EqualFold(rr.Suite, benchmark.FormatRepoStateV1) {
		tierCaveat = "; on " + benchmark.FormatRepoStateV1 + " the suite name is not author-distinguishable " +
			"(every manifest declares the same literal), so this may equally mean the run-result was anchored " +
			"to a DIFFERENT repo-state suite of the same suite_version rather than that it is short"
	}

	switch {
	case len(missing) > 0 && len(extra) > 0:
		return fmt.Errorf("run-result %s declares a %d-case suite but the suite manifest at %s has %d: "+
			"missing %s; not in the suite: %s%s",
			path, len(declared), suitePath, len(m.CaseIDs), summarizeMissing(missing), summarizeMissing(extra), tierCaveat)
	case len(missing) > 0:
		// The denominator claim is made ONLY where it is warranted: on a tier whose
		// suite name already proved the two files describe the same suite.
		cause := "; every reviewer row was therefore scored against a shrunken denominator"
		if tierCaveat != "" {
			cause = tierCaveat
		}
		return fmt.Errorf("run-result %s declares a %d-case suite but the suite manifest at %s has %d, "+
			"missing %s%s",
			path, len(declared), suitePath, len(m.CaseIDs), summarizeMissing(missing), cause)
	case len(extra) > 0:
		return fmt.Errorf("run-result %s declares case(s) the suite manifest at %s does not contain: %s%s",
			path, suitePath, summarizeMissing(extra), tierCaveat)
	}
	return nil
}

// validateCoveredSet rejects a coverage row that is not a subset-without-repeats of
// the suite. Both shapes are impossible from the producer — it appends one case id
// per fold, from the manifest — so either one means the file was assembled by hand,
// and both inflate a row's apparent coverage past what the suite can support.
//
// On success it returns the suite case ids the row still owes, sorted so the
// diagnostic is deterministic. The membership map it must build anyway doubles as
// the missing-id lookup, so the gate builds ONE map per row rather than two — a
// caller-supplied file can carry thousands of rows, and the per-row constant factor
// is the only cost this function controls.
//
// PRECONDITION: model and persona arrive already stripped of terminal control runes.
// Both errors below are operator-facing, and checkCoverage strips the identity ONCE per
// row rather than at each of its own nine interpolation sites — re-stripping here would
// add a per-row scan to buy nothing. A future caller must strip before calling.
func validateCoveredSet(suite map[string]bool, covered []string, path, model, persona string) ([]string, error) {
	seen := make(map[string]bool, len(covered))
	for _, id := range covered {
		if seen[id] {
			return nil, fmt.Errorf("run-result %s: reviewer %s/%s lists case %q more than once in its coverage; "+
				"a case is scored at most once per reviewer, so this file is malformed", path, model, persona, id)
		}
		seen[id] = true
		if !suite[id] {
			return nil, fmt.Errorf("run-result %s: reviewer %s/%s records case %q, which is not one of the "+
				"%d cases in this suite", path, model, persona, id, len(suite))
		}
	}
	var missing []string
	for id := range suite {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

// summarizeMissing renders up to maxNamedMissingCases ids, then an overflow count.
//
// It strips terminal control runes from every id it names. Case ids are untrusted for
// the same reason the reviewer identity is — they come from the run-result being
// validated, and export is where a hand-supplied file first enters the tool.
//
// It is the route for EXPORT's capped diagnostics — both of checkCoverage's shortfall
// messages (the rejection and the --allow-partial-coverage warning) and all three of
// anchorSuiteDenominator's — and sanitizing here rather than at those five call sites
// is what keeps the guarantee checkable for them: a further capped diagnostic that
// names ids has to come through this function to get the cap, so it inherits the
// stripping with it.
//
// It is NOT, however, the only place a case id reaches the terminal under %s.
// warnCaseFailures (cli/benchmark_repostate.go) prints the failed cases on the RUN
// path and strips independently with the same stripTerminalControlRunes call. It
// applies its own cap (maxNamedFailedCases) rather than inheriting this one, so the
// two sites now share BOTH rules while sharing no code. Two sites applying one rule is
// the accurate statement; claiming a single choke point would leave the next author of
// a run-path diagnostic believing the stripping came for free.
//
// The id sites inside validateCoveredSet are deliberately left alone — they use %q,
// which already renders a control rune as a literal escape sequence.
func summarizeMissing(missing []string) string {
	named := missing
	if len(named) > maxNamedMissingCases {
		named = named[:maxNamedMissingCases]
	}
	safe := make([]string, len(named))
	for i, id := range named {
		safe[i] = stripTerminalControlRunes(id)
	}
	if len(missing) <= maxNamedMissingCases {
		return strings.Join(safe, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(safe, ", "), len(missing)-maxNamedMissingCases)
}

// describeMissing splits one row's shortfall into the three things it can be, and
// says which.
//
// They call for different responses, and each label names a different actor:
//
//   - `unmeasured` — the CASE never ran, for anybody (rr.CaseFailures). Whether a
//     re-run helps depends on the reason, which is why the reason is printed.
//   - `unshown` — the case ran and the rest of the panel scored it; THIS reviewer
//     was not shown it (rr.SlotFailures). Re-running the case would not have helped
//     the other reviewers, who already have it.
//   - `missing` — accounted for by neither: the row claims a suite it was not scored
//     over. That is the shape the original "re-run the missing cases" instruction was
//     written for, and before the slot skip existed it was reachable only by
//     tampering.
//
// Collapsing any pair loses the distinction that picks the remedy. Labelling a slot
// shortfall `missing` told an operator to re-run cases that ran perfectly and made one
// flaky provider read as a hand-assembled file; labelling it `unmeasured` would claim
// the case never ran while the surviving reviewers' rows visibly contain it.
//
// All three halves route through summarizeMissing, so each inherits the per-row cap and
// the control-rune stripping described there rather than re-deriving them — the
// reason is stripped with the id it is composed onto.
//
// Each half is capped independently, so the bound is PER HALF and a row carrying all
// three kinds of shortfall names up to 3*maxNamedMissingCases ids with three overflow
// counts — see maxNamedMissingCases for why that is deliberate rather than an oversight.
//
// PRECONDITION: missing is non-empty. An empty slice returns "", which the caller
// composes into `m/p (2/3 cases, )` — a shortfall message with a blank explanation.
// checkCoverage is the only caller and reaches this line only past its own
// `len(missing) == 0` continue, so the condition holds by construction rather than by
// a check here.
//
// PRECONDITION: every reason in failed already satisfies
// benchmark.ValidCaseFailureReason, and every reason in slotFailed satisfies
// benchmark.ValidSlotFailureReason. Both filters live in the CALLER, not in this
// signature, so a second caller that builds either map itself would print an
// attacker-chosen string as an explanation for a case. Terminal safety survives it —
// summarizeMissing strips the composed string — but truthfulness does not.
// validateCoveredSet states its own equivalent contract the same way, and this
// function owes the same statement.
//
// PRECONDITION: slotFailed holds only THIS row's reviewer. It is an
// identity-scoped projection, and passing the whole run's slot failures would attach
// one reviewer's excuse to another's short row.
func describeMissing(missing []string, failed, slotFailed map[string]string) string {
	var unexplained, unmeasured, unshown []string
	for _, id := range missing {
		// Case-level first: a case NOBODY was shown is the stronger statement, and the
		// producer cannot record both for one (reviewer, case) pair — a failed case is
		// skipped before the per-agent loop that records slot failures ever runs. The
		// order therefore only settles a hand-assembled file, where naming the wider
		// fault is the honest reading.
		if reason, ok := failed[id]; ok {
			unmeasured = append(unmeasured, fmt.Sprintf("%s (%s)", id, reason))
			continue
		}
		if reason, ok := slotFailed[id]; ok {
			unshown = append(unshown, fmt.Sprintf("%s (%s)", id, reason))
			continue
		}
		unexplained = append(unexplained, id)
	}
	var parts []string
	if len(unexplained) > 0 {
		parts = append(parts, "missing "+summarizeMissing(unexplained))
	}
	if len(unmeasured) > 0 {
		parts = append(parts, "unmeasured "+summarizeMissing(unmeasured))
	}
	if len(unshown) > 0 {
		parts = append(parts, "unshown "+summarizeMissing(unshown))
	}
	// " / ", not "; ": checkCoverage joins distinct short ROWS with "; ", and a
	// multi-short-row run is the normal case on a large roster. Using one delimiter at
	// two nesting levels fragments a single reviewer row into two for a reader — and
	// for anything downstream that splits the message on it.
	return strings.Join(parts, halfSeparator)
}

// firstNonPrintingRune reports the first control (Cc) or format (Cf) rune in s —
// the same predicate stripTerminalControlRunes applies to operator-facing
// diagnostics. In validateScrubbedCaseIDs it is a REJECTION, not a sanitization:
// these runes must not reach the published document at all. A U+202E flips the
// rendering of the id and everything after it in the same text node on the board,
// and a zero-width rune makes two different ids render identically, defeating the
// documented SET comparison at the human layer even while it holds programmatically.
func firstNonPrintingRune(s string) (rune, bool) {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return r, true
		}
	}
	return 0, false
}

// validateScrubbedCaseIDs rejects a run-result whose case ids do not survive
// publication intact. It exists because submission_schema 2 made the case ids a
// PUBLISHED field while every other check in this file still validates the raw ones.
//
// The governing check is that the published id NAMES THE SAME CASE as the raw one:
// any id the scrub rewrites is rejected. That single check subsumes the two failure
// shapes below, both of which produce a document the coverage gate approved but
// never actually examined:
//
//   - An id that scrubs to empty publishes as "" — a rewrite to the empty string.
//     Genuinely producible: the bundled importer builds ids as
//     <owner>-<repo>-pr-<n>, which a credential rule can consume whole.
//
//   - Two distinct raw ids that scrub to the SAME value publish a denominator with
//     a repeated entry. Under the documented SET comparison of a row's case_ids
//     against suite_case_ids, a short row then reads as fully covered — defeating
//     the reason coverage is carried at all. But two DISTINCT raw ids reaching one
//     published id means at least one was rewritten, so the rewrite check fires
//     first and this shape has no reachable diagnostic of its own.
//
// Ahead of both, a printability ban rejects any id carrying a control (Cc) or
// format (Cf) rune — the same predicate stripTerminalControlRunes applies to
// diagnostics, enforced here for the document that ships.
//
// What survives below the rewrite check: a literally EMPTY raw id (no rewrite, yet
// publishes as ""), and a verbatim-repeated id, which is deferred to checkCoverage's
// sharper "more than once" diagnostic — the raw duplicate is the plainer defect and
// the actionable one.
//
// Row ids are checked only when a denominator EXISTS: a coverage array with no
// suite_case_ids is already destined for checkCoverage's sharper structural
// rejection ("records reviewer coverage but no suite_case_ids"), and a per-id scrub
// diagnostic would pre-empt it with a privacy message about a file whose real
// defect is its shape.
//
// Errors name the PRE-scrub id. The scrubbed value is empty or rewritten by
// construction, so reporting it would tell the operator what went wrong but never
// which line of their file to fix.
func validateScrubbedCaseIDs(rr benchmark.RunResult, path string) error {
	// Memoized: covered ids are typically a subset of the suite ids, so an
	// unmemoized closure scrubs the whole case set once per loop — and once per
	// row — over again. BuildSubmission re-scrubs for publication with its own
	// memo; this gate's job is only to validate, so one map serves both loops.
	scrubMemo := make(map[string]string, len(rr.SuiteCaseIDs))
	scrubCaseID := func(s string) string {
		v, ok := scrubMemo[s]
		if !ok {
			v = scorecard.ScrubPublicString(s)
			scrubMemo[s] = v
		}
		return v
	}

	for _, id := range rr.SuiteCaseIDs {
		if r, bad := firstNonPrintingRune(id); bad {
			return fmt.Errorf("run-result %s lists suite case %q, which contains a non-printing rune (U+%04X); "+
				"control and format runes are invisible or reorder text in the published document, "+
				"so rename the case in the suite manifest",
				path, id, r)
		}
		// No TrimSpace: scrubOnce ends with strings.Join(strings.Fields(s), " "),
		// so the scrubbed value can never carry leading or trailing whitespace.
		s := scrubCaseID(id)
		if s != id {
			return fmt.Errorf("run-result %s lists suite case %q, which the publication scrub rewrites to %q; "+
				"the published suite_case_ids must name the same cases as the raw file, "+
				"so rename the case in the suite manifest",
				path, id, s)
		}
		// Reachable only for a literally empty raw id: anything the scrub empties
		// is a rewrite, caught above, and a verbatim repeat defers to
		// checkCoverage's "lists suite case %q more than once" — the plainer
		// defect with the actionable remedy (delete a line, not hunt a privacy
		// rule).
		if s == "" {
			return fmt.Errorf("run-result %s lists suite case %q, which is empty once scrubbed for publication; "+
				"a case id that scrubs away publishes as \"\" in suite_case_ids, "+
				"so rename the case in the suite manifest",
				path, id)
		}
	}

	for _, c := range rr.Coverage {
		if len(rr.SuiteCaseIDs) == 0 {
			break
		}
		for _, id := range c.CaseIDs {
			if r, bad := firstNonPrintingRune(id); bad {
				return fmt.Errorf("run-result %s records covered case %q for %s/%s, which contains "+
					"a non-printing rune (U+%04X); rename the case in the suite manifest",
					path, id,
					stripTerminalControlRunes(c.Model), stripTerminalControlRunes(c.Persona), r)
			}
			s := scrubCaseID(id)
			if s != id {
				return fmt.Errorf("run-result %s records covered case %q for %s/%s, which the publication scrub "+
					"rewrites to %q; rename the case in the suite manifest",
					path, id,
					stripTerminalControlRunes(c.Model), stripTerminalControlRunes(c.Persona), s)
			}
			if s == "" {
				return fmt.Errorf("run-result %s records covered case %q for %s/%s, which is empty once scrubbed "+
					"for publication; a case id that scrubs away publishes as \"\" in reviewer_coverage, "+
					"so rename the case in the suite manifest",
					path, id,
					stripTerminalControlRunes(c.Model), stripTerminalControlRunes(c.Persona))
			}
		}
	}
	return nil
}

// validateSuiteIdentityForPublication applies validateScrubbedCaseIDs' predicate to
// the identity that names the WHOLE document.
//
// suite/suite_version are published scrubbed (BuildSubmission), and the gate that
// used to sit here rejected only the value that scrubs to EMPTY. Two arms were
// missing, and both matter more here than they do for a single case id:
//
//   - A value the scrub REWRITES publishes under a different name than the one
//     anchorSuiteDenominator validated against the manifest — that check compares the
//     PRE-scrub value. Two genuinely different suites can then publish a
//     byte-identical (suite, suite_version) and the board merges them into one
//     comparability bucket. A plain alphanumeric name is enough to trigger it:
//     "team-suite bench@acme" scrubs to "team-suite".
//   - The scrub provably does NOT strip control or format runes, so a bidi override
//     in a suite name reaches the published envelope intact.
//
// Printability is checked first — it is the defect a reader cannot see. Empty is
// checked BEFORE rewrite here, unlike the case-id gate: scrubbing away is also a
// rewrite, and "publishes as \"\"" is the sharper diagnostic for it. The case-id gate
// can order them the other way because a case id that scrubs to empty is reported by
// its rewrite arm with the empty result named in the message anyway.
func validateSuiteIdentityForPublication(rr benchmark.RunResult, path string) error {
	for _, f := range []struct{ name, value string }{
		{"suite", rr.Suite},
		{"suite_version", rr.SuiteVersion},
	} {
		if r, bad := firstNonPrintingRune(f.value); bad {
			return fmt.Errorf("run-result %s has %s %q, which contains a non-printing rune (U+%04X); "+
				"control and format runes are invisible or reorder text in the published document, "+
				"so rename the suite in the manifest",
				path, f.name, f.value, r)
		}
		// No TrimSpace: scrubOnce ends with strings.Join(strings.Fields(s), " "),
		// so the scrubbed value can never carry leading or trailing whitespace.
		s := scorecard.ScrubPublicString(f.value)
		if s == "" {
			return fmt.Errorf("run-result %s has %s %q, which is empty once scrubbed for publication; "+
				"a suite identity that scrubs away publishes as \"\" in the envelope",
				path, f.name, f.value)
		}
		if s != f.value {
			return fmt.Errorf("run-result %s has %s %q, which the publication scrub rewrites to %q; "+
				"the published envelope must name the same suite the manifest does — "+
				"anchorSuiteDenominator validates the PRE-scrub value, so two different suites "+
				"would publish one identity; rename the suite in the manifest",
				path, f.name, f.value, s)
		}
	}
	return nil
}

// validateCaseFailures is the EXPORT trust boundary for the per-case failure
// channel — the only live one for this tier, since checkRepoStateFlags refuses
// --checkpoint for repo-state-v1 and there is no resume path to guard.
//
// It exists because the array is CONSEQUENTIAL at the gate below: checkCoverage
// reads it to explain a coverage shortfall, so an unvalidated entry is a way to
// attach an excuse to a row that never earned one. A run-result reaches export
// hand-suppliable, having never passed through the producer, which is the same
// premise every other check in this file is built on.
//
// The four rejections are the four ways an entry can be MALFORMED: a reason outside
// the vocabulary (including the empty one — a failure record always states its
// cause), a case the suite does not declare, a case some reviewer also scored, and a
// case named twice. Each is impossible from the producer, which records each failed
// case once, with a constant, before the case can be scored.
//
// WHAT THIS DOES NOT PROVE. It establishes that an entry is well-formed and
// internally consistent, NOT that the case really failed. A valid reason paired with
// a real, unscored case id passes every arm, so a truncated run can still be
// relabelled from "missing" to "unmeasured" in the diagnostic below. That gap is not
// closeable from inside the file being validated — only the producer knows what ran
// — so it is stated rather than papered over, in docs/benchmark.md as well as here.
//
// NOT TIER-SCOPED, DELIBERATELY. The arms above accept case_failures on ANY
// run-result, including a standard-v1 one whose producer never writes the array — so
// a hand-edited standard-v1 file can carry, say, a `materialize` reason for a pipeline
// that has no materialize stage. That is tolerated on three grounds. First, the entry
// still has to name a declared, unscored, unrepeated case with a vocabulary reason, so
// the claim it can make is "this case was not measured" — true of a case absent from
// every coverage row whatever tier produced the file, and already covered by the
// paragraph above. Second, the run-result's only tier discriminator is UNTRUSTED at
// this boundary. `suite` is a real one — a repo-state manifest must declare the
// literal `repo-state-v1` (internal/benchmark/repostate.go:181), and checkCoverage
// already routes its remedy on it — but it is a field of the same hand-suppliable file
// the arm would be policing, so anyone editing in a `materialize` reason edits the
// discriminator beside it. (ReviewerCoverage.GroundingEnabled is NOT the alternative:
// standard-v1 publishes it too, as `false`, since cli/benchmark_run.go:451 carries up
// the gate state of a range-less run that failed open.) Third, the header's own premise
// — that the export boundary is the only live one — is what makes this a
// diagnostic-quality question rather than a resume-safety one. Add the arm only
// alongside a tier discriminator this file cannot restate about itself.
func validateCaseFailures(rr benchmark.RunResult, path string) error {
	if len(rr.CaseFailures) == 0 {
		return nil
	}
	// A failure array with NO denominator gets its own rejection rather than the
	// membership arm's. Without it every entry fails `suite[f.CaseID]` and the message
	// blames the case id for a file whose actual defect is the absent suite_case_ids —
	// and the mirror shape one field over already has a sharper rejection (checkCoverage
	// rejects "records reviewer coverage but no suite_case_ids"). It also closes the arm
	// on which the channel was unvalidatable: checkCoverage returns early on a file with
	// neither field, so nothing downstream reads the array either, and a hand-edited
	// pre-coverage file could carry an arbitrary one through export with no gate firing.
	if len(rr.SuiteCaseIDs) == 0 {
		return fmt.Errorf("run-result %s records case_failures but no suite_case_ids; "+
			"a failure names a case of the declared suite, and `atcr benchmark run` writes the two together, "+
			"so this file is malformed", path)
	}
	// An impossible SHAPE is rejected before any index is built, the way the sibling
	// validators in this file reject one rather than merely iterating it. Every entry
	// must name a distinct declared case (the membership and repeat arms below), so an
	// array longer than the declared suite cannot be well-formed whatever it contains —
	// and the declared count is the bound rather than the distinct one, so a repeated
	// suite id still reaches checkCoverage's sharper duplicate diagnostic instead of
	// being preempted here.
	if len(rr.CaseFailures) > len(rr.SuiteCaseIDs) {
		return fmt.Errorf("run-result %s records %d case_failures entries over a %d-case suite; "+
			"the producer records each failed case at most once, so this file is malformed",
			path, len(rr.CaseFailures), len(rr.SuiteCaseIDs))
	}
	suite := make(map[string]bool, len(rr.SuiteCaseIDs))
	for _, id := range rr.SuiteCaseIDs {
		suite[id] = true
	}
	// A case any reviewer scored cannot also be unmeasured. Built from every
	// coverage row rather than the first: the contradiction is worth catching
	// wherever it appears, and the producer writes one identical covered set per
	// fully-scored row anyway.
	scored := map[string]bool{}
	for _, c := range rr.Coverage {
		for _, id := range c.CaseIDs {
			scored[id] = true
		}
	}
	seen := make(map[string]bool, len(rr.CaseFailures))
	for _, f := range rr.CaseFailures {
		// Stripped for the same reason every identity in this file is: these
		// diagnostics reach the operator's terminal, and a case id is untrusted
		// input here.
		id := stripTerminalControlRunes(f.CaseID)
		if !benchmark.ValidCaseFailureReason(f.Reason) {
			return fmt.Errorf("run-result %s records case_failures reason %q for case %q, outside the failure vocabulary; "+
				"the producer writes only benchmark.CaseFailure* values, so this file is malformed",
				path, stripTerminalControlRunes(f.Reason), id)
		}
		// A blank id names nothing, so the membership arm below would report it as
		// `an entry for "", which suite_case_ids does not declare` — a sentence that
		// blames the denominator for an entry that never identified a case at all.
		// This is the shape a partly-populated hand edit produces, the same one the
		// empty reason is rejected for one field up.
		if strings.TrimSpace(f.CaseID) == "" {
			return fmt.Errorf("run-result %s records a case_failures entry with a blank case_id; "+
				"a failure record names the case it is about, so this file is malformed", path)
		}
		// A non-printing rune gets its OWN arm, before the membership arm below, because
		// every comparison here is on the RAW id while every message prints the stripped
		// one. validateScrubbedCaseIDs guarantees no suite_case_ids entry carries such a
		// rune, so "case-02​" can never match — and the membership arm would then
		// report `an entry for "case-02", which suite_case_ids does not declare` about a
		// file whose suite_case_ids visibly contains case-02. Naming the rune is the rule
		// duplicateIdentityError and anchorSuiteDenominator already state for the same
		// class of message: a difference-showing diagnostic must show the difference.
		if r, bad := firstNonPrintingRune(f.CaseID); bad {
			return fmt.Errorf("run-result %s records a case_failures entry for case %q carrying a non-printing rune (U+%04X); "+
				"the producer records the suite's own case ids, which cannot contain one, so this file is malformed",
				path, id, r)
		}
		if !suite[f.CaseID] {
			return fmt.Errorf("run-result %s records a case_failures entry for %q, which suite_case_ids does not declare; "+
				"a failure naming a case outside the suite explains no shortfall in it, so this file is malformed",
				path, id)
		}
		if scored[f.CaseID] {
			return fmt.Errorf("run-result %s records case %q as both scored and infrastructure-failed; "+
				"the producer skips a failed case before scoring it, so this file is malformed",
				path, id)
		}
		if seen[f.CaseID] {
			return fmt.Errorf("run-result %s records case %q in case_failures more than once; "+
				"the producer records each failed case exactly once, so this file is malformed",
				path, id)
		}
		seen[f.CaseID] = true
	}
	return nil
}

// validateSlotFailures is the export trust boundary for the per-SLOT failure channel,
// the role validateCaseFailures plays one level up.
//
// It exists on the same premise: a run-result reaches export hand-suppliable, having
// never passed through the producer, and this array is CONSEQUENTIAL — the shortfall
// diagnostic reads it to explain why a reviewer row is short, so an unvalidated entry
// is a way to attach an excuse to a row that never earned one, and to interpolate an
// attacker-chosen string into an operator's terminal.
//
// The arms are the ways an entry can be MALFORMED, each impossible from the producer,
// which writes one record per (reviewer, case) with a constant reason at the moment
// it skips the slot:
//
//   - a reason outside the vocabulary, the empty one included
//   - a blank or non-printing case id or identity
//   - a case the suite does not declare
//   - a case that same reviewer's coverage row also claims to have scored
//   - the same (reviewer, case) pair twice
//
// WHAT IT DOES NOT PROVE, stated for the same reason its sibling states it: that an
// entry is well-formed is not that the slot really failed. A valid reason on a real,
// unscored (reviewer, case) pair passes every arm. Only the producer knows what ran.
//
// The identity is matched against the COVERAGE rows rather than the reviewer rows
// because coverage is what carries the covered set this record explains, and the two
// are already cross-checked in both directions by checkCoverage.
func validateSlotFailures(rr benchmark.RunResult, path string) error {
	if len(rr.SlotFailures) == 0 {
		return nil
	}
	// The no-denominator rejection its sibling carries, for the same reason: without
	// it every entry fails the membership arm and the message blames the case id for a
	// file whose actual defect is the absent suite_case_ids.
	if len(rr.SuiteCaseIDs) == 0 {
		return fmt.Errorf("run-result %s records slot_failures but no suite_case_ids; "+
			"a slot failure names a case of the declared suite, and `atcr benchmark run` writes the two together, "+
			"so this file is malformed", path)
	}
	suite := make(map[string]bool, len(rr.SuiteCaseIDs))
	for _, cid := range rr.SuiteCaseIDs {
		suite[cid] = true
	}
	// Per-identity covered sets: a slot failure says THIS reviewer did not get this
	// case, so the contradiction is with that reviewer's own row, not with any row.
	//
	// Keyed on the RAW pair, not the scrubbed one — this map and its lookup below both
	// build reviewerKey directly, where checkCoverage re-scrubs through coverageKey. The
	// two agree in practice because the producer writes both arrays from one already-
	// scrubbed accumulator, and where they would not, this join fails CLOSED: a raw pair
	// that only collides after scrubbing misses its covered set and is rejected by the
	// no-reviewer_coverage-row arm. Nothing upstream makes them agree by construction,
	// though — runBenchmarkExport (cli/benchmark.go:454) checks only that the scrubbed
	// identity is non-empty and printable, never that it is scrub-STABLE. Switching both
	// sites to coverageKey would close the gap; correcting the claim is the smaller step
	// and the one taken here.
	covered := map[reviewerKey]map[string]bool{}
	for _, c := range rr.Coverage {
		k := reviewerKey{model: c.Model, persona: c.Persona}
		if covered[k] == nil {
			covered[k] = map[string]bool{}
		}
		for _, cid := range c.CaseIDs {
			covered[k][cid] = true
		}
	}
	seen := map[string]bool{}
	for _, sf := range rr.SlotFailures {
		// Stripped for display for the same reason every identity in this file is;
		// every COMPARISON below stays on the raw value, which is why the rune arm
		// has to fire before the membership arms.
		id := stripTerminalControlRunes(sf.CaseID)
		model := stripTerminalControlRunes(sf.Model)
		persona := stripTerminalControlRunes(sf.Persona)

		if !benchmark.ValidSlotFailureReason(sf.Reason) {
			return fmt.Errorf("run-result %s records slot_failures reason %q for %s/%s on case %q, outside the failure "+
				"vocabulary; the producer writes only benchmark.SlotFailure* values, so this file is malformed",
				path, stripTerminalControlRunes(sf.Reason), model, persona, id)
		}
		if strings.TrimSpace(sf.CaseID) == "" {
			return fmt.Errorf("run-result %s records a slot_failures entry with a blank case_id; "+
				"a failure record names the case it is about, so this file is malformed", path)
		}
		if strings.TrimSpace(sf.Model) == "" || strings.TrimSpace(sf.Persona) == "" {
			return fmt.Errorf("run-result %s records a slot_failures entry for case %q with a blank model or persona; "+
				"a slot failure names the reviewer whose row it explains, so this file is malformed", path, id)
		}
		for _, f := range []struct{ name, value string }{
			{"case_id", sf.CaseID}, {"model", sf.Model}, {"persona", sf.Persona},
		} {
			if r, bad := firstNonPrintingRune(f.value); bad {
				return fmt.Errorf("run-result %s records a slot_failures entry whose %s %q carries a non-printing rune "+
					"(U+%04X); the producer records the run's own scrubbed identities and suite case ids, which cannot "+
					"contain one, so this file is malformed",
					path, f.name, stripTerminalControlRunes(f.value), r)
			}
		}
		if !suite[sf.CaseID] {
			return fmt.Errorf("run-result %s records a slot_failures entry for %q, which suite_case_ids does not declare; "+
				"a failure naming a case outside the suite explains no shortfall in it, so this file is malformed",
				path, id)
		}
		k := reviewerKey{model: sf.Model, persona: sf.Persona}
		if _, ok := covered[k]; !ok {
			return fmt.Errorf("run-result %s records a slot_failures entry for %s/%s, which has no reviewer_coverage row; "+
				"a slot failure explains why one row is short, so a record naming no row explains nothing and this file "+
				"is malformed", path, model, persona)
		}
		if covered[k][sf.CaseID] {
			return fmt.Errorf("run-result %s records case %q as both scored by and slot-failed for %s/%s; "+
				"the producer skips a failed slot before scoring it, so this file is malformed",
				path, id, model, persona)
		}
		pair := sf.Model + "\x00" + sf.Persona + "\x00" + sf.CaseID
		if seen[pair] {
			return fmt.Errorf("run-result %s records %s/%s on case %q in slot_failures more than once; "+
				"the producer records each failed slot exactly once, so this file is malformed",
				path, model, persona, id)
		}
		seen[pair] = true
	}
	return nil
}
