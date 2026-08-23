package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.5.1 TD: a context_window_tokens declaration was only observable
// through a failed provider call — and an over-declaration surfaces as an opaque
// HTTP 400, not as a context-window error. The doctor is the pre-flight surface
// that already enumerates every roster agent, so it reports each agent's
// RESOLVED window and the tier it resolved from, making a declaration that took
// effect (and one that contradicts a known model) visible without a real run.

// windowRegistry has one agent declaring a window, one on a table-known model,
// and one on an unknown alias that falls through to the conservative default.
func windowRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	declared := 262144
	return &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		Agents: map[string]registry.AgentConfig{
			"decl":  {Provider: "p", Model: "glm-5.2", ContextWindowTokens: &declared},
			"table": {Provider: "p", Model: "openai/gpt-5.5"},
			"deflt": {Provider: "p", Model: "some-proxy-alias"},
		},
	}
}

func windowRows(t *testing.T, res *Resolution) map[string]AgentTarget {
	t.Helper()
	out := map[string]AgentTarget{}
	for _, at := range res.Agents {
		out[at.Agent] = at
	}
	return out
}

func TestResolve_CarriesResolvedWindowAndSource(t *testing.T) {
	res, err := Resolve(windowRegistry(t), &registry.ProjectConfig{Agents: []string{"decl", "table", "deflt"}})
	require.NoError(t, err)
	rows := windowRows(t, res)

	assert.Equal(t, 262144, rows["decl"].ContextWindowTokens,
		"a declaration must be reported verbatim — that is what proves it took effect")
	assert.Equal(t, payload.WindowSourceDeclaration, rows["decl"].WindowSource)

	assert.Equal(t, 128000, rows["table"].ContextWindowTokens)
	assert.Equal(t, payload.WindowSourceTable, rows["table"].WindowSource)

	assert.Equal(t, 32768, rows["deflt"].ContextWindowTokens,
		"an unknown proxy alias resolves the conservative default")
	assert.Equal(t, payload.WindowSourceDefault, rows["deflt"].WindowSource)
}

func TestResolve_WindowIsPerAgentNotPerTarget(t *testing.T) {
	// Two agents can share one probe target (same provider+model+base_url) while
	// declaring different windows, so the window cannot ride on Target.
	declared := 262144
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://api.example/v1"}},
		Agents: map[string]registry.AgentConfig{
			// A table-KEYED model, so the undeclared sibling has a table window to
			// fall back to — the point being that it keeps that window rather than
			// inheriting its target-mate's declaration.
			"a": {Provider: "p", Model: "openai/gpt-5.5", ContextWindowTokens: &declared},
			"b": {Provider: "p", Model: "openai/gpt-5.5"},
		},
	}
	res, err := Resolve(reg, &registry.ProjectConfig{Agents: []string{"a", "b"}})
	require.NoError(t, err)
	require.Len(t, res.Targets, 1, "the two agents must still share one probe target")

	rows := windowRows(t, res)
	assert.Equal(t, 262144, rows["a"].ContextWindowTokens)
	assert.Equal(t, 128000, rows["b"].ContextWindowTokens,
		"the undeclared sibling keeps the table window despite sharing a target")
}

func TestRun_PopulatesWindowColumns(t *testing.T) {
	t.Setenv("K", "key")
	res, err := Resolve(windowRegistry(t), &registry.ProjectConfig{Agents: []string{"decl"}})
	require.NoError(t, err)
	fake := newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })

	rep := Run(context.Background(), fake, res, Options{Nonce: testNonce, MaxTokens: 2048})
	require.Len(t, rep.Agents, 1)
	assert.Equal(t, 262144, rep.Agents[0].ContextWindowTokens)
	assert.Equal(t, payload.WindowSourceDeclaration, rep.Agents[0].WindowSource)
}

