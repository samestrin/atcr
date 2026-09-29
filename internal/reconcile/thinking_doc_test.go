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
	"github.com/samestrin/atcr/internal/llmclient"
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
			{"set a `thinking_level` instead", "reasoning_effort has no off value; the row states the constraint without prescribing low, which epic 35.16.11.2.2.2 measured silencing nemotron-3-super-120b"},
			{"see **Thinking and `max_tokens`**", "the row cross-references the note that says which level is safe"},
			{"`on` requires a `thinking_level`", "reasoning_effort has no on-without-level value"},
			{"under `thinking_style: anthropic`, thinking on needs `temperature` unset or `1`", "Anthropic rejects extended thinking at any other temperature; a declared one fails the load"},
			{"under `thinking_style: anthropic`, thinking on cannot be combined with `supports_function_calling: true`", "the guard stays (AC 05-02); skeptic, debate, and fallback lanes can put any function-calling agent in the loop"},
			{"has not been verified against a live Anthropic model", "the guard's true reason: no Anthropic model was served to prove the replay live"},
			{"thinking blocks are missing or altered", "what Anthropic rejects on a tool-use turn"},
			{"only the `anthropic` combination is rejected at load", "no second style-keyed guard (D6)"},
			{"another style (for example `reasoning_effort`)", "a Claude model under another style loads and runs the same replay, not live-verified either (TD-015)"},
			{"under `thinking_style: anthropic`, thinking on cannot be combined with `response_format: json_object`", "providers map response_format onto a forced tool_choice, which Anthropic rejects while extended thinking is on; the live-proxy probe was inconclusive, so the clause rests on the documented provider constraint"},
			{"the tool loop sends each assistant turn's reasoning back on every later turn", "the replay, stated positively, not just the old caveat removed"},
			{"in the shape the provider returned it", "each provider's own member is replayed unedited, never converted"},
			{"on assistant turns only", "reasoning never rides a user or tool-result turn"},
			{"whatever the `thinking_style`", "the replay has no style gate (D1)"},
			{"an empty array or object is not sent back either", "an empty container is dropped as absent, not replayed as received"},
		}},
		{"`thinking_level`", registry.ThinkingLevels(), []struct{ token, why string }{
			{"level alone implies `thinking: on`", "a level without thinking is not a missing-value error"},
			{"`thinking: off` with a level is rejected at load", "off plus a level is contradictory config"},
			{"loads with a warning and is sent as `high`", "max under reasoning_effort warns at load; the operator must not be surprised"},
			{"`thinking_style: template_kwargs` any level is rejected at load", "template_kwargs carries only on/off, so a level would be silently dropped"},
			// TD internal/reconcile/thinking_doc_test.go:53: the glm clause is pinned
			// as a token AND against a real load, so it cannot be deleted or reversed
			// silently.
			{"The same holds under `thinking_style: glm`", "glm has no level field either; the sentence can go stale just like the template_kwargs one"},
		}},
		{"`thinking_style`", registry.ThinkingStyles(), []struct{ token, why string }{
			{"there is no default style", "a thinking key without a style is a load error"},
			{"style alone is inert", "a style with no thinking or level sends nothing"},
		}},
		{"`preserve_thinking`", registry.ThinkingValues(), []struct{ token, why string }{
			// TD-019: the phrase is built from registry.PreserveThinkingStyles(),
			// not restated literals, so a third preserve style updates the token.
			{"requires " + preserveThinkingStylesPhrase(registry.PreserveThinkingStyles()), "only those styles have a preserved-thinking field"},
			// TD internal/reconcile/thinking_doc_test.go:65: the level-alone half of
			// "thinking on" must be pinned as a token and against a real load.
			{"or a `thinking_level` under `qwen`", "thinking on also means a level under qwen; the drift test pinned only the thinking: on path"},
			{"and thinking on", "the flag with thinking off is a load error"},
			{"`preserve_thinking: true`", "the qwen wire field"},
			{"`off` sends `preserve_thinking: false`", "the qwen off value is an explicit signal, not nothing"},
			{"`thinking: {\"type\":\"enabled\",\"clear_thinking\":false}`", "the glm on object; llmclient's thinking tests pin the wire bytes"},
			{"`\"clear_thinking\":true`", "the glm off value is inverted"},
			{"the loop sends it back with or without the flag", "the flag asks the model to use the replay, it does not turn the replay on"},
			{"Unset sends nothing", "an undeclared agent's body is unchanged"},
			{"It is sent by the review fan-out, the skeptic, the debate seats, and `atcr doctor`", "the lanes that send the flag, matching the thinking row's lane list"},
			{"accepted on the wire but its later-turn effect is not live-verified", "TD-022: GLM never reached turn 2 in any live run, so the rows must not read as verified"},
			{"can spend the whole output cap on turn 1", "the observed failure: finish_reason=length at both the 16384 and 32768 budgets in two of three runs"},
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
	// The replay shipped in Sprint 35.16.11.2.2.1, so the old caveat must be gone.
	thinking := docRow(t, doc, "`thinking`")
	for _, stale := range []string{"does not send", "epic 35.16.11.2.2.1", "may fail on turn 2"} {
		require.NotContains(t, thinking, stale, "the `thinking` row still carries the pre-replay caveat")
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
	fcErr := load("    thinking: " + registry.ThinkingOn + "\n    supports_function_calling: true\n" + anthropic)
	require.ErrorContains(t, fcErr, "cannot use supports_function_calling",
		"the doc says anthropic thinking on with function calling is rejected at load")
	require.ErrorContains(t, fcErr, "has not been verified against a live Anthropic model",
		"the doc and the load error must give the same reason")
	require.NoError(t, load("    thinking_level: "+registry.ThinkingLevelHigh+"\n    supports_function_calling: true\n"+effort),
		"the doc says only the anthropic combination is rejected: another style with function calling loads")
	glm := "    thinking_style: " + registry.ThinkingStyleGLM + "\n"
	// TD internal/reconcile/thinking_doc_test.go:65: the doc's "or a thinking_level
	// under qwen" half of thinking-on must actually load.
	require.NoError(t, load("    thinking_level: "+registry.ThinkingLevelHigh+"\n    preserve_thinking: "+registry.ThinkingOn+"\n"+style),
		"the doc says preserve_thinking is legal with a thinking_level alone under qwen")
	for _, s := range []string{style, glm} {
		require.NoError(t, load("    thinking: "+registry.ThinkingOn+"\n    preserve_thinking: "+registry.ThinkingOn+"\n"+s),
			"the doc says preserve_thinking loads under qwen or glm with thinking on")
	}
	// TD-019: the rejection check loops over EVERY non-preserve style, not just
	// anthropic, so a new style added to the registry without preserve support
	// is caught here too.
	for _, s := range registry.ThinkingStyles() {
		if slices.Contains(registry.PreserveThinkingStyles(), s) {
			continue
		}
		require.ErrorContainsf(t,
			load("    thinking: "+registry.ThinkingOn+"\n    preserve_thinking: "+registry.ThinkingOn+"\n    thinking_style: "+s+"\n"),
			"has no preserve_thinking", "the doc says preserve_thinking is rejected under %s", s)
	}
	require.ErrorContains(t, load("    thinking: "+registry.ThinkingOff+"\n    preserve_thinking: "+registry.ThinkingOn+"\n"+style),
		"thinking is not on", "the doc says preserve_thinking needs thinking on")
	require.ErrorContains(t, load("    thinking_level: "+registry.ThinkingLevelLow+"\n    thinking_style: "+registry.ThinkingStyleTemplateKwargs+"\n"),
		`"template_kwargs" has no level`, "the doc says a level under template_kwargs is rejected at load")
	// TD internal/reconcile/thinking_doc_test.go:53: the glm half of that sentence
	// is a claim about the loader too.
	require.ErrorContains(t, load("    thinking_level: "+registry.ThinkingLevelLow+"\n"+glm),
		`"glm" has no level`, "the doc says the template_kwargs level rejection holds under glm")
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
			{"`preserve_thinking: bool` when `preserve_thinking` is set", "the qwen preserved-thinking field"},
		},
		registry.ThinkingStyleTemplateKwargs: {
			{"`chat_template_kwargs: {\"enable_thinking\": bool}`", "the template_kwargs style's only field"},
			{"no level", "a level under template_kwargs is rejected at load"},
		},
		registry.ThinkingStyleReasoningEffort: {
			{"`reasoning_effort`", "the reasoning_effort style's field"},
			{"`max` is sent as `high`", "high is the most the style accepts"},
			{"no off value", "thinking: off is a load error under this style"},
			{"set a `thinking_level` instead", "the row states the constraint without prescribing low, which epic 35.16.11.2.2.2 measured silencing nemotron-3-super-120b"},
			{"see **Thinking and `max_tokens`**", "the row cross-references the note that says which level is safe"},
		},
		registry.ThinkingStyleAnthropic: {
			{"`thinking: {\"type\": \"enabled\", \"budget_tokens\": N}`", "the anthropic style's on shape"},
			{"`thinking: {\"type\": \"disabled\"}`", "the anthropic style's off shape"},
			{"sends no `temperature`", "Anthropic rejects extended thinking at any temperature but 1"},
		},
		registry.ThinkingStyleGLM: {
			{"`thinking: {\"type\": \"enabled\"}`", "the glm style's on shape, with no budget"},
			{"`thinking: {\"type\": \"disabled\"}`", "the glm style's off shape"},
			{"no level", "a level under glm is rejected at load"},
			{"keeps its `temperature`", "only anthropic drops the temperature"},
			{"`\"clear_thinking\":false` when `preserve_thinking` is `on`", "the glm preserved-thinking field is inverted, compact as the wire bytes"},
			{"`\"clear_thinking\":true` when it is `off`", "the glm off value is inverted too, compact as the wire bytes"},
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
		// TD: since the anthropic hard-constraint check, a budget not below
		// max_tokens is a LOAD ERROR for anthropic (Anthropic rejects
		// budget_tokens >= max_tokens); only qwen's advisory budget reaches
		// warnThinkingBudget. The line must not read as a blanket warning.
		{"fails to load under `anthropic`", "config.go rejects budget_tokens >= max_tokens at load for the anthropic style"},
		{"loads with a warning under `qwen`", "only the qwen style's advisory budget reaches warnThinkingBudget"},
	})
}

