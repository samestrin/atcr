package scorecard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/timewindow"
)

// FilterOpts narrows the record set before aggregation. An empty field means "no
// restriction" for that dimension. Since is a duration string (Nd/Nw/Nm); Model
// is a case-insensitive substring match (a full id always matches, so exact-id
// callers are unaffected); Persona is an exact-match string.
type FilterOpts struct {
	Since   string
	Model   string
	Persona string
}

// LeaderboardRow is one aggregated (reviewer, model) group: the per-run records
// summed into run count, finding totals, and cost/latency. CostPerCorroborated is
// valid only when HasCostPerCorroborated is true (a group with zero corroborated
// findings has no defined cost-per-corroborated and renders as a dash).
type LeaderboardRow struct {
	Reviewer             string
	Model                string
	Runs                 int
	FindingsRaised       int
	FindingsCorroborated int
	// FindingsDocShielded sums the doc-shield carve-out counts. The leaderboard
	// does not render it; TrustPriors reads it to keep shielded routings inside
	// the trust rate's denominator.
	FindingsDocShielded    int
	CorroborationRate      float64
	TotalCostUSD           float64
	CostPerCorroborated    float64
	HasCostPerCorroborated bool
	AvgLatencyMS           int64
}

// ParseSince parses a leaderboard window string into a duration. It is a thin
// alias for timewindow.Parse, which owns the one grammar every window flag in
// atcr shares — Nd (days), Nw (weeks), Nm (30-day months), plus "all". The alias
// is kept so this package's callers read in its own vocabulary; it must never
// grow a grammar of its own again.
func ParseSince(s string) (time.Duration, error) {
	d, err := timewindow.Parse(s)
	if err != nil {
		// Name the flag: timewindow is shared by several window flags, so the
		// grammar error alone does not tell the user which input to correct.
		return 0, fmt.Errorf("--since: %w", err)
	}
	return d, nil
}

// ApplyFilters returns the per-reviewer records (aggregate records are dropped)
// matching every set filter, evaluated against now for the time window. Filters
// compose with AND semantics; an empty filter field is "match all". The time
// boundary is inclusive (a record exactly at the cutoff is kept). A record whose
// run_id timestamp cannot be parsed is excluded only when a --since window is
// active (it cannot be proven inside the window). An invalid Since is an error.
func ApplyFilters(records []Record, opts FilterOpts, now time.Time) ([]Record, error) {
	var cutoff time.Time
	hasSince := strings.TrimSpace(opts.Since) != ""
	if hasSince {
		d, err := ParseSince(opts.Since)
		if err != nil {
			return nil, err
		}
		cutoff = now.Add(-d)
	}

	out := make([]Record, 0, len(records))
	for _, r := range records {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		// Model is a case-insensitive substring, matching `personas search
		// --model` (the two commands previously disagreed: leaderboard was
		// exact-only). A full exact id still matches, so exact-id callers are
		// unaffected; only previously impossible fragment queries change.
		if opts.Model != "" && !strings.Contains(strings.ToLower(r.Model), strings.ToLower(opts.Model)) {
			continue
		}
		if opts.Persona != "" && r.Reviewer != opts.Persona {
			continue
		}
		if hasSince {
			ts, ok := runIDTime(r.RunID)
			if !ok || ts.Before(cutoff) {
				continue
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// Aggregate groups per-reviewer records by (reviewer, model) and sums them into
// ranked LeaderboardRows. Aggregate records are skipped defensively. Rows are
// sorted by corroboration rate descending, then reviewer then model ascending, so
// the order is deterministic even when rates tie.
func Aggregate(records []Record) []LeaderboardRow {
	type key struct{ reviewer, model string }
	groups := map[key]*LeaderboardRow{}
	order := []key{}
	totalLatency := map[key]int64{}

	for _, r := range records {
		if r.RecordType != RecordTypeReviewer {
			continue
		}
		k := key{r.Reviewer, r.Model}
		row, ok := groups[k]
		if !ok {
			row = &LeaderboardRow{Reviewer: r.Reviewer, Model: r.Model}
			groups[k] = row
			order = append(order, k)
		}
		row.Runs++
		row.FindingsRaised += r.FindingsRaised
		row.FindingsCorroborated += r.FindingsCorroborated
		row.FindingsDocShielded += r.FindingsDocShielded
		row.TotalCostUSD += r.CostUSD
		totalLatency[k] += r.LatencyMS
	}

	rows := make([]LeaderboardRow, 0, len(order))
	for _, k := range order {
		row := groups[k]
		row.CorroborationRate = ratio(row.FindingsCorroborated, row.FindingsRaised)
		if row.FindingsCorroborated > 0 {
			// CostPerCorroborated is, by definition, total cost / corroborated
			// findings ("cost per corroborated finding") — the formula matches the
			// metric name. It is distinct from a hypothetical "cost per verified
			// finding" (cost / (corroborated + refuted)), which is new scope: it
			// would require tracking FindingsRefuted on LeaderboardRow and summing
			// it in Aggregate, not a change to this formula.
			row.CostPerCorroborated = row.TotalCostUSD / float64(row.FindingsCorroborated)
			row.HasCostPerCorroborated = true
		}
		if row.Runs > 0 {
			row.AvgLatencyMS = totalLatency[k] / int64(row.Runs)
		}
		rows = append(rows, *row)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		// Compare rates exactly via cross-multiplication (a/b vs c/d ⇔ a*d vs c*b,
		// all non-negative) rather than the stored float: two groups that are
		// equal-by-value but summed in a different order can differ by a sub-ULP
		// as floats and break the intended (reviewer, model) tie-break.
		//
		// Exactness here assumes the products fit int64. Finding counts are summed
		// across runs but stay far below 2^31 in practice, so each operand is
		// << 2^31 and the product << 2^62 — safely inside int64. Counts large
		// enough to overflow (corroborated AND raised each summing to ≈2^31) are
		// not reachable in realistic operation; should that ever change, fall back
		// to comparing the stored float CorroborationRate when a product overflows.
		li := int64(rows[i].FindingsCorroborated) * int64(rows[j].FindingsRaised)
		lj := int64(rows[j].FindingsCorroborated) * int64(rows[i].FindingsRaised)
		if li != lj {
			return li > lj
		}
		if rows[i].Reviewer != rows[j].Reviewer {
			return rows[i].Reviewer < rows[j].Reviewer
		}
		return rows[i].Model < rows[j].Model
	})
	return rows
}

// runIDTime extracts and parses the RFC3339 timestamp prefix from a run_id for
// --since window comparison; ok is false for a run_id without a parseable prefix.
// The prefix shape-check is shared (rfc3339Prefix, in paths.go); this function
// owns only the parse-to-time.Time step the window comparison needs.
func runIDTime(runID string) (time.Time, bool) {
	m, ok := rfc3339Prefix(runID)
	if !ok {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, m)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}
