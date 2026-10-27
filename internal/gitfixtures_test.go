// Package internal_test also guards how tests invoke git.
//
// A test helper that shells out to git operates on whatever repository git
// discovers, and inside a checkout that is the developer's own. This is not
// theoretical: fixture helpers wrote commits into this repository four times in
// one afternoon (Epic 45.0), rewriting a branch ref and discarding nine commits,
// stacking hundreds of stray commits, and flipping core.bare to true.
//
// Scoping a call with `-C <dir>` or cmd.Dir is NOT enough. Git exports GIT_DIR
// into a hook's environment, and GIT_DIR beats both, so a correctly scoped
// fixture run from the pre-push hook still retargeted at the real repository.
// The rule is therefore structural: every git command a test builds goes through
// gitexec.FixtureCommandContext, which pins GIT_DIR, GIT_WORK_TREE and
// GIT_CEILING_DIRECTORIES to the fixture. A bare exec.Command("git", ...) and the
// production constructors gitexec.CommandFn / gitexec.CommandContextFn (stubbable,
// and blind to an inherited GIT_DIR) are both offenders in a _test.go file.
package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ambientGitAllowlist named test files that invoked git against the ambient
// working directory while they waited to be converted. Epic 45.0 converted every
// one of them, and the gate below no longer accepts a scoped-but-bare call
// either, so there is nothing left to freeze. It stays empty: a test that needs
// git uses gitexec.FixtureCommandContext, and a test that only reads the real
// checkout belongs in readOnlyGitReaders with its reason.
var ambientGitAllowlist = map[string]bool{}

// readOnlyGitReaders are the test files that deliberately run git against the
// real checkout rather than a fixture, so FixtureCommandContext (which pins
// GIT_DIR to a fixture's .git) does not fit them. Each only reads; none writes.
var readOnlyGitReaders = map[string]string{
	"skills/docs_test.go":                        "git ls-files over the repository's tracked .go files",
	"skills/layout_test.go":                      "git ls-files over the repository's tracked files",
	"internal/payload/escalation_replay_test.go": "git log / rev-parse over the repository's own history",
	"cli/changelog_tags_test.go":                 "git tag --list over the repository's own tags",
	"internal/gitexec/gitexec_test.go":           "tests the constructors themselves, against temp repos",
}

// gitexecImportPath is the package whose fixture constructor the gate requires.
const gitexecImportPath = "github.com/samestrin/atcr/internal/gitexec"

// TestTestHelpers_ScopeGitInvocations fails when a _test.go file builds a git
// command any way other than gitexec.FixtureCommandContext.
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
		if ambientGitAllowlist[rel] || readOnlyGitReaders[rel] != "" {
			return nil
		}

		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // unparseable files are not this test's concern
		}
		for _, line := range unfixturedGitCalls(fset, file) {
			offenders = append(offenders, rel+":"+strconv.Itoa(line))
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(offenders)
	assert.Empty(t, offenders,
		"these tests build a git command without gitexec.FixtureCommandContext. Inside a "+
			"checkout, or under a hook that exports GIT_DIR, such a command can write into the "+
			"developer's own repository even when it passes `-C <dir>` or sets cmd.Dir. Build it "+
			"with gitexec.FixtureCommandContext(ctx, fixtureDir, args...). A test that only reads "+
			"the real checkout is a deliberate exception: add it to readOnlyGitReaders with its reason.")
}

// unfixturedGitCalls returns the line of every git command file builds without
// gitexec.FixtureCommandContext: an os/exec Command/CommandContext whose program
// is the literal "git", or a call to gitexec.CommandFn / gitexec.CommandContextFn.
// Import aliases are honoured for both packages.
func unfixturedGitCalls(fset *token.FileSet, file *ast.File) []int {
	execPkg := importLocalNames(file, "os/exec", "exec")
	gitexecPkg := importLocalNames(file, gitexecImportPath, "gitexec")
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		bad := false
		switch {
		case execPkg[pkg.Name]:
			nameArg := map[string]int{"Command": 0, "CommandContext": 1}
			if i, ok := nameArg[sel.Sel.Name]; ok && i < len(call.Args) {
				lit, ok := call.Args[i].(*ast.BasicLit)
				bad = ok && lit.Kind == token.STRING && lit.Value == `"git"`
			}
		case gitexecPkg[pkg.Name]:
			bad = sel.Sel.Name == "CommandFn" || sel.Sel.Name == "CommandContextFn"
		}
		if bad {
			lines = append(lines, fset.Position(call.Pos()).Line)
		}
		return true
	})
	return lines
}

// importLocalNames returns every identifier file binds to importPath: its alias,
// or def when unaliased. A file may import one path under several names.
func importLocalNames(file *ast.File, importPath, def string) map[string]bool {
	names := map[string]bool{}
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err != nil || p != importPath {
			continue
		}
		if imp.Name != nil {
			names[imp.Name.Name] = true
		} else {
			names[def] = true
		}
	}
	return names
}

// TestUnfixturedGitCalls_FlagsEveryNonFixtureShape pins what the gate catches.
// The scoped shapes matter most: `-C dir` and cmd.Dir were accepted before
// Epic 45.0, and both still wrote into the checkout under a hook.
func TestUnfixturedGitCalls_FlagsEveryNonFixtureShape(t *testing.T) {
	const src = `package p

import (
	"context"
	"os/exec"
	xexec "os/exec"

	"github.com/samestrin/atcr/internal/gitexec"
	gx "github.com/samestrin/atcr/internal/gitexec"
)

func f(ctx context.Context, dir string) {
	_ = exec.Command("git", "init")
	_ = exec.Command("git", "-C", dir, "init")
	c := exec.CommandContext(ctx, "git", "init")
	c.Dir = dir
	_ = xexec.Command("git", "status")
	_ = gitexec.CommandFn("status")
	_ = gx.CommandContextFn(ctx, "-C", dir, "log")
	_ = gitexec.FixtureCommandContext(ctx, dir, "init")
	_ = exec.Command("go", "build")
	_ = exec.CommandContext(ctx, "sh", "-c", "git status")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p_test.go", src, 0)
	require.NoError(t, err)
	assert.Equal(t, []int{13, 14, 15, 17, 18, 19}, unfixturedGitCalls(fset, file),
		"every non-fixture git construction is flagged; FixtureCommandContext and non-git programs are not")
}

// TestReadOnlyGitReaders_HasNoStaleEntries keeps the exceptions honest: an entry
// for a file that no longer exists, or no longer runs git, silently widens the
// gate and should be deleted.
func TestReadOnlyGitReaders_HasNoStaleEntries(t *testing.T) {
	assert.Empty(t, ambientGitAllowlist, "ambientGitAllowlist stays empty; see its doc comment")
	for rel := range readOnlyGitReaders {
		src, err := os.ReadFile(filepath.Join("..", rel))
		require.NoErrorf(t, err, "readOnlyGitReaders names %s, which no longer exists — remove the entry", rel)
		if !strings.Contains(string(src), `"git"`) && !strings.Contains(string(src), "CommandContextFn(") &&
			!strings.Contains(string(src), "CommandFn(") {
			t.Errorf("%s no longer runs git — remove it from readOnlyGitReaders", rel)
		}
	}
}
