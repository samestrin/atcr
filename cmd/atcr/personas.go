package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	commpersonas "github.com/samestrin/atcr/internal/personas"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/spf13/cobra"
)

// personasScoreData carries the per-reviewer corroboration rates (keyed by
// lowercase reviewer name) for `personas list --scores`, plus the scorecard
// path checked. An empty rates map drives the "no data" footer.
type personasScoreData struct {
	rates map[string]float64
	path  string
}

// personasScores loads corroboration rates from the scorecard store. A package
// var so tests inject a fake without filesystem access — and so the baseline
// `list` path can be verified never to call it.
var personasScores = loadPersonasScores

// loadPersonasScores reads the scorecard records, aggregates them, and keys the
// corroboration rate by lowercase reviewer name. A missing store yields zero
// records (no error) so `--scores` degrades to an all-n/a table with a footer.
func loadPersonasScores(errW io.Writer) (personasScoreData, error) {
	dir, err := scorecard.DefaultDir()
	if err != nil {
		return personasScoreData{}, err
	}
	records, err := scorecard.ReadAll(dir, scorecard.ReadOpts{Writer: errW})
	if err != nil {
		return personasScoreData{path: dir}, err
	}
	return personasScoreData{rates: reviewerCorroborationRates(scorecard.Aggregate(records)), path: dir}, nil
}

// reviewerCorroborationRates collapses leaderboard rows into one corroboration
// rate per reviewer, keyed by lowercase reviewer name. Aggregate groups by
// (reviewer, model), so a reviewer that ran under several models yields multiple
// rows sharing one Reviewer name; this sums corroborated/raised across those
// rows and recomputes the ratio (matching scorecard's own formula) so the rate
// is a true per-reviewer aggregate rather than whichever model's row sorted last.
func reviewerCorroborationRates(rows []scorecard.LeaderboardRow) map[string]float64 {
	type tally struct{ corroborated, raised int }
	byReviewer := map[string]*tally{}
	for _, row := range rows {
		key := strings.ToLower(row.Reviewer)
		t := byReviewer[key]
		if t == nil {
			t = &tally{}
			byReviewer[key] = t
		}
		t.corroborated += row.FindingsCorroborated
		t.raised += row.FindingsRaised
	}
	rates := make(map[string]float64, len(byReviewer))
	for name, t := range byReviewer {
		if t.raised > 0 {
			rates[name] = float64(t.corroborated) / float64(t.raised)
		} else {
			rates[name] = 0
		}
	}
	return rates
}

// personasDir resolves the community personas directory. A package var so tests
// can point it at a temp directory.
var personasDir = commpersonas.PersonasDir

// personasClient is the HTTP client used for community-repo fetches. Tests point
// ATCR_PERSONAS_URL at an httptest server and let the default client hit it.
var personasClient commpersonas.HTTPClient = &http.Client{Timeout: 30 * time.Second}

