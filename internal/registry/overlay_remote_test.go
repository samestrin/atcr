package registry

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

// resetInsecureWarn swaps the insecure-http warning sink to buf and resets the
// one-time guard so a test can observe the warning deterministically.
func resetInsecureWarn(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prevWriter := insecureRegistryWarnWriter
	insecureRegistryWarnWriter = buf
	insecureRegistryWarnOnce = sync.Once{}
	t.Cleanup(func() {
		insecureRegistryWarnWriter = prevWriter
		insecureRegistryWarnOnce = sync.Once{}
	})
}

// TestFetchRemoteRegistry_Unreachable_HardError proves a set-but-unreachable URL
// is an unconditional error with NO silent fallback to the local file: the local
// path here is valid and readable, yet the load still fails because the URL wins
// and the fetch cannot connect.
func TestFetchRemoteRegistry_Unreachable_HardError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/registry.yaml"
	srv.Close() // now nothing is listening -> connection refused, fast

	t.Setenv("ATCR_REGISTRY_URL", url)
	localOK := writeRegistry(t, validRegistry) // readable, but must NOT be used

	_, err := LoadRegistry(localOK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ATCR_REGISTRY_URL")
	// No silent fallback: the local-file "registry not found" path was not taken,
	// and the readable local file was not loaded.
	assert.NotContains(t, err.Error(), "registry not found at")
}

// TestFetchRemoteRegistry_Non200_HardError covers server-error and 404 statuses:
// each is a hard error naming the status, never a fallback.
func TestFetchRemoteRegistry_Non200_HardError(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, status, "nope"))
			_, err := LoadRegistry(writeRegistry(t, validRegistry))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unexpected status")
		})
	}
}

// TestFetchRemoteRegistry_MalformedYAML_HardError proves a reachable-but-invalid
// remote body is a parse error (not a fallback), attributed to a remote label
// derived from the URL path.
func TestFetchRemoteRegistry_MalformedYAML_HardError(t *testing.T) {
	t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, http.StatusOK, "providers: [this is: not valid"))
	_, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry.yaml")
}

// TestFetchRemoteRegistry_EmptyBody_HardError proves an empty remote body yields
// the same "is empty" guidance a local empty file would.
func TestFetchRemoteRegistry_EmptyBody_HardError(t *testing.T) {
	t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, http.StatusOK, "   \n"))
	_, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty")
}

// TestFetchRemoteRegistry_RejectsNonHTTPScheme proves a non-http(s) URL is
// rejected before any network access.
func TestFetchRemoteRegistry_RejectsNonHTTPScheme(t *testing.T) {
	t.Setenv("ATCR_REGISTRY_URL", "ftp://example.com/registry.yaml")
	_, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an http or https URL")
}

// TestFetchRemoteRegistry_MalformedURL proves an unparseable URL is a clear
// error, not a panic or a fallback.
func TestFetchRemoteRegistry_MalformedURL(t *testing.T) {
	t.Setenv("ATCR_REGISTRY_URL", "://missing-scheme")
	_, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ATCR_REGISTRY_URL")
}

// TestFetchRemoteRegistry_BodyLimit proves an oversized response is rejected
// rather than read unbounded (DoS guard).
func TestFetchRemoteRegistry_BodyLimit(t *testing.T) {
	prev := remoteRegistryBodyLimit
	remoteRegistryBodyLimit = 64
	t.Cleanup(func() { remoteRegistryBodyLimit = prev })

	oversized := strings.Repeat("# padding comment line\n", 100) // > 64 bytes
	t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, http.StatusOK, oversized))
	_, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")
}

// TestWarnInsecureRegistryURL_OnceAndRedacted proves the non-https warning fires
// exactly once and strips embedded credentials from the shown URL.
func TestWarnInsecureRegistryURL_OnceAndRedacted(t *testing.T) {
	var buf bytes.Buffer
	resetInsecureWarn(t, &buf)

	warnInsecureRegistryURLOnce("http://user:supersecret@team.example/registry.yaml")
	warnInsecureRegistryURLOnce("http://user:supersecret@team.example/registry.yaml")

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "warning:"), "warning must fire once")
	assert.Contains(t, out, "team.example")
	assert.NotContains(t, out, "supersecret", "password must be redacted")
}

// TestRedactRegistryURL strips userinfo and is a no-op for a clean URL.
func TestRedactRegistryURL(t *testing.T) {
	assert.Equal(t, "https://team.example/registry.yaml",
		redactRegistryURL("https://user:pw@team.example/registry.yaml"))
	assert.Equal(t, "https://team.example/registry.yaml",
		redactRegistryURL("https://team.example/registry.yaml"))
}

// TestRemoteRegistryLabel derives a file label from the URL path, ignoring the
// query string, and falls back to the standard user label.
func TestRemoteRegistryLabel(t *testing.T) {
	assert.Equal(t, "registry.yaml", remoteRegistryLabel("https://h/team/registry.yaml?token=abc"))
	assert.Equal(t, "custom.yaml", remoteRegistryLabel("https://h/custom.yaml"))
	assert.Equal(t, userRegistryLabel, remoteRegistryLabel("https://h/"))
	assert.Equal(t, userRegistryLabel, remoteRegistryLabel("https://h"))
}

// TestFetchRemoteRegistry_HTTPSNoWarning proves an https URL does not draw the
// insecure warning.
func TestFetchRemoteRegistry_HTTPSNoWarning(t *testing.T) {
	var buf bytes.Buffer
	resetInsecureWarn(t, &buf)
	// url.Parse succeeds; the fetch itself will fail (nothing listening), but the
	// scheme check runs first and must not warn for https.
	_, _ = fetchRemoteRegistry("https://127.0.0.1:1/registry.yaml")
	assert.Empty(t, buf.String(), "https must not warn")
}
