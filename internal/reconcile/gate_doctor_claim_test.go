package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD internal/reconcile/gate.go:223: the collapse message and its comment used
// to claim atcr doctor "inspects the roster and cannot see a reply shape".
//
// That is false. doctor's thinking probe reads the reply CONTENT through
// reasoningSignal -> inlineThinking -> llmclient.HasThinkMarkup
// (internal/doctor/run.go), and inlineThinking's own doc states the live
// disagreement IS the bare closer. The honest, narrower claim is that doctor's
// thinking verdict CAN show an agent emits inline think markup, but only for
// agents that declare thinking or thinking_level, and it never sees whether a
// verdict envelope sat on both sides of the closer.
//
// Asserted on whole-message substrings so the false claim cannot come back
// without a failing test, and pinned to the true claim so a future editor
// cannot quietly drop it either.
func TestAllUnverifiableCollapse_DoesNotClaimDoctorCannotSeeReplyShape(t *testing.T) {
	dir := t.TempDir()
	verPath := filepath.Join(dir, "verification.json")
	require.NoError(t, os.WriteFile(verPath, []byte(`{"findings":[{"verdict":"unverifiable"}]}`), 0o600))

	err := allUnverifiableCollapse(verPath)
	require.Error(t, err)
	msg := err.Error()

	assert.NotContains(t, msg, "which doctor cannot see",
		"doctor's thinking probe DOES read reply content; the blanket denial routes the operator away from the one command that would show them the bare closer")
	assert.Contains(t, msg, "declare thinking",
		"the true, narrower claim: doctor's probe is gated on a thinking declaration")
	assert.Contains(t, msg, "both sides of the closer",
		"and it never sees whether a verdict envelope sat on both sides of the closer")
}

// The same false claim sat in the comment above the message (gate.go:220-221),
// so a reader of the source learns the wrong thing even if the runtime string
// were fixed. This reads the source and pins both halves.
func TestGateSource_DoctorClaimCommentMatchesTheProbe(t *testing.T) {
	src := readRepoFile(t, "gate.go")

	assert.NotContains(t, src, "inspects the roster and cannot see a reply shape",
		"the comment repeated the false claim; it must state what doctor can and cannot see")

	// The comment must still point the operator at doctor — removing the false
	// claim must not remove the pointer that makes the message actionable.
	assert.Contains(t, src, "Run 'atcr doctor'",
		"the actionable pointer stays; only the false blanket denial goes")
}

// A drift assertion in the spirit of the project rule that doctor must mirror
// review accurately: while internal/doctor/run.go still reads reply content via
// inlineThinking, the collapse message must not claim doctor is blind to reply
// shape. This test fails the moment run.go stops calling inlineThinking AND
// someone re-adds the denial — the pair is the invariant, not either alone.
func TestGateDoctorClaim_WhileDoctorReadsInlineThinkingTheMessageStaysHonest(t *testing.T) {
	runSrc := readRepoFile(t, "../doctor/run.go")
	if !strings.Contains(runSrc, "inlineThinking(comp.Content)") {
		t.Skip("doctor no longer derives its verdict from reply content — revisit the collapse message")
	}

	dir := t.TempDir()
	verPath := filepath.Join(dir, "verification.json")
	require.NoError(t, os.WriteFile(verPath, []byte(`{"findings":[{"verdict":"unverifiable"}]}`), 0o600))

	err := allUnverifiableCollapse(verPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declare thinking",
		"run.go:329 gates the thinking probe on declaresThinking(); the message must say so while that gate stands")
}
