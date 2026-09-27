package reconcile

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/doctor"
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
			{"under `thinking_style: anthropic`, thinking on cannot be combined with `supports_function_calling: true`", "the tool loop does not send reasoning back, which Anthropic requires on a tool-use turn; skeptic, debate, and fallback lanes can put any function-calling agent in the loop"},
			{"under `thinking_style: anthropic`, thinking on cannot be combined with `response_format: json_object`", "providers map response_format onto a forced tool_choice, which Anthropic rejects while extended thinking is on; the live-proxy probe was inconclusive, so the clause rests on the documented provider constraint"},
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
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOn+"\n    supports_function_calling: true\n"+anthropic), "cannot use supports_function_calling",
		"the doc says anthropic thinking on with function calling is rejected at load")
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

// docLineWith returns the one line of doc that contains marker. Every asserted
// phrase must sit on that line, so no phrase can straddle a hard wrap.
func docLineWith(t *testing.T, doc, marker string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	t.Fatalf("no line containing %q in docs/registry.md", marker)
	return ""
}

// styleTable returns the style table: the lines after the **Thinking styles.**
// intro, up to the first blank line after the table.
func styleTable(t *testing.T, doc string) string {
	t.Helper()
	start := strings.Index(doc, "**Thinking styles.**")
	require.True(t, start >= 0, "docs/registry.md has no **Thinking styles.** paragraph")
	rest := doc[start:]
	tableStart := strings.Index(rest, "\n|")
	require.True(t, tableStart >= 0, "no table after **Thinking styles.**")
	rest = rest[tableStart+1:]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// assertStates fails for each token the line does not contain.
func assertStates(t *testing.T, where, line string, must []struct{ token, why string }) {
	t.Helper()
	for _, m := range must {
		if !strings.Contains(line, m.token) {
			t.Errorf("docs/registry.md's %s must state %q: %s\nline was: %s", where, m.token, m.why, line)
		}
	}
}

// AC 07-01 Scenario 2 and Edge Case 1: the style table maps each style to the
// wire field llmclient sends, and the budget line states the numbers
// registry.ThinkingBudgetTokens returns.
func TestRegistryDoc_ThinkingStyleTable(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	type musts = []struct{ token, why string }
	rows := map[string]musts{
		registry.ThinkingStyleQwen: {
			{"`enable_thinking: bool`", "the qwen style's on/off field"},
			{"`thinking_budget`", "the qwen style sends the level's budget"},
		},
		registry.ThinkingStyleTemplateKwargs: {
			{"`chat_template_kwargs: {\"enable_thinking\": bool}`", "the template_kwargs style's only field"},
			{"no level", "a level under template_kwargs is rejected at load"},
		},
		registry.ThinkingStyleReasoningEffort: {
			{"`reasoning_effort`", "the reasoning_effort style's field"},
			{"`max` is sent as `high`", "high is the most the style accepts"},
			{"no off value", "thinking: off is a load error under this style"},
			{"use `thinking_level: low`", "the fix the load error gives"},
		},
		registry.ThinkingStyleAnthropic: {
			{"`thinking: {\"type\": \"enabled\", \"budget_tokens\": N}`", "the anthropic style's on shape"},
			{"`thinking: {\"type\": \"disabled\"}`", "the anthropic style's off shape"},
			{"sends no `temperature`", "Anthropic rejects extended thinking at any temperature but 1"},
		},
	}
	// Rows are read from the table after the intro only, so another table with a
	// `qwen` or `anthropic` row cannot satisfy them, and the table's style set must
	// equal the registry's.
	table := styleTable(t, doc)
	var listed []string
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "| `") {
			listed = append(listed, strings.Split(line, "`")[1])
		}
	}
	require.Equal(t, registry.ThinkingStyles(), listed, "the style table must list exactly the registry's styles, in order")
	efforts := slices.DeleteFunc(registry.ThinkingLevels(), func(l string) bool { return l == registry.ThinkingLevelMax })
	var effortList []string
	for _, l := range efforts {
		effortList = append(effortList, "`"+l+"`")
	}
	rows[registry.ThinkingStyleReasoningEffort] = append(rows[registry.ThinkingStyleReasoningEffort],
		struct{ token, why string }{strings.Join(effortList[:len(effortList)-1], ", ") + ", or " + effortList[len(effortList)-1], "every level but max is sent as itself"})
	for _, style := range registry.ThinkingStyles() {
		must, ok := rows[style]
		require.True(t, ok, "style %q has no expected wire field in this test; add it", style)
		assertStates(t, "style table row for `"+style+"`", docRow(t, table, "`"+style+"`"), must)
	}

	intro := docLineWith(t, doc, "**Thinking styles.**")
	assertStates(t, "thinking styles intro", intro, musts{
		{"no default style", "a thinking key without a style is a load error"},
		{"no inference from the model id", "the honored field differs per model and the id does not predict it"},
	})

	budgets := docLineWith(t, doc, "**Thinking budgets.**")
	for _, level := range registry.ThinkingLevels() {
		want := "`" + level + "` = " + strconv.Itoa(registry.ThinkingBudgetTokens(registry.ThinkingOn, level, registry.ThinkingStyleQwen))
		if !strings.Contains(budgets, want) {
			t.Errorf("docs/registry.md's thinking budgets line must state %q, the budget registry.ThinkingBudgetTokens returns\nline was: %s", want, budgets)
		}
	}
	medium := strconv.Itoa(registry.ThinkingBudgetTokens(registry.ThinkingOn, "", registry.ThinkingStyleAnthropic))
	assertStates(t, "thinking budgets line", budgets, musts{
		{"`anthropic` with `thinking: on` and no level uses " + medium, "Anthropic requires a budget when thinking is enabled"},
		{"loads with a warning", "a budget not below max_tokens warns at load"},
	})
}

