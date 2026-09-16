package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPredicateRuleGaps_NamesOnlyTheRuleLess pins the doctor pre-flight check
// for the panel-composition gap the predicate-exhaustiveness epic left
// deliberately open: the class guard walks embedded BUILT-INS only (community
// personas are Out of Scope), so an operator who configures a community persona
// gets a panel where every built-in carries the panel-wide rule and theirs does
// not — with no signal. PredicateRuleGaps is what makes the gap observable: it
// names the roster agents whose resolved prompt lacks the rule, and stays silent
// about carriers and unresolvable personas alike.
func TestPredicateRuleGaps_NamesOnlyTheRuleLess(t *testing.T) {
	project := t.TempDir()

	// A community-style persona that does NOT carry the rule — the shape every
	// community prompt shipped before the epic (and still ships, by the epic's
	// own Out of Scope decision).
	require.NoError(t, os.WriteFile(filepath.Join(project, "owasp.md"),
		[]byte("# owasp\n\n## Focus\n1. Injection findings\n\n## Output Format\nSEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE\n"),
		0o644))

	// A project persona that DOES carry the rule (both anchors verbatim).
	require.NoError(t, os.WriteFile(filepath.Join(project, "strict.md"),
		[]byte("# strict\n\n## Focus\n1. Predicate exhaustiveness: enumerate every branch of that predicate and every field it is contracted to cover — file it on the edited branch's changed line, quoting the sibling branch as evidence\n"),
		0o644))

	dirs := PersonaDirs{Project: project}

	gaps := PredicateRuleGaps(map[string]string{
		"sec-agent":  "owasp",  // rule-less community persona → named
		"strict-bot": "strict", // carrier → silent
		"bruce":      "bruce",  // embedded built-in carrier (empty dirs → level 5) → silent
		// NOT an error case, despite the name. persona == agentName, so resolution
		// never takes the explicit-ref failure at persona.go:102-105; it falls to
		// level 5, personas.Get("ghost") fails, and personas.Base() hands back the
		// embedded _base.md — which carries both anchors. So this row is silent as a
		// CARRIER. Calling it "unresolvable" here is what left the err arm below
		// untested: the suite stayed green with the guard deleted, because no case
		// ever reached it. The real error case is its own test.
		"ghost": "ghost",
	}, dirs)

	require.Equal(t, []string{"sec-agent"}, gaps,
		"exactly the rule-less roster agent should be named; carriers and unresolvable personas stay silent")
}

// TestPredicateRuleGaps_EmptyRoster verifies the vacuous case: no roster, no
// gaps, no panic.
func TestPredicateRuleGaps_EmptyRoster(t *testing.T) {
	require.Empty(t, PredicateRuleGaps(map[string]string{}, PersonaDirs{}))
}

// A persona that genuinely FAILS to resolve is not reported as a gap — the
// `if err != nil { continue }` arm — and until this test existed nothing reached
// that arm. Deleting the guard left the suite green, because the only case that
// claimed to cover it ("ghost", above) resolves successfully to embedded
// _base.md and is silent for an entirely different reason.
//
// Without the guard the zero-value ResolvedPersona flows on, `p.Text` is "",
// CarriesPredicateRule("") is false, and the agent is reported as a gap — a
// rule-absence verdict on a prompt that was never read. That is the failure this
// pins: the distinction between "resolved, and the rule is missing" and "never
// resolved at all".
//
// An EXPLICIT persona ref is what makes resolution fail rather than fall
// through: persona != agentName, so persona.go:102-105 returns
// ErrPersonaNotFound instead of descending to _base.md or the embedded default.
func TestPredicateRuleGaps_UnresolvablePersonaIsNotAGap(t *testing.T) {
	project := t.TempDir()
	dirs := PersonaDirs{Project: project}

	// Precondition: this ref really does fail to resolve. Asserted rather than
	// assumed — if a future resolution change made it fall through to a carrier,
	// the test below would pass for the wrong reason and stop guarding the arm.
	_, err := ResolvePersona("sec-agent", "never-installed", nil, dirs)
	require.ErrorIs(t, err, ErrPersonaNotFound,
		"precondition: an explicit ref with no file must fail, not fall through")

	gaps := PredicateRuleGaps(map[string]string{
		"sec-agent": "never-installed",
	}, dirs)

	require.Empty(t, gaps,
		"an agent whose persona could not be resolved must not be reported as lacking "+
			"the rule — nothing was read, so there is no verdict to give")
}
