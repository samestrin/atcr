package personas

import (
	"testing"

	builtins "github.com/samestrin/atcr/personas"
	"github.com/stretchr/testify/require"
)

// TestTemplateFixtureRunner_CommunityPersonasPass asserts the fixture runner
// resolves and passes every embedded community-library persona (AC 04-04): each
// resolves its co-located community/<slug>.md template and
// community/testdata/<slug>_fixture.patch fixture, renders cleanly with no
// leftover template actions, and reports one passing fixture case — no longer the
// pre-5.10 HasFixture:false short-circuit for non-built-in names.
func TestTemplateFixtureRunner_CommunityPersonasPass(t *testing.T) {
	names := builtins.CommunityNames()
	require.NotEmpty(t, names, "expected embedded community personas")
	r := TemplateFixtureRunner{}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			out, err := r.RunFixture(name)
			require.NoError(t, err)
			require.Truef(t, out.HasFixture, "community persona %q should resolve a fixture", name)
			require.Equalf(t, out.Total, out.Passed, "community persona %q fixture must pass", name)
			require.Equalf(t, 1, out.Passed, "community persona %q: expected exactly one passing fixture case", name)
		})
	}
}
