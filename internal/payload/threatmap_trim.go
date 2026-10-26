package payload

import "sort"

// TrimToPaths narrows entries to the paths in keep, the set a threat-model
// pass scoped the review to (Epic 37.0). It is pure: entries is not modified
// and no filesystem is touched.
//
// Shed-exempt synthetic sections — the claim ledger and the Context
// Definitions block — always stay, whatever keep says. Their value depends on
// reaching every reviewer identically, and the exemption keys on the
// unexported shedExempt sentinel, never on Path, so a repository file named
// "<claims>" is trimmed like any other file.
//
// An empty (or nil) keep means "no scope": every entry comes back unchanged
// and dropped is nil, so a threat map that names nothing never empties the
// payload. Otherwise kept preserves input order and dropped lists the paths
// that were removed, sorted for stable output.
func TrimToPaths(entries []FileEntry, keep map[string]bool) (kept []FileEntry, dropped []string) {
	if len(keep) == 0 {
		return entries, nil
	}
	kept = make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		if e.shedExempt || keep[e.Path] {
			kept = append(kept, e)
			continue
		}
		dropped = append(dropped, e.Path)
	}
	sort.Strings(dropped)
	return kept, dropped
}

// ReviewablePaths returns the path of every real file in entries, in order,
// skipping the shed-exempt synthetic sections: the path set a threat-model
// pass is shown (Epic 37.0). The synthetic sections are not files and always
// stay (TrimToPaths), so offering them to the mapper would only invite it to
// name a "<claims>" path that keeps nothing.
func ReviewablePaths(entries []FileEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.shedExempt {
			paths = append(paths, e.Path)
		}
	}
	return paths
}
