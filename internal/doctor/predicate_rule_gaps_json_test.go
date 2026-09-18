package doctor

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Report.PredicateRuleGaps carries a json tag, but RenderJSON marshals a private
// wrapper struct rather than the Report itself — so a field added to Report is
// emitted only if the wrapper is extended too. It was not, and nothing noticed:
// docs/registry.md and the field's own doc comment both promise `--json` carries
// `predicate_rule_gaps`, while cli/doctor.go emits the human-readable warning
// ONLY in the table branch. A `--json` run was therefore silent in both channels,
// and a CI script reading the key could not tell "no gaps" from "never emitted".
//
// Asserted through a real json.Unmarshal rather than a substring match on the
// output: the key has to survive as a usable array, which is the only form a
// consumer can act on.
func TestRenderJSON_ReportsPredicateRuleGaps(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{
		Agents:            []AgentResult{{Agent: "a", Provider: "p", Model: "m", Status: StatusOK}},
		PredicateRuleGaps: []string{"sec-agent", "zeta-agent"},
	}))

	var parsed struct {
		Agents            []map[string]any `json:"agents"`
		PredicateRuleGaps []string         `json:"predicate_rule_gaps"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))

	assert.Equal(t, []string{"sec-agent", "zeta-agent"}, parsed.PredicateRuleGaps,
		"the gap list must reach --json in the order PredicateRuleGaps produced it; "+
			"docs/registry.md tells operators to read this key")
	require.Len(t, parsed.Agents, 1, "the agents array must be unaffected by the new key")
}

// The omitempty on the field is deliberate and load-bearing in the other
// direction: a clean roster must not grow a `"predicate_rule_gaps": null` key
// that a reader could mistake for a reported value. Pin the quiet case so a
// later "just drop omitempty" does not silently change what a clean run says.
func TestRenderJSON_OmitsPredicateRuleGapsWhenThereAreNone(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{
		Agents: []AgentResult{{Agent: "a", Provider: "p", Model: "m", Status: StatusOK}},
	}))

	assert.NotContains(t, buf.String(), "predicate_rule_gaps",
		"a roster with no gaps emits no key at all, so its absence reads as 'nothing to report'")
}

// The wrapper trap, one field later. RenderJSON marshals a private struct, not
// Report, so a json tag added to Report alone emits nothing — which is exactly how
// PredicateRuleGaps shipped silently once. PersonaResolutionErrors is the next
// field through the same door, so it gets the same guard rather than trusting the
// comment.
func TestRenderJSON_ReportsPersonaResolutionErrors(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{
		Agents:                  []AgentResult{},
		PersonaResolutionErrors: []string{"broken-agent", "zeta-agent"},
	}))

	var parsed struct {
		PersonaResolutionErrors []string `json:"persona_resolution_errors"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, []string{"broken-agent", "zeta-agent"}, parsed.PersonaResolutionErrors,
		"the unresolved list must reach --json; a Report-only json tag emits nothing through the wrapper")
}

// Omitted when empty, so a healthy roster's JSON is unchanged.
func TestRenderJSON_OmitsPersonaResolutionErrorsWhenThereAreNone(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{}}))
	assert.NotContains(t, buf.String(), "persona_resolution_errors")
}
