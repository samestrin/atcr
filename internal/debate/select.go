// Package debate implements the cross-examination stage (Epic 6.0): where
// reviewers disagree (severity splits, gray-zone clusters) or a finding is
// unverifiable by skeptic disagreement, a bounded proposer/challenger/judge
// debate settles the finding. This file covers trigger filtering and item
// selection (which disputed items to debate, in what priority, under the
// cost cap). The protocol engine, judge envelope, and artifact emission live
// in sibling files.
//
// Import-cycle guard: debate imports reconcile, registry, stream; none of them
// import debate. Keep it that way.
package debate

import (
	"sort"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/stream"
)

// Config is the resolved (defaults-applied) debate configuration the stage acts
// on. Triggers is the enabled-kind set; MaxItems is the cost cap (0 = unlimited);
// AllowSingleModel opts in to same-model persona fallback.
type Config struct {
	Triggers         map[string]bool
	MaxItems         int // 0 = unlimited
	AllowSingleModel bool
	MaxParallel      int // bounded worker pool cap; 0 = default 4 (resolved at the loop)
}

// ResolveConfig turns the registry's optional DebateConfig into the resolved
// Config the stage acts on, applying defaults: an empty trigger list enables all
// three kinds, an unset (nil) max_items resolves to DefaultDebateMaxItems, and an
// explicit 0 means unlimited. It is pure and total — a zero-value DebateConfig
// yields the full default configuration.
func ResolveConfig(dc registry.DebateConfig) Config {
	triggers := map[string]bool{}
	src := dc.Triggers
	if len(src) == 0 {
		// Shared default source — the same set applyDefaults fills at load — so the
		// two resolution paths cannot drift to different enabled sets.
		src = registry.DefaultDebateTriggers()
	}
	for _, t := range src {
		triggers[t] = true
	}
	maxItems := registry.DefaultDebateMaxItems
	if dc.MaxItems != nil {
		maxItems = *dc.MaxItems
	}
	return Config{Triggers: triggers, MaxItems: maxItems, AllowSingleModel: dc.AllowSingleModel, MaxParallel: dc.MaxParallel}
}

// Selection is the outcome of trigger filtering and cost-cap application over a
// disagreements radar. Selected items are debated, in priority order (highest
// severity first); Overflow items matched an enabled trigger but exceeded
// MaxItems and are recorded (never silently dropped) so the report can disclose
// what was not debated.
type Selection struct {
	Selected []reconcile.DisagreementItem
	Overflow []reconcile.DisagreementItem
}

// SelectItems filters a disagreements radar to the items whose kind is an enabled
// trigger, orders them by debate priority, and applies the MaxItems cost cap.
//
// Priority order is severity rank descending (the cap should spend the budget on
// the most severe disputes first — the epic's "priority by severity"), then the
// radar's tension score descending, then a stable total order (file, line, kind)
// so the same radar always yields the same selection.
func SelectItems(df reconcile.DisagreementsFile, cfg Config) Selection {
	matched := make([]reconcile.DisagreementItem, 0, len(df.Items))
	for _, it := range df.Items {
		if cfg.Triggers[it.Kind] {
			matched = append(matched, it)
		}
	}
	sortByPriority(matched)

	// MaxItems == 0 means unlimited; a list at or under the cap is fully selected.
	if cfg.MaxItems <= 0 || len(matched) <= cfg.MaxItems {
		return Selection{Selected: matched, Overflow: []reconcile.DisagreementItem{}}
	}
	return Selection{
		Selected: matched[:cfg.MaxItems],
		Overflow: matched[cfg.MaxItems:],
	}
}

// sortByPriority orders disputed items by debate priority in place: severity rank
// descending first (spend the cost cap on the most severe disputes), then the
// radar tension score descending, then a stable total order (file, line, kind)
// so the same radar always yields the same selection. The order is independent
// of the radar's own score-first sort: "priority by severity" is the cost-cap
// contract, distinct from the radar's tension ranking.
func sortByPriority(items []reconcile.DisagreementItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		ra := reconcile.SeverityRank[stream.NormalizeSeverity(a.Severity)]
		rb := reconcile.SeverityRank[stream.NormalizeSeverity(b.Severity)]
		if ra != rb {
			return ra > rb
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Kind < b.Kind
	})
}
