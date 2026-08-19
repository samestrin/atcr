package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingEchoProvider behaves like echoProvider but captures the max_tokens the
// client actually put on the wire, so a test can assert on the probe's resolved
// output cap rather than on a hand-constructed Options struct.
func recordingEchoProvider(t *testing.T, seen *[]*int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MaxTokens *int `json:"max_tokens"`
			Messages  []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		*seen = append(*seen, req.MaxTokens)

		content := ""
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

// setupDoctorEnvDeclaringMaxTokens is setupDoctorEnv with a per-agent max_tokens
// declaration, so the declaration and any --max-tokens flag value are distinguishable
// in the probe's outgoing request.
func setupDoctorEnvDeclaringMaxTokens(t *testing.T, baseURL string, declared int) {
	t.Helper()
	setupDoctorEnv(t, baseURL)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regPath := filepath.Join(home, ".config", "atcr", "registry.yaml")
	data, err := os.ReadFile(regPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(regPath,
		append(data, []byte("    max_tokens: "+strconv.Itoa(declared)+"\n")...), 0o644))
}

// probe() prefers the agent's declared max_tokens ONLY when the operator did not type
// --max-tokens, and that distinction is carried entirely by Options.MaxTokensSet —
// which cli/doctor.go fills from cmd.Flags().Changed("max-tokens"). Replacing that
// expression with a literal false survives the whole ./cli suite: the unit test builds
// Options{MaxTokensSet: true} by hand, so it proves the helper honours the flag, not
// that the command ever sets it. With the line broken, `atcr doctor --max-tokens N` is
// silently ignored for every agent carrying a declaration. Drive the real command in
// both directions against a provider that records what was actually sent.
func TestDoctorCmd_MaxTokensFlagOverridesTheAgentDeclaration(t *testing.T) {
	const declared, flagged = 4321, 777

	t.Run("no flag uses the declaration", func(t *testing.T) {
		var seen []*int
		srv := recordingEchoProvider(t, &seen)
		setupDoctorEnvDeclaringMaxTokens(t, srv.URL, declared)
		t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

		_, err := execute(t, "doctor")
		require.NoError(t, err)

		require.Len(t, seen, 1)
		require.NotNil(t, seen[0], "a declared cap must reach the wire")
		assert.Equal(t, declared, *seen[0],
			"an agent that declares a cap is probed at that cap, not at doctor's default")
	})

	t.Run("explicit flag wins over the declaration", func(t *testing.T) {
		var seen []*int
		srv := recordingEchoProvider(t, &seen)
		setupDoctorEnvDeclaringMaxTokens(t, srv.URL, declared)
		t.Setenv("ATCR_DOCTOR_TEST_KEY", "sk-test")

		_, err := execute(t, "doctor", "--max-tokens", "777")
		require.NoError(t, err)

		require.Len(t, seen, 1)
		require.NotNil(t, seen[0])
		assert.Equal(t, flagged, *seen[0],
			"the operator's per-run --max-tokens must win over the agent declaration")
	})
}
