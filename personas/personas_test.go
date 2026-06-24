package personas

import (
	"os"
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
		"sentinel", "tracer", "idiomatic", "otto",
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

func TestBase(t *testing.T) {
	s, err := Base()
	require.NoError(t, err)
	require.NotEmpty(t, s)
}

// TestGet_BonusPersonasNonEmpty confirms each of the three bonus personas
// resolves to a non-empty embedded template (AC 01-01 Scenario 2).
func TestGet_BonusPersonasNonEmpty(t *testing.T) {
	for _, name := range []string{"sentinel", "tracer", "idiomatic"} {
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
	for _, name := range []string{"sentinel", "tracer", "idiomatic"} {
		text, err := Get(name)
		require.NoErrorf(t, err, "Get(%q)", name)
		out, err := payload.RenderPrompt(text, renderContext("<sample diff>"))
		require.NoErrorf(t, err, "RenderPrompt(%q)", name)
		require.NotContainsf(t, out, "{{", "persona %q left an unrendered action", name)
		require.Containsf(t, out, "tester", "persona %q should render AgentName", name)
	}
}

// fixtureTest renders personaName against its committed .patch fixture and
// asserts the rendered prompt names the expected finding category. Rendering is
// pure template execution: no LLM, no network — the category keyword is authored
// into the persona template itself (AC 01-03).
func fixtureTest(t *testing.T, personaName, fixturePath, wantCategory string) {
	t.Helper()
	diff, err := os.ReadFile(fixturePath)
	require.NoErrorf(t, err, "read fixture %s", fixturePath)
	text, err := Get(personaName)
	require.NoErrorf(t, err, "Get(%q)", personaName)
	out, err := payload.RenderPrompt(text, renderContext(string(diff)))
	require.NoErrorf(t, err, "RenderPrompt(%q)", personaName)
	require.Containsf(t, strings.ToLower(out), wantCategory,
		"persona %q output does not name category %q", personaName, wantCategory)
}

func TestSentinelFixture(t *testing.T) {
	fixtureTest(t, "sentinel", "testdata/sentinel_fixture.patch", "injection")
}

func TestTracerFixture(t *testing.T) {
	fixtureTest(t, "tracer", "testdata/tracer_fixture.patch", "n+1")
}

func TestIdiomaticFixture(t *testing.T) {
	fixtureTest(t, "idiomatic", "testdata/idiomatic_fixture.patch", "error")
}
