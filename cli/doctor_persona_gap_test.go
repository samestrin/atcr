package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
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
	require.Error(t, err, "an agent whose persona cannot be resolved has no working invocation path")

	assert.Contains(t, out, "persona resolution errors",
		"an agent whose prompt could not be read must be named, not silently absent")
	assert.Contains(t, out, "bruce")
	assert.NotContains(t, out, "predicate-exhaustiveness rule gaps",
		"and it must NOT be reported as lacking the rule — nothing was read, so there is no verdict")
}

// The exit code, not just the warning. doctor's contract is "exits 0 when every agent
// has a working invocation path, 1 when any agent has none", and `atcr review`
// resolves these same personas and FAILS the run — so an agent whose persona cannot
// be resolved has none. The "exit code is unchanged" rationale was written for
// PredicateRuleGaps, a genuine composition advisory, and was carried onto a list that
// measures invocation health.
//
// --json is the case that matters most: RenderJSON skips the human warning entirely,
// so a JSON consumer checking only the exit code saw NOTHING at all and a CI gate
// concluded from exit 0 that review would run, exactly when it would not.
func TestDoctor_PersonaResolutionErrorFailsTheExitCode(t *testing.T) {
	for _, mode := range []string{"table", "json"} {
		t.Run(mode, func(t *testing.T) {
			srv := echoProvider(t, 0)
			setupDoctorEnvWithPersonaRef(t, srv.URL, "never-installed")
			t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

			args := []string{"doctor"}
			if mode == "json" {
				args = append(args, "--json")
			}
			out, err := execute(t, args...)

			require.Error(t, err, "an unresolvable persona must not exit 0")
			assert.Contains(t, out, "bruce", "the failing agent must be named in the output")
		})
	}
}

// Both causes at once: a dead endpoint (the self-test fails) AND an unresolvable
// persona. The persona branch must not shadow the endpoint failure — the two are
// repaired in different files (an endpoint failure is a key or base_url, an
// unresolvable persona is a missing prompt), so a message naming only one sends
// the operator to the wrong repair. Under --json the human warnings are skipped
// entirely, so this error string is the only prose the caller gets.
func TestDoctor_ExitMessageNamesBothCausesWhenBothFire(t *testing.T) {
	srv := echoProvider(t, http.StatusUnauthorized)
	setupDoctorEnvWithPersonaRef(t, srv.URL, "never-installed")
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-bad")

	out, err := execute(t, "doctor", "--json")
	require.Error(t, err, "a dead endpoint plus an unresolvable persona must not exit 0")

	msg := err.Error()
	assert.Contains(t, msg, "persona", "the persona cause must still be named")
	assert.Contains(t, msg, "endpoint", "the endpoint failure must be named too, not shadowed by the persona branch")
	_ = out
}

// The advisory half must NOT have been dragged along. A rule gap is a genuine
// composition signal about a persona that WAS read — review runs fine on it — so it
// keeps exiting 0. Pinned beside the change above because widening the exit code to
// both lists is the obvious over-correction.
func TestDoctor_RuleGapAloneStillExitsZero(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	// A readable project persona carrying neither anchor: a rule gap with no
	// resolution error beside it.
	writeProjectPersona(t, "bruce", "# bruce\n\n## Focus\n1. Correctness\n")

	out, err := execute(t, "doctor")
	require.NoError(t, err, "a readable persona that lacks the rule is advisory, not an invocation failure")
	assert.Contains(t, out, "predicate-exhaustiveness rule gaps")
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

// writePersonaStamp records what "init wrote" for the given files, so a test can
// set up a stamped workspace without running init.
func writePersonaStamp(t *testing.T, files map[string]string) {
	t.Helper()
	require.NoError(t, registry.WritePersonaStamp(filepath.Join(".atcr", "personas"),
		registry.PersonaStamp{Files: files}))
}

// A workspace initialized before Epic 35.16.9: no stamp, and the installed copy
// lacks the predicate rule. The warning must name the FILE (not only the agent),
// say it has no stamp, point at the refresh command, and leave the exit code at 0.
func TestDoctor_StaleWarningNamesAPreRuleFile(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	writeProjectPersona(t, "bruce", "# bruce\n\n## Focus\n1. Correctness\n")

	out, err := execute(t, "doctor")
	require.NoError(t, err, "a stale persona copy is advisory: the exit code must stay 0")

	assert.Contains(t, out, "stale persona files")
	assert.Contains(t, out, "bruce.md (bruce; no stamp, edits unknown)",
		"the warning names the file, the agent it serves, and that edits cannot be known without a stamp")
	assert.Contains(t, out, "atcr init --refresh-personas", "the warning must say how to fix it")
}

// The staleness scan reads the same filtered agentToPersona as the rule-gap scan,
// so with --agents it covers only the selected subset — and copies serving
// unselected agents go unchecked. The stale line has to name that scope too, or it
// reads as "every copy was checked". Isolated to the stale line: the rule-gap line
// fires on the same fixture and carries the scope already.
func TestDoctor_StaleWarningNamesTheSelectedScope(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	writeProjectPersona(t, "bruce", "# bruce\n\n## Focus\n1. Correctness\n")

	staleLine := func(out string) string {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "stale persona files") {
				return line
			}
		}
		t.Fatalf("no stale persona warning in output:\n%s", out)
		return ""
	}

	filtered, err := execute(t, "doctor", "--agents", "bruce")
	require.NoError(t, err, "a stale persona copy is advisory: the exit code must stay 0")
	line := staleLine(filtered)
	assert.Contains(t, line, "serve the selected agents",
		"with --agents the scan covers the selected subset, and the stale warning must say so")
	assert.Contains(t, line, "bruce.md (bruce; no stamp, edits unknown)", "the file list is unchanged")
	assert.Contains(t, line, "run `atcr init --refresh-personas` to update them", "the refresh command is unchanged")

	unfiltered, err := execute(t, "doctor")
	require.NoError(t, err)
	line = staleLine(unfiltered)
	assert.Contains(t, line, "serve the roster agents",
		"without --agents the scan covers the roster, and the stale warning must say so")
	assert.Contains(t, line, "bruce.md (bruce; no stamp, edits unknown)", "the file list is unchanged")
	assert.Contains(t, line, "run `atcr init --refresh-personas` to update them", "the refresh command is unchanged")
}

