package personas

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
	"github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/require"
)

func TestNames_ReturnsAllNine(t *testing.T) {
	names := Names()
	require.Len(t, names, 9)
	require.Equal(t, []string{
		"bruce", "greta", "kai", "mira", "dax",
		"sasha", "penny", "ingrid", "otto",
	}, names)
}

func TestGet_KnownPersona(t *testing.T) {
	s, err := Get("bruce")
	require.NoError(t, err)
	require.NotEmpty(t, s)
}

func TestGet_UnknownPersona(t *testing.T) {
	_, err := Get("nonexistent")
	require.Error(t, err)
}

func TestIsRegistered_KnownAndUnknown(t *testing.T) {
	require.True(t, isRegistered("bruce"))
	require.True(t, isRegistered("otto"))
	require.False(t, isRegistered("nonexistent"))
	require.False(t, isRegistered(""))
}

func TestBase(t *testing.T) {
	s, err := Base()
	require.NoError(t, err)
	require.NotEmpty(t, s)
}

// TestEmbeddedFilesMatchNames verifies that the //go:embed *.md directive only
// captures the registered personas plus the shared _base.md template. A stray
// markdown file or a missing persona template becomes a build/test failure
// rather than a latent runtime internal-error.
func TestEmbeddedFilesMatchNames(t *testing.T) {
	want := expectedEmbeddedFiles()

	entries, err := files.ReadDir(".")
	require.NoError(t, err)
	got := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		got[e.Name()] = struct{}{}
	}
	require.Equal(t, want, got, "embedded .md files must exactly match registered personas plus _base.md")
}

// TestGet_BonusPersonasNonEmpty confirms each of the three bonus personas
// resolves to a non-empty embedded template (AC 01-01 Scenario 2).
func TestGet_BonusPersonasNonEmpty(t *testing.T) {
	for _, name := range []string{"sasha", "penny", "ingrid"} {
		s, err := Get(name)
		require.NoErrorf(t, err, "Get(%q)", name)
		require.NotEmptyf(t, s, "Get(%q) should be non-empty", name)
	}
}

// renderContext is the typed payload the persona templates render against. It
// mirrors the canonical PayloadContext used by internal/payload's own render
// test (the single source of truth for persona template variables).
func renderContext(diff string) payload.PayloadContext {
	return payload.PayloadContext{
		AgentName:   "tester",
		BaseRef:     "main",
		HeadRef:     "feature",
		FileCount:   1,
		PayloadMode: string(payload.ModeBlocks),
		Payload:     diff,
		ScopeRule:   payload.ScopeRule(payload.ModeBlocks),
	}
}

// TestBonusPersonas_TemplateRenders confirms each bonus persona parses and
// executes against PayloadContext with no unrendered template actions left.
func TestBonusPersonas_TemplateRenders(t *testing.T) {
	for _, name := range []string{"sasha", "penny", "ingrid"} {
		text, err := Get(name)
		require.NoErrorf(t, err, "Get(%q)", name)
		out, err := payload.RenderPrompt(text, renderContext("<sample diff>"))
		require.NoErrorf(t, err, "RenderPrompt(%q)", name)
		require.NotContainsf(t, out, "{{", "persona %q left an unrendered action", name)
		require.Containsf(t, out, "tester", "persona %q should render AgentName", name)
	}
}

// fixtureTest verifies a bonus persona's contract without an LLM or network.
// It (1) loads the committed .patch fixture (a missing/uncommitted fixture fails
// here), (2) asserts the expected finding category is authored into the persona
// TEMPLATE itself — checked against the raw template, not the rendered prompt, so
// a category word that merely appears in the injected diff cannot satisfy it —
// and (3) confirms the template renders cleanly with the fixture as the diff
// payload, leaving no unrendered template actions (AC 01-03).
func fixtureTest(t *testing.T, personaName, fixturePath, wantCategory string) {
	t.Helper()
	diff, err := os.ReadFile(fixturePath)
	require.NoErrorf(t, err, "read fixture %s", fixturePath)
	text, err := Get(personaName)
	require.NoErrorf(t, err, "Get(%q)", personaName)
	require.Containsf(t, strings.ToLower(text), wantCategory,
		"persona %q template does not name category %q", personaName, wantCategory)
	out, err := payload.RenderPrompt(text, renderContext(string(diff)))
	require.NoErrorf(t, err, "RenderPrompt(%q)", personaName)
	require.NotContainsf(t, out, "{{", "persona %q left an unrendered action", personaName)
}

func TestSashaFixture(t *testing.T) {
	fixtureTest(t, "sasha", "testdata/sasha_fixture.patch", "injection")
}

