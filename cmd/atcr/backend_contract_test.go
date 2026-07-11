package main

// backend_contract_test.go locks the backend-integration contract documented in
// docs/code-review-backend.md: the `atcr review --output-dir` + `atcr reconcile`
// output tree, the pipe-delimited column shapes, and the id-or-path resolution
// rule shared by `atcr reconcile`/`atcr verify`. Private-skill consumers
// (execute-code-review / reconcile-code-review) depend on this surface, so a
// silent drift here is a backward-compatibility break — this test is the
// regression lock that turns such a drift into a failing build.
//
// Hermeticity (AC02-03): the provider is mocked via httptest.NewServer (no real
// network), git fixtures are built via os/exec argument slices (never a shell),
// and every path stays under t.TempDir() with HOME/XDG redirected by isolate().

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeBackendContractConfig writes the isolated user registry + project config
// so `atcr review` resolves a single-agent roster ("bruce") whose provider
// points at baseURL. It mirrors writeReviewFixtureConfig (review_test.go) but
// parameterizes the provider base_url, so the RED step can point at a dead
// endpoint (no findings → tree never written) and the GREEN step at a live
// httptest mock (findings → full documented tree).
func writeBackendContractConfig(t *testing.T, baseURL string) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	regDir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(regDir, 0o755))
	reg := "providers:\n" +
		"  testprov:\n" +
		"    api_key_env: ATCR_TEST_REVIEW_KEY\n" +
		"    base_url: " + baseURL + "\n" +
		"agents:\n" +
		"  bruce:\n" +
		"    provider: testprov\n" +
		"    model: test-model\n"
	require.NoError(t, os.WriteFile(filepath.Join(regDir, "registry.yaml"), []byte(reg), 0o644))
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "config.yaml"), []byte("agents: [bruce]\n"), 0o644))
}

// mockFindingsServer returns an httptest server speaking the OpenAI
// chat-completions shape, returning one valid pipe-delimited findings line so
// the review step produces a non-empty pool stream. All requests terminate on
// 127.0.0.1 — zero real network egress (AC02-03). Mirrors mockProvider in
// internal/fanout/review_test.go, replicated here because test helpers do not
// cross package boundaries.
func mockFindingsServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := "CRITICAL|a.txt:2|Unchecked change|Guard it|security|15|line two added"
		resp := map[string]any{"choices": []map[string]any{
			{"message": map[string]string{"role": "assistant", "content": content}},
		}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// documentedCoreFiles is the always-present file contract from
// docs/code-review-backend.md:44-64 that a backend caller relies on. The
// conditionally-produced entries (sources/pool/raw/agent/<agent>/,
// reconciled/ambiguous.json, reconciled/disagreements.json) are intentionally
// excluded per AC02-01 Edge Case 3: a single-agent, no-gray-zone fixture cannot
// generate them, so their absence is not a contract regression.
var documentedCoreFiles = []string{
	"manifest.json",
	filepath.Join("sources", "pool", "findings.txt"),
	filepath.Join("sources", "pool", "summary.json"),
	filepath.Join("reconciled", "findings.txt"),
	filepath.Join("reconciled", "findings.json"),
	filepath.Join("reconciled", "report.md"),
	filepath.Join("reconciled", "summary.json"),
}

// assertFindingsColumns asserts the stream carries the version header and that
// every non-comment finding row has exactly `want` pipe-delimited columns.
func assertFindingsColumns(t *testing.T, path string, want int) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "reading %s", path)
	require.Contains(t, string(data), "# atcr-findings/v1", "%s must carry the version header", path)
	rows := 0
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		cols := strings.Split(ln, "|")
		assert.Len(t, cols, want, "%s row %q must have exactly %d columns", path, ln, want)
		rows++
	}
	assert.Positive(t, rows, "%s must contain at least one finding row", path)
}

