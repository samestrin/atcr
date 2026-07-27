// Package internal_test also guards how tests invoke git.
//
// A test helper that shells out to git without scoping the call to an explicit
// directory operates on whatever the ambient working directory happens to be.
// Inside a checkout, that is the developer's own repository. This is not
// theoretical: helpers of exactly this shape wrote fixture commits into this
// repository during a feature branch's development, rewriting its ref and
// discarding nine commits (recovered from the reflog).
//
// The rule is therefore structural rather than advisory: a git invocation in a
// test must name its target, via `-C <dir>` or cmd.Dir. Anything else is an
// invisible dependency on cwd that an unrelated test in another file can break
// — Go's t.Chdir is process-wide and explicitly incompatible with t.Parallel.
package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ambientGitAllowlist names test files that invoke git against the ambient
// working directory and have not yet been converted to an explicit target.
//
// This is a freeze, not an endorsement: it exists so the rule can be enforced
// for new code today rather than waiting on a sweep of existing helpers. Each
// entry is a latent hazard. Shrink this list; do not grow it. A new file added
// here should be a deliberate, argued exception, not a convenience.
//
// cli/range_test.go is additionally protected at runtime by
// requireIsolatedWorkdir, which refuses to run when cwd is inside a git
// repository. That guard is the pattern the remaining entries should adopt if
// converting them to an explicit directory proves awkward.
var ambientGitAllowlist = map[string]bool{
	"cli/range_test.go":                true,
	"cli/resume_test.go":               true,
	"cli/review_test.go":               true,
	"internal/personas/submit_test.go": true,
}

// TestTestHelpers_ScopeGitInvocations fails when a _test.go file builds a git
// command without naming the directory it should run in.
func TestTestHelpers_ScopeGitInvocations(t *testing.T) {
	root := ".."
	fset := token.NewFileSet()
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "node_modules", ".treehouse":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		if ambientGitAllowlist[rel] {
			return nil
		}

		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // unparseable files are not this test's concern
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		// cmd.Dir assignment is a statement away from the call expression, so a
		// per-call AST walk would miss it. File scope is the honest granularity:
		// a file that never mentions cmd.Dir and never passes -C has no way to
		// be scoping its git calls.
		scopesByDir := strings.Contains(string(src), ".Dir =")

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "exec" {
				return true
			}
			// First arg after any context must be the literal "git".
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || lit.Value != `"git"` {
					continue
				}
				if scopesByDir || strings.Contains(string(src), `"-C"`) {
					return true
				}
				offenders = append(offenders,
					rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line))
				return true
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)

	assert.Empty(t, offenders,
		"these tests invoke git against the ambient working directory, which inside a "+
			"checkout is the developer's own repository. Scope the call with `-C <dir>` or "+
			"cmd.Dir, or guard it the way cli/range_test.go's requireIsolatedWorkdir does.")
}

// TestAmbientGitAllowlist_HasNoStaleEntries keeps the freeze honest: an entry
// for a file that no longer exists, or that has since been scoped, silently
// weakens the rule and should be deleted rather than left to rot.
func TestAmbientGitAllowlist_HasNoStaleEntries(t *testing.T) {
	for rel := range ambientGitAllowlist {
		path := filepath.Join("..", rel)
		src, err := os.ReadFile(path)
		require.NoErrorf(t, err, "allowlist names %s, which no longer exists — remove the entry", rel)

		if !strings.Contains(string(src), `exec.Command("git"`) {
			t.Errorf("%s no longer invokes git directly — remove it from ambientGitAllowlist", rel)
		}
	}
}
