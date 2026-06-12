package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/doctor"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/spf13/cobra"
)

// newDoctorCmd builds `atcr doctor`: a pre-flight self-test that invokes every
// configured model endpoint once and reports which agents can actually be
// reached, so misconfiguration is caught before a real review run.
func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Self-test every configured model endpoint",
		Long: "Resolve the effective roster (agents + serial_agents, including fallback\n" +
			"chains), deduplicate to distinct (provider, model, base_url) targets, and\n" +
			"invoke each one once with a trivial nonce prompt. Reports a per-agent table\n" +
			"(or --json) and exits 0 when every agent has a working invocation path, 1\n" +
			"when any agent has none, and 2 for usage/configuration errors.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDoctor,
	}
	cmd.Flags().Int("max-tokens", 2048, "completion budget per self-test call (high enough that thinking models emit the marker)")
	cmd.Flags().Int("timeout", 60, "per-call timeout in seconds")
	cmd.Flags().Bool("json", false, "emit machine-readable JSON to stdout instead of the table")
	cmd.Flags().String("agents", "", "comma-separated subset of listed agents to test (default: all)")
	return cmd
}

// runDoctor loads config, resolves targets, probes them, and renders the
// report. Config/usage problems map to exit 2; an unreachable agent maps to
// exit 1 with the report still printed.
func runDoctor(cmd *cobra.Command, _ []string) error {
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")
	timeoutSecs, _ := cmd.Flags().GetInt("timeout")
	asJSON, _ := cmd.Flags().GetBool("json")
	agentsFilter, _ := cmd.Flags().GetString("agents")

	if maxTokens <= 0 {
		return usageError(fmt.Errorf("--max-tokens must be positive"))
	}
	if timeoutSecs <= 0 {
		return usageError(fmt.Errorf("--timeout must be positive (seconds)"))
	}

	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return usageError(err)
	}
	reg, err := registry.LoadRegistry(regPath)
	if err != nil {
		return usageError(err)
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath("."))
	if err != nil {
		return usageError(err)
	}
	if err := proj.ValidateAgainst(reg); err != nil {
		return usageError(err)
	}

	if agentsFilter != "" {
		proj, err = filterRoster(proj, agentsFilter)
		if err != nil {
			return usageError(err)
		}
	}

	res, err := doctor.Resolve(reg, proj)
	if err != nil {
		return usageError(err)
	}

	nonce, err := doctor.RandomNonce()
	if err != nil {
		return usageError(fmt.Errorf("generating self-test nonce: %w", err))
	}

	rep := doctor.Run(cmd.Context(), llmclient.New(), res, doctor.Options{
		MaxTokens: maxTokens,
		Timeout:   time.Duration(timeoutSecs) * time.Second,
		Nonce:     nonce,
	})

	if asJSON {
		if err := doctor.RenderJSON(cmd.OutOrStdout(), rep); err != nil {
			return err
		}
	} else {
		doctor.RenderTable(cmd.OutOrStdout(), rep)
	}

	if rep.ExitCode != 0 {
		// Plain error → exit 1. The report is already on stdout; the summary
		// line goes to stderr via main's centralized handler.
		return fmt.Errorf("one or more agents have no working endpoint")
	}
	return nil
}

// filterRoster restricts the roster to the named subset, preserving each
// agent's original lane. Every requested name must be a directly-listed agent.
func filterRoster(proj *registry.ProjectConfig, csv string) (*registry.ProjectConfig, error) {
	want := map[string]bool{}
	for _, name := range strings.Split(csv, ",") {
		if n := strings.TrimSpace(name); n != "" {
			want[n] = true
		}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("--agents was set but lists no agent names")
	}

	listed := map[string]bool{}
	for _, n := range proj.Agents {
		listed[n] = true
	}
	for _, n := range proj.SerialAgents {
		listed[n] = true
	}
	for n := range want {
		if !listed[n] {
			return nil, fmt.Errorf("--agents: %q is not a listed agent in .atcr/config.yaml", n)
		}
	}

	out := &registry.ProjectConfig{}
	for _, n := range proj.Agents {
		if want[n] {
			out.Agents = append(out.Agents, n)
		}
	}
	for _, n := range proj.SerialAgents {
		if want[n] {
			out.SerialAgents = append(out.SerialAgents, n)
		}
	}
	return out, nil
}
