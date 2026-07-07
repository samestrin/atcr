package personas

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AC 01-01: RegistryBaseURL repointed to samestrin/atcr ------------------

// repointedBaseURL is the canonical in-repo community persona path on samestrin/atcr.
const repointedBaseURL = "https://raw.githubusercontent.com/samestrin/atcr/main/personas/community"

// TestRegistryBaseURL_RepointedValue covers AC 01-01 Scenario 3: the constant equals
// the samestrin/atcr in-repo community path byte-for-byte.
func TestRegistryBaseURL_RepointedValue(t *testing.T) {
	assert.Equal(t, repointedBaseURL, RegistryBaseURL)
}

// TestBaseURL_DefaultResolvesToRepointedURL covers AC 01-01 Scenario 1: with the env
// override unset, BaseURL() resolves to the repointed default (not atcr/personas).
func TestBaseURL_DefaultResolvesToRepointedURL(t *testing.T) {
	t.Setenv(envPersonasURL, "")
	assert.Equal(t, repointedBaseURL, BaseURL())
	assert.NotContains(t, BaseURL(), "atcr/personas", "old registry path must not resolve")
}

// TestBaseURL_DefaultIsHTTPS covers the HTTPS-only requirement: the default base URL
// uses the https scheme (no plaintext fetch of untrusted persona content).
func TestBaseURL_DefaultIsHTTPS(t *testing.T) {
	assert.True(t, strings.HasPrefix(RegistryBaseURL, "https://"),
		"default registry URL must be HTTPS-only, got %q", RegistryBaseURL)
}

// TestBaseURL_EnvOverrideWinsOverRepoint covers AC 01-01 Scenario 2: the
// ATCR_PERSONAS_URL override still takes precedence after the repoint.
func TestBaseURL_EnvOverrideWinsOverRepoint(t *testing.T) {
	t.Setenv(envPersonasURL, "https://example.test/mock-registry")
	assert.Equal(t, "https://example.test/mock-registry", BaseURL())
}

// TestBaseURL_WhitespaceOverrideFallsBack covers AC 01-01 Edge Cases 1 & 2: a
// whitespace-only or empty override trims to empty and falls back to the repointed
// default (existing trim-then-check behavior preserved).
func TestBaseURL_WhitespaceOverrideFallsBack(t *testing.T) {
	for _, v := range []string{"   ", ""} {
		t.Setenv(envPersonasURL, v)
		assert.Equal(t, repointedBaseURL, BaseURL())
	}
}

// TestFetchIndex_ConstructsURLWithoutDoubleSlash covers AC 01-01 Edge Case 3: the
// index URL is base + "/index.json" with no double slash, regardless of the base
// having no trailing slash. A capturing server records the exact requested path.
func TestFetchIndex_ConstructsURLWithoutDoubleSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	// srv.URL has no trailing slash, mirroring the repointed constant's shape.
	_, err := FetchIndex(srv.Client(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "/index.json", gotPath, "no double slash and no trailing-slash ambiguity")
}
