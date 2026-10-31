package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Planted credentials are assembled at runtime so the repo's own pre-commit
// secret scan does not flag this file.
var (
	guardAWSKey   = "AKIA" + "QYLPMN5HHHFPZAM2"
	guardGitHubPA = "ghp_" + "Zx9Kq2Lm8Np4Rt7Vw3Yb6Cd1Fg5Hj0Aa1Bb2"
)

// guardPayload is a blocks-mode payload with a key planted in config.go and a
// clean main.go, so a block error has a file to name.
func guardPayload() string {
	return "diff --git a/main.go b/main.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		"+port := 8080\n" +
		"+email := user.email\n" +
		"diff --git a/config.go b/config.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		"+const awsKey = \"" + guardAWSKey + "\"\n"
}

// guardCfg is toolCfg under the given secret_guard policy, with gitleaks
// hidden from PATH so the built-in ruleset decides deterministically.
func guardCfg(t *testing.T, policy string) *ReviewConfig {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	cfg := toolCfg()
	cfg.Settings.SecretGuard = policy
	return cfg
}

func buildGuarded(t *testing.T, cfg *ReviewConfig, text string) (Agent, string, error) {
	t.Helper()
	payloads := map[string]modePayload{"blocks": {Text: text, FileCount: 2}}
	var (
		a   Agent
		err error
	)
	out := captureGuardLog(t, func(ctx context.Context) {
		a, _, err = buildOneAgentCtx(ctx, cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "")
	})
	return a, out, err
}

// recomputedKey is the diff-cache key of a's own prompt, so a test can pin that
// the key was computed over exactly the text sent.
func recomputedKey(a Agent) string {
	return diffCacheKey(a.Invocation.Prompt, cacheKeyInputs{
		Model: a.Invocation.Model, BaseURL: a.Invocation.BaseURL, Temperature: a.Invocation.Temperature,
		Sizing: fmt.Sprintf("%d:%d", a.EffectiveBudget, a.chunkMaxLines), MaxTokens: a.ResolvedMaxTokens,
	})
}

func TestSecretGuard_RedactSendsThePlaceholderAndKeysTheRedactedPrompt(t *testing.T) {
	a, stderr, err := buildGuarded(t, guardCfg(t, "redact"), guardPayload())
	require.NoError(t, err)

	assert.NotContains(t, a.Invocation.Prompt, guardAWSKey, "the key must not reach the provider")
	assert.Contains(t, a.Invocation.Prompt, payload.SecretPlaceholder)
	assert.Equal(t, a.Prompt, a.Invocation.Prompt, "Agent.Prompt (a fallback's source) is the redacted text too")
	assert.Equal(t, recomputedKey(a), a.CacheKey, "the cache key is computed over the redacted prompt")
	for _, ref := range a.CodeContext {
		assert.NotContains(t, ref.Body, guardAWSKey, "the per-file audit record carries the redacted payload")
	}

	assert.NotContains(t, stderr, guardAWSKey, "the value is never logged")
	assert.Contains(t, stderr, "aws-access-token")
	assert.Contains(t, stderr, "config.go")
}

func TestSecretGuard_BlockFailsTheRunNamingAgentKindAndFile(t *testing.T) {
	_, stderr, err := buildGuarded(t, guardCfg(t, "block"), guardPayload())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSecretGuardBlocked))
	msg := err.Error()
	assert.Contains(t, msg, `"greta"`)
	assert.Contains(t, msg, "aws-access-token")
	assert.Contains(t, msg, "config.go")
	assert.NotContains(t, msg, "main.go", "only the file holding the secret is named")
	assert.NotContains(t, msg, guardAWSKey, "the error holds no secret value")
	assert.NotContains(t, stderr, guardAWSKey)
}

// A gitleaks that is on PATH but fails cannot be trusted, so block fails closed
// even on a clean payload; warn falls back to the built-in rules and proceeds.
func TestSecretGuard_ScannerFailure(t *testing.T) {
	failingGitleaks := func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "gitleaks"), []byte("#!/bin/sh\nexit 2\n"), 0o755))
		t.Setenv("PATH", dir)
	}
	clean := "diff --git a/main.go b/main.go\n@@ -1 +1 @@\n+port := 8080\n"

	t.Run("block fails closed", func(t *testing.T) {
		cfg := toolCfg()
		cfg.Settings.SecretGuard = "block"
		failingGitleaks(t)
		_, stderr, err := buildGuarded(t, cfg, clean)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrSecretGuardBlocked))
		assert.True(t, errors.Is(err, payload.ErrSecretScanFailed))
		assert.Contains(t, stderr, "secret scan")
	})
	t.Run("warn proceeds on the built-in rules", func(t *testing.T) {
		cfg := toolCfg()
		cfg.Settings.SecretGuard = "warn"
		failingGitleaks(t)
		a, stderr, err := buildGuarded(t, cfg, guardPayload())
		require.NoError(t, err)
		assert.Contains(t, a.Invocation.Prompt, guardAWSKey, "warn sends the prompt unchanged")
		assert.Contains(t, stderr, "using the built-in ruleset")
		assert.Contains(t, stderr, "aws-access-token", "the built-in rules still found the key")
		assert.NotContains(t, stderr, guardAWSKey)
	})
}

