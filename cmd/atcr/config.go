package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/registry"
)

// newConfigCmd builds `atcr config`: a thin mutation namespace over the
// project's .atcr/config.yaml. It follows the newDebtCmd subcommand-group
// pattern (a parent whose RunE prints help, with the real work in children).
// Today its only child is `set`, and the only settable key is the telemetry
// opt-out (Sprint 28.0); the surface is deliberately scoped to that one key.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and update project configuration (.atcr/config.yaml)",
		Long: "atcr config edits the project-level configuration in .atcr/config.yaml.\n" +
			"Today it exposes a single mutation — the telemetry opt-out — via\n" +
			"`atcr config set telemetry <true|false>`; run that from the repo root.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newConfigSetCmd())
	return cmd
}

// newConfigSetCmd builds `atcr config set <key> <value>`. The key is restricted
// to an allowlist of exactly `telemetry` (any other key is a usage error), and
// the value must parse as a boolean. It persists to .atcr/config.yaml via the
// registry's surgical yaml-node edit, leaving every other key untouched.
func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a project config key (currently only: telemetry)",
		Long: "atcr config set persists a single project-config key to .atcr/config.yaml.\n\n" +
			"Supported key (the only one today):\n" +
			"  telemetry <true|false>   Enable or disable the anonymous usage ping.\n" +
			"                           Accepts the strconv.ParseBool vocabulary:\n" +
			"                           true/false, 1/0, t/f, True/False, TRUE/FALSE.\n\n" +
			"Persisting `telemetry false` is one of two opt-out surfaces; the other is\n" +
			"the ATCR_TELEMETRY environment variable. They are OR'd — telemetry is\n" +
			"disabled whenever EITHER says so, and neither can re-enable what the other\n" +
			"disabled.\n\n" +
			"Note the inverse boolean direction versus ATCR_DISABLE_AST_GROUPING:\n" +
			"ATCR_TELEMETRY names the ENABLED state directly, so `ATCR_TELEMETRY=0`\n" +
			"(not `=1`) disables telemetry. Setting `telemetry true` here re-enables it.",
		Args: usageArgs(cobra.ExactArgs(2)),
		RunE: runConfigSet,
	}
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, raw := args[0], args[1]
	if key != "telemetry" {
		return usageError(fmt.Errorf("unsupported config key %q: only \"telemetry\" is supported", key))
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return usageError(fmt.Errorf("invalid value %q for telemetry: must be a boolean (true/false/1/0)", raw))
	}
	// cwd-relative, matching how every other command locates project config.
	if err := registry.SetTelemetrySetting(".", enabled); err != nil {
		// An I/O failure (missing/unwritable file) is an environment error (exit
		// 1), NOT a usage mistake — config set never silently creates the file.
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set telemetry = %t in .atcr/config.yaml\n", enabled)
	return nil
}
