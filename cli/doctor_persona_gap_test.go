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

// The warning said "these roster agents", but filterRoster has already narrowed
// proj when --agents is set, so the scan covers only the selected subset while the
// wording claims the roster. docs/registry.md:518 says "effective roster". The
// message has to name the scope it actually checked.
func TestDoctor_RuleGapWarningNamesTheSelectedScope(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	writeProjectPersona(t, "bruce", "# bruce\n\n## Focus\n1. Correctness\n")

	filtered, err := execute(t, "doctor", "--agents", "bruce")
	require.NoError(t, err)
	assert.Contains(t, filtered, "predicate-exhaustiveness rule gaps")
	assert.Contains(t, filtered, "selected agents",
		"with --agents the scan covers the selected subset, and the warning must say so")

	unfiltered, err := execute(t, "doctor")
	require.NoError(t, err)
	assert.Contains(t, unfiltered, "roster agents",
		"without --agents the scan really does cover the roster, and the wording is unchanged")
}

// setupDoctorEnvWithFallback is setupDoctorEnv plus a fallback link: bruce falls
// back to bruce-backup. doctor.Resolve registers EVERY node of the chain in
// res.Agents, which is what let the gap scan reach an agent whose persona is never
// rendered at review time.
func setupDoctorEnvWithFallback(t *testing.T, baseURL string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	registryYAML := "" +
		"providers:\n" +
		"  mock:\n" +
		"    api_key_env: ATCR_DOCTOR_TEST_KEY\n" +
		"    base_url: " + baseURL + "/v1\n" +
		"agents:\n" +
		"  bruce:\n" +
		"    provider: mock\n" +
		"    model: test-model\n" +
		"    fallback: bruce-backup\n" +
		"  bruce-backup:\n" +
		"    provider: mock\n" +
		"    model: test-model\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(registryYAML), 0o644))

	work := t.TempDir()
	t.Chdir(work)
	atcrDir := filepath.Join(work, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	projYAML := "" +
		"agents:\n" +
		"  - bruce\n" +
		"payload_mode: blocks\n" +
		"timeout_secs: 600\n" +
		"fail_on: HIGH\n"
	require.NoError(t, os.WriteFile(filepath.Join(atcrDir, "config.yaml"), []byte(projYAML), 0o644))
}

// agentToPersona was built from res.Agents, which carries every node of every
// fallback chain. A fallback's OWN persona is never rendered: internal/fanout's
// review path resolves the persona by the PRIMARY's name. So doctor could name a
// -backup agent as a rule gap and send the operator to edit a file that affects no
// review. Over-report only, but the remedy it prescribes does nothing.
func TestDoctor_RuleGapSkipsAFallbackOnlyAgent(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnvWithFallback(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	// Only the FALLBACK's persona lacks the rule; the listed head keeps the
	// embedded built-in, which carries it.
	writeProjectPersona(t, "bruce-backup", "# bruce-backup\n\n## Focus\n1. Correctness\n")

	out, err := execute(t, "doctor")
	require.NoError(t, err)

	// Asserted on the WARNING, not on the whole output: bruce-backup legitimately
	// appears in the health table (it is a real endpoint doctor probes). What must
	// not happen is it being named as a rule gap.
	assert.NotContains(t, out, "predicate-exhaustiveness rule gaps",
		"a fallback's own persona is never rendered, so naming it is a remedy that changes nothing")
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

// The stderr half: an agent whose persona cannot be resolved gets its OWN line,
// distinct from the rule-gap warning. Before this, doctor reported a clean roster
// while `atcr review` hard-failed on the same config — the two states were the same
// observation.
func TestDoctor_ReportsPersonaResolutionErrorsSeparately(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnvWithPersonaRef(t, srv.URL, "never-installed")
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	out, err := execute(t, "doctor")
	require.NoError(t, err, "an unresolvable persona is a composition signal, not an endpoint failure")

	assert.Contains(t, out, "persona resolution errors",
		"an agent whose prompt could not be read must be named, not silently absent")
	assert.Contains(t, out, "bruce")
	assert.NotContains(t, out, "predicate-exhaustiveness rule gaps",
		"and it must NOT be reported as lacking the rule — nothing was read, so there is no verdict")
}

// setupDoctorEnvWithPersonaRef is setupDoctorEnv with an explicit `persona:` ref on
// bruce, which is what makes resolution FAIL rather than fall through to the
// embedded default (persona != agentName).
func setupDoctorEnvWithPersonaRef(t *testing.T, baseURL, personaRef string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(""+
		"providers:\n"+
		"  mock:\n"+
		"    api_key_env: ATCR_DOCTOR_TEST_KEY\n"+
		"    base_url: "+baseURL+"/v1\n"+
		"agents:\n"+
		"  bruce:\n"+
		"    provider: mock\n"+
		"    model: test-model\n"+
		"    persona: "+personaRef+"\n"), 0o644))

	work := t.TempDir()
	t.Chdir(work)
	atcrDir := filepath.Join(work, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(atcrDir, "config.yaml"), []byte(""+
		"agents:\n  - bruce\npayload_mode: blocks\ntimeout_secs: 600\nfail_on: HIGH\n"), 0o644))
}
