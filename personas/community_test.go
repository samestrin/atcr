package personas

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

// canonicalOutputContract is the exact 7-column pipe-delimited output header the
// reconciler parses byte-for-byte; every persona's ## Output Format block must
// carry it verbatim (AC 04-03 Scenario 2 / docs/personas-authoring.md §2).
const canonicalOutputContract = "SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE"

// requiredTemplateTokens are the variables the renderer relies on; a persona
// template that never mentions one renders cleanly yet drops a required field, so
// AC 04-03 Scenario 5 asserts each token is literally present in the source text.
var requiredTemplateTokens = []string{
	"{{.AgentName}}", "{{.ScopeRule}}", "{{.FileCount}}",
	"{{.BaseRef}}", "{{.HeadRef}}", "{{.PayloadMode}}", "{{.Payload}}",
}

// vendorGuidanceRe matches the machine-checkable vendor-guidance citation marker
// with a non-empty value (AC 04-03 Scenario 3).
var vendorGuidanceRe = regexp.MustCompile(`(?m)<!--\s*vendor-guidance:\s*(\S.*?)\s*-->`)

// communityPersona describes one authored community-library persona. Slug is the
// lowercased human name and the shared basename of its <slug>.yaml, <slug>.md,
// and testdata/<slug>_fixture.patch. VendorToken is the token that MUST appear in
// the persona's bound model id (the load-bearing grouping key — discovery is by
// model, never by provider, which is always the openrouter routing key). Category
// is the single lowercase finding-category word its fixture plants and its prompt
// template must name (fixture-integrity contract from docs/personas-authoring.md).
type communityPersona struct {
	Slug        string
	VendorToken string // claude|gpt|gemini|deepseek|qwen|kimi|glm
	Tier        string // flagship|fallback|open
	Category    string // single lowercase category word
}

// communityPersonas is the authoritative roster of the model-indexed library.
// Frontier vendors ship a flagship+fallback pair (AC 04-01); flat-rate open
// models ship one persona each (AC 04-02). The open-model rows are appended in
// task 5.4.
var communityPersonas = []communityPersona{
	{Slug: "anthony", VendorToken: "claude", Tier: "flagship", Category: "coupling"},
	{Slug: "sonny", VendorToken: "claude", Tier: "fallback", Category: "logic"},
	{Slug: "gene", VendorToken: "gpt", Tier: "flagship", Category: "contract"},
	{Slug: "milo", VendorToken: "gpt", Tier: "fallback", Category: "validation"},
	{Slug: "gia", VendorToken: "gemini", Tier: "flagship", Category: "race"},
	{Slug: "flint", VendorToken: "gemini", Tier: "fallback", Category: "leak"},
	{Slug: "delia", VendorToken: "deepseek", Tier: "open", Category: "complexity"},
	{Slug: "quinn", VendorToken: "qwen", Tier: "open", Category: "type"},
	{Slug: "celeste", VendorToken: "kimi", Tier: "open", Category: "dependency"},
	{Slug: "glenna", VendorToken: "glm", Tier: "open", Category: "observability"},
}

const communityDir = "community"

// communityPath joins elem under the on-disk community persona directory.
func communityPath(elem ...string) string {
	return filepath.Join(append([]string{communityDir}, elem...)...)
}

// TestCommunityPersonas_FixtureAndPromptCategory is the per-persona fixture
// contract for the library (mirrors the built-in fixtureTest): (1) the persona's
// category word is authored into the prompt TEMPLATE itself — not merely present
// in the injected fixture diff, so a leaked word cannot satisfy it; (2) a committed
// <slug>_fixture.patch exists in community/testdata/; (3) the template renders
// cleanly against the fixture payload with no unrendered {{ }} actions.
func TestCommunityPersonas_FixtureAndPromptCategory(t *testing.T) {
	for _, p := range communityPersonas {
		t.Run(p.Slug, func(t *testing.T) {
			promptPath := communityPath(p.Slug + ".md")
			text, err := os.ReadFile(promptPath)
			require.NoErrorf(t, err, "read prompt %s", promptPath)

			require.Containsf(t, strings.ToLower(string(text)), p.Category,
				"persona %q template must name its category word %q (not leak it from the diff)", p.Slug, p.Category)

			fixturePath := communityPath("testdata", p.Slug+"_fixture.patch")
			diff, err := os.ReadFile(fixturePath)
			require.NoErrorf(t, err, "read fixture %s", fixturePath)

			out, err := payload.RenderPrompt(string(text), renderContext(string(diff)))
			require.NoErrorf(t, err, "RenderPrompt(%q)", p.Slug)
			require.NotContainsf(t, out, "{{", "persona %q left an unrendered template action", p.Slug)
		})
	}
}

// TestCommunityPersonas_PromptStructure enforces the AC 04-03 source-text
// contract on every persona template (Scenarios 2, 3, 5): all required template
// tokens are literally present (a render can't catch an omitted token), the
// mandatory ## Role and ## Output Format headings are present, the exact 7-column
// output contract appears byte-for-byte, and exactly one non-empty vendor-guidance
// citation is present.
func TestCommunityPersonas_PromptStructure(t *testing.T) {
	for _, p := range communityPersonas {
		t.Run(p.Slug, func(t *testing.T) {
			raw, err := os.ReadFile(communityPath(p.Slug + ".md"))
			require.NoErrorf(t, err, "read prompt %s", p.Slug)
			text := string(raw)

			for _, tok := range requiredTemplateTokens {
				require.Containsf(t, text, tok, "persona %q template is missing required token %s", p.Slug, tok)
			}
			require.Containsf(t, text, "## Role", "persona %q missing mandatory ## Role heading", p.Slug)
			require.Containsf(t, text, "## Output Format", "persona %q missing mandatory ## Output Format heading", p.Slug)
			require.Containsf(t, text, canonicalOutputContract,
				"persona %q ## Output Format must carry the 7-column contract byte-for-byte", p.Slug)

			matches := vendorGuidanceRe.FindAllStringSubmatch(text, -1)
			require.Lenf(t, matches, 1, "persona %q must carry exactly one vendor-guidance citation", p.Slug)
			require.NotEmptyf(t, strings.TrimSpace(matches[0][1]),
				"persona %q vendor-guidance citation must be non-empty", p.Slug)
		})
	}
}

// TestCommunityPersonas_RendersInBothToolStates covers AC 04-03 Edge Case 1: the
// optional {{if .ToolsEnabled}} block renders cleanly with tools on and off,
// leaving no unrendered actions and raising no execution error in either state.
func TestCommunityPersonas_RendersInBothToolStates(t *testing.T) {
	for _, p := range communityPersonas {
		raw, err := os.ReadFile(communityPath(p.Slug + ".md"))
		require.NoErrorf(t, err, "read prompt %s", p.Slug)
		for _, tools := range []bool{true, false} {
			ctx := renderContext("<sample diff>")
			ctx.ToolsEnabled = tools
			out, err := payload.RenderPrompt(string(raw), ctx)
			require.NoErrorf(t, err, "RenderPrompt(%q) ToolsEnabled=%v", p.Slug, tools)
			require.NotContainsf(t, out, "{{", "persona %q left an unrendered action (ToolsEnabled=%v)", p.Slug, tools)
		}
	}
}
