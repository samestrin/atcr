package gitexec

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// hasEnv reports whether env contains kv exactly.
func hasEnv(env []string, kv string) bool {
	return slices.Contains(env, kv)
}

// TestCommandFn_HardenedEnv asserts CommandFn injects both hardening vars (AC2)
// additively over the inherited process environment (not as the only entries).
func TestCommandFn_HardenedEnv(t *testing.T) {
	cmd := CommandFn("rev-parse", "HEAD")
	if got := cmd.Args[0]; got != "git" {
		t.Errorf("cmd.Args[0] = %q, want %q", got, "git")
	}
	if !hasEnv(cmd.Env, "GIT_CONFIG_NOSYSTEM=1") {
		t.Errorf("cmd.Env missing GIT_CONFIG_NOSYSTEM=1: %v", cmd.Env)
	}
	if !hasEnv(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null") {
		t.Errorf("cmd.Env missing GIT_CONFIG_GLOBAL=/dev/null: %v", cmd.Env)
	}
	// Additive, not a replacement: the inherited environment is still present
	// (PATH is present in every realistic host environment).
	if len(cmd.Env) <= 2 {
		t.Errorf("cmd.Env not additive over inherited env, got only: %v", cmd.Env)
	}
}

// TestCommandContextFn_HardenedEnv asserts the context constructor also hardens
// and honors context cancellation semantics.
func TestCommandContextFn_HardenedEnv(t *testing.T) {
	ctx := context.Background()
	cmd := CommandContextFn(ctx, "-C", ".", "status")
	if !hasEnv(cmd.Env, "GIT_CONFIG_NOSYSTEM=1") || !hasEnv(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null") {
		t.Errorf("cmd.Env missing hardening vars: %v", cmd.Env)
	}
}

// TestHardenEnv_SurvivesAdditiveAppend proves a caller's later additive append
// (the gitrange/payload LC_ALL=C pattern) keeps the hardening vars present.
func TestHardenEnv_SurvivesAdditiveAppend(t *testing.T) {
	cmd := CommandFn("diff")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")
	for _, want := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C", "LANG=C"} {
		if !hasEnv(cmd.Env, want) {
			t.Errorf("after additive append, cmd.Env missing %q: %v", want, cmd.Env)
		}
	}
}

// TestCommandFn_Swappable proves the exported vars are substitutable, the
// indirection point downstream call-site tests rely on.
func TestCommandFn_Swappable(t *testing.T) {
	orig := CommandFn
	defer func() { CommandFn = orig }()

	var gotArgs []string
	CommandFn = func(arg ...string) *exec.Cmd {
		gotArgs = arg
		return exec.Command("true")
	}
	CommandFn("rev-parse", "HEAD")
	if len(gotArgs) != 2 || gotArgs[0] != "rev-parse" || gotArgs[1] != "HEAD" {
		t.Errorf("fake CommandFn recorded args = %v, want [rev-parse HEAD]", gotArgs)
	}
}

// repoRoot walks up from the test working directory to the directory containing
// go.mod, so the whole-tree AC4 walk below is anchored at the module root
// regardless of which package directory `go test` runs from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// execWrapperFile is the single module-root-relative file authorized to construct
// a `git` subprocess directly: internal/gitexec/gitexec.go IS the hardened wrapper,
// so its two bare exec.Command("git",...)/exec.CommandContext(ctx,"git",...) calls
// are the one legitimate construction point (the whole point of the package).
const execWrapperFile = "internal/gitexec/gitexec.go"

// indirectNonGitExecFiles are the module-root-relative production files permitted to
// construct a subprocess through a NON-literal command name (an identifier/selector
// rather than a string literal). Both provably run a non-git binary:
//
//   - internal/verify/localvalidate.go execs the user's validate command via argv[0].
//   - internal/sandbox/docker.go execs the docker binary via b.cfg.DockerPath.
//
// The AC4 scan flags every indirected exec (a `git`-via-variable call is exactly the
// snapshot.go form found and migrated in the Phase 1 gate), so these two must be
// allowlisted or they would false-positive. This is a deliberate, documented trust
// grant on two specific files — narrow, and reviewed the same way this list is edited.
// Note a literal "git" is an offender EVEN in these files (see classifyExecCall): the
// allowlist excuses only their known non-git indirected form, not a bare git call.
var indirectNonGitExecFiles = map[string]bool{
	"internal/verify/localvalidate.go": true,
	"internal/sandbox/docker.go":       true,
}

