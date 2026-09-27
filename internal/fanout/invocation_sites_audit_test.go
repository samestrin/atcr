package fanout

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// invocationSite names one non-test llmclient.Invocation{} literal by file and
// enclosing function, so the audit survives line shifts.
type invocationSite struct{ file, fn string }

// thinkingFieldNames are the Invocation keys a declared thinking setting rides.
var thinkingFieldNames = []string{"Thinking", "ThinkingLevel", "ThinkingStyle"}

// Sprint 35.16.11.2.2 AC 04-05: the closed inventory of non-test
// llmclient.Invocation{} literals. A declared thinking setting is only as
// trustworthy as its weakest call site, so a new literal must be added here on
// purpose, with a decision about thinking.
//
//   - thinking: the four pipeline sites (review primary and fallback, skeptic,
//     debate seat) set all three keys from the agent's own AgentConfig.
//   - doctor: the two probe sites. Their thinking wiring is Phase 4 (Story 5),
//     which extends this map; until then they are inventoried, not asserted.
//   - excluded: the verify executor (fix generation). ExecutorConfig has no
//     thinking keys — nor response_format or max_tokens — so there is no
//     declaration to forward; adding one is a registry change, not a wiring fix.
//     This is the documented exclusion, recorded here instead of in executor.go.
var invocationSiteInventory = map[invocationSite]string{
	{"internal/fanout/review.go", "renderAgent"}:          "thinking",
	{"internal/fanout/review.go", "buildFallbackAgent"}:   "thinking",
	{"internal/verify/invoke.go", "buildSkepticAgent"}:    "thinking",
	{"internal/debate/protocol.go", "buildDebateAgent"}:   "thinking",
	{"internal/doctor/run.go", "probe"}:                   "doctor",
	{"internal/doctor/run.go", "responseFormatCall"}:      "doctor",
	{"internal/verify/executor.go", "callExecutor"}:       "excluded",
	{"internal/verify/executor.go", "buildExecutorAgent"}: "excluded",
}

// invocationLiteral is one llmclient.Invocation{} literal found in source.
type invocationLiteral struct {
	site invocationSite
	pos  string
	// keys maps each keyed field to its value expression.
	keys map[string]ast.Expr
}

// findInvocationLiterals parses every non-test .go file under root and returns
// each llmclient.Invocation{} composite literal with its enclosing function.
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
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isInvocationType(lit.Type) {
					return true
				}
				keys := map[string]ast.Expr{}
				for _, el := range lit.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok {
							keys[id.Name] = kv.Value
						}
					}
				}
				out = append(out, invocationLiteral{
					site: invocationSite{rel, fn.Name.Name},
					pos:  fset.Position(lit.Pos()).String(),
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

func isInvocationType(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Invocation" {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "llmclient"
}

func TestInvocationSites_ThinkingAudit(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "go.mod"))
	require.NoError(t, err, "the audit must walk the repository root")

	lits := findInvocationLiterals(t, root)
	seen := map[invocationSite]bool{}
	for _, lit := range lits {
		kind, ok := invocationSiteInventory[lit.site]
		if !assert.True(t, ok, "unexpected llmclient.Invocation{} literal at %s (%s), not in the inventory — decide its thinking wiring and add it", lit.pos, lit.site.fn) {
			continue
		}
		seen[lit.site] = true
		switch kind {
		case "thinking":
			for _, name := range thinkingFieldNames {
				sel, ok := lit.keys[name].(*ast.SelectorExpr)
				if assert.True(t, ok, "%s (%s) must set %s from its own AgentConfig", lit.pos, lit.site.fn, name) {
					assert.Equal(t, name, sel.Sel.Name, "%s (%s): %s must read the same-named AgentConfig field", lit.pos, lit.site.fn, name)
				}
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
