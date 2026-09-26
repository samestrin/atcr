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
// (response_format) request with 503 — what a rate-limited or erroring upstream
// does to a probe that can reach no verdict.
func unverifiedProvider(t *testing.T) *httptest.Server {
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
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"simulated upstream 503"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A declared agent whose probe ends ResponseFormatUnverified gets its own stderr
// line naming the agent and model, stating no verdict was reached and to re-run —
// distinct from the not-honored warning, which tells the operator to drop the
// declaration. Without it a CI log scanner reading the one-line summary sees a
// clean run even though response_format was never verified.
func TestDoctor_WarnsWhenResponseFormatIsUnverified(t *testing.T) {
	srv := unverifiedProvider(t)
	setupDoctorEnv(t, srv.URL)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(regPath, append(data, []byte("    response_format: json_object\n")...), 0o644))
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

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
