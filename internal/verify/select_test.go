package verify

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// buildRegistry merges skeptic and reviewer agent maps into one *Registry.
func buildRegistry(skeptics, reviewers map[string]registry.AgentConfig) *registry.Registry {
	agents := make(map[string]registry.AgentConfig)
	for n, c := range skeptics {
		agents[n] = c
	}
	for n, c := range reviewers {
		agents[n] = c
	}
	return &registry.Registry{Agents: agents}
}

// namesOf reverse-maps selected configs back to their agent names using the
// combined agent map, preserving selection order. Fixtures keep each config
// distinct (Role+Model) so the DeepEqual match is unambiguous.
func namesOf(reg *registry.Registry, got []registry.AgentConfig) []string {
	names := make([]string, 0, len(got))
	for _, g := range got {
		for name, cfg := range reg.Agents {
			if reflect.DeepEqual(cfg, g) {
				names = append(names, name)
				break
			}
		}
	}
	return names
}

func skeptic(model string) registry.AgentConfig {
	return registry.AgentConfig{Provider: "p", Model: model, Role: registry.RoleSkeptic}
}

func reviewer(model string) registry.AgentConfig {
	return registry.AgentConfig{Provider: "p", Model: model, Role: registry.RoleReviewer}
}

func TestSelectEligibleSkeptics(t *testing.T) {
	tests := []struct {
		name      string
		skeptics  map[string]registry.AgentConfig
		reviewers map[string]registry.AgentConfig
		finding   reconcile.JSONFinding
		n         int
		wantNames []string // expected, in selection order
	}{
		{
			name:      "different-model skeptic is eligible",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{"alice": reviewer("claude-sonnet-4-20250514")},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice"}},
			n:         1,
			wantNames: []string{"s1"},
		},
		{
			name: "multiple eligible n=2 ordered by name, shared-model excluded",
			skeptics: map[string]registry.AgentConfig{
				"s1": skeptic("gpt-4o"),
				"s2": skeptic("gemini-2.5-pro"),
				"s3": skeptic("claude-sonnet-4-20250514"),
			},
			reviewers: map[string]registry.AgentConfig{"alice": reviewer("claude-sonnet-4-20250514")},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice"}},
			n:         2,
			wantNames: []string{"s1", "s2"},
		},
		{
			name:      "exact string match no aliasing",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{"alice": reviewer("gpt-4o-mini")},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice"}},
			n:         1,
			wantNames: []string{"s1"},
		},
		{
			name:      "unresolvable reviewer skipped silently",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{},
			finding:   reconcile.JSONFinding{Reviewers: []string{"ghost"}},
			n:         1,
			wantNames: []string{"s1"},
		},
		{
			name:      "n greater than candidates returns all",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{},
			finding:   reconcile.JSONFinding{Reviewers: nil},
			n:         5,
			wantNames: []string{"s1"},
		},
		{
			name:      "n zero returns empty",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{},
			finding:   reconcile.JSONFinding{Reviewers: nil},
			n:         0,
			wantNames: []string{},
		},
		{
			name:      "no skeptics registered returns empty",
			skeptics:  map[string]registry.AgentConfig{},
			reviewers: map[string]registry.AgentConfig{"alice": reviewer("gpt-4o")},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice"}},
			n:         2,
			wantNames: []string{},
		},
		{
			name: "all skeptics excluded returns empty",
			skeptics: map[string]registry.AgentConfig{
				"s1": skeptic("gpt-4o"),
				"s2": skeptic("claude-sonnet-4-20250514"),
			},
			reviewers: map[string]registry.AgentConfig{
				"alice": reviewer("gpt-4o"),
				"bob":   reviewer("claude-sonnet-4-20250514"),
			},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice", "bob"}},
			n:         2,
			wantNames: []string{},
		},
		{
			name:      "nil reviewers all eligible",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{},
			finding:   reconcile.JSONFinding{Reviewers: nil},
			n:         1,
			wantNames: []string{"s1"},
		},
		{
			name: "more eligible than n truncates to first n by name",
			skeptics: map[string]registry.AgentConfig{
				"alpha": skeptic("m-alpha"),
				"mango": skeptic("m-mango"),
				"zebra": skeptic("m-zebra"),
			},
			reviewers: map[string]registry.AgentConfig{},
			finding:   reconcile.JSONFinding{Reviewers: nil},
			n:         2,
			wantNames: []string{"alpha", "mango"},
		},
		{
			name: "deterministic ordering by name",
			skeptics: map[string]registry.AgentConfig{
				"zebra": skeptic("m-zebra"),
				"alpha": skeptic("m-alpha"),
				"mango": skeptic("m-mango"),
			},
			reviewers: map[string]registry.AgentConfig{},
			finding:   reconcile.JSONFinding{Reviewers: nil},
			n:         3,
			wantNames: []string{"alpha", "mango", "zebra"},
		},
		{
			name:      "duplicate reviewer names handled",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{"alice": reviewer("claude-sonnet-4-20250514")},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice", "alice"}},
			n:         1,
			wantNames: []string{"s1"},
		},
		{
			name:      "single skeptic excluded by single reviewer model",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{"alice": reviewer("gpt-4o")},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice"}},
			n:         1,
			wantNames: []string{},
		},
		{
			name: "multiple reviewers exclude by union of models",
			skeptics: map[string]registry.AgentConfig{
				"s1": skeptic("gpt-4o"),
				"s2": skeptic("gemini-2.5-pro"),
			},
			reviewers: map[string]registry.AgentConfig{
				"alice": reviewer("claude-sonnet-4-20250514"),
				"bob":   reviewer("gpt-4o"),
			},
			finding:   reconcile.JSONFinding{Reviewers: []string{"alice", "bob"}},
			n:         10,
			wantNames: []string{"s2"},
		},
		{
			name:      "empty-role reviewer model still excludes",
			skeptics:  map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
			reviewers: map[string]registry.AgentConfig{"legacy": {Provider: "p", Model: "gpt-4o", Role: ""}},
			finding:   reconcile.JSONFinding{Reviewers: []string{"legacy"}},
			n:         5,
			wantNames: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := buildRegistry(tt.skeptics, tt.reviewers)
			got := SelectEligibleSkeptics(reg, tt.finding, tt.n)
			assert.NotNil(t, got, "result must be non-nil even when empty")
			assert.Equal(t, tt.wantNames, namesOf(reg, got))
		})
	}
}

