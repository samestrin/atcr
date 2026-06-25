package personas

import "strings"

// PersonaIndexEntry is one entry in the community repo index.json. Unknown JSON
// fields are ignored by the decoder.
type PersonaIndexEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Path        string `json:"path"`
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
