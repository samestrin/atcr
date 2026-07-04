package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/debt"
)

// defaultDashboardOut is the flat sibling-file location of the generated
// dashboard. The all-caps name signals a machine-generated, do-not-hand-edit
// artifact, distinct from the authoritative README.
const defaultDashboardOut = ".planning/technical-debt/DASHBOARD.md"

func newDebtDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Generate an aggregated technical-debt dashboard (Markdown)",
		Long: "atcr debt dashboard renders an aggregated rollup (totals, by severity,\n" +
			"by component, by age, and a top-priority list) to a read-only Markdown\n" +
			"file, distinct from the authoritative README. Secret-shaped tokens in\n" +
			"finding text are scrubbed. Use --check in CI or a pre-commit hook to fail\n" +
			"when the committed dashboard is out of date; the render is deterministic\n" +
			"so --check flags real content drift, not clock movement.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtDashboard,
	}
	addSourceFlags(cmd)
	cmd.Flags().String("out", defaultDashboardOut, "output path for the generated dashboard")
	cmd.Flags().Int("top", 10, "number of top-priority items to list")
	cmd.Flags().Bool("check", false, "verify the on-disk dashboard matches freshly generated output; exit non-zero on drift")
	cmd.Flags().Bool("stdout", false, "print the dashboard to stdout instead of writing the file")
	return cmd
}

func runDebtDashboard(cmd *cobra.Command, _ []string) error {
	recs, err := loadRecords(cmd)
	if err != nil {
		return err
	}
	top, _ := cmd.Flags().GetInt("top")
	content := debt.RenderDashboard(recs, top)

	out := mustFlag(cmd, "out")
	check, _ := cmd.Flags().GetBool("check")
	toStdout, _ := cmd.Flags().GetBool("stdout")

	switch {
	case check && toStdout:
		return usageError(fmt.Errorf("--check and --stdout are mutually exclusive"))
	case check:
		return checkDashboard(cmd, out, content)
	case toStdout:
		_, err := fmt.Fprint(cmd.OutOrStdout(), content)
		return err
	default:
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return fmt.Errorf("create dashboard directory: %w", err)
		}
		if err := os.WriteFile(out, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write dashboard: %w", err)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Wrote dashboard to %s.\n", out)
		return err
	}
}

// checkDashboard compares the on-disk dashboard against freshly generated
// content and returns a non-nil (exit 1) error when they differ or the file is
// absent, so a CI job or pre-commit hook fails on drift.
func checkDashboard(cmd *cobra.Command, out, content string) error {
	existing, err := os.ReadFile(out)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("dashboard %s does not exist; run `atcr debt dashboard` to generate it", out)
		}
		return fmt.Errorf("read dashboard: %w", err)
	}
	if string(existing) != content {
		return fmt.Errorf("dashboard %s is out of date; regenerate with `atcr debt dashboard`", out)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Dashboard %s is up to date.\n", out)
	return err
}
