package personas

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
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

// vendorPrefixTable maps an alias-less family to its catalog vendor prefix for
// the created-timestamp newest-in-vendor-prefix scan. Critically, family "glm"
// keys the "z-ai/" namespace — there is NO "glm/" namespace in the catalog (a
// regression caught during /refine-epic; encoded as a test, never a bare comment).
var vendorPrefixTable = map[string]string{
	"deepseek": "deepseek/",
	"qwen":     "qwen/",
	"glm":      "z-ai/",
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
	// Strategy 1 — explicit pin short-circuit. A non-empty pin is the always-
	// available escape hatch: it is returned verbatim and NEVER floats, regardless
	// of family, channel, or catalog contents. An empty/whitespace pin is treated
	// as "no pin" and falls through to the alias/created-timestamp strategies.
	if pin := strings.TrimSpace(b.Pin); pin != "" {
		if err := validateResolvedSlug(pin); err != nil {
			return "", fmt.Errorf("invalid pin %q for family %q: %w", pin, b.Family, err)
		}
		return pin, nil
	}

	// Strategy 2 — alias passthrough: a static map lookup, exact-match, no scan.
	if slug, ok := aliasTable[b.Family]; ok {
		return slug, nil
	}

	// Strategy 3 — created-timestamp newest-in-vendor-prefix scan.
	if prefix, ok := vendorPrefixTable[b.Family]; ok {
		return resolveNewestInPrefix(prefix, b, models)
	}

	return "", fmt.Errorf("no alias, pin, or vendor-prefix strategy found for persona family %q", b.Family)
}

// resolveNewestInPrefix returns the slug of the newest-by-`created` catalog entry
// whose ID carries the exact vendor prefix. Entries with an ineligible `created`
// (absent/zero) are skipped. Ties on `created` break to the lexicographically
// greater slug, deterministically and independent of catalog array order. It
// fails closed with a descriptive error when no eligible entry exists — never a
// stale or zero-value slug. The resolved slug is validated as a plain printable
// identifier before return (mirrors 19.6 TD-008 control-char sanitization).
func resolveNewestInPrefix(prefix string, b Binding, models []CatalogModel) (string, error) {
	channel, ok := normalizeChannel(b.Channel)
	if !ok {
		return "", fmt.Errorf("unrecognized channel %q for family %q: expected \"@stable\" or \"@latest\"", b.Channel, b.Family)
	}
	var best *CatalogModel
	for i := range models {
		m := &models[i]
		if !strings.HasPrefix(m.ID, prefix) { // exact-prefix: "z-ai-evil/" ≠ "z-ai/"
			continue
		}
		if m.Created <= 0 { // absent/zero/unparseable created → ineligible
			continue
		}
		if !channelEligible(*m, channel) { // channel-conditional preview/deprecation exclusion
			continue
		}
		if best == nil || newerCandidate(*m, *best) {
			best = m
		}
	}
	if best == nil {
		return "", fmt.Errorf("no eligible %s-prefixed model found in catalog for family %q", prefix, b.Family)
	}
	if err := validateResolvedSlug(best.ID); err != nil {
		return "", fmt.Errorf("resolved model for family %q: %w", b.Family, err)
	}
	return best.ID, nil
}

// previewTokenSet is the set of hyphen-delimited slug segments that mark a model
// as preview/pre-release, excluded under @stable. Derived from the Phase 1 spike
// (`-preview`, `-exp` observed live; the rest retained as forward-looking watch
// tokens). Matched as whole segments, never bare substrings, so stable models are
// not over-excluded on collisions (e.g. "latest" contains "test", "search"
// contains "rc").
var previewTokenSet = map[string]bool{
	"preview": true, "beta": true, "exp": true, "alpha": true,
	"rc": true, "experimental": true, "nightly": true, "snapshot": true,
}

