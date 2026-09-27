package reconcile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// docs/registry.md's thinking, thinking_level, and thinking_style rows are the
// operator-facing statement of the per-agent thinking keys (Epic 35.16.11.2.2).
// Legal values are read from the registry accessors, not restated here, so
// renaming or adding one without touching the doc fails this test.
//
// Asserted on PHRASES inside single-line table rows, per
// response_format_doc_test.go, so no phrase can straddle a hard wrap.
func TestRegistryDoc_ThinkingRows(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	shared := []struct{ token, why string }{
		{"never inherited through `fallback:`", "a fallback sends its OWN declaration, never the primary's, like response_format"},
		{"rejected on a community persona", "rejectMachineLocalFields bans the key; a persona author must not be surprised at install"},
	}

	rows := []struct {
		key    string
		values []string
		extra  []struct{ token, why string }
	}{
		{"`thinking`", registry.ThinkingValues(), []struct{ token, why string }{
			{"byte-identical", "an agent with no thinking keys sends the same request body as before the keys existed"},
			{"bare `true`/`false` is rejected", "the field is a string so a YAML bool is not aliased to on/off"},
			{"`off` is rejected (use `thinking_level: low`)", "reasoning_effort has no off value; the row must give the fix the load error gives"},
			{"`on` requires a `thinking_level`", "reasoning_effort has no on-without-level value"},
			{"under `thinking_style: anthropic`, thinking on needs `temperature` unset or `1`", "Anthropic rejects extended thinking at any other temperature; a declared one fails the load"},
			{"under `thinking_style: anthropic`, thinking on cannot be combined with `tools: true`", "the tool loop does not send reasoning back, which Anthropic requires on a tool-use turn"},
		}},
		{"`thinking_level`", registry.ThinkingLevels(), []struct{ token, why string }{
			{"level alone implies `thinking: on`", "a level without thinking is not a missing-value error"},
			{"`thinking: off` with a level is rejected at load", "off plus a level is contradictory config"},
			{"loads with a warning and is sent as `high`", "max under reasoning_effort warns at load; the operator must not be surprised"},
			{"`thinking_style: template_kwargs` any level is rejected at load", "template_kwargs carries only on/off, so a level would be silently dropped"},
		}},
		{"`thinking_style`", registry.ThinkingStyles(), []struct{ token, why string }{
			{"there is no default style", "a thinking key without a style is a load error"},
			{"style alone is inert", "a style with no thinking or level sends nothing"},
		}},
	}
	for _, r := range rows {
		row := docRow(t, doc, r.key)
		var must []struct{ token, why string }
		if got := documentedValues(t, row); !slices.Equal(got, r.values) {
			t.Errorf("docs/registry.md's %s row lists legal values %v, validateAgent accepts %v", r.key, got, r.values)
		}
		must = append(must, shared...)
		must = append(must, r.extra...)
		for _, m := range must {
			if !strings.Contains(row, m.token) {
				t.Errorf("docs/registry.md's %s row must state %q: %s\nrow was: %s", r.key, m.token, m.why, row)
			}
		}
	}
}

// The thinking row says a bare YAML bool is rejected and the level row says off
// with a level is rejected. Those are claims about validateAgent, so they are
// checked against a real load.
func TestRegistryDoc_ThinkingRejectsWhatTheDocExcludes(t *testing.T) {
	load := func(keys string) error {
		path := filepath.Join(t.TempDir(), "registry.yaml")
		body := "providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n" + keys
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := registry.LoadRegistry(path)
		return err
	}
	style := "    thinking_style: " + registry.ThinkingStyleQwen + "\n"
	effort := "    thinking_style: " + registry.ThinkingStyleReasoningEffort + "\n"
	require.NoError(t, load("    thinking: "+registry.ThinkingOff+"\n"+style), "the documented off value must load")
	require.NoError(t, load(style), "the doc says a style alone loads")
	require.NoError(t, load("    thinking_level: "+registry.ThinkingLevelHigh+"\n"+style), "the doc says a level alone loads")
	for _, bad := range []string{"true", "false", "On"} {
		require.ErrorContainsf(t, load("    thinking: "+bad+"\n"+style), "invalid thinking", "the doc says %s is rejected at load", bad)
	}
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOff+"\n    thinking_level: "+registry.ThinkingLevelLow+"\n"+style),
		`thinking is "off" but thinking_level`, "the doc says off with a level is rejected at load")
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOn+"\n"), "there is no default style")
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOff+"\n"+effort), "set thinking_level: low")
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOn+"\n"+effort), "needs a thinking_level")
	anthropic := "    thinking_style: " + registry.ThinkingStyleAnthropic + "\n    max_tokens: 65536\n"
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOn+"\n    temperature: 0.7\n"+anthropic), "needs temperature 1",
		"the doc says anthropic thinking on with another temperature is rejected at load")
	require.NoError(t, load("    thinking: "+registry.ThinkingOn+"\n"+anthropic), "the doc says an unset temperature loads")
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOn+"\n    tools: true\n"+anthropic), "cannot use tools",
		"the doc says anthropic thinking on with tools is rejected at load")
	require.ErrorContains(t, load("    thinking_level: "+registry.ThinkingLevelLow+"\n    thinking_style: "+registry.ThinkingStyleTemplateKwargs+"\n"),
		`"template_kwargs" has no level`, "the doc says a level under template_kwargs is rejected at load")
}

// documentedValues returns the backticked values in a row's "must be unset,
// ... (exact" clause, in order, so an extra or missing value in the doc fails.
func documentedValues(t *testing.T, row string) []string {
	t.Helper()
	start := strings.Index(row, "must be unset, ")
	end := strings.Index(row, " (exact")
	require.True(t, start >= 0 && end > start, "row must enumerate its legal values as \"must be unset, ... (exact\": %s", row)
	var out []string
	parts := strings.Split(row[start:end], "`")
	for i := 1; i < len(parts); i += 2 {
		out = append(out, parts[i])
	}
	return out
}