func TestPennyFixture(t *testing.T) {
	fixtureTest(t, "penny", "testdata/penny_fixture.patch", "n+1")
}

func TestIngridFixture(t *testing.T) {
	fixtureTest(t, "ingrid", "testdata/ingrid_fixture.patch", "error")
}

// workedExamples decodes every ```json fence inside a prompt's ## Output Format
// section into finding objects. It reads each value by key, so an example may
// list its keys in any order and quote a | or " inside a sibling value.
func workedExamples(text string) ([]map[string]any, error) {
	var out []map[string]any
	var block []string
	inFence := false
	for _, line := range strings.Split(sectionBody(text, "## Output Format"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inFence && trimmed == "```json":
			inFence, block = true, nil
		case inFence && trimmed == "```":
			inFence = false
			var objs []map[string]any
			if err := json.Unmarshal([]byte(strings.Join(block, "\n")), &objs); err != nil {
				return nil, err
			}
			out = append(out, objs...)
		case inFence:
			block = append(block, line)
		}
	}
	return out, nil
}

// allPrompts returns every embedded persona prompt keyed by its file name:
// _base.md, the registered personas, and the community library.
func allPrompts(t *testing.T) map[string]string {
	t.Helper()
	prompts := map[string]string{}

	base, err := Base()
	require.NoError(t, err)
	prompts["_base.md"] = base

	for _, name := range Names() {
		text, err := Get(name)
		require.NoErrorf(t, err, "Get(%q)", name)
		prompts[name+".md"] = text
	}
	for _, name := range CommunityNames() {
		text, err := CommunityGet(name)
		require.NoErrorf(t, err, "CommunityGet(%q)", name)
		prompts["community/"+name+".md"] = text
	}
	return prompts
}

// TestPersonaExamples_UseVocabularyCategories is the class guard for epic
// 35.16.4: every prompt now carries "Use CATEGORY from this closed vocabulary,
// spelled exactly as listed" (injected through {{.ScopeRule}}), so a worked
// example that demonstrates a non-member has the prompt contradicting itself in
// the one place models weight most heavily. Few-shot examples outweigh
// enumerations for most models, so such a reviewer keeps emitting the
// non-member word and stays unscoreable.
//
// It asserts the class rather than the known instances: any persona added later
// with an off-vocabulary example fails here. Only in-repo prompts are reachable
// (personas/*.md and personas/community/*.md); prompts installed outside the
// repo are covered by the same rule but cannot be checked by CI.
func TestPersonaExamples_UseVocabularyCategories(t *testing.T) {
	members := make(map[string]bool, len(reconcile.Categories()))
	for _, c := range reconcile.Categories() {
		members[c] = true
	}

	checked := 0
	for file, text := range allPrompts(t) {
		examples, err := workedExamples(text)
		require.NoErrorf(t, err, "%s worked example is not a valid JSON array of finding objects", file)
		for i, ex := range examples {
			cat, ok := ex["category"].(string)
			require.Truef(t, ok, "%s example %d has no string category key", file, i+1)
			checked++
			require.Truef(t, members[cat],
				"%s example %d emits CATEGORY %q, which is not a member of the closed vocabulary — "+
					"the same prompt tells the model to spell CATEGORY exactly as listed, so the example contradicts the rule",
				file, i+1, cat)
		}
	}

	// Non-vacuous: a layout change that stops the fence scan from finding the
	// examples would otherwise turn this guard into a silent pass.
	require.GreaterOrEqual(t, checked, 1+len(Names())+len(CommunityNames()),
		"expected at least one worked example per prompt — the fence scan found too few")
}

// pipeSwapRe catches a reworded pipe-to-slash rule ("replace | with /",
// "swap | for /") that the literal ban list would miss.
var pipeSwapRe = regexp.MustCompile(`(?i)\|\s*(with|for|to|by|into)\s*/`)

// TestPersonas_NoPipeContract is the Go form of the AC 01-05 sweep: no embedded
// prompt may still tell a model to replace | with / or to emit pipe-delimited
// columns. Either one corrupts code a finding quotes (bitwise OR, type unions,
// regex alternation, shell pipelines) before the parser ever sees it.
func TestPersonas_NoPipeContract(t *testing.T) {
	prompts := allPrompts(t)
	require.Len(t, prompts, 1+len(Names())+len(CommunityNames()))
	for file, text := range prompts {
		for _, banned := range []string{"literal |", "pipe-delimited", "SEVERITY|FILE:LINE"} {
			require.NotContainsf(t, text, banned, "%s still carries the pipe contract (%q)", file, banned)
		}
		require.NotRegexpf(t, pipeSwapRe, text, "%s still tells the model to swap | for /", file)
	}
}

