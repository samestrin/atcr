package fanout

import (
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The chunked twin of TestBuildSlots_ZeroBudget*OnOverflow* in
// zero_budget_bulk_test.go. The bulk zero-budget arm routes fail/fallback through
// applyOverflowPolicy so their typed errors propagate out of buildSlots
// pre-dispatch; the chunked arm marked the same degradation and emitted the same
// warning but never consulted the policy — so on_overflow: fail hard-failed under
// the default strategy and silently degraded under review_strategy: chunked,
// shipping chunks at the minChunkLines floor the window provably cannot hold.
//
// chunked_overflow_test.go already pins the two paths to the same degradation
// action and the same warning text; the policy dispatch is the half that actually
// stops the run.

// chunkedZeroBudgetCfg gives greta a 1-token declaration (EffectiveByteBudget 0)
// on a model absent from the static table, under review_strategy: chunked.
func chunkedZeroBudgetCfg(t *testing.T, onOverflow string) *ReviewConfig {
	t.Helper()
	cfg := declaredWindowRoster(t, 1)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.OnOverflow = onOverflow
	return cfg
}

// chunkedDiffPayload builds a splittable diff payload carrying the per-file
// Entries a production payload always has — the zero-budget arms are gated on a
// non-empty entry list on both strategies.
func chunkedDiffPayload(fileCount, bodyLines int) map[string]modePayload {
	var entries []payload.FileEntry
	var text string
	for i := 0; i < fileCount; i++ {
		path := "f" + itoa(i) + ".go"
		body := fileSeg(path, bodyLines)
		entries = append(entries, payload.FileEntry{Path: path, Size: int64(len(body)), Body: body})
		text += body
	}
	return map[string]modePayload{"blocks": {Entries: entries, Kept: entries, Text: text, FileCount: fileCount}}
}

func TestBuildSlots_ChunkedZeroBudgetHonorsOnOverflowFail(t *testing.T) {
	cfg := chunkedZeroBudgetCfg(t, OverflowFail)

	_, _, err := buildSlots(cfg, chunkedDiffPayload(4, 100), ReviewRange{Base: "a", Head: "b"}, "", "", false)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrOverflowPolicyFail),
		"on_overflow=fail must hard-fail pre-dispatch on a zero budget under review_strategy=chunked, exactly as it does for bulk, got: %v", err)
}

func TestBuildSlots_ChunkedZeroBudgetHonorsOnOverflowFallback(t *testing.T) {
	cfg := chunkedZeroBudgetCfg(t, OverflowFallback)

	_, _, err := buildSlots(cfg, chunkedDiffPayload(4, 100), ReviewRange{Base: "a", Head: "b"}, "", "", false)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFallbackUnavailable),
		"on_overflow=fallback must surface its typed error pre-dispatch on a zero budget under review_strategy=chunked, got: %v", err)
}

// The `len(mp.Entries) > 0` half of the policy gate, pinned. The comment above it
// claims the chunked and bulk arms "must agree on WHEN the policy fires, not only on
// what it does once it fires" — the bulk arm gates its whole zero-budget block on a
// non-empty entry list (review.go:1959), so a chunked payload carrying no entries must
// likewise pass through untouched. Without this the guard survives replacement with a
// literal `true` against the whole package, and an entry-less chunked payload
// hard-fails the run under on_overflow fail/fallback — the exact divergence from the
// bulk twin the guard exists to prevent.
func TestBuildSlots_ChunkedZeroBudgetWithNoEntriesSkipsThePolicy(t *testing.T) {
	for _, policy := range []string{OverflowFail, OverflowFallback} {
		t.Run(policy, func(t *testing.T) {
			cfg := chunkedZeroBudgetCfg(t, policy)
			// Text but no Entries: the payload shape the bulk arm's own entries guard
			// admits without consulting the policy.
			payloads := map[string]modePayload{
				"blocks": {Text: fileSeg("f0.go", 100), FileCount: 1},
			}
			require.Empty(t, payloads["blocks"].Entries, "precondition: the gate's subject is an empty entry list")

			_, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "blocks", "", false)

			require.NoError(t, err,
				"an entry-less payload must not trip the overflow policy — the bulk twin does not")
		})
	}
}

func TestBuildSlots_ChunkedUsableWindowNotFailedByOnOverflowFail(t *testing.T) {
	// The gate fires on a ZERO budget only: a funded agent whose diff merely splits
	// into chunks is a no-loss degradation and must not be hard-failed.
	cfg := declaredWindowRoster(t, 128000)
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	cfg.Settings.OnOverflow = OverflowFail

	slots, _, err := buildSlots(cfg, chunkedDiffPayload(12, 900), ReviewRange{Base: "a", Head: "b"}, "", "", false)

	require.NoError(t, err)
	require.Greater(t, len(slots), 1, "precondition: this diff must still split at the declared budget")
}
