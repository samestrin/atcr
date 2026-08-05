package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/log"
)

// This file is the aggregation and dashboard-rendering half of `atcr debt`,
// ported from the deleted internal/debt package onto localdebt.Record when
// .atcr/debt/ became the single system of record (Plan 35.13). It lives in
// package cli rather than a new internal package because it has exactly one
// consumer — the debt subcommands — and the cli package already hosts
// selectOpenDebt/severityRank/renderResolveList as directly unit-tested
// package-level functions.
//
// Two things changed in the retyping, both forced by the new record shape:
//
//   - Component bucketing no longer strips a ":line" suffix. localdebt.Record
//     carries Line as a separate int, so File never has one. The prose heuristic
//     (hasExtension/wordCount) is carried over verbatim: File is still a
//     free-text string on the wire, and a hand-edited or third-party record can
//     put prose there — the one-off-bucket explosion the sentinel prevents.
//   - Age comes from the record's RFC3339 Timestamp rather than a shard date.
//
// One thing was added: wontfix. It has no counterpart in the .planning/-scoped
// store this was ported from, but it is a first-class terminal status here
// (Epic 24.0). Folding it into Open or Resolved would misreport the backlog, so
// it gets its own counter and column and is excluded from the live backlog.

// debtLocation renders a record's position as the "file:line" a human reads and
// copies. Line 0 means "no line recorded" (a free-text or file-scoped finding),
// so the bare File is rendered rather than a misleading ":0".
func debtLocation(r localdebt.Record) string {
	if r.Line > 0 {
		return r.File + ":" + strconv.Itoa(r.Line)
	}
	return r.File
}

// debtRecordDate returns the record's YYYY-MM-DD date, taken from the RFC3339
// Timestamp prefix. An empty string means the timestamp was too short to carry
// one, which callers route to an "unknown" bucket rather than treating as epoch.
func debtRecordDate(r localdebt.Record) string {
	if len(r.Timestamp) < 10 {
		return ""
	}
	return r.Timestamp[:10]
}

