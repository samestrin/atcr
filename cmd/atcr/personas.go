package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/samestrin/atcr/internal/personas"
	"github.com/spf13/cobra"
)

// personasDir resolves the community personas directory. A package var so tests
// can point it at a temp directory.
var personasDir = personas.PersonasDir

// personasClient is the HTTP client used for community-repo fetches. Tests point
// ATCR_PERSONAS_URL at an httptest server and let the default client hit it.
var personasClient personas.HTTPClient = http.DefaultClient

// personasFixtureRunner runs a persona's fixture for `atcr personas test`. The
// default runner reports no fixture (a real LLM-backed runner is out of this
// phase's scope — see TD); tests inject a stub to exercise the pass/fail paths.
var personasFixtureRunner personas.FixtureRunner = noFixtureRunner{}

// noFixtureRunner is the default: it reports that no runnable fixture is wired,
// so `atcr personas test <name>` exits 0 with an informational message rather
// than making a live LLM call.
type noFixtureRunner struct{}

func (noFixtureRunner) RunFixture(string) (personas.FixtureOutcome, error) {
	return personas.FixtureOutcome{HasFixture: false}, nil
}

// newPersonasCmd builds `atcr personas`: a parent command hosting the six
// community-persona lifecycle sub-subcommands.
func newPersonasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "personas",
		Short: "Manage community reviewer personas",
		Long: "Manage community-contributed reviewer personas: install, list, search,\n" +
			"remove, test, and upgrade personas fetched from a configurable repository.\n\n" +
			"Installed personas live under ~/.config/atcr/personas/ and are available to\n" +
			"the reviewer panel on the next review. The repository base URL defaults to\n" +
			"the public community repo and is overridable via ATCR_PERSONAS_URL.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		newPersonasInstallCmd(),
		newPersonasListCmd(),
		newPersonasSearchCmd(),
		newPersonasRemoveCmd(),
		newPersonasTestCmd(),
		newPersonasUpgradeCmd(),
	)
	return cmd
}

func newPersonasInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <namespace/name>",
		Short: "Install a community persona from the repository",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := personasDir()
			if err != nil {
				return err
			}
			name := args[0]
			if bundleName, ok := strings.CutPrefix(name, "bundle/"); ok {
				return installBundle(cmd, dir, bundleName)
			}
			if err := personas.Install(personasClient, personas.BaseURL(), name, dir); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Installed persona %q\n", name)
			return nil
		},
	}
}

// installBundle expands a bundle into its member personas and installs each,
// reporting per-member outcome. An unknown bundle exits non-zero with a clear
// message; a member fetch/write failure is reported but does not abort the
// remaining members, and the command exits non-zero if any member failed.
func installBundle(cmd *cobra.Command, dir, bundleName string) error {
	outcomes, err := personas.InstallBundle(personasClient, personas.BaseURL(), bundleName, dir)
	if err != nil {
		return err // includes ErrUnknownBundle ("unknown bundle: \"<name>\"")
	}
	var failed bool
	for _, o := range outcomes {
		switch {
		case o.Err != nil:
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "failed to install %s: %v\n", o.Name, o.Err)
			failed = true
		case o.AlreadyPresent:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s already present\n", o.Name)
		default:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Installed %s\n", o.Name)
		}
	}
	if failed {
		return fmt.Errorf("one or more bundle personas failed to install")
	}
	return nil
}

func newPersonasListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed personas (built-in and community)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := personasDir()
			if err != nil {
				return err
			}
			if scores, _ := cmd.Flags().GetBool("scores"); scores {
				// Scores are wired to the scorecard in a later phase; accepted as a
				// no-op here so scripts can opt in early without erroring.
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "(--scores available in a future release)")
			}
			metas, listErr := personas.List(dir)
			if listErr != nil {
				// Degrade gracefully: warn but still render the built-ins.
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", listErr)
			}
			return renderPersonaList(cmd.OutOrStdout(), metas)
		},
	}
	cmd.Flags().Bool("scores", false, "show per-persona corroboration scores (future release)")
	return cmd
}

func newPersonasSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <keyword>",
		Short: "Search the community repository by keyword",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyword := args[0]
			entries, err := personas.Search(personasClient, personas.BaseURL(), keyword)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No personas found matching %q\n", keyword)
				return nil
			}
			return renderPersonaSearch(cmd.OutOrStdout(), entries)
		},
	}
}

func newPersonasRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <namespace/name>",
		Short: "Remove an installed community persona",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := personasDir()
			if err != nil {
				return err
			}
			if err := personas.Remove(args[0], dir); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed persona %q\n", args[0])
			return nil
		},
	}
}

func newPersonasTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <name>",
		Short: "Run a persona against its fixture and report pass/fail",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := personasDir()
			if err != nil {
				return err
			}
			name := args[0]
			outcome, err := personas.TestPersona(dir, name, personasFixtureRunner)
			if err != nil {
				return err
			}
			switch {
			case !outcome.HasFixture:
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No fixture defined for persona %q\n", name)
				return nil
			case outcome.Passed == outcome.Total:
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "PASS: %s (%d/%d cases)\n", name, outcome.Passed, outcome.Total)
				return nil
			default:
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "FAIL: %s (%d/%d cases)\n", name, outcome.Passed, outcome.Total)
				return fmt.Errorf("persona %q fixture failed: %d/%d cases passed", name, outcome.Passed, outcome.Total)
			}
		},
	}
}

func newPersonasUpgradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade [name]",
		Short: "Upgrade an installed community persona (or all with --all)",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := personasDir()
			if err != nil {
				return err
			}
			all, _ := cmd.Flags().GetBool("all")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if all && len(args) > 0 {
				return usageError(fmt.Errorf("cannot specify both a persona name and --all"))
			}
			if !all && len(args) == 0 {
				return usageError(fmt.Errorf("requires a persona name or --all"))
			}

			names := args
			if all {
				names, err = installedCommunityNames(dir)
				if err != nil {
					return err
				}
				if len(names) == 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No community personas installed")
					return nil
				}
			}
			return runPersonaUpgrades(cmd, dir, names, dryRun)
		},
	}
	cmd.Flags().Bool("all", false, "upgrade every installed community persona")
	cmd.Flags().Bool("dry-run", false, "report what would change without writing")
	return cmd
}

// runPersonaUpgrades upgrades each named persona sequentially, reporting per
// persona. A fetch/validation failure for one persona is reported and skipped;
// the command exits non-zero if any persona failed.
func runPersonaUpgrades(cmd *cobra.Command, dir string, names []string, dryRun bool) error {
	var failed bool
	for _, name := range names {
		res, err := personas.Upgrade(personasClient, personas.BaseURL(), dir, name, dryRun)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "failed to upgrade %s: %v (skipping)\n", name, err)
			failed = true
			continue
		}
		switch {
		case res.UpToDate:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s is already up to date (%s)\n", name, res.ToVersion)
		case dryRun:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Would upgrade %s: %s → %s\n", name, res.FromVersion, res.ToVersion)
		default:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Upgraded %s: %s → %s\n", name, res.FromVersion, res.ToVersion)
		}
	}
	if failed {
		return fmt.Errorf("one or more personas failed to upgrade")
	}
	return nil
}

// installedCommunityNames lists the names of community personas under dir.
func installedCommunityNames(dir string) ([]string, error) {
	metas, err := personas.List(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range metas {
		if m.Source == "community" {
			names = append(names, m.Name)
		}
	}
	return names, nil
}

// renderPersonaList writes the Name/Version/Source/Language table.
func renderPersonaList(w io.Writer, metas []personas.PersonaMeta) error {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tVERSION\tSOURCE\tLANGUAGE")
	for _, m := range metas {
		lang := "-"
		if len(m.Language) > 0 {
			lang = strings.Join(m.Language, ", ")
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.Name, m.Version, m.Source, lang)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// renderPersonaSearch writes the Name/Version/Description table of index hits.
func renderPersonaSearch(w io.Writer, entries []personas.PersonaIndexEntry) error {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tVERSION\tDESCRIPTION")
	for _, e := range entries {
		version := e.Version
		if version == "" {
			version = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, version, e.Description)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}
