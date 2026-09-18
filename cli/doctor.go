package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/doctor"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/payload"
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
	// payload.DefaultOutputTokens, matching the review fan-out's own
	// defaultMaxTokens (both reference the same constant, so they cannot drift).
	// A probe is only
	// evidence about the invocation it reproduces, and for an agent that declares no
	// max_tokens the invocation `atcr review` makes is capped at 8192 - so a probe at
	// the old 2048 reproduced a call review never makes, at a quarter the output
	// budget, and then reported the tier as "default" as though the two agreed. That
	// produced ok_warning on agents review runs fine, and the documented remedy (raise
	// the declaration) could make things worse: the cap is reserved out of the context
	// window, so raising it on a proxy-alias model can collapse the input budget to 0.
	cmd.Flags().Int("max-tokens", payload.DefaultOutputTokens, "completion budget per self-test call; matches the review fan-out's default so an undeclared agent is probed at the cap review will use")
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

	// Panel-composition warning: the built-in class guard walks embedded built-ins
	// only (community personas are a deliberate Out-of-Scope exclusion), so an
	// agent configured with a community persona can sit on a panel where every
	// built-in carries the panel-wide predicate-exhaustiveness rule and its
	// prompt does not — observable until now only as a quieter member in review
	// output. Name the gap at pre-flight; exit code is unchanged (this is
	// composition, not invocation health).
	// DIRECTLY-LISTED agents only, not res.Agents. doctor.Resolve registers every
	// node of every fallback chain in res.Agents, but a fallback's OWN persona is
	// never rendered: internal/fanout resolves the persona by the PRIMARY's name, so
	// a -backup entry's prompt reaches no review. Scanning the chain therefore named
	// agents whose rule gap cannot affect anything and sent the operator to edit a
	// file with no effect — an over-report whose prescribed remedy does nothing.
	// (Over-report only: a listed head is still scanned, so no real gap is missed.)
	listed := make([]string, 0, len(proj.Agents)+len(proj.SerialAgents))
	listed = append(listed, proj.Agents...)
	listed = append(listed, proj.SerialAgents...)
	agentToPersona := make(map[string]string, len(listed))
	for _, name := range listed {
		if ac, ok := reg.Agents[name]; ok {
			agentToPersona[name] = ac.Persona // loader defaults an empty persona to the agent name
		}
	}
	personaDirs := registry.PersonaDirs{
		Project:  filepath.Join(".atcr", "personas"),
		Registry: filepath.Join(filepath.Dir(regPath), "personas"),
	}
	rep.PredicateRuleGaps, rep.PersonaResolutionErrors = registry.PredicateRuleGaps(agentToPersona, personaDirs)

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
		if len(rep.PredicateRuleGaps) > 0 {
			// The scan covers whatever filterRoster left in proj, so with --agents it
			// covers the SELECTED subset — not the roster the old wording claimed.
			// Naming the real scope is what keeps the warning from implying the
			// unselected agents were checked and found clean.
			scope := "roster agents"
			if len(agentsFilter) > 0 {
				scope = "selected agents"
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"doctor: WARNING — predicate-exhaustiveness rule gaps: these %s resolve "+
					"to personas that do not carry the panel-wide rule (built-ins are test-enforced; "+
					"community and project personas are not): %s\n",
				scope, strings.Join(rep.PredicateRuleGaps, ", "))
		}
		// A DISTINCT line, not folded into the warning above. These agents were not
		// found to lack the rule — their prompt could not be read at all, so no
		// verdict was reached — and `atcr review` hard-fails on the same config that
		// doctor would otherwise report as clean.
		if len(rep.PersonaResolutionErrors) > 0 {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"doctor: WARNING — persona resolution errors: these agents' personas could not be "+
					"resolved, so no rule verdict was reached for them (`atcr review` resolves the same "+
					"personas and fails the run): %s\n",
				strings.Join(rep.PersonaResolutionErrors, ", "))
		}
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
