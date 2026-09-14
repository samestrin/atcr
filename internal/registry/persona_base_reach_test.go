package registry

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/require"
)

// baseOnlySectionHeading names a section that exists in personas/_base.md and in
// no per-agent persona file. It is the lever this test pulls: text living only in
// _base.md is, by construction, text no registered agent receives.
const baseOnlySectionHeading = "## Grounding (mandatory)"

// sectionFromBase returns the body of heading in the embedded _base.md, up to the
// next "## " heading. It fails loudly rather than returning empty, so a reworded
// _base.md turns this test into a clear instruction instead of a silent pass.
func sectionFromBase(t *testing.T, heading string) string {
	t.Helper()

	base, err := personas.Base()
	require.NoError(t, err)

	start := strings.Index(base, heading)
	require.GreaterOrEqualf(t, start, 0,
		"_base.md no longer contains %q — this test needs a section present ONLY in _base.md "+
			"to demonstrate what base-only text reaches; point it at another one", heading)

	rest := base[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	body := strings.TrimSpace(rest)
	require.NotEmptyf(t, body, "the %s section in _base.md is empty — nothing to assert against", heading)
	return body
}

// TestPersonaResolution_BaseOnlyTextReachesNoRegisteredAgent pins the trap that
// makes "edit _base.md" a no-op for the review panel.
//
// _base.md is a resolution FALLBACK, not an inherited prefix. Level 4 reads it
// only when no per-agent file matched at levels 2-3, and level 5 prefers embedded
// <agentName>.md over embedded _base.md. Every registered agent ships its own .md,
// so every registered agent takes the earlier exit and never sees _base.md at all.
//
// The consequence, and the reason this is pinned: a panel-wide review rule written
// into _base.md ALONE changes the behaviour of no registered reviewer. The edit
// lands, the suite stays green, and every agent reviews exactly as before — the
// change looks shipped and moves nothing. Such a rule has to be written into all
// ten files; personas.TestEveryBuiltinPersona_CarriesThePredicateExhaustivenessRule
// is what enforces that for the predicate-exhaustiveness rule.
//
// Resolution ORDER is already covered by TestPersonaResolution_FallbackToBase,
// _FallbackToEmbedded, _UnknownAgentFallsToEmbeddedBase and _EmptyFileFallsThrough.
// This test deliberately asserts something none of them do: the reachability
// consequence of that order, measured against the real shipped prompts rather
// than fixtures.
//
// If _base.md is ever made a shared prefix that every persona inherits, this test
// fails by design. That is a behaviour change to decide on, not one to discover
// later from a panel that quietly started reviewing differently.
func TestPersonaResolution_BaseOnlyTextReachesNoRegisteredAgent(t *testing.T) {
	baseOnly := sectionFromBase(t, baseOnlySectionHeading)

	// Empty dirs are the shipped layout: nothing installed on disk, so level 5
	// decides — exactly the path a default `atcr review` takes.
	dirs := personaDirs(t)

	names := personas.Names()
	require.NotEmpty(t, names, "no registered personas — the loop below would assert nothing")

	for _, name := range names {
		got, err := ResolvePersona(name, name, nil, dirs)
		require.NoErrorf(t, err, "resolving registered agent %q", name)

		require.Equalf(t, "embedded:"+name, got.Source,
			"registered agent %q resolved from %q rather than its own embedded file — this "+
				"test's premise (every registered agent ships its own .md) no longer holds",
			name, got.Source)

		require.NotContainsf(t, got.Text, baseOnly,
			"registered agent %q received text that exists only in _base.md. Either _base.md "+
				"became a shared prefix (a deliberate change to make, not to inherit), or %q is "+
				"no longer base-only. While it holds: a rule added to _base.md alone reaches no "+
				"registered agent, so a panel-wide rule belongs in every persona file.",
			name, baseOnlySectionHeading)
	}
}
