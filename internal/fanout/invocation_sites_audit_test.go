package fanout

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// invocationSite names one non-test llmclient.Invocation declaration — a
// composite literal, a new(llmclient.Invocation), a var (ValueSpec) of the type,
// or an alias of it (TD-016) — by file and enclosing function, so the audit
// survives line shifts. A package-level declaration has an empty fn; a method is
// keyed with its receiver as (T).name or (*T).name, so two same-named methods in
// one file are two entries, not one.
type invocationSite struct{ file, fn string }

// siteRule is the audit's decision for one site. recv is the identifier the
// thinking keys must be read from — the site's own AgentConfig — so a site
// reading its primary's or a lane default's value fails.
type siteRule struct{ kind, recv string }

// thinkingFieldNames are the Invocation keys a declared thinking setting rides.
var thinkingFieldNames = []string{"Thinking", "ThinkingLevel", "ThinkingStyle"}

// Sprint 35.16.11.2.2 AC 04-05: the closed inventory of non-test
// llmclient.Invocation{} literals. A declared thinking setting is only as
// trustworthy as its weakest call site, so a new literal must be added here on
// purpose, with a decision about thinking.
//
//   - thinking: the four pipeline sites (review primary and fallback, skeptic,
//     debate seat) set all three keys from the agent's own AgentConfig.
//   - the two doctor probe sites set all three keys from the probed Target,
//     which carries the declaration of the agents sharing it (Story 5).
//   - control: the doctor's thinking control call, which must send NO thinking
//     key — it is the same prompt without the declaration, and a key there would
//     make it measure the declaration it exists to compare against.
//   - excluded: the verify executor (fix generation). ExecutorConfig has no
//     thinking keys — nor response_format or max_tokens — so there is no
//     declaration to forward; adding one is a registry change, not a wiring fix.
//     This is the documented exclusion, recorded here instead of in executor.go.
var invocationSiteInventory = map[invocationSite]siteRule{
	{"internal/fanout/review.go", "renderAgent"}:          {"thinking", "ac"},
	{"internal/fanout/review.go", "buildFallbackAgent"}:   {"thinking", "ac"},
	{"internal/verify/invoke.go", "buildSkepticAgent"}:    {"thinking", "c"},
	{"internal/debate/protocol.go", "buildDebateAgent"}:   {"thinking", "c"},
	{"internal/doctor/run.go", "probe"}:                   {"thinking", "tgt"},
	{"internal/doctor/run.go", "responseFormatCall"}:      {"thinking", "tgt"},
	{"internal/doctor/run.go", "thinkingControlCall"}:     {kind: "control"},
	{"internal/verify/executor.go", "callExecutor"}:       {kind: "excluded"},
	{"internal/verify/executor.go", "buildExecutorAgent"}: {kind: "excluded"},
}

// llmclientImportPath is the package whose Invocation literals are audited.
const llmclientImportPath = "github.com/samestrin/atcr/internal/llmclient"

// invocationLiteral is one llmclient.Invocation{} literal found in source.
type invocationLiteral struct {
	site invocationSite
	pos  string
	// keys maps each keyed field to its value expression.
	keys map[string]ast.Expr
}

// funcDeclName renders a FuncDecl's site key: the bare name for a plain
// function, the receiver-qualified (*T).name / (T).name for a method, so
// same-named methods on different receivers do not share an inventory entry
// (TD-016). A generic receiver falls back to the bare name.
func funcDeclName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 || fn.Name == nil {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		if id, ok := star.X.(*ast.Ident); ok {
			return "(*" + id.Name + ")." + fn.Name.Name
		}
		return fn.Name.Name
	}
	if id, ok := recv.(*ast.Ident); ok {
		return "(" + id.Name + ")." + fn.Name.Name
	}
	return fn.Name.Name
}

// isInvocationNew reports whether call is a new(llmclient.Invocation).
func isInvocationNew(call *ast.CallExpr, pkgName string, local bool) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "new" && len(call.Args) == 1 && isInvocationType(call.Args[0], pkgName, local)
}

// findInvocationLiterals parses every non-test .go file under root and returns
// each llmclient.Invocation declaration — composite literals, new(...) calls,
// and var (ValueSpec) declarations of the type or of an alias of it — with its
// enclosing function, including package-level initializers and files under an
// import alias. A dot import of llmclient fails the audit, since its
// declarations are unqualified.
func findInvocationLiterals(t *testing.T, root string) []invocationLiteral {
	t.Helper()
	var out []invocationLiteral
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		pkgName, local := llmclientName(f, rel)
		if pkgName == "." {
			return fmt.Errorf("%s dot-imports llmclient; the audit cannot see its Invocation literals", rel)
		}
		if pkgName == "" && !local {
			return nil
		}
		// TD-016: a package-level `type X = llmclient.Invocation` alias lets a
		// declaration hide behind X, so collect the file's aliases first and
		// treat an alias name as the type everywhere below.
		aliases := map[string]bool{}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if ok && ts.Assign.IsValid() && isInvocationType(ts.Type, pkgName, local) {
					aliases[ts.Name.Name] = true
				}
			}
		}
		for _, decl := range f.Decls {
			fnName := ""
			if fn, ok := decl.(*ast.FuncDecl); ok {
				fnName = funcDeclName(fn)
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				var keys map[string]ast.Expr
				var pos token.Pos
				switch node := n.(type) {
				case *ast.CompositeLit:
					if !isInvocationType(node.Type, pkgName, local) && !aliases[identName(node.Type)] {
						return true
					}
					keys = map[string]ast.Expr{}
					for _, el := range node.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if id, ok := kv.Key.(*ast.Ident); ok {
								keys[id.Name] = kv.Value
							}
						}
					}
					pos = node.Pos()
				case *ast.CallExpr:
					if !isInvocationNew(node, pkgName, local) {
						return true
					}
					pos = node.Pos()
				case *ast.ValueSpec:
					if node.Type == nil || (!isInvocationType(node.Type, pkgName, local) && !aliases[identName(node.Type)]) {
						return true
					}
					pos = node.Pos()
				default:
					return true
				}
				out = append(out, invocationLiteral{
					site: invocationSite{rel, fnName},
					pos:  fset.Position(pos).String(),
					keys: keys,
				})
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// llmclientName returns the name f refers to llmclient by ("" when it does
// not import it), and whether f is itself in package llmclient.
func llmclientName(f *ast.File, rel string) (name string, local bool) {
	local = path.Dir(rel) == "internal/llmclient"
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != llmclientImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name, local
		}
		return "llmclient", local
	}
	return "", local
}

