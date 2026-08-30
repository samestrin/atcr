package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
		assert.Contains(t, out, "0 skipped (settled: resolved or wontfix)",
			"the settled suppression is per-record and silent otherwise: without this counter \"0 scanned\" reads the same whether the store needed no repair or was entirely suppressed")
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
	const hostileID = "aaaa\x1b[31m1111\u202E"

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
	assert.NotContains(t, out, "\u202E", "a bidi override from the store must never reach the terminal raw")
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

	// Two changes in ONE shard are not a collision: the disambiguator keys on the raw
	// filename, so the everyday multi-line listing must stay free of hash suffixes.
	assert.Contains(t, out, "2026-08.jsonl:1 ", "a shard listed twice is still one file, so no suffix")
	assert.Contains(t, out, "2026-08.jsonl:2 ", "a shard listed twice is still one file, so no suffix")
	assert.Contains(t, out, "2 rewritten (2 lines)",
		"a multi-line repair must read \"lines\", not \"line\"")
	assert.NotContains(t, out, "(2 line)")
}

// ReExtractJustification applies the producer's size cap and regular-file rule at the
// replay, returning (ok=false, err=nil) — the same shape as "this file is not the one".
// replayCandidates collapses both into an empty candidate list, so a record whose
// review.md is PRESENT and readable at its own source_report path, merely excluded by
// policy, was reported as "unresolved (no surviving review.md)". The documented remedy
// for that label is "prune the pointer or restore the file", which sends the operator
// to fix a file that is already there.
//
// The exclusion itself is correct and mirrors the producer. Only the reported reason
// was wrong, so what this pins is the WORDING an operator acts on.
func TestDebtBackfillJustifications_UnresolvedLabelDoesNotClaimTheFileIsGone(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))

	// Over the producer's 1 MiB cap, so the replay declines it by policy. The
	// narrative it would otherwise yield sits at the top, so the ONLY reason this
	// candidate fails is its size.
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n" +
		strings.Repeat("padding line to push the file over the producer size cap\n", 40000)
	reviewPath := filepath.Join(rd, "review.md")
	require.NoError(t, os.WriteFile(reviewPath, []byte(body), 0o600))
	fi, err := os.Stat(reviewPath)
	require.NoError(t, err)
	require.Greater(t, fi.Size(), int64(1<<20), "premise: the file must exceed the producer's cap")

	rec := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08.jsonl"), []byte(rec+"\n"), 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)

	require.Contains(t, out, "1 unresolved", "premise: the record must land in the unresolved bucket")
	assert.NotContains(t, out, "no surviving review.md",
		"the review.md is present and readable at the record's own path — the label must not send the operator to restore it")
	assert.FileExists(t, reviewPath, "premise: nothing removed the file")
}

