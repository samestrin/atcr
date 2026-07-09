package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	commpersonas "github.com/samestrin/atcr/internal/personas"
	"github.com/spf13/cobra"
)

// driftFoundError signals that `models check` found one or more drift,
// deprecation, or missing-slug conditions. It maps to exit code 1 — distinct from
// a clean run (0) and a usage/command failure (2) — so scripts and Epic 19.8's
// mechanical agent can act on the result by exit code alone. The drift report
// itself is already written to stdout; this error only carries the exit code.
type driftFoundError struct{ n int }

func (e *driftFoundError) Error() string {
	return fmt.Sprintf("%d model drift condition(s) found", e.n)
}

// ExitCode maps a "conditions found" result to exit 1 (exitFailure), read by
// main()'s exitCode() dispatch.
func (e *driftFoundError) ExitCode() int { return exitFailure }

// newModelsCmd builds `atcr models`: the top-level command family for inspecting
// model bindings, drift, and the catalog snapshot. `check` is its first
// subcommand; a `refresh` subcommand follows in Phase 8.
func newModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Inspect model bindings, drift, and the catalog snapshot",
		Long: "Inspect the model bindings and resolved-slug locks of installed personas.\n\n" +
			"`atcr models check` reports drift (a newer family member is available),\n" +
			"deprecation (the locked slug is expiring), and missing-slug conditions\n" +
			"against a checked-in catalog snapshot — read-only, deterministic, and with\n" +
			"no network I/O in its default path.",
		Args: usageArgs(cobra.NoArgs),
		// Bare `atcr models` prints help (exit 0); RunE keeps exit-code mapping
		// centralized in main(), matching the personas parent command.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newModelsCheckCmd())
	return cmd
}

func newModelsCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check [name]",
		Short: "Report model drift, deprecation, and missing-slug conditions for installed personas",
		Long: "Compare each installed community persona's resolved-slug lock against the\n" +
			"catalog snapshot and report three conditions: a newer family member is\n" +
			"available (drift), the locked slug carries an expiration date (deprecation),\n" +
			"or the locked slug is absent from the catalog (missing).\n\n" +
			"With no argument, every installed community persona is checked; pass a name\n" +
			"to check a single persona. Exit codes: 0 = clean, 1 = conditions found,\n" +
			"2 = usage or command failure.",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runModelsCheck,
	}
	cmd.Flags().Bool("json", false, "emit machine-readable JSON (one object per condition)")
	return cmd
}

func runModelsCheck(cmd *cobra.Command, args []string) error {
	jsonOut, _ := cmd.Flags().GetBool("json")

	dir, err := personasDir()
	if err != nil {
		return err
	}

	// Load the catalog snapshot up front. A missing/corrupt snapshot is a command
	// failure (exit 2) — no drift report can be computed — never a "conditions
	// found" (1) or clean (0) result.
	models, err := commpersonas.SnapshotModels()
	if err != nil {
		return usageError(err)
	}

	// Enumerate personas the same way `atcr personas list` does (project >
	// community > built-in, dedup by name), so the two commands consider an
	// identical persona set. Only community personas carry a resolved lock to
	// check; built-in/project rows have nothing to compare.
	projectDir := filepath.Join(".atcr", "personas")
	metas, listErr := commpersonas.ListTiers(projectDir, dir)
	if listErr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", listErr)
	}

	var filter string
	if len(args) == 1 {
		filter = strings.TrimSpace(args[0])
	}

	locks := make([]commpersonas.InstalledLock, 0, len(metas))
	checked := 0
	for _, m := range metas {
		if m.Source != "community" {
			continue
		}
		if filter != "" && !strings.EqualFold(m.Name, filter) {
			continue
		}
		checked++
		lock, err := commpersonas.LoadLock(dir, m.Name)
		if err != nil {
			// Per-persona failure: surface on stderr, exclude from the report, and
			// keep checking the rest (AC 05-01 Error Scenario 1). It does not by
			// itself escalate the exit code.
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", err)
			continue
		}
		locks = append(locks, lock)
	}

	findings := commpersonas.CheckDrift(locks, models)

	if jsonOut {
		if err := renderDriftJSON(cmd.OutOrStdout(), findings); err != nil {
			return err
		}
	} else {
		renderDriftText(cmd.OutOrStdout(), findings, checked)
	}

	if len(findings) > 0 {
		return &driftFoundError{n: len(findings)}
	}
	return nil
}

// renderDriftText writes one line per finding, or an explicit non-empty
// confirmation when there are none — distinguishing "nothing to check" (no
// community personas) from "checked, all clean".
func renderDriftText(w io.Writer, findings []commpersonas.DriftFinding, checked int) {
	if len(findings) == 0 {
		if checked == 0 {
			_, _ = fmt.Fprintln(w, "No community personas installed; nothing to check.")
		} else {
			_, _ = fmt.Fprintln(w, "No drift, deprecation, or missing-slug conditions found.")
		}
		return
	}
	for _, f := range findings {
		_, _ = fmt.Fprintln(w, driftLine(f))
	}
}

// driftLine renders one finding in the documented human-readable line format.
func driftLine(f commpersonas.DriftFinding) string {
	switch f.Condition {
	case commpersonas.ConditionNewerMember:
		return fmt.Sprintf("%s: %s → %s (newer member)", f.Persona, f.CurrentSlug, f.SuggestedSlug)
	case commpersonas.ConditionDeprecation:
		return fmt.Sprintf("%s: %s has expiration %s (deprecation)", f.Persona, f.CurrentSlug, f.ExpirationDate)
	case commpersonas.ConditionMissing:
		return fmt.Sprintf("%s: %s no longer in catalog (missing)", f.Persona, f.CurrentSlug)
	default:
		return fmt.Sprintf("%s: %s (%s)", f.Persona, f.CurrentSlug, f.Condition)
	}
}

// renderDriftJSON emits the findings as a JSON array (one object per condition),
// always well-formed — an empty result is "[]", never null or blank — so machine
// consumers can json.Unmarshal the output unconditionally.
func renderDriftJSON(w io.Writer, findings []commpersonas.DriftFinding) error {
	if findings == nil {
		findings = []commpersonas.DriftFinding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(findings)
}