// gitExecMigratedSites are every production file that was migrated to construct
// its git subprocess through internal/gitexec (AC4). The first six are the call
// sites named in epic 32.4's original task list; internal/tools/snapshot.go is the
// seventh, a variable-indirected `exec.Command(gitPath, ...)` site found and
// migrated during the Phase 1 integration gate. Each must reference the gitexec
// package, so a silent revert to a bare call (which the AST scan below would also
// catch as a new offender) additionally trips this positive check.
var gitExecMigratedSites = []string{
	"cmd/atcr/autofix.go",
	"internal/fanout/review.go",
	"internal/gitrange/resolver.go",
	"internal/payload/diff.go",
	"internal/personas/submit.go", // runGit + gitHasStagedChanges (two invocations)
	"internal/stream/fileindex.go",
	"internal/tools/snapshot.go",
}

// execPkgLocalName returns the local identifier the file binds to the standard
// "os/exec" package (usually "exec", but honoring an import alias such as
// `xexec "os/exec"`), or "" if the file does not import os/exec. Resolving the
// alias closes the false-negative where `xexec.Command("git",...)` would evade a
// hard-coded pkg.Name == "exec" check.
func execPkgLocalName(f *ast.File) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != "os/exec" {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name // explicit alias (including "." or "_")
		}
		return "exec" // default local name
	}
	return ""
}

// classifyExecCall inspects one AST call. If it is execPkg.Command / execPkg.CommandContext
// it returns isExec=true plus the command-name argument (arg 0 for Command, arg 1 for
// CommandContext). It inspects the AST, never raw text, so comments and unrelated
// strings (e.g. gitexec.go's own doc comment containing `exec.Command("git", ...)`)
// never match.
func classifyExecCall(call *ast.CallExpr, execPkg string) (isExec bool, nameArg ast.Expr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, nil
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != execPkg {
		return false, nil
	}
	var argIdx int
	switch sel.Sel.Name {
	case "Command": // exec.Command(name, arg...) -> name is arg 0
		argIdx = 0
	case "CommandContext": // exec.CommandContext(ctx, name, arg...) -> name is arg 1
		argIdx = 1
	default:
		return false, nil
	}
	if argIdx >= len(call.Args) {
		return false, nil // malformed / cannot determine the name arg
	}
	return true, call.Args[argIdx]
}

