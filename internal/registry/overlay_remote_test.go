package registry

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain neutralizes ATCR_REGISTRY_URL from the ambient environment so this
// package's many registry-loading tests stay hermetic — a dev or CI shell that
// happened to export it must not redirect unrelated tests to the network. Tests
// that exercise the remote path set it explicitly via t.Setenv.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("ATCR_REGISTRY_URL")
	os.Exit(m.Run())
}

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
	url := srv.URL + "/registry.yaml?token=leak-me-not"
	srv.Close() // now nothing is listening -> connection refused, fast

	t.Setenv("ATCR_REGISTRY_URL", url)
	localOK := writeRegistry(t, validRegistry) // readable, but must NOT be used

	_, err := LoadRegistry(localOK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ATCR_REGISTRY_URL")
	// No silent fallback: the local-file "registry not found" path was not taken,
	// and the readable local file was not loaded.
	assert.NotContains(t, err.Error(), "registry not found at")
	// The wrapped transport error must not leak a query-string token embedded in
	// the URL — the message names only the env var and the underlying cause.
	assert.NotContains(t, err.Error(), "leak-me-not")
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

	insecure := "http://user:supersecret@team.example/registry.yaml?token=tok-secret"
	warnInsecureRegistryURLOnce(insecure)
	warnInsecureRegistryURLOnce(insecure)

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "warning:"), "warning must fire once")
	assert.Contains(t, out, "team.example")
	assert.NotContains(t, out, "supersecret", "password must be redacted")
	assert.NotContains(t, out, "tok-secret", "query-string token must be redacted")
}

// TestWarnInsecureRegistryURL_PerDistinctURL proves the warning is keyed per
// distinct insecure URL, so changing ATCR_REGISTRY_URL to a different plaintext
// registry still draws a warning rather than being silently accepted.
func TestWarnInsecureRegistryURL_PerDistinctURL(t *testing.T) {
	var buf bytes.Buffer
	resetInsecureWarn(t, &buf)

	warnInsecureRegistryURLOnce("http://a.example/registry.yaml")
	warnInsecureRegistryURLOnce("http://b.example/registry.yaml")
	warnInsecureRegistryURLOnce("http://a.example/registry.yaml") // duplicate, must not re-warn

	out := buf.String()
	assert.Equal(t, 2, strings.Count(out, "warning:"), "each distinct insecure URL must warn once")
}

// TestRedactRegistryURL strips userinfo, query, and fragment, and is a no-op for
// a clean URL.
func TestRedactRegistryURL(t *testing.T) {
	assert.Equal(t, "https://team.example/registry.yaml",
		redactRegistryURL("https://user:pw@team.example/registry.yaml"))
	assert.Equal(t, "https://team.example/registry.yaml",
		redactRegistryURL("https://team.example/registry.yaml"))
	// A token in the query string (or a fragment) must not survive redaction.
	assert.Equal(t, "https://team.example/registry.yaml",
		redactRegistryURL("https://user:pw@team.example/registry.yaml?token=secret#frag"))
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

// --- Task 2: validate/merge the remote registry + env-var-only key guarantee ---

// TestRemoteRegistry_MergesWithLocalProjectOverlay proves the remote user
// registry is a first-class input to the merged loader: a LOCAL project overlay
// (.atcr/registry.yaml) still merges over it, adding a project agent that binds a
// remote-defined user provider (no trust gate — the provider is user-defined).
func TestRemoteRegistry_MergesWithLocalProjectOverlay(t *testing.T) {
	t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, http.StatusOK, validRegistry))

	root := t.TempDir()
	writeProjectRegistry(t, root, `
agents:
  team-extra:
    provider: openai   # a provider defined only in the remote user registry
    model: gpt-4
`)
	bogusLocal := filepath.Join(root, "registry.yaml") // ignored: URL wins

	reg, err := LoadMergedRegistry(bogusLocal, root)
	require.NoError(t, err)
	// Remote user entries present…
	assert.Contains(t, reg.Agents, "bruce")
	assert.Equal(t, SourceUser, reg.AgentTier("bruce"))
	// …and the local project overlay merged over the remote base.
	assert.Contains(t, reg.Agents, "team-extra")
	assert.Equal(t, SourceProject, reg.AgentTier("team-extra"))
}

// TestRemoteRegistry_LiteralAPIKeyRejected proves a secret placed directly in the
// remote file cannot be loaded: the schema has no api_key field and decoding is
// strict, so the unknown field is a hard load error — the key is never read.
func TestRemoteRegistry_LiteralAPIKeyRejected(t *testing.T) {
	remote := `
providers:
  p:
    api_key_env: ATCR_TEST_REMOTE_KEY
    api_key: sk-should-never-be-read
agents:
  a:
    provider: p
    model: m
`
	t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, http.StatusOK, remote))
	_, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key", "the literal key field must be rejected as an unknown field")
}

// TestRemoteRegistry_KeyReferencedNotStored proves the env-var contract is
// unchanged across the remote path: the remote file carries only the api_key_env
// NAME, and loading succeeds whether or not that variable is set locally (the
// value is resolved from the local environment at invoke time, never at load).
func TestRemoteRegistry_KeyReferencedNotStored(t *testing.T) {
	remote := `
providers:
  p:
    api_key_env: ATCR_TEST_REMOTE_KEY
agents:
  a:
    provider: p
    model: m
`
	// Deliberately unset locally: load must still succeed.
	require.NoError(t, os.Unsetenv("ATCR_TEST_REMOTE_KEY"))
	t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, http.StatusOK, remote))

	reg, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.NoError(t, err)
	assert.Equal(t, "ATCR_TEST_REMOTE_KEY", reg.Providers["p"].APIKeyEnv)
}

// TestRemoteRegistry_ValidationRunsOverRemote proves schema validation runs over
// the fetched content, not just the local file: a dangling fallback in the remote
// registry fails the load exactly as a local one would.
func TestRemoteRegistry_ValidationRunsOverRemote(t *testing.T) {
	remote := `
providers:
  p:
    api_key_env: ATCR_TEST_REMOTE_KEY
agents:
  a:
    provider: p
    model: m
    fallback: does-not-exist
`
	t.Setenv("ATCR_REGISTRY_URL", serveRegistry(t, http.StatusOK, remote))
	_, err := LoadRegistry(writeRegistry(t, validRegistry))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}
