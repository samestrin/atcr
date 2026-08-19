package cli

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
			"chains), deduplicate to distinct (provider, model, base_url, max_tokens)\n" +
			"targets — a differing declared max_tokens is a different invocation, so it\n" +
			"probes separately — and\n" +
			"invoke each one once with a trivial nonce prompt. Reports a per-agent table\n" +
			"(or --json) and exits 0 when every agent has a working invocation path, 1\n" +
			"when any agent has none, and 2 for usage/configuration errors.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runDoctor,
	}
	// 8192, matching the review fan-out's own defaultMaxTokens. A probe is only
	// evidence about the invocation it reproduces, and for an agent that declares no
	// max_tokens the invocation `atcr review` makes is capped at 8192 - so a probe at
	// the old 2048 reproduced a call review never makes, at a quarter the output
	// budget, and then reported the tier as "default" as though the two agreed. That
	// produced ok_warning on agents review runs fine, and the documented remedy (raise
	// the declaration) could make things worse: the cap is reserved out of the context
	// window, so raising it on a proxy-alias model can collapse the input budget to 0.
	cmd.Flags().Int("max-tokens", 8192, "completion budget per self-test call; matches the review fan-out's default so an undeclared agent is probed at the cap review will use")
	cmd.Flags().Int("timeout", 60, "per-call timeout in seconds")
	cmd.Flags().Bool("json", false, "emit machine-readable JSON to stdout instead of the table")
	cmd.Flags().StringSlice("agents", nil, "subset of listed agents to test (comma-separated or repeated; default: all)")
	return cmd
}

// runDoctor loads config, resolves targets, probes them, and renders the
// report. Config/usage problems map to exit 2; an unreachable agent maps to
// exit 1 with the report still printed.
func runDoctor(cmd *cobra.Command, _ []string) error {
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")
	timeoutSecs, _ := cmd.Flags().GetInt("timeout")
	asJSON, _ := cmd.Flags().GetBool("json")
	agentsFilter, _ := cmd.Flags().GetStringSlice("agents")

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
	// Merge the optional project registry overlay so doctor self-tests project
	// definitions too; the merged loader enforces the project-provider trust gate.
	reg, err := registry.LoadMergedRegistry(regPath, ".")
	if err != nil {
		return usageError(err)
	}
	if banner := reg.ProjectProviderBanner(); banner != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), banner)
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath("."))
	if err != nil {
		return usageError(err)
	}
	if err := proj.ValidateAgainst(reg); err != nil {
		return usageError(err)
	}

	if len(agentsFilter) > 0 {
		proj, err = filterRoster(proj, agentsFilter)
		if err != nil {
			return usageError(err)
		}
	}

	// The override reaches target IDENTITY, not just probe time: when it is set every
	// declaration is overridden, so agents sharing an endpoint make the same call and
	// must share one probe rather than paying for N identical ones.
	override := 0
	if cmd.Flags().Changed("max-tokens") {
		override = maxTokens
	}
	res, err := doctor.ResolveWithCap(reg, proj, override)
	if err != nil {
		return usageError(err)
	}

	nonce, err := doctor.RandomNonce()
	if err != nil {
		return usageError(fmt.Errorf("generating self-test nonce: %w", err))
	}

	rep := doctor.Run(cmd.Context(), llmclient.New(), res, doctor.Options{
		MaxTokens: maxTokens,
		// Changed(), not a value comparison: the flag's default is a real number, so
		// only cobra can distinguish "operator typed 2048" from "operator typed
		// nothing" — and that distinction is what lets a declared max_tokens apply.
		MaxTokensSet: cmd.Flags().Changed("max-tokens"),
		Timeout:      time.Duration(timeoutSecs) * time.Second,
		Nonce:        nonce,
	})

	if asJSON {
		if err := doctor.RenderJSON(cmd.OutOrStdout(), rep); err != nil {
			return err
		}
	} else {
		if err := doctor.RenderTableError(cmd.OutOrStdout(), rep); err != nil {
			return err
		}
		// Emit a CI-readable one-line summary to stderr after the table so log
		// scanners get a status signal without parsing the table output.
		var okCount int
		for _, a := range rep.Agents {
			if a.Status == doctor.StatusOK || a.Status == doctor.StatusOKWarning {
				okCount++
			}
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "doctor: %d ok / %d failed\n", okCount, len(rep.Agents)-okCount)
	}

	if rep.ExitCode != 0 {
		return fmt.Errorf("one or more agents have no working endpoint")
	}
	return nil
}

// filterRoster restricts the roster to the named subset, preserving each
// agent's original lane. Every requested name must be a directly-listed agent.
// --agents is a StringSlice, so names arrives already comma-split and
// repeat-accumulated by pflag.
func filterRoster(proj *registry.ProjectConfig, names []string) (*registry.ProjectConfig, error) {
	want := map[string]bool{}
	for _, name := range names {
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
