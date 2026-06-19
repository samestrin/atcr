package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
	"github.com/samestrin/atcr/internal/validation"
	"github.com/spf13/cobra"
)

// newReportCmd builds `atcr report`: render md, json, or checklist views over
// the reconciled findings.json.
func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report [id-or-path]",
		Short: "Render md, json, or checklist views over reconciled findings",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE:  runReport,
	}
	cmd.Flags().String("format", "md", "output format: md, json, or checklist")
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

	// Validate --output (when set) before any rendering: a path under a system
	// directory is rejected at the input layer (exit 2). Validate the resolved
	// absolute form so a legitimate relative path (e.g. ../report.md) is preserved.
	if output, _ := cmd.Flags().GetString("output"); output != "" {
		abs, err := filepath.Abs(output)
		if err != nil {
			return usageError(fmt.Errorf("resolving --output: %w", err))
		}
		if err := validation.FilePath(abs); err != nil {
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
		return usageError(err) // missing/malformed reconciled data → exit 2
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
		if err := report.RenderMarkdownWithDisagreements(&buf, findings, df); err != nil {
			return usageError(err)
		}
	default:
		if err := report.Render(&buf, findings, format); err != nil {
			return usageError(err)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "" {
		_, err := cmd.OutOrStdout().Write(buf.Bytes())
		return err
	}
	if err := os.WriteFile(output, buf.Bytes(), 0o644); err != nil {
		// A local I/O failure is an infrastructure/usage error (exit 2), the
		// same classification reconcile.go applies to its disk writes.
		return usageError(fmt.Errorf("failed to write report to %q: %w", output, err))
	}
	return nil
}

// readReconciledFindings wraps the shared reconcile loader with the CLI's
// guidance: a missing findings.json is the "run reconcile first" usage error.
func readReconciledFindings(reviewDir string) ([]reconcile.JSONFinding, error) {
	findings, err := reconcile.ReadReconciledFindings(reviewDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no reconciled data found: run 'atcr reconcile' first")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse findings: %w", err)
	}
	return findings, nil
}
