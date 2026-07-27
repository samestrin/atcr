package registry

import (
	"reflect"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestPayloadEscalationMirrorsPayloadOverrides pins registry.PayloadEscalationConfig
// to internal/payload.EscalationOverrides field for field.
//
// The two types are duplicated on purpose: registry imports nothing under
// internal/, so it cannot reference payload's type, and payload must not import
// registry (see internal/payload/sprintplan.go). That is the same boundary that
// keeps validPayloadModes hand-synced in payload.go. This test makes the
// duplication safe — adding a threshold to one struct and not the other fails
// here rather than silently dropping the operator's setting on the floor.
func TestPayloadEscalationMirrorsPayloadOverrides(t *testing.T) {
	reg := reflect.TypeOf(PayloadEscalationConfig{})
	pay := reflect.TypeOf(payload.EscalationOverrides{})

	require.Equal(t, pay.NumField(), reg.NumField(),
		"registry.PayloadEscalationConfig and payload.EscalationOverrides must have the same fields")

	for i := 0; i < reg.NumField(); i++ {
		rf := reg.Field(i)
		pf, ok := pay.FieldByName(rf.Name)
		require.Truef(t, ok, "payload.EscalationOverrides is missing field %s", rf.Name)
		require.Equalf(t, pf.Type, rf.Type, "field %s type drifted between the two structs", rf.Name)
	}
}

func TestPayloadEscalation_AbsentBlockIsAllUnset(t *testing.T) {
	var r Registry

	// A registry with no payload_escalation block must leave every threshold
	// unset so the resolver applies the built-in defaults.
	require.Nil(t, r.PayloadEscalation.ChurnRatio)
	require.Nil(t, r.PayloadEscalation.MinHunks)
	require.Nil(t, r.PayloadEscalation.HunkGapLines)
	require.Nil(t, r.PayloadEscalation.MinCyclomatic)
	require.Nil(t, r.PayloadEscalation.MaxFiles)
}

func TestPayloadEscalation_ParsesFromYAML(t *testing.T) {
	var r Registry
	err := yaml.Unmarshal([]byte(`
payload_escalation:
  churn_ratio: 0.75
  min_hunks: 6
  hunk_gap_lines: 4
  min_cyclomatic: 20
  max_files: 120
`), &r)
	require.NoError(t, err)

	require.NotNil(t, r.PayloadEscalation.ChurnRatio)
	require.Equal(t, 0.75, *r.PayloadEscalation.ChurnRatio)
	require.Equal(t, 6, *r.PayloadEscalation.MinHunks)
	require.Equal(t, 4, *r.PayloadEscalation.HunkGapLines)
	require.Equal(t, 20, *r.PayloadEscalation.MinCyclomatic)
	require.Equal(t, 120, *r.PayloadEscalation.MaxFiles)
}

func TestPayloadEscalation_ValidateRejectsNegatives(t *testing.T) {
	neg := -1
	negF := -0.5

	for name, cfg := range map[string]PayloadEscalationConfig{
		"churn_ratio":    {ChurnRatio: &negF},
		"min_hunks":      {MinHunks: &neg},
		"hunk_gap_lines": {HunkGapLines: &neg},
		"min_cyclomatic": {MinCyclomatic: &neg},
		"max_files":      {MaxFiles: &neg},
	} {
		t.Run(name, func(t *testing.T) {
			r := Registry{PayloadEscalation: cfg}
			err := r.validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "payload_escalation."+name)
		})
	}
}

func TestPayloadEscalation_ValidateRejectsChurnRatioAboveOne(t *testing.T) {
	over := 1.5
	r := Registry{PayloadEscalation: PayloadEscalationConfig{ChurnRatio: &over}}

	err := r.validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "payload_escalation.churn_ratio")
}

func TestPayloadEscalation_ValidateAcceptsZeroAndDefaults(t *testing.T) {
	zero := 0
	zeroF := 0.0
	one := 1.0

	// Zero is the documented "disable this signal" value, and 1.0 is a legal
	// churn ratio (every line changed) — neither is an error.
	r := Registry{PayloadEscalation: PayloadEscalationConfig{
		ChurnRatio: &zeroF, MinHunks: &zero, HunkGapLines: &zero, MinCyclomatic: &zero, MaxFiles: &zero,
	}}
	require.NoError(t, r.validate())

	r = Registry{PayloadEscalation: PayloadEscalationConfig{ChurnRatio: &one}}
	require.NoError(t, r.validate())

	require.NoError(t, (&Registry{}).validate())
}
