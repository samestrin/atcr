package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/log"
	"github.com/stretchr/testify/require"
)

// TestCorrelateAndRedact_ScrubsNonSkProviderKey is the epic 4.9 AC2 integration
// test: a non-sk-/non-Bearer-shaped provider key (Google AIzaSy…) present in a
// log line emitted through the production CLI redaction path is replaced with
// [redacted]. Before 4.9 the exact-value scrub was dead at this site (no secrets
// passed to NewRedactor), so this key — lacking the sk-/Bearer token shapes —
// would leak verbatim.
func TestCorrelateAndRedact_ScrubsNonSkProviderKey(t *testing.T) {
	const key = "AIzaSyNonSkShapedProviderKey1234567890" // no sk-/Bearer prefix
	t.Setenv("ATCR_REDACT_NONSK_KEY", key)

	var buf bytes.Buffer
	base, err := log.New("info", "json", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), base)

	// Drive the same enumeration + wiring the CLI uses: resolve the slot's key
	// values and hand them to correlateAndRedact, exactly as runReview/runResume do.
	prep := &fanout.PreparedReview{
		ID:    "2026-06-20_nonsk",
		Repo:  t.TempDir(),
		Slots: []fanout.Slot{slotWithKeys("ATCR_REDACT_NONSK_KEY")},
	}
	secrets, _ := prep.SecretValues()
	ctx = correlateAndRedact(ctx, prep.ID, prep.Repo, secrets...)

	log.FromContext(ctx).Info("provider call failed", "detail", "x-goog-api-key: "+key)

	out := buf.String()
	require.NotContains(t, out, key, "AC2: a non-sk provider key must not leak verbatim in log output")
	require.Contains(t, out, "[redacted]", "AC2: the scrubbed key must be replaced with the redaction marker")
}

// TestCorrelateAndRedact_PreservesReviewIDAndRedactsKey verifies the AC9 vs
// AC5/AC2 contract still holds with secrets wired in: the review_id stays
// greppable while the configured key is scrubbed in the same line.
func TestCorrelateAndRedact_PreservesReviewIDAndRedactsKey(t *testing.T) {
	const key = "AIzaSyAnotherNonSkKeyValue0987654321"
	t.Setenv("ATCR_REDACT_NONSK_KEY2", key)

	var buf bytes.Buffer
	base, err := log.New("info", "json", &buf)
	require.NoError(t, err)
	ctx := log.NewContext(context.Background(), base)

	prep := &fanout.PreparedReview{
		ID:    "2026-06-20_grep",
		Repo:  t.TempDir(),
		Slots: []fanout.Slot{slotWithKeys("ATCR_REDACT_NONSK_KEY2")},
	}
	secrets, _ := prep.SecretValues()
	ctx = correlateAndRedact(ctx, prep.ID, prep.Repo, secrets...)
	log.FromContext(ctx).Info("emitting key " + key)

	out := buf.String()
	require.Contains(t, out, "2026-06-20_grep", "review_id must stay greppable (AC9)")
	require.False(t, strings.Contains(out, key), "the configured key must be scrubbed")
}