// personasFixtureRunner runs a persona's fixture for `atcr personas test`.
// The production default renders built-in persona templates against their
// embedded patch fixtures without a live LLM call. Tests inject stubs via
// withFixtureRunner to exercise pass/fail paths with controlled outcomes.
var personasFixtureRunner commpersonas.FixtureRunner = commpersonas.TemplateFixtureRunner{
	PersonasDir: func() (string, error) { return personasDir() },
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
				if bundleName == "" {
					return usageError(fmt.Errorf("bundle name is required (e.g. bundle/security)"))
				}
				return installBundle(cmd, dir, bundleName)
			}
			// InstallUnit delivers the complete self-contained unit — the YAML plus
			// its co-located custom prompt (<name>.md) when present — and enforces
			// the C3 fetched-prompt guardrails, so `personas install` and the
			// init/quickstart fetch-and-pin path deliver identical units (C2).
			if err := commpersonas.InstallUnit(personasClient, commpersonas.BaseURL(), name, dir); err != nil {
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
	outcomes, err := commpersonas.InstallBundle(personasClient, commpersonas.BaseURL(), bundleName, dir)
	if err != nil {
		return err // includes ErrUnknownBundle ("unknown bundle: \"<name>\"")
	}
	var failed int
	for _, o := range outcomes {
		switch {
		case o.Err != nil:
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "failed to install %s: %v\n", o.Name, o.Err)
			failed++
		case o.AlreadyPresent:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s already present\n", o.Name)
		default:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Installed %s\n", o.Name)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d bundle personas failed to install", failed, len(outcomes))
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
				return listPersonasWithScores(cmd, dir)
			}
			// Show all three resolver tiers (project > community > built-in).
			projectDir := filepath.Join(".atcr", "personas")
			metas, listErr := commpersonas.ListTiers(projectDir, dir)
			if listErr != nil {
				// Degrade gracefully: warn but still render the built-ins.
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", listErr)
			}
			return renderPersonaList(cmd.OutOrStdout(), metas)
		},
	}
	cmd.Flags().Bool("scores", false, "show each persona's corroboration rate from past review runs (n/a when no run history)")
	return cmd
}

// listPersonasWithScores renders the persona table with a CORROBORATION column,
// joining each persona to its scorecard rate. When the scorecard store has no
// data, every row shows n/a and a footer names the path that was checked.
func listPersonasWithScores(cmd *cobra.Command, dir string) error {
	data, err := personasScores(cmd.ErrOrStderr())
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not read scorecard data: %v\n", err)
	}
	// Use the same three-tier resolver ordering as the plain list so the Source
	// column is consistent and project overrides shadow community/built-ins.
	projectDir := filepath.Join(".atcr", "personas")
	scored, listErr := commpersonas.ListTiersWithScores(projectDir, dir, data.rates)
	if listErr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", listErr)
	}
	if err := renderScoredList(cmd.OutOrStdout(), scored); err != nil {
		return err
	}
	switch {
	case err != nil:
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nScorecard data at %s is unreadable\n", data.path)
	case len(data.rates) == 0:
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nNo scorecard data found at %s\n", data.path)
	}
	return nil
}

func newPersonasSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [keyword]",
		Short: "Search the community repository by keyword, --model, or --provider",
		Long: "Search community personas. A positional keyword matches a persona's name,\n" +
			"description, provider, or model. --model and --provider filter on the\n" +
			"structured index fields only (never free text) and combine with the keyword\n" +
			"and each other as AND conditions. Discover a persona by the model you hold:\n" +
			"`atcr personas search --model deepseek`.",
		// Relaxed from ExactArgs(1): a --model/--provider-only invocation needs no
		// positional keyword. The RunE guard rejects the all-empty case.
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			var keyword string
			if len(args) == 1 {
				keyword = strings.TrimSpace(args[0])
			}
			model, _ := cmd.Flags().GetString("model")
			provider, _ := cmd.Flags().GetString("provider")
			model = strings.TrimSpace(model)
			provider = strings.TrimSpace(provider)

			// At least one non-empty filter is required; an all-empty invocation must
			// not silently run an unfiltered whole-index match (AC 03-03).
			if keyword == "" && model == "" && provider == "" {
				return usageError(fmt.Errorf("provide a keyword, --model, or --provider"))
			}

			entries, err := commpersonas.SearchWithOptions(personasClient, commpersonas.BaseURL(),
				commpersonas.SearchOptions{Keyword: keyword, Model: model, Provider: provider})
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				// Preserve the exact "matching <keyword>" wording for the keyword path
				// (AC 03-02 Edge Case 1); a flag-only search has no single keyword.
				if keyword != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No personas found matching %q\n", keyword)
				} else {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No personas found")
				}
				return nil
			}
			return renderPersonaSearch(cmd.OutOrStdout(), entries)
		},
	}
	cmd.Flags().String("model", "", "filter by the persona's bound model (structured field; substring, case-insensitive)")
	cmd.Flags().String("provider", "", "filter by the persona's routing-endpoint provider key (structured field)")
	return cmd
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
			if err := commpersonas.Remove(args[0], dir); err != nil {
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
			name := args[0]
			// Resolution of the target (built-in vs. installed community persona) is
			// owned by personasFixtureRunner's PersonasDir seam, so the command does
			// not resolve the personas dir itself.
			outcome, err := commpersonas.TestPersona(name, personasFixtureRunner)
			if err != nil {
				return err
			}
			switch {
			case !outcome.HasFixture:
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No fixture defined for persona %q\n", name)
				return nil
			case outcome.Total == 0:
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "WARN: no test cases defined for persona %q\n", name)
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
		res, err := commpersonas.Upgrade(personasClient, commpersonas.BaseURL(), dir, name, dryRun)
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "failed to upgrade %s: %v (skipping)\n", name, err)
			failed = true
			continue
		}
		switch {
		case res.Resolved && res.SlugChanged && dryRun:
			// 19.7 resolved-lock path (dry run): report the before→after slug.
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Would upgrade %s: %s → %s\n", name, res.FromSlug, res.ToSlug)
		case res.Resolved && res.SlugChanged:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Upgraded %s: %s → %s\n", name, res.FromSlug, res.ToSlug)
		case res.Resolved:
			// Resolution ran but the lock did not advance — report explicitly, never omit.
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (unchanged)\n", name, res.ToSlug)
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
	metas, err := commpersonas.List(dir)
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

// writeTable renders a tab-separated table to w using a tabwriter. header is a
// tab-delimited column heading; rows are tab-delimited data lines.
func writeTable(w io.Writer, header string, rows []string) error {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, header)
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, row)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// formatLanguages returns a comma-joined language list, or "-" when empty.
func formatLanguages(langs []string) string {
	if len(langs) == 0 {
		return "-"
	}
	return strings.Join(langs, ", ")
}

