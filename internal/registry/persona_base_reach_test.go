package registry

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/require"
)

// personaBaseOnlyHeading names a section that exists in personas/_base.md and in
// no per-agent persona file. It is the lever this test pulls: text living only in
// _base.md is, by construction, text no registered agent receives.
const personaBaseOnlyHeading = "## Grounding (mandatory)"

// personaSectionFromBase returns the body of heading in the embedded _base.md, up to the
// next "## " heading. It fails loudly rather than returning empty, so a reworded
// _base.md turns this test into a clear instruction instead of a silent pass.
func personaSectionFromBase(t *testing.T, heading string) string {
	t.Helper()

	base, err := personas.Base()
	require.NoError(t, err)

	// Locate the heading LINE-WISE and require exactly one match. strings.Index
	// takes the first occurrence and is not anchored to a line start, so a prose
	// mention of the phrase earlier in the file silently redirects the extraction
	// (verified: an injected "Note: the ## Grounding (mandatory) rule below..."
	// line yields a 19-byte lever and a test that still passes — on text that is
	// not the section). A second match fails loudly instead of picking one.
	lines := strings.Split(base, "\n")
	sectionStart := -1
	for i, line := range lines {
		if stripTemplateActions(line) == heading {
			if sectionStart >= 0 {
				t.Fatalf("_base.md contains %q more than once (lines %d and %d) — the "+
					"extraction needs exactly one; disambiguate the heading or the prose", heading, sectionStart+1, i+1)
			}
			sectionStart = i
		}
	}
	require.GreaterOrEqualf(t, sectionStart, 0,
		"_base.md no longer contains %q — this test needs a section present ONLY in _base.md "+
			"to demonstrate what base-only text reaches; point it at another one", heading)

	// Stop at the next section heading. Slicing on a literal "\n## " is WRONG
	// here: _base.md's following headings are prefixed by template actions
	// ({{if .ToolsEnabled}}## Tool-Assisted Review, {{end}}## Severity Rubric), so
	// a literal scan runs straight past them and swallows the whole tool block —
	// which every per-agent file carries verbatim, making the returned text
	// anything but base-only and the assertion far coarser than it reads.
	var collected []string
	for _, line := range lines[sectionStart+1:] {
		if personaBaseSectionOpens(line) {
			break
		}
		collected = append(collected, line)
	}

	body := strings.TrimSpace(strings.Join(collected, "\n"))
	require.NotEmptyf(t, body, "the %s section in _base.md is empty — nothing to assert against", heading)

	// Premise: the extraction must span the FULL grounding section. A structural
	// edit that shortens or splits the section would silently NARROW the lever —
	// weakening the reachability assertion below while this test stays green — so
	// both ends of the section are pinned here. Rewording _base.md's grounding
	// prose means updating these two phrases with it.
	require.Contains(t, body, "Every finding MUST cite an exact FILE:LINE",
		"the extracted lever no longer opens with the grounding section's first sentence — "+
			"_base.md's grounding prose was reworded or the extraction is mis-anchored; "+
			"update this premise check alongside the prose")
	require.True(t, strings.HasSuffix(body, "exempt from the discard."),
		"the extracted lever does not end with the grounding section's closing sentence — "+
			"the extraction is truncating early (or the prose was reworded); it must span the "+
			"full section for the reachability assertion to mean what it claims")
	return body
}

// stripTemplateActions removes any leading {{...}} template actions from line and
// returns the remainder, whitespace-trimmed. An unterminated action yields "".
func stripTemplateActions(line string) string {
	line = strings.TrimSpace(line)
	for strings.HasPrefix(line, "{{") {
		stop := strings.Index(line, "}}")
		if stop < 0 {
			return ""
		}
		line = strings.TrimSpace(line[stop+2:])
	}
	return line
}

// personaBaseSectionOpens reports whether line starts a new "## " section heading, ignoring
// any leading template actions ({{if ...}}, {{else}}, {{end}}, {{range ...}}).
// Leading whitespace is trimmed first, and a heading is matched on "##" WITHOUT
// requiring the trailing space — a "##X" line is treated as a heading (truncate)
// because the failure mode of missing one is the lever silently swallowing the
// tool block, while "###"-deeper lines are not section headings. Known limit,
// documented rather than solved: a bare "## X" line inside a fenced code block is
// indistinguishable from a real heading at line level and truncates early.
// _base.md carries no fenced blocks.
func personaBaseSectionOpens(line string) bool {
	stripped := stripTemplateActions(line)
	return strings.HasPrefix(stripped, "##") && !strings.HasPrefix(stripped, "###")
}

// TestOpensSection pins the helper's line-level contract, including the shapes a
// reviewer verified the old version got wrong: "##X" (no space) and a leading
// space before "{{" both silently extended the section (the lever grew to ~1685
// bytes and swallowed the tool block), and "#### deeper" must stay non-heading.
func TestOpensSection(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"plain heading", "## Scope", true},
		{"heading after if-action", "{{if .ToolsEnabled}}## Tool-Assisted Review", true},
		{"heading after end-action", "{{end}}## Severity Rubric", true},
		{"heading after else-action", "{{else}}## Fallback", true},
		{"heading after range-action", "{{range .Items}}## Loop", true},
		{"heading without space", "##Tool-Assisted Review", true},
		{"indented action and heading", "  {{end}}## Payload", true},
		{"deeper heading is not a section", "#### deeper", false},
		{"plain prose", "The payload below is the changed diff.", false},
		{"unterminated action", "{{if .ToolsEnabled ## Never Closed", false},
		{"fence line", "```markdown", false},
		{"bare heading inside a fenced block (documented limit)", "## Inside a fence", true},
	}
	for _, tc := range cases {
		require.Equalf(t, tc.want, personaBaseSectionOpens(tc.line), "%s: %q", tc.name, tc.line)
	}
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
	baseOnly := personaSectionFromBase(t, personaBaseOnlyHeading)

	// Check the premise rather than trusting the comment on the const. If the
	// extraction ever runs past the grounding section it picks up the
	// Tool-Assisted Review block, which every per-agent file carries verbatim —
	// the assertions below would still pass, but on text that is not base-only.
	require.NotContains(t, baseOnly, "## Tool-Assisted Review",
		"the extracted lever overran the grounding section into the tool block, which every "+
			"per-agent persona carries verbatim — it is no longer base-only, so this test would "+
			"be asserting something much weaker than it claims")

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
			name, personaBaseOnlyHeading)
	}
}
