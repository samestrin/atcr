package reconcile

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/benchmark.md documents the publishable-identity contract that `benchmark verify`
// and `benchmark run` enforce, and nothing pinned it. That is the exact defect the
// section was written to repair: the doc had already drifted from the code once, and a
// prose-only correction leaves the next drift just as silent as the last.
//
// The guard is BIDIRECTIONAL on purpose. Asserting only that the doc names the rules
// catches a doc edit but not a code edit — the direction that actually misleads an
// operator, because the doc keeps promising a check the binary no longer performs. So
// each rule is asserted twice: once as the claim the doc makes, and once as the arm in
// cli/benchmark_run.go that makes the claim true. Deleting either half fails the test.
//
// Each code-side needle is chosen to be BEHAVIOUR-BEARING, not merely present: an
// incidental mention (a len() in a capacity hint, say) would keep the assertion green
// through a real removal. Every needle below was mutation-checked by deleting the
// behaviour it stands for and confirming this test goes red.
//
// The code half is matched as TEXT rather than by calling the functions: checkPublishable
// and validatePublishableReviewerRoster are unexported members of package cli, so this
// package cannot invoke them. Matching their error strings is the strongest available
// grounding, and it is the strings themselves that reach the operator.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift tests
// (no Go package lives under docs/, and the published reconcile/ module must not assume
// this repo's file layout). See justification_record_boundary_test.go for the precedent.
func TestBenchmarkDoc_PublishableIdentityRulesMatchTheCode(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	code := readRepoFile(t, "../../cli/benchmark_run.go")

	// The three identity rules `checkPublishable` applies to suite, suite_version and
	// every cases[].id.
	for _, rule := range []struct {
		name    string
		inDoc   string
		inCode  string
		because string
	}{
		{
			name:    "no control (Cc) or format (Cf) rune",
			inDoc:   "No control (Cc) or format (Cf) rune",
			inCode:  "which contains a non-printing rune",
			because: "the publication scrub leaves these alone, so the rune survives into the published document",
		},
		{
			name:    "not empty once scrubbed",
			inDoc:   "empty once scrubbed",
			inCode:  "which is empty once scrubbed for publication",
			because: "the submission would publish an empty identity",
		},
		{
			name:    "not rewritten by the scrub",
			inDoc:   "rewritten by the scrub",
			inCode:  "which the publication scrub rewrites to",
			because: "the envelope would name a different suite than the manifest does",
		},
	} {
		t.Run(rule.name, func(t *testing.T) {
			assert.Contains(t, doc, rule.inDoc,
				"docs/benchmark.md must still name this rule (%s)", rule.because)
			assert.Contains(t, code, rule.inCode,
				"cli/benchmark_run.go must still enforce it, or the doc promises a check the binary no longer performs")
		})
	}

	// The reviewer-roster rule: a separate gate over the CONFIGURED panel, whose
	// coverage the doc states explicitly. Each clause is a claim the code has to keep.
	require.Contains(t, code, "func validatePublishableReviewerRoster",
		"the doc describes a configured-roster gate; it must still exist")

	for _, clause := range []struct {
		name   string
		inDoc  string
		inCode string
		why    string
	}{
		{
			name:   "covers both roster lanes",
			inDoc:  "`agents` and `serial_agents`",
			inCode: "append(names, cfg.Project.SerialAgents...)",
			why:    "fanout builds slots for the serial lane too, so a serial reviewer publishes a row like any other",
		},
		{
			// The needle is the MODEL arm's own call shape, not the bare
			// `cfg.Registry.Agents[cur].Fallback` the walk is written with. The two arms
			// share the file, so a chain literal is satisfied by whichever arm happens to
			// carry it — which is how the doc came to promise a transitive PERSONA check
			// while this test stayed green over a persona arm that never walked anything.
			name:   "walks the transitive fallback chain for the model",
			inDoc:  "`model` is checked along each agent's transitive `fallback` chain",
			inCode: `check(n, cur, "model"`,
			why:    "reviewerModel prefers a fallback model, so a fallback's model is publishable",
		},
		{
			// `n, n` IS the claim: the roster agent stands as both the roster key and the
			// checked entry, which is what "not along the chain" means in code. The model
			// arm passes `n, cur` and cannot satisfy this.
			name:   "checks the persona on the roster agent only",
			inDoc:  "`persona` is checked on the roster agent only",
			inCode: `check(n, n, "persona"`,
			why:    "fanout reassigns the result to the primary slot name, so a fallback's own persona is never published",
		},
		{
			name:   "treats the agent name as a persona when none is configured",
			inDoc:  "the **agent name itself** stands in for it",
			inCode: "persona = n",
			why:    "reviewerPersona falls back to the agent name, making the roster key itself a published identity",
		},
	} {
		t.Run("reviewer roster: "+clause.name, func(t *testing.T) {
			assert.Contains(t, doc, clause.inDoc,
				"docs/benchmark.md must still state this coverage (%s)", clause.why)
			assert.Contains(t, code, clause.inCode,
				"cli/benchmark_run.go must still implement it, or the doc overstates what the gate checks")
		})
	}
}

// readRepoFile reads a repo file by path relative to this package, failing the test
// rather than the package if it has moved — a renamed doc must read as "update this
// test", not as a panic in an unrelated suite.
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoErrorf(t, err, "reading %s: if the file moved, update this drift test to follow it", path)
	return string(b)
}