// renderPersonaList writes the Name/Version/Source/Language table.
func renderPersonaList(w io.Writer, metas []commpersonas.PersonaMeta) error {
	rows := make([]string, len(metas))
	for i, m := range metas {
		rows[i] = fmt.Sprintf("%s\t%s\t%s\t%s",
			sanitizeCell(m.Name), sanitizeCell(m.Version), sanitizeCell(m.Source), sanitizeCell(formatLanguages(m.Language)))
	}
	return writeTable(w, "NAME\tVERSION\tSOURCE\tLANGUAGE", rows)
}

// renderScoredList writes the Name/Version/Source/Language/Corroboration table,
// rendering each persona's rate as "XX.X%" or "n/a".
func renderScoredList(w io.Writer, scored []commpersonas.ScoredPersona) error {
	rows := make([]string, len(scored))
	for i, s := range scored {
		rows[i] = fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
			sanitizeCell(s.Name), sanitizeCell(s.Version), sanitizeCell(s.Source),
			sanitizeCell(formatLanguages(s.Language)), commpersonas.FormatRate(s.Rate))
	}
	return writeTable(w, "NAME\tVERSION\tSOURCE\tLANGUAGE\tCORROBORATION", rows)
}

// renderPersonaSearch writes the Name/Version/Provider/Model/Description table of
// index hits. Provider and Model let a user confirm which model a returned persona
// targets before installing. Empty Version/Provider/Model render as "-".
func renderPersonaSearch(w io.Writer, entries []commpersonas.PersonaIndexEntry) error {
	rows := make([]string, len(entries))
	for i, e := range entries {
		rows[i] = fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
			sanitizeCell(e.Name), orDash(sanitizeCell(e.Version)), orDash(sanitizeCell(e.Provider)),
			orDash(sanitizeCell(e.Model)), sanitizeCell(e.Description))
	}
	return writeTable(w, "NAME\tVERSION\tPROVIDER\tMODEL\tDESCRIPTION", rows)
}

// orDash returns s, or "-" when s is empty — the placeholder convention shared by
// the persona table renderers for absent optional values.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
