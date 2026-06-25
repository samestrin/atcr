package verify

import (
	"math"
	"os"
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
			got := SelectEligibleSkeptics(reg, tt.finding, tt.n, nil)
			assert.NotNil(t, got, "result must be non-nil even when empty")
			assert.Equal(t, tt.wantNames, skepticNames(got))
		})
	}
}

// TestSelectEligibleSkeptics_NilRegistry covers the defensive nil-registry guard
// (AC 01-02 Error Scenario 1): no panic, non-nil empty slice.
func TestSelectEligibleSkeptics_NilRegistry(t *testing.T) {
	got := SelectEligibleSkeptics(nil, reconcile.JSONFinding{Reviewers: []string{"alice"}}, 2, nil)
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

	assert.Equal(t, []string{"s2", "s3"}, skepticNames(SelectEligibleSkeptics(reg, f1, 10, nil)))
	assert.Equal(t, []string{"s1", "s3"}, skepticNames(SelectEligibleSkeptics(reg, f2, 10, nil)))
	assert.Equal(t, []string{"s3"}, skepticNames(SelectEligibleSkeptics(reg, f3, 10, nil)))
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

	_ = SelectEligibleSkeptics(reg, finding, 1, nil)

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
	got := SelectEligibleSkeptics(reg, reconcile.JSONFinding{}, 10, nil)
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

	got := SelectEligibleSkeptics(reg, reconcile.JSONFinding{}, 1, nil)
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
	got := SelectEligibleSkeptics(reg, reconcile.JSONFinding{Reviewers: []string{"alice"}}, 1, nil)
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

// langSkeptic builds a skeptic AgentConfig declaring the given canonical
// language tokens (dotless, lowercase — the form applyDefaults produces at load).
func langSkeptic(model string, langs ...string) registry.AgentConfig {
	return registry.AgentConfig{Provider: "p", Model: model, Role: registry.RoleSkeptic, Language: langs}
}

// TestSelectEligibleSkeptics_LanguageMatch (AC 03-02): a skeptic whose Language
// scope matches the finding's file extension is partitioned ahead of an unscoped
// skeptic, so the n-cap favors the language-matched one.
func TestSelectEligibleSkeptics_LanguageMatch(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"gopher": langSkeptic("m-gopher", "go"), // matches .go
			"plain":  skeptic("m-plain"),            // no Language → unscoped
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "internal/foo/main.go"}

	// Full order: matched first, then unmatched.
	got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, nil))
	assert.Equal(t, []string{"gopher", "plain"}, got)

	// Under a tight cap the matched skeptic wins despite "gopher" > "plain"
	// alphabetically — proving the partition, not raw name sort, drives selection.
	capped := skepticNames(SelectEligibleSkeptics(reg, finding, 1, nil))
	assert.Equal(t, []string{"gopher"}, capped)
}

// TestSelectEligibleSkeptics_NoMatchFallback (AC 03-04): when no skeptic declares
// the finding's language, selection falls back silently to the prior behavior —
// deterministic alphabetical order, no error, no preference.
func TestSelectEligibleSkeptics_NoMatchFallback(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"zebra": langSkeptic("m-zebra", "py"), // declares python, not rust
			"alpha": langSkeptic("m-alpha", "ts"), // declares typescript, not rust
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "src/lib.rs"} // .rs matches neither

	got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, nil))
	assert.Equal(t, []string{"alpha", "zebra"}, got, "no language match → alphabetical fallback")
}

// TestSelectEligibleSkeptics_TieBreakByScore (AC 03-02): when multiple skeptics
// match the language, the matched partition is ordered by corroboration score
// descending, overriding alphabetical order.
func TestSelectEligibleSkeptics_TieBreakByScore(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"alpha": langSkeptic("m-alpha", "go"),
			"zebra": langSkeptic("m-zebra", "go"),
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "main.go"}
	scores := map[string]float64{"alpha": 0.20, "zebra": 0.90}

	got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, scores))
	assert.Equal(t, []string{"zebra", "alpha"}, got, "higher score sorts first")
}

// TestSelectEligibleSkeptics_NaNScoreLowestRank (AC 03-02): a NaN corroboration
// score must not break strict-weak ordering; it is treated as the lowest rank so
// finite scores always sort above it.
func TestSelectEligibleSkeptics_NaNScoreLowestRank(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"alpha": langSkeptic("m-alpha", "go"),
			"zebra": langSkeptic("m-zebra", "go"),
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "main.go"}
	scores := map[string]float64{"alpha": math.NaN(), "zebra": 0.90}

	got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, scores))
	assert.Equal(t, []string{"zebra", "alpha"}, got, "NaN score sorts below finite scores")
}

// TestSelectEligibleSkeptics_AllNaNScoresAlphabetical: when every matched skeptic
// has a NaN score the comparator ties on the sanitized value and falls back to
// alphabetical order, keeping selection deterministic.
func TestSelectEligibleSkeptics_AllNaNScoresAlphabetical(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"alpha": langSkeptic("m-alpha", "go"),
			"zebra": langSkeptic("m-zebra", "go"),
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "main.go"}
	scores := map[string]float64{"alpha": math.NaN(), "zebra": math.NaN()}

	got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, scores))
	assert.Equal(t, []string{"alpha", "zebra"}, got, "all NaN scores fall back to alphabetical")
}

// TestSelectEligibleSkeptics_TieBreakAlphabeticalWhenNoScores (AC 03-02): equal
// (or absent) scores fall through to alphabetical within the matched partition.
func TestSelectEligibleSkeptics_TieBreakAlphabeticalWhenNoScores(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"zebra": langSkeptic("m-zebra", "go"),
			"alpha": langSkeptic("m-alpha", "go"),
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "main.go"}
	scores := map[string]float64{} // present but empty → no score data

	got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, scores))
	assert.Equal(t, []string{"alpha", "zebra"}, got, "equal scores → alphabetical")
}

