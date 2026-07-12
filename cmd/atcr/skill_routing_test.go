package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skillRoutingRow matches a routing-table row in skill/SKILL.md of the form
// `| ` + "`atcr <name>`" + ` | ... |`, capturing the top-level command name.
// Anchoring to the leading table pipe keeps prose mentions and the frontmatter
// description (which also list command names) out of the parsed set.
var skillRoutingRow = regexp.MustCompile("^\\|\\s*`atcr ([a-z][a-z-]*)`")

// topLevelRegistry returns the real top-level command surface registered in
// newRootCmd, excluding cobra's auto-registered help/completion which are not
// user-routed atcr commands and never appear in the SKILL.md routing table.
func topLevelRegistry() map[string]bool {
	reg := map[string]bool{}
	for _, c := range newRootCmd().Commands() {
		name := c.Name()
		if name == "help" || name == "completion" {
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
	data, err := os.ReadFile(filepath.Join(repoRootDir(t), "skill", "SKILL.md"))
	require.NoError(t, err, "read skill/SKILL.md")
	routed := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := skillRoutingRow.FindStringSubmatch(line); m != nil {
			routed[m[1]] = true
		}
	}
	require.NotEmpty(t, routed, "no `atcr <command>` routing rows parsed from skill/SKILL.md")
	return routed
}

// TestSkillRoutingTableMatchesRegistry closes the gap left by
// skill/skill_test.go's TestSkill_DispatcherRoutingTable, which only proves the
// SKILL.md table is a SUPERSET of a hand-copied slice. Living in cmd/atcr's own
// test package (which can import package main, unlike the skill package), it
// asserts BIDIRECTIONAL set-equality between the SKILL.md routing table and the
// real newRootCmd registry: every registered top-level command is routed, and
// every routed command is really registered. Adding a command to newRootCmd
// without documenting it, or documenting a command that does not exist, now
// fails the build. No production code or new package is required (TD
// skill/skill_test.go:131).
func TestSkillRoutingTableMatchesRegistry(t *testing.T) {
	registry := topLevelRegistry()
	routed := skillRoutedCommands(t)

	for name := range registry {
		assert.Contains(t, routed, name,
			"command %q is registered in newRootCmd but missing from the SKILL.md routing table", name)
	}
	for name := range routed {
		assert.Contains(t, registry, name,
			"SKILL.md routing table documents `atcr %s` but no such command is registered in newRootCmd", name)
	}
}
