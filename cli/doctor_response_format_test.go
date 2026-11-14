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

// jsonModeIgnoringProvider echoes the marker for the marker probe and answers the
// response_format probe with a fenced object — what a provider that silently drops
// response_format lets a model do.
func jsonModeIgnoringProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		content := "```json\n{\"findings\":[]}\n```"
		if len(req.Messages) > 0 {
			if i := strings.Index(req.Messages[0].Content, "ATCR-OK-"); i >= 0 {
				content = req.Messages[0].Content[i:]
			}
		}
		resp := map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// unverifiedProvider answers the marker probe normally but rejects every other
// (response_format) request with failStatus — what a rate-limited (429) or
// erroring (5xx) upstream does to a probe that can reach no verdict.
func unverifiedProvider(t *testing.T, failStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "ATCR-OK-") {
			content := "ATCR-OK-ok"
			resp := map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(failStatus)
		_, _ = io.WriteString(w, `{"error":{"message":"simulated upstream failure"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupDeclaredDoctorEnv is setupDoctorEnv plus a response_format declaration on
// the single agent — the shared prelude of the declared-roster CLI cases.
func setupDeclaredDoctorEnv(t *testing.T, baseURL string) {
	t.Helper()
	setupDoctorEnv(t, baseURL)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(regPath, append(data, []byte("    response_format: json_object\n")...), 0o644))
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")
}

// An unverified agent (429 on the probe) must stay OUT of the "drop the
// declaration" warning: a 429 says nothing about the declaration, and telling the
// operator to drop a working declaration over a rate limit is exactly wrong. This
// also pins the human warnings as stderr-only (the merged-stream execute helper
// cannot see that) and out of --json.
func TestDoctor_UnverifiedAgentIsNotToldToDropItsDeclaration(t *testing.T) {
	srv := unverifiedProvider(t, http.StatusTooManyRequests)
	setupDeclaredDoctorEnv(t, srv.URL)

	stdout, stderr, err := executeSplit(t, "doctor")
	require.NoError(t, err)
	assert.Contains(t, stderr, "doctor: WARNING — response_format unverified:")
	assert.NotContains(t, stderr, "response_format not honored")
	assert.NotContains(t, stdout, "doctor: WARNING — response_format not honored")
	assert.NotContains(t, stdout, "doctor: WARNING — response_format unverified")

	// --json consumers read structured fields, never the human warning lines.
	jsonOut, err := execute(t, "doctor", "--json")
	require.NoError(t, err)
	assert.NotContains(t, jsonOut, "doctor: WARNING")
}

// A declared agent whose probe ends ResponseFormatUnverified gets its own stderr
// line naming the agent and model, stating no verdict was reached and to re-run —
// distinct from the not-honored warning, which tells the operator to drop the
// declaration. Without it a CI log scanner reading the one-line summary sees a
// clean run even though response_format was never verified.
func TestDoctor_WarnsWhenResponseFormatIsUnverified(t *testing.T) {
	srv := unverifiedProvider(t, http.StatusServiceUnavailable)
	setupDeclaredDoctorEnv(t, srv.URL)

	stdout, stderr, err := executeSplit(t, "doctor")
	require.NoError(t, err, "an unverified probe never fails the exit code")
	assert.Contains(t, stderr, "1 ok / 0 failed")
	assert.Contains(t, stderr, "doctor: WARNING — response_format unverified:")
	assert.Contains(t, stderr, "bruce (test-model)")
	assert.Contains(t, stderr, "reached no verdict")
	assert.Contains(t, stderr, "re-run doctor to retry")
	// The summary line is stderr-only; the table's HINT column on stdout may name
	// the same status, but the WARNING line itself must not leak there.
	assert.NotContains(t, stdout, "doctor: WARNING — response_format unverified")
}

// A declared agent whose provider ignores response_format gets a named warning line
// with its agent and model, while the exit code and the ok/failed count, which speak
// for the endpoint, stay unchanged.
func TestDoctor_WarnsWhenResponseFormatIsNotHonored(t *testing.T) {
	srv := jsonModeIgnoringProvider(t)
	setupDoctorEnv(t, srv.URL)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(regPath, append(data, []byte("    response_format: json_object\n")...), 0o644))
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	out, err := execute(t, "doctor")
	require.NoError(t, err, "a response_format warning never fails the exit code")
	assert.Contains(t, out, "1 ok / 0 failed")
	assert.Contains(t, out, "doctor: WARNING — response_format not honored:")
	assert.Contains(t, out, "bruce (test-model)")
}

// An undeclared roster prints no response_format warning.
func TestDoctor_NoResponseFormatWarningForAnUndeclaredRoster(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	out, err := execute(t, "doctor")
	require.NoError(t, err)
	assert.NotContains(t, out, "response_format")
}

// shortReplyProvider echoes the marker, then answers the review-shaped probe the
// way gemma4:31b did in the epic 113 benchmark: a valid {"findings":[]} in 4
// completion tokens, reported in the usage block.
func shortReplyProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		content, tokens := `{"findings":[]}`, 4
		if i := strings.Index(string(body), "ATCR-OK-"); i >= 0 {
			content, tokens = "ATCR-OK-ok", 6
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
			"usage":   map[string]int{"prompt_tokens": 210, "completion_tokens": tokens},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A review-shaped reply under the token floor fails doctor, and the error names
// the agent and model, so a benchmark pre-flight stops the candidate before it is
// scored (epic 113 AC2).
func TestDoctor_ShortReviewReplyFailsAndNamesTheModel(t *testing.T) {
	srv := shortReplyProvider(t)
	setupDeclaredDoctorEnv(t, srv.URL)

	stdout, stderr, err := executeSplit(t, "doctor")
	require.Error(t, err, "a reply under the floor must fail the exit code")
	assert.Contains(t, err.Error(), "bruce (test-model)")
	assert.Contains(t, err.Error(), "token floor")
	assert.Contains(t, stderr, "0 ok / 1 failed")
	assert.Contains(t, stdout, "short_reply")
	assert.Contains(t, stdout, "model test-model answered the review-shaped probe in 4 tokens")

	jsonOut, err := execute(t, "doctor", "--json")
	require.Error(t, err)
	assert.Contains(t, jsonOut, `"status": "short_reply"`)
}
