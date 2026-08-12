package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/benchmark"
)

// maxNamedMissingCases caps how many missing case ids the shortfall message spells
// out per row. The message exists to make the shortfall ACTIONABLE, and a row short
// by fifty cases is diagnosed by the first few plus the count; printing all of them
// buries the other short rows below it. It is a fixed limit, not a configuration
// knob — no flag or setting surfaces it.
const maxNamedMissingCases = 3

// checkCoverage is the publication gate: no reviewer row may reach the public board
// having been scored over less than the full suite.
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
	// than any of the tampering shapes below.
	if len(rr.SuiteCaseIDs) == 0 {
		_, _ = fmt.Fprintf(w,
			"warning: run-result %s carries no case coverage, so it cannot be verified against the full suite — "+
				"publishing it asserts nothing about how many cases each reviewer actually scored. "+
				"Re-run `atcr benchmark run` to produce a run-result that records coverage.\n", path)
		return nil
	}
	if len(rr.Coverage) == 0 {
		return fmt.Errorf("run-result %s lists a %d-case suite but records no reviewer coverage; "+
			"`atcr benchmark run` writes the two together, so this file is malformed",
			path, len(rr.SuiteCaseIDs))
	}

	suite := make(map[string]bool, len(rr.SuiteCaseIDs))
	for _, id := range rr.SuiteCaseIDs {
		suite[id] = true
	}

	// Index coverage by the (model, persona) identity the reviewer rows carry, so a
	// reviewer row with NO coverage row is caught rather than skipped. An
	// unverifiable row is exactly what a hand-supplied run-result would use to slip
	// past this gate, and export is where hand-supplied files first enter the tool.
	byIdentity := make(map[reviewerKey]benchmark.ReviewerCoverage, len(rr.Coverage))
	for _, c := range rr.Coverage {
		key := reviewerKey{model: c.Model, persona: c.Persona}
		// A DUPLICATE identity is rejected outright rather than resolved. The
		// producer emits one row per unique identity by construction, so a duplicate
		// means the file was hand-assembled — and last-write-wins would let a full
		// row mask a short one carrying the same identity, which is the cheapest
		// possible way to walk a partial run past this gate.
		if _, dup := byIdentity[key]; dup {
			return fmt.Errorf("run-result %s records coverage for %s/%s more than once; "+
				"a reviewer identity has exactly one covered case set, so this file is malformed",
				path, c.Model, c.Persona)
		}
		byIdentity[key] = c
	}

	var short []string
	for _, rev := range rr.Reviewers {
		key := reviewerKey{model: rev.Model, persona: rev.Persona}
		cov, ok := byIdentity[key]
		if !ok {
			short = append(short, fmt.Sprintf("%s/%s (no coverage recorded)", rev.Model, rev.Persona))
			continue
		}
		// `runs` and the covered set are appended together by the producer, so they
		// are equal by construction and a mismatch can only come from editing. Left
		// unchecked, a row could publish `runs` measured over two cases while
		// presenting a seventeen-case coverage list as its provenance.
		if rev.Runs != len(cov.CaseIDs) {
			return fmt.Errorf("run-result %s: reviewer %s/%s reports runs=%d but records %d covered case(s); "+
				"the two are written together by `atcr benchmark run`, so this file is malformed",
				path, rev.Model, rev.Persona, rev.Runs, len(cov.CaseIDs))
		}
		// A covered set must be a set OF THE SUITE. Checking only for missing ids
		// lets ["case-01","case-02","case-03","case-01"] satisfy a 3-case suite while
		// reporting runs=4 — full marks for a row that scored one case twice and
		// carries a bigger denominator than the suite has cases.
		if err := validateCoveredSet(suite, rr.SuiteCaseIDs, cov.CaseIDs, path, rev.Model, rev.Persona); err != nil {
			return err
		}
		missing := missingCases(suite, cov.CaseIDs)
		if len(missing) == 0 {
			continue
		}
		// Counted by MEMBERSHIP rather than derived as len(suite)-len(missing): the
		// two differ whenever the hand-supplied suite list repeats an id, and a
		// diagnostic that misreports the shortfall is worse than none.
		covered := 0
		for _, id := range cov.CaseIDs {
			if suite[id] {
				covered++
			}
		}
		short = append(short, fmt.Sprintf("%s/%s (%d/%d cases, missing %s)",
			rev.Model, rev.Persona, covered, len(suite), summarizeMissing(missing)))
	}

	if len(short) == 0 {
		return nil
	}
	if allowPartial {
		_, _ = fmt.Fprintf(w,
			"warning: publishing %s with partial coverage (--allow-partial-coverage): %s. "+
				"The submission records each row's covered cases, so the shortfall stays visible to consumers.\n",
			path, summarizeShortRows(short))
		return nil
	}
	return fmt.Errorf("run-result %s has reviewer row(s) scored over less than the full %d-case suite: %s; "+
		"re-run the missing cases, or pass --allow-partial-coverage to publish the shortfall explicitly",
		path, len(rr.SuiteCaseIDs), summarizeShortRows(short))
}

// validateCoveredSet rejects a coverage row that is not a subset-without-repeats of
// the suite. Both shapes are impossible from the producer — it appends one case id
// per fold, from the manifest — so either one means the file was assembled by hand,
// and both inflate a row's apparent coverage past what the suite can support.
func validateCoveredSet(suite map[string]bool, suiteIDs, covered []string, path, model, persona string) error {
	seen := make(map[string]bool, len(covered))
	for _, id := range covered {
		if seen[id] {
			return fmt.Errorf("run-result %s: reviewer %s/%s lists case %q more than once in its coverage; "+
				"a case is scored at most once per reviewer, so this file is malformed", path, model, persona, id)
		}
		seen[id] = true
		if !suite[id] {
			return fmt.Errorf("run-result %s: reviewer %s/%s records case %q, which is not one of the "+
				"%d cases in this suite", path, model, persona, id, len(suiteIDs))
		}
	}
	return nil
}

// missingCases returns the suite case ids absent from covered, sorted so the
// diagnostic is deterministic. Extra ids in covered that the suite does not contain
// are NOT reported as missing — they are already implied by the shortfall count, and
// the actionable fact is which suite cases the row still owes.
func missingCases(suite map[string]bool, covered []string) []string {
	have := make(map[string]bool, len(covered))
	for _, id := range covered {
		have[id] = true
	}
	var missing []string
	for id := range suite {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}

// summarizeMissing renders up to maxNamedMissingCases ids, then an overflow count.
func summarizeMissing(missing []string) string {
	if len(missing) <= maxNamedMissingCases {
		return strings.Join(missing, ", ")
	}
	return fmt.Sprintf("%s and %d more",
		strings.Join(missing[:maxNamedMissingCases], ", "), len(missing)-maxNamedMissingCases)
}

// summarizeShortRows applies the maxNamedMissingCases principle one level up:
// the shortfall message is a single line, so an uncapped list of short rows
// re-creates exactly the burial the constant exists to prevent. The first rows
// stay named and actionable; the rest collapse to an overflow count.
func summarizeShortRows(short []string) string {
	if len(short) <= maxNamedMissingCases {
		return strings.Join(short, "; ")
	}
	return fmt.Sprintf("%s; and %d more rows",
		strings.Join(short[:maxNamedMissingCases], "; "), len(short)-maxNamedMissingCases)
}
