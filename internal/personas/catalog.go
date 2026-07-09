package personas

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// CatalogBaseURL is OpenRouter's public API base. The model catalog lives at
// <base>/models. Tests point a CatalogClient at an httptest server by setting its
// BaseURL field directly (the same injection seam client.go's fetch path uses),
// so CI exercises the fetch path with zero live network calls; production leaves
// BaseURL empty and FetchModels falls back to this constant.
const CatalogBaseURL = "https://openrouter.ai/api/v1"

// CatalogModel is one entry of OpenRouter's /api/v1/models `data` array, reduced
// to the fields the resolver needs (per documentation/openrouter-catalog-api.md).
// Unknown fields are ignored by the decoder so the full live schema decodes
// without listing every field. ExpirationDate is a pointer so JSON null (not
// deprecated) is distinguishable from a present date string.
type CatalogModel struct {
	ID             string
	CanonicalSlug  string
	Created        int64
	ExpirationDate *string
}

// catalogModelJSON is the wire shape. Created is captured as raw JSON so a
// missing, zero, or non-numeric value degrades to 0 (treated as ineligible for
// newest-selection, AC 03-02 Edge Case 4) instead of failing the whole decode —
// json.RawMessage accepts any JSON token without erroring, and parseCreated does
// the best-effort conversion.
type catalogModelJSON struct {
	ID             string          `json:"id"`
	CanonicalSlug  string          `json:"canonical_slug"`
	Created        json.RawMessage `json:"created"`
	ExpirationDate *string         `json:"expiration_date"`
}

// UnmarshalJSON tolerates a missing/zero/unparseable `created` by leaving Created
// at 0 rather than erroring, so one malformed entry never crashes the catalog scan.
func (m *CatalogModel) UnmarshalJSON(data []byte) error {
	var raw catalogModelJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.ID = raw.ID
	m.CanonicalSlug = raw.CanonicalSlug
	m.ExpirationDate = raw.ExpirationDate
	m.Created = parseCreated(raw.Created)
	return nil
}

// parseCreated best-effort-converts a raw `created` value to a Unix timestamp.
// It accepts a JSON number (int or float) or a numeric JSON string, and returns
// 0 for anything absent, non-numeric, or otherwise unparseable — so a single
// malformed entry is treated as ineligible rather than aborting the catalog parse.
func parseCreated(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		if v, err := n.Int64(); err == nil {
			return v
		}
		if f, err := n.Float64(); err == nil && f > 0 {
			return int64(f)
		}
		return 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			return v
		}
	}
	return 0
}

// catalogResponse is the envelope OpenRouter returns: {"data": [ ...models... ]}.
type catalogResponse struct {
	Data []CatalogModel `json:"data"`
}

// CatalogClient fetches the OpenRouter model catalog. It accepts the same
// injectable HTTPClient seam as the rest of internal/personas, so tests point it
// at an httptest.NewServer-backed client and CI stays zero-live-network.
type CatalogClient struct {
	HTTPClient HTTPClient
	BaseURL    string
}

// ErrCatalogNotFound is returned when the catalog endpoint 404s.
var ErrCatalogNotFound = fmt.Errorf("catalog endpoint not found")

// FetchModels GETs <BaseURL>/models and returns the parsed model list. It reuses
// fetch()'s retry/backoff/timeout/body-size-cap guards unchanged.
func (c *CatalogClient) FetchModels() ([]CatalogModel, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = strings.TrimRight(CatalogBaseURL, "/")
	}
	data, err := fetch(c.HTTPClient, base+"/models", ErrCatalogNotFound)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch model catalog: %w", err)
	}
	var resp catalogResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse model catalog: %w", err)
	}
	return resp.Data, nil
}

// Binding is a persona's resolved-model binding, parsed from the on-disk
// PersonaIndexEntry.Binding string (Story 2). Exactly one strategy applies:
//   - Pin (non-empty): an explicit concrete slug, returned verbatim, never floats.
//   - Family + Channel: a logical family/channel binding resolved via either the
//     alias table (provider-owned -latest) or the created-timestamp vendor scan.
//
// Channel is "@stable" (default) or "@latest"; it is consulted only by the
// created-timestamp strategy — the alias and pin paths ignore it.
type Binding struct {
	Family  string
	Channel string
	Pin     string
}

// aliasTable maps an alias-covered family/tier to its provider-owned ~…-latest
// alias slug (confirmed completion-routable by the Phase 1 spike, AC 01-01). The
// provider resolves these server-side, so the resolver passes them through
// unchanged with no catalog scan. Keyed by family/tier (not merely vendor) so two
// tiers of the same vendor (opus vs sonnet) get distinct slugs (AC 03-01 EC1).
var aliasTable = map[string]string{
	"anthropic/claude-opus":   "~anthropic/claude-opus-latest",
	"anthropic/claude-sonnet": "~anthropic/claude-sonnet-latest",
	"openai/gpt":              "~openai/gpt-latest",
	"openai/gpt-mini":         "~openai/gpt-mini-latest",
	"google/gemini-pro":       "~google/gemini-pro-latest",
	"google/gemini-flash":     "~google/gemini-flash-latest",
	"moonshotai/kimi":         "~moonshotai/kimi-latest",
}

// ResolveModel turns a persona's binding into exactly one concrete model slug,
// selecting the strategy deterministically: explicit pin first, then the alias
// table, then the created-timestamp vendor-prefix scan. It never falls back to a
// zero-value slug — an unresolvable binding returns a descriptive error.
//
// models is the already-parsed catalog (the caller fetches it once per explicit
// upgrade). The alias and pin paths ignore models entirely; only the
// created-timestamp scan reads it.
func ResolveModel(b Binding, models []CatalogModel) (string, error) {
	// Strategy 1 — explicit pin short-circuit (added in Element 3).

	// Strategy 2 — alias passthrough: a static map lookup, exact-match, no scan.
	if slug, ok := aliasTable[b.Family]; ok {
		return slug, nil
	}

	// Strategy 3 — created-timestamp vendor-prefix scan (added in Element 2).

	return "", fmt.Errorf("no alias, pin, or vendor-prefix strategy found for persona family %q", b.Family)
}
