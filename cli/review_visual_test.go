package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// visualCfg is a config whose roster holds greta and, when withFelix, felix;
// eye is a vision agent on the provider whose base_url is visionURL.
func visualCfg(withFelix bool, visionURL, visionAgent string) *fanout.ReviewConfig {
	roster := []string{"greta"}
	if withFelix {
		roster = append(roster, fanout.VisualPersonaName)
	}
	return &fanout.ReviewConfig{
		Registry: &registry.Registry{
			Providers: map[string]registry.Provider{
				"p":      {BaseURL: "http://127.0.0.1:1"},
				"vision": {BaseURL: visionURL},
			},
			Agents: map[string]registry.AgentConfig{
				"greta":                  {Provider: "p", Model: "m-greta", Persona: "greta"},
				fanout.VisualPersonaName: {Provider: "p", Model: "m-felix", Persona: fanout.VisualPersonaName},
				"eye":                    {Provider: "vision", Model: "m-eye", SupportsVision: true},
			},
		},
		Project:  &registry.ProjectConfig{Agents: roster},
		Settings: registry.Settings{VisualReview: registry.VisualReview{Agent: visionAgent}},
	}
}

func runVisualFromFlags(t *testing.T, cfg *fanout.ReviewConfig, baseline bool, flags map[string]string) (fanout.VisualReviewer, string, error) {
	t.Helper()
	cmd := newReviewCmd()
	for k, v := range flags {
		require.NoError(t, cmd.Flags().Set(k, v))
	}
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))
	v, err := visualStageFromFlags(ctx, cmd, cfg, baseline)
	return v, buf.String(), err
}

func TestVisualStageFromFlags_NoPreviewURLBuildsNothing(t *testing.T) {
	v, _, err := runVisualFromFlags(t, visualCfg(true, "http://localhost:11434", "eye"), false, nil)
	require.NoError(t, err)
	assert.Nil(t, v, "without --preview-url no stage is built, so the review skips felix with its warning")
}

// T5 success criterion: --preview-url with felix in the roster and no usable
// vision agent is an error, raised before any provider or sandbox work.
func TestVisualStageFromFlags_PreviewURLWithoutAVisionAgentFails(t *testing.T) {
	flags := map[string]string{previewURLFlag: "http://localhost:3000"}

	_, _, err := runVisualFromFlags(t, visualCfg(true, "http://localhost:11434", ""), false, flags)
	require.Error(t, err)
	assert.ErrorIs(t, err, fanout.ErrVisionAgentUnset)
	assert.Equal(t, 2, exitCodeOf(err), "a missing vision agent is a usage error")

	cloud := visualCfg(true, "https://api.example.com/v1", "eye")
	_, _, err = runVisualFromFlags(t, cloud, false, flags)
	require.ErrorIs(t, err, fanout.ErrVisionAgentRefused, "a cloud vision agent is refused without the opt-in")
	assert.Contains(t, err.Error(), "--allow-cloud-vision")
}

// --allow-cloud-vision admits a non-loopback vision agent: selection passes,
// and the build goes on to the sandbox, which this config lacks.
func TestVisualStageFromFlags_AllowCloudVisionAdmitsACloudAgent(t *testing.T) {
	cloud := visualCfg(true, "https://api.example.com/v1", "eye")
	_, _, err := runVisualFromFlags(t, cloud, false, map[string]string{previewURLFlag: "http://localhost:3000", allowCloudVisionFlag: "true"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, fanout.ErrVisionAgentRefused)
	assert.Contains(t, err.Error(), "sandbox")
}

func TestVisualStageFromFlags_PreviewURLWithoutFelixWarns(t *testing.T) {
	v, logs, err := runVisualFromFlags(t, visualCfg(false, "http://localhost:11434", "eye"), false, map[string]string{previewURLFlag: "http://localhost:3000"})
	require.NoError(t, err)
	assert.Nil(t, v)
	assert.Contains(t, logs, "level=WARN")
	assert.Contains(t, logs, "felix")
}

func TestVisualStageFromFlags_RefusesABaselineScan(t *testing.T) {
	_, _, err := runVisualFromFlags(t, visualCfg(true, "http://localhost:11434", "eye"), true, map[string]string{previewURLFlag: "http://localhost:3000"})
	require.Error(t, err)
	assert.Equal(t, 2, exitCodeOf(err))
}

func TestVisualStageFromFlags_AllowCloudVisionAloneIsAUsageError(t *testing.T) {
	_, _, err := runVisualFromFlags(t, visualCfg(true, "http://localhost:11434", "eye"), false, map[string]string{allowCloudVisionFlag: "true"})
	require.Error(t, err)
	assert.Equal(t, 2, exitCodeOf(err))
	assert.Contains(t, err.Error(), "--preview-url")
}

// exitCodeOf is the exit code err maps to.
func exitCodeOf(err error) int {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.ExitCode()
	}
	return 1
}

