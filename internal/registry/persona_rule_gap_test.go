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
		"ghost":      "ghost",  // unresolvable → NOT a gap (resolution errors surface elsewhere)
	}, dirs)

	require.Equal(t, []string{"sec-agent"}, gaps,
		"exactly the rule-less roster agent should be named; carriers and unresolvable personas stay silent")
}

// TestPredicateRuleGaps_EmptyRoster verifies the vacuous case: no roster, no
// gaps, no panic.
func TestPredicateRuleGaps_EmptyRoster(t *testing.T) {
	require.Empty(t, PredicateRuleGaps(map[string]string{}, PersonaDirs{}))
}
