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
