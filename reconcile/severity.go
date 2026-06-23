package reconcile

import "strings"

// SeverityRank is the canonical severity ordinal rubric. Higher rank is more
// severe: it wins a merge, clears a higher min_severity floor, and sorts earlier
// in the reconcile radar and the report view. An unrecognized or empty token is
// absent from the map (rank 0) and therefore always ranks/sorts below every real
// level.
//
// This is the single source of truth (extracted from internal/stream in Epic
// 8.0). ATCR's stream, fanout, verify, and report packages consume it through a
// re-export so a severity rename or a non-canonical casing can never desync
// fan-out truncation from reconcile merging.
//
// Read-only after init: the map is written once at package load and only read
// thereafter. Concurrent consumers share this map, so a write would race — if
// mutation is ever needed, copy it locally first.
var SeverityRank = map[string]int{
	"CRITICAL": 4,
	"HIGH":     3,
	"MEDIUM":   2,
	"LOW":      1,
}

// NormalizeSeverity upper-cases and trims a severity token to its canonical form
// so a SeverityRank lookup is case- and whitespace-insensitive. Every consumer
// normalizes through this single helper so their lookups stay identical.
func NormalizeSeverity(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
