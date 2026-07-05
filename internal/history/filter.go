package history

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSince parses a --since window into a positive duration. It extends Go's
// time.ParseDuration (which rejects day/week units) with a leading-number `d`
// (days) and `w` (weeks) form: "30d", "2w". Native composite durations such as
// "1h30m" still parse. The result must be strictly positive — a zero or negative
// window is rejected as a usage error.
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	var d time.Duration
	switch unit := s[len(s)-1]; unit {
	case 'd', 'w':
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: want a number before %q (e.g. 30%c)", s, string(unit), unit)
		}
		per := 24 * time.Hour
		if unit == 'w' {
			per = 7 * 24 * time.Hour
		}
		d = time.Duration(n * float64(per))
	default:
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: use h/m/s or d/w (e.g. 30d, 2w, 48h)", s)
		}
		d = parsed
	}

	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
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
