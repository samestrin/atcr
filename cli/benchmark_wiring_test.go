package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The vocabulary diagnostics had exactly one production wiring —
// warnVocabularyDiagnostics(cmd.ErrOrStderr(), rr) inside runBenchmarkRun — and no
// test reached it: deleting that line left `go test ./cli/...` fully green while the
// operator signal vanished from the command. Every existing test called
// warnDriftingReviewers/warnVocabularyDiagnostics directly, which proves the helpers
// format correctly and proves nothing about whether the command calls them.
//
// This drives the actual cobra command through its RunE, with the config and
// completer seams swapped for a roster that drifts 100% of its findings, and asserts
// the row lands on the command's stderr. Reverting runBenchmarkRun to drop the call
// fails here; so does routing it to stdout.
func TestBenchmarkRunCmd_VocabularyDiagnosticsReachStderr(t *testing.T) {
	cfg := benchCfg([3]string{"greta", "m-greta", "greta"})

	restoreCfg := benchmarkLoadConfig
	restoreCompleter := benchmarkNewCompleter
	t.Cleanup(func() {
		benchmarkLoadConfig = restoreCfg
		benchmarkNewCompleter = restoreCompleter
	})
	benchmarkLoadConfig = func(string) (*fanout.ReviewConfig, error) { return cfg, nil }
	// "bug" is not a member of the closed vocabulary, so every finding this roster
	// raises drifts: the reviewer's own rate is 1.00, well past MaxReviewerDriftRate.
	benchmarkNewCompleter = func(context.Context) fanout.Completer { return stubCategoryCompleter{category: "bug"} }

	code, stdout, stderr := execCmdSplit(t, "benchmark", "run", "--suite-path", suiteValidPath)
	require.Equal(t, 0, code, "a drifting roster is a diagnostic, never an exit-code change")

	assert.Contains(t, stderr, "outside the offered vocabulary",
		"the per-reviewer drift row must reach the command's stderr through the real wiring")
	assert.Contains(t, stderr, "m-greta/greta",
		"the row must name WHICH reviewer drifted — the whole point of the per-reviewer breakdown")
	assert.NotContains(t, stdout, "outside the offered vocabulary",
		"the diagnostic must not contaminate the run-result JSON on stdout")

	// stdout stays a clean run-result: the diagnostic is additive, not a replacement.
	assert.True(t, strings.Contains(stdout, `"suite"`),
		"stdout must still carry the run-result JSON the command exists to produce")
}

// Every test in this package swaps benchmarkLoadConfig, so the production closure's
// body — the one line that decides WHERE `atcr benchmark run` looks for a project
// config — was never executed and could be repointed (to a hardcoded path, to the
// user's home, to an empty root) with the suite fully green.
//
// This calls the default value directly, before any test replaces it, from a temp cwd
// carrying a project config with a DISTINCTIVE value. That value is what makes the
// test discriminating: in an empty directory every candidate root resolves to the same
// embedded defaults, so repointing the seam away from the cwd would go unnoticed. Here
// only a cwd-rooted load can see it.
func TestBenchmarkLoadConfig_DefaultSeamResolvesFromTheWorkingDirectory(t *testing.T) {
	// The production closure itself: captured before any sibling test's swap.
	seam := benchmarkLoadConfig

	// isolate points HOME/XDG_CONFIG_HOME and the cwd at temp dirs, so the registry
	// below is the only one in scope and the developer's real config cannot leak in.
	isolate(t)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(
		"providers:\n  testprov:\n    api_key_env: ATCR_TEST_KEY\n    base_url: http://unused\n"+
			"agents:\n  bruce:\n    provider: testprov\n    model: test-model\n"), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"),
		[]byte("agents: [bruce]\ntimeout_secs: 991\n"), 0o644))

	cfg, err := seam(".")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, 991, cfg.Settings.TimeoutSecs,
		"the production seam must resolve the project config rooted at the CWD — a seam pointed "+
			"anywhere else cannot see this value")

	// And it must pass EMPTY CLI overrides: benchmark run has no review flags to
	// forward, so a seam that smuggled some in would silently diverge from the config
	// the operator's file describes.
	direct, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
	require.NoError(t, err)
	assert.Equal(t, direct.Settings, cfg.Settings)
}