// TestPersonas_OutputFormatIsUniformJSON checks that all 24 prompts (base plus
// 23 personas) declare the same fenced-JSON contract inside ## Output Format,
// and that each worked example parses through the producing parser into the
// finding it shows. A prompt whose example the parser cannot read would teach
// every model it serves to emit unparseable output.
func TestPersonas_OutputFormatIsUniformJSON(t *testing.T) {
	for file, text := range allPrompts(t) {
		section := sectionBody(text, "## Output Format")
		require.Containsf(t, section, canonicalOutputContract, "%s ## Output Format must carry the JSON key contract", file)
		require.Containsf(t, section, canonicalOutputRule, "%s ## Output Format must carry the fenced-JSON rule", file)
		require.Containsf(t, section, "NO FINDINGS", "%s ## Output Format must keep the clean-review marker", file)

		examples, err := workedExamples(text)
		require.NoErrorf(t, err, "%s worked example must be valid JSON", file)
		require.NotEmptyf(t, examples, "%s must show at least one worked example", file)

		got := stream.ParseModelOutput([]byte(section))
		require.Lenf(t, got, len(examples), "%s: ParseModelOutput must read every worked example", file)
		for i, ex := range examples {
			require.Equalf(t, ex["severity"], got[i].Severity, "%s example %d severity", file, i+1)
			require.Equalf(t, ex["category"], got[i].Category, "%s example %d category", file, i+1)
			require.Equalf(t, ex["problem"], got[i].Problem, "%s example %d problem", file, i+1)
			require.Equalf(t, ex["evidence"], got[i].Evidence, "%s example %d evidence", file, i+1)
			require.Equalf(t, ex["fix"], got[i].Fix, "%s example %d fix", file, i+1)
			require.Equalf(t, ex["file_line"], fmt.Sprintf("%s:%d", got[i].File, got[i].Line), "%s example %d file_line", file, i+1)
			est, ok := ex["est_minutes"].(float64)
			require.Truef(t, ok, "%s example %d: est_minutes must be a JSON number", file, i+1)
			require.Equalf(t, int(est), got[i].EstMinutes, "%s example %d est_minutes", file, i+1)
			require.Emptyf(t, got[i].Reviewer, "%s example %d: model output never carries a reviewer", file, i+1)
		}
	}
}

// goWordRe matches the standalone language name "go"/"Go" (whole word,
// case-insensitive) but not compound words like "goroutine" or "good".
var goWordRe = regexp.MustCompile(`(?i)\bgo\b`)

// TestIngridGeneralizedBeyondGo covers AC 05-02: ingrid's Role/Focus read as a
// language-agnostic idiomatic lens (no literal "Go" as the review target), and a
// NON-Go fixture (a Python swallowed-exception diff) exercises the generalized
// lens and passes — proving "generalized beyond Go" by an executed check, not
// prose. The original Go fixture (Edge Case 2) is still covered by TestIngridFixture.
func TestIngridGeneralizedBeyondGo(t *testing.T) {
	text, err := Get("ingrid")
	require.NoError(t, err)

	roleFocus := strings.ToLower(sectionBody(text, "## Role") + sectionBody(text, "## Focus"))
	require.NotRegexp(t, goWordRe, roleFocus,
		"ingrid Role/Focus must be language-agnostic — no literal 'Go' as the review target")
	// Beyond the bare word "go", ban Go-specific construct tokens so the lens is
	// framed generally (thread/coroutine, not goroutine; stdlib category, not strconv).
	for _, tok := range []string{"goroutine", "golang", "strconv", "defer ", "sync."} {
		require.NotContainsf(t, roleFocus, tok,
			"ingrid Role/Focus must not name the Go-specific construct %q", tok)
	}

	require.Contains(t, strings.ToLower(text), "error",
		"ingrid must still name a concrete idiomatic category (error handling)")

	diff, err := os.ReadFile("testdata/ingrid_lang2_fixture.patch")
	require.NoError(t, err, "non-Go fixture must exist")
	out, err := payload.RenderPrompt(text, renderContext(string(diff)))
	require.NoError(t, err, "generalized ingrid must render against a non-Go fixture")
	require.NotContains(t, out, "{{", "no unresolved template action against the non-Go fixture")
	// Non-vacuous: the Python fixture's payload must actually flow into the render,
	// proving the generalized lens is exercised against a non-Go sample.
	require.Contains(t, out, "except Exception",
		"the non-Go (Python) fixture payload must render into the prompt")
}