// TestSelectEligibleSkeptics_NilScoresMap (AC 03-02/03-03): a nil scores map is a
// valid "no score data" signal — no panic, matched partition ordered alphabetically.
func TestSelectEligibleSkeptics_NilScoresMap(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"zebra": langSkeptic("m-zebra", "go"),
			"alpha": langSkeptic("m-alpha", "go"),
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "main.go"}

	var scores map[string]float64 // nil
	assert.NotPanics(t, func() {
		got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, scores))
		assert.Equal(t, []string{"alpha", "zebra"}, got)
	})
}

// TestSelectEligibleSkeptics_BackwardCompatNoLanguageField (AC 03-05): a registry
// whose skeptics declare no Language field routes exactly as before — alphabetical
// order with the n-cap — regardless of the finding's extension.
func TestSelectEligibleSkeptics_BackwardCompatNoLanguageField(t *testing.T) {
	reg := buildRegistry(
		map[string]registry.AgentConfig{
			"alpha": skeptic("m-alpha"),
			"mango": skeptic("m-mango"),
			"zebra": skeptic("m-zebra"),
		},
		nil,
	)
	finding := reconcile.JSONFinding{File: "main.go"} // extension present but no skeptic scopes it

	got := skepticNames(SelectEligibleSkeptics(reg, finding, 2, nil))
	assert.Equal(t, []string{"alpha", "mango"}, got, "no Language field → prior alphabetical n-cap behavior")
}

// TestSelectEligibleSkeptics_ScoreMapKeyNormalization (TD-013): the scores map
// produced by reviewerCorroborationRates is keyed lowercase, but registry agent
// names retain their YAML casing. Without lowercasing the lookup, a mixed-case
// agent name like "Archer" misses its rate and sorts as if it were 0.
func TestSelectEligibleSkeptics_ScoreMapKeyNormalization(t *testing.T) {
	reg := &registry.Registry{
		Providers: map[string]registry.Provider{"p1": {BaseURL: "u", APIKeyEnv: "K"}},
		Agents: map[string]registry.AgentConfig{
			"Archer": {Role: registry.RoleSkeptic, Model: "m-a", Language: []string{"go"}, Provider: "p1"},
			"bravo":  {Role: registry.RoleSkeptic, Model: "m-b", Language: []string{"go"}, Provider: "p1"},
		},
	}
	finding := reconcile.JSONFinding{File: "main.go", Reviewers: []string{}}
	// scores keyed lowercase — exactly as reviewerCorroborationRates produces.
	scores := map[string]float64{"archer": 0.90, "bravo": 0.20}
	got := skepticNames(SelectEligibleSkeptics(reg, finding, 10, scores))
	require.Equal(t, []string{"Archer", "bravo"}, got, "mixed-case registry key must match lowercase score-map key")
}

// TestSelectEligibleSkeptics_DotfileExtensionless verifies that dotfiles whose
// basename equals filepath.Ext (e.g. .gitignore, .env) are treated as extensionless
// so they do not match language-scoped skeptics. Without the guard, filepath.Ext
// returns the whole name and normalizeExt strips the dot, causing a spurious match.
func TestSelectEligibleSkeptics_DotfileExtensionless(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		langKey string
	}{
		{"gitignore", ".gitignore", "gitignore"},
		{"env", ".env", "env"},
		{"dotgo", ".go", "go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := buildRegistry(
				map[string]registry.AgentConfig{
					"alpha":   langSkeptic("m-alpha", "ts"),         // unscoped for this dotfile
					"matched": langSkeptic("m-matched", tt.langKey), // would wrongly lead without guard
				},
				nil,
			)
			finding := reconcile.JSONFinding{File: tt.file}
			// Under n=1 without guard: "matched" leads (language match); with guard: alphabetical → "alpha".
			got := skepticNames(SelectEligibleSkeptics(reg, finding, 1, nil))
			assert.Equal(t, []string{"alpha"}, got, "dotfile %q must be treated as extensionless", tt.file)
		})
	}
}

// TestSelectEligibleSkeptics_LoadedRegistryNoEmptyLanguage verifies the
// cross-package invariant: a registry loaded via registry.LoadRegistry contains
// no skeptic whose Language slice includes an empty token. If validateAgent
// regresses and stops rejecting empty Language entries, this catches it at the
// registry-load boundary — stronger coverage than the isolated unit test at
// internal/registry/config_test.go:664-695, which tests validateAgent directly
// but not the end-to-end YAML → load path that feeds SelectEligibleSkeptics.
func TestSelectEligibleSkeptics_LoadedRegistryNoEmptyLanguage(t *testing.T) {
	const yamlBody = `
providers:
  mock:
    base_url: http://mock.example.com
    api_key_env: MOCK_KEY
agents:
  gopher:
    provider: mock
    model: mock-model
    role: skeptic
    language:
      - go
      - ts
  generalist:
    provider: mock
    model: mock-model-2
    role: skeptic
`
	f, err := os.CreateTemp("", "registry-*.yaml")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	_, err = f.WriteString(yamlBody)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	reg, err := registry.LoadRegistry(f.Name())
	require.NoError(t, err)

	for name, cfg := range reg.AgentsByRole(registry.RoleSkeptic) {
		for _, lang := range cfg.Language {
			assert.NotEmpty(t, lang,
				"skeptic %q has empty language token in loaded registry — validateAgent must have regressed",
				name)
		}
	}
}