// AC 07-01 Scenario 3: the max_tokens interaction.
func TestRegistryDoc_ThinkingMaxTokensNote(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	assertStates(t, "thinking and max_tokens note", docLineWith(t, doc, "**Thinking and `max_tokens`.**"), []struct{ token, why string }{
		{"thinking tokens count against the output cap on most providers", "raising max_tokens alone does not stop a runaway thinker"},
		{"`thinking: off` is the first fix for a model that truncates with zero findings", "archer ran to about 100k tokens with no findings"},
		{"under `reasoning_effort`, use `thinking_level: low` instead", "thinking: off is a load error under that style"},
	})
	// TD-008: the executor lane's gap is named, as the max_tokens row names its own,
	// and the claim is checked against ExecutorConfig so it cannot go stale.
	executor := reflect.TypeOf(registry.ExecutorConfig{})
	for i := 0; i < executor.NumField(); i++ {
		require.NotContains(t, executor.Field(i).Name, "Thinking", "ExecutorConfig gained a thinking field; update the `thinking` row's executor clause")
	}
	assertStates(t, "`thinking` row", docRow(t, doc, "`thinking`"), []struct{ token, why string }{
		{"the executor (fix generation) has no thinking keys", "ExecutorConfig has no thinking fields, so fix generation always takes the provider default"},
	})
}

// AC 07-01 Scenario 4: the doctor verdict. Rows are read from the thinking
// verdict section only, so a later table with the same status names cannot
// satisfy them.
func TestRegistryDoc_ThinkingDoctorVerdict(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	section := docSection(t, doc, "### Thinking verdict")
	intro := docLineWith(t, section, "declares `thinking` or `thinking_level`")
	assertStates(t, "thinking verdict intro", intro, []struct{ token, why string }{
		{"`reasoning_tokens > 0` or non-empty reasoning content", "the either-signal rule: some upstreams never report reasoning_tokens"},
		{"a `reasoning_content` or `reasoning` field, or inline `<think>` text in the content", "every signal reasoningSignal reads"},
		{"never changes the ok/failed count or the exit code", "the verdict is a warning, like response_format's"},
		{"the same prompt and cap without the declaration", "a silent reply is judged only after a control probe shows the provider reports reasoning"},
	})
	assertStates(t, "thinking verdict warning line", docLineWith(t, section, "The HINT column labels"), []struct{ token, why string }{
		{"one warning line for each declared polarity of `" + doctor.ThinkingNotHonored + "`", "the not-honored remedy differs by polarity (TD cli/doctor.go:255)"},
		{"one warning line for `" + doctor.ThinkingUnverified + "`", "--json prints no warning lines and honored prints none"},
	})
	rows := map[string][]struct{ token, why string }{
		doctor.ThinkingHonored: {
			{"does not prove the declared level", "a signal under thinking: on shows only that thinking is on"},
			{"a level with no signal while the control probe shows reasoning", "a level can legitimately remove reasoning on a short prompt"},
		},
		"`not_honored` (declared `off`)": {
			{"another `thinking_style`", "first remedy for a declared-off that is ignored"},
			{"a larger `max_tokens`", "the off-polarity remedy keeps the max_tokens escape hatch (TD cli/doctor.go:255)"},
		},
		"`not_honored` (declared `on`)": {
			{"another `thinking_style`", "first remedy for a declared-on that produced no signal"},
			{"a different model", "the on-polarity remedy drops the max_tokens escape hatch (TD cli/doctor.go:255)"},
		},
		doctor.ThinkingUnverified: {
			{"may not report reasoning at all", "silence on both calls cannot be told apart from a provider that cannot report"},
			{"cut off before any signal showed", "a cut-off reply with no signal reaches no verdict"},
		},
	}
	for status, must := range rows {
		assertStates(t, "thinking verdict row `"+status+"`", docRow(t, section, "`"+status+"`"), must)
	}
	assertStates(t, "doctor JSON schema", docLineWith(t, doc, "`thinking_status` (`"), []struct{ token, why string }{
		{"`thinking_status` (`" + doctor.ThinkingHonored + "`, `" + doctor.ThinkingNotHonored + "`, or `" + doctor.ThinkingUnverified + "`)", "the JSON field's values are the doctor constants"},
		{"`thinking_detail`", "the verdict's reason rides beside it in --json"},
		{"`thinking_declared`", "the declared polarity (off/on/level) rides beside the verdict so remedies can be split without parsing detail prose"},
	})
}
