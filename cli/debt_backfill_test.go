package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// backfillFixture writes a store whose one open record carries a stale, marker-free
// justification, plus the review.md it was stamped from — whose fence is dangling, so
// a replay today emits the synthetic opener the stored text lacks.
func backfillFixture(t *testing.T) (store, reviewRoot string) {
	t.Helper()
	root := t.TempDir()
	store = filepath.Join(root, "debt")
	reviewRoot = filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	rec := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08.jsonl"), []byte(rec+"\n"), 0o600))
	return store, reviewRoot
}

// The wontfix gate reads the excerpt already on disk, so repairing the store is what
// closes the hole — a code change alone reaches only records persisted after it.
func TestDebtBackfillJustifications(t *testing.T) {
	t.Run("rewrites the stale excerpt and reports the counts", func(t *testing.T) {
		store, reviewRoot := backfillFixture(t)

		code, out := execCmdCapture(t, "debt", "backfill-justifications",
			"--store", store, "--review-root", reviewRoot)
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, "1 rewritten")

		b, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)
		assert.Contains(t, string(b), "```",
			"the replayed excerpt carries the synthetic dangling-fence marker")
	})

	t.Run("dry run changes nothing", func(t *testing.T) {
		store, reviewRoot := backfillFixture(t)
		before, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)

		code, out := execCmdCapture(t, "debt", "backfill-justifications",
			"--store", store, "--review-root", reviewRoot, "--dry-run")
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, "1 rewritten")

		after, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after))
	})

	// --dry-run is documented as the step to run FIRST on the one subcommand that
	// rewrites the store in place. A bare counter cannot reveal that the pass would
	// overwrite an operator's typed rationale, so the dry run has to show the text.
	t.Run("dry run prints the before and after of every line it would touch", func(t *testing.T) {
		store, reviewRoot := backfillFixture(t)

		code, out := execCmdCapture(t, "debt", "backfill-justifications",
			"--store", store, "--review-root", reviewRoot, "--dry-run")
		require.Equal(t, 0, code, out)

		assert.Contains(t, out, "1 rewritten (1 line)",
			"the counter must name LINES as well as records: one id can carry several lines")
		assert.Contains(t, out, "2026-08.jsonl:1", "each line is named by shard and line number")
		assert.Contains(t, out, "before:")
		assert.Contains(t, out, "after:")
	})

	t.Run("is registered under debt", func(t *testing.T) {
		_, out := execCmdCapture(t, "debt", "--help")
		assert.Contains(t, out, "backfill-justifications")
	})
}

// (c) of the answered clarification: whichever repair ships, the docstring must state
// the guarantee's SCOPE, so a reader of the gate is not left inferring that every
// stored excerpt is covered by it.
func TestDebtResolveDocumentsBackfillScope(t *testing.T) {
	b, err := os.ReadFile("debt_resolve.go")
	require.NoError(t, err)
	src := string(b)
	assert.Contains(t, src, "backfill-justifications",
		"the gate's docstring must name the command that repairs pre-existing records")
	assert.True(t,
		strings.Contains(src, "not retroactive") || strings.Contains(src, "NOT RETROACTIVE"),
		"the docstring must say the guarantee does not reach records persisted earlier")
}