func TestSecretGuard_WarnAndOffDispatchThePromptUnchanged(t *testing.T) {
	// The unguarded baseline: the same payload rendered with the guard off.
	off, offErr, err := buildGuarded(t, guardCfg(t, "off"), guardPayload())
	require.NoError(t, err)
	assert.Contains(t, off.Invocation.Prompt, guardAWSKey)
	assert.Empty(t, offErr, "off does not scan, so it has nothing to say")

	for _, policy := range []string{"warn", ""} { // "" resolves to the registry default, warn
		t.Run("policy="+policy, func(t *testing.T) {
			a, stderr, err := buildGuarded(t, guardCfg(t, policy), guardPayload())
			require.NoError(t, err)
			assert.Equal(t, off.Invocation.Prompt, a.Invocation.Prompt, "warn sends the prompt byte-identical to off")
			assert.Equal(t, off.CacheKey, a.CacheKey)
			assert.Contains(t, stderr, "1 secret(s) [aws-access-token] in config.go")
			assert.NotContains(t, stderr, guardAWSKey, "the value is never logged")
		})
	}
}

func TestSecretGuard_CleanPayloadIsUntouchedAndSilent(t *testing.T) {
	clean := "diff --git a/main.go b/main.go\n@@ -1,2 +1,2 @@\n+port := 8080\n+email := user.email\n"
	for _, policy := range []string{"warn", "redact", "block"} {
		t.Run(policy, func(t *testing.T) {
			a, stderr, err := buildGuarded(t, guardCfg(t, policy), clean)
			require.NoError(t, err)
			assert.Contains(t, a.Invocation.Prompt, clean)
			assert.NotContains(t, a.Invocation.Prompt, payload.SecretPlaceholder)
			assert.Empty(t, stderr)
		})
	}
}

// The sprint-plan scope constraint is prepended to the payload, so it is egress
// text too.
func TestSecretGuard_RedactsTheScopeConstraint(t *testing.T) {
	cfg := guardCfg(t, "redact")
	payloads := map[string]modePayload{"blocks": {Text: "diff --git a/main.go b/main.go\n+x\n", FileCount: 1}}
	var a Agent
	var err error
	stderr := captureGuardLog(t, func(ctx context.Context) {
		a, _, err = buildOneAgentCtx(ctx, cfg, "greta", payloads, ReviewRange{Base: "a", Head: "b"}, "", "SCOPE: rotate "+guardGitHubPA+"\n")
	})
	require.NoError(t, err)
	assert.NotContains(t, a.Invocation.Prompt, guardGitHubPA)
	assert.Contains(t, stderr, secretScopeLabel)
}

