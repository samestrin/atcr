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
		src = []string{
			registry.DebateTriggerSeveritySplit,
			registry.DebateTriggerGrayZone,
			registry.DebateTriggerVerificationDisagreement,
		}
	}
	for _, t := range src {
		triggers[t] = true
	}
	maxItems := registry.DefaultDebateMaxItems
	if dc.MaxItems != nil {
		maxItems = *dc.MaxItems
	}
	return Config{Triggers: triggers, MaxItems: maxItems, AllowSingleModel: dc.AllowSingleModel}
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
	// stub — implemented in GREEN
	_ = sort.SliceStable
	_ = stream.NormalizeSeverity
	return Selection{}
}
