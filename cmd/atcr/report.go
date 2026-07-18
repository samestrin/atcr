package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/samestrin/atcr/internal/debate"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
	"github.com/samestrin/atcr/internal/validation"
	"github.com/spf13/cobra"
)

// newReportCmd builds `atcr report`: render the supported views (report.Formats())
// over the reconciled findings.json.
func newReportCmd() *cobra.Command {
	// Derive the Short + --format help from report.Formats() — the single source of
	// truth the enum/validation already use — so adding a format (e.g. axi in Sprint
	// 31.0) can never leave the help text advertising a stale subset that the
	// invalid-format error and --format validation contradict (TD-001; mirrors the
	// MCP descReport/tools.go dynamic pattern).
	cmd := &cobra.Command{
		Use:   "report [id-or-path]",
		Short: "Render " + report.Formats() + " views over reconciled findings",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE:  runReport,
	}
	cmd.Flags().String("format", "md", "output format: "+report.Formats())
	cmd.Flags().String("output", "", "write to a file instead of stdout")
	cmd.Flags().Bool("disagreements", false, "render the disagreement radar: a ranked view of the highest-tension spots (severity splits, solo findings, gray-zone clusters) instead of the standard report")
	return cmd
}

func runReport(cmd *cobra.Command, args []string) error {
	// Validate --format against the enum before any I/O (TD-003): a bad value is
	// a usage error (exit 2), consistent with the rest of the CLI.
	format, _ := cmd.Flags().GetString("format")
	if !report.ValidFormat(format) {
		return usageError(fmt.Errorf("unknown format %q: supported formats are %s", format, report.Formats()))
	}

	// Validate --output (when set) before any rendering: resolve it to an
	// absolute, symlink-resolved path so a path under a system directory — or a
	// symlink that points into one — is rejected at the input layer (exit 2). The
	// resolved path is also the path written below, so the value validated is the
	// value used (no link-follow bypass between check and write).
	output, _ := cmd.Flags().GetString("output")
	var outputPath string
	if output != "" {
		var err error
		outputPath, err = resolveOutputPath(output)
		if err != nil {
			return usageError(err)
		}
		if err := validation.FilePath(outputPath); err != nil {
			return usageError(err)
		}
	}

	arg := ""
	if len(args) == 1 {
		arg = args[0]
	}
	reviewDir, err := anchorDir(arg)
	if err != nil {
		return usageError(err)
	}

	findings, err := readReconciledFindings(reviewDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return usageError(err) // absent data → exit 2 (usage: run reconcile first)
		}
		return &codedError{code: exitFailure, err: err} // present-but-malformed → exit 1
	}

	var buf bytes.Buffer
	disagreements, _ := cmd.Flags().GetBool("disagreements")
	// The disagreement radar is a focused, ranked view; it replaces the standard
	// report rather than layering onto a chosen --format. --format json is
	// honored (machine-contract DisagreementsFile); unsupported combinations
	// (e.g. --disagreements --format checklist) are usage errors.
	switch {
	case disagreements:
		clusters, err := reconcile.ReadAmbiguousClusters(reviewDir)
		if err != nil {
			return usageError(fmt.Errorf("failed to read ambiguous clusters: %w", err))
		}
		df := reconcile.BuildDisagreements(findings, clusters)
		switch format {
		case report.FormatJSON:
			if err := report.RenderDisagreementsJSON(&buf, df); err != nil {
				return usageError(err)
			}
		case report.FormatMarkdown:
			if err := report.RenderDisagreements(&buf, df); err != nil {
				return usageError(err)
			}
		default:
			return usageError(fmt.Errorf("--disagreements does not support --format %s", format))
		}
	case format == report.FormatMarkdown:
		// The standard markdown report carries the radar above its findings. A
		// corrupt ambiguous.json degrades to a findings-only radar rather than
		// failing the report (the dedicated --disagreements view above surfaces
		// such errors explicitly instead).
		df := reconcile.LoadDisagreements(reviewDir, findings)
		// Contested-findings section (Epic 6.0): a present-but-malformed debate.json
		// degrades to no section rather than failing the report, matching the
		// tolerant-read contract the radar uses for ambiguous.json.
		cr := loadContested(reviewDir)
		if err := report.RenderMarkdownWithContested(&buf, findings, df, cr); err != nil {
			return usageError(err)
		}
	case format == report.FormatAXI:
		// AXI routes through the single shared pagination wrapper (AC 03-04): the
		// same internal/report step atcr review --axi's findings path would use, so
		// neither command reimplements truncation. The line cap resolves once from
		// ATCR_AXI_MAX_LINES (AC 03-03); RenderAXIPaginated caps the payload, preserves
		// the header's true N, and emits the truncated flag (AC 03-01/03-02).
		if err := report.RenderAXIPaginated(&buf, findings, axiMaxLinesFromEnv()); err != nil {
			// An AXI serialization fault is an internal, non-operator-fixable rendering
			// fault → exit 1 (generic failure), left unwrapped so it defaults to
			// exitFailure (AC 02-02 Error Scenario 3, which names `atcr report --axi`).
			return fmt.Errorf("axi output rendering failed: %w", err)
		}
	default:
		if err := report.Render(&buf, findings, format); err != nil {
			// The non-AXI formats keep their pre-existing usage-error classification
			// (out of this story's scope).
			return usageError(err)
		}
	}

	if output == "" {
		_, err := cmd.OutOrStdout().Write(buf.Bytes())
		return err
	}
	if err := os.WriteFile(outputPath, buf.Bytes(), 0o644); err != nil {
		// A local I/O failure is an infrastructure/usage error (exit 2), the
		// same classification reconcile.go applies to its disk writes.
		return usageError(fmt.Errorf("failed to write report to %q: %w", outputPath, err))
	}
	return nil
}