// TestSelectEligibleSkeptics_NilRegistry covers the defensive nil-registry guard
// (AC 01-02 Error Scenario 1): no panic, non-nil empty slice.
func TestSelectEligibleSkeptics_NilRegistry(t *testing.T) {
	got := SelectEligibleSkeptics(nil, reconcile.JSONFinding{Reviewers: []string{"alice"}}, 2)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

// TestSelectEligibleSkeptics_MultipleFindings confirms each finding gets its own
// eligible set based on its own reviewer models (AC 01-05 Edge Case 3).
func TestSelectEligibleSkeptics_MultipleFindings(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"s1": skeptic("gpt-4o"),
			"s2": skeptic("gemini-2.5-pro"),
			"s3": skeptic("claude-sonnet-4-20250514"),
		},
		map[string]registry.AgentConfig{
			"alice": reviewer("gpt-4o"),
			"bob":   reviewer("gemini-2.5-pro"),
		},
	)

	f1 := reconcile.JSONFinding{Reviewers: []string{"alice"}}        // excludes s1
	f2 := reconcile.JSONFinding{Reviewers: []string{"bob"}}          // excludes s2
	f3 := reconcile.JSONFinding{Reviewers: []string{"alice", "bob"}} // excludes s1+s2

	assert.Equal(t, []string{"s2", "s3"}, namesOf(reg, SelectEligibleSkeptics(reg, f1, 10)))
	assert.Equal(t, []string{"s1", "s3"}, namesOf(reg, SelectEligibleSkeptics(reg, f2, 10)))
	assert.Equal(t, []string{"s3"}, namesOf(reg, SelectEligibleSkeptics(reg, f3, 10)))
}

// TestSelectEligibleSkeptics_NoMutation verifies selection is read-only: the
// registry's agent map and the finding's reviewers are unchanged afterwards.
func TestSelectEligibleSkeptics_NoMutation(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{"s1": skeptic("gpt-4o")},
		map[string]registry.AgentConfig{"alice": reviewer("claude-sonnet-4-20250514")},
	)
	finding := reconcile.JSONFinding{Reviewers: []string{"alice", "alice"}}
	before := finding.Reviewers

	_ = SelectEligibleSkeptics(reg, finding, 1)

	assert.Equal(t, registry.RoleSkeptic, reg.Agents["s1"].Role)
	assert.Equal(t, []string{"alice", "alice"}, before, "finding reviewers must not be mutated")
	assert.Len(t, reg.Agents, 2)
}
