package cli

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// Epic 165.2 T1: AXI principle 1 is TOON on stdout. This is the registry-walking
// guard for it, in the style of cli/json_convention_test.go: every visible
// runnable command must have an AXI mode (hasAXIMode: an --axi flag or the
// markAXIDefault annotation), be a written exception below, or be on the gap
// list (axiPending, cli/axi_pending_test.go). A new command in none of them fails
// the walk, so it cannot ship printing text to an agent without someone deciding.

// axiExceptions are the written exceptions (epic 165.2, "Written exceptions"):
// commands that stay text or a non-TOON protocol, each with its reason. Each
// becomes an exception line of the AXI catalog entry; 165.2.1 T5 finishes it.
var axiExceptions = map[string]string{
	"atcr serve":   "1: MCP JSON-RPC over stdio; the protocol is the output",
	"atcr webhook": "2: a long-running daemon; its stdout is log lines, not a result",
	// 3 (completion) is cobra's hidden generator and never reaches the walk.
	"atcr benchmark": "4: a group that prints cobra help",
	"atcr config":    "4: a group that prints cobra help",
	"atcr debt":      "4: a group that prints cobra help",
	"atcr personas":  "4: a group that prints cobra help",
	// 5 (quickstart's interactive prompts) is a path, not a command: quickstart
	// itself is on the gap list.
	"atcr debt report":        "6: a Markdown document; the document is the product",
	"atcr debt dashboard":     "6: an HTML/Markdown document; the document is the product",
	"atcr leaderboard export": "6: the public submission JSON; its consumers read that schema",
	"atcr benchmark export":   "6: the suite-tagged submission JSON; its consumers read that schema",
	"atcr range":              "7: always prints the resolution JSON the skill pre-flight reads",
	"atcr version":            "prints the bare version string, the same in both modes (epic 165 T3)",
}

// axiCoverageViolations returns one message per command in root's tree that
// breaks the guard, sorted for stable output.
func axiCoverageViolations(root *cobra.Command) []string {
	pending := map[string]bool{}
	for _, cmds := range axiPending {
		for _, p := range cmds {
			pending[p] = true
		}
	}
	var out []string
	for path, c := range visibleRunnableCommands(root) {
		_, exception := axiExceptions[path]
		switch {
		case hasAXIMode(c) && (exception || pending[path]):
			out = append(out, path+": has an AXI mode; delete it from axiExceptions or axiPending")
		case hasAXIMode(c), exception, pending[path]:
		default:
			out = append(out, path+": no AXI mode; give it one (markAXIDefault and writeAXIResult), or list it in axiExceptions (cli/axi_coverage_test.go) or axiPending (cli/axi_pending_test.go)")
		}
	}
	sort.Strings(out)
	return out
}

func TestAXICoverage_EveryCommandHasAModeOrIsListed(t *testing.T) {
	assert.Empty(t, axiCoverageViolations(NewRootCmd()))
}

// TestAXICoverage_ListsNameRealCommands keeps the lists from rotting: a renamed
// or removed command must leave them, and no command is in two places.
func TestAXICoverage_ListsNameRealCommands(t *testing.T) {
	cmds := visibleRunnableCommands(NewRootCmd())
	seen := map[string]int{}
	for p := range axiExceptions {
		seen[p]++
	}
	for _, list := range axiPending {
		for _, p := range list {
			seen[p]++
		}
	}
	for p, n := range seen {
		assert.Contains(t, cmds, p, "listed command %q is not a visible runnable command", p)
		assert.Equal(t, 1, n, "command %q is listed more than once", p)
	}
}

// TestAXICoverage_StubCommand is the done-when: a stub grafted onto the real
// tree fails the walk while it is in no list, and passes once it is a written
// exception, on the gap list, or has an AXI mode.
func TestAXICoverage_StubCommand(t *testing.T) {
	const path = "atcr fresh-stub"
	withStub := func(mark bool) *cobra.Command {
		root := NewRootCmd()
		stub := &cobra.Command{Use: "fresh-stub", RunE: func(*cobra.Command, []string) error { return nil }}
		if mark {
			markAXIDefault(stub)
		}
		root.AddCommand(stub)
		return root
	}

	assert.Equal(t, []string{
		path + ": no AXI mode; give it one (markAXIDefault and writeAXIResult), or list it in axiExceptions (cli/axi_coverage_test.go) or axiPending (cli/axi_pending_test.go)",
	}, axiCoverageViolations(withStub(false)))

	t.Run("exception", func(t *testing.T) {
		axiExceptions[path] = "a test stub"
		t.Cleanup(func() { delete(axiExceptions, path) })
		assert.Empty(t, axiCoverageViolations(withStub(false)))
	})
	t.Run("pending", func(t *testing.T) {
		axiPending["test group"] = []string{path}
		t.Cleanup(func() { delete(axiPending, "test group") })
		assert.Empty(t, axiCoverageViolations(withStub(false)))
	})
	t.Run("AXI mode", func(t *testing.T) {
		assert.Empty(t, axiCoverageViolations(withStub(true)))
	})
}

// TestAXICoverage_CoveredCommandLeavesItsList pins the drop-off rule: once a
// pending or excepted command gains an AXI mode, the walk names it until its
// entry is deleted.
func TestAXICoverage_CoveredCommandLeavesItsList(t *testing.T) {
	root := NewRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "doctor" || c.Name() == "range" {
			markAXIDefault(c)
		}
	}
	assert.Equal(t, []string{
		"atcr doctor: has an AXI mode; delete it from axiExceptions or axiPending",
		"atcr range: has an AXI mode; delete it from axiExceptions or axiPending",
	}, axiCoverageViolations(root))
}