func TestRenderTable_ShowsWindowAndSource(t *testing.T) {
	rep := &Report{Agents: []AgentResult{{
		Agent: "decl", Provider: "p", Model: "glm-5.2", Status: StatusOK,
		ContextWindowTokens: 262144, WindowSource: payload.WindowSourceDeclaration,
	}}}
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, rep))
	out := buf.String()
	assert.Contains(t, out, "WINDOW", "the table must carry a window header")
	assert.Contains(t, out, "262144")
	assert.Contains(t, out, payload.WindowSourceDeclaration,
		"the tier must be visible, not just the number — a declaration and a table hit look identical otherwise")
}

func TestRenderJSON_CarriesWindowFields(t *testing.T) {
	rep := &Report{Agents: []AgentResult{{
		Agent: "decl", Provider: "p", Model: "glm-5.2", Status: StatusOK,
		ContextWindowTokens: 262144, WindowSource: payload.WindowSourceDeclaration,
	}}}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, rep))

	var got struct {
		Agents []struct {
			ContextWindowTokens int    `json:"context_window_tokens"`
			WindowSource        string `json:"window_source"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got.Agents, 1)
	assert.Equal(t, 262144, got.Agents[0].ContextWindowTokens)
	assert.Equal(t, payload.WindowSourceDeclaration, got.Agents[0].WindowSource)
}

// The ok_warning hint tells the operator to raise a cap, and the default (non---json)
// table is where they read it — but the table had no column or cell carrying the cap, so
// the hint named a number the surface refused to show. window() is the in-file precedent:
// a resolved value plus its tier, rendered in one cell.
func TestRenderTable_ShowsTheProbedCapAlongsideTheWindow(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "bruce", Provider: "p", Model: "m", Status: StatusOKWarning,
		ContextWindowTokens: 32768, WindowSource: "default",
		MaxTokens: 32000, MaxTokensSource: MaxTokensSourceDeclaration,
	}}}))
	got := buf.String()

	assert.Contains(t, got, "32768", "the window is still rendered")
	assert.Contains(t, got, "32000",
		"the hint tells the operator to change this cap; the table must show what it currently is")
	assert.Contains(t, got, "declaration",
		"and where it came from, so 'raise the declaration' is actionable")
}

// A row with no probed cap (the probe short-circuited) must not render a bare 0 —
// and must not advertise review's cap either.
//
// The fixture leaves ReviewMaxTokens at its zero value no longer: Run ALWAYS assigns
// it (run.go:189 floors at reviewDefaultMaxTokens, run.go:207 assigns
// unconditionally), so a zero there models a state production cannot reach and the
// NotContains below was guaranteed by the fixture rather than by the code. Pinned at
// the value Run emits for a short-circuited probe, the assertion is about the cell an
// operator actually sees for a missing_key row.
func TestRenderTable_OmitsTheCapWhenNoCallWasPlaced(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "bruce", Provider: "p", Model: "m", Status: StatusMissingKey,
		ContextWindowTokens: 32768, WindowSource: "default",
		ReviewMaxTokens: reviewDefaultMaxTokens, // what Run emits when the probe short-circuits
	}}}))
	line := buf.String()

	assert.NotContains(t, line, "cap 0", "a probe that never ran has no cap to report")
	assert.NotContains(t, line, "review cap",
		"no call was placed, so the only cap left in the cell would be one that was never applied — and a reader who knows the documented `<window> / cap N (tier)` shape reads that two-part form as the cap the probe used")
	assert.Contains(t, line, "32768", "the window is still rendered")
}

// Under --max-tokens the probed cap and the cap `atcr review` will resolve DIFFER, and
// the ok_warning hint quotes review's cap. The WINDOW cell must carry both, or the hint
// names a number the surface refuses to display — the same defect the probed-cap render
// fixed, one tier over.
func TestRenderTable_ShowsTheReviewCapWhenItDiffersFromTheProbedCap(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "bruce", Provider: "p", Model: "m", Status: StatusOKWarning,
		ContextWindowTokens: 32768, WindowSource: "default",
		MaxTokens: 4096, MaxTokensSource: MaxTokensSourceFlag,
		ReviewMaxTokens: 32000,
	}}}))
	got := buf.String()

	assert.Contains(t, got, "cap 4096 (flag)", "the probed cap is still rendered, with its tier")
	assert.Contains(t, got, "32000",
		"the ok_warning hint quotes review's cap; the table must show what it currently is")
}

// The two coincide on the declaration tier (and whenever no flag overrides): the cell
// must not say the same number twice.
func TestRenderTable_OmitsTheReviewCapWhenItMatchesTheProbedCap(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "bruce", Provider: "p", Model: "m", Status: StatusOK,
		ContextWindowTokens: 32768, WindowSource: "default",
		MaxTokens: 32000, MaxTokensSource: MaxTokensSourceDeclaration,
		ReviewMaxTokens: 32000,
	}}}))
	got := buf.String()

	assert.NotContains(t, got, "review cap", "a review cap equal to the probed cap adds nothing")
}

// The `ReviewMaxTokens > 0` conjunct, which nothing pinned. Every other fixture in
// this file either sets the two caps EQUAL (asserting omission, which the `!=` half
// already guarantees) or sets MaxTokens to 0 — so deleting `> 0` left the suite
// green. Its live consumer is the decode path the field was made non-omitempty for:
// a report produced by an OLDER atcr carries no review_max_tokens, so ReviewMaxTokens
// is 0 while MaxTokens is positive, `0 != 8192` is true, and the mutant renders
// "/ review cap 0" — a cap of zero, the exact misattribution the field exists to
// prevent. Run itself never emits 0 (reviewMaxTokens floors at the shared default),
// which is why only a decoded report reaches this state and no other test does.
func TestRenderTable_OmitsReviewCapWhenNoReviewMaxTokensIsKnown(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "bruce", Provider: "p", Model: "m", Status: StatusOK,
		ContextWindowTokens: 32768, WindowSource: "default",
		MaxTokens: 8192, MaxTokensSource: MaxTokensSourceDeclaration,
		ReviewMaxTokens: 0, // a report decoded from an atcr predating the field
	}}}))
	got := buf.String()

	assert.Contains(t, got, "cap 8192", "the probed cap is real and must still render")
	assert.NotContains(t, got, "review cap",
		"an absent review_max_tokens is unknown, not zero — rendering it as a cap of 0 misattributes the budget to a cap that was never applied")
}

// --json must carry review's cap too: a script re-deriving the budget from
// context_window_tokens and max_tokens alone computes a healthy budget and disagrees
// with the ok_warning atcr shipped.
func TestRenderJSON_CarriesReviewMaxTokens(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{{
		Agent: "bruce", Provider: "p", Model: "m", Status: StatusOKWarning,
		ContextWindowTokens: 32768, WindowSource: "default",
		MaxTokens: 4096, MaxTokensSource: MaxTokensSourceFlag,
		ReviewMaxTokens: 32000,
	}}}))

	var got struct {
		Agents []struct {
			ReviewMaxTokens int `json:"review_max_tokens"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got.Agents, 1)
	assert.Equal(t, 32000, got.Agents[0].ReviewMaxTokens)
}

// The `MaxTokens <= 0` early return precedes the review-cap suffix, so a row with no
// probed cap shows NEITHER cap while its ok_warning hint quotes review's — the exact
// state the repo's own TestRun_ZeroBudgetVerdictDoesNotBlameACapThatWasNeverApplied
// exercises, and docs/registry.md states the cell carries the hint's operand. The
// probed cap is genuinely absent here and must stay absent; review's is the one the
// hint speaks for and must be shown.
func TestRenderTable_ShowsTheReviewCapWhenNoProbedCapWasApplied(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderTableError(&buf, &Report{Agents: []AgentResult{{
		Agent: "bruce", Provider: "p", Model: "m", Status: StatusOKWarning,
		ContextWindowTokens: 32768, WindowSource: "default",
		MaxTokens:       0, // the probe applied no cap at all
		ReviewMaxTokens: 8192,
	}}}))
	got := buf.String()

	assert.Contains(t, got, "review cap 8192",
		"the hint quotes review's cap, so a cell that shows no cap at all names a knob whose current value it refuses to display")
	assert.NotContains(t, got, "/ cap ",
		"the PROBED cap really was absent — inventing one would misreport what ran")
}
