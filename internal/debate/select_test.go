package debate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// kinds returns the Kind of each item, in order — a compact assertion helper.
func kinds(items []reconcile.DisagreementItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Kind
	}
	return out
}

// TestDriftGuard_TriggerConstantsMatchReconcileKinds fails if the registry's
// debate-trigger constants ever diverge from the reconcile disagreement kinds
// they mirror — the decoupling (registry not importing reconcile) is only safe
// while the values stay byte-equal.
func TestDriftGuard_TriggerConstantsMatchReconcileKinds(t *testing.T) {
	assert.Equal(t, reconcile.KindSeveritySplit, registry.DebateTriggerSeveritySplit)
	assert.Equal(t, reconcile.KindGrayZone, registry.DebateTriggerGrayZone)
	assert.Equal(t, reconcile.KindVerificationDisagreement, registry.DebateTriggerVerificationDisagreement)
}

func TestResolveConfig_Defaults(t *testing.T) {
	cfg := ResolveConfig(registry.DebateConfig{})
	assert.True(t, cfg.Triggers[registry.DebateTriggerSeveritySplit])
	assert.True(t, cfg.Triggers[registry.DebateTriggerGrayZone])
	assert.True(t, cfg.Triggers[registry.DebateTriggerVerificationDisagreement])
	assert.Len(t, cfg.Triggers, 3)
	assert.Equal(t, registry.DefaultDebateMaxItems, cfg.MaxItems)
	assert.False(t, cfg.AllowSingleModel)
}

func TestResolveConfig_ExplicitZeroMeansUnlimited(t *testing.T) {
	zero := 0
	cfg := ResolveConfig(registry.DebateConfig{MaxItems: &zero, Triggers: []string{registry.DebateTriggerGrayZone}})
	assert.Equal(t, 0, cfg.MaxItems)
	assert.Len(t, cfg.Triggers, 1)
	assert.True(t, cfg.Triggers[registry.DebateTriggerGrayZone])
}

func TestSelectItems(t *testing.T) {
	all := Config{
		Triggers: map[string]bool{
			registry.DebateTriggerSeveritySplit:            true,
			registry.DebateTriggerGrayZone:                 true,
			registry.DebateTriggerVerificationDisagreement: true,
		},
		MaxItems: 5,
	}

	cases := []struct {
		name         string
		cfg          Config
		items        []reconcile.DisagreementItem
		wantSelected []string // kinds, in order
		wantOverflow int
	}{
		{
			name: "under cap selects all matching",
			cfg:  all,
			items: []reconcile.DisagreementItem{
				{Kind: reconcile.KindSeveritySplit, Severity: "HIGH", File: "a.go", Line: 1, Score: 6},
				{Kind: reconcile.KindGrayZone, Severity: "MEDIUM", File: "b.go", Line: 2, Score: 2},
			},
			wantSelected: []string{reconcile.KindSeveritySplit, reconcile.KindGrayZone},
			wantOverflow: 0,
		},
		{
			name: "non-trigger kinds are filtered out",
			cfg:  all,
			items: []reconcile.DisagreementItem{
				{Kind: reconcile.KindSoloFinding, Severity: "CRITICAL", File: "a.go", Line: 1, Score: 9},
				{Kind: reconcile.KindGrayZone, Severity: "LOW", File: "b.go", Line: 2, Score: 1},
			},
			wantSelected: []string{reconcile.KindGrayZone},
			wantOverflow: 0,
		},
		{
			name: "disabled trigger excludes its items entirely",
			cfg: Config{
				Triggers: map[string]bool{registry.DebateTriggerSeveritySplit: true},
				MaxItems: 5,
			},
			items: []reconcile.DisagreementItem{
				{Kind: reconcile.KindSeveritySplit, Severity: "HIGH", File: "a.go", Line: 1, Score: 6},
				{Kind: reconcile.KindGrayZone, Severity: "CRITICAL", File: "b.go", Line: 2, Score: 9},
			},
			wantSelected: []string{reconcile.KindSeveritySplit},
			wantOverflow: 0,
		},
		{
			name: "over cap: severity priority wins over score, overflow recorded",
			cfg: Config{
				Triggers: map[string]bool{
					registry.DebateTriggerSeveritySplit: true,
					registry.DebateTriggerGrayZone:      true,
				},
				MaxItems: 2,
			},
			items: []reconcile.DisagreementItem{
				{Kind: reconcile.KindSeveritySplit, Severity: "CRITICAL", File: "a.go", Line: 1, Score: 1},
				{Kind: reconcile.KindGrayZone, Severity: "MEDIUM", File: "b.go", Line: 2, Score: 9}, // highest score, lowest severity
				{Kind: reconcile.KindSeveritySplit, Severity: "HIGH", File: "c.go", Line: 3, Score: 2},
			},
			// CRITICAL then HIGH selected (severity desc); MEDIUM overflows despite top score.
			wantSelected: []string{reconcile.KindSeveritySplit, reconcile.KindSeveritySplit},
			wantOverflow: 1,
		},
		{
			name: "max_items 0 is unlimited",
			cfg: Config{
				Triggers: map[string]bool{registry.DebateTriggerGrayZone: true},
				MaxItems: 0,
			},
			items: []reconcile.DisagreementItem{
				{Kind: reconcile.KindGrayZone, Severity: "LOW", File: "a.go", Line: 1, Score: 1},
				{Kind: reconcile.KindGrayZone, Severity: "LOW", File: "b.go", Line: 2, Score: 1},
				{Kind: reconcile.KindGrayZone, Severity: "LOW", File: "c.go", Line: 3, Score: 1},
			},
			wantSelected: []string{reconcile.KindGrayZone, reconcile.KindGrayZone, reconcile.KindGrayZone},
			wantOverflow: 0,
		},
		{
			name:         "empty radar yields empty selection",
			cfg:          all,
			items:        nil,
			wantSelected: []string{},
			wantOverflow: 0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			df := reconcile.DisagreementsFile{Items: tc.items}
			got := SelectItems(df, tc.cfg)
			assert.Equal(t, tc.wantSelected, kinds(got.Selected))
			assert.Len(t, got.Overflow, tc.wantOverflow)
		})
	}
}

// TestSelectItems_OverflowIsTheLowestPriority verifies the overflow tail holds
// exactly the items that lost the priority ordering, so the report's "not
// debated" disclosure is accurate.
func TestSelectItems_OverflowIsTheLowestPriority(t *testing.T) {
	cfg := Config{
		Triggers: map[string]bool{registry.DebateTriggerSeveritySplit: true},
		MaxItems: 1,
	}
	df := reconcile.DisagreementsFile{Items: []reconcile.DisagreementItem{
		{Kind: reconcile.KindSeveritySplit, Severity: "LOW", File: "a.go", Line: 1, Score: 9},
		{Kind: reconcile.KindSeveritySplit, Severity: "CRITICAL", File: "b.go", Line: 2, Score: 1},
	}}
	got := SelectItems(df, cfg)
	require.Len(t, got.Selected, 1)
	require.Len(t, got.Overflow, 1)
	assert.Equal(t, "CRITICAL", got.Selected[0].Severity)
	assert.Equal(t, "LOW", got.Overflow[0].Severity)
}
