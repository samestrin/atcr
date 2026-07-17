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
// Its only child is `set`, whose settable keys are the telemetry opt-out
// (Sprint 28.0) and the quality_signal opt-in (Sprint 30.0); the surface is
// deliberately scoped to those two keys.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and update project configuration (.atcr/config.yaml)",
		Long: "atcr config edits the project-level configuration in .atcr/config.yaml.\n" +
			"It exposes two mutations — the telemetry opt-out and the quality-signal\n" +
			"opt-in — via `atcr config set <telemetry|quality_signal> <true|false>`;\n" +
			"run that from the repo root.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newConfigSetCmd())
	return cmd
}

// newConfigSetCmd builds `atcr config set <key> <value>`. The key is restricted
// to an allowlist of exactly `telemetry` and `quality_signal` (any other key is a
// usage error), and the value must parse as a boolean. It persists to
// .atcr/config.yaml via the registry's surgical yaml-node edit, leaving every
// other key untouched.
func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a project config key (telemetry or quality_signal)",
		Long: "atcr config set persists a single project-config key to .atcr/config.yaml.\n\n" +
			"Supported keys:\n" +
			"  telemetry <true|false>       Enable or disable the anonymous usage ping.\n" +
			"                               Accepts the strconv.ParseBool vocabulary:\n" +
			"                               true/false, 1/0, t/f, True/False, TRUE/FALSE.\n" +
			"  quality_signal <true|false>  Opt in or out of the community prompt quality\n" +
			"                               signal (opt-in, content-free; off by default).\n" +
			"                               Same boolean vocabulary.\n\n" +
			"telemetry is OPT-OUT: it is enabled by default and disabled whenever EITHER\n" +
			"the ATCR_TELEMETRY env var OR `telemetry false` says so; neither can re-enable\n" +
			"what the other disabled. Note the inverse boolean direction versus\n" +
			"ATCR_DISABLE_AST_GROUPING: ATCR_TELEMETRY names the ENABLED state directly, so\n" +
			"`ATCR_TELEMETRY=0` (not `=1`) disables telemetry.\n\n" +
			"quality_signal is OPT-IN: it is disabled by default and enabled whenever EITHER\n" +
			"the ATCR_QUALITY_SIGNAL env var OR `quality_signal true` opts in — the exact\n" +
			"inverse consent model. The two settings are fully independent: neither key's\n" +
			"value has any bearing on the other.\n\n" +
			"On I/O failure (missing/unwritable .atcr/config.yaml) the command returns exit 1.",
		Args: usageArgs(cobra.ExactArgs(2)),
		RunE: runConfigSet,
	}
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, raw := args[0], args[1]
	// The settable-key allowlist is an explicit two-entry switch, never a loosened
	// prefix/fuzzy match — an unrecognized key is always a usage error.
	switch key {
	case "telemetry", "quality_signal":
	default:
		return usageError(fmt.Errorf("unsupported config key %q: only \"telemetry\" and \"quality_signal\" are supported", key))
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return usageError(fmt.Errorf("invalid value %q for %s: must be a boolean (true/false/1/0)", raw, key))
	}
	// Resolve the repo root so `config set` works from any subdirectory, not
	// just the directory that happens to contain .atcr/config.yaml.
	root, err := repoRoot()
	if err != nil {
		return err
	}
	// Dispatch to the key-specific persister. Each mutates only its own key via a
	// surgical yaml-node edit, so the two settings never interfere. An I/O failure
	// (missing/unwritable file) is an environment error (exit 1), NOT a usage
	// mistake — config set never silently creates the file.
	switch key {
	case "telemetry":
		if err := registry.SetTelemetrySetting(root, enabled); err != nil {
			return err
		}
	case "quality_signal":
		if err := registry.SetQualitySignalSetting(root, enabled); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %t in .atcr/config.yaml\n", key, enabled)
	return nil
}
