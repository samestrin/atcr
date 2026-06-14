package reconcile

import (
	"fmt"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
)

// mkMerged builds a Merged with the given severity and category. A non-empty
// verdict attaches a Verification block; the literal "nil" leaves Verification
// nil (a v1 finding); "" attaches a Verification with an empty verdict.
func mkMerged(sev, category, verdict string) Merged {
	m := Merged{Finding: stream.Finding{Severity: sev, Category: category}}
	if verdict != "nil" {
		m.Verification = &Verification{Verdict: verdict, Skeptic: "s"}
	}
	return m
}

// TestGateMatrix is the verdict × severity × flag-state matrix (AC 05-01 /
// 05-02): >= 18 verdict/severity/flag combinations plus nil-verification,
// empty-verdict, out-of-scope, and empty-findings edge cases. It pins the gate
// counter the CLI (atcr reconcile / atcr verify) and MCP (failingFindings) both
// rely on.
func TestGateMatrix(t *testing.T) {
	type tc struct {
		name      string
		findings  []Merged
		threshold string
		require   bool
		want      int
	}

	cases := []tc{}

	// 3 verdicts × 3 severities × 2 flag states at threshold LOW (so AtOrAbove is
	// always satisfied and the verdict/flag dimensions drive the result).
	for _, sev := range []string{SevHigh, SevMedium, SevLow} {
		for _, v := range []string{VerdictConfirmed, VerdictRefuted, VerdictUnverifiable} {
			for _, req := range []bool{false, true} {
				want := 0
				switch {
				case v == VerdictRefuted:
					want = 0 // refuted never counts
				case req:
					want = boolToInt(v == VerdictConfirmed) // only confirmed under requireVerified
				default:
					want = 1 // confirmed + unverifiable count when not requiring verified
				}
				cases = append(cases, tc{
					name:      fmt.Sprintf("%s/%s/require=%v", sev, v, req),
					findings:  []Merged{mkMerged(sev, "security", v)},
					threshold: SevLow,
					require:   req,
					want:      want,
				})
			}
		}
	}

	// Severity-floor dimension: at threshold HIGH a MEDIUM finding is below the
	// floor and never counts; a HIGH one does.
	cases = append(cases,
		tc{"below-floor MEDIUM confirmed @HIGH", []Merged{mkMerged(SevMedium, "security", VerdictConfirmed)}, SevHigh, false, 0},
		tc{"at-floor HIGH confirmed @HIGH", []Merged{mkMerged(SevHigh, "security", VerdictConfirmed)}, SevHigh, false, 1},
		tc{"above-floor CRITICAL confirmed @HIGH", []Merged{mkMerged(SevCritical, "security", VerdictConfirmed)}, SevHigh, true, 1},
	)

	// Edge: nil Verification (v1 finding) — counts under default, not under requireVerified.
	cases = append(cases,
		tc{"nil-verification default", []Merged{mkMerged(SevHigh, "security", "nil")}, SevHigh, false, 1},
		tc{"nil-verification requireVerified", []Merged{mkMerged(SevHigh, "security", "nil")}, SevHigh, true, 0},
	)

	// Edge: empty verdict string — not refuted (counts under default), not VERIFIED.
	cases = append(cases,
		tc{"empty-verdict default", []Merged{mkMerged(SevHigh, "security", "")}, SevHigh, false, 1},
		tc{"empty-verdict requireVerified", []Merged{mkMerged(SevHigh, "security", "")}, SevHigh, true, 0},
	)

	// Edge: out-of-scope confirmed CRITICAL is excluded even under requireVerified.
	cases = append(cases,
		tc{"out-of-scope confirmed", []Merged{mkMerged(SevCritical, CategoryOutOfScope, VerdictConfirmed)}, SevCritical, true, 0},
		tc{"out-of-scope confirmed default", []Merged{mkMerged(SevCritical, CategoryOutOfScope, VerdictConfirmed)}, SevCritical, false, 0},
	)

	// Edge: empty findings list → 0 (gate passes).
	cases = append(cases,
		tc{"empty findings default", nil, SevHigh, false, 0},
		tc{"empty findings requireVerified", nil, SevHigh, true, 0},
	)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, CountAtOrAbove(c.findings, c.threshold, c.require))
		})
	}
}

// TestGateMatrix_StoryFixture is the canonical story fixture (AC 05-02): three
// findings (HIGH+refuted, MEDIUM+confirmed, HIGH+unverifiable) at --fail-on HIGH
// → count=1 without require-verified (the unverifiable HIGH; refuted excluded,
// confirmed is below the HIGH floor), count=0 with require-verified (no VERIFIED
// finding sits at or above HIGH).
func TestGateMatrix_StoryFixture(t *testing.T) {
	findings := []Merged{
		mkMerged(SevHigh, "security", VerdictRefuted),
		mkMerged(SevMedium, "security", VerdictConfirmed),
		mkMerged(SevHigh, "security", VerdictUnverifiable),
	}
	assert.Equal(t, 1, CountAtOrAbove(findings, SevHigh, false))
	assert.Equal(t, 0, CountAtOrAbove(findings, SevHigh, true))
}

// TestGateMatrix_RefutedNeverCounted: a refuted finding is excluded at every
// severity and under both flag states.
func TestGateMatrix_RefutedNeverCounted(t *testing.T) {
	for _, sev := range []string{SevCritical, SevHigh, SevMedium, SevLow} {
		f := []Merged{mkMerged(sev, "security", VerdictRefuted)}
		assert.Equal(t, 0, CountAtOrAbove(f, SevLow, false), "refuted %s default", sev)
		assert.Equal(t, 0, CountAtOrAbove(f, SevLow, true), "refuted %s requireVerified", sev)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
