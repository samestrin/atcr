package fanout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Story 4 (Sprint 35.16.11.2.1): a review agent declaring response_format:
// json_object gets one shared ## Output Format block in place of its persona's
// fenced-array section; every undeclared agent's prompt stays byte-identical.

// payloadHeadingSentinel is diff text that repeats the heading, so a locator that
// matched anything but the FIRST occurrence would corrupt the payload.
const payloadHeadingSentinel = "+## Output Format\n+PAYLOAD-SENTINEL-KEEP-ME\n"

// allPersonaPrompts returns the 24 embedded persona templates: _base.md, the
// registered personas, and the community library (mirrors personas.allPrompts).
func allPersonaPrompts(t *testing.T) map[string]string {
	t.Helper()
	prompts := map[string]string{}
	base, err := personas.Base()
	require.NoError(t, err)
	prompts["_base.md"] = base
	for _, name := range personas.Names() {
		text, err := personas.Get(name)
		require.NoErrorf(t, err, "Get(%q)", name)
		prompts[name+".md"] = text
	}
	for _, name := range personas.CommunityNames() {
		text, err := personas.CommunityGet(name)
		require.NoErrorf(t, err, "CommunityGet(%q)", name)
		prompts["community/"+name+".md"] = text
	}
	require.Len(t, prompts, 24, "precondition: the swap must be proven over all 24 persona files")
	return prompts
}

// outputFormatSection returns the ## Output Format section (heading included) up
// to the next "\n## " heading, matching personas/community_test.go's sectionBody.
func outputFormatSection(text string) string {
	i := strings.Index(text, outputFormatHeading)
	if i < 0 {
		return ""
	}
	rest := text[i:]
	if j := strings.Index(rest[len(outputFormatHeading):], "\n## "); j >= 0 {
		return rest[:len(outputFormatHeading)+j]
	}
	return rest
}

func renderPersona(t *testing.T, text string) string {
	t.Helper()
	out, err := payload.RenderPrompt(text, payload.PayloadContext{
		AgentName:   "tester",
		BaseRef:     "main",
		HeadRef:     "feature",
		FileCount:   1,
		PayloadMode: string(payload.ModeBlocks),
		Payload:     payloadHeadingSentinel,
		ScopeRule:   payload.ScopeRule(payload.ModeBlocks),
	})
	require.NoError(t, err)
	return out
}

func TestJSONObjectOutputFormat_Content(t *testing.T) {
	block := jsonObjectOutputFormat
	require.True(t, strings.HasPrefix(block, outputFormatHeading+"\n"), "the shared block replaces the section heading too")
	assert.Contains(t, block, "JSON", "json_object mode requires a message to mention JSON")
	assert.Contains(t, block, `{"findings":[...]}`)
	assert.Contains(t, block, `{"findings":[]}`)
	assert.Contains(t, block, `"severity", "file_line", "problem", "fix", "category", "est_minutes", "evidence"`,
		"the shared block keeps the persona sections' seven-key contract")
	assert.NotContains(t, block, "```", "json_object mode sends no code fence")
	assert.NotContains(t, block, "NO FINDINGS", "json_object mode cannot emit the plain-text sentinel")
	assert.NotContains(t, block, "\n## ", "the block must be one section, so the next heading still ends it")
	assert.NotContains(t, block, "{{", "the swap runs on rendered text; a template action would never render")

	// The worked example must be readable by the unchanged 35.16.11.2 parser, and
	// the clean reply the block asks for must read as a clean review.
	ex := block[strings.Index(block, "Example:"):]
	got := stream.ParseModelOutput([]byte(ex))
	require.Len(t, got, 1, "ParseModelOutput must read the shared block's worked example")
	assert.Equal(t, "HIGH", got[0].Severity)
	assert.True(t, stream.IsNoFindings(`{"findings":[]}`), "the clean reply the block asks for must be a clean review")
}

