package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPayloadEscalation_AbsentBlockIsAllUnset(t *testing.T) {
	var r Registry

	// A registry with no payload_escalation block must leave every threshold
	// unset so the resolver applies the built-in defaults.
	require.Nil(t, r.PayloadEscalation.ChurnRatio)
	require.Nil(t, r.PayloadEscalation.MinHunks)
	require.Nil(t, r.PayloadEscalation.HunkGapLines)
	require.Nil(t, r.PayloadEscalation.MinCyclomatic)
	require.Nil(t, r.PayloadEscalation.MaxFiles)
	require.Nil(t, r.PayloadEscalation.MaxSkeletonLines)
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
  max_skeleton_lines: 12
`), &r)
	require.NoError(t, err)

	require.NotNil(t, r.PayloadEscalation.ChurnRatio)
	require.Equal(t, 0.75, *r.PayloadEscalation.ChurnRatio)
	require.Equal(t, 6, *r.PayloadEscalation.MinHunks)
	require.Equal(t, 4, *r.PayloadEscalation.HunkGapLines)
	require.Equal(t, 20, *r.PayloadEscalation.MinCyclomatic)
	require.Equal(t, 120, *r.PayloadEscalation.MaxFiles)
	require.Equal(t, 12, *r.PayloadEscalation.MaxSkeletonLines)
}

func TestPayloadEscalation_ValidateRejectsNegatives(t *testing.T) {
	neg := -1
	negF := -0.5

	for name, cfg := range map[string]PayloadEscalationConfig{
		"churn_ratio":        {ChurnRatio: &negF},
		"min_hunks":          {MinHunks: &neg},
		"hunk_gap_lines":     {HunkGapLines: &neg},
		"min_cyclomatic":     {MinCyclomatic: &neg},
		"max_files":          {MaxFiles: &neg},
		"max_skeleton_lines": {MaxSkeletonLines: &neg},
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

func TestPayloadEscalation_ValidateRejectsAboveCeilings(t *testing.T) {
	overGap := MaxEscalationHunkGapLines + 1
	overFiles := MaxEscalationFiles + 1
	overSkel := MaxEscalationSkeletonLines + 1

	for name, cfg := range map[string]PayloadEscalationConfig{
		"hunk_gap_lines":     {HunkGapLines: &overGap},
		"max_files":          {MaxFiles: &overFiles},
		"max_skeleton_lines": {MaxSkeletonLines: &overSkel},
	} {
		t.Run(name, func(t *testing.T) {
			r := Registry{PayloadEscalation: cfg}
			err := r.validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "payload_escalation."+name)
		})
	}

	// The ceilings themselves are legal — only values above them are absurd.
	atGap, atFiles, atSkel := MaxEscalationHunkGapLines, MaxEscalationFiles, MaxEscalationSkeletonLines
	r := Registry{PayloadEscalation: PayloadEscalationConfig{
		HunkGapLines: &atGap, MaxFiles: &atFiles, MaxSkeletonLines: &atSkel,
	}}
	require.NoError(t, r.validate())
}

func TestPayloadEscalation_ValidateRejectsNonFiniteChurnRatio(t *testing.T) {
	// yaml.v3 resolves .nan/.inf to NaN/+Inf, and NaN satisfies neither < 0 nor
	// > 1, so without an explicit non-finite guard these values validate cleanly
	// and silently disable the churn signal downstream.
	for _, doc := range []string{".nan", ".NaN", ".inf", "-.inf"} {
		t.Run(doc, func(t *testing.T) {
			var r Registry
			err := yaml.Unmarshal([]byte("payload_escalation:\n  churn_ratio: "+doc+"\n"), &r)
			require.NoError(t, err)
			require.NotNil(t, r.PayloadEscalation.ChurnRatio)

			err = r.validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), "payload_escalation.churn_ratio")
		})
	}
}

func TestPayloadEscalation_ValidateAcceptsZeroAndDefaults(t *testing.T) {
	zero := 0
	zeroF := 0.0
	one := 1.0

	// Zero is the documented "disable this signal" value, and 1.0 is a legal
	// churn ratio (every line changed) — neither is an error.
	r := Registry{PayloadEscalation: PayloadEscalationConfig{
		ChurnRatio: &zeroF, MinHunks: &zero, HunkGapLines: &zero, MinCyclomatic: &zero, MaxFiles: &zero, MaxSkeletonLines: &zero,
	}}
	require.NoError(t, r.validate())

	r = Registry{PayloadEscalation: PayloadEscalationConfig{ChurnRatio: &one}}
	require.NoError(t, r.validate())

	require.NoError(t, (&Registry{}).validate())
}