// identName returns e's bare identifier name, or "" when e is not one.
func identName(e ast.Expr) string {
	id, ok := e.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}

func isInvocationType(e ast.Expr, pkgName string, local bool) bool {
	if id, ok := e.(*ast.Ident); ok {
		return local && id.Name == "Invocation"
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Invocation" {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && pkgName != "" && x.Name == pkgName
}

func TestInvocationSites_ThinkingAudit(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "the audit must walk the repository root")

	lits := findInvocationLiterals(t, root)
	seen := map[invocationSite]bool{}
	for _, lit := range lits {
		rule, ok := invocationSiteInventory[lit.site]
		if !assert.True(t, ok, "unexpected llmclient.Invocation{} literal at %s (%s), not in the inventory — decide its thinking wiring and add it", lit.pos, lit.site.fn) {
			continue
		}
		seen[lit.site] = true
		switch rule.kind {
		case "thinking":
			for _, name := range thinkingFieldNames {
				sel, ok := lit.keys[name].(*ast.SelectorExpr)
				if !assert.True(t, ok, "%s (%s) must set %s from its own AgentConfig", lit.pos, lit.site.fn, name) {
					continue
				}
				assert.Equal(t, name, sel.Sel.Name, "%s (%s): %s must read the same-named AgentConfig field", lit.pos, lit.site.fn, name)
				recv, _ := sel.X.(*ast.Ident)
				assert.True(t, recv != nil && recv.Name == rule.recv,
					"%s (%s): %s must be read from %s (the site's own AgentConfig), not a primary or lane default",
					lit.pos, lit.site.fn, name, rule.recv)
			}
		case "control":
			for _, name := range thinkingFieldNames {
				assert.NotContains(t, lit.keys, name, "%s (%s) must not set %s — the control call is the probe without the declaration", lit.pos, lit.site.fn, name)
			}
		case "excluded":
			for _, name := range thinkingFieldNames {
				assert.NotContains(t, lit.keys, name, "%s (%s) must not set %s — the fix-generation lane is out of scope", lit.pos, lit.site.fn, name)
			}
		}
	}

	var missing []string
	for site := range invocationSiteInventory {
		if !seen[site] {
			missing = append(missing, site.file+":"+site.fn)
		}
	}
	sort.Strings(missing)
	assert.Empty(t, missing, "inventoried llmclient.Invocation{} sites no longer found — update the inventory")
}

// underauditSrc is a synthetic non-test file exercising the declaration forms
// TD-016 says the audit must catch beyond composite literals: new(llmclient.Invocation),
// a var (ValueSpec) of the type, and a package-level alias of it.
const underauditSrc = `package underaudit

import "github.com/samestrin/atcr/internal/llmclient"

type invAlias = llmclient.Invocation

func viaNew() *llmclient.Invocation {
	return new(llmclient.Invocation)
}

func viaVar() {
	var zero llmclient.Invocation
	_ = zero
}

func viaAlias() {
	a := invAlias{Model: "m"}
	_ = a
}
`

func findUnderauditSites(t *testing.T, src string) map[string]bool {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "underaudit.go"), []byte(src), 0o600))
	sites := map[string]bool{}
	for _, lit := range findInvocationLiterals(t, dir) {
		sites[lit.site.fn] = true
	}
	return sites
}

// TestInvocationAudit_CatchesNonLiteralDeclarations pins half 1 of TD-016: the
// audit must see new(llmclient.Invocation), a var of the type, and an alias of
// it — not just llmclient.Invocation{} literals — so a site cannot escape the
// inventory by switching declaration form.
func TestInvocationAudit_CatchesNonLiteralDeclarations(t *testing.T) {
	sites := findUnderauditSites(t, underauditSrc)
	for _, fn := range []string{"viaNew", "viaVar", "viaAlias"} {
		assert.True(t, sites[fn], "the audit must flag the Invocation declared in %s (TD-016: ValueSpec/new/alias forms escape it)", fn)
	}
}

// TestInvocationAudit_KeysMethodsByReceiver pins half 2 of TD-016: a method's
// site key carries its receiver as (*T).name (or (T).name), so two same-named
// methods in one file are two inventory entries, not one.
func TestInvocationAudit_KeysMethodsByReceiver(t *testing.T) {
	src := `package underaudit

import "github.com/samestrin/atcr/internal/llmclient"

type alpha struct{}
type beta struct{}

func (alpha) probe() { _ = llmclient.Invocation{} }
func (b *beta) probe() { _ = llmclient.Invocation{Model: "m"} }
`
	sites := findUnderauditSites(t, src)
	assert.True(t, sites["(alpha).probe"], "value-receiver method must be keyed as (T).name; got sites %v", sites)
	assert.True(t, sites["(*beta).probe"], "pointer-receiver method must be keyed as (*T).name; got sites %v", sites)
}
