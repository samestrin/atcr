package debt

import (
	"sort"
	"strings"
	"time"
)

// Component derives a stable, coarse component label from an item's File value:
// the first two path segments (path_depth=2, matching the TD grouping
// convention). A line suffix is stripped first. A value with no path separator
// (free-text File) is bucketed under a single "(unscoped)" sentinel so
// aggregation never explodes into one-off buckets.
func Component(file string) string {
	p := filePath(file)
	if !strings.Contains(p, "/") {
		if p == "" {
			return "(unscoped)"
		}
		// A bare filename (e.g. main.go:3 -> main.go) is a legitimate one-segment
		// component; genuinely path-less prose is not. Distinguish the two by
		// looking for a file extension or a single free-form phrase.
		if strings.Contains(p, " ") && !hasExtension(p) && wordCount(p) > 1 {
			return "(unscoped)"
		}
		return p
	}
	segs := strings.SplitN(p, "/", 3)
	return segs[0] + "/" + segs[1]
}

// hasExtension reports whether s ends with a dot-prefixed extension made of
// letters or digits. It is a coarse signal for "this looks like a filename".
func hasExtension(s string) bool {
	i := strings.LastIndex(s, ".")
	if i <= 0 || i == len(s)-1 {
		return false
	}
	for _, r := range s[i+1:] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// wordCount returns the number of whitespace-delimited words in s.
func wordCount(s string) int {
	return len(strings.Fields(s))
}

// SeverityCount is the open/deferred/resolved breakdown for one severity.
type SeverityCount struct {
	Severity                 string
	Open, Deferred, Resolved int
	Total                    int
}

// ComponentCount is the item total for one component.
type ComponentCount struct {
	Component string
	Total     int
}

// AgeBucket is the unresolved-item count for one age band.
type AgeBucket struct {
	Label string
	Count int
}

// Summary is the aggregated view a dashboard renders: totals, per-severity and
// per-component breakdowns, an unresolved-item age profile, and the top-priority
// unresolved items.
type Summary struct {
	Total                    int
	Open, Deferred, Resolved int
	BySeverity               []SeverityCount
	ByComponent              []ComponentCount
	ByAge                    []AgeBucket
	Top                      []Record
}

// severityOrder is the canonical most-severe-first ordering for presentation.
var severityOrder = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// ageBands defines the unresolved-item age profile, evaluated in order; the
// first band whose max (inclusive, in days) is >= the item's age wins. The final
// catch-all band uses a max of -1 to mean "no upper bound".
var ageBands = []struct {
	label string
	max   int
}{
	{"0-7d", 7},
	{"8-30d", 30},
	{"31-90d", 90},
	{">90d", -1},
}

// unresolved reports whether an item is still live debt (open or deferred).
func unresolved(r Record) bool {
	return !strings.EqualFold(r.Status, "resolved")
}

// ageDays returns the item's age in whole days relative to now, and whether the
// shard date parsed. An unparseable date yields ok=false so callers can route it
// to an "unknown" bucket instead of silently treating it as age 0.
func ageDays(r Record, now time.Time) (int, bool) {
	d, err := time.Parse("2006-01-02", strings.TrimSpace(r.Date))
	if err != nil {
		return 0, false
	}
	days := int(now.Sub(d).Hours() / 24)
	if days < 0 {
		days = 0 // a future-dated shard is "new", not negative age
	}
	return days, true
}

func bandLabel(days int) string {
	for _, b := range ageBands {
		if b.max < 0 || days <= b.max {
			return b.label
		}
	}
	return ageBands[len(ageBands)-1].label
}

// Summarize aggregates recs into a Summary. Age buckets and Top cover only
// unresolved (open+deferred) items — resolved debt is not part of the live
// backlog. Top is ordered most-severe first, then oldest first, capped at topN.
func Summarize(recs []Record, now time.Time, topN int) Summary {
	var s Summary
	s.Total = len(recs)

	sevIdx := map[string]*SeverityCount{}
	sevCounts := make([]SeverityCount, len(severityOrder))
	for i, name := range severityOrder {
		sevCounts[i] = SeverityCount{Severity: name}
		sevIdx[name] = &sevCounts[i]
	}

	compCount := map[string]int{}
	ageCount := map[string]int{}
	var top []Record

	for _, r := range recs {
		switch {
		case strings.EqualFold(r.Status, "resolved"):
			s.Resolved++
		case strings.EqualFold(r.Status, "deferred"):
			s.Deferred++
		default: // treat any non-resolved/deferred status as open
			s.Open++
		}

		if sc, ok := sevIdx[strings.ToUpper(r.Severity)]; ok {
			sc.Total++
			switch {
			case strings.EqualFold(r.Status, "resolved"):
				sc.Resolved++
			case strings.EqualFold(r.Status, "deferred"):
				sc.Deferred++
			default:
				sc.Open++
			}
		}

		compCount[Component(r.File)]++

		if unresolved(r) {
			if days, ok := ageDays(r, now); ok {
				ageCount[bandLabel(days)]++
			} else {
				ageCount["unknown"]++
			}
			top = append(top, r)
		}
	}

	s.BySeverity = sevCounts

	// Components: Total desc, then name asc for a stable presentation.
	for name, n := range compCount {
		s.ByComponent = append(s.ByComponent, ComponentCount{Component: name, Total: n})
	}
	sort.Slice(s.ByComponent, func(i, j int) bool {
		if s.ByComponent[i].Total != s.ByComponent[j].Total {
			return s.ByComponent[i].Total > s.ByComponent[j].Total
		}
		return s.ByComponent[i].Component < s.ByComponent[j].Component
	})

	// Age: emit the fixed bands in order (only those with a count), then unknown.
	for _, b := range ageBands {
		if n := ageCount[b.label]; n > 0 {
			s.ByAge = append(s.ByAge, AgeBucket{Label: b.label, Count: n})
		}
	}
	if n := ageCount["unknown"]; n > 0 {
		s.ByAge = append(s.ByAge, AgeBucket{Label: "unknown", Count: n})
	}

	// Top priority: severity then age (oldest first), deterministic on ties.
	_ = Sort(top, SortSeverity) // severity rank, then Date asc, then File
	if topN >= 0 && len(top) > topN {
		top = top[:topN]
	}
	s.Top = top

	return s
}
