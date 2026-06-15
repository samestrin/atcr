package scorecard

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FilterOpts narrows the record set before aggregation. An empty field means "no
// restriction" for that dimension. Since is a duration string (Nd/Nw/Nm); Model
// and Persona are exact-match strings.
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
	Reviewer               string
	Model                  string
	Runs                   int
	FindingsRaised         int
	FindingsCorroborated   int
	CorroborationRate      float64
	TotalCostUSD           float64
	CostPerCorroborated    float64
	HasCostPerCorroborated bool
	AvgLatencyMS           int64
}

// ParseSince parses a leaderboard window string into a duration: "Nd" days, "Nw"
// weeks, "Nm" months (30-day months). N must be a positive integer. An
// unrecognized unit, a non-integer count, or a non-positive value is an error
// with actionable guidance — the message is shown to the user verbatim.
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, invalidSinceErr(s)
	}
	unit := s[len(s)-1]
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, invalidSinceErr(s)
	}
	var per time.Duration
	switch unit {
	case 'd':
		per = 24 * time.Hour
	case 'w':
		per = 7 * 24 * time.Hour
	case 'm':
		per = 30 * 24 * time.Hour
	default:
		return 0, invalidSinceErr(s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("--since must be a positive duration")
	}
	return time.Duration(n) * per, nil
}

func invalidSinceErr(s string) error {
	return fmt.Errorf("invalid --since value %q: supported formats are Nd (days), Nw (weeks), Nm (months), e.g. 30d, 2w, 3m", s)
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
		if opts.Model != "" && r.Model != opts.Model {
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
		row.TotalCostUSD += r.CostUSD
		totalLatency[k] += r.LatencyMS
	}

	rows := make([]LeaderboardRow, 0, len(order))
	for _, k := range order {
		row := groups[k]
		row.CorroborationRate = ratio(row.FindingsCorroborated, row.FindingsRaised)
		if row.FindingsCorroborated > 0 {
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

// rfc3339Prefix matches the leading RFC3339 timestamp of a run_id
// (<timestamp>-<base>), tolerating both the UTC 'Z' form the emitter writes and a
// numeric offset, so a record is never silently dropped from a --since window
// just because its timestamp carries an offset.
var rfc3339Prefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)

// runIDTime extracts the RFC3339 timestamp prefix from a run_id; ok is false for
// a run_id without a parseable prefix.
func runIDTime(runID string) (time.Time, bool) {
	m := rfc3339Prefix.FindString(runID)
	if m == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, m)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}
