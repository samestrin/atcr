// Package personas implements the `atcr personas` lifecycle: installing,
// listing, searching, removing, testing, and upgrading community-contributed
// reviewer personas fetched from a configurable repository. All HTTP access
// flows through the injectable HTTPClient so the fetch path is exercised in CI
// against httptest.NewServer with zero live network calls.
package personas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
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

// fetchTimeout is the per-request deadline applied inside fetch. It is a
// package-level variable so tests can lower it without affecting callers.
var fetchTimeout = 30 * time.Second

// fetchBodyLimit caps the response body size to guard against DoS via an
// oversized community-repo response. 5 MB is well above any realistic persona
// or index.json size.
const fetchBodyLimit int64 = 5 * 1024 * 1024

// fetch performs a GET against url and returns the body for a 2xx, notFound for
// a 404, or a descriptive error otherwise. The body is always closed.
// A context timeout of fetchTimeout is applied to guard against server hangs.
func fetch(client HTTPClient, url string, notFound error) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	// Read at most fetchBodyLimit+1 bytes; if we get more the response is
	// oversized and we reject it to prevent DoS via a multi-GB community body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > fetchBodyLimit {
		return nil, fmt.Errorf("response body exceeds %d-byte limit", fetchBodyLimit)
	}
	return body, nil
}

// FetchPersonaYAML fetches <baseURL>/<name>.yaml from the community repo.
// The name is validated before any network access so the fetch boundary is
// self-guarding regardless of caller discipline.
func FetchPersonaYAML(client HTTPClient, baseURL, name string) ([]byte, error) {
	if err := validatePersonaName(name); err != nil {
		return nil, fmt.Errorf("invalid persona name: %w", err)
	}
	// PathEscape each path segment so the URL is safe even if future callers
	// bypass validatePersonaName (defense in depth; no-op for valid names).
	segments := strings.Split(name, "/")
	escaped := make([]string, len(segments))
	for i, seg := range segments {
		escaped[i] = url.PathEscape(seg)
	}
	safeName := strings.Join(escaped, "/")
	data, err := fetch(client, strings.TrimRight(baseURL, "/")+"/"+safeName+".yaml", ErrPersonaNotFound)
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
