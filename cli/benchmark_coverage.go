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
// buries the other short rows below it.
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
	if len(rr.SuiteCaseIDs) == 0 || len(rr.Coverage) == 0 {
		_, _ = fmt.Fprintf(w,
			"warning: run-result %s carries no case coverage, so it cannot be verified against the full suite — "+
				"publishing it asserts nothing about how many cases each reviewer actually scored. "+
				"Re-run `atcr benchmark run` to produce a run-result that records coverage.\n", path)
		return nil
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
		missing := missingCases(suite, cov.CaseIDs)
		if len(missing) == 0 {
			continue
		}
		covered := len(rr.SuiteCaseIDs) - len(missing)
		short = append(short, fmt.Sprintf("%s/%s (%d/%d cases, missing %s)",
			rev.Model, rev.Persona, covered, len(rr.SuiteCaseIDs), summarizeMissing(missing)))
	}

	if len(short) == 0 {
		return nil
	}
	if allowPartial {
		_, _ = fmt.Fprintf(w,
			"warning: publishing %s with partial coverage (--allow-partial-coverage): %s. "+
				"The submission records each row's covered cases, so the shortfall stays visible to consumers.\n",
			path, strings.Join(short, "; "))
		return nil
	}
	return fmt.Errorf("run-result %s has reviewer row(s) scored over less than the full %d-case suite: %s; "+
		"re-run the missing cases, or pass --allow-partial-coverage to publish the shortfall explicitly",
		path, len(rr.SuiteCaseIDs), strings.Join(short, "; "))
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
