package cli

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// Epic 35.22 T5: atcr's convention is a human default with an opt-in `--json`
// (docs/diff-smell.md). This file is the registry-walking guard for it: every
// visible runnable command must be named in exactly one of the lists below, and
// every command in reportCommands must accept a boolean --json. A new command
// that is in no list fails the walk, so it cannot ship without someone deciding
// whether it is a report — and if it is, it cannot ship without --json.

// reportCommands produce a report: a read-only rendering of stored or computed
// state for a reader. Each must accept --json.
var reportCommands = map[string]bool{
	"atcr debt dashboard": true,
	"atcr debt list":      true,
	"atcr debt resolve":   true,
	"atcr diff-smell":     true,
	"atcr doctor":         true,
	"atcr history":        true,
	"atcr leaderboard":    true,
	"atcr models check":   true,
	"atcr personas list":  true,
	"atcr quality-report": true,
	"atcr report":         true,
	"atcr scorecard":      true,
}

// jsonExemptions are the plan's named exemptions. Each must NOT have --json:
// once one gains a real flag (35.16.14.1 is slated to give `status` one), this
// test fails until the command moves to reportCommands.
var jsonExemptions = map[string]string{
	"atcr range":  "never prints a human form: its only output is the resolution JSON",
	"atcr status": "never prints a human form: its only output is the progress JSON",
	"atcr trust":  "an authorization command, not a report",
}

// jsonConventionGaps produce a report but have no --json, and are not among the
// commands epic 35.22's source rows named (cli/leaderboard.go:34,
// cli/history.go:36). They are listed here, not silently exempted, so the gap
// stays visible; like jsonExemptions, an entry must leave this list when its
// command gains --json.
var jsonConventionGaps = map[string]string{
	"atcr audit-report":    "markdown compliance report; no machine-readable path yet",
	"atcr benchmark fit":   "per persona+model verdict table; no machine-readable path yet",
	"atcr personas search": "search-result table; no machine-readable path yet",
}

// nonReportCommands act, write files, or print help; the convention does not
// apply to them.
var nonReportCommands = map[string]string{
	"atcr benchmark":                    "group: prints help",
	"atcr benchmark export":             "writes the suite-tagged submission record (JSON by construction)",
	"atcr benchmark run":                "executes a suite and writes a run-result",
	"atcr benchmark verify":             "validates a manifest and prints its hash",
	"atcr config":                       "group: prints help",
	"atcr config set":                   "writes .atcr/config.yaml",
	"atcr debate":                       "runs the debate stage",
	"atcr debt":                         "group: prints help",
	"atcr debt add":                     "writes a TD item",
	"atcr debt backfill-justifications": "repairs stored justifications",
	"atcr debt compact":                 "rewrites the local TD store",
	"atcr debt sync-outcomes":           "records TD README outcomes",
	"atcr github":                       "posts a check run",
	"atcr init":                         "writes config and persona files",
	"atcr leaderboard export":           "writes the public submission envelope (JSON by construction)",
	"atcr models":                       "group: prints help",
	"atcr models refresh":               "regenerates the catalog snapshot",
	"atcr personas":                     "group: prints help",
	"atcr personas install":             "installs a persona",
	"atcr personas remove":              "removes a persona",
	"atcr personas submit":              "opens a fork+PR",
	"atcr personas test":                "runs a persona against its fixture",
	"atcr personas upgrade":             "upgrades a persona",
	"atcr quickstart":                   "interactive onboarding",
	"atcr reconcile":                    "writes reconciled artifacts",
	"atcr review":                       "runs the reviewer fan-out",
	"atcr serve":                        "runs the MCP server",
	"atcr skill":                        "group: prints help",
	"atcr skill export":                 "writes the embedded skill tree",
	"atcr verify":                       "runs the skeptic stage",
	"atcr version":                      "prints the version string",
}