// TD (registry.md:243): the replay contract must be discoverable without
// reading the thinking table — an operator whose agents declare no thinking
// keys has no reason to open that row, yet replay changes their turn-2+
// request body too. A top-level subsection beside **Safety:** carries the
// contract, and the thinking row cross-references it.
func TestRegistryDoc_ReasoningReplaySubsection(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	// Anchor on the line that STARTS the subsection — the thinking row's
	// cross-reference also contains the marker, earlier in the file.
	var replay string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "**Reasoning replay.**") {
			replay = line
			break
		}
	}
	require.NotEmpty(t, replay, "docs/registry.md has no top-level **Reasoning replay.** subsection")
	assertStates(t, "reasoning replay subsection", replay, []struct{ token, why string }{
		{"re-sends provider reasoning on every later turn", "the contract, stated for operators who never configured thinking"},
		{"whether or not `thinking` is declared", "not gated by any thinking key"},
		{"changes the turn-2+ request body for all tool-enabled agents", "the blast radius: every tool-loop roster, not just thinking ones"},
	})
	// The thinking row keeps its full contract text and points here, so a
	// reader who arrives via the table still finds the top-level statement.
	require.Contains(t, docRow(t, doc, "`thinking`"), "**Reasoning replay.**", "the thinking row must cross-reference the top-level replay subsection")
}

