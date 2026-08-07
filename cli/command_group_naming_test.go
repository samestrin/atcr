package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD row (cli/main.go): the six command groups split between singular and
// plural nouns with no documented rule. The clarified fix is to DOCUMENT the
// convention rather than rename the surface: singular is the rule (the in-repo
// majority and the git/docker/kubectl/gh convention), `models` and `personas`
// are grandfathered plurals, and `debt` (a mass noun) and `config` (one file)
// could not be pluralized anyway. Renaming would touch ~172 references —
// including skills/reference_test.go's pinned `atcr debt` prefix — for a
// consistency win no user feels.

// TestCommandGroupNounsAreSingular is the tripwire half of the convention: any
// NEW command group (a top-level command owning subcommands) must use a
// singular noun, so the documented rule cannot silently drift the next time a
// group is added.
func TestCommandGroupNounsAreSingular(t *testing.T) {
	grandfathered := map[string]bool{
		"models":   true, // plural since introduction; renaming is breaking
		"personas": true, // plural since introduction; renaming is breaking
	}
	for _, c := range NewRootCmd().Commands() {
		if len(c.Commands()) == 0 {
			continue // a leaf, not a group
		}
		if grandfathered[c.Name()] {
			continue
		}
		assert.False(t, strings.HasSuffix(c.Name(), "s"),
			"command group %q is a plural noun — the documented convention is singular (README command table); "+
				"add a singular group or record a new grandfathered exception in this test and the README", c.Name())
	}
}

// TestCommandGroupConventionIsDocumented pins the docs half: the README command
// table must state the singular rule and name the grandfathered exceptions, so
// the next contributor (or TD reviewer) finds a rule instead of drift.
func TestCommandGroupConventionIsDocumented(t *testing.T) {
	root := repoRootDir(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	require.NoError(t, err, "read README.md")
	readme := string(b)

	assert.Contains(t, readme, "Command-group nouns are singular",
		"README must document the singular command-group noun convention")
	assert.Contains(t, readme, "models", "the convention note must name the grandfathered plurals")
	assert.Contains(t, readme, "personas", "the convention note must name the grandfathered plurals")
	assert.Contains(t, readme, "grandfathered",
		"the convention note must mark models/personas as grandfathered exceptions, not examples to follow")
}
