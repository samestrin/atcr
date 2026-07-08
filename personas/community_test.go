package personas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// jaccardStopwords is the small stopword set dropped before the AC 04-07
// token-set similarity comparison, so shared grammar words don't inflate J.
var jaccardStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "you": true, "your": true, "is": true,
	"are": true, "and": true, "or": true, "of": true, "to": true, "in": true,
	"it": true, "that": true, "this": true, "with": true, "for": true, "on": true,
	"as": true, "be": true, "no": true, "not": true, "into": true, "its": true,
}

// tokenSet lowercases text, splits on non-alphanumeric runs, drops stopwords, and
// returns the deduplicated token set (AC 04-07 locked metric).
func tokenSet(text string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if jaccardStopwords[tok] {
			continue
		}
		set[tok] = struct{}{}
	}
	return set
}

// jaccard returns |A ∩ B| / |A ∪ B| for two token sets.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// indexEntry mirrors the JSON shape of internal/personas.PersonaIndexEntry. It is
// re-declared locally rather than imported to avoid an import cycle: this test
// lives in package personas, and internal/personas imports package personas.
type indexEntry struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Tasks       []string `json:"tasks"`
	Tags        []string `json:"tags"`
}

// readCommunityIndex decodes personas/community/index.json into entries.
func readCommunityIndex(t *testing.T) []indexEntry {
	t.Helper()
	raw, err := os.ReadFile(communityPath("index.json"))
	require.NoError(t, err, "community index.json must exist and be readable")
	var entries []indexEntry
	require.NoError(t, json.Unmarshal(raw, &entries), "community index.json must be valid JSON")
	return entries
}

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

// sectionBody returns the text of the markdown section introduced by heading, up
// to the next "## " heading (or end of file). Used to anchor a contract check to
// the section it belongs in, rather than accepting it anywhere in the file.
func sectionBody(text, heading string) string {
	i := strings.Index(text, heading)
	if i < 0 {
		return ""
	}
	rest := text[i+len(heading):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// markerRenderContext populates every persona template variable with a
// distinctive marker so a test can prove each required field's VALUE actually
// reaches the rendered output — a source-text token in a dead {{if false}} branch
// or a comment would pass a presence check yet never render.
func markerRenderContext(diff string) payload.PayloadContext {
	return payload.PayloadContext{
		AgentName:   "MARKER_AGENT",
		BaseRef:     "MARKER_BASE",
		HeadRef:     "MARKER_HEAD",
		FileCount:   4242,
		PayloadMode: "MARKER_MODE",
		Payload:     diff,
		ScopeRule:   "MARKER_SCOPE",
	}
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
			require.NotContainsf(t, out, "{{", "persona %q left an unrendered open action", p.Slug)
			require.NotContainsf(t, out, "}}", "persona %q left a stray close action", p.Slug)
			require.Containsf(t, out, "tester", "persona %q did not render AgentName into output", p.Slug)
		})
	}
}

