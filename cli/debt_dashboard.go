package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/validation"
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
	cmd.Flags().String("output", "", "write to a file instead of stdout")
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

	// Validate --output before any rendering or I/O, the same way runReport does:
	// resolve it to an absolute, symlink-resolved path so a target under a system
	// directory — or a symlink pointing into one — is rejected at the input layer
	// (exit 2). The resolved path is also the path written below, so the value
	// validated is the value used and no link-follow can slip in between.
	outputPath := out
	if out != "" {
		var err error
		if outputPath, err = resolveDashboardOutput(out); err != nil {
			return usageError(err)
		}
		if err := validation.FilePath(outputPath); err != nil {
			return usageError(err)
		}
	}

	recs, err := loadLocalDebt(cmd)
	if err != nil {
		return err
	}
	top, _ := cmd.Flags().GetInt("top")
	content := renderDebtDashboard(recs, top)

	switch {
	case check:
		return checkDashboard(cmd, outputPath, content)
	case out == "":
		_, err := fmt.Fprint(cmd.OutOrStdout(), content)
		return err
	default:
		// The one deliberate divergence from `atcr report`'s --output contract:
		// the dashboard is a generated artifact routinely pointed at a path in a
		// not-yet-created directory (docs/, .github/), so it creates the parent
		// rather than failing. This is intentional, not an oversight.
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return usageError(fmt.Errorf("failed to create the directory for %q: %w", outputPath, err))
		}
		// A local I/O failure is an infrastructure/usage error (exit 2), matching
		// report.go's classification of its own disk writes.
		if err := os.WriteFile(outputPath, []byte(content), 0o644); err != nil {
			return usageError(fmt.Errorf("failed to write dashboard to %q: %w", outputPath, err))
		}
		// Nothing on stdout: report writes no "wrote it" line either, and a status
		// line is noise to a caller that redirected the payload to a file.
		return nil
	}
}

// resolveDashboardOutput resolves --output for the dashboard's create-the-parent
// contract, which report's --output does not have.
//
// resolveOutputPath (report.go) resolves symlinks only in components that
// already exist and falls open to the plain absolute path otherwise. That is
// safe for report, which never creates a directory: an unresolvable path simply
// fails to open. It is NOT safe here, because os.MkdirAll below WILL follow a
// symlinked ancestor — so a target two levels below a link (`link/newdir/d.md`)
// would pass validation as its literal path and then be written inside the
// link's target, bypassing the guard that rejects `link/d.md`.
//
// Resolving the deepest EXISTING ancestor and rejoining the not-yet-created
// components puts the real destination in front of validation before anything is
// created, so the check and the write agree on where the file lands.
func resolveDashboardOutput(output string) (string, error) {
	abs, err := absFn(output)
	if err != nil {
		return "", fmt.Errorf("resolving --output: %w", err)
	}
	// Resolve the WHOLE path first, exactly as resolveOutputPath does, after
	// following a dangling leaf link the same way it does. Starting at the parent
	// would leave a LEAF symlink unresolved — validation would see the link path
	// while os.WriteFile follows it to its target, which is the same bypass in its
	// shortest form.
	abs = followDanglingLinkLeaf(abs)
	if resolved, err := evalSymlinksFn(abs); err == nil {
		return resolved, nil
	}
	missing := []string{filepath.Base(abs)}
	for dir := filepath.Dir(abs); ; {
		if resolved, err := evalSymlinksFn(dir); err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Nothing on the path exists (an unrooted or vanished tree): fall open
			// to the absolute path, which is what MkdirAll will create verbatim.
			return abs, nil
		}
		missing = append([]string{filepath.Base(dir)}, missing...)
		dir = parent
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