// resolveOutputPath returns the --output target in absolute, symlink-resolved
// form so validation and the subsequent write both act on the real on-disk
// location. Resolving symlinks first closes a bypass where --output is a symlink
// into a system directory: filepath.Abs would validate the link path while
// os.WriteFile follows the link to its target. A not-yet-created output file has
// no on-disk form to resolve, so it falls open to the absolute path (mirrors
// resolveRedactRoot's fail-open in review.go).
func resolveOutputPath(output string) (string, error) {
	abs, err := absFn(output)
	if err != nil {
		return "", fmt.Errorf("resolving --output: %w", err)
	}
	resolved, err := evalSymlinksFn(abs)
	if err != nil {
		return abs, nil
	}
	return resolved, nil
}

// loadContested reads the debate stage's reconciled/debate.json (Epic 6.0) and
// maps it onto the report's presentation-only Contested view. It is the seam that
// keeps the report package decoupled from the debate package: the command (the
// composition root) owns the artifact read and the mapping. A missing debate.json
// (the stage never ran) or a malformed one degrades to an empty report — the
// contested section is then omitted — matching the radar's tolerant-read contract.
func loadContested(reviewDir string) report.ContestedReport {
	df, found, err := debate.ReadDebateFile(reviewDir)
	if err != nil || !found {
		return report.ContestedReport{}
	}
	items := make([]report.Contested, 0, len(df.Items))
	for _, it := range df.Items {
		items = append(items, report.Contested{
			File:              it.File,
			Line:              it.Line,
			Outcome:           it.Outcome,
			OriginalSeverity:  it.OriginalSeverity,
			SettledSeverity:   it.SettledSeverity,
			Judge:             it.Judge,
			Reasoning:         it.Reasoning,
			Reason:            it.Reason,
			ChallengeSurvived: it.ChallengeSurvived,
			SingleModel:       it.SingleModel,
			ClusterDecision:   it.ClusterDecision,
		})
	}
	return report.ContestedReport{Items: items, Overflow: len(df.Overflow)}
}

// readReconciledFindings wraps the shared reconcile loader with the CLI's
// guidance: a missing findings.json is the "run reconcile first" usage error.
func readReconciledFindings(reviewDir string) ([]reconcile.JSONFinding, error) {
	findings, err := reconcile.ReadReconciledFindings(reviewDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no reconciled data found: run 'atcr reconcile' first: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse findings: %w", err)
	}
	return findings, nil
}