// felix's call-1 adapter resolves its own agent, provider and persona, and
// sends the prompt to felix's model.
func TestNewFelixAuthor_SendsThePromptToFelixsModel(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	srv, bodies := threatMapProvider(t, "unused")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "felix.md"), []byte("# {{.AgentName}} — frontend QA reviewer\n"), 0o644))
	cfg := visualCfg(true, "http://localhost:11434", "eye")
	cfg.Registry.Providers["p"] = registry.Provider{BaseURL: srv.URL, APIKeyEnv: "ATCR_TEST_KEY"}
	cfg.PersonaDirs = registry.PersonaDirs{Project: dir}

	a, err := newFelixAuthor(context.Background(), cfg)
	require.NoError(t, err)
	assert.Contains(t, a.Persona(), "frontend QA reviewer")
	reply, err := a.Complete(context.Background(), "write the spec")
	require.NoError(t, err)
	assert.Equal(t, "NO FINDINGS", reply)
	require.Len(t, bodies()["m-felix"], 1)
	assert.Contains(t, bodies()["m-felix"][0], "write the spec")
}

func TestNewFelixAuthor_MissingPiecesAreErrors(t *testing.T) {
	noAgent := visualCfg(true, "http://localhost:11434", "eye")
	delete(noAgent.Registry.Agents, fanout.VisualPersonaName)
	_, err := newFelixAuthor(context.Background(), noAgent)
	assert.ErrorContains(t, err, "not found in registry")

	noProvider := visualCfg(true, "http://localhost:11434", "eye")
	delete(noProvider.Registry.Providers, "p")
	_, err = newFelixAuthor(context.Background(), noProvider)
	assert.ErrorContains(t, err, `provider "p"`)

	noPersona := visualCfg(true, "http://localhost:11434", "eye")
	noPersona.PersonaDirs = registry.PersonaDirs{Project: t.TempDir(), Registry: t.TempDir()}
	noPersona.Registry.Agents[fanout.VisualPersonaName] = registry.AgentConfig{Provider: "p", Model: "m-felix", Persona: "nobody-by-this-name"}
	_, err = newFelixAuthor(context.Background(), noPersona)
	assert.Error(t, err)
}

// With a sandbox block, a felix agent the registry lacks still fails as a
// usage error before the sandbox preflight.
func TestVisualStageFromFlags_FelixAgentErrorIsAUsageError(t *testing.T) {
	cfg := visualCfg(true, "http://localhost:11434", "eye")
	cfg.Project.Sandbox = &registry.SandboxConfig{}
	delete(cfg.Registry.Agents, fanout.VisualPersonaName)
	cfg.Project.Agents = []string{"greta", fanout.VisualPersonaName}
	_, _, err := runVisualFromFlags(t, cfg, false, map[string]string{previewURLFlag: "http://localhost:3000"})
	require.Error(t, err)
	assert.Equal(t, 2, exitCodeOf(err))
	assert.Contains(t, err.Error(), "felix")
}
