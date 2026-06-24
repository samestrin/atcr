// Package personas implements the `atcr personas` lifecycle: installing,
// listing, searching, removing, testing, and upgrading community-contributed
// reviewer personas fetched from a configurable repository. All HTTP access
// flows through the injectable HTTPClient so the fetch path is exercised in CI
// against httptest.NewServer with zero live network calls.
package personas

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// RegistryBaseURL is the default community persona repository, raw content root.
// It is overridable per-invocation via the ATCR_PERSONAS_URL environment
// variable (see BaseURL) so no published source is hardcoded unconditionally.
const RegistryBaseURL = "https://raw.githubusercontent.com/atcr/personas/main"

// envPersonasURL overrides RegistryBaseURL when set (e.g. an httptest server).
const envPersonasURL = "ATCR_PERSONAS_URL"

// HTTPClient is the minimal HTTP surface the fetch path depends on, so tests
// inject an httptest.NewServer client. *http.Client satisfies it.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

var (
	// ErrPersonaNotFound is returned when the community repo has no persona at
	// the requested name (HTTP 404).
	ErrPersonaNotFound = errors.New("not found in community repo")
	// ErrIndexNotFound is returned when the community repo index.json is absent.
	ErrIndexNotFound = errors.New("community repo index not found")
)

// BaseURL returns the effective community-repo base URL: the ATCR_PERSONAS_URL
// override when set (non-empty after trimming), else the hardcoded default.
func BaseURL() string {
	if v := strings.TrimSpace(os.Getenv(envPersonasURL)); v != "" {
		return v
	}
	return RegistryBaseURL
}

// fetch performs a GET against url and returns the body for a 2xx, notFound for
// a 404, or a descriptive error otherwise. The body is always closed.
func fetch(client HTTPClient, url string, notFound error) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, notFound
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// FetchPersonaYAML fetches <baseURL>/<name>.yaml from the community repo.
func FetchPersonaYAML(client HTTPClient, baseURL, name string) ([]byte, error) {
	data, err := fetch(client, strings.TrimRight(baseURL, "/")+"/"+name+".yaml", ErrPersonaNotFound)
	if err != nil {
		if errors.Is(err, ErrPersonaNotFound) {
			return nil, fmt.Errorf("persona %q %w", name, ErrPersonaNotFound)
		}
		return nil, fmt.Errorf("failed to fetch persona %q: %w", name, err)
	}
	return data, nil
}

// FetchIndex fetches and parses <baseURL>/index.json into index entries.
func FetchIndex(client HTTPClient, baseURL string) ([]PersonaIndexEntry, error) {
	data, err := fetch(client, strings.TrimRight(baseURL, "/")+"/index.json", ErrIndexNotFound)
	if err != nil {
		if errors.Is(err, ErrIndexNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to fetch community repo index: %w", err)
	}
	var entries []PersonaIndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse community repo index: %w", err)
	}
	return entries, nil
}