// AC 04-01: heading-located swap across all 24 personas; undeclared byte-identical.
func TestSwapOutputFormatSection_AllPersonas(t *testing.T) {
	for _, toolsEnabled := range []bool{false, true} {
		for file, text := range allPersonaPrompts(t) {
			t.Run(fmt.Sprintf("%s/tools=%t", file, toolsEnabled), func(t *testing.T) {
				ctx := payload.PayloadContext{
					AgentName:    "tester",
					BaseRef:      "main",
					HeadRef:      "feature",
					FileCount:    1,
					PayloadMode:  string(payload.ModeBlocks),
					Payload:      payloadHeadingSentinel,
					ScopeRule:    payload.ScopeRule(payload.ModeBlocks),
					ToolsEnabled: toolsEnabled,
				}
				rendered, err := payload.RenderPrompt(text, ctx)
				require.NoError(t, err)
				i := strings.Index(rendered, outputFormatHeading)
				require.GreaterOrEqualf(t, i, 0, "precondition: %s carries the heading", file)
				original := outputFormatSection(rendered)
				require.Contains(t, original, "```json", "precondition: the persona section asks for the fenced array")

				start := strings.Index(rendered, payloadHeadingSentinel)
				require.Greater(t, start, i, "precondition: the payload follows the persona's section")
				// The production locator (review.go's renderedPayloadStart), not just a
				// strings.Index guess: a shipped persona that made the locator fail safe
				// to 0 would drop every swap to the append path — both the fenced-array
				// and the JSON-object contracts sent at once (the D4 edge case).
				require.Equal(t, start, renderedPayloadStart(rendered, text, ctx),
					"the production payload locator must agree with the sentinel position")

				// Undeclared: byte-identical.
				require.Equal(t, rendered, promptForResponseFormat(rendered, start, ""))

				// Declared: the section is exactly the shared block.
				swapped := promptForResponseFormat(rendered, start, registry.ResponseFormatJSONObject)
				section := outputFormatSection(swapped)
				assert.Equal(t, jsonObjectOutputFormat, section, "the declared section must be exactly the shared block")
				assert.NotContains(t, section, "```json")
				assert.NotContains(t, section, "NO FINDINGS")

				// Only the section changed: prefix and everything from the next heading on
				// (payload included, with its own copy of the heading) are untouched.
				assert.Equal(t, rendered[:i], swapped[:i], "text before the section must be unchanged")
				assert.Equal(t, rendered[i+len(original):], swapped[i+len(jsonObjectOutputFormat):],
					"text after the section must be unchanged")
				assert.Equal(t, 1, strings.Count(swapped, "PAYLOAD-SENTINEL-KEEP-ME"))
				assert.Equal(t, strings.Count(rendered, outputFormatHeading), strings.Count(swapped, outputFormatHeading),
					"the payload's copy of the heading must survive; only the first match is swapped")
			})
		}
	}
}

func TestSwapOutputFormatSection_NestedSubheadingAndLastSection(t *testing.T) {
	nested := "## Role\nr\n\n## Output Format\nold\n### Sub\nstill old\n\n## Payload\nP\n"
	assert.Equal(t, "## Role\nr\n\n"+jsonObjectOutputFormat+"\n## Payload\nP\n", swapOutputFormatSection(nested, len(nested)),
		"a ### subheading stays inside the replaced span; only a ## heading ends it")

	last := "## Role\nr\n\n## Output Format\nold\n### Sub\nold"
	assert.Equal(t, "## Role\nr\n\n"+jsonObjectOutputFormat, swapOutputFormatSection(last, len(last)),
		"with no following ## heading the section runs to end of prompt")
}

// AC 04-02: a declared persona with no ## Output Format heading gets the block
// appended, silently; undeclared is unchanged.
func TestSwapOutputFormatSection_NoHeadingAppends(t *testing.T) {
	custom := "## Role\nYou review code.\n\n## Payload\nsome diff\n"

	var got string
	stderr := captureStderr(t, func() { got = promptForResponseFormat(custom, len(custom), registry.ResponseFormatJSONObject) })
	assert.Empty(t, stderr, "the append path is the intended fallback and warns nothing")
	assert.True(t, strings.HasPrefix(got, custom), "the rest of the prompt is unchanged")
	assert.True(t, strings.HasSuffix(got, jsonObjectOutputFormat), "the shared block is appended at the end")

	assert.Equal(t, custom, promptForResponseFormat(custom, len(custom), ""))

	// A near-miss heading is not the heading: it falls through to the append path.
	for _, nearMiss := range []string{
		"## Role\nr\n\n##Output Format\nold\n",
		"## Role\nr\n\n### Output Format\nold\n",
		"## Role\nr\n\n## Output Formatting\nold\n",
	} {
		got = promptForResponseFormat(nearMiss, len(nearMiss), registry.ResponseFormatJSONObject)
		assert.Truef(t, strings.HasPrefix(got, nearMiss), "%q must be left intact", nearMiss)
		assert.Truef(t, strings.HasSuffix(got, jsonObjectOutputFormat), "%q is not the heading, so the block is appended", nearMiss)
	}
}

