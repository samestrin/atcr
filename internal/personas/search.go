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
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	Provider    string   `json:"provider,omitempty"`
	Model       string   `json:"model,omitempty"`
	Tasks       []string `json:"tasks,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// Search fetches the community index and returns entries whose name or
// description contains keyword (case-insensitive). An empty result is not an
// error — the caller reports "no personas found".
func Search(client HTTPClient, baseURL, keyword string) ([]PersonaIndexEntry, error) {
	entries, err := FetchIndex(client, baseURL)
	if err != nil {
		return nil, err
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	var out []PersonaIndexEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), kw) ||
			strings.Contains(strings.ToLower(e.Description), kw) {
			out = append(out, e)
		}
	}
	return out, nil
}
