package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
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
