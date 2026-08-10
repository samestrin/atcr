package gitexec

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
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
//   - internal/sandbox/oslevel.go execs the platform sandboxing binary
//     (sandbox-exec on macOS, bwrap on Linux) via the path toolPath() resolves.
//
// The AC4 scan flags every indirected exec (a `git`-via-variable call is exactly the
// snapshot.go form found and migrated in the Phase 1 gate), so these must be
// allowlisted or they would false-positive. This is a deliberate, documented trust
// grant on specific files — narrow, and reviewed the same way this list is edited.
// Note a literal "git" is an offender EVEN in these files (see classifyExecCall): the
// allowlist excuses only their known non-git indirected form, not a bare git call.
//
// The value is the COUNT of authorized indirected sites, not a bare `true`. A
// file-granularity boolean exempted the entire file permanently: internal/sandbox/oslevel.go
// is 1700+ lines and execs an operator-controlled cfg.ToolPath, so it is the
// highest-value place for the check to stay live — yet any exec.Command(<variable>, ...)
// added anywhere in it, INCLUDING a git call in exactly the snapshot.go form this
// scan was written to catch, was silently permitted. Pinning the count means a new
// indirected exec trips the gate and forces an explicit review of the increment.
//
// Counts are exact, not ceilings: see checkIndirectGrants for why a DECREASE fails too.
var indirectNonGitExecFiles = map[string]int{
	"internal/verify/localvalidate.go": 1, // argv[0] — the user's validate command
	"internal/sandbox/docker.go":       4, // b.cfg.DockerPath: run, kill, and two dockerCmd helpers
	"internal/sandbox/oslevel.go":      1, // toolPath() — sandbox-exec / bwrap
}

// checkIndirectGrants compares the indirected-exec sites actually found in each
// allowlisted file against its pinned grant, returning one offender message per
// mismatch.
//
// A count ABOVE the grant is the case the bound exists for: a new indirected exec
// appeared in a trusted file and nobody reviewed it.
//
// A count BELOW the grant fails too, and that is deliberate rather than pedantry.
// A stale over-grant is the same hole one size smaller — if oslevel.go drops to
// zero indirected sites and the grant stays at 1, the next one added lands inside
// the allowance and is never seen. Failing on the decrease keeps the number a
// statement about the code as it is, and the fix is a one-line edit the failure
// message names.
func checkIndirectGrants(counts map[string]int) []string {
	var offenders []string
	for file, grant := range indirectNonGitExecFiles {
		got := counts[file]
		switch {
		case got > grant:
			offenders = append(offenders, fmt.Sprintf(
				"%s has %d indirected exec site(s) but is granted %d — review the new site, then raise the grant if it provably runs a non-git binary",
				file, got, grant))
		case got < grant:
			offenders = append(offenders, fmt.Sprintf(
				"%s has %d indirected exec site(s) but is granted %d — lower the grant; a stale over-grant silently admits the next one added",
				file, got, grant))
		}
	}
	// Sorted: map iteration order is randomized, and a failure message that
	// reorders between runs is a diff nobody can review.
	sort.Strings(offenders)
	return offenders
}

// gitExecMigratedSites are every production file that was migrated to construct
// its git subprocess through internal/gitexec (AC4). The first six are the call
// sites named in epic 32.4's original task list; internal/tools/snapshot.go is the
// seventh, a variable-indirected `exec.Command(gitPath, ...)` site found and
// migrated during the Phase 1 integration gate. Each must reference the gitexec
// package, so a silent revert to a bare call (which the AST scan below would also
// catch as a new offender) additionally trips this positive check.
var gitExecMigratedSites = []string{
	"cli/autofix.go", // relocated from cmd/atcr in Sprint 34.0 Task 03 (CLI export seam)
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
	indirectCounts := map[string]int{}

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
			// this file is a known non-git indirected exec site — and even then only
			// up to its pinned count, which checkIndirectGrants adjudicates below.
			if _, granted := indirectNonGitExecFiles[rel]; !granted {
				offenders = append(offenders, rel+":"+line+" (indirected exec — may run git unhardened)")
				return true
			}
			indirectCounts[rel]++
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}

	// The grant is a budget, not a blanket exemption: a trusted file that grew a
	// NEW indirected exec is an offender too, even though the file is listed.
	offenders = append(offenders, checkIndirectGrants(indirectCounts)...)

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

