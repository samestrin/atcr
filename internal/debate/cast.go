package debate

import (
	"sort"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// Role labels for the three debate seats. They double as transcript attribution
// and (in the single-model fallback) the persona a shared model is asked to adopt.
const (
	LabelProposer   = "proposer"
	LabelChallenger = "challenger"
	LabelJudge      = "judge"
)

// Unresolved reasons recorded when a debate item cannot be cast. They are stable
// tokens surfaced in debate.json and the report so an operator can see why an
// item was not debated.
const (
	ReasonNoProposer         = "no_resolvable_proposer"
	ReasonInsufficientModels = "insufficient_distinct_models"
)

// Caster is one filled debate seat: the label, the backing registry agent, and
// the resolved provider/config needed to invoke it through the tool loop.
type Caster struct {
	Label    string
	Agent    string
	Config   registry.AgentConfig
	Provider registry.Provider
}

// Cast is the three-seat assignment for one debated item. SingleModel is true
// when the distinct-model rule could not be met and the same-model persona
// fallback was used (opt-in via Config.AllowSingleModel) — the report discloses
// it so the weaker independence guarantee is never hidden.
type Cast struct {
	Proposer    Caster
	Challenger  Caster
	Judge       Caster
	SingleModel bool
}

// CastRoles assigns the proposer, challenger, and judge for a debated item.
//
// The distinct-model rule is enforced here, never left to configuration: the
// proposer is a crediting reviewer's agent; the challenger is a role:skeptic
// agent whose model differs from the proposer's; the judge is a role:judge agent
// whose model differs from both. Candidates are taken in sorted-name order so the
// cast is deterministic for the same registry and item.
//
// When three distinct models cannot be assembled, the outcome depends on
// cfg.AllowSingleModel:
//   - false (default): the item is left unresolved — CastRoles returns ok=false
//     with a stable reason — rather than silently loosening independence.
//   - true: the same-model persona fallback casts all three seats on the
//     proposer's model (SingleModel=true), simulating the debate via distinct
//     proposer/skeptic/judge personas.
//
// A missing proposer (no crediting reviewer resolves and no reviewer-role agent
// can stand in) is always unresolved, even under AllowSingleModel: a debate needs
// at least one agent to run.
func CastRoles(reg *registry.Registry, item reconcile.DisagreementItem, cfg Config) (Cast, bool, string) {
	if reg == nil {
		return Cast{}, false, ReasonNoProposer
	}

	proposer, ok := resolveProposer(reg, item)
	if !ok {
		return Cast{}, false, ReasonNoProposer
	}

	// Distinct path: a role:skeptic with a different model than the proposer, and
	// a role:judge with a model different from both.
	challenger, cOK := pickDistinct(reg, registry.RoleSkeptic, LabelChallenger, []string{proposer.Config.Model})
	if cOK {
		judge, jOK := pickDistinct(reg, registry.RoleJudge, LabelJudge,
			[]string{proposer.Config.Model, challenger.Config.Model})
		if jOK {
			return Cast{Proposer: proposer, Challenger: challenger, Judge: judge}, true, ""
		}
	}

	// Distinct path failed. Without explicit opt-in, leave the item unresolved.
	if !cfg.AllowSingleModel {
		return Cast{}, false, ReasonInsufficientModels
	}

	// Single-model fallback: all three personas on the proposer's model.
	challengerSeat := proposer
	challengerSeat.Label = LabelChallenger
	judgeSeat := proposer
	judgeSeat.Label = LabelJudge
	return Cast{
		Proposer:    proposer,
		Challenger:  challengerSeat,
		Judge:       judgeSeat,
		SingleModel: true,
	}, true, ""
}

// resolveProposer picks the proposer seat from a crediting reviewer's agent. It
// prefers an agent named in the finding's Reviewers (sorted, deterministic); if
// none resolve to a registered, provider-backed agent, it falls back to any
// reviewer-role agent so a debate can still run. Returns ok=false only when no
// usable reviewer-role agent exists at all.
func resolveProposer(reg *registry.Registry, item reconcile.DisagreementItem) (Caster, bool) {
	reviewers := append([]string{}, item.Reviewers...)
	sort.Strings(reviewers)
	for _, name := range reviewers {
		cfg, ok := reg.Agents[name]
		if !ok {
			continue
		}
		prov, pok := resolveProvider(reg, cfg)
		if !pok {
			continue
		}
		return Caster{Label: LabelProposer, Agent: name, Config: cfg, Provider: prov}, true
	}
	// Fallback: any reviewer-role agent, deterministically.
	if seat, ok := pickDistinct(reg, registry.RoleReviewer, LabelProposer, nil); ok {
		return seat, true
	}
	return Caster{}, false
}

// pickDistinct returns the first agent (sorted by name) whose effective role is
// role and whose model is not in excludeModels, resolved to a provider-backed
// seat with the given label. ok=false when none qualifies.
func pickDistinct(reg *registry.Registry, role, label string, excludeModels []string) (Caster, bool) {
	excluded := map[string]bool{}
	for _, m := range excludeModels {
		excluded[m] = true
	}
	candidates := reg.AgentsByRole(role)
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cfg := candidates[name]
		if excluded[cfg.Model] {
			continue
		}
		prov, ok := resolveProvider(reg, cfg)
		if !ok {
			continue
		}
		return Caster{Label: label, Agent: name, Config: cfg, Provider: prov}, true
	}
	return Caster{}, false
}

// resolveProvider resolves an agent's provider. A nil Providers map or a
// zero-value provider fails so a seat with no endpoint/key is never cast.
func resolveProvider(reg *registry.Registry, cfg registry.AgentConfig) (registry.Provider, bool) {
	if reg.Providers == nil {
		return registry.Provider{}, false
	}
	p, ok := reg.Providers[cfg.Provider]
	if !ok || p.BaseURL == "" {
		return registry.Provider{}, false
	}
	return p, true
}