// TestBackendContract_OutputDirTreeMatchesDocumentedShape drives the full
// `atcr review --output-dir` + `atcr reconcile` flow in-process against a
// fixture git repo and mocked provider, then asserts the documented output tree
// (AC02-01), the 8-/9-column stream shapes (Scenarios 2/3), the summary.json
// fields (Edge Case 1), and that --output-dir does not touch .atcr/latest
// (Edge Case 2).
func TestBackendContract_OutputDirTreeMatchesDocumentedShape(t *testing.T) {
	isolate(t)
	t.Setenv("ATCR_TEST_REVIEW_KEY", "k")
	initGitRepoWithChange(t)

	// Point the single-agent roster at the hermetic mock provider so the review
	// produces a non-empty pool stream and the documented tree is written.
	srv := mockFindingsServer(t)
	writeBackendContractConfig(t, srv.URL)

	out := filepath.Join(t.TempDir(), "backend-out")
	require.Equal(t, 0, execCmd(t, "review", "--output-dir", out, "--base", "HEAD^", "--head", "HEAD"),
		"atcr review --output-dir must exit 0")
	require.Equal(t, 0, execCmd(t, "reconcile", out),
		"atcr reconcile <output-dir> must exit 0")

	// AC02-01 Scenario 1: the always-present documented tree.
	require.DirExists(t, filepath.Join(out, "payload"),
		"docs/code-review-backend.md output tree: payload/ missing")
	for _, rel := range documentedCoreFiles {
		require.FileExists(t, filepath.Join(out, rel),
			"docs/code-review-backend.md output tree: %s missing", rel)
	}

	// AC02-01 Scenarios 2 & 3: documented column shapes.
	assertFindingsColumns(t, filepath.Join(out, "sources", "pool", "findings.txt"), 8)
	assertFindingsColumns(t, filepath.Join(out, "reconciled", "findings.txt"), 9)

	// AC02-01 Edge Case 1: reconciled/summary.json exposes the caller-surfaced fields.
	var summary map[string]json.RawMessage
	sdata, err := os.ReadFile(filepath.Join(out, "reconciled", "summary.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(sdata, &summary), "reconciled/summary.json must decode")
	require.Contains(t, summary, "total_findings", "summary.json must expose total_findings")
	require.Contains(t, summary, "partial", "summary.json must expose partial")
	_, hasScanned := summary["sources_scanned"]
	_, hasCounts := summary["per_source_counts"]
	require.True(t, hasScanned || hasCounts,
		"summary.json must expose sources_scanned and/or per_source_counts")

	// AC02-01 Edge Case 2: --output-dir must not update .atcr/latest.
	require.NoFileExists(t, filepath.Join(".atcr", "latest"),
		"--output-dir must not write .atcr/latest (docs/code-review-backend.md:24-26)")
}

// TestBackendContract_IdOrPathResolution locks the id-or-path resolution rule
// documented at docs/code-review-backend.md:14-33 as one explicit table-driven
// contract test (AC02-02): bare id → .atcr/reviews/<id>/, explicit path → used
// as-is, omitted → .atcr/latest. reconcile operates on pre-written fixtures, so
// no provider/network is needed here.
func TestBackendContract_IdOrPathResolution(t *testing.T) {
	// verify shares the same id-or-path resolution implementation as reconcile
	// (docs/code-review-backend.md: "shared by atcr reconcile and atcr verify").
	// This table locks it on reconcile; verify's identical resolution is covered
	// by its own command tests and is intentionally not re-parameterized here to
	// avoid provider wiring verify would otherwise require.
	source := map[string]string{
		"sources/host/findings.txt": "HIGH|a.go:1|boom|fix|security|10|ev|host\n",
	}

	t.Run("bare id resolves to .atcr/reviews/<id>", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r1", source)
		require.Equal(t, 0, execCmd(t, "reconcile", "r1"))
		require.FileExists(t, filepath.Join(".atcr", "reviews", "r1", "reconciled", "findings.txt"))
	})

	t.Run("explicit path is used as-is", func(t *testing.T) {
		isolate(t)
		ext := filepath.Join(t.TempDir(), "ext-review")
		require.NoError(t, os.MkdirAll(filepath.Join(ext, "sources", "host"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(ext, "reconciled"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(ext, "sources", "host", "findings.txt"),
			[]byte("# atcr-findings/v1\nHIGH|a.go:1|boom|fix|security|10|ev|host\n"), 0o644))

		require.Equal(t, 0, execCmd(t, "reconcile", ext))
		require.FileExists(t, filepath.Join(ext, "reconciled", "findings.txt"))
		require.NoFileExists(t, filepath.Join(".atcr", "latest"),
			"explicit-path reconcile must not touch .atcr/latest")
	})

	t.Run("omitted argument resolves to .atcr/latest", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r2", source) // fixtureReview points .atcr/latest at r2
		require.Equal(t, 0, execCmd(t, "reconcile"))
		require.FileExists(t, filepath.Join(".atcr", "reviews", "r2", "reconciled", "findings.txt"))
	})

	t.Run("bare id takes precedence over .atcr/latest pointer", func(t *testing.T) {
		isolate(t)
		fixtureReview(t, "r1", source)
		fixtureReview(t, "r2", source) // r2 written last → .atcr/latest points at r2
		require.Equal(t, 0, execCmd(t, "reconcile", "r1"))
		require.FileExists(t, filepath.Join(".atcr", "reviews", "r1", "reconciled", "findings.txt"),
			"bare id r1 must reconcile r1, not the latest pointer r2")
		require.NoFileExists(t, filepath.Join(".atcr", "reviews", "r2", "reconciled", "findings.txt"),
			"reconcile r1 must not operate on r2")
	})
}
