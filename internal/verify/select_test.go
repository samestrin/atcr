package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// skepticNames extracts the agent names from a selection result, preserving
// order. Selection carries the name directly (Skeptic.Name), so no reverse
// lookup is needed.
func skepticNames(got []Skeptic) []string {
	names := make([]string, 0, len(got))
	for _, s := range got {
		names = append(names, s.Name)
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
			assert.Equal(t, tt.wantNames, skepticNames(got))
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

	assert.Equal(t, []string{"s2", "s3"}, skepticNames(SelectEligibleSkeptics(reg, f1, 10)))
	assert.Equal(t, []string{"s1", "s3"}, skepticNames(SelectEligibleSkeptics(reg, f2, 10)))
	assert.Equal(t, []string{"s3"}, skepticNames(SelectEligibleSkeptics(reg, f3, 10)))
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

// TestSelectEligibleSkeptics_UndefinedProviderExcludesSkeptic asserts that a
// skeptic whose provider key is absent from reg.Providers is excluded at
// selection time rather than being returned with a zero-value Provider that
// silently routes to an empty endpoint at invocation.
func TestSelectEligibleSkeptics_UndefinedProviderExcludesSkeptic(t *testing.T) {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{
			"known": {BaseURL: "http://api.example.com", APIKeyEnv: "KEY"},
		},
		Agents: map[string]registry.AgentConfig{
			"s-valid":   {Provider: "known", Model: "m1", Role: registry.RoleSkeptic, SupportsFC: true},
			"s-missing": {Provider: "undefined", Model: "m2", Role: registry.RoleSkeptic, SupportsFC: true},
		},
	}
	got := SelectEligibleSkeptics(reg, reconcile.JSONFinding{}, 10)
	names := skepticNames(got)
	assert.Equal(t, []string{"s-valid"}, names, "skeptic with undefined provider must be excluded")
	// Additionally, the returned skeptic must carry the resolved Provider.
	require.Len(t, got, 1)
	assert.Equal(t, "http://api.example.com", got[0].Provider.BaseURL)
}

// TestSelectEligibleSkeptics_CarriesConfig confirms the selection carries the
// TestSelectEligibleSkeptics_ScopeSliceIsIndependent verifies the read-only
// contract documented on SelectEligibleSkeptics: appending to a returned Scope
// slice must not corrupt the registry's own copy. Scope is a reference field
// that aliases the registry's backing memory when len==cap (the common case
// after JSON decode). If append would corrupt the registry, this test fails and
// a deep-copy is required.
func TestSelectEligibleSkeptics_ScopeSliceIsIndependent(t *testing.T) {
	t.Parallel()
	reg := buildRegistry(
		map[string]registry.AgentConfig{"sk": {
			Provider: "openai",
			Model:    "gpt-4o",
			Role:     registry.RoleSkeptic,
			Scope:    []string{"foo", "bar"},
		}},
		nil,
	)
	original := []string{"foo", "bar"}

	got := SelectEligibleSkeptics(reg, reconcile.JSONFinding{}, 1)
	require.Len(t, got, 1)

	// Append to the returned Scope — safe only when len==cap so append allocates
	// a new backing array. Assert the registry copy is untouched.
	got[0].Config.Scope = append(got[0].Config.Scope, "injected")

	assert.Equal(t, original, reg.Agents["sk"].Scope,
		"appending to a returned Scope must not mutate the registry backing array")
}

// usable AgentConfig alongside the name, so Phase 2 can invoke the skeptic
// without re-resolving it (AC 01-02: result values are usable, not zeroed).
func TestSelectEligibleSkeptics_CarriesConfig(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{"s1": {Provider: "openai", Model: "gpt-4o", Role: registry.RoleSkeptic}},
		map[string]registry.AgentConfig{"alice": reviewer("claude-sonnet-4-20250514")},
	)
	got := SelectEligibleSkeptics(reg, reconcile.JSONFinding{Reviewers: []string{"alice"}}, 1)
	require.Len(t, got, 1)
	assert.Equal(t, "s1", got[0].Name)
	assert.Equal(t, "openai", got[0].Config.Provider)
	assert.Equal(t, "gpt-4o", got[0].Config.Model)
}

// AC 03-01: normalizeExt canonicalizes a file extension to the same dotless,
// lowercase form AgentConfig.Language entries are canonicalized to, so both
// sides of the language-routing match compare identically. It strips a single
// leading dot and lowercases; a dotless input is already canonical; an empty
// input stays empty.
func TestNormalizeExt_WithAndWithoutDot(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{".go", "go"},
		{"go", "go"},
		{".GO", "go"},
		{"GO", "go"},
		{".TS", "ts"},
		{"", ""},
		{".tar.gz", "tar.gz"}, // only the single leading dot is stripped
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, normalizeExt(tt.in), "normalizeExt(%q)", tt.in)
	}
}
