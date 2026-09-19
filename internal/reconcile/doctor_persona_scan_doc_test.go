package reconcile

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/registry.md's `atcr doctor` section describes the persona scan's SCOPE and
// its effect on the EXIT CODE. Both had drifted from cli/doctor.go, and both drifts
// mislead in the direction that matters: the doc claimed a wider scan than runs, and
// said nothing about the list that decides whether review will work at all.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift
// tests — no Go package lives under docs/, and the published reconcile/ module must
// not assume this repo's file layout. See justification_record_boundary_test.go.
//
// BIDIRECTIONAL, like its siblings: each claim is asserted against the doc AND
// against the code arm that makes it true, so a code edit that silently widens or
// narrows the behaviour fails here rather than leaving the doc quietly false.
func TestRegistryDoc_DoctorPersonaScanMatchesTheCode(t *testing.T) {
	doc := readDoctorDoc(t)
	code := readDoctorCode(t)

	// SCOPE. cli/doctor.go builds the scanned set from proj.Agents + proj.SerialAgents
	// — the DIRECTLY-LISTED agents — and deliberately not from res.Agents, which
	// registers every node of every fallback chain. The narrowing is correct (a
	// fallback's own persona never renders: fanout resolves a chain's persona by the
	// PRIMARY's name), but the doc still promised "any effective-roster agent", which
	// this same page defines as including fallback-reachable agents. A reader would
	// conclude a -backup entry's prompt had been checked.
	assert.Contains(t, doc, "directly-listed",
		"the doc must scope the rule-gap scan to directly-listed agents, not the effective roster")
	assert.NotContains(t, doc, "When any effective-roster agent resolves to such a prompt",
		"the overstated scope claim must be gone, not merely qualified elsewhere")
	assert.Contains(t, code, "listed = append(listed, proj.Agents...)",
		"the scan must still be built from the directly-listed agents the doc now names")

	// EXIT CODE. The two lists are treated differently and the doc must say which is
	// which: a rule gap is composition (the persona WAS read; review runs), while a
	// resolution error is invocation health (`atcr review` resolves the same persona
	// and fails the run), so only the second moves the exit code.
	assert.Contains(t, doc, "predicate_rule_gaps",
		"the doc must name the advisory --json key")
	assert.Contains(t, doc, "persona_resolution_errors",
		"the doc must name the --json key that signals review will fail")
	assert.Contains(t, code, "len(rep.PersonaResolutionErrors) > 0 && rep.ExitCode == 0",
		"a resolution error must still fail the exit code the doc describes")

	// The claim a JSON consumer acts on. --json skips the human warning entirely, so
	// the exit code is the only signal it gets, and the doc is where that is stated.
	assert.Contains(t, doc, "exits non-zero",
		"the doc must state that a persona resolution error changes the exit code")
}

// The `--agents` line describes what a SUBSET run still probes. "probed" there means
// endpoint reachability — doctor invokes every node of a selected agent's fallback
// chain so its health verdict stays accurate — and NOT the persona scan, which never
// walks the chain. One unqualified word covered both and made the scope correction
// above read as contradicted two lines up.
func TestRegistryDoc_AgentsFlagProbeWordingExcludesThePersonaScan(t *testing.T) {
	doc := readDoctorDoc(t)

	for _, line := range strings.Split(doc, "\n") {
		if !strings.Contains(line, "--agents") || !strings.Contains(line, "fallback chain") {
			continue
		}
		assert.Contains(t, line, "endpoint",
			"the --agents wording must say the fallback chain is probed for ENDPOINT health, "+
				"so it does not read as the persona scan walking the chain too: %s", line)
	}
}

func readDoctorDoc(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../docs/registry.md")
	require.NoError(t, err)
	return string(data)
}

func readDoctorCode(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../cli/doctor.go")
	require.NoError(t, err)
	return string(data)
}
