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
		// producer's own collision check (benchmark_run.go:267-273) uses the same
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
const maxNamedMissingCases = 3

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
			if k != benchmark.OutcomeUnknownLabel && !benchmark.ValidOutcome(k) {
				return fmt.Errorf("run-result %s records outcome tally key %q for %s/%s, outside the outcome vocabulary; "+
					"the producer writes only benchmark.Outcome* values, so this file is malformed",
					path, k, model, persona)
			}
		}
		byIdentity[key] = c
	}

	var short []string
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
		short = append(short, fmt.Sprintf("%s/%s (%d/%d cases, missing %s)",
			model, persona, len(cov.CaseIDs), len(suite), summarizeMissing(missing)))
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
	if allowPartial {
		_, _ = fmt.Fprintf(w,
			"warning: publishing %s with partial coverage (--allow-partial-coverage): %s — "+
				partialCoverageVisibilityAdvisory+
				", but the rows still are not comparable to fully-covered ones.\n",
			path, strings.Join(short, "; "))
		return nil
	}
	return fmt.Errorf("run-result %s has reviewer row(s) scored over less than the full %d-case suite: %s; "+
		"re-run the missing cases, or pass --allow-partial-coverage to publish the shortfall explicitly",
		path, len(suite), strings.Join(short, "; "))
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
// the case list itself, compared as a SET in both directions — a missing id is the
// truncation this exists to catch, and an EXTRA id is a denominator inflated past what
// the suite can support.
func anchorSuiteDenominator(rr benchmark.RunResult, suitePath, path string) error {
	m, err := benchmark.Load(suitePath)
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
	manifestIDs := make(map[string]bool, len(m.Cases))
	var missing []string
	for _, c := range m.Cases {
		manifestIDs[c.ID] = true
		if !declared[c.ID] {
			missing = append(missing, c.ID)
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

	switch {
	case len(missing) > 0 && len(extra) > 0:
		return fmt.Errorf("run-result %s declares a %d-case suite but the suite manifest at %s has %d: "+
			"missing %s; not in the suite: %s",
			path, len(declared), suitePath, len(m.Cases), summarizeMissing(missing), summarizeMissing(extra))
	case len(missing) > 0:
		return fmt.Errorf("run-result %s declares a %d-case suite but the suite manifest at %s has %d, "+
			"missing %s; every reviewer row was therefore scored against a shrunken denominator",
			path, len(declared), suitePath, len(m.Cases), summarizeMissing(missing))
	case len(extra) > 0:
		return fmt.Errorf("run-result %s declares case(s) the suite manifest at %s does not contain: %s",
			path, suitePath, summarizeMissing(extra))
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
// It strips terminal control runes from every id it names, because this is the ONLY
// route by which a case id reaches the operator's terminal under %s: both of
// checkCoverage's shortfall messages (the rejection and the --allow-partial-coverage
// warning) and all three of anchorSuiteDenominator's diagnostics funnel through here.
// Case ids are untrusted for the same reason the reviewer identity is — they come from
// the run-result being validated, and export is where a hand-supplied file first enters
// the tool.
//
// Sanitizing HERE rather than at the five call sites is what makes that claim checkable:
// a future diagnostic that names ids has to come through this function to get the cap,
// so it inherits the stripping with it. The id sites inside validateCoveredSet are
// deliberately left alone — they use %q, which already renders a control rune as a
// literal escape sequence.
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

func validateScrubbedCaseIDs(rr benchmark.RunResult, path string) error {
	scrubCaseID := func(s string) string {
		return scorecard.ScrubPublicString(s)
	}

	for _, id := range rr.SuiteCaseIDs {
		if r, bad := firstNonPrintingRune(id); bad {
			return fmt.Errorf("run-result %s lists suite case %q, which contains a non-printing rune (U+%04X); "+
				"control and format runes are invisible or reorder text in the published document, "+
				"so rename the case in the suite manifest",
				path, id, r)
		}
		s := strings.TrimSpace(scrubCaseID(id))
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
				"a case id that scrubs away publishes as \"\" in suite_case_ids",
				path, stripTerminalControlRunes(id))
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
			s := strings.TrimSpace(scrubCaseID(id))
			if s != id {
				return fmt.Errorf("run-result %s records covered case %q for %s/%s, which the publication scrub "+
					"rewrites to %q; rename the case in the suite manifest",
					path, id,
					stripTerminalControlRunes(c.Model), stripTerminalControlRunes(c.Persona), s)
			}
			if s == "" {
				return fmt.Errorf("run-result %s records covered case %q for %s/%s, which is empty once scrubbed "+
					"for publication; a case id that scrubs away publishes as \"\" in reviewer_coverage",
					path, stripTerminalControlRunes(id),
					stripTerminalControlRunes(c.Model), stripTerminalControlRunes(c.Persona))
			}
		}
	}
	return nil
}