// debtComponent derives a stable, coarse component label from a record's File
// value: the first two path segments (path_depth=2, matching the TD grouping
// convention). A value with no path separator (free-text File) is bucketed under
// a single "(unscoped)" sentinel so aggregation never explodes into one-off
// buckets.
//
// The value is normalized (Clean + slash) before the separator test and split,
// because File is a free-text wire string: "./internal/foo.go" must land in the
// same bucket as "internal/foo.go", and redundant separators must collapse. An
// absolute path loses its leading separator so the two-segment rule applies to
// the real path ("/abs/x/y.go" buckets as "abs/x") rather than every absolute
// path collapsing under "/abs". The empty check runs first because
// filepath.Clean("") is ".", which would otherwise bucket as a bogus component.
func debtComponent(file string) string {
	p := strings.TrimSpace(file)
	if p == "" {
		return "(unscoped)"
	}
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.TrimPrefix(p, "/")
	if !strings.Contains(p, "/") {
		// A bare filename (e.g. main.go) is a legitimate one-segment component;
		// genuinely path-less prose is not. Distinguish the two by looking for a
		// file extension or a single free-form phrase.
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

// debtSeverityCount is the status breakdown for one severity.
type debtSeverityCount struct {
	Severity                          string
	Open, Deferred, Resolved, Wontfix int
	Total                             int
}

// debtComponentCount is the live (open+deferred) item count for one component.
// Settled items are excluded: By Component is the dashboard's prioritization
// rollup, so it shares the live-backlog scope of By Age and Top Priority.
type debtComponentCount struct {
	Component string
	Total     int
}

// debtAgeBucket is the live-backlog item count for one age band.
type debtAgeBucket struct {
	Label string
	Count int
}

// debtSummary is the aggregated view a dashboard renders: totals, per-severity
// and per-component breakdowns, a live-backlog age profile, and the
// top-priority live items.
type debtSummary struct {
	Total                             int
	Open, Deferred, Resolved, Wontfix int
	BySeverity                        []debtSeverityCount
	ByComponent                       []debtComponentCount
	ByAge                             []debtAgeBucket
	Top                               []localdebt.Record
}

// debtSeverityOrder is the canonical most-severe-first ordering for presentation.
var debtSeverityOrder = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// unknownSeverityLabel names the catch-all severity row. It is parenthesized so
// it cannot collide with a real severity value read out of the store, and it
// sorts last by construction (it is appended, not sorted into place).
const unknownSeverityLabel = "(unknown)"

// debtAgeBands defines the live-backlog age profile, evaluated in order; the
// first band whose max (inclusive, in days) is >= the item's age wins. The final
// catch-all band uses a max of -1 to mean "no upper bound".
//
// Not currently rendered. debtSummary.ByAge has no production consumer: the only
// caller of summarizeDebt is renderDebtDashboard, which passes a zero `now` and
// renders the time-invariant debtMonthHistogram instead, because a
// clock-dependent age band would make `dashboard --check` fail on the passage of
// time rather than on content drift. This was equally true of the code it was
// ported from, and is retained (with its ported tests) so a caller that supplies
// a real `now` — an `--as-of` flag, or a report that is not drift-checked — gets
// the bands rather than having to re-derive them.
//
// Retained, but no longer computed for nobody: summarizeDebt skips the banding
// entirely when `now` is zero, so the dashboard's per-record date parse is gone
// and a future caller cannot read a profile that was never measured against a
// real clock.
var debtAgeBands = []struct {
	label string
	max   int
}{
	{"0-7d", 7},
	{"8-30d", 30},
	{"31-90d", 90},
	{">90d", -1},
}

// debtStatusBucket classifies a record into one of the four presentation
// buckets. The store's canonical open record carries an empty status, so an
// unrecognized or absent status is open — the same "treat anything else as open"
// rule the ported code used, extended to cover the empty spelling.
func debtStatusBucket(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "resolved":
		return "resolved"
	case "deferred":
		return "deferred"
	case "wontfix":
		return "wontfix"
	default:
		return "open"
	}
}

// debtIsLive reports whether an item is still work to do. It delegates to
// localdebt.IsSettledStatus so the aggregation, the rendered views, and
// markDebtResolved's closability guard all answer this question the same way — a
// disagreement here is what would let the dashboard rank an item as top-priority
// work that `debt resolve` refuses to touch.
func debtIsLive(r localdebt.Record) bool {
	return !localdebt.IsSettledStatus(r.Status)
}

// debtAgeDays returns the item's age in whole days relative to now, and whether
// its date parsed. An unparseable timestamp yields ok=false so callers can route
// it to an "unknown" bucket instead of silently treating it as age 0.
func debtAgeDays(r localdebt.Record, now time.Time) (int, bool) {
	d, err := time.Parse("2006-01-02", debtRecordDate(r))
	if err != nil {
		return 0, false
	}
	days := int(now.Sub(d).Hours() / 24)
	if days < 0 {
		days = 0 // a future-dated record is "new", not negative age
	}
	return days, true
}

func debtBandLabel(days int) string {
	for _, b := range debtAgeBands {
		if b.max < 0 || days <= b.max {
			return b.label
		}
	}
	return debtAgeBands[len(debtAgeBands)-1].label
}

// summarizeDebt aggregates recs into a debtSummary. ByComponent, age buckets,
// and Top cover only live (open+deferred) items — resolved and dismissed debt
// is not part of the backlog. Top is ordered most-severe first, then oldest
// first, capped at topN.
//
// A ZERO `now` means "no clock supplied" and yields a nil ByAge rather than a
// profile measured against year zero. Every other field is clock-independent, so
// the dashboard — which passes a zero time deliberately, to keep `--check` from
// failing on the passage of time — is unaffected.
func summarizeDebt(recs []localdebt.Record, now time.Time, topN int) debtSummary {
	var s debtSummary
	s.Total = len(recs)

	sevIdx := map[string]*debtSeverityCount{}
	sevCounts := make([]debtSeverityCount, len(debtSeverityOrder), len(debtSeverityOrder)+1)
	for i, name := range debtSeverityOrder {
		sevCounts[i] = debtSeverityCount{Severity: name}
		sevIdx[name] = &sevCounts[i]
	}
	// Appended last, and only if something off-enum actually shows up.
	var unknownSev *debtSeverityCount

	compCount := map[string]int{}
	ageCount := map[string]int{}
	var top []localdebt.Record

	for _, r := range recs {
		bucket := debtStatusBucket(r.Status)
		switch bucket {
		case "resolved":
			s.Resolved++
		case "deferred":
			s.Deferred++
		case "wontfix":
			s.Wontfix++
		default:
			s.Open++
		}

		// An off-enum severity (a hand-edited record, a schema skew, anything that
		// writes to the store without `debt add`'s validation) must not fall out of
		// the breakdown while still counting in Total: that printed a By Severity
		// table that did not sum to the header, with nothing on screen to explain
		// the gap. It lands in the (unknown) row instead, which is created lazily so
		// a clean store never grows an empty one.
		sc, ok := sevIdx[strings.ToUpper(strings.TrimSpace(r.Severity))]
		if !ok {
			if unknownSev == nil {
				sevCounts = append(sevCounts, debtSeverityCount{Severity: unknownSeverityLabel})
				// sevCounts may have reallocated; re-point every index at the
				// current backing array before taking another pointer into it.
				for i := range sevCounts {
					sevIdx[sevCounts[i].Severity] = &sevCounts[i]
				}
				unknownSev = sevIdx[unknownSeverityLabel]
			}
			sc = unknownSev
		}
		sc.Total++
		switch bucket {
		case "resolved":
			sc.Resolved++
		case "deferred":
			sc.Deferred++
		case "wontfix":
			sc.Wontfix++
		default:
			sc.Open++
		}

		if debtIsLive(r) {
			compCount[debtComponent(r.File)]++
			// No clock, no age profile. The only production caller passes a zero
			// `now`, against which every age is negative and clamps to 0, so the
			// bands were computed for nobody and would have handed a future caller
			// a profile claiming the whole backlog is 0-7 days old.
			if !now.IsZero() {
				if days, ok := debtAgeDays(r, now); ok {
					ageCount[debtBandLabel(days)]++
				} else {
					ageCount["unknown"]++
				}
			}
			top = append(top, r)
		}
	}

	s.BySeverity = sevCounts

	// Components: Total desc, then name asc for a stable presentation.
	for name, n := range compCount {
		s.ByComponent = append(s.ByComponent, debtComponentCount{Component: name, Total: n})
	}
	sort.Slice(s.ByComponent, func(i, j int) bool {
		if s.ByComponent[i].Total != s.ByComponent[j].Total {
			return s.ByComponent[i].Total > s.ByComponent[j].Total
		}
		return s.ByComponent[i].Component < s.ByComponent[j].Component
	})

	// Age: emit the fixed bands in order (only those with a count), then unknown.
	for _, b := range debtAgeBands {
		if n := ageCount[b.label]; n > 0 {
			s.ByAge = append(s.ByAge, debtAgeBucket{Label: b.label, Count: n})
		}
	}
	if n := ageCount["unknown"]; n > 0 {
		s.ByAge = append(s.ByAge, debtAgeBucket{Label: "unknown", Count: n})
	}

	// Top priority: severity then age (oldest first), deterministic on ties. The
	// cap is unconditional: the CLI rejects a negative topN as a usage error
	// before renderDebtDashboard — the only caller — can pass one here.
	_ = sortDebt(top, sortKeySeverity) // severity rank, then date asc, then file
	if len(top) > topN {
		top = top[:topN]
	}
	s.Top = top

	return s
}

// renderDebtDashboard renders a deterministic Markdown dashboard from recs:
// totals, a per-severity breakdown, a per-component rollup, an age profile
// grouped by calendar month (so the render never drifts merely because time
// passed), and a top-priority list of the topN highest live items.
//
// The output contains NO generation timestamp: a stable render is what lets
// `atcr debt dashboard --check` detect real content drift rather than clock
// movement. Free-text problem statements in the top list are passed through the
// secret-token redactor so a key accidentally pasted into a finding never lands
// in a published dashboard.
func renderDebtDashboard(recs []localdebt.Record, topN int) string {
	red := log.NewRedactor("") // empty root: token scrub only, no path relativization

	// Severity/component/top are all independent of the wall clock. summarizeDebt
	// needs a `now` only for its relative-age buckets, which the dashboard does
	// not use (it renders a time-invariant by-month view below), so a zero time
	// is passed — which summarizeDebt reads as "no clock" and leaves ByAge nil
	// instead of bucketing every record against year zero.
	sum := summarizeDebt(recs, time.Time{}, topN)

	var b strings.Builder
	b.WriteString("# Technical Debt Dashboard\n\n")
	b.WriteString("_Generated by `atcr debt dashboard` — do not edit by hand; regenerate to update._\n\n")

	fmt.Fprintf(&b, "**Total:** %d  |  **Open:** %d  |  **Deferred:** %d  |  **Resolved:** %d  |  **Wontfix:** %d\n\n",
		sum.Total, sum.Open, sum.Deferred, sum.Resolved, sum.Wontfix)

	// By severity.
	b.WriteString("## By Severity\n\n")
	b.WriteString("| Severity | Open | Deferred | Resolved | Wontfix | Total |\n")
	b.WriteString("|----------|------|----------|----------|---------|-------|\n")
	for _, c := range sum.BySeverity {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d |\n", c.Severity, c.Open, c.Deferred, c.Resolved, c.Wontfix, c.Total)
	}
	b.WriteString("\n")

	// By component — live items only; the header names the scope so the count
	// cannot be misread as all-statuses.
	b.WriteString("## By Component\n\n")
	if len(sum.ByComponent) == 0 {
		b.WriteString("_No items._\n\n")
	} else {
		b.WriteString("| Component | Unresolved |\n|-----------|------------|\n")
		for _, c := range sum.ByComponent {
			fmt.Fprintf(&b, "| %s | %d |\n", debtMarkdownCell(c.Component), c.Total)
		}
		b.WriteString("\n")
	}

	// By age — live items grouped by month (YYYY-MM), oldest first.
	b.WriteString("## By Age (unresolved, by month)\n\n")
	months := debtMonthHistogram(recs)
	if len(months) == 0 {
		b.WriteString("_No unresolved items._\n\n")
	} else {
		b.WriteString("| Month | Unresolved |\n|-------|------------|\n")
		for _, m := range months {
			fmt.Fprintf(&b, "| %s | %d |\n", m.month, m.count)
		}
		b.WriteString("\n")
	}

	// Top priority — most-severe, then oldest, live items.
	b.WriteString("## Top Priority\n\n")
	hasBacklog := sum.Open+sum.Deferred > 0
	if !hasBacklog {
		b.WriteString("_No unresolved items._\n")
	} else if topN <= 0 {
		b.WriteString("_(top list suppressed)_\n")
	} else {
		// The id leads the row for the same reason it leads `debt list`: this is
		// the view a human scans for what to fix next, so it is also the view they
		// copy the `atcr debt resolve --resolve <id>` argument out of. It is NOT
		// passed through the redactor — an id is a content hash, and scrubbing it
		// would break the join contract this table exists to serve.
		b.WriteString("| ID | Severity | File | Est | Problem |\n|----|----------|------|-----|---------|\n")
		for _, r := range sum.Top {
			fmt.Fprintf(&b, "| %s | %s | %s | %d | %s |\n",
				debtMarkdownCell(debtIDCell(r.ID)), r.Severity, debtMarkdownCell(debtLocation(r)),
				r.EstMinutes, debtMarkdownCell(red.Redact(r.Problem)))
		}
	}

	return b.String()
}

type debtMonthCount struct {
	month string
	count int
}

// debtMonthHistogram counts live items per calendar month (the YYYY-MM prefix of
// the record's timestamp), sorted oldest month first. A timestamp too short or
// malformed to carry a valid month prefix is bucketed under "unknown", sorted
// last.
func debtMonthHistogram(recs []localdebt.Record) []debtMonthCount {
	counts := map[string]int{}
	for _, r := range recs {
		if !debtIsLive(r) {
			continue
		}
		m := "unknown"
		if len(r.Timestamp) >= 7 {
			if _, err := time.Parse("2006-01", r.Timestamp[:7]); err == nil {
				m = r.Timestamp[:7]
			}
		}
		counts[m]++
	}
	out := make([]debtMonthCount, 0, len(counts))
	for m, n := range counts {
		out = append(out, debtMonthCount{month: m, count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		// "unknown" sorts last; otherwise oldest month first.
		if (out[i].month == "unknown") != (out[j].month == "unknown") {
			return out[j].month == "unknown"
		}
		return out[i].month < out[j].month
	})
	return out
}

// debtMarkdownCell makes a value safe to embed in a Markdown table cell:
// newlines collapse to spaces and literal pipes become "/", so a crafted or
// hand-edited field cannot break the table's column count. It is distinct from
// this package's sanitizeCell (which strips terminal control sequences for the
// scorecard's plain-text tables) because the hazard is different: here it is
// Markdown structure, not ANSI injection.
func debtMarkdownCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	return strings.TrimSpace(s)
}
