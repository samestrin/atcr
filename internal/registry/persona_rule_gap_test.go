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

	gaps, _ := PredicateRuleGaps(map[string]string{
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

// The sort survived mutation: deleting sort.Strings left the whole suite green,
// because every other case produces exactly ONE gap and a one-element slice is
// sorted by construction. Go map iteration is randomised and the doc comment
// promises "sorted agent names" — and the unordered output would reach the
// strings.Join in doctor's warning, so a second run would name the same gaps in a
// different order and read as a changed roster.
//
// Three gaps whose insertion-order names are deliberately NOT alphabetical, and
// the exact slice asserted. With a single iteration this would still pass ~1/6 of
// the time, so the map is driven repeatedly: randomisation means one run proves
// nothing, and the point is that NO ordering escapes.
func TestPredicateRuleGaps_NamesMultipleGapsInSortedOrder(t *testing.T) {
	project := t.TempDir()
	ruleLess := []byte("# p\n\n## Focus\n1. Injection findings\n")
	for _, name := range []string{"zulu", "mike", "alpha"} {
		require.NoError(t, os.WriteFile(filepath.Join(project, name+".md"), ruleLess, 0o644))
	}
	dirs := PersonaDirs{Project: project}

	roster := map[string]string{
		"zulu":  "zulu",
		"mike":  "mike",
		"alpha": "alpha",
	}
	for i := 0; i < 50; i++ {
		got, _ := PredicateRuleGaps(roster, dirs)
		require.Equal(t, []string{"alpha", "mike", "zulu"}, got,
			"the doc promises sorted names; Go map iteration is randomised, so the sort is what makes the output stable")
	}
}

// The swallow made "resolved, and carries the rule" byte-identical to "the prompt
// could not be read at all": both produce silence. The epic's own remediation makes
// that reachable — operators are told to paste a ~1.1 KB bullet into installed
// persona files, and a Registry-tier persona is re-validated against
// MaxPersonaPromptLen on every resolve, so growing one past the cap makes the agent
// VANISH from doctor's warning (reading as "fix applied") while `atcr review`
// hard-fails on the same config.
//
// An unresolvable persona is still NOT a gap — that part of the contract is
// unchanged, and asserted here. What changes is that it stops being invisible.
func TestPredicateRuleGaps_ReportsUnresolvedSeparatelyFromGaps(t *testing.T) {
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, "owasp.md"),
		[]byte("# owasp\n\n## Focus\n1. Injection findings\n"), 0o644))
	dirs := PersonaDirs{Project: project}

	gaps, unresolved := PredicateRuleGaps(map[string]string{
		"sec-agent":    "owasp",           // rule-less, resolves → a GAP
		"broken-agent": "never-installed", // explicit ref, no file → UNRESOLVED
		"bruce":        "bruce",           // embedded carrier → silent in both
	}, dirs)

	require.Equal(t, []string{"sec-agent"}, gaps,
		"an unreadable prompt supports no rule-absence verdict, so it must not be a gap")
	require.Equal(t, []string{"broken-agent"}, unresolved,
		"but it must not be silent either — silence has to mean \"read, and carries the rule\"")
}

// Both lists are sorted for the same reason: Go map iteration is randomised and
// both reach a strings.Join in doctor's output.
func TestPredicateRuleGaps_UnresolvedIsSorted(t *testing.T) {
	dirs := PersonaDirs{Project: t.TempDir()}
	roster := map[string]string{
		"zulu-agent":  "no-such-persona",
		"mike-agent":  "no-such-persona",
		"alpha-agent": "no-such-persona",
	}
	for i := 0; i < 50; i++ {
		_, unresolved := PredicateRuleGaps(roster, dirs)
		require.Equal(t, []string{"alpha-agent", "mike-agent", "zulu-agent"}, unresolved)
	}
}

// TestPredicateRuleGaps_EmptyRoster verifies the vacuous case: no roster, no
// gaps, no panic.
func TestPredicateRuleGaps_EmptyRoster(t *testing.T) {
	gaps, unresolved := PredicateRuleGaps(map[string]string{}, PersonaDirs{})
	require.Empty(t, gaps)
	require.Empty(t, unresolved)
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

	gaps, _ := PredicateRuleGaps(map[string]string{
		"sec-agent": "never-installed",
	}, dirs)

	require.Empty(t, gaps,
		"an agent whose persona could not be resolved must not be reported as lacking "+
			"the rule — nothing was read, so there is no verdict to give")
}
