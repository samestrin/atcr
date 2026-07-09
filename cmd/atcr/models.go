package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

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
	cmd.AddCommand(newModelsRefreshCmd())
	return cmd
}

// defaultSnapshotOutput is where `atcr models refresh` writes by default: the
// checked-in fixture snapshot.go embeds at build time. A refreshed file reaches
// the default `models check` path either by recompiling the binary (the embed
// re-reads it) or, at runtime, via the ATCR_CATALOG_SNAPSHOT override (TD-009).
const defaultSnapshotOutput = "internal/personas/testdata/catalog_snapshot.json"

func newModelsRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Regenerate the checked-in catalog snapshot from a live OpenRouter fetch (maintainer-only)",
		Long: "Fetch OpenRouter's /api/v1/models once and rewrite the checked-in catalog\n" +
			"snapshot the resolver tests and the embedded `models check` path consume.\n\n" +
			"This is a MAINTAINER command, never run in CI: on the live default path it\n" +
			"requires OPENROUTER_API_KEY and fails closed (exit 2) without it, so CI — which\n" +
			"has no key — can never fetch live. A refreshed file reaches the default\n" +
			"`models check` path by recompiling the binary (the snapshot is embedded) or at\n" +
			"runtime via the ATCR_CATALOG_SNAPSHOT override.\n\n" +
			"By default it writes " + defaultSnapshotOutput + "; use --output to write elsewhere.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runModelsRefresh,
	}
	cmd.Flags().String("output", "", "path to write the snapshot (default: "+defaultSnapshotOutput+")")
	return cmd
}

func runModelsRefresh(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	if strings.TrimSpace(output) == "" {
		output = defaultSnapshotOutput
	}

	// Maintainer-auth gate: on the live default path (no ATCR_CATALOG_URL override)
	// require OPENROUTER_API_KEY. CI has no key, so refresh fails closed there and
	// can never fetch live — the key presence is the maintainer gate even though the
	// catalog GET itself is unauthenticated (AC 08-02 Error Scenario 1).
	if commpersonas.CatalogURLOverride() == "" && strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) == "" {
		return usageError(fmt.Errorf("OPENROUTER_API_KEY is required to refresh the catalog snapshot"))
	}

	// Fetch once. Any transport/status error leaves the existing fixture untouched
	// (we return before the write) and maps to exit 2.
	models, err := commpersonas.NewLiveCatalogClient(personasClient).FetchModels()
	if err != nil {
		return usageError(err)
	}
	if len(models) == 0 {
		return usageError(fmt.Errorf("refusing to overwrite fixture with empty catalog"))
	}

	data, err := commpersonas.MarshalSnapshot(models)
	if err != nil {
		return usageError(err)
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		return usageError(fmt.Errorf("writing catalog snapshot to %s: %w", output, err))
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d models to %s\n", len(models), output)
	return nil
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
			"2 = usage or command failure.\n\n" +
			"The comparison uses a catalog snapshot compiled into the binary (no network).\n" +
			"Set ATCR_CATALOG_SNAPSHOT to a file path to compare against a different\n" +
			"snapshot (e.g. one produced by a future `atcr models refresh`).",
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
		// Failing to resolve the personas directory is a command/environment
		// failure (exit 2), not a "conditions found" (1) result — consistent with
		// the SnapshotModels failure below.
		return usageError(err)
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
		// A stdout write error is ignored symmetrically with the text path
		// (renderDriftText also ignores its Fprintln errors), so the exit code is
		// derived purely from the findings in BOTH modes — the identical-exit-code
		// contract (AC 05-03 EC2). renderDriftJSON cannot fail on marshaling
		// (DriftFinding is always encodable); the only possible error is an
		// unrecoverable stdout write, which is not a usage/command failure.
		_ = renderDriftJSON(cmd.OutOrStdout(), findings)
	} else {
		renderDriftText(cmd.OutOrStdout(), findings, checked, filter)
	}

	if len(findings) > 0 {
		return &driftFoundError{n: len(findings)}
	}
	return nil
}

// renderDriftText writes one line per finding, or an explicit non-empty
// confirmation when there are none — distinguishing "nothing to check" (no
// community personas), "no such persona to check" (a name filter matched
// nothing), and "checked, all clean".
func renderDriftText(w io.Writer, findings []commpersonas.DriftFinding, checked int, filter string) {
	if len(findings) == 0 {
		switch {
		case checked == 0 && filter != "":
			_, _ = fmt.Fprintf(w, "No community persona named %q to check.\n", sanitizeDisplay(filter))
		case checked == 0:
			_, _ = fmt.Fprintln(w, "No community personas installed; nothing to check.")
		default:
			_, _ = fmt.Fprintln(w, "No drift, deprecation, or missing-slug conditions found.")
		}
		return
	}
	for _, f := range findings {
		_, _ = fmt.Fprintln(w, driftLine(f))
	}
}

// driftLine renders one finding in the documented human-readable line format.
// Every interpolated field is control-char-sanitized so a crafted persona model
// field or catalog id cannot inject terminal escapes into operator stdout.
func driftLine(f commpersonas.DriftFinding) string {
	persona := sanitizeDisplay(f.Persona)
	cur := sanitizeDisplay(f.CurrentSlug)
	switch f.Condition {
	case commpersonas.ConditionNewerMember:
		return fmt.Sprintf("%s: %s → %s (newer member)", persona, cur, sanitizeDisplay(f.SuggestedSlug))
	case commpersonas.ConditionDeprecation:
		return fmt.Sprintf("%s: %s has expiration %s (deprecation)", persona, cur, sanitizeDisplay(f.ExpirationDate))
	case commpersonas.ConditionMissing:
		return fmt.Sprintf("%s: %s no longer in catalog (missing)", persona, cur)
	default:
		return fmt.Sprintf("%s: %s (%s)", persona, cur, sanitizeDisplay(f.Condition))
	}
}

// sanitizeDisplay strips control characters (including U+2028/2029 line and
// paragraph separators) from a value bound for a human-readable line, mirroring
// the TD-008 control-char discipline. The --json path needs no equivalent — the
// standard-library encoder escapes control characters itself.
func sanitizeDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, s)
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
