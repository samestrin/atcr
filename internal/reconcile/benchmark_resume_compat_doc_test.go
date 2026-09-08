package reconcile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// needleProximity is how far (in characters) an inCodeNear companion may sit
// from its needle and still count as the same condition. It tolerates
// conjunct reorder and re-wrapping as the arm grows (roughly a dozen added
// conjuncts), while staying far below the >1000 characters that separate the
// compat arm from the decoy unstamped gate inside the empty-recorded-roster
// branch.
const needleProximity = 400

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
		// inCodeFile, when set, reads the code needle from that file instead of
		// cli/benchmark_checkpoint.go — some doc claims are made true by a
		// construct that lives in the runner, not the checkpoint writer.
		inCodeFile string
		// inCodeCount, when > 0, requires the needle to occur exactly that many
		// times instead of the default at-least-once Contains.
		inCodeCount int
		// inCodeNear, when set, must appear within needleProximity characters of
		// the inCode occurrence — the same condition, tolerant of conjunct
		// reorder and source re-wrapping.
		inCodeNear string
	}{
		{
			name: "the exception is scoped to a checkpoint written before roster_format was stamped",
			// The doc must name the precondition, not merely admit that some
			// exception exists — an operator cannot tell whether their own
			// checkpoint is covered otherwise.
			inDoc: "roster_format",
			// The needle pairs the projection compare with the unstamped gate by
			// PROXIMITY instead of pinning the full conjunct list: conjunct order,
			// the exact emptiness term, and re-wrapping a growing condition are
			// the cli package's own concern (its mutation tests own them), and
			// none of them changes what the doc claims. `cp.RosterFormat == ""`
			// alone occurs twice in the file — the compat arm and the
			// empty-recorded-roster branch — so it stays satisfied when the stamp
			// test is deleted from the arm, which is exactly the deletion that
			// makes the doc's "a stamped checkpoint never qualifies" false; the
			// proximity window keeps that decoy gate from satisfying the pair.
			inCode:      "equalStrings(recorded, sortedCopy(legacyRoster))",
			inCodeCount: 1,
			inCodeNear:  `cp.RosterFormat == ""`,
			because:     "the arm fires only on an UNSTAMPED checkpoint, so a stamped one is unaffected",
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
			// AC5 added this branch and the doc sentence naming it in the same epic,
			// so the drift guard must cover both halves: deleting the code branch or
			// deleting the doc sentence has to fail this test. `len(recorded) == 0`
			// occurs exactly once in cli/benchmark_checkpoint.go — the branch itself
			// — so the needle dies with the branch rather than surviving on a
			// comment or on the sentinel it shares with every other arm.
			name:    "a checkpoint that records an empty roster is rejected",
			inDoc:   "records an empty roster is rejected",
			inCode:  "len(recorded) == 0",
			because: "an empty roster proves nothing about the panel, so it must never be excused",
		},
		{
			// The upgrade is applied to the in-memory struct, and the runner's save
			// runs only after a case actually EXECUTES. A resume that replays every
			// completed case, or aborts before the first one, therefore leaves the
			// legacy form on disk and takes the exception again next time. The doc
			// said "once ... in place", which promised a durability the binary does
			// not provide — the same class of drift this whole test exists to close,
			// so it is pinned rather than merely corrected. The needle is the
			// runner's save CALL SITE, not the prose comment describing it, and
			// inCodeCount pins it to exactly one call in the runner, so an added
			// unconditional save trips this guard. Moving the single call out of
			// the per-case loop cannot happen silently: its error path reads the
			// loop's case variable, so the compiler, not this test, enforces that
			// the save stays inside the loop body.
			name:        "the upgrade persists only if the resumed run scores a further case",
			inDoc:       "written back only if the resumed run scores at least one further",
			inCode:      "saveCheckpoint(checkpointPath, cp)",
			inCodeFile:  "../../cli/benchmark_run.go",
			inCodeCount: 1,
			because:     "the runner's single save call sits inside the per-case loop, after the scored case is appended",
		},
	} {
		t.Run(claim.name, func(t *testing.T) {
			assert.Contains(t, doc, claim.inDoc,
				"docs/benchmark.md's Resume bullet must still name this half of the exception (%s)", claim.because)
			target := code
			targetName := "cli/benchmark_checkpoint.go"
			if claim.inCodeFile != "" {
				target = readRepoFile(t, claim.inCodeFile)
				targetName = strings.TrimPrefix(claim.inCodeFile, "../../")
			}
			if claim.inCodeCount > 0 {
				require.Equal(t, claim.inCodeCount, strings.Count(target, claim.inCode),
					"%s must implement %q exactly %d time(s), or the doc describes an exception the binary no longer grants", targetName, claim.inCode, claim.inCodeCount)
			} else {
				assert.Contains(t, target, claim.inCode,
					"cli/benchmark_checkpoint.go must still implement it, or the doc describes an exception the binary no longer grants")
			}
			if claim.inCodeNear != "" {
				anchor := strings.Index(target, claim.inCode)
				if assert.NotEqual(t, -1, anchor,
					"cli/benchmark_checkpoint.go must still contain %q — the doc's claim has no code half", claim.inCode) {
					lo := anchor - needleProximity
					if lo < 0 {
						lo = 0
					}
					hi := anchor + len(claim.inCode) + needleProximity
					if hi > len(target) {
						hi = len(target)
					}
					assert.Contains(t, target[lo:hi], claim.inCodeNear,
						"the unstamped gate must sit in the same condition as %q — the doc describes their conjunction", claim.inCode)
				}
			}
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
