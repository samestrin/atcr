package localdebt

import (
	"sort"
	"strings"
)

// qualitysignal.go builds the content-free, per-(persona, model) quality signal
// (Sprint 30.0) from the append-only local-debt stream. It reads only the
// Reviewers, Model, and Status fields already present on Record — never code,
// file paths, or problem/fix text — so the aggregated shape is structurally
// incapable of carrying finding content.

// foldTerminalByID folds the append-only record stream by ID down to at most one
// TERMINAL record per id, discarding ids that never reached a terminal
// (resolved/wontfix/deferred) status. It reuses FoldRecords for the precedence
// logic — terminal wins over open, higher-precedence terminal wins over lower
// (wontfix > resolved > deferred), later timestamp breaks a same-rank tie — and
// then keeps only the ids whose effective record is terminal, because an
// unresolved (still-open) finding is not yet a quality signal. The fold is O(n):
// FoldRecords does a single keyed pass and this adds one linear filter, with no
// per-id rescan of the whole stream.
func foldTerminalByID(records []Record) []Record {
	effective := FoldRecords(records)
	terminal := make([]Record, 0, len(effective))
	for _, r := range effective {
		if IsClosedStatus(r.Status) {
			terminal = append(terminal, r)
		}
	}
	return terminal
}

// QualityRow is one aggregated per-(persona, model) quality-signal row: how many
// findings that persona+model raised were later dismissed (status wontfix) versus
// confirmed (status resolved). It is the internal aggregation shape — a fixed,
// content-free set of fields (never code, path, or finding text) — that the
// outbound telemetry.QualitySignal payload and the maintainer report are built
// from.
type QualityRow struct {
	Persona        string
	Model          string
	DismissedCount int
	ConfirmedCount int
}

// AggregateQualitySignal folds the append-only debt stream by ID to its terminal
// records, then groups those by (persona, model) and sums dismissed (wontfix) and
// confirmed (resolved) counts, returning one row per distinct pair sorted persona
// ascending then model ascending. It mirrors internal/scorecard/aggregate.go's
// Aggregate() grouping/sort idiom (map-of-key + insertion-order slice +
// sort.SliceStable tie-break).
//
// Exclusion rules (all content-free, reading only Reviewers/Model/Status):
//   - Records with an empty Model (v1, or v2 with unresolved attribution) are
//     excluded from every per-model row rather than bucketed under "" (AC 01-02).
//   - A terminal status that is neither wontfix nor resolved (i.e. deferred)
//     contributes to neither counter and creates no group, so a deferred-only
//     pair emits no row (AC 01-01 EC2).
//   - Every listed persona in Reviewers receives the outcome, deduplicated
//     per-record with empty entries skipped (AC 01-03); an empty Reviewers slice
//     contributes to no group.
//
// It is a total, pure function: nil input yields a non-nil empty slice, and
// repeated calls on the same input are byte-for-byte identical (no shared mutable
// state). Complexity is O(n) fold + O(sum reviewers) group + O(k log k) sort.
func AggregateQualitySignal(records []Record) []QualityRow {
	type key struct{ persona, model string }
	groups := map[key]*QualityRow{}
	order := []key{}

	for _, rec := range foldTerminalByID(records) {
		if rec.Model == "" {
			continue // attribution-incomplete: excluded from per-model rows
		}
		var dismissed, confirmed int
		switch strings.ToLower(strings.TrimSpace(rec.Status)) {
		case "wontfix":
			dismissed = 1
		case "resolved":
			confirmed = 1
		default:
			continue // deferred (or any other terminal) is neither a signal nor a group
		}

		seen := map[string]bool{}
		for _, persona := range rec.Reviewers {
			if persona == "" || seen[persona] {
				continue
			}
			seen[persona] = true
			k := key{persona, rec.Model}
			row, ok := groups[k]
			if !ok {
				row = &QualityRow{Persona: persona, Model: rec.Model}
				groups[k] = row
				order = append(order, k)
			}
			row.DismissedCount += dismissed
			row.ConfirmedCount += confirmed
		}
	}

	rows := make([]QualityRow, 0, len(order))
	for _, k := range order {
		rows = append(rows, *groups[k])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Persona != rows[j].Persona {
			return rows[i].Persona < rows[j].Persona
		}
		return rows[i].Model < rows[j].Model
	})
	return rows
}
