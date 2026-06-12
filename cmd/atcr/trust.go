package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/spf13/cobra"
)

// newTrustCmd builds `atcr trust`: review and authorize the providers a project
// defines in .atcr/registry.yaml. A project-defined provider can direct requests
// to an arbitrary base_url under any api_key_env, so it cannot receive a key
// until it is explicitly trusted here. Project agents that reference an existing
// user-defined provider need no authorization.
//
//	atcr trust            list project providers and their trust status
//	atcr trust <name>...  authorize the named project providers
//	atcr trust --all      authorize every project provider
func newTrustCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust [provider...]",
		Short: "Authorize project-defined providers (.atcr/registry.yaml)",
		Long: "Project-defined providers can direct requests to an arbitrary base_url under\n" +
			"any api_key_env, so atcr refuses to send a key to one until you authorize it.\n" +
			"Run with no arguments to list project providers and their trust status; pass\n" +
			"provider names (or --all) to pin them in ~/.config/atcr/trusted_providers.yaml.\n" +
			"Trust pins the (base_url, api_key_env) pair: change either and you must re-trust.",
		Args: usageArgs(cobra.ArbitraryArgs),
		RunE: runTrust,
	}
	cmd.Flags().Bool("all", false, "authorize every project-defined provider")
	return cmd
}

// runTrust lists or authorizes project-defined providers. Configuration
// problems map to exit 2.
func runTrust(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")

	pr, err := registry.LoadProjectRegistry(registry.DefaultProjectRegistryPath("."))
	if err != nil {
		return usageError(err)
	}
	if pr == nil || len(pr.Providers) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No project-defined providers in .atcr/registry.yaml.")
		return nil
	}

	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return usageError(err)
	}
	storePath := registry.DefaultTrustStorePath(filepath.Dir(regPath))
	store, err := registry.LoadTrustStore(storePath)
	if err != nil {
		return usageError(err)
	}

	// No selection → list status and exit.
	if !all && len(args) == 0 {
		listProjectProviders(cmd, pr, store)
		return nil
	}

	// Resolve the selection.
	want := args
	if all {
		want = sortedProviderNames(pr)
	}
	var trusted int
	for _, name := range want {
		p, ok := pr.Providers[name]
		if !ok {
			return usageError(fmt.Errorf("%q is not a project-defined provider in .atcr/registry.yaml", name))
		}
		if store.IsTrusted(p) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "already trusted: %s\n", name)
			continue
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"trusting %s → base_url=%s  api_key_env=%s\n", name, p.BaseURL, p.APIKeyEnv)
		store.Trust(name, p)
		trusted++
	}
	if trusted > 0 {
		if err := store.Save(); err != nil {
			return usageError(err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %d new trust entr%s to %s\n", trusted, plural(trusted), storePath)
	}
	return nil
}

// listProjectProviders prints each project provider with its endpoint and a
// trusted/untrusted marker.
func listProjectProviders(cmd *cobra.Command, pr *registry.ProjectRegistry, store *registry.TrustStore) {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PROVIDER\tBASE_URL\tAPI_KEY_ENV\tTRUST")
	for _, name := range sortedProviderNames(pr) {
		p := pr.Providers[name]
		status := "UNTRUSTED"
		if store.IsTrusted(p) {
			status = "trusted"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, p.BaseURL, p.APIKeyEnv, status)
	}
	_ = tw.Flush()
}

// sortedProviderNames returns the project provider names in sorted order.
func sortedProviderNames(pr *registry.ProjectRegistry) []string {
	names := make([]string, 0, len(pr.Providers))
	for name := range pr.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
