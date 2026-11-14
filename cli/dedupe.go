package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/tddedupe"
)

// newDedupeCmd is `llm-support td-dedupe` over the reconcile library (epic
// 110): it merges N reviewer streams into location clusters before a model
// adjudicates them. Every cluster of 2+ is needs_review with its members, so
// the "same issue?" call stays with the model. It reads files and writes
// nothing.
func newDedupeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dedupe",
		Short: "Cluster and merge reviewer td-streams into {merged, summary} JSON for model adjudication (replaces llm-support td-dedupe)",
		Long: `Read each td-stream (atcr-findings v1/v2, a "# Format:" declaration, or a
headerless 5/6/8/10-column file), cluster the rows by file and line within
--tolerance, and merge each cluster: REVIEWERS union, SEVERITY max with a
"<lo> vs <hi>" disagreement, CATEGORY modal, EST_MINUTES max, PROBLEM and FIX
pooled with " // ", EVIDENCE the highest-severity row's followed by each other
distinct one, joined with " / ", and CONFIDENCE HIGH for 2+ reviewers, LOW when every row
comes from an --untrusted source, else MEDIUM. Every cluster of 2+ is
needs_review and lists its members with their own values, EVIDENCE included.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: runDedupe,
	}
	cmd.Flags().String("streams", "", "comma-separated td-stream paths (required)")
	cmd.Flags().String("source-tags", "", "comma-separated source tags parallel to --streams (default: each stream's parent directory name)")
	cmd.Flags().Int("tolerance", 3, "line window that clusters findings on one file")
	cmd.Flags().String("untrusted", "", "comma-separated source tags whose findings alone are LOW confidence")
	cmd.Flags().Bool("json", true, "emit {merged, summary} as JSON (the default); --json=false prints a one-line count")
	return cmd
}

func runDedupe(cmd *cobra.Command, _ []string) error {
	paths := splitCSVFlag(mustFlag(cmd, "streams"))
	if len(paths) == 0 {
		return usageError(fmt.Errorf("--streams is required: comma-separated td-stream paths"))
	}
	// Tags are positional: an empty entry ("a,,b") keeps its place and falls
	// back to the directory name, so later tags stay on their own streams.
	tags := strings.Split(mustFlag(cmd, "source-tags"), ",")
	streams := make([]tddedupe.Stream, 0, len(paths))
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return usageError(fmt.Errorf("read stream %s: %w", p, err))
		}
		tag := ""
		if i < len(tags) {
			tag = strings.TrimSpace(tags[i])
		}
		if tag == "" {
			tag = filepath.Base(filepath.Dir(p))
		}
		streams = append(streams, tddedupe.Stream{Tag: tag, Data: data})
	}
	tolerance, _ := cmd.Flags().GetInt("tolerance")
	res, err := tddedupe.Run(streams, tddedupe.Options{
		Tolerance: tolerance,
		Untrusted: splitCSVFlag(mustFlag(cmd, "untrusted")),
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if asJSON, _ := cmd.Flags().GetBool("json"); !asJSON {
		_, err := fmt.Fprintf(out, "%d finding(s) → %d merged (%d need review)\n",
			res.Summary.InputRows, res.Summary.MergedRows, res.Summary.NeedsReviewCount)
		return err
	}
	return renderDebtDocJSON(out, res)
}

// splitCSVFlag splits a comma-separated flag value, trimming each entry and
// dropping empty ones.
func splitCSVFlag(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
