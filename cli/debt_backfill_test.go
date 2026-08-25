package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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

	// The DEFAULT --review-root is the whole repo root — the widest search scope, on
	// the one subcommand that rewrites the store in place, and the scope an operator
	// is most likely to run under. Both other cases pass --review-root explicitly, so
	// the branch that decides it was never exercised.
	t.Run("omitting --review-root searches the repo root", func(t *testing.T) {
		store, reviewRoot := backfillFixture(t)
		// repoRoot() walks up for a .git DIRECTORY. Planting one at the parent of
		// the review tree makes that walk terminate at a known path, so the
		// assertion below is about the resolution, not about the ambient checkout.
		repo := filepath.Dir(reviewRoot)
		require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o750))
		// Run from a subdirectory that holds NO review.md. Only a search rooted at
		// the repo can reach the narrative from here, so a resolution that quietly
		// used the working directory instead would report "0 rewritten".
		elsewhere := filepath.Join(repo, "elsewhere")
		require.NoError(t, os.MkdirAll(elsewhere, 0o750))
		t.Chdir(elsewhere)

		code, out := execCmdCapture(t, "debt", "backfill-justifications", "--store", store)
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, "1 rewritten (1 line)",
			"the default root must reach the review.md that --review-root reached explicitly")

		b, err := os.ReadFile(filepath.Join(store, "2026-08.jsonl"))
		require.NoError(t, err)
		assert.Contains(t, string(b), "```")
	})

	// The BackfillJustifications error is WRAPPED, not returned bare: without the
	// "backfill-justifications:" prefix a store-level failure reads as if it came
	// from somewhere else in the debt namespace.
	t.Run("wraps a store failure with the subcommand name", func(t *testing.T) {
		_, reviewRoot := backfillFixture(t)
		// A regular file where the store directory belongs. Every store operation
		// fails on it, and the specific errno is not what this test pins — the
		// prefix is.
		notADir := filepath.Join(t.TempDir(), "store-is-a-file")
		require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))

		code, out := execCmdCapture(t, "debt", "backfill-justifications",
			"--store", notADir, "--review-root", reviewRoot)
		require.NotEqual(t, 0, code, "a store that cannot be read must not report success: %s", out)
		assert.Contains(t, out, "backfill-justifications:")
	})

	// repoRoot()'s own error arm (cli/debt_backfill.go) is unreachable from a test:
	// repoRoot falls back to the working directory and returns an error only when
	// os.Getwd fails, which no portable test can force. It is left uncovered
	// deliberately rather than pinned with a fake.

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

// The store is world-appendable, so every field of a record is untrusted input. The
// --dry-run listing is the documented step to run FIRST on the one subcommand that
// rewrites the store in place, so a bidi override or an ANSI CSI in an id can
// misrepresent WHICH line is about to be overwritten — on the exact surface an
// operator consults to decide whether to proceed.
//
// The id reaches JustificationChange unvalidated (internal/localdebt/backfill.go reads
// it as `id, _ := m["id"].(string)`), and the sibling surface `atcr debt list` already
// strips these through sanitizeCell. The escaping has to hold for EVERY field of the
// line, not only before/after.
func TestDebtBackfillJustifications_DryRunEscapesTheUntrustedID(t *testing.T) {
	const hostileID = "aaaa\x1b[31m1111‮"

	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	recID, err := json.Marshal(hostileID)
	require.NoError(t, err)
	rec := `{"schema_version":3,"id":` + string(recID) + `,"run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08.jsonl"), []byte(rec+"\n"), 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)

	// Premise: the listing really did reach this record, so the assertions below are
	// about escaping rather than about a line that was never printed.
	require.Contains(t, out, "2026-08.jsonl:1", "the dry run must have listed the record")

	assert.NotContains(t, out, "\x1b", "an ANSI escape from the store must never reach the terminal raw")
	assert.NotContains(t, out, "‮", "a bidi override from the store must never reach the terminal raw")
	// The id still has to be identifiable — escaping is not redaction.
	assert.Contains(t, out, strconv.Quote(hostileID))
}

// The counter's plural branch (pluralLines' `return "lines"`) is reachable from the
// cli only through a multi-line rewrite, and every other cli subtest rewrites exactly
// one line — so the wording an operator reads on any real repair was exercised for
// n == 1 only. internal/localdebt pins the underlying multi-line BEHAVIOUR; what is
// unpinned here is the rendering.
func TestDebtBackfillJustifications_DryRunCounterPluralisesLines(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	// Two DISTINCT records anchored at the same finding: both carry the same stale
	// marker-free excerpt, so one pass rewrites two lines of the shard.
	rec := func(id string) string {
		return `{"schema_version":3,"id":"` + id + `","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
			`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
			`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
			`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
	}
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08.jsonl"),
		[]byte(rec("aaaa1111")+"\n"+rec("bbbb2222")+"\n"), 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)

	assert.Contains(t, out, "2 rewritten (2 lines)",
		"a multi-line repair must read \"lines\", not \"line\"")
	assert.NotContains(t, out, "(2 line)")
}
