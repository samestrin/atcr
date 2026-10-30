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
	indirectCounts := map[string]int{}

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
		for _, c := range unfixturedGitCalls(fset, file) {
			if c.indirect && indirectExecTests[rel] > 0 {
				indirectCounts[rel]++
				continue
			}
			offenders = append(offenders, rel+":"+strconv.Itoa(c.line))
		}
		return nil
	})
	require.NoError(t, err)
	offenders = append(offenders, checkIndirectTestGrants(indirectCounts)...)

	sort.Strings(offenders)
	assert.Empty(t, offenders,
		"these tests build a git command without gitexec.FixtureCommandContext. Inside a "+
			"checkout, or under a hook that exports GIT_DIR, such a command can write into the "+
			"developer's own repository even when it passes `-C <dir>` or sets cmd.Dir. Build it "+
			"with gitexec.FixtureCommandContext(ctx, fixtureDir, args...). A test that only reads "+
			"the real checkout is a deliberate exception: add it to readOnlyGitReaders with its reason. "+
			"An exec call whose program is a variable that provably is not git is counted in indirectExecTests.")
}

// gitCall is one git command construction unfixturedGitCalls found. indirect
// marks an os/exec call whose program is not a string literal: it may be git
// (`exec.Command(gitBin, ...)`) or may not, so the gate weighs it against
// indirectExecTests while the staleness guard does not count it as git.
type gitCall struct {
	line     int
	indirect bool
}

// unfixturedGitCalls returns every git command file builds without
// gitexec.FixtureCommandContext: an os/exec Command/CommandContext whose program
// is the literal "git" or is not a literal at all (indirect), or a call to
// gitexec.CommandFn / gitexec.CommandContextFn, unqualified when file is package
// gitexec itself or dot-imports it. Import aliases are honoured for both packages.
// A program of os.Args[0] is the test binary re-executing itself, never git.
func unfixturedGitCalls(fset *token.FileSet, file *ast.File) []gitCall {
	execPkg := importLocalNames(file, "os/exec", "exec")
	gitexecPkg := importLocalNames(file, gitexecImportPath, "gitexec")
	unqualified := file.Name.Name == "gitexec" || gitexecPkg["."]
	isConstructor := func(name string) bool { return name == "CommandFn" || name == "CommandContextFn" }
	var calls []gitCall
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		bad, indirect := false, false
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			bad = unqualified && isConstructor(fn.Name)
		case *ast.SelectorExpr:
			pkg, ok := fn.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case execPkg[pkg.Name]:
				nameArg := map[string]int{"Command": 0, "CommandContext": 1}
				if i, ok := nameArg[fn.Sel.Name]; ok && i < len(call.Args) {
					if lit, ok := call.Args[i].(*ast.BasicLit); ok {
						bad = lit.Kind == token.STRING && lit.Value == `"git"`
					} else {
						bad, indirect = !isOSArgs0(call.Args[i]), true
					}
				}
			case gitexecPkg[pkg.Name]:
				bad = isConstructor(fn.Sel.Name)
			}
		}
		if bad {
			calls = append(calls, gitCall{line: fset.Position(call.Pos()).Line, indirect: indirect})
		}
		return true
	})
	return calls
}

// isOSArgs0 reports whether expr is os.Args[0], the re-exec-the-test-binary idiom.
func isOSArgs0(expr ast.Expr) bool {
	idx, ok := expr.(*ast.IndexExpr)
	if !ok {
		return false
	}
	lit, ok := idx.Index.(*ast.BasicLit)
	if !ok || lit.Value != "0" {
		return false
	}
	sel, ok := idx.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Args" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "os"
}

// indirectExecTests are the test files whose os/exec calls take a non-literal
// program that provably is not git, with the exact count of such calls. As with
// the production gate's indirectNonGitExecFiles, the count is exact: a new
// indirect call, or a removed one, fails until the grant is edited to match.
var indirectExecTests = map[string]int{
	"cli/install_script_source_test.go":                          2, // bash from exec.LookPath("bash")
	"cli/install_script_test.go":                                 2, // bash from exec.LookPath, and the built atcr binary
	"internal/sandbox/docker_test.go":                            1, // bb from exec.LookPath("busybox")
	"internal/sandbox/oslevel_profile_linux_integration_test.go": 1, // goBin from exec.LookPath("go")
	"internal/sandbox/oslevel_test.go":                           1, // probe, a script written to t.TempDir()
	"internal/sandbox/oslevel_unix_test.go":                      8, // writeFakeOSLevel scripts, missing paths, sandboxLimitShell (/bin/sh)
}

