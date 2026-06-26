package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/benchmark"
	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/spf13/cobra"
)

// newBenchmarkCmd builds `atcr benchmark`: the standard-suite tooling for the
// public Model-Eval Leaderboard (Epic 10.0 / 10.2). `verify` validates a suite
// manifest and prints its reproducibility hash; `run` executes a suite through the
// review pipeline and writes a scored run-result; `export` wraps a run-result in
// the suite-tagged public submission envelope. The curated standard-v1 suite
// content lives in the external atcr/benchmark-suite repo.
func newBenchmarkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Standard benchmark-suite tooling for the public leaderboard",
		Long: "Tooling for the standard benchmark suite that feeds the public Model-Eval\n" +
			"Leaderboard. `verify` validates a suite manifest and prints its\n" +
			"reproducibility hash; `run` executes the suite through the review pipeline\n" +
			"and writes a scored run-result; `export` produces a suite-tagged public\n" +
			"submission record (distinct from `leaderboard --export`, so suite runs are\n" +
			"distinguishable from production runs on the public board).",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newBenchmarkVerifyCmd(), newBenchmarkRunCmd(), newBenchmarkExportCmd())
	return cmd
}

// newBenchmarkVerifyCmd builds `atcr benchmark verify --suite-path <dir>`: load
// and validate the suite manifest, confirm every case diff exists, and print the
// deterministic reproducibility hash. Read-only.
func newBenchmarkVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Validate a benchmark suite manifest and print its reproducibility hash",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runBenchmarkVerify,
	}
	cmd.Flags().String("suite-path", "", "path to the suite directory (containing suite.json)")
	_ = cmd.MarkFlagRequired("suite-path")
	return cmd
}

func runBenchmarkVerify(cmd *cobra.Command, _ []string) error {
	// Cobra GetString error is unreachable: flag registered above, MarkFlagRequired
	// enforces presence before RunE executes. Project-wide convention (27 sites).
	suitePath, _ := cmd.Flags().GetString("suite-path")

	m, err := benchmark.Load(suitePath)
	if err != nil {
		return err
	}
	hash, err := benchmark.ReproHashManifest(m, suitePath)
	if err != nil {
		return err
	}
	noun := "cases"
	if len(m.Cases) == 1 {
		noun = "case"
	}
	_, werr := fmt.Fprintf(cmd.OutOrStdout(),
		"suite %q version %s: %d %s, valid\nreproducibility hash: %s\n",
		m.Suite, m.SuiteVersion, len(m.Cases), noun, hash)
	return werr
}

// newBenchmarkRunCmd builds `atcr benchmark run --suite-path <dir> [--out <file>]`:
// load + validate the suite, execute each case's diff through the review pipeline
// (the diff-file ingestion path), score the findings against each case's expected
// categories, and write the suite-tagged run-result that `benchmark export`
// consumes. The run-result's GeneratedAt is stamped from the wall clock here; the
// scoring is deterministic given the same suite + transcript.
func newBenchmarkRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a benchmark suite through the review pipeline and write a scored run-result",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runBenchmarkRun,
	}
	cmd.Flags().String("suite-path", "", "path to the suite directory (containing suite.json)")
	cmd.Flags().String("out", "", "write the run-result JSON to this file instead of stdout (atomically replaces the target; a symlink at the path is replaced, not followed)")
	cmd.Flags().String("checkpoint", "", "opt-in: path to a run checkpoint file. Each scored case is durably recorded here before the next begins; re-running the same suite resumes from the first unscored case instead of restarting (and re-paying for) the whole run. Empty = no checkpointing (default).")
	_ = cmd.MarkFlagRequired("suite-path")
	return cmd
}

func runBenchmarkRun(cmd *cobra.Command, _ []string) error {
	// Cobra GetString errors are unreachable: all flags are registered above
	// ("suite-path" is MarkFlagRequired). Project-wide convention.
	suitePath, _ := cmd.Flags().GetString("suite-path")
	out, _ := cmd.Flags().GetString("out")
	checkpoint, _ := cmd.Flags().GetString("checkpoint")

	// Discover config the same way `atcr review` does (registry + project config
	// rooted at the cwd), so the benchmark roster is the project's reviewers.
	cfg, err := fanout.LoadReviewConfig(".", registry.CLIOverrides{})
	if err != nil {
		return err
	}

	rr, err := executeBenchmarkRun(cmd.Context(), cfg, llmclient.New(), suitePath, time.Now().UTC(), checkpoint)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(rr, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding run-result: %w", err)
	}
	if out == "" {
		_, werr := cmd.OutOrStdout().Write(append(data, '\n'))
		return werr
	}
	// writeExportFile (leaderboard.go) atomically writes to path, creating parents.
	return writeExportFile(out, data)
}

// newBenchmarkExportCmd builds `atcr benchmark export --in <run-result.json>`:
// read a suite run-result and emit the suite-tagged public submission envelope.
// The run-result is produced by `atcr benchmark run`; export reads it rather than
// the local scorecard, so a production run can never be passed off as a suite
// submission.
func newBenchmarkExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Emit a suite-tagged public submission record from a benchmark run-result",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runBenchmarkExport,
	}
	cmd.Flags().String("in", "", "path to a benchmark run-result JSON file (produced by `atcr benchmark run`)")
	cmd.Flags().String("output", "", "write the submission JSON to this file instead of stdout (atomically replaces the target; a symlink at the path is replaced, not followed)")
	_ = cmd.MarkFlagRequired("in")
	return cmd
}

func runBenchmarkExport(cmd *cobra.Command, _ []string) error {
	// Cobra GetString errors are unreachable: both flags are registered above
	// ("in" is MarkFlagRequired), so GetString returns the flag value or its
	// default, never an error. Project-wide convention (27 sites).
	in, _ := cmd.Flags().GetString("in")
	output, _ := cmd.Flags().GetString("output")

	data, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("reading run-result %s: %w", in, err)
	}
	var rr benchmark.RunResult
	if err := json.Unmarshal(data, &rr); err != nil {
		return fmt.Errorf("parsing run-result %s: %w", in, err)
	}
	if strings.TrimSpace(rr.Suite) == "" || strings.TrimSpace(rr.SuiteVersion) == "" {
		return fmt.Errorf("run-result %s is missing suite/suite_version", in)
	}
	if len(rr.Reviewers) == 0 {
		return fmt.Errorf("run-result %s has no reviewers", in)
	}

	generatedAt, err := time.Parse(time.RFC3339, rr.GeneratedAt)
	if err != nil {
		return fmt.Errorf("parsing generated_at %q: %w", rr.GeneratedAt, err)
	}
	sub := benchmark.BuildSubmission(rr, generatedAt)
	out, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding submission: %w", err)
	}
	if output == "" {
		_, werr := cmd.OutOrStdout().Write(append(out, '\n'))
		return werr
	}
	// writeExportFile (leaderboard.go) atomically writes to path, creating parents.
	return writeExportFile(output, out)
}
