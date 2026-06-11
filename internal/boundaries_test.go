// Package internal_test verifies the internal package layout and its
// dependency direction. Each internal package is a black box with a single
// responsibility; lower layers must never import higher ones.
package internal_test

import (
	"go/build"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/samestrin/atcr"

// allowedInternalImports maps every internal package to the set of internal
// packages it may import. Absence means "may import no internal package".
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

// repoRoot resolves the repository root from this test file's location.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Dir(filepath.Dir(file))
}

// importsOf returns the import paths of internal/<name>.
func importsOf(t *testing.T, root, name string) []string {
	t.Helper()
	pkg, err := build.Default.ImportDir(filepath.Join(root, "internal", name), 0)
	require.NoError(t, err, "internal/%s must exist and contain valid Go source", name)
	return pkg.Imports
}

func TestInternalPackages_ExistAndCompile(t *testing.T) {
	root := repoRoot(t)
	for name := range allowedInternalImports {
		t.Run(name, func(t *testing.T) {
			importsOf(t, root, name)
		})
	}
}

func TestInternalPackages_DependencyDirection(t *testing.T) {
	root := repoRoot(t)
	for name, allowed := range allowedInternalImports {
		t.Run(name, func(t *testing.T) {
			allowedSet := map[string]bool{}
			for _, a := range allowed {
				allowedSet[modulePath+"/internal/"+a] = true
			}
			for _, imp := range importsOf(t, root, name) {
				if !strings.HasPrefix(imp, modulePath+"/") {
					continue // stdlib or third-party
				}
				assert.False(t, strings.HasPrefix(imp, modulePath+"/cmd"),
					"internal/%s must not import cmd packages (imports %s)", name, imp)
				if strings.HasPrefix(imp, modulePath+"/internal/") {
					assert.True(t, allowedSet[imp],
						"internal/%s imports %s which is not in its allowlist", name, imp)
				}
			}
		})
	}
}