// Adversarial review 3.2.A: the swap must never read or rewrite the payload.
func TestSwapOutputFormatSection_NeverTouchesPayload(t *testing.T) {
	// A persona with no heading whose DIFF carries one, as a line of its own.
	prefix := "## Role\nr\n\n## Payload\n"
	diff := "## Output Format\nremoved-line\n## Next\nkept\n"
	got := swapOutputFormatSection(prefix+diff, len(prefix))
	assert.Equal(t, prefix+diff+"\n"+jsonObjectOutputFormat, got, "the diff is unchanged and the block appended")

	// A persona whose section runs straight into the payload, with no heading between.
	prefix = "## Role\nr\n\n## Output Format\nold rules\n"
	diff = "body\n## Heading in a markdown file\nmore\n"
	got = swapOutputFormatSection(prefix+diff, len(prefix))
	assert.Equal(t, "## Role\nr\n\n"+jsonObjectOutputFormat+diff, got, "the section ends where the payload begins")
}

// The same guarantee through renderAgent, whose payloadStart is derived from the
// rendered payload rather than passed in.
func TestRenderAgent_NoHeadingPersonaKeepsHeadingInDiff(t *testing.T) {
	cfg := swapRoster(registry.ResponseFormatJSONObject, "")
	persona := registry.ResolvedPersona{Text: "## Role\nr\n\n## Payload\n{{.Payload}}\n"}
	diff := "## Output Format\n+secret-line\n"

	a, err := renderAgent(cfg, "greta", cfg.Registry.Agents["greta"], persona, "blocks", diff, 1, payload.Truncation{}, ReviewRange{}, "", agentSizing{})
	require.NoError(t, err)
	un := a.swap.rebuildUnswapped(a.Prompt)
	assert.Equal(t, un+"\n"+jsonObjectOutputFormat, a.Prompt, "the swapped prompt is the unswapped text plus the appended block")
	assert.Contains(t, a.Prompt, diff, "the diff reaches the model whole")
}

// Every persona's Reasoning Budget still says "the single ```json array"; the
// block must say it replaces that.
func TestJSONObjectOutputFormat_SupersedesEarlierArrayWording(t *testing.T) {
	assert.Contains(t, jsonObjectOutputFormat, "replaces any earlier instruction in this prompt to emit a fenced JSON array")
}

// swapRoster is two plain review agents, greta falling back to kai, with each
// agent's response_format set as given.
func swapRoster(gretaRF, kaiRF string) *ReviewConfig {
	cfg := twoAgentConfig("http://unused")
	g := cfg.Registry.Agents["greta"]
	g.Fallback = "kai"
	g.ResponseFormat = gretaRF
	cfg.Registry.Agents["greta"] = g
	k := cfg.Registry.Agents["kai"]
	k.ResponseFormat = kaiRF
	cfg.Registry.Agents["kai"] = k
	cfg.Project.Agents = []string{"greta"}
	return cfg
}

func isSwapped(prompt string) bool {
	return outputFormatSection(prompt) == jsonObjectOutputFormat
}

