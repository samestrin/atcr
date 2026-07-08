package personas

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
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
