package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/reconcile"
)

// newIgnoreCmd builds `atcr ignore <finding-id> --reason <why>` (Epic 40.0): the
// local feedback loop for a false positive. It looks the id up in a review's
// reconciled/findings.json and appends a rule derived from that finding to
// .atcr/ignore.yaml, which the reconcile phase reads to drop matching findings.
//
// The rule never stores the id or the line: both drift with every edit above
// the finding (the id hashes file, line and problem). It keys on the finding's
// file and category plus a match regex-quoted from its Problem, whose "(symbol) "
// anchor keeps the rule on the same symbol after the line moves.
func newIgnoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ignore <finding-id>",
		Short: "Suppress a reconciled finding in future reconciles via .atcr/ignore.yaml",
		Long: "atcr ignore reads a review's reconciled/findings.json (the latest review by\n" +
			"default), finds the finding with the given id and appends a rule to\n" +
			".atcr/ignore.yaml scoped to its file and category, with a match quoted from\n" +
			"its problem text. The next atcr reconcile drops findings that match the rule.\n" +
			"--reason is required: report.md lists it beside the suppressed count.",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: runIgnoreCmd,
	}
	cmd.Flags().String("reason", "", "why this finding is a false positive (required)")
	cmd.Flags().String("review", "", "review id or directory to read findings from (default: .atcr/latest)")
	return cmd
}

// ignoreFilePath is the rule file the reconcile phase reads, relative to the
// repository root (the working directory, as for every other .atcr/ path).
func ignoreFilePath() string { return filepath.Join(".atcr", reconcile.IgnoreFileName) }

func runIgnoreCmd(cmd *cobra.Command, args []string) error {
	id := strings.TrimSpace(args[0])
	if id == "" {
		return usageError(fmt.Errorf("atcr ignore requires a non-empty finding id"))
	}
	reason, _ := cmd.Flags().GetString("reason")
	if strings.TrimSpace(reason) == "" {
		return usageError(fmt.Errorf("--reason is required: say why this finding is a false positive"))
	}
	reviewArg, _ := cmd.Flags().GetString("review")
	dir, err := anchorDir(reviewArg)
	if err != nil {
		return usageError(err)
	}

	findings, err := reconcile.ReadReconciledFindings(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return usageError(fmt.Errorf("no reconciled findings in %s: run 'atcr reconcile' first", dir))
		}
		return fmt.Errorf("%s: %w", dir, err)
	}
	var found *reconcile.JSONFinding
	for i := range findings {
		if findings[i].ID == id {
			found = &findings[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("no finding with id %q in %s", id, filepath.Join(dir, "reconciled", reconcile.FindingsJSON))
	}

	rule := ruleForFinding(*found, strings.TrimSpace(reason))
	path := ignoreFilePath()
	existing, err := reconcile.ReadIgnoreFile(path)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if r.File == rule.File && r.Package == "" && r.Category == rule.Category && r.Match == rule.Match {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "finding %s is already ignored by a rule in %s\n", id, path)
			return nil
		}
	}
	if err := reconcile.AppendIgnoreRule(path, rule); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ignored finding %s: added a rule for %s to %s\n", id, rule.File, path)
	return nil
}

// ruleForFinding derives the ignore rule for f: scoped to its file (glob-
// escaped, so a path like `app/[id]/page.tsx` matches itself rather than
// reading as a character class) and category, matching its Problem text
// literally (regexp.QuoteMeta, so a parenthesised symbol anchor or a `*` in
// the prose is not a live regex).
func ruleForFinding(f reconcile.JSONFinding, reason string) reconcile.IgnoreRule {
	return reconcile.IgnoreRule{
		File:     reconcile.EscapeIgnoreGlob(f.File),
		Category: f.Category,
		Match:    regexp.QuoteMeta(f.Problem),
		Reason:   reason,
	}
}
