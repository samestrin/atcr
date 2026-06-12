package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echoProvider returns a chat-completions server that echoes the user prompt
// back as the assistant message, so the doctor's nonce marker round-trips and
// the endpoint classifies as ok. failStatus > 0 makes it fail instead.
func echoProvider(t *testing.T, failStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failStatus > 0 {
			w.WriteHeader(failStatus)
			_, _ = io.WriteString(w, `{"error":{"message":"simulated failure"}}`)
			return
		}
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		content := ""
		if len(req.Messages) > 0 {
			content = req.Messages[0].Content
		}
		resp := map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupDoctorEnv writes a registry + project config wired to baseURL and points
// HOME and the working directory at fresh temp dirs.
func setupDoctorEnv(t *testing.T, baseURL string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	registryYAML := "" +
		"providers:\n" +
		"  mock:\n" +
		"    api_key_env: ATCR_DOCTOR_TEST_KEY\n" +
		"    base_url: " + baseURL + "/v1\n" +
		"agents:\n" +
		"  bruce:\n" +
		"    provider: mock\n" +
		"    model: test-model\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(registryYAML), 0o644))

	work := t.TempDir()
	t.Chdir(work)
	atcrDir := filepath.Join(work, ".atcr")
	require.NoError(t, os.MkdirAll(atcrDir, 0o755))
	projYAML := "" +
		"agents:\n" +
		"  - bruce\n" +
		"payload_mode: blocks\n" +
		"timeout_secs: 600\n" +
		"fail_on: HIGH\n"
	require.NoError(t, os.WriteFile(filepath.Join(atcrDir, "config.yaml"), []byte(projYAML), 0o644))
}

func TestDoctor_JSONHappyPath(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	out, err := execute(t, "doctor", "--json")
	require.NoError(t, err, "all agents healthy → exit 0")

	var parsed struct {
		Agents []struct {
			Agent  string `json:"agent"`
			Status string `json:"status"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	require.Len(t, parsed.Agents, 1)
	assert.Equal(t, "bruce", parsed.Agents[0].Agent)
	assert.Equal(t, "ok", parsed.Agents[0].Status)
}

func TestDoctor_MissingKeyExits1(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	// ATCR_DOCTOR_TEST_KEY deliberately unset.

	out, err := execute(t, "doctor")
	require.Error(t, err, "an agent with no working path → exit 1")
	assert.Equal(t, 1, exitCode(err))
	assert.Contains(t, out, "missing_key")
}

func TestDoctor_AuthFailureExits1(t *testing.T) {
	srv := echoProvider(t, http.StatusUnauthorized)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-bad")

	out, err := execute(t, "doctor", "--max-tokens", "256", "--timeout", "5")
	require.Error(t, err)
	assert.Equal(t, 1, exitCode(err))
	assert.Contains(t, out, "auth_failed")
}

func TestDoctor_NoConfigIsUsageError(t *testing.T) {
	// HOME has a registry but the working dir has no .atcr/config.yaml.
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	require.NoError(t, os.Remove(filepath.Join(".atcr", "config.yaml")))

	_, err := execute(t, "doctor")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "missing config is a usage/config error")
}

func TestDoctor_UnknownAgentFilterIsUsageError(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	_, err := execute(t, "doctor", "--agents", "ghost")
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err))
}

func TestDoctor_AgentsFilterSubset(t *testing.T) {
	srv := echoProvider(t, 0)
	setupDoctorEnv(t, srv.URL)
	t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

	out, err := execute(t, "doctor", "--agents", "bruce", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, "bruce")
}