// The listing hardened the id and left the LOCATOR beside it open. The shard half of
// `<shard>:<line>` goes through sanitizeCell, which strips C0/ESC/DEL, C1 and
// U+2028/U+2029 but deliberately KEEPS category Cf — so a bidi override in a shard
// filename passes through unchanged, on the one field that literally names the line.
// The comment eight lines above the print names "reordering WHICH line is named" as
// the whole attack, so the mitigation has to cover the field that does the naming.
//
// c.Shard is e.Name() from os.ReadDir over the store directory, which the same comment
// classifies as untrusted on its world-appendable-store premise.
func TestDebtBackfillJustifications_DryRunStripsBidiFromTheShardLocator(t *testing.T) {
	// A store filename carrying a right-to-left override. It still ends in .jsonl, so
	// the backfill scan picks it up.
	const hostileShard = "2026-08\u202E-a.jsonl"

	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
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
	require.NoError(t, os.WriteFile(filepath.Join(store, hostileShard), []byte(rec+"\n"), 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)

	// Premise: the listing really did reach this record, so the assertion below is
	// about the locator rather than about a line that was never printed.
	require.Contains(t, out, "aaaa1111", "the dry run must have listed the record")

	assert.NotContains(t, out, "\u202E", "a bidi override in a shard filename must never reach the terminal raw")
	// Stripped, not quoted: `<shard>:<line>` has to stay ONE copy-pasteable token, so
	// the line number cannot be pushed outside a quoted name.
	assert.Contains(t, out, "2026-08-a.jsonl:1",
		"the locator must survive as a single token with the offending rune removed")
}

// Stripping Cf rather than escaping it is lossy: two DIFFERENT shard filenames that
// differ only by a format rune reduce to the same token, so the listing goes ambiguous
// about which file the in-place rewrite would touch — on the one surface an operator
// consults to decide whether to let it proceed. Tagging the row "(name sanitized)"
// cannot fix that: both rows would carry the same tag and still read identically. The
// locator has to tell the two apart.
func TestDebtBackfillJustifications_DryRunDisambiguatesCollidingShardLocators(t *testing.T) {
	// Both reduce to "2026-08-a.jsonl" once Cf is stripped.
	shards := []string{"2026-08\u202E-a.jsonl", "2026-08\u200B-a.jsonl"}

	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	for i, sh := range shards {
		rec := fmt.Sprintf(`{"schema_version":3,"id":"aaaa111%d","run_id":"2026-08-01T00:00:00Z-multi-agent",`+
			`"ts":"2026-08-01T00:00:00Z","severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p",`+
			`"fix":"f","category":"correctness","est_minutes":10,"evidence":"e","reviewers":["dax"],`+
			`"confidence":"HIGH",`+
			`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",`+
			`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`, i)
		require.NoError(t, os.WriteFile(filepath.Join(store, sh), []byte(rec+"\n"), 0o600))
	}

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "aaaa1110", "the dry run must have listed the first record")
	require.Contains(t, out, "aaaa1111", "the dry run must have listed the second record")
	assert.NotContains(t, out, "\u202E")
	assert.NotContains(t, out, "\u200B")

	locators := regexp.MustCompile(`(?m)^  (\S+):1 `).FindAllStringSubmatch(out, -1)
	require.Len(t, locators, 2, "both shards must be listed")
	assert.NotEqual(t, locators[0][1], locators[1][1],
		"two distinct shard files must not render as one identical locator")
	for _, l := range locators {
		assert.Contains(t, l[1], "2026-08-a.jsonl",
			"the disambiguator must extend the sanitized name, not replace it")
	}
}

// The disambiguator is a collision remedy, not decoration: a shard whose sanitized name
// is unique must print exactly that name, so the common listing stays clean.
func TestDebtBackfillJustifications_DryRunDoesNotDisambiguateAUniqueLocator(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
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

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "2026-08.jsonl:1 ",
		"an unambiguous locator must print bare, with no disambiguator appended")
}

// The ambiguity the disambiguator removes is between the printed token and a real file
// ON DISK, so detecting a collision only within the printed change set leaves the
// dangerous half uncovered: a colliding shard that produced no rewrite never enters the
// map, and the changed one prints bare. Here the genuine 2026-08.jsonl has nothing to
// repair and a planted 2026-08<U+200B>.jsonl carries the record — the operator reads
// "2026-08.jsonl:1", approves believing their August shard is being repaired, and the
// planted file is what gets rewritten.
func TestDebtBackfillJustifications_DryRunDisambiguatesAgainstAnUnchangedShardOnDisk(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	// The operator's genuine August shard: present, listable, nothing to repair.
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08.jsonl"), []byte(""), 0o600))

	// The planted twin. Its name reduces to the genuine one once Cf is stripped.
	rec := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08​.jsonl"), []byte(rec+"\n"), 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "aaaa1111", "the planted shard's record must have been listed")
	assert.NotContains(t, out, "​")

	locators := regexp.MustCompile(`(?m)^  (\S+):1 `).FindAllStringSubmatch(out, -1)
	require.Len(t, locators, 1, "exactly one record changes")
	assert.Regexp(t, `^2026-08\.jsonl#[0-9a-f]{6}$`, locators[0][1],
		"a shard whose sanitized name collides with a real file on disk must be disambiguated, "+
			"even when that file produced no rewrite")
}

// The disambiguator stays a collision remedy when the store holds other shards: a name
// that is unique among ALL listable shards still prints bare.
func TestDebtBackfillJustifications_DryRunLeavesAUniqueLocatorBareAlongsideOtherShards(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-07.jsonl"), []byte(""), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(store, "notes.txt"), []byte("not a shard\n"), 0o600))

	rec := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08.jsonl"), []byte(rec+"\n"), 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "2026-08.jsonl:1 ",
		"an unambiguous locator must print bare even when the store holds other shards")
}
