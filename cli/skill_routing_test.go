package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skillRoutingRow matches a routing-table row in skills/atcr/SKILL.md of the form
// `| ` + "`atcr <name>`" + ` | ... |`, capturing the top-level command name.
// Anchoring to the leading table pipe keeps prose mentions and the frontmatter
// description (which also list command names) out of the parsed set.
var skillRoutingRow = regexp.MustCompile("^\\|\\s*`atcr ([a-z][a-z-]*)`")

// topLevelRegistry returns the real top-level command surface registered in
// NewRootCmd, excluding cobra's auto-registered help/completion which are not
// user-routed atcr commands and never appear in the SKILL.md routing table.
// Hidden commands are maintainer-facing and deliberately not user-routed, so
// they are excluded too — the same definition of "the command surface" that
// TestRootCmd_HasExactlyTwentySevenSubcommands (cli/main_test.go) uses.
func topLevelRegistry() map[string]bool {
	reg := map[string]bool{}
	for _, c := range NewRootCmd().Commands() {
		name := c.Name()
		if c.Hidden || name == "help" || name == "completion" {
			continue
		}
		reg[name] = true
	}
	return reg
}

// skillRoutedCommands returns the top-level commands the SKILL.md routing table
// documents as `atcr <name>`.
func skillRoutedCommands(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootDir(t), "skills", "atcr", "SKILL.md"))
	require.NoError(t, err, "read skills/atcr/SKILL.md")
	routed := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := skillRoutingRow.FindStringSubmatch(line); m != nil {
			routed[m[1]] = true
		}
	}
	require.NotEmpty(t, routed, "no `atcr <command>` routing rows parsed from skills/atcr/SKILL.md")
	return routed
}

// TestSkillRoutingTableMatchesRegistry closes the gap left by
// skills/skills_test.go's TestSkill_DispatcherRoutingTable, which only proves the
// SKILL.md table is a SUPERSET of a hand-copied slice. Living in cmd/atcr's own
// test package (which can import package main, unlike the skill package), it
// asserts BIDIRECTIONAL set-equality between the SKILL.md routing table and the
// real NewRootCmd registry: every registered top-level command is routed, and
// every routed command is really registered. Adding a command to NewRootCmd
// without documenting it, or documenting a command that does not exist, now
// fails the build. No production code or new package is required (TD
// skills/skills_test.go:131).
func TestSkillRoutingTableMatchesRegistry(t *testing.T) {
	registry := topLevelRegistry()
	routed := skillRoutedCommands(t)

	for name := range registry {
		assert.Contains(t, routed, name,
			"command %q is registered in NewRootCmd but missing from the SKILL.md routing table", name)
	}
	for name := range routed {
		assert.Contains(t, registry, name,
			"SKILL.md routing table documents `atcr %s` but no such command is registered in NewRootCmd", name)
	}
}

// skillSubcommandOwners matches SKILL.md's one sentence naming the top-level
// commands that own subcommands, capturing the backticked list.
var skillSubcommandOwners = regexp.MustCompile(`Top-level commands that own subcommands \(([^)]*)\)`)

// TestSkillSubcommandOwnersMatchRegistry (epic 35.22 T7) — SKILL.md tells the
// agent which commands expose subcommands through `atcr <command> --help` and
// never to invent subcommand names. A command missing from that list is one
// whose subcommands the agent is told nothing about: `leaderboard` gained
// `export` in epic 35.22. The list must equal the registry's top-level commands
// that own at least one visible subcommand.
func TestSkillSubcommandOwnersMatchRegistry(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRootDir(t), "skills", "atcr", "SKILL.md"))
	require.NoError(t, err, "read skills/atcr/SKILL.md")
	m := skillSubcommandOwners.FindStringSubmatch(string(data))
	require.NotNil(t, m, "SKILL.md no longer names the top-level commands that own subcommands")
	documented := map[string]bool{}
	for _, name := range regexp.MustCompile("`([a-z][a-z-]*)`").FindAllStringSubmatch(m[1], -1) {
		documented[name[1]] = true
	}

	owners := map[string]bool{}
	for _, c := range NewRootCmd().Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		for _, sub := range c.Commands() {
			if !sub.Hidden {
				owners[c.Name()] = true
				break
			}
		}
	}
	assert.Equal(t, owners, documented,
		"SKILL.md's list of commands that own subcommands must match the registry")
}
