package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// thinkingProvider echoes the marker and reports reasoningTokens reasoning tokens
// on every reply, so a thinking: off declaration reads as not honored. With
// reasoningTokens < 0 it reports no reasoning-token field at all, so a silent
// provider leaves the verdict unverified.
func thinkingProvider(t *testing.T, reasoningTokens int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		content := "ok"
		if len(req.Messages) > 0 {
			if i := strings.Index(req.Messages[0].Content, "ATCR-OK-"); i >= 0 {
				content = req.Messages[0].Content[i:]
			}
		}
		resp := map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
		}
		if reasoningTokens >= 0 {
			resp["usage"] = map[string]any{"completion_tokens": 3, "completion_tokens_details": map[string]int{"reasoning_tokens": reasoningTokens}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupThinkingDoctorEnv is setupDoctorEnv plus thinking: off on the one agent.
func setupThinkingDoctorEnv(t *testing.T, baseURL string) {
	t.Helper()
	setupDoctorEnv(t, baseURL)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(regPath, append(data, []byte("    thinking: \"off\"\n    thinking_style: qwen\n")...), 0o644))
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")
}

// AC 05-04 Scenario 2 and Error Scenario 2: a not-honored declaration gets a named
// stderr warning suggesting a remedy, and the count and exit code stand.
func TestDoctor_WarnsWhenThinkingIsNotHonored(t *testing.T) {
	setupThinkingDoctorEnv(t, thinkingProvider(t, 40).URL)

	stdout, stderr, err := executeSplit(t, "doctor")
	require.NoError(t, err, "a thinking warning never fails the exit code")
	assert.Contains(t, stderr, "1 ok / 0 failed")
	assert.Contains(t, stderr, "doctor: WARNING — thinking not honored:")
	assert.Contains(t, stderr, "a larger max_tokens or a different model")
	assert.Contains(t, stderr, "bruce (test-model)")
	assert.NotContains(t, stderr, "thinking unverified")
	assert.NotContains(t, stdout, "doctor: WARNING — thinking")
	assert.Contains(t, stdout, "thinking not honored: ")
}

// AC 05-04 Error Scenario 1: an unverified verdict gets its own distinct line.
func TestDoctor_WarnsWhenThinkingIsUnverified(t *testing.T) {
	setupThinkingDoctorEnv(t, thinkingProvider(t, -1).URL)

	_, stderr, err := executeSplit(t, "doctor")
	require.NoError(t, err)
	assert.Contains(t, stderr, "1 ok / 0 failed")
	assert.Contains(t, stderr, "doctor: WARNING — thinking unverified: these agents declare thinking but the probe reached no verdict, so nothing is known about the declaration either way; re-run doctor to retry: bruce (test-model)")
	assert.NotContains(t, stderr, "thinking not honored")
}

// An honored declaration and an undeclared roster print no thinking warning.
func TestDoctor_NoThinkingWarningWhenHonoredOrUndeclared(t *testing.T) {
	setupThinkingDoctorEnv(t, thinkingProvider(t, 0).URL)
	_, stderr, err := executeSplit(t, "doctor")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "thinking")

	setupDoctorEnv(t, thinkingProvider(t, 40).URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")
	_, stderr, err = executeSplit(t, "doctor")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "thinking")
}
