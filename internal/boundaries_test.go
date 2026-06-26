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
	"atomicfs":       {},
	"atomicwrite":    {"atomicfs"},
	"cache":          {"atomicfs"}, // diff cache leaf; atomicfs for atomic entry writes (epic 5.2)
	"stream":         {"metrics"},  // metrics: observability counters for a git-unavailable file index and indeterminate/unresolvable path validation (stdlib-only leaf, no cycle)
	"gitrange":       {},
	"log":            {},                                       // single diagnostic sink; stdlib-only (epic 4.0)
	"errors":         {},                                       // error-classification taxonomy; stdlib-only (epic 4.0)
	"registry":       {"stream"},                               // stream is the canonical zero-dependency severity leaf (epic 3.5)
	"tools":          {"sandbox"},                              // sandbox: run_tests/run_script execute in the container backend (epic 11.0, opt-in --exec)
	"sandbox":        {"log"},                                  // container-isolated executor for --exec reproduction; log: structured audit line per sandbox run (epic 11.0)
	"repro":          {"reconcile", "sandbox"},                 // 2-run determinism + evidence_exec write-back: runs the sandbox, stamps reconcile findings (epic 11.0)
	"metrics":        {},                                       // in-process metrics collector; stdlib-only leaf (epic 4.4)
	"version":        {},                                       // build-version holder (atcr_version in the leaderboard submission); stdlib-only leaf, no imports (epic 10.0)
	"circuitbreaker": {"metrics"},                              // per-provider breaker; pushes state to the metrics gauge (epic 4.5)
	"validation":     {},                                       // user-input validators; stdlib-only leaf (epic 4.3)
	"payload":        {"gitrange", "atomicfs", "log"},          // log: single diagnostic sink, injected via context (epic 4.0 phase 4.1)
	"llmclient":      {"registry", "errors", "circuitbreaker"}, // circuitbreaker: per-provider fail-fast on the API call path (epic 4.5)
	"doctor":         {"llmclient", "registry"},
	"fanout":         {"llmclient", "registry", "stream", "payload", "tools", "log", "metrics", "circuitbreaker", "validation", "atomicfs", "cache"}, // log: WithAgent per-agent correlation (epic 4.0 phase 4.2); metrics: fan-out instrumentation (epic 4.4); circuitbreaker: provider threaded onto the call context (epic 4.5); validation: engine-level --output-dir system-path reject for non-CLI callers (stdlib-only leaf); atomicfs: CopyPath for the EXDEV copy-fallback in backupExisting's crash-safe swap, the shared low-level fs leaf reconcile/verify already import (epic 4.7.1); cache: diff-cache replay on the single-shot review path (epic 5.2)
	"reconcile":      {"stream", "atomicfs"},
	"scorecard":      {"llmclient", "reconcile", "fanout", "version"}, // version: atcr_version stamped into the public submission envelope (epic 10.0)
	"personas":       {"registry", "payload"},                         // community persona lifecycle: validates fetched YAML via registry.ValidateAgentYAML; built-in roster from top-level personas/ (non-internal) (epic 9.0); payload: TemplateFixtureRunner calls RenderPrompt to validate built-in templates against embedded fixtures (TD-012)
	"report":         {"stream", "reconcile"},
	"ghaction":       {"reconcile"},                                                                                                               // GitHub Action renderer/client: reads reconciled findings, posts check runs (epic 7.3)
	"verify":         {"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs", "atomicwrite", "log", "sandbox"}, // log: skeptic-failure routing (epic 4.0 phase 4.2); atomicwrite: shared group-write helper; sandbox: --exec backend resolution + dispatcher wiring (epic 11.0)
	"debate":         {"reconcile", "stream", "registry", "fanout", "payload", "tools", "llmclient", "atomicfs", "atomicwrite", "log"},            // cross-examination stage; mirrors verify's harness; atomicwrite shared group-write helper (epic 6.0)
	"mcp":            {"gitrange", "payload", "registry", "llmclient", "fanout", "stream", "reconcile", "report", "verify", "debate", "scorecard", "log", "metrics"},
	"benchmark":      {"scorecard", "version"}, // standard-suite contract + suite-tagged submission envelope; reuses scorecard.PublicRecord for one public reviewer schema, version for atcr_version (epic 10.0)
	// integration holds only end-to-end _test.go files (no production code).
	// The dependency-direction walk skips _test.go, so this entry exists to
	// satisfy the allowlist-completeness check; it records the packages those
	// tests exercise (epic 4.0 phase 5.2).
	"integration": {"fanout", "llmclient", "log", "errors", "registry", "mcp"},
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
		// The dependency-direction rule governs production code only: test
		// files do not ship and cannot create production import cycles, and
		// cross-boundary test imports are how enum-parity tests (e.g.
		// registry's TestPayloadModeEnumParity) detect drift between packages
		// the boundary keeps apart.
		if strings.HasSuffix(path, "_test.go") {
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
					// Non-internal in-module packages (currently only
					// personas/, embedded data) are deliberately importable
					// from anywhere.
					continue
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
