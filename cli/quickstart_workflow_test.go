package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	builtins "github.com/samestrin/atcr/personas"

	"github.com/samestrin/atcr/internal/quickstart"
)

// workflowStep is the subset of a GitHub Actions step the fresh-runner test
// replays: its run script and its env block.
type workflowStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

// scaffoldedWorkflowSteps parses the WorkflowYAML output and returns the steps
// of its single job.
func scaffoldedWorkflowSteps(t *testing.T, wf string) []workflowStep {
	t.Helper()
	var doc struct {
		Jobs map[string]struct {
			Steps []workflowStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(wf), &doc), "scaffolded workflow must be valid YAML")
	job, ok := doc.Jobs["atcr"]
	require.True(t, ok, "scaffolded workflow has an atcr job")
	return job.Steps
}

// TestQuickstartWorkflow_ReviewsOnFreshRunnerHome replays the scaffolded
// .github/workflows/atcr.yml on a fresh runner: an empty HOME with no
// ~/.config/atcr/registry.yaml, only the committed .atcr/ workspace. Every run:
// step before Review executes in bash (the checkout/setup-go/install steps are
// `uses:` or install the binary, which the test stands in for), then review runs
// with the step's key env. Without a step that provisions the user registry the
// review exits 2 "registry not found" (AC3 round 5, finding 1).
func TestQuickstartWorkflow_ReviewsOnFreshRunnerHome(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	isolate(t)
	initGitRepoWithChange(t)
	// A LOW finding: the scaffolded config gates at HIGH, so a clean exit 0
	// proves the review ran end to end rather than tripping the gate.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		content := "LOW|a.txt:1|Nit|Tidy it|style|1|evidence"
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	// The committed workspace quickstart scaffolds: init's config + personas.
	require.NoError(t, runInit(".", false, &bytes.Buffer{}, &bytes.Buffer{}))

	m, err := quickstart.LoadManifest()
	require.NoError(t, err)
	m.Provider.BaseURL = srv.URL // keep the replay off the network
	wf := quickstart.WorkflowYAML(m, quickstart.RegistryYAML(m, builtins.Names()))

	// Fresh runner: a HOME nothing has written to.
	runnerHome := t.TempDir()
	t.Setenv("HOME", runnerHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ATCR_REGISTRY_URL", "")

	var review *workflowStep
	steps := scaffoldedWorkflowSteps(t, wf)
	for i := range steps {
		s := &steps[i]
		if strings.Contains(s.Run, "atcr review") {
			review = s
			break
		}
		if s.Run == "" || strings.Contains(s.Run, "go install") {
			continue
		}
		cmd := exec.Command("bash", "-e", "-c", s.Run)
		cmd.Env = append(os.Environ(), "HOME="+runnerHome)
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "step %q failed: %s", s.Name, out)
	}
	require.NotNil(t, review, "scaffolded workflow has a review step")

	keyEnv := m.Provider.APIKeyEnv
	require.Contains(t, review.Env, keyEnv, "review step exports the registry's api_key_env")
	t.Setenv(keyEnv, "secret")

	code, stdout, stderr := execCmdSplit(t, "review", "--base", "HEAD^")
	require.Equalf(t, 0, code, "review on a fresh runner HOME must succeed\nstdout: %s\nstderr: %s", stdout, stderr)
	require.FileExists(t, filepath.Join(runnerHome, ".config", "atcr", "registry.yaml"))
}

// TestQuickstartWorkflow_EmbedsTheWrittenRegistry pins that the scaffolded
// workflow provisions the same registry quickstart wrote to the local HOME, so
// the runner resolves the same roster the workstation does.
func TestQuickstartWorkflow_EmbedsTheWrittenRegistry(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, runQuickstart(quickstartOpts{
		dir: dir, in: strings.NewReader(quickstartInput), out: &bytes.Buffer{}, errOut: &bytes.Buffer{},
	}))
	local, err := os.ReadFile(filepath.Join(home, ".config", "atcr", "registry.yaml"))
	require.NoError(t, err)
	wf, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", "atcr.yml"))
	require.NoError(t, err)

	runnerHome := t.TempDir()
	for _, s := range scaffoldedWorkflowSteps(t, string(wf)) {
		if !strings.Contains(s.Run, "registry.yaml") || strings.Contains(s.Run, "atcr review") {
			continue
		}
		cmd := exec.Command("bash", "-e", "-c", s.Run)
		cmd.Env = append(os.Environ(), "HOME="+runnerHome)
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "step %q failed: %s", s.Name, out)
	}
	written, err := os.ReadFile(filepath.Join(runnerHome, ".config", "atcr", "registry.yaml"))
	require.NoError(t, err, "a workflow step writes the runner's user registry")
	require.Equal(t, string(local), string(written), "runner registry matches the one quickstart wrote")
}
