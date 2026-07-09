package personas

import "strings"

// PersonaIndexEntry is one entry in the community repo index.json. Unknown JSON
// fields are ignored by the decoder (the index decode path stays permissive so
// old-shape payloads remain forward-compatible).
//
// Provider names a routing-endpoint key present in the registry Providers map
// (e.g. "openrouter", "synthetic"), NOT the vendor/brand identity — vendor
// identity lives in Model (e.g. "deepseek", "claude-sonnet-4-6"). Discovery and
// grouping are by Model. Tasks and Tags are forward-looking schema only: decoded
// and stored additively but not yet consumed by any search filter.
type PersonaIndexEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Provider    string `json:"provider,omitempty"`
	Model       string `json:"model,omitempty"`
	// Binding is the logical family/channel target (e.g. "anthropic/claude-opus@stable")
	// a future resolver (Epic 19.7 Theme 3) reads at `atcr personas upgrade` time to
	// recompute Model (the resolved lock). Additive and inert in Phase 2: decoded
	// permissively like Provider/Model, never read on any path, so an absent key
	// decodes as "" with no error and existing indexes stay backward-compatible.
	Binding string   `json:"binding,omitempty"`
	Tasks   []string `json:"tasks,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// SearchOptions carries the filters for a community-index search. Every supplied
// (non-empty, post-trim) field is an independent AND condition; an empty field is
// skipped. All matching is case-insensitive substring matching.
//
//   - Keyword: the bare positional `search <keyword>` term. It matches Name OR
//     Description OR Provider OR Model — i.e. it also reaches the STRUCTURED
//     provider/model fields so `search deepseek` finds a persona bound to that
//     vendor even when the token appears only in structured data (AC 03-02). This
//     is structured matching, distinct from the free-text leak AC 03-01 forbids.
//   - Model: matches the structured Model field ONLY — never free text. A model
//     string that appears only in a Description does not satisfy Model (AC 03-01).
//   - Provider: matches the structured Provider field ONLY. Provider is the
//     routing-endpoint key (e.g. "openrouter"), NOT the vendor/brand — vendor
//     discovery lives in Model, so use Model, not Provider, to find "deepseek".
type SearchOptions struct {
	Keyword  string
	Model    string
	Provider string
}

// Search fetches the community index and returns entries whose Name, Description,
// Provider, or Model contains keyword (case-insensitive substring — see
// matchesKeyword). Preserved as a thin back-compat wrapper over SearchWithOptions
// for existing keyword-only callers. An empty result is not an error — the caller
// reports "no personas found".
func Search(client HTTPClient, baseURL, keyword string) ([]PersonaIndexEntry, error) {
	return SearchWithOptions(client, baseURL, SearchOptions{Keyword: keyword})
}

// SearchWithOptions fetches the community index and returns entries satisfying
// every non-empty filter in opts (AND). Matching is case-insensitive and
// substring-tolerant — `--model gpt-4` deliberately matches `gpt-4o` (near-miss
// substring, not exact match; see AC 03-01 Edge Case 1). An empty result is not
// an error. When all filters are empty every entry is returned; the CLI guards
// against an all-empty invocation before calling here (AC 03-03).
func SearchWithOptions(client HTTPClient, baseURL string, opts SearchOptions) ([]PersonaIndexEntry, error) {
	entries, err := FetchIndex(client, baseURL)
	if err != nil {
		return nil, err
	}
	kw := strings.ToLower(strings.TrimSpace(opts.Keyword))
	model := strings.ToLower(strings.TrimSpace(opts.Model))
	provider := strings.ToLower(strings.TrimSpace(opts.Provider))

	var out []PersonaIndexEntry
	for _, e := range entries {
		if kw != "" && !matchesKeyword(e, kw) {
			continue
		}
		// --model matches the structured Model field only (never Name/Description).
		if model != "" && !strings.Contains(strings.ToLower(e.Model), model) {
			continue
		}
		// --provider matches the structured Provider (routing-endpoint) field only.
		if provider != "" && !strings.Contains(strings.ToLower(e.Provider), provider) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// matchesKeyword reports whether the lowercased keyword kw is a substring of any
// of the entry's Name, Description, Provider, or Model fields. Including the two
// structured fields (Provider/Model) is what lets a bare `search deepseek` find a
// deepseek-bound persona from structured data (AC 03-02), without stuffing the
// vendor token into free text.
func matchesKeyword(e PersonaIndexEntry, kw string) bool {
	return strings.Contains(strings.ToLower(e.Name), kw) ||
		strings.Contains(strings.ToLower(e.Description), kw) ||
		strings.Contains(strings.ToLower(e.Provider), kw) ||
		strings.Contains(strings.ToLower(e.Model), kw)
}