// AC 04-01 wiring: renderAgent swaps on its own ac, feeds both Prompt and the
// Invocation, and records the pre-swap text.
func TestRenderAgent_SwapsOnDeclaredFlag(t *testing.T) {
	payloads := map[string]modePayload{"blocks": {Text: "diff", FileCount: 1}}
	rng := ReviewRange{Base: "a", Head: "b"}

	undeclared, _, err := buildOneAgent(swapRoster("", ""), "greta", payloads, rng, "", "")
	require.NoError(t, err)
	declared, _, err := buildOneAgent(swapRoster(registry.ResponseFormatJSONObject, ""), "greta", payloads, rng, "", "")
	require.NoError(t, err)

	assert.False(t, isSwapped(undeclared.Prompt))
	assert.Equal(t, undeclared.Prompt, undeclared.swap.rebuildUnswapped(undeclared.Prompt), "no swap: the unswapped text IS the prompt")

	assert.True(t, isSwapped(declared.Prompt))
	assert.Equal(t, declared.Prompt, declared.Invocation.Prompt, "the wire prompt is the swapped prompt")
	assert.Equal(t, undeclared.Prompt, declared.swap.rebuildUnswapped(declared.Prompt), "the pre-swap text is recoverable for the fallback")
	assert.NotEqual(t, undeclared.CacheKey, declared.CacheKey)
}

// AC 04-02 Edge Case 1: scope focus comes first, the appended block last.
func TestRenderAgent_ScopeFocusThenAppendedBlock(t *testing.T) {
	cfg := swapRoster(registry.ResponseFormatJSONObject, "")
	ac := cfg.Registry.Agents["greta"]
	ac.Scope = []string{"security"}
	persona := registry.ResolvedPersona{Text: "## Role\nr\n\n## Payload\n{{.Payload}}\n"}

	a, err := renderAgent(cfg, "greta", ac, persona, "blocks", "diff", 1, payload.Truncation{}, ReviewRange{}, "", agentSizing{})
	require.NoError(t, err)
	focus := payload.ScopeFocus(ac.Scope)
	require.NotEmpty(t, focus)
	assert.Less(t, strings.Index(a.Prompt, focus), strings.Index(a.Prompt, jsonObjectOutputFormat))
	assert.True(t, strings.HasSuffix(a.Prompt, jsonObjectOutputFormat))
}

// AC 04-03 Scenarios 1-2 and Edge Case 1: the fallback swaps on its OWN flag.
func TestBuildFallbackAgent_SwapKeyedOnOwnFlag(t *testing.T) {
	payloads := map[string]modePayload{"blocks": {Text: "diff", FileCount: 1}}
	rng := ReviewRange{Base: "a", Head: "b"}
	cases := []struct {
		name           string
		gretaRF, kaiRF string
	}{
		{"undeclared primary, declared fallback", "", registry.ResponseFormatJSONObject},
		{"declared primary, undeclared fallback", registry.ResponseFormatJSONObject, ""},
		{"both declared", registry.ResponseFormatJSONObject, registry.ResponseFormatJSONObject},
		{"neither declared", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := swapRoster(tc.gretaRF, tc.kaiRF)
			primary, _, err := buildOneAgent(cfg, "greta", payloads, rng, "", "")
			require.NoError(t, err)
			fb, _, err := buildFallbackAgent(cfg, primary, "kai", true, fallbackRefit{})
			require.NoError(t, err)

			assert.Equal(t, tc.gretaRF != "", isSwapped(primary.Prompt), "primary follows its own flag")
			assert.Equal(t, tc.kaiRF != "", isSwapped(fb.Prompt), "fallback follows its own flag")
			assert.Equal(t, fb.Prompt, fb.Invocation.Prompt)
			assert.Equal(t, primary.swap.rebuildUnswapped(primary.Prompt), fb.swap.rebuildUnswapped(fb.Prompt),
				"the no-refit fallback reviews the primary's payload")
			if tc.kaiRF == "" {
				assert.Equal(t, primary.swap.rebuildUnswapped(primary.Prompt), fb.Prompt,
					"an undeclared fallback's prompt is the primary's unswapped text")
			}
		})
	}
}

