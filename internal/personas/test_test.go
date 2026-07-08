package personas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	builtins "github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssertBoundModel_RejectsEmpty exercises AC 06-03 Error Scenario 1: a
// community/library persona reaching the fixture runner with a blank bound model
// fails with a clear, attributable error naming the persona and the missing
// field, distinct from the template-unrendered failure path.
func TestAssertBoundModel_RejectsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		persona string
		model   string
		wantErr bool
	}{
		{"populated", "delia", "deepseek/deepseek-v4-pro", false},
		{"empty", "brokenpersona", "", true},
		{"whitespace-only", "brokenpersona", "   ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertBoundModel(tc.persona, tc.model)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.persona)
				require.Contains(t, err.Error(), "bound model")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestCommunityModel_ReturnsBoundModel asserts the embedded community YAML's
// structured `model` field is reachable for the fixture runner's assertion, and
// that an unknown slug errors. It asserts the `provider/model` SHAPE (non-empty,
// namespaced) rather than a literal model id, which churns as models are repinned.
func TestCommunityModel_ReturnsBoundModel(t *testing.T) {
	model, err := builtins.CommunityModel("delia")
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(model))
	require.Contains(t, model, "/", "expected a namespaced provider/model id")

	_, err = builtins.CommunityModel("not-a-real-persona")
	require.Error(t, err)
}

// TestRunFixture_CommunityAssertsBoundModel asserts every embedded community
// persona both passes its render fixture AND carries a non-empty bound model in
// structured metadata (the AC7 authoring-contract gate, AC 06-03 Scenario 2/3).
func TestRunFixture_CommunityAssertsBoundModel(t *testing.T) {
	names := builtins.CommunityNames()
	require.NotEmpty(t, names, "expected embedded community personas")
	r := TemplateFixtureRunner{}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			out, err := r.RunFixture(name)
			require.NoError(t, err)
			require.Truef(t, out.HasFixture, "community persona %q should resolve a fixture", name)
			require.Equalf(t, out.Total, out.Passed, "community persona %q fixture must pass", name)

			model, err := builtins.CommunityModel(name)
			require.NoError(t, err)
			require.NotEmptyf(t, strings.TrimSpace(model),
				"community persona %q must bind a non-empty model in structured metadata", name)
		})
	}
}

// TestRunFixture_BuiltinExemptFromModelAssertion asserts a built-in persona with
// a fixture (sasha) still runs its template-render check unchanged and is NOT
// subjected to the community bound-model assertion — built-ins are model-agnostic
// per C2 (they carry no provider/model), so the assertion is SKIPPED for them,
// not asserted to pass (AC 06-03 Scenario 1 / Edge Case 2).
func TestRunFixture_BuiltinExemptFromModelAssertion(t *testing.T) {
	r := TemplateFixtureRunner{}
	out, err := r.RunFixture("sasha")
	require.NoError(t, err)
	require.True(t, out.HasFixture)
	require.Equal(t, 1, out.Passed)
	require.Equal(t, 1, out.Total)
}

// TestRenderFixture_PassesWhenPayloadContainsBraces asserts that a fixture diff
// containing literal double-brace syntax (common in the code these personas
// review) does not falsely fail the render check. The render result includes the
// payload verbatim, so scanning the output for "{{" is unsound; RenderPrompt's
// own error-on-missing-key behavior plus an AgentName interpolation check are
// sufficient to verify the template rendered.
func TestRenderFixture_PassesWhenPayloadContainsBraces(t *testing.T) {
	text := "## Role\nReviewer {{.AgentName}}\n## Focus\n{{.Payload}}\n"
	patch := "code with {{ }} braces"
	out, err := renderFixture("test-persona", text, patch)
	require.NoError(t, err)
	assert.True(t, out.HasFixture)
	assert.Equal(t, 1, out.Passed)
	assert.Equal(t, 1, out.Total)
}

// TestTemplateFixtureRunner_PersonasDirReadsInstalledUnit verifies the
// PersonasDir seam resolves a community persona from an on-disk installed unit
// before falling back to the embedded library copy.
func TestTemplateFixtureRunner_PersonasDirReadsInstalledUnit(t *testing.T) {
	md, err := builtins.CommunityGet("delia")
	require.NoError(t, err)
	model, err := builtins.CommunityModel("delia")
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "delia.md"), []byte(md), 0o644))
	yaml := "provider: openrouter\nmodel: " + model + "\nrole: reviewer\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "delia.yaml"), []byte(yaml), 0o644))

	runner := TemplateFixtureRunner{
		PersonasDir: func() (string, error) { return dir, nil },
	}
	out, err := runner.RunFixture("delia")
	require.NoError(t, err)
	require.True(t, out.HasFixture)
	require.Equal(t, 1, out.Passed)
}