// validSlug mirrors internal/registry.validateName's rules (persona.go): a
// resolvable persona slug is non-empty, carries no path separator, no `..`
// segment, no leading dot, and is not the reserved `_base`. Replicated here
// because validateName is unexported; the community render/resolve tests need the
// same guarantee that every library slug is safely resolvable.
func validSlug(slug string) bool {
	if slug == "" || slug == "_base" || strings.HasPrefix(slug, ".") {
		return false
	}
	if strings.ContainsAny(slug, `/\`) {
		return false
	}
	for _, seg := range strings.Split(slug, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// TestCommunityPersonas_SlugConsistency covers AC 04-04 Scenario 4: for each
// library persona the YAML `name` equals the slug equals the `.md` basename, and
// the slug passes the resolver's validateName rules. (The index `path`-stem
// cross-check is enforced by the AC 04-05 index-registration gate.)
func TestCommunityPersonas_SlugConsistency(t *testing.T) {
	for _, p := range communityPersonas {
		t.Run(p.Slug, func(t *testing.T) {
			require.Truef(t, validSlug(p.Slug), "slug %q must pass validateName rules", p.Slug)

			raw, err := os.ReadFile(communityPath(p.Slug + ".yaml"))
			require.NoErrorf(t, err, "read yaml %s", p.Slug)
			var meta struct {
				Name string `yaml:"name"`
			}
			require.NoErrorf(t, yaml.Unmarshal(raw, &meta), "parse yaml %s", p.Slug)
			require.Equalf(t, p.Slug, meta.Name, "YAML name must equal the slug for %q", p.Slug)

			_, err = os.Stat(communityPath(p.Slug + ".md"))
			require.NoErrorf(t, err, "prompt template community/%s.md must exist", p.Slug)
		})
	}
}

// TestCommunityPersonas_Differentiation covers AC 04-07 Scenario 1 / Error 1: no
// pair of personas has combined ## Role+## Focus token-set Jaccard above the
// locked 0.85 threshold, evidencing genuine per-model task scoping rather than one
// generic list restated ten times. Runs over all C(10,2)=45 pairs.
func TestCommunityPersonas_Differentiation(t *testing.T) {
	sets := make(map[string]map[string]struct{}, len(communityPersonas))
	for _, p := range communityPersonas {
		raw, err := os.ReadFile(communityPath(p.Slug + ".md"))
		require.NoErrorf(t, err, "read prompt %s", p.Slug)
		text := string(raw)
		combined := sectionBody(text, "## Role") + " " + sectionBody(text, "## Focus")
		sets[p.Slug] = tokenSet(combined)
		require.NotEmptyf(t, sets[p.Slug], "persona %q has empty Role+Focus token set", p.Slug)
	}
	const threshold = 0.85
	for i := 0; i < len(communityPersonas); i++ {
		for j := i + 1; j < len(communityPersonas); j++ {
			a, b := communityPersonas[i].Slug, communityPersonas[j].Slug
			jac := jaccard(sets[a], sets[b])
			require.LessOrEqualf(t, jac, threshold,
				"personas %q and %q are too similar: Jaccard(Role+Focus)=%.3f > %.2f", a, b, jac, threshold)
		}
	}
}

// TestCommunityPersonas_DistinctCategories is a categorical anti-duplication
// guard complementing the AC-locked Jaccard gate (whose 0.85 threshold is loose
// vs the observed ~0.168 — see TD-009): the 10 personas' finding-category words
// must all be distinct, so a "same lens, renamed target" duplicate the loose
// Jaccard would miss is caught here.
func TestCommunityPersonas_DistinctCategories(t *testing.T) {
	seen := make(map[string]string, len(communityPersonas))
	for _, p := range communityPersonas {
		if other, dup := seen[p.Category]; dup {
			t.Fatalf("personas %q and %q share category %q — lenses must be distinct", other, p.Slug, p.Category)
		}
		seen[p.Category] = p.Slug
	}
	require.Lenf(t, seen, len(communityPersonas), "every persona must have a distinct category")
}

// TestCommunityPersonas_DistinctTaskScoping covers AC 04-07 DoD: each persona
// carries a distinct primary task tag in the index, so the library spans distinct
// review lenses rather than repeating one.
func TestCommunityPersonas_DistinctTaskScoping(t *testing.T) {
	entries := readCommunityIndex(t)
	seen := make(map[string]string, len(entries))
	for _, e := range entries {
		require.NotEmptyf(t, e.Tasks, "persona %q must carry a task tag", e.Name)
		primary := e.Tasks[0]
		if other, dup := seen[primary]; dup {
			t.Fatalf("personas %q and %q share primary task %q — lenses must be differentiated", other, e.Name, primary)
		}
		seen[primary] = e.Name
	}
}

// TestCommunityIndex_Registration covers AC 04-05: the in-repo community index
// registers exactly one entry per authored persona, discoverable by its bound
// model. It asserts (Scenario 1) one entry per persona with path→a real file;
// (Scenario 2) index provider/model equal the persona YAML; (Scenario 4) the
// index is non-empty; (Scenario 5) slug consistency name==stem(path)==<slug>.md
// and validateName; (Edge 2) tasks/tags populated; (Scenario 3) each persona is
// discoverable by the structured model vendor token.
func TestCommunityIndex_Registration(t *testing.T) {
	entries := readCommunityIndex(t)
	require.NotEmpty(t, entries, "community index.json must not be an empty [] array once personas are authored")

	byStem := make(map[string]indexEntry, len(entries))
	for _, e := range entries {
		stem := strings.TrimSuffix(e.Path, ".yaml")
		byStem[stem] = e
	}
	require.Lenf(t, byStem, len(communityPersonas),
		"expected exactly one index entry per authored persona (%d)", len(communityPersonas))

	for _, p := range communityPersonas {
		t.Run(p.Slug, func(t *testing.T) {
			e, ok := byStem[p.Slug]
			require.Truef(t, ok, "persona %q has no index entry", p.Slug)

			// Scenario 5: slug consistency + validateName.
			require.Equalf(t, p.Slug, e.Name, "index name must equal slug for %q", p.Slug)
			require.Equalf(t, p.Slug+".yaml", e.Path, "index path must be <slug>.yaml for %q", p.Slug)
			require.Truef(t, validSlug(p.Slug), "slug %q must pass validateName rules", p.Slug)

			// Edge Case 1 / Error 1: path resolves to a committed file.
			_, err := os.Stat(communityPath(e.Path))
			require.NoErrorf(t, err, "index path %q must resolve to a committed YAML", e.Path)

			// Scenario 2: provider/model/description match the persona YAML exactly.
			var ym struct {
				Provider    string `yaml:"provider"`
				Model       string `yaml:"model"`
				Description string `yaml:"description"`
			}
			yraw, err := os.ReadFile(communityPath(p.Slug + ".yaml"))
			require.NoErrorf(t, err, "read yaml %s", p.Slug)
			require.NoError(t, yaml.Unmarshal(yraw, &ym))
			require.Equalf(t, ym.Provider, e.Provider, "provider drift for %q", p.Slug)
			require.Equalf(t, ym.Model, e.Model, "model drift for %q", p.Slug)
			require.Equalf(t, ym.Description, e.Description, "description drift for %q", p.Slug)
			require.NotEmptyf(t, e.Model, "index model must be non-empty for %q", p.Slug)
			// Pin the routing key: every community persona routes through openrouter,
			// never a vendor-named provider (LOCKED Q3 / Phase 5 clarifications).
			require.Equalf(t, "openrouter", e.Provider, "index provider must be the openrouter routing key for %q", p.Slug)

			// Grouping key: the vendor token lives in model, never provider.
			require.Containsf(t, strings.ToLower(e.Model), p.VendorToken,
				"model %q must carry vendor token %q", e.Model, p.VendorToken)

			// Edge Case 2: task-scoped personas carry tasks/tags.
			require.NotEmptyf(t, e.Tasks, "persona %q must carry at least one task tag", p.Slug)
			require.NotEmptyf(t, e.Tags, "persona %q must carry at least one tag", p.Slug)

			// Scenario 3: discoverable by the structured model field (substring,
			// case-insensitive — mirrors SearchWithOptions Model filtering).
			var found bool
			for _, cand := range entries {
				if strings.Contains(strings.ToLower(cand.Model), p.VendorToken) && cand.Name == p.Slug {
					found = true
					break
				}
			}
			require.Truef(t, found, "persona %q must be discoverable via its model vendor token %q", p.Slug, p.VendorToken)
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

			// Anchor the contract to its own section: the header line AND the
			// "7 pipe-delimited columns" rule text must live inside ## Output
			// Format, not merely somewhere in the file.
			outputSection := sectionBody(text, "## Output Format")
			require.Containsf(t, outputSection, canonicalOutputContract,
				"persona %q ## Output Format must carry the 7-column contract byte-for-byte", p.Slug)
			require.Containsf(t, outputSection, "7 pipe-delimited columns",
				"persona %q ## Output Format must keep the one-per-line / 7-column rule text", p.Slug)

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
			require.NotContainsf(t, out, "{{", "persona %q left an unrendered open action (ToolsEnabled=%v)", p.Slug, tools)
			require.NotContainsf(t, out, "}}", "persona %q left a stray close action (ToolsEnabled=%v)", p.Slug, tools)
		}
	}
}

// TestCommunityPersonas_RequiredValuesRender proves each required template
// variable's VALUE actually reaches the rendered output (AC 04-03 Scenario 5
// strengthened): a token buried in a dead {{if false}} branch or a comment would
// satisfy the source-text presence check yet never render, so here every field is
// given a distinctive marker value and the render must surface all of them.
func TestCommunityPersonas_RequiredValuesRender(t *testing.T) {
	const sampleDiff = "MARKER_DIFF_PAYLOAD"
	wantValues := []string{
		"MARKER_AGENT", "MARKER_SCOPE", "4242",
		"MARKER_BASE", "MARKER_HEAD", "MARKER_MODE", sampleDiff,
	}
	for _, p := range communityPersonas {
		t.Run(p.Slug, func(t *testing.T) {
			raw, err := os.ReadFile(communityPath(p.Slug + ".md"))
			require.NoErrorf(t, err, "read prompt %s", p.Slug)
			out, err := payload.RenderPrompt(string(raw), markerRenderContext(sampleDiff))
			require.NoErrorf(t, err, "RenderPrompt(%q)", p.Slug)
			for _, want := range wantValues {
				require.Containsf(t, out, want,
					"persona %q rendered output is missing a required field value %q", p.Slug, want)
			}
		})
	}
}
