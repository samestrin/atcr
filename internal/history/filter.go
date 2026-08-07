package history

import (
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/timewindow"
)

// ParseSince parses a --since window into a positive duration. It is a thin
// alias for timewindow.Parse, which owns the one grammar every window flag in
// atcr shares — Nd/Nw/Nm plus "all". The alias is kept so this package's callers
// read in its own vocabulary; it must never grow a grammar of its own again.
func ParseSince(s string) (time.Duration, error) {
	return timewindow.Parse(s)
}

// Filter returns the records that fall within the `since` window (Timestamp not
// older than now-since) and, when pkg is non-empty, whose package matches pkg by
// a separator-aware path prefix. The check is `rec.Package == pkg ||
// HasPrefix(rec.Package, pkg+"/")`, so "internal/registry" matches
// "internal/registry" and "internal/registry/sub" but never the sibling
// "internal/registry2". Stored packages are slash-normalized (see PackageOf), so
// the query is normalized to slashes and a trailing slash is trimmed.
func Filter(recs []Record, since time.Duration, pkg string, now time.Time) []Record {
	cutoff := now.Add(-since)
	pkg = strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(pkg), "\\", "/"), "/")

	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		if r.Timestamp.Before(cutoff) {
			continue
		}
		if pkg != "" && !packageMatch(r.Package, pkg) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// packageMatch reports whether recPkg is query or a path nested under it. The
// trailing separator is load-bearing: it stops a sibling directory sharing a
// name prefix (registry vs registry2) from matching as nested — mirroring the
// containment check in internal/reconcile/discover.go.
func packageMatch(recPkg, query string) bool {
	return recPkg == query || strings.HasPrefix(recPkg, query+"/")
}