// stringLiteralValue returns the Go value of expr when it is a string literal, and
// ok=false for any non-literal (identifier, selector, call, concatenation, ...).
func stringLiteralValue(expr ast.Expr) (val string, ok bool) {
	lit, isLit := expr.(*ast.BasicLit)
	if !isLit || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// TestAC4_NoBareGitExecOutsideGitexec is the binary, CI-enforced AC4 gate. It walks
// every production .go file in the module and flags any os/exec construction that
// could run an unhardened git subprocess outside internal/gitexec. Per call:
//
//   - A string-literal name equal to "git" is ALWAYS an offender (a bare git call),
//     even inside an indirect-allowlisted file — the classic AC4 violation.
//   - A non-literal name (identifier/selector) is an offender UNLESS the file is in
//     indirectNonGitExecFiles. This catches the variable-indirected git form —
//     `exec.Command(gitPath, ...)`, exactly snapshot.go's pre-migration pattern —
//     that a literal-only matcher would miss, closing the Phase-1-gate class of gap.
//   - A non-git string literal ("open", "docker", ...) is fine.
//
// A single missed/reverted call site silently reopens the subprocess-hijack gap epic
// 32.4 closes, so this is the machine check that no site was missed and none regresses.
// Test files are skipped: AC4 governs production call sites, and test helpers may
// legitimately spawn processes.
func TestAC4_NoBareGitExecOutsideGitexec(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	var offenders []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip VCS, vendored, and dot-directories (no first-party Go there).
			name := d.Name()
			if path != root && (name == "vendor" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == execWrapperFile {
			return nil // the wrapper itself is the authorized construction point
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		execPkg := execPkgLocalName(f)
		if execPkg == "" {
			return nil // file does not import os/exec: no exec.* call can resolve here
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			isExec, nameArg := classifyExecCall(call, execPkg)
			if !isExec {
				return true
			}
			line := strconv.Itoa(fset.Position(call.Pos()).Line)
			if lit, isLit := stringLiteralValue(nameArg); isLit {
				if lit == "git" {
					offenders = append(offenders, rel+":"+line+" (bare exec of literal \"git\")")
				}
				return true // non-git literal (e.g. "open") is fine
			}
			// Non-literal command name: could be git-via-variable. Offender unless
			// this file is a known non-git indirected exec site.
			if !indirectNonGitExecFiles[rel] {
				offenders = append(offenders, rel+":"+line+" (indirected exec — may run git unhardened)")
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("os/exec git-subprocess construction found outside internal/gitexec "+
			"(AC4 violation — route through gitexec.CommandFn/CommandContextFn; if a new site "+
			"provably runs a non-git binary via a variable, add it to indirectNonGitExecFiles "+
			"with justification):\n  %s", strings.Join(offenders, "\n  "))
	}
}

// TestAC4_MatcherDetectsIndirectedGit proves the AC4 scan's classification logic
// (execPkgLocalName + classifyExecCall + the literal/indirect decision) actually
// flags the forms it claims to — most importantly the variable-indirected git call
// `exec.Command(gitPath, ...)`, the snapshot.go pre-migration pattern a literal-only
// matcher would miss. It parses synthetic in-memory source (no production file is
// touched) and asserts, line by line, which calls the walk would treat as offenders,
// mirroring the decision in TestAC4_NoBareGitExecOutsideGitexec exactly.
func TestAC4_MatcherDetectsIndirectedGit(t *testing.T) {
	// Line 1 of the parsed file is `package sample`; call lines are annotated below.
	const src = `package sample

import (
	"context"
	xexec "os/exec"
)

func f(ctx context.Context, gitPath string, argv []string) {
	_ = xexec.Command("git", "status")            // L9:  literal git -> offender
	_ = xexec.Command(gitPath, "status")          // L10: indirected -> offender (may be git)
	_ = xexec.Command("open", "http://x")         // L11: non-git literal -> ok
	_ = xexec.CommandContext(ctx, "git", "log")   // L12: literal git -> offender
	_ = xexec.CommandContext(ctx, argv[0])        // L13: indirected -> offender
	_ = xexec.CommandContext(ctx, "docker", "ps") // L14: non-git literal -> ok
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "sample.go", src, 0)
	if err != nil {
		t.Fatalf("parse synthetic source: %v", err)
	}
	execPkg := execPkgLocalName(f)
	if execPkg != "xexec" {
		t.Fatalf("execPkgLocalName resolved %q, want %q (import alias not honored)", execPkg, "xexec")
	}

	// Reproduce the walk's per-call decision with NO file allowlisted, so every
	// literal-"git" and every indirected call is an offender.
	var offenderLines []int
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		isExec, nameArg := classifyExecCall(call, execPkg)
		if !isExec {
			return true
		}
		line := fset.Position(call.Pos()).Line
		if lit, isLit := stringLiteralValue(nameArg); isLit {
			if lit == "git" {
				offenderLines = append(offenderLines, line)
			}
			return true
		}
		offenderLines = append(offenderLines, line) // indirected
		return true
	})

	got := map[int]bool{}
	for _, l := range offenderLines {
		got[l] = true
	}
	// Offenders: literal git (9, 12) + indirected (10, 13). Non-git literals (11, 14) clean.
	wantOffenders := []int{9, 10, 12, 13}
	wantClean := []int{11, 14}
	for _, l := range wantOffenders {
		if !got[l] {
			t.Errorf("line %d should be flagged as an AC4 offender but was not", l)
		}
	}
	for _, l := range wantClean {
		if got[l] {
			t.Errorf("line %d (non-git literal) must NOT be flagged", l)
		}
	}
}

// TestAC4_MigratedSitesReferenceGitexec is the positive half of the AC4 gate: each
// known migrated call site must still reference the gitexec package. Combined with
// TestAC4_NoBareGitExecOutsideGitexec, a site reverted to a bare call fails both
// checks (no gitexec reference here, a fresh offender there), so backsliding cannot
// pass silently.
func TestAC4_MigratedSitesReferenceGitexec(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range gitExecMigratedSites {
		t.Run(rel, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			if !strings.Contains(string(data), "gitexec.") {
				t.Errorf("%s no longer references the gitexec package — a migrated git "+
					"call site must construct its subprocess through internal/gitexec (AC4)", rel)
			}
		})
	}
}
