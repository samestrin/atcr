package history

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The storage layout lives in exactly one place: ShardDir and LegacyLedgerPath.
// The write hooks (atcr review, atcr resume) and the read path (atcr history)
// all derive locations from these helpers, so they cannot drift on where the
// monthly shards or the legacy flat ledger live.
func TestShardDir_Layout(t *testing.T) {
	assert.Equal(t, filepath.Join("repo", ".atcr", "history"), ShardDir("repo"))
}

func TestLegacyLedgerPath_Layout(t *testing.T) {
	assert.Equal(t, filepath.Join("repo", ".atcr", "findings-history.jsonl"), LegacyLedgerPath("repo"))
}

// AC3: atcr's history code has no .planning/ dependency — no path resolution
// resolves there. These return-value assertions pin the two layout helpers;
// the repo-wide guarantee is TestHistoryCode_HasNoPlanningLiteral below (a
// helper-only assertion can never fail while those two functions are literal,
// so it cannot catch a .planning fallback reintroduced in any other file).
func TestHistoryPaths_NeverResolveUnderPlanning(t *testing.T) {
	for _, got := range []string{ShardDir("repo"), LegacyLedgerPath("repo")} {
		assert.NotContains(t, filepath.ToSlash(got), ".planning",
			"history storage must not resolve under .planning/")
	}
}

// AC3, proven rather than restated: no string literal anywhere in atcr's
// PRODUCTION history code (internal/history/*.go, cli/history*.go) may
// reference .planning. Parsing the AST and inspecting only BasicLit STRINGs
// means comments — including paths.go's deliberate note that pre-existing
// .planning/history shards are NOT read — never trip the scan, while any real
// path-constructing literal would. Test files are excluded: they legitimately
// name ".planning" in assertion needles and messages (including this one);
// AC3 constrains what production code resolves, not what tests assert.
// The needle is built by concatenation so this file stays honest either way.
func TestHistoryCode_HasNoPlanningLiteral(t *testing.T) {
	needle := ".plan" + "ning"
	root := filepath.Join("..", "..") // go test runs with CWD = the package dir
	var files []string
	for _, pattern := range []string{
		filepath.Join(root, "internal", "history", "*.go"),
		filepath.Join(root, "cli", "history*.go"),
	} {
		matches, err := filepath.Glob(pattern)
		require.NoError(t, err)
		files = append(files, matches...)
	}
	require.NotEmpty(t, files, "the history source scan must not silently scan nothing")

	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, f, nil, 0) // comments never enter the AST
		require.NoError(t, err)
		ast.Inspect(node, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				assert.NotContains(t, lit.Value, needle,
					"%s: history code must not reference .planning/ (35.14 AC3)", f)
			}
			return true
		})
	}
	require.Positive(t, scanned, "no production history files were scanned")
}

// Both storage locations live under the same .atcr/ root, so a standalone user
// who has only .atcr/ gets the full queryable history (AC1 + AC2).
func TestHistoryPaths_ShareAtcrRoot(t *testing.T) {
	root := filepath.Join("repo", ".atcr")
	assert.Equal(t, root, filepath.Dir(ShardDir("repo")))
	assert.Equal(t, root, filepath.Dir(LegacyLedgerPath("repo")))
}

// A record written under a root via the shared layout helper is always visible
// to a query rooted the same way. This is the contract the write hooks and the
// history read path both depend on — the fix for review/resume writing to a
// CWD-relative dir that `atcr history` (repo-root-relative) never read.
func TestShardDir_WriteReadAgree(t *testing.T) {
	root := t.TempDir()
	ts := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, Append(ShardPath(ShardDir(root), ts), []Record{{Timestamp: ts, ID: "x", File: "a.go"}}))

	recs, err := LoadAll(ShardDir(root), LegacyLedgerPath(root))
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "x", recs[0].ID)
}
