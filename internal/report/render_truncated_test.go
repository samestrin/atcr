package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/samestrin/atcr/internal/reconcile"
	reclib "github.com/samestrin/atcr/reconcile"
)

// TestWriteSkepticBlock_AnnotatesATruncatedRead closes the half of the
// truncation signal that never reached a human.
//
// invokeSkeptic exempts a trip on a window-DERIVED tool ceiling: the read is cut
// short but the skeptic's confirmed/refuted STANDS. Its own comment promises
// "the audit record says the skeptic worked from a shortened view" — and that
// fact reached exactly one artifact, reconciled/verification.json. report.md
// rendered such a finding with NO caveat at all, because writeSkepticBlock
// annotated only the unverifiable case and the model's own Reasoning never
// mentions the truncation (it comes from parseVerdict, not failureNotes).
//
// A reader of the report therefore could not tell a verdict formed from the
// whole file from one formed from a fragment of it.
func TestWriteSkepticBlock_AnnotatesATruncatedRead(t *testing.T) {
	t.Parallel()

	t.Run("a truncated confirmed verdict is flagged", func(t *testing.T) {
		t.Parallel()
		var b bytes.Buffer
		writeSkepticBlock(&b, &reclib.Verification{
			Verdict: reclib.VerdictConfirmed, Skeptic: "otto", Truncated: true,
			Notes: "read token.go and confirmed the missing verify",
		})
		out := b.String()
		assert.Contains(t, out, "confirmed", "the verdict still stands — this is an annotation, not a downgrade")
		assert.Contains(t, strings.ToLower(out), "truncated",
			"a verdict formed from a shortened read must say so where a human reads it")
	})

	t.Run("an untruncated verdict gains nothing", func(t *testing.T) {
		t.Parallel()
		var b bytes.Buffer
		writeSkepticBlock(&b, &reclib.Verification{
			Verdict: reclib.VerdictConfirmed, Skeptic: "otto",
		})
		assert.NotContains(t, strings.ToLower(b.String()), "truncated",
			"the annotation must be absent on a full read, or it says nothing")
	})

	t.Run("a truncated unverifiable verdict keeps both annotations", func(t *testing.T) {
		t.Parallel()
		var b bytes.Buffer
		writeSkepticBlock(&b, &reclib.Verification{
			Verdict: reclib.VerdictUnverifiable, Skeptic: "otto", Truncated: true,
		})
		out := strings.ToLower(b.String())
		assert.Contains(t, out, "could not verify", "the existing unverifiable annotation must survive")
		assert.Contains(t, out, "truncated", "the two facts are independent and both belong on the line")
	})
}

// TestRenderReport_RefutedSectionCarriesTheTruncationCaveat closes the gap the
// per-finding annotation alone leaves open, and it is the highest-stakes one.
//
// A refuted finding does NOT go through writeSkepticBlock — it is rendered in
// the collapsed "Refuted Findings" section by writeRefutedSection, which builds
// its own abbreviated line. And refuted is precisely the verdict the
// derived-ceiling exemption exists to protect (a skeptic that reads large files
// and correctly refutes a false positive), AND the verdict reconcile's gate
// EXCLUDES. So a refuted-from-a-truncated-read silently removes a finding from
// CI, in the one place the report offered no caveat at all.
func TestRenderReport_RefutedSectionCarriesTheTruncationCaveat(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writeRefutedSection(&b, []reconcile.JSONFinding{{
		File: "db/query.go", Line: 13, Confidence: "LOW", Problem: "sql built by concatenation",
		Verification: &reclib.Verification{
			Verdict: reclib.VerdictRefuted, Skeptic: "otto", Truncated: true,
			Notes: "the input is bound, not concatenated",
		},
	}})
	out := b.String()
	assert.Contains(t, strings.ToLower(out), "truncated",
		"the gate DROPS a refuted finding — a reader must see it was refuted from a shortened read")
}

// TestRenderReport_RefutedSectionSilentOnAFullRead is the other half: the
// caveat must be absent when the read was complete, or it says nothing.
func TestRenderReport_RefutedSectionSilentOnAFullRead(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writeRefutedSection(&b, []reconcile.JSONFinding{{
		File: "db/query.go", Line: 13, Confidence: "LOW", Problem: "sql built by concatenation",
		Verification: &reclib.Verification{
			Verdict: reclib.VerdictRefuted, Skeptic: "otto",
		},
	}})
	assert.NotContains(t, strings.ToLower(b.String()), "truncated")
}