// A stamped copy whose embedded content has since changed, and which the operator
// also edited: stale AND marked edited, so the operator knows a refresh would
// overwrite the edit.
func TestDoctor_StaleWarningMarksAnEditedFile(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	embedded, err := personas.Get("bruce")
	require.NoError(t, err)
	writeProjectPersona(t, "bruce", embedded+"\nlocal rule\n")
	writePersonaStamp(t, map[string]string{
		"bruce.md": registry.HashPersonaContent([]byte("what an older atcr wrote")),
	})

	out, err := execute(t, "doctor")
	require.NoError(t, err)
	assert.Contains(t, out, "bruce.md (bruce; edited)")
	assert.NotContains(t, out, "no stamp")
}

// A current workspace: init wrote the embedded content and the stamp says so.
// The warning must stay silent — including when the operator has since edited
// the copy, which is a hand edit, not staleness.
func TestDoctor_StaleWarningSilentOnACurrentWorkspace(t *testing.T) {
	embedded, err := personas.Get("bruce")
	require.NoError(t, err)

	for name, body := range map[string]string{
		"unedited": embedded,
		"edited":   embedded + "\nlocal rule\n",
	} {
		t.Run(name, func(t *testing.T) {
			srv := echoProvider(t, 0)
			setupDoctorEnv(t, srv.URL)
			t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

			writeProjectPersona(t, "bruce", body)
			writePersonaStamp(t, map[string]string{"bruce.md": registry.HashPersonaContent([]byte(embedded))})

			out, err := execute(t, "doctor")
			require.NoError(t, err)
			assert.NotContains(t, out, "stale persona files")
		})
	}
}

// --json skips the stderr warning entirely, so the field is the only place a
// JSON consumer learns which files are stale. It must carry the same files.
func TestDoctor_StalePersonasInJSON(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	writeProjectPersona(t, "bruce", "# bruce\n\n## Focus\n1. Correctness\n")

	stdout, _, err := executeSplit(t, "doctor", "--json")
	require.NoError(t, err, "the exit code is unchanged under --json too")

	var got struct {
		StalePersonas []struct {
			File    string   `json:"file"`
			Agents  []string `json:"agents"`
			Edited  bool     `json:"edited"`
			Stamped bool     `json:"stamped"`
		} `json:"stale_personas"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout must be valid JSON: %s", stdout)
	require.Len(t, got.StalePersonas, 1)
	assert.Equal(t, "bruce.md", got.StalePersonas[0].File)
	assert.Equal(t, []string{"bruce"}, got.StalePersonas[0].Agents)
	assert.False(t, got.StalePersonas[0].Edited)
	assert.False(t, got.StalePersonas[0].Stamped)
}

// A clean workspace emits no stale_personas key at all.
func TestDoctor_StalePersonasOmittedFromJSONWhenCurrent(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	stdout, _, err := executeSplit(t, "doctor", "--json")
	require.NoError(t, err)
	assert.NotContains(t, stdout, "stale_personas")
}
