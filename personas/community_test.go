package personas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

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
