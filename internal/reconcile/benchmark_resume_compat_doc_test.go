package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/benchmark.md's Resume bullet stated an UNCONDITIONAL fail-closed guarantee:
// "If the suite content changed, or the roster changed (a reviewer added/removed, or a
// model swapped), the run fails closed with a clear message". Epic 35.16.6.6
// deliberately added an exception the bullet did not mention — validateCheckpointRoster
// carries a pre-serial-lane compatibility arm that RESUMES an unstamped checkpoint
// across an added serial reviewer — so a user-facing document promised a check the
// binary does not perform for that case. The existing
// benchmark_publishable_doc_test.go covers the configured-roster publishable-identity
// gate only and does not reach the Resume bullet at all.
//
// The guard is BIDIRECTIONAL, for the same reason its sibling is. Asserting only that
// the doc names the exception catches a doc edit but not a code edit — and the code
// direction is the one that misleads an operator, because the doc would keep promising
// an exception the binary no longer grants (or, worse, keep describing an arm whose
// conditions have quietly changed). So each claim is asserted twice: once as the
// sentence the doc makes, once as the construct in cli/benchmark_checkpoint.go that
// makes the sentence true. Deleting either half fails this test.
//
// Each code-side needle is BEHAVIOUR-BEARING rather than incidental: every one of them
// is a term the arm cannot fire without, so deleting the behaviour it stands for turns
// this test red rather than leaving it green through a real removal.
//
// The code half is matched as TEXT rather than by calling the function:
// validateCheckpointRoster, runCheckpoint and rosterFormatUnion are unexported members
// of package cli, so this package cannot invoke or reference them. Matching the source
// is the strongest available grounding for a doc-vs-code claim across that boundary.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift tests
// (no Go package lives under docs/, and the published reconcile/ module must not assume
// this repo's file layout). See justification_record_boundary_test.go for the precedent
// and benchmark_publishable_doc_test.go for the bidirectional idiom.
func TestBenchmarkDoc_ResumeCompatExceptionMatchesTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	code := readRepoFile(t, "../../cli/benchmark_checkpoint.go")

	require.Contains(t, code, "func validateCheckpointRoster",
		"the doc describes a roster gate on resume; it must still exist")

	for _, claim := range []struct {
		name    string
		inDoc   string
		inCode  string
		because string
	}{
		{
			name: "the exception is scoped to a checkpoint written before roster_format was stamped",
			// The doc must name the precondition, not merely admit that some
			// exception exists — an operator cannot tell whether their own
			// checkpoint is covered otherwise.
			inDoc: "roster_format",
			// The needle carries the FULL arm, not `cp.RosterFormat == ""` alone.
			// That shorter string now occurs twice in the file — the compat arm and
			// the empty-recorded-roster branch below it — so it stays satisfied when
			// the stamp test is deleted from the arm, which is exactly the deletion
			// that makes the doc's "a stamped checkpoint never qualifies" false.
			inCode:  `if cp.RosterFormat == "" && len(recorded) > 0 && equalStrings(`,
			because: "the arm fires only on an UNSTAMPED checkpoint, so a stamped one is unaffected",
		},
		{
			name:    "the recorded roster must equal the parallel-lane-only projection",
			inDoc:   "parallel-lane-only",
			inCode:  "equalStrings(recorded, sortedCopy(legacyRoster))",
			because: "the arm matches only that projection of the CURRENT config, so a drifted parallel reviewer still mismatches",
		},
		{
			name:    "such a checkpoint resumes across an added serial reviewer",
			inDoc:   "added serial reviewer",
			inCode:  "cp.Roster = current",
			because: "the arm returns nil for that case rather than failing closed",
		},
		{
			name:    "and is upgraded to the union form",
			inDoc:   "upgraded to the union form",
			inCode:  "cp.RosterFormat = rosterFormatUnion",
			because: "the upgrade is what stops the exception being a standing hole",
		},
		{
			// The upgrade is applied to the in-memory struct, and saveCheckpoint runs
			// only after a case actually EXECUTES. A resume that replays every
			// completed case, or aborts before the first one, therefore leaves the
			// legacy form on disk and takes the exception again next time. The doc
			// said "once ... in place", which promised a durability the binary does
			// not provide — the same class of drift this whole test exists to close,
			// so it is pinned rather than merely corrected.
			name:    "the upgrade persists only if the resumed run scores a further case",
			inDoc:   "written back only if the resumed run scores at least one further",
			inCode:  "a write happens only",
			because: "saveCheckpoint runs after a scored case, so a pure-replay resume never persists the upgrade",
		},
	} {
		t.Run(claim.name, func(t *testing.T) {
			assert.Contains(t, doc, claim.inDoc,
				"docs/benchmark.md's Resume bullet must still name this half of the exception (%s)", claim.because)
			assert.Contains(t, code, claim.inCode,
				"cli/benchmark_checkpoint.go must still implement it, or the doc describes an exception the binary no longer grants")
		})
	}

	// The exception is NARROW, and the doc must not read as a general amnesty. The
	// unconditional half of the promise still holds for every stamped checkpoint, so
	// the bullet keeps saying so and the code keeps the fail-closed default.
	assert.Contains(t, doc, "fails closed",
		"the Resume bullet must still state the default, or the exception reads as the rule")
	assert.Contains(t, code, "errCheckpointRosterMismatch",
		"the fail-closed default must still be reachable")
}
