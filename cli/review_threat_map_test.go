package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initThreatMapRepo seeds a repo whose HEAD commit changes auth.go, style.css
// and README.md, the Epic 37.0 T3 fixture range.
func initThreatMapRepo(t *testing.T) {
	t.Helper()
	wd := requireTempWorkdir(t)
	run := func(args ...string) { runFixtureGit(t, wd, args...) }
	run("init", "-q")
	write := func(name, body string) { require.NoError(t, os.WriteFile(name, []byte(body), 0o644)) }
	write("auth.go", "package main\n\nfunc a() {}\n")
	write("style.css", "body { color: red; }\n")
	write("README.md", "# demo\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	write("auth.go", "package main\n\nfunc a() { b() }\n\nfunc b() {}\n")
	write("style.css", "body { color: blue; }\n")
	write("README.md", "# demo\n\nMore words.\n")
	run("add", ".")
	run("commit", "-q", "-m", "change")
}

// threatMapProvider answers the mapper model m-scout with mapperReply and every
// reviewer with no findings, recording each request body per model.
func threatMapProvider(t *testing.T, mapperReply string) (*httptest.Server, func() map[string][]string) {
	t.Helper()
	var mu sync.Mutex
	bodies := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		bodies[req.Model] = append(bodies[req.Model], string(raw))
		mu.Unlock()
		content := "NO FINDINGS"
		if req.Model == "m-scout" {
			content = mapperReply
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": content}},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string][]string {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string][]string, len(bodies))
		for k, v := range bodies {
			out[k] = append([]string(nil), v...)
		}
		return out
	}
}

// threatMapConfig writes a registry with reviewers greta and kai plus the
// mapper agent scout on the embedded threat_mapper persona, and a
// project config whose roster is the two reviewers. projectExtra is appended
// to .atcr/config.yaml.
func threatMapConfig(t *testing.T, srvURL, projectExtra string) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	reg := "providers:\n  p:\n    api_key_env: " + testReviewKeyEnv + "\n    base_url: " + srvURL + "\nagents:\n" +
		"  greta:\n    provider: p\n    model: m-greta\n    persona: greta\n" +
		"  kai:\n    provider: p\n    model: m-kai\n    persona: kai\n" +
		"  scout:\n    provider: p\n    model: m-scout\n    persona: threat_mapper\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(reg), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [greta, kai]\n"+projectExtra), 0o644))
}

func threatMapReviewDir(t *testing.T) string {
	t.Helper()
	id, err := os.ReadFile(filepath.Join(".atcr", "latest"))
	require.NoError(t, err)
	return filepath.Join(".atcr", "reviews", strings.TrimSpace(string(id)))
}

func agentStatus(t *testing.T, dir, agent string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "sources", "pool", "raw", "agent", agent, "status.json"))
	require.NoError(t, err)
	var st map[string]any
	require.NoError(t, json.Unmarshal(data, &st))
	return st
}

// Epic 37.0 T3 success criterion through the real CLI: `atcr review
// --threat-map` calls the threat_map.agent once, sends only auth.go to every
// reviewer, and lists the other two in each status.json's threat_map_dropped.
func TestReviewCmd_ThreatMap_ScopesEveryReviewerToTheKeptFile(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initThreatMapRepo(t)
	srv, bodies := threatMapProvider(t, `{"keep": ["auth.go"]}`)
	threatMapConfig(t, srv.URL, "threat_map:\n  agent: scout\n")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^", "--threat-map"))

	got := bodies()
	require.Len(t, got["m-scout"], 1, "the mapper is called once")
	for _, f := range []string{"auth.go", "style.css", "README.md"} {
		assert.Contains(t, got["m-scout"][0], f, "the mapper is shown every changed file")
	}
	for _, model := range []string{"m-greta", "m-kai"} {
		require.NotEmpty(t, got[model], model)
		for _, b := range got[model] {
			assert.Contains(t, b, "auth.go", model)
			assert.NotContains(t, b, "style.css", model)
			assert.NotContains(t, b, "README.md", model)
		}
	}
	dir := threatMapReviewDir(t)
	for _, agent := range []string{"greta", "kai"} {
		st := agentStatus(t, dir, agent)
		assert.Equal(t, []any{"README.md", "style.css"}, st["threat_map_dropped"], agent)
		assert.Equal(t, []any{}, st["files_dropped"], agent)
	}
}

// A mapper whose reply does not parse fails open: every reviewer gets all three
// files and no status.json carries threat_map_dropped.
func TestReviewCmd_ThreatMap_FailingMapperReviewsEveryFile(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initThreatMapRepo(t)
	srv, bodies := threatMapProvider(t, "I think auth.go matters most.")
	threatMapConfig(t, srv.URL, "threat_map:\n  agent: scout\n")

	code, out := execCmdCapture(t, "review", "--base", "HEAD^", "--threat-map")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "threat map failed", "the fail-open fallback is warned about")

	got := bodies()
	require.Len(t, got["m-scout"], 1)
	for _, model := range []string{"m-greta", "m-kai"} {
		require.NotEmpty(t, got[model], model)
		for _, b := range got[model] {
			for _, f := range []string{"auth.go", "style.css", "README.md"} {
				assert.Contains(t, b, f, "%s must see %s when the mapper failed", model, f)
			}
		}
	}
	dir := threatMapReviewDir(t)
	for _, agent := range []string{"greta", "kai"} {
		_, present := agentStatus(t, dir, agent)["threat_map_dropped"]
		assert.False(t, present, agent)
	}
}

// Without the flag the setting is inert: the mapper is never called.
func TestReviewCmd_ThreatMap_SettingAloneDoesNotMap(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initThreatMapRepo(t)
	srv, bodies := threatMapProvider(t, `{"keep": ["auth.go"]}`)
	threatMapConfig(t, srv.URL, "threat_map:\n  agent: scout\n")

	require.Equal(t, 0, execCmd(t, "review", "--base", "HEAD^"))
	got := bodies()
	assert.Empty(t, got["m-scout"], "the phase is opt-in")
	for _, b := range got["m-greta"] {
		assert.Contains(t, b, "style.css")
	}
}

// The flag's refusals are usage errors raised before any provider call.
func TestReviewCmd_ThreatMap_UsageErrors(t *testing.T) {
	cases := []struct {
		name    string
		extra   string
		args    []string
		message string
	}{
		{"no agent configured", "", []string{"review", "--base", "HEAD^", "--threat-map"}, "threat_map.agent"},
		{"agent not in the registry", "threat_map:\n  agent: nobody\n", []string{"review", "--base", "HEAD^", "--threat-map"}, `"nobody"`},
		{"baseline scan", "threat_map:\n  agent: scout\n", []string{"review", "--all", "--threat-map"}, "git-range"},
		{"resume", "threat_map:\n  agent: scout\n", []string{"review", "--resume", "latest", "--threat-map"}, "--threat-map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolate(t)
			t.Setenv(testReviewKeyEnv, "secret")
			initThreatMapRepo(t)
			srv, bodies := threatMapProvider(t, `{"keep": ["auth.go"]}`)
			threatMapConfig(t, srv.URL, tc.extra)

			code, out := execCmdCapture(t, tc.args...)
			assert.Equal(t, 2, code, out)
			assert.Contains(t, out, tc.message)
			assert.Empty(t, bodies(), "no provider is called")
		})
	}
}
