package registry

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveRegistry starts an httptest server that returns body with the given
// status for every request, and registers cleanup. It returns the URL of a
// registry.yaml served by it.
func serveRegistry(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/registry.yaml"
}

// TestParseRegistryFile_RemoteURL_Loads proves that when ATCR_REGISTRY_URL is
// set, the user registry is fetched over HTTP and the local path is ignored
// (here it points at a non-existent file, so a successful load can only have
// come from the remote source).
func TestParseRegistryFile_RemoteURL_Loads(t *testing.T) {
	url := serveRegistry(t, http.StatusOK, validRegistry)
	t.Setenv("ATCR_REGISTRY_URL", url)

	bogusLocal := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	reg, err := LoadRegistry(bogusLocal)
	require.NoError(t, err)
	assert.Contains(t, reg.Providers, "openai")
	assert.Contains(t, reg.Agents, "bruce")
	// Every entry is stamped with the user tier, exactly as a local load.
	assert.Equal(t, SourceUser, reg.AgentTier("bruce"))
}

// TestParseRegistryFile_URLUnset_ReadsLocal is the control: with the env var
// unset, loading reads the local file as before.
func TestParseRegistryFile_URLUnset_ReadsLocal(t *testing.T) {
	reg, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.NoError(t, err)
	assert.Contains(t, reg.Providers, "openai")
}