// TD-018: the replay-shape key list in the `thinking` row is not restated
// here as typed literals — it is reflected off llmclient.Message's reasoning
// json tags (as TestReasoningKeys_ReadFromMessage in internal/fanout does), so
// a member added, renamed, or removed without a doc update fails this test.
func TestRegistryDoc_ReplayShapeKeysMatchMessage(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	plain := map[string]bool{"role": true, "content": true, "tool_calls": true, "tool_call_id": true}
	var keys []string
	typ := reflect.TypeOf(llmclient.Message{})
	for i := 0; i < typ.NumField(); i++ {
		if k := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]; k != "" && !plain[k] {
			keys = append(keys, k)
		}
	}
	require.NotEmpty(t, keys, "Message must carry reasoning members")

	thinking := docRow(t, doc, "`thinking`")
	marker := "in the shape the provider returned it ("
	start := strings.Index(thinking, marker)
	require.GreaterOrEqual(t, start, 0, "the thinking row must state the replay shape")
	clause := thinking[start+len(marker):]
	if end := strings.Index(clause, ")"); end >= 0 {
		clause = clause[:end]
	}
	documented := map[string]bool{}
	for _, token := range strings.Split(clause, "`") {
		if strings.Contains(token, "reasoning") || strings.Contains(token, "thinking") {
			documented[token] = true
		}
	}
	for _, k := range keys {
		require.Containsf(t, documented, k, "the thinking row's replay-shape list must name llmclient.Message member %q exactly", k)
	}
	for d := range documented {
		require.Containsf(t, keys, d, "the thinking row documents %q, which is not a reasoning member of llmclient.Message", d)
	}
}

