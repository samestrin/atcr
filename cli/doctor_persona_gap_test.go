package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeProjectPersona installs a project-tier persona file, which wins resolution
// at level 2 over the embedded built-in of the same name — the exact shape `atcr
// init` produces and never overwrites, and therefore the shape an operator is
// actually running.
func writeProjectPersona(t *testing.T, agent, body string) {
	t.Helper()
	dir := filepath.Join(".atcr", "personas")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, agent+".md"), []byte(body), 0o644))
}

// The panel-composition warning is doctor's ONLY signal for a roster agent whose
// prompt lacks the panel-wide predicate-exhaustiveness rule: the class guard walks
// embedded built-ins only, so a community or hand-edited project persona can sit on
// the panel carrying nothing and fail nothing. Until this test the whole block was
// uncovered — no assertion on whether it fires, on which agents it names, or on the
// exit code it must leave alone.
//
// Exit 0 is the load-bearing half. The gap is roster COMPOSITION, not invocation
// health: every endpoint here is reachable and the run is healthy. A non-zero exit
// would fail CI on a config that works, which is why the warning goes to stderr
// beside the summary line rather than through the status column.
func TestDoctor_WarnsWhenARosterPersonaLacksThePredicateRule(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	// A project persona carrying neither anchor — the pre-rule prompt every
	// existing install still has on disk.
	writeProjectPersona(t, "bruce", "# bruce\n\n## Focus\n1. Correctness\n")

	out, err := execute(t, "doctor")
	require.NoError(t, err, "a composition gap must NOT change the exit code — every endpoint is healthy")

	assert.Contains(t, out, "predicate-exhaustiveness rule gaps",
		"the warning must name the defect class, not just emit a bare agent list")
	assert.Contains(t, out, "bruce",
		"the warning must name the offending agent so the operator knows which file to edit")
	assert.Contains(t, out, "1 ok / 0 failed",
		"the ordinary summary line must still be emitted alongside the warning")
}

// The complementary half: a warning that fires unconditionally is worth as little
// as one that never fires. A roster whose personas all carry the rule must stay
// silent — and the default roster resolves to the embedded built-ins, which the
// class guard already holds to both anchors.
func TestDoctor_SilentWhenEveryRosterPersonaCarriesTheRule(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	// Precondition: with no project persona on disk, bruce resolves to the
	// embedded built-in, which carries the rule. Asserted rather than assumed —
	// if that stopped being true, the silence below would prove nothing.
	embedded, err := personas.Get("bruce")
	require.NoError(t, err)
	require.True(t, personas.CarriesPredicateRule(embedded),
		"precondition: the embedded built-in carries both anchors")

	out, err := execute(t, "doctor")
	require.NoError(t, err)

	assert.NotContains(t, out, "predicate-exhaustiveness rule gaps",
		"a roster with no gaps must emit no warning at all")
}

// A persona carrying only ONE of the two anchors is still a gap: the lens phrase
// without the filing phrase produces findings cited on the untouched sibling line,
// which the grounding gate discards before the report. This is the case that makes
// the conjunction in CarriesPredicateRule load-bearing all the way out to the CLI.
func TestDoctor_WarnsForAHalfCarrierPersona(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	lensOnly := "# bruce\n\n## Focus\n1. When a change edits one branch of a predicate, " +
		"enumerate every branch of that predicate and every field it is contracted to cover.\n"
	require.True(t, personas.CarriesPredicateRule(lensOnly) == false,
		"precondition: the lens anchor alone is not a carrier")
	writeProjectPersona(t, "bruce", lensOnly)

	out, err := execute(t, "doctor")
	require.NoError(t, err)

	assert.Contains(t, out, "predicate-exhaustiveness rule gaps",
		"a prompt that can spot the asymmetry but not file it reportably is still a gap")
	assert.Contains(t, out, "bruce")
}
