package personas

import (
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// TestPersonasDir_EqualsResolverRegistryDir covers AC 01-06 Edge Case 3: the
// community install dir MUST equal the resolver's Registry personas dir
// (filepath.Dir(DefaultRegistryPath())/personas) on every OS, so a fetched
// persona lands on the resolution chain. On darwin these historically differed
// (os.UserConfigDir() = ~/Library/Application Support vs. the resolver's
// hardcoded ~/.config), which would strand a fetched persona off the chain.
func TestPersonasDir_EqualsResolverRegistryDir(t *testing.T) {
	got, err := PersonasDir()
	require.NoError(t, err)

	regPath, err := registry.DefaultRegistryPath()
	require.NoError(t, err)
	want := filepath.Join(filepath.Dir(regPath), "personas")

	require.Equal(t, want, got, "install dir must equal the resolver's Registry personas dir")
}
