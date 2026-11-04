package fanout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/payload"
)

// GuardOutbound is the exported form of the similarity-query guard, for text
// that leaves for an embedding endpoint outside a review (epic 96 T2).
func TestGuardOutbound_EachPolicy(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // the built-in ruleset decides deterministically
	texts := []OutboundText{
		{Label: "row abc problem", Text: "token " + guardAWSKey + " leaks"},
		{Label: "row abc fix", Text: "rotate it"},
	}
	for _, tc := range []struct {
		policy string
		want   []string
		block  bool
	}{
		{policy: "", want: []string{texts[0].Text, texts[1].Text}},
		{policy: secretGuardOff, want: []string{texts[0].Text, texts[1].Text}},
		{policy: secretGuardWarn, want: []string{texts[0].Text, texts[1].Text}},
		{policy: secretGuardRedact, want: []string{"token " + payload.SecretPlaceholder + " leaks", "rotate it"}},
		{policy: secretGuardBlock, block: true},
	} {
		t.Run("policy="+tc.policy, func(t *testing.T) {
			var (
				got []string
				err error
			)
			stderr := captureStderr(t, func() {
				got, err = GuardOutbound(context.Background(), tc.policy, "the embedding endpoint", texts)
			})
			assert.NotContains(t, stderr, guardAWSKey, "a warning must never print the secret")
			if tc.block {
				assert.True(t, errors.Is(err, ErrOutboundBlocked))
				assert.Nil(t, got)
				assert.Contains(t, stderr, "secret_guard: block")
				assert.Contains(t, stderr, "row abc problem", "the warning names the text by label")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			if tc.policy == secretGuardWarn || tc.policy == "" {
				assert.Contains(t, stderr, "sent unchanged to the embedding endpoint")
			}
		})
	}
}

func TestGuardOutbound_BlockFailsClosedOnAnUntrustedScan(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gitleaks"), []byte("#!/bin/sh\nexit 2\n"), 0o755))
	t.Setenv("PATH", dir)
	var err error
	stderr := captureStderr(t, func() {
		_, err = GuardOutbound(context.Background(), secretGuardBlock, "the embedding endpoint", []OutboundText{{Label: "x", Text: "clean"}})
	})
	assert.True(t, errors.Is(err, ErrOutboundBlocked))
	assert.True(t, strings.Contains(stderr, "could not be checked"))
}