// AC 04-03 Scenario 3 and Edge Case 2: the truncate re-fit re-renders under the
// PRIMARY's config, so the fallback must re-key the swap on its own flag.
func TestBuildFallbackAgent_RefitSwapKeyedOnOwnFlag(t *testing.T) {
	cases := []struct {
		name           string
		gretaRF, kaiRF string
	}{
		{"undeclared primary, declared fallback", "", registry.ResponseFormatJSONObject},
		{"declared primary, undeclared fallback", registry.ResponseFormatJSONObject, ""},
		{"neither declared", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := refitRoster(t, 128000, OverflowTruncate)
			g := cfg.Registry.Agents["greta"]
			g.ResponseFormat = tc.gretaRF
			cfg.Registry.Agents["greta"] = g
			k := cfg.Registry.Agents["kai"]
			k.ResponseFormat = tc.kaiRF
			cfg.Registry.Agents["kai"] = k

			slot := buildRefitSlot(t, cfg)
			primary, fb := slot.Primary, slot.Fallbacks[0]
			require.Equal(t, degradationTruncate, fb.DegradationAction, "precondition: the re-fit arm was taken")

			assert.Equal(t, tc.gretaRF != "", isSwapped(primary.Prompt), "primary follows its own flag")
			assert.Equal(t, tc.kaiRF != "", isSwapped(fb.Prompt), "re-fit fallback follows its own flag, not refit.primaryConfig's")
			assert.Equal(t, fb.Prompt, fb.Invocation.Prompt)
			assert.Less(t, len(fb.swap.rebuildUnswapped(fb.Prompt)), len(primary.swap.rebuildUnswapped(primary.Prompt)),
				"the re-fit payload is smaller")
			if tc.kaiRF == "" {
				assert.Equal(t, fb.swap.rebuildUnswapped(fb.Prompt), fb.Prompt)
				assert.Contains(t, outputFormatSection(fb.Prompt), "```json", "an undeclared re-fit keeps the fenced contract")
			}
		})
	}
}

func TestRenderedPayloadStart(t *testing.T) {
	ctx := payload.PayloadContext{Payload: "diff"}
	render := func(tmpl string) string {
		out, err := payload.RenderPrompt(tmpl, ctx)
		require.NoError(t, err)
		return out
	}

	// The payload text also appears in the persona's own words, earlier.
	tmpl := "copy it from the diff\n## Payload\n{{.Payload}}\ntail"
	prompt := render(tmpl)
	assert.Equal(t, strings.Index(prompt, "## Payload\n")+len("## Payload\n"), renderedPayloadStart(prompt, tmpl, ctx))

	// A persona that never renders the payload fails safe to 0 (append-only): a
	// missing marker is indistinguishable from an escaper having rewritten it, and
	// len(prompt) would let the swap search the whole prompt including the diff.
	tmpl = "## Output Format\nold\n"
	prompt = render(tmpl)
	assert.Equal(t, 0, renderedPayloadStart(prompt, tmpl, ctx))

	// A prompt that does not match its template yields 0, so the swap only appends.
	assert.Equal(t, 0, renderedPayloadStart("unrelated", "## Payload\n{{.Payload}}", ctx))

	// A template that fails to render (payload.RenderPrompt error) fails safe to 0
	// as well — a corrupted template must never send the swap searching the prompt.
	assert.Equal(t, 0, renderedPayloadStart("anything", "## Payload\n{{.Payload", ctx))
}

// A template that renders the payload through an escaper (printf %q) must not
// blind the payload locator: with the marker lost, the old len(prompt) fallback
// let the swap search — and rewrite — the diff itself, so a diff carrying a
// markdown "## Output Format" line (atcr's own docs do) reached the model with
// its heading swapped out. The diff must reach the model unchanged.
func TestRenderedPayloadStart_EscapedPayloadKeepsTheDiffWhole(t *testing.T) {
	diff := "## Output Format\n+secret-line\n"
	ctx := payload.PayloadContext{Payload: diff}
	tmpl := "## Role\nr\n\n{{printf \"%q\" .Payload}}"
	prompt, err := payload.RenderPrompt(tmpl, ctx)
	require.NoError(t, err)

	// End to end: a declared agent whose persona lacks the section, reviewing a
	// diff that itself contains the heading, gets the shared block APPENDED —
	// byte-for-byte prompt plus block — never a rewritten diff.
	swapped, span := swapOutputFormatSectionWithSpan(prompt, renderedPayloadStart(prompt, tmpl, ctx))
	assert.True(t, span.appended, "the swap must be append-only when the payload boundary cannot be trusted")
	assert.Equal(t, prompt+"\n"+jsonObjectOutputFormat, swapped, "the render is untouched; the block is appended")
	// The diff content survives verbatim in its escaped form — %q rewrites the
	// newlines, but the heading clause and the secret line are still there for the
	// model, and the shared block never displaced them.
	assert.Contains(t, swapped, `## Output Format\n+secret-line\n`)
}