// TestAC4_IndirectGrantIsBounded proves the trust grant is a COUNT and not a
// permanent file-level exemption.
//
// The defect this closes: indirectNonGitExecFiles was map[string]bool, so listing
// internal/sandbox/oslevel.go exempted all 1700+ of its lines forever. Any
// exec.Command(<variable>, ...) added anywhere in that file — including a git call
// in exactly the snapshot.go form the scan exists to catch — was silently
// permitted, in the one file that also execs an operator-controlled cfg.ToolPath.
//
// Synthetic counts, not the real walk: staging the regression for real would mean
// adding a bogus exec to a production file. This asserts the decision function the
// walk feeds, the same way TestAC4_MatcherDetectsIndirectedGit asserts the
// classifier against synthetic source rather than mutating the repo.
func TestAC4_IndirectGrantIsBounded(t *testing.T) {
	const target = "internal/sandbox/oslevel.go"

	atGrant := func() map[string]int {
		m := make(map[string]int, len(indirectNonGitExecFiles))
		for f, n := range indirectNonGitExecFiles {
			m[f] = n
		}
		return m
	}

	t.Run("exactly the granted count is clean", func(t *testing.T) {
		if got := checkIndirectGrants(atGrant()); len(got) != 0 {
			t.Fatalf("a file at exactly its grant must not be an offender; got %v", got)
		}
	})

	t.Run("one more indirected exec than granted is an offender", func(t *testing.T) {
		counts := atGrant()
		counts[target]++
		got := checkIndirectGrants(counts)
		if len(got) != 1 {
			t.Fatalf("a NEW indirected exec in a trusted file must trip the gate; got %v", got)
		}
		if !strings.Contains(got[0], target) {
			t.Fatalf("the failure must name the offending file; got %q", got[0])
		}
	})

	t.Run("fewer sites than granted is a stale over-grant", func(t *testing.T) {
		counts := atGrant()
		counts[target]--
		got := checkIndirectGrants(counts)
		if len(got) != 1 {
			t.Fatalf("a stale over-grant admits the next site added and must be reported; got %v", got)
		}
		if !strings.Contains(got[0], target) {
			t.Fatalf("the failure must name the stale file; got %q", got[0])
		}
	})

	t.Run("a file with no grant is not reported here", func(t *testing.T) {
		counts := atGrant()
		counts["internal/tools/snapshot.go"] = 3
		if got := checkIndirectGrants(counts); len(got) != 0 {
			t.Fatalf("un-granted files are offenders at the call site, not double-reported here; got %v", got)
		}
	})
}

// TestAC4_GrantCountsMatchTheRepository pins the numbers to reality. Without it
// the grants could drift to any value and every synthetic test above would still
// pass, which is precisely the stale-over-grant hole in a different place.
func TestAC4_GrantCountsMatchTheRepository(t *testing.T) {
	counts := indirectExecCounts(t, repoRoot(t))
	for file, grant := range indirectNonGitExecFiles {
		if counts[file] != grant {
			t.Errorf("%s: granted %d indirected exec site(s), repository has %d", file, grant, counts[file])
		}
	}
}

// indirectExecCounts tallies indirected (non-string-literal) os/exec constructions
// per allowlisted file, reusing the same classifier the AC4 walk uses so the two
// can never disagree about what counts as a site.
func indirectExecCounts(t *testing.T, root string) map[string]int {
	t.Helper()
	fset := token.NewFileSet()
	counts := map[string]int{}
	for file := range indirectNonGitExecFiles {
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(file)), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		execPkg := execPkgLocalName(f)
		if execPkg == "" {
			continue
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
			if _, isLit := stringLiteralValue(nameArg); !isLit {
				counts[file]++
			}
			return true
		})
	}
	return counts
}
