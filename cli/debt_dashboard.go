package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDebtDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Generate an aggregated technical-debt dashboard (Markdown)",
		Long: "atcr debt dashboard renders an aggregated rollup (totals, by severity,\n" +
			"by component, by age, and a top-priority list) as Markdown. It writes to\n" +
			"stdout by default; pass --output <file> to write a file instead, mirroring\n" +
			"atcr report. Secret-shaped tokens in finding text are scrubbed. Use --check\n" +
			"with --output in CI or a pre-commit hook to fail when the committed\n" +
			"dashboard is out of date; the render is deterministic so --check flags real\n" +
			"content drift, not clock movement.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDebtDashboard,
	}
	addDebtStoreFlag(cmd)
	// An empty default means stdout. The previous default wrote a file into the
	// .planning/-scoped tree, which no longer exists as far as atcr is concerned.
	cmd.Flags().String("output", "", "write the dashboard to this file instead of stdout")
	cmd.Flags().Int("top", 10, "number of top-priority items to list")
	cmd.Flags().Bool("check", false, "verify the file at --output matches freshly generated output; exit non-zero on drift")
	return cmd
}

func runDebtDashboard(cmd *cobra.Command, _ []string) error {
	out := mustFlag(cmd, "output")
	check, _ := cmd.Flags().GetBool("check")
	// --check compares against a file, so it has nothing to compare when the
	// dashboard is going to stdout. Rejecting it is clearer than silently
	// checking a file the user never named.
	if check && out == "" {
		return usageError(errors.New("--check requires --output <file>"))
	}

	recs, err := loadLocalDebt(cmd)
	if err != nil {
		return err
	}
	top, _ := cmd.Flags().GetInt("top")
	content := renderDebtDashboard(recs, top)

	switch {
	case check:
		return checkDashboard(cmd, out, content)
	case out == "":
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
			return fmt.Errorf("dashboard %s does not exist; run `atcr debt dashboard --output %s` to generate it", out, out)
		}
		return fmt.Errorf("read dashboard: %w", err)
	}
	if string(existing) != content {
		return fmt.Errorf("dashboard %s is out of date; regenerate with `atcr debt dashboard --output %s`", out, out)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Dashboard %s is up to date.\n", out)
	return err
}
