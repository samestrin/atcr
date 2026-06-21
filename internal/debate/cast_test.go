package debate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// rosterReg builds a registry from a compact agent spec for casting tests.
// Each agent is name -> (model, role); all share one provider.
func rosterReg(agents map[string][2]string) *registry.Registry {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p": {APIKeyEnv: "K", BaseURL: "https://x"}},
		Agents:    map[string]registry.AgentConfig{},
	}
	for name, spec := range agents {
		reg.Agents[name] = registry.AgentConfig{Provider: "p", Model: spec[0], Role: spec[1]}
	}
	return reg
}

func splitItem(reviewers ...string) reconcile.DisagreementItem {
	return reconcile.DisagreementItem{
		Kind: reconcile.KindSeveritySplit, File: "a.go", Line: 1, Severity: "HIGH", Reviewers: reviewers,
	}
}

func TestCastRoles_FullDistinctRoster(t *testing.T) {
	reg := rosterReg(map[string][2]string{
		"alice": {"model-a", registry.RoleReviewer},
		"bob":   {"model-b", registry.RoleSkeptic},
		"carol": {"model-c", registry.RoleJudge},
	})
	cast, ok, reason := CastRoles(reg, splitItem("alice"), Config{})
	require.True(t, ok, "reason: %s", reason)
	assert.Equal(t, "alice", cast.Proposer.Agent)
	assert.Equal(t, "bob", cast.Challenger.Agent)
	assert.Equal(t, "carol", cast.Judge.Agent)
	assert.False(t, cast.SingleModel)
	// All three models distinct.
	assert.NotEqual(t, cast.Proposer.Config.Model, cast.Challenger.Config.Model)
	assert.NotEqual(t, cast.Proposer.Config.Model, cast.Judge.Config.Model)
	assert.NotEqual(t, cast.Challenger.Config.Model, cast.Judge.Config.Model)
}

func TestCastRoles_NoSkepticRole_Unresolved(t *testing.T) {
	reg := rosterReg(map[string][2]string{
		"alice": {"model-a", registry.RoleReviewer},
		"carol": {"model-c", registry.RoleJudge},
	})
	_, ok, reason := CastRoles(reg, splitItem("alice"), Config{})
	assert.False(t, ok)
	assert.NotEmpty(t, reason)
}

func TestCastRoles_OnlyTwoDistinctModels_Unresolved(t *testing.T) {
	// skeptic and judge share a model -> cannot reach 3 distinct.
	reg := rosterReg(map[string][2]string{
		"alice": {"model-a", registry.RoleReviewer},
		"bob":   {"model-b", registry.RoleSkeptic},
		"carol": {"model-b", registry.RoleJudge},
	})
	_, ok, _ := CastRoles(reg, splitItem("alice"), Config{})
	assert.False(t, ok)
}

func TestCastRoles_SingleModelFallback_WhenAllowed(t *testing.T) {
	// Only reviewer-role agents (today's baseline). Distinct path fails on roles,
	// but allow_single_model casts all three personas on the proposer's model.
	reg := rosterReg(map[string][2]string{
		"alice": {"model-a", registry.RoleReviewer},
	})
	cast, ok, reason := CastRoles(reg, splitItem("alice"), Config{AllowSingleModel: true})
	require.True(t, ok, "reason: %s", reason)
	assert.True(t, cast.SingleModel)
	assert.Equal(t, "model-a", cast.Proposer.Config.Model)
	assert.Equal(t, "model-a", cast.Challenger.Config.Model)
	assert.Equal(t, "model-a", cast.Judge.Config.Model)
	assert.Equal(t, LabelProposer, cast.Proposer.Label)
	assert.Equal(t, LabelChallenger, cast.Challenger.Label)
	assert.Equal(t, LabelJudge, cast.Judge.Label)
}

func TestCastRoles_SingleModelDisallowed_StaysUnresolved(t *testing.T) {
	reg := rosterReg(map[string][2]string{
		"alice": {"model-a", registry.RoleReviewer},
	})
	_, ok, reason := CastRoles(reg, splitItem("alice"), Config{AllowSingleModel: false})
	assert.False(t, ok)
	assert.NotEmpty(t, reason)
}

func TestCastRoles_NoResolvableProposer(t *testing.T) {
	// Reviewers name an agent absent from the registry, and there is no other
	// reviewer-role agent to stand in.
	reg := rosterReg(map[string][2]string{
		"bob":   {"model-b", registry.RoleSkeptic},
		"carol": {"model-c", registry.RoleJudge},
	})
	_, ok, reason := CastRoles(reg, splitItem("ghost"), Config{AllowSingleModel: true})
	assert.False(t, ok)
	assert.Contains(t, reason, "proposer")
}

func TestCastRoles_ProposerProviderResolved(t *testing.T) {
	reg := rosterReg(map[string][2]string{
		"alice": {"model-a", registry.RoleReviewer},
		"bob":   {"model-b", registry.RoleSkeptic},
		"carol": {"model-c", registry.RoleJudge},
	})
	cast, ok, _ := CastRoles(reg, splitItem("alice"), Config{})
	require.True(t, ok)
	assert.Equal(t, "K", cast.Proposer.Provider.APIKeyEnv)
	assert.Equal(t, "https://x", cast.Judge.Provider.BaseURL)
}

func TestCastRoles_NilProviders_FailsFast(t *testing.T) {
	// A registry with agents but no Providers map should fail at cast time rather
	// than casting zero-value providers that halt every turn.
	reg := &registry.Registry{
		Providers: nil,
		Agents: map[string]registry.AgentConfig{
			"alice": {Provider: "p", Model: "model-a", Role: registry.RoleReviewer},
			"bob":   {Provider: "p", Model: "model-b", Role: registry.RoleSkeptic},
			"carol": {Provider: "p", Model: "model-c", Role: registry.RoleJudge},
		},
	}
	_, ok, reason := CastRoles(reg, splitItem("alice"), Config{})
	assert.False(t, ok)
	assert.Equal(t, ReasonNoProposer, reason)
}