// TD-020: the `preserve_thinking` row and the glm style row must spell
// clear_thinking the same way, and that way must be the wire bytes llmclient
// actually sends (compact JSON — Go's encoding/json emits no spaces), so the
// style table cannot document a body the provider never receives.
func TestRegistryDoc_GLMClearThinkingSpellingMatchesWire(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	// Go's encoding/json emits compact JSON with no spaces, and llmclient's
	// thinking tests pin those wire bytes — the docs must spell the field the
	// way the provider actually receives it, in both places it appears.
	compact, spaced := `"clear_thinking":false`, `"clear_thinking": false`
	row := docRow(t, doc, "`preserve_thinking`")
	require.Contains(t, row, compact, "the preserve_thinking row must carry the compact wire spelling")
	require.NotContains(t, row, spaced, "the preserve_thinking row must not carry a spaced variant")
	glmLine := docLineWith(t, styleTable(t, doc), "`glm`")
	require.Contains(t, glmLine, compact, "the glm style row must spell clear_thinking exactly as the wire does, matching the preserve_thinking row")
	require.NotContains(t, glmLine, spaced, "the glm style row must not carry a spaced variant")
}

// AC 07-01 Scenario 3: the max_tokens interaction.
func TestRegistryDoc_ThinkingMaxTokensNote(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	maxTokensNote := docLineWith(t, doc, "**Thinking and `max_tokens`.**")
	assertStates(t, "thinking and max_tokens note", maxTokensNote, []struct{ token, why string }{
		{"thinking tokens count against the output cap on most providers", "raising max_tokens alone does not stop a runaway thinker"},
		{"`thinking: off` is the first fix for a model that truncates with zero findings", "archer ran to about 100k tokens with no findings"},
		// Epic 35.16.11.2.2.2: the old remedy (thinking_level: low) silenced
		// nemotron-3-super-120b, and medium/high still truncated, so the note
		// names the measured fix instead.
		{"a lower level is not a safe fix", "thinking_level: low stops the review instead of the runaway"},
		{"answer `NO FINDINGS` in under 20 output tokens", "the measured silent-lane shape on nemotron-3-super-120b"},
		{"`medium` and `high` still truncated", "no reasoning_effort level fixed that model"},
		{"repoint the agent to a different model", "the fix the epic's probe matrix proved"},
	})
	require.NotContains(t, maxTokensNote, "use `thinking_level: low` instead", "the max_tokens note must not offer thinking_level: low as a remedy: it silences the lane")
	// Epic 35.16.11.2.2.2: thinking: off reaches only the reasoning channel; the
	// content-channel runaway needs a persona fix, and JSON mode is not one.
	assertStates(t, "prose-in-reply note", docLineWith(t, doc, "**Thinking and prose in the reply.**"), []struct{ token, why string }{
		{"`thinking: off` stops the reasoning channel only", "archer and llm-large still planned in the reply with thinking off"},
		{"No thinking key reaches that channel", "the doctor honored verdict does not predict a clean review"},
		{"forbids analysis in the reply", "the persona remedy the probe matrix proved"},
		{"close the array before stopping", "archer left a finding's JSON unclosed"},
		{"`response_format: json_object` is not a fix for this", "JSON mode answered whole chunks with an empty object"},
		{"`{\"findings\":[]}`", "the measured empty-review shape under JSON mode"},
		// Claim 4: the prose must WARN that JSON mode drops the persona output
		// rule, not merely describe the consequence after the fact.
		{"Warning: JSON mode swaps the persona's `## Output Format` section", "the JSON-mode consequence must be framed as a warning before it is explained"},
		{"declaring `response_format: json_object` also drops this rule", "JSON mode swaps the persona's ## Output Format section at render time, so the persona fix is lost silently"},
	})
	// Sprint 35.16.11.2.2.1: LiteLLM's modify_params hides a missing-reasoning
	// failure instead of raising it, so the doc names the silent failure mode.
	assertStates(t, "modify_params warning", docLineWith(t, doc, "**Thinking and LiteLLM `modify_params`.**"), []struct{ token, why string }{
		{"`modify_params=True`", "the proxy setting that causes it"},
		{"silently turns thinking off for that turn", "the specific failure mode, not a generic caveat"},
		{"instead of returning the provider's 400", "the visible failure it replaces"},
		{"another style (for example `reasoning_effort`)", "the anthropic style is already rejected with function calling, so the risk is a Claude model under another style"},
		{"Neither atcr nor `atcr doctor` can see the proxy setting", "the operator must check the proxy; atcr cannot"},
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
	// TD-017: the probes carry the agent's preserve_thinking declaration and the
	// verdict detail names it, so the section must say so — and must say what a
	// single-turn probe can and cannot conclude about the flag.
	assertStates(t, "thinking verdict intro", intro, []struct{ token, why string }{
		{"carries the agent's `preserve_thinking` declaration", "the probe sends the flag (internal/doctor/run.go probe targets), matching the detail naming it"},
		{"a single-turn probe cannot verify it", "the flag's effect is on later tool-loop turns, which the probe never reaches"},
		{"retried without the flag before changing the style", "a 4xx on a flagged probe may be the flag, not the thinking declaration"},
	})
	// TD: the intro's no-verdict rule must cover permanent failures too — run.go
	// gates the verdict on thinkingProbeWorthwhile, so 401/403/404/transport rows
	// placed a call and still get none. The JSON schema line claims the thinking_*
	// fields are present whenever the probe placed a call, which is necessary but
	// not sufficient; both places must name the permanent-failure classes.
	assertStates(t, "thinking verdict intro", intro, []struct{ token, why string }{
		{"failed permanently", "run.go:294 gates the verdict on thinkingProbeWorthwhile: permanent failures get no verdict despite placing a call"},
	})
	assertStates(t, "doctor JSON schema", docLineWith(t, doc, "`thinking_status` (`"), []struct{ token, why string }{
		{"did not fail permanently", "a placed call is necessary but not sufficient: auth_failed, not_found, and network_error rows get no thinking fields"},
	})
	assertStates(t, "thinking verdict warning line", docLineWith(t, section, "The HINT column labels"), []struct{ token, why string }{
		{"one warning line for each declared polarity of `" + doctor.ThinkingNotHonored + "`", "the not-honored remedy differs by polarity (TD cli/doctor.go:255)"},
		{"one warning line for `" + doctor.ThinkingUnverified + "`", "--json prints no warning lines and honored prints none"},
	})
	// Row keys are the exact docRow keys (backticks included), so the split
	// not_honored rows can be pinned separately (TD cli/doctor.go:255).
	rows := map[string][]struct{ token, why string }{
		"`" + doctor.ThinkingHonored + "`": {
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
		"`" + doctor.ThinkingUnverified + "`": {
			{"may not report reasoning at all", "silence on both calls cannot be told apart from a provider that cannot report"},
			{"cut off before any signal showed", "a cut-off reply with no signal reaches no verdict"},
		},
	}
	for status, must := range rows {
		assertStates(t, "thinking verdict row "+status, docRow(t, section, status), must)
	}
	// TD: transport errors and empty replies classify as network_error, which
	// thinkingProbeWorthwhile excludes — those rows get NO verdict (the thinking
	// tests expect "" for them), so the unverified row must not list them as
	// unverified causes and must name the permanent-failure no-verdict classes.
	unverified := docRow(t, section, "`"+doctor.ThinkingUnverified+"`")
	require.NotContains(t, unverified, "transport error", "transport errors get no verdict (network_error is excluded by thinkingProbeWorthwhile), not unverified")
	require.NotContains(t, unverified, "empty reply", "empty replies get no verdict (network_error is excluded by thinkingProbeWorthwhile), not unverified")
	assertStates(t, "thinking verdict unverified row", unverified, []struct{ token, why string }{
		{"Permanent failures", "auth_failed, not_found, and network_error repeat identically, so they get no verdict rather than unverified"},
	})
	assertStates(t, "doctor JSON schema", docLineWith(t, doc, "`thinking_status` (`"), []struct{ token, why string }{
		{"`thinking_status` (`" + doctor.ThinkingHonored + "`, `" + doctor.ThinkingNotHonored + "`, or `" + doctor.ThinkingUnverified + "`)", "the JSON field's values are the doctor constants"},
		{"`thinking_detail`", "the verdict's reason rides beside it in --json"},
		{"`thinking_declared`", "the declared polarity (off/on/level) rides beside the verdict so remedies can be split without parsing detail prose"},
	})
}

// preserveThinkingStylesPhrase builds the doc-row token from the accessor's
// live set (TD-019): "requires `thinking_style: qwen` or `thinking_style: glm`"
// for the current two, extending automatically if a third style ships.
func preserveThinkingStylesPhrase(styles []string) string {
	parts := make([]string, len(styles))
	for i, s := range styles {
		parts[i] = "`thinking_style: " + s + "`"
	}
	return strings.Join(parts, " or ")
}