// checkIndirectTestGrants returns one offender per indirectExecTests entry whose
// count differs from the indirect calls the walk found in that file.
func checkIndirectTestGrants(counts map[string]int) []string {
	var offenders []string
	for rel, grant := range indirectExecTests {
		if got := counts[rel]; got != grant {
			offenders = append(offenders, rel+": "+strconv.Itoa(got)+" indirect exec call(s), granted "+
				strconv.Itoa(grant)+" in indirectExecTests")
		}
	}
	return offenders
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
// Epic 45.0, and both still wrote into the checkout under a hook. The indirect
// shapes matter next: `exec.Command(gitBin, ...)` runs git as surely as a
// literal does, and a literal-only check never saw it.
func TestUnfixturedGitCalls_FlagsEveryNonFixtureShape(t *testing.T) {
	const src = `package p

import (
	"context"
	"os"
	"os/exec"
	xexec "os/exec"

	"github.com/samestrin/atcr/internal/gitexec"
	gx "github.com/samestrin/atcr/internal/gitexec"
)

func f(ctx context.Context, dir, gitBin string) {
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
	_ = exec.Command(gitBin, "init")
	_ = xexec.CommandContext(ctx, gitBin, "-C", dir, "log")
	_ = exec.Command(os.Args[0], "-test.run=^TestHelper$")
	_ = CommandFn("status")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p_test.go", src, 0)
	require.NoError(t, err)
	assert.Equal(t, []gitCall{{line: 14}, {line: 15}, {line: 16}, {line: 18}, {line: 19}, {line: 20},
		{line: 24, indirect: true}, {line: 25, indirect: true}},
		unfixturedGitCalls(fset, file),
		"every non-fixture git construction is flagged, a non-literal program as indirect; FixtureCommandContext, "+
			"non-git programs, os.Args[0] and an unqualified CommandFn outside package gitexec are not")
}

// TestUnfixturedGitCalls_FlagsUnqualifiedConstructorsInGitexec pins the shape
// gitexec's own tests use: inside package gitexec, or under a dot import, the
// constructors are called unqualified, and the gate (and so the staleness
// guard) must still see them.
func TestUnfixturedGitCalls_FlagsUnqualifiedConstructorsInGitexec(t *testing.T) {
	for _, src := range []string{
		"package gitexec\n\nimport \"context\"\n\nfunc f(ctx context.Context) {\n" +
			"\t_ = CommandFn(\"status\")\n\t_ = CommandContextFn(ctx, \"log\")\n\torig := CommandFn\n\t_ = orig\n}\n",
		"package p\n\nimport (\n\t\"context\"\n\n\t. \"" + gitexecImportPath + "\"\n)\n\nfunc f(ctx context.Context) {\n" +
			"\t_ = CommandFn(\"status\")\n\t_ = CommandContextFn(ctx, \"log\")\n}\n",
	} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "p_test.go", src, 0)
		require.NoError(t, err)
		calls := unfixturedGitCalls(fset, file)
		require.Len(t, calls, 2, "both unqualified constructor calls are flagged, and a bare reference is not:\n%s", src)
		assert.False(t, calls[0].indirect || calls[1].indirect)
	}
}

// readerRunsGit reports whether the test file at path builds a git command the
// gate would flag: the same AST classifier, not a substring search, so a "git"
// in a comment or an unrelated string keeps no entry alive. An indirect call is
// not counted, because it may run something other than git.
func readerRunsGit(t *testing.T, path string, src any) bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, 0)
	require.NoErrorf(t, err, "parse %s", path)
	for _, c := range unfixturedGitCalls(fset, file) {
		if !c.indirect {
			return true
		}
	}
	return false
}

// TestReaderRunsGit_IgnoresMentionsOfGit pins why the staleness guard parses
// rather than searches: this file mentions "git", CommandFn( and
// CommandContextFn( and runs none of them, which the old substring check took
// for a live git reader.
func TestReaderRunsGit_IgnoresMentionsOfGit(t *testing.T) {
	const stale = `package p

import "os/exec"

// Once ran exec.Command("git", "log") and gitexec.CommandFn("status").
var note = "git CommandFn( CommandContextFn("

func f(bin string) {
	_ = exec.Command("go", "build")
	_ = exec.Command(bin, "version")
}
`
	assert.False(t, readerRunsGit(t, "stale_test.go", stale), "a file that only mentions git is stale")
	assert.True(t, readerRunsGit(t, "live_test.go",
		"package p\n\nimport \"os/exec\"\n\nfunc f() { _ = exec.Command(\"git\", \"log\") }\n"))
}

// TestReadOnlyGitReaders_HasNoStaleEntries keeps the exceptions honest: an entry
// for a file that no longer exists, or no longer runs git, silently widens the
// gate and should be deleted.
func TestReadOnlyGitReaders_HasNoStaleEntries(t *testing.T) {
	assert.Empty(t, ambientGitAllowlist, "ambientGitAllowlist stays empty; see its doc comment")
	for rel := range readOnlyGitReaders {
		path := filepath.Join("..", rel)
		_, err := os.Stat(path)
		require.NoErrorf(t, err, "readOnlyGitReaders names %s, which no longer exists — remove the entry", rel)
		if !readerRunsGit(t, path, nil) {
			t.Errorf("%s no longer runs git — remove it from readOnlyGitReaders", rel)
		}
	}
}