// normalizeChannel resolves a binding's channel to one of the two known literals,
// defaulting an empty/whitespace channel to "@stable" (the documented default).
// It returns ok=false for any other value so the resolver fails closed on an
// unrecognized channel rather than silently defaulting (AC 03-05 Error Scenario 1).
func normalizeChannel(channel string) (string, bool) {
	switch strings.TrimSpace(channel) {
	case "":
		return "@stable", true
	case "@stable":
		return "@stable", true
	case "@latest":
		return "@latest", true
	default:
		return "", false
	}
}

// channelEligible reports whether a model qualifies for the given (normalized)
// channel. Deprecation (non-null expiration_date) is ALWAYS excluded — a
// sunsetting model would 404 at review time regardless of preview status. Beyond
// that, @stable additionally excludes preview/beta/exp-tagged models, while
// @latest includes them. So @latest bypasses ONLY the preview-token exclusion.
func channelEligible(m CatalogModel, channel string) bool {
	if isDeprecated(m) {
		return false // deprecation excludes under both channels (fails closed)
	}
	if channel == "@latest" {
		return true // preview-tagged members are eligible under @latest
	}
	return !hasPreviewToken(m) // @stable excludes preview-tagged members
}

// isDeprecated reports whether a model carries a deprecation signal: a non-null,
// non-empty expiration_date. Per TD-002 this is an any-non-null rule (fails
// closed, no horizon window) — a far-future sentinel date is still treated as
// deprecated. An empty/whitespace string is treated as equivalent to JSON null
// (not deprecated), since the schema is `string | null` (AC 03-04 Edge Case 3).
func isDeprecated(m CatalogModel) bool {
	return m.ExpirationDate != nil && strings.TrimSpace(*m.ExpirationDate) != ""
}

// hasPreviewToken reports whether a model's id or canonical_slug contains a
// preview segment token. The slug is normalized before tokenizing (TD-001): the
// `:variant` suffix (e.g. `:free`, `:thinking`) and the vendor prefix are stripped
// first, then the remainder is split on "-" and each segment checked against
// previewTokenSet — so `deepseek/deepseek-v5-preview:free` is correctly excluded
// while `~anthropic/claude-opus-latest` (segment "latest") is not.
func hasPreviewToken(m CatalogModel) bool {
	return slugHasPreviewSegment(m.ID) || slugHasPreviewSegment(m.CanonicalSlug)
}

func slugHasPreviewSegment(slug string) bool {
	if i := strings.IndexByte(slug, ':'); i >= 0 {
		slug = slug[:i] // strip :variant suffix
	}
	if i := strings.LastIndexByte(slug, '/'); i >= 0 {
		slug = slug[i+1:] // strip vendor prefix
	}
	for _, seg := range strings.Split(slug, "-") {
		if previewTokenSet[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// newerCandidate reports whether cand should replace cur as the newest: a larger
// `created` wins; on a tie the lexicographically greater ID wins. This total
// order makes selection independent of the catalog's array order.
func newerCandidate(cand, cur CatalogModel) bool {
	if cand.Created != cur.Created {
		return cand.Created > cur.Created
	}
	return cand.ID > cur.ID
}

// validateResolvedSlug rejects a resolved/pinned slug that is empty, carries
// control characters (mirrors 19.6 TD-008: unicode.IsControl plus the U+2028/2029
// line/paragraph separators), or lacks a "/" (not a vendor/model slug) — before
// it is ever returned to be written into a lock or an outbound request.
func validateResolvedSlug(slug string) error {
	if strings.TrimSpace(slug) == "" {
		return fmt.Errorf("resolved slug is empty")
	}
	if strings.IndexFunc(slug, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	}) >= 0 {
		return fmt.Errorf("resolved slug %q contains control characters", slug)
	}
	// Require a non-empty segment on BOTH sides of the first "/", so a malformed
	// vendor-only ("z-ai/") or model-only ("/glm-5.2") slug is rejected rather
	// than written into a lock.
	if i := strings.IndexByte(slug, '/'); i <= 0 || i == len(slug)-1 {
		return fmt.Errorf("resolved slug %q is not a vendor/model slug", slug)
	}
	return nil
}
