package verify

import (
	"context"
	"encoding/json"
	reclib "github.com/samestrin/atcr/reconcile"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

// loadFinding reads a planted fixture from testdata/ into a reconcile.JSONFinding.
func loadFinding(t *testing.T, name string) reconcile.JSONFinding {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoErrorf(t, err, "read fixture %s", name)
	var f reconcile.JSONFinding
	require.NoErrorf(t, json.Unmarshal(data, &f), "fixture %s must parse as reconcile.JSONFinding", name)
	require.NotEmpty(t, f.File)
	require.NotEmpty(t, f.Problem)
	require.NotEmpty(t, f.Severity)
	return f
}

// TestVerifyE2E_PlantedFindings is the executing form of the epic success
// criterion "a deliberately false finding gets refuted; a true finding gets
// confirmed" (AC 06-04 Scenario 6). It drives each planted fixture through the
// full Phase 1–3 pipeline — buildSkepticPrompt → invokeSkeptic → aggregateVerdicts
// → confidenceV2 — with a scripted mock skeptic, and asserts the end-to-end tier.
func TestVerifyE2E_PlantedFindings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		fixture      string
		skepticReply string
		wantVerdict  string
		wantConf     string
	}{
		{
			name:         "true finding confirmed → VERIFIED",
			fixture:      "true-finding.json",
			skepticReply: `{"verdict":"confirmed","reasoning":"read handler.go:42 — req.User is dereferenced on the public route with no nil guard"}`,
			wantVerdict:  verdictConfirmed,
			wantConf:     ConfidenceVerified,
		},
		{
			name:         "false finding refuted → LOW",
			fixture:      "false-finding.json",
			skepticReply: `{"verdict":"refuted","reasoning":"store.go:71 and :88 are both called under s.mu.Lock(); the access is already synchronized"}`,
			wantVerdict:  verdictRefuted,
			wantConf:     reclib.ConfLow,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			finding := loadFinding(t, tc.fixture)

			// Phase 2: prompt construction (entries nil — skeptic reads code via the tool loop).
			prompt := buildSkepticPrompt(finding, nil)
			require.NotEmpty(t, prompt)

			// Phase 2: invocation with a scripted mock skeptic (no real LLM / network).
			cc := finalChat(tc.skepticReply)
			v, _, err := invokeSkeptic(context.Background(), testSkeptic(), prompt, cc, okDispatcher(), false)
			require.NoError(t, err, "runtime failures collapse to unverifiable, never an error")
			require.NotNil(t, v)

			// Phase 2: single-skeptic vote passes through.
			agg := aggregateVerdicts([]*reclib.Verification{v})
			require.NotNil(t, agg)
			assert.Equal(t, tc.wantVerdict, agg.Verdict)

			// Phase 3: confidence v2 from the finding's v1 confidence + the verdict.
			got := confidenceV2(finding.Confidence, agg.Verdict)
			assert.Equalf(t, tc.wantConf, got, "v2 confidence tier for %s", tc.name)
		})
	}
}
