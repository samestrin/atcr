// Package internal_test verifies the internal package layout and its
// dependency direction. Each internal package is a black box with a single
// responsibility; lower layers must never import higher ones.
package internal_test

import (
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

const modulePath = "github.com/samestrin/atcr"

// allowedInternalImports maps every top-level internal package to the set of
// internal packages it may import (subpackages inherit their top-level
// entry). Absence of a directory here fails the completeness check.
var allowedInternalImports = map[string][]string{
	"stream":    {},
	"gitrange":  {},
	"registry":  {},
	"payload":   {"gitrange"},
	"llmclient": {"registry"},
	"fanout":    {"llmclient", "registry", "stream", "payload"},
	"reconcile": {"stream"},
	"report":    {"stream", "reconcile"},
	"mcp":       {"gitrange", "payload", "registry", "llmclient", "fanout", "stream", "reconcile", "report"},
}

// repoRoot walks up from the working directory to the directory containing
// go.mod. Robust under -trimpath, unlike runtime.Caller paths.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found above working directory")
		dir = parent
	}
}

// internalImports walks internal/<name> recursively and returns, per Go file
// (including _test.go files and files behind build tags), the union of all
// import paths. Parsing with go/parser ImportsOnly sidesteps host build
// constraints entirely.
func internalImports(t *testing.T, root, name string) []string {
	t.Helper()
	pkgDir := filepath.Join(root, "internal", name)
	seen := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(pkgDir, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		require.NoError(t, perr, "every Go file under internal/%s must parse", name)
		for _, imp := range f.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			require.NoError(t, uerr)
			seen[p] = true
		}
		return nil
	})
	require.NoError(t, err, "internal/%s must exist", name)

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func TestInternalPackages_AllowlistIsComplete(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	require.NoError(t, err)

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	keys := make([]string, 0, len(allowedInternalImports))
	for k := range allowedInternalImports {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, keys, dirs,
		"every internal/ directory must have an allowlist entry and vice versa")
}

func TestInternalPackages_AllowlistIsAcyclic(t *testing.T) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(string) bool
	visit = func(n string) bool {
		color[n] = gray
		for _, dep := range allowedInternalImports[n] {
			switch color[dep] {
			case gray:
				return false
			case white:
				if !visit(dep) {
					return false
				}
			}
		}
		color[n] = black
		return true
	}
	for n := range allowedInternalImports {
		if color[n] == white {
			assert.True(t, visit(n), "allowlist contains a dependency cycle through %s", n)
		}
	}
}

func TestInternalPackages_DependencyDirection(t *testing.T) {
	root := repoRoot(t)
	for name, allowed := range allowedInternalImports {
		t.Run(name, func(t *testing.T) {
			allowedSet := map[string]bool{}
			for _, a := range allowed {
				// A package may import its own subpackages and itself-adjacent
				// internal targets named in the allowlist.
				allowedSet[a] = true
			}
			for _, imp := range internalImports(t, root, name) {
				if imp != modulePath && !strings.HasPrefix(imp, modulePath+"/") {
					continue // stdlib or third-party
				}
				assert.False(t, imp == modulePath,
					"internal/%s must not import the module root package", name)
				assert.False(t, imp == modulePath+"/cmd" || strings.HasPrefix(imp, modulePath+"/cmd/"),
					"internal/%s must not import cmd packages (imports %s)", name, imp)

				rest, ok := strings.CutPrefix(imp, modulePath+"/internal/")
				if !ok {
					continue // other in-module path (none exist today)
				}
				top := rest
				if i := strings.Index(rest, "/"); i >= 0 {
					top = rest[:i]
				}
				if top == name {
					continue // own subpackage
				}
				assert.True(t, allowedSet[top],
					"internal/%s imports %s which is not in its allowlist", name, imp)
			}
		})
	}
}