// The folded claim-ledger row: a credential pasted into a commit message rides
// the ledger into every prompt, so the guard must see the ledger entry, not
// only the diff-derived entries.
func TestSecretGuard_CommitMessageCredentialInTheClaimLedger(t *testing.T) {
	ledgerRepo := func(t *testing.T) (string, string, string) {
		dir := t.TempDir()
		fanoutGit(t, dir, "init", "-q", "-b", "main")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "cursor.go"), []byte("package p\n\nfunc Begin() int { return 0 }\n"), 0o644))
		fanoutGit(t, dir, "add", "-A")
		fanoutGit(t, dir, "commit", "-q", "-m", "seed the cursor file")
		base := fanoutGit(t, dir, "rev-parse", "HEAD")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "cursor.go"), []byte("package p\n\nfunc Begin() int { return 1 }\n"), 0o644))
		fanoutGit(t, dir, "add", "-A")
		fanoutGit(t, dir, "commit", "-q", "-m", "preserve the cursor across a cold drain\n\n- Begin() no longer returns a wiped offset\n- Begin() now authenticates with "+guardAWSKey+" from the deploy box\n")
		return dir, base, fanoutGit(t, dir, "rev-parse", "HEAD")
	}
	build := func(t *testing.T, policy string) ([]Slot, string, error) {
		dir, base, head := ledgerRepo(t)
		cfg := sizingRosterConfig()
		cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
		cfg.Settings.SecretGuard = policy
		payloads, _, err := buildPayloads(context.Background(), cfg, dir, base, head, false)
		require.NoError(t, err)
		require.NotEmpty(t, payloads)
		for _, mp := range payloads {
			section, ok := payload.ClaimLedgerPromptSection(mp.Text)
			require.True(t, ok, "precondition: the payload carries a claim ledger")
			require.Contains(t, section, guardAWSKey, "precondition: the credential reached the ledger")
		}
		var slots []Slot
		stderr := captureGuardLog(t, func(ctx context.Context) {
			slots, _, err = buildSlots(ctx, cfg, payloads, ReviewRange{Base: base, Head: head}, "", "", false)
		})
		return slots, stderr, err
	}
	// git must stay reachable for the repo build, so only gitleaks is hidden.
	hideGitleaks(t)

	t.Run("redact", func(t *testing.T) {
		slots, stderr, err := build(t, "redact")
		require.NoError(t, err)
		require.NotEmpty(t, slots)
		for _, s := range slots {
			section, ok := payload.ClaimLedgerPromptSection(s.Primary.Invocation.Prompt)
			require.True(t, ok, "the ledger still reaches the prompt")
			assert.NotContains(t, section, guardAWSKey)
			assert.Contains(t, section, payload.SecretPlaceholder)
			assert.Contains(t, section, "Begin() no longer returns a wiped offset", "the rest of the ledger is intact")
		}
		assert.Contains(t, stderr, secretPreambleLabel)
		assert.NotContains(t, stderr, guardAWSKey)
	})
	t.Run("block", func(t *testing.T) {
		_, stderr, err := build(t, "block")
		require.Error(t, err)
		assert.Contains(t, err.Error(), secretPreambleLabel)
		assert.NotContains(t, err.Error(), guardAWSKey)
		assert.NotContains(t, stderr, guardAWSKey)
	})
}

// hideGitleaks drops any real gitleaks from PATH while keeping git reachable.
func hideGitleaks(t *testing.T) {
	t.Helper()
	var keep []string
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(d, "gitleaks")); err == nil {
			continue
		}
		keep = append(keep, d)
	}
	t.Setenv("PATH", strings.Join(keep, string(os.PathListSeparator)))
}

// A fallback built from a guarded primary ships the primary's already-guarded
// prompt: redact carries the placeholder over, and warn does not repeat the
// primary's warning.
func TestSecretGuard_FallbackInheritsTheGuardedPrompt(t *testing.T) {
	for _, policy := range []string{"redact", "warn"} {
		t.Run(policy, func(t *testing.T) {
			cfg := guardCfg(t, policy)
			primary, _, err := buildGuarded(t, cfg, guardPayload())
			require.NoError(t, err)
			var fb Agent
			stderr := captureGuardLog(t, func(ctx context.Context) {
				fb, _, err = buildFallbackAgent(ctx, cfg, primary, "kai", false, fallbackRefit{})
			})
			require.NoError(t, err)
			assert.Empty(t, stderr, "the primary's scan already reported this payload")
			if policy == "redact" {
				assert.NotContains(t, fb.Invocation.Prompt, guardAWSKey)
				assert.Contains(t, fb.Invocation.Prompt, payload.SecretPlaceholder)
			}
		})
	}
}

// A hand-built primary never passed renderAgent, so buildFallbackAgent applies
// the guard itself, before its cache key and invocation.
func TestSecretGuard_FallbackGuardsAnUnguardedPrompt(t *testing.T) {
	primary := Agent{Name: "greta", Prompt: "review this:\n" + guardPayload(), PayloadMode: "blocks"}

	t.Run("redact", func(t *testing.T) {
		cfg := guardCfg(t, "redact")
		var fb Agent
		var err error
		stderr := captureGuardLog(t, func(ctx context.Context) {
			fb, _, err = buildFallbackAgent(ctx, cfg, primary, "kai", false, fallbackRefit{})
		})
		require.NoError(t, err)
		assert.NotContains(t, fb.Invocation.Prompt, guardAWSKey)
		assert.Contains(t, fb.Invocation.Prompt, payload.SecretPlaceholder)
		assert.Equal(t, recomputedKey(fb), fb.CacheKey, "the fallback's key is computed over its redacted prompt")
		assert.Contains(t, stderr, "config.go")
		assert.NotContains(t, stderr, guardAWSKey)
	})
	t.Run("block", func(t *testing.T) {
		cfg := guardCfg(t, "block")
		var err error
		captureGuardLog(t, func(ctx context.Context) {
			_, _, err = buildFallbackAgent(ctx, cfg, primary, "kai", false, fallbackRefit{})
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"kai"`)
		assert.NotContains(t, err.Error(), guardAWSKey)
	})
}