// visibleRunnableCommands walks root and returns every command a user can run
// and see in help, keyed by its full command path. Hidden commands are
// deprecated aliases, not surface.
func visibleRunnableCommands(root *cobra.Command) map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			if sub.Runnable() {
				out[sub.CommandPath()] = sub
			}
			walk(sub)
		}
	}
	walk(root)
	return out
}

func hasBoolJSONFlag(c *cobra.Command) bool {
	f := c.Flags().Lookup("json")
	return f != nil && f.Value.Type() == "bool"
}

// jsonConventionViolations returns one message per command in root's tree that
// breaks the convention, sorted for stable output.
func jsonConventionViolations(root *cobra.Command) []string {
	var out []string
	for path, c := range visibleRunnableCommands(root) {
		_, exempt := jsonExemptions[path]
		_, gap := jsonConventionGaps[path]
		_, nonReport := nonReportCommands[path]
		switch {
		case reportCommands[path]:
			if !hasBoolJSONFlag(c) {
				out = append(out, path+": a report-producing command must accept --json (use addJSONFlag)")
			}
		case exempt || gap:
			if c.Flags().Lookup("json") != nil {
				out = append(out, path+": now has --json; move it from its exemption/gap list to reportCommands")
			}
		case nonReport:
		default:
			out = append(out, path+": not classified; add it to reportCommands (and give it --json) or to nonReportCommands in cli/json_convention_test.go")
		}
	}
	sort.Strings(out)
	return out
}

func TestJSONConvention_EveryReportCommandAcceptsJSON(t *testing.T) {
	assert.Empty(t, jsonConventionViolations(NewRootCmd()))
}

// TestJSONConvention_ListsNameRealCommands keeps the lists from rotting: a
// renamed or removed command must leave them, or an entry would vouch for
// nothing.
func TestJSONConvention_ListsNameRealCommands(t *testing.T) {
	cmds := visibleRunnableCommands(NewRootCmd())
	seen := map[string]int{}
	for p := range reportCommands {
		seen[p]++
	}
	for _, m := range []map[string]string{jsonExemptions, jsonConventionGaps, nonReportCommands} {
		for p := range m {
			seen[p]++
		}
	}
	for p, n := range seen {
		assert.Contains(t, cmds, p, "listed command %q is not a visible runnable command", p)
		assert.Equal(t, 1, n, "command %q is in more than one list", p)
	}
}

// TestJSONConvention_FailsOnNewReportWithoutJSON is the done-when: grafting a
// new command without --json onto the real tree makes the walk fail, whether it
// is left unclassified or classified as a report.
func TestJSONConvention_FailsOnNewReportWithoutJSON(t *testing.T) {
	newReport := func() *cobra.Command {
		root := NewRootCmd()
		root.AddCommand(&cobra.Command{Use: "fresh-report", RunE: func(*cobra.Command, []string) error { return nil }})
		return root
	}

	assert.Equal(t, []string{
		"atcr fresh-report: not classified; add it to reportCommands (and give it --json) or to nonReportCommands in cli/json_convention_test.go",
	}, jsonConventionViolations(newReport()))

	reportCommands["atcr fresh-report"] = true
	t.Cleanup(func() { delete(reportCommands, "atcr fresh-report") })
	assert.Equal(t, []string{
		"atcr fresh-report: a report-producing command must accept --json (use addJSONFlag)",
	}, jsonConventionViolations(newReport()))

	withJSON := newReport()
	for _, c := range withJSON.Commands() {
		if c.Name() == "fresh-report" {
			addJSONFlag(c)
		}
	}
	assert.Empty(t, jsonConventionViolations(withJSON))
}

// TestJSONConvention_ExemptionGainingJSONFails pins the drop-off rule: when an
// exempt command (status, per 35.16.14.1) gains --json, the walk names it.
func TestJSONConvention_ExemptionGainingJSONFails(t *testing.T) {
	root := NewRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "status" {
			addJSONFlag(c)
		}
	}
	assert.Equal(t, []string{
		"atcr status: now has --json; move it from its exemption/gap list to reportCommands",
	}, jsonConventionViolations(root))
}
