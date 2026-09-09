package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/localdebt"
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

	// The legend is a collision remedy, like the suffix it explains: no collision, no
	// suffix, no note. Printing it unconditionally would train the operator to skip it.
	assert.NotContains(t, out, "shard names collide",
		"a listing with no suffixed locator must not carry the suffix legend")

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
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08\u200B.jsonl"), []byte(rec+"\n"), 0o600))

	code, out := execCmdCapture(t, "debt", "backfill-justifications",
		"--store", store, "--review-root", reviewRoot, "--dry-run")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "aaaa1111", "the planted shard's record must have been listed")
	assert.NotContains(t, out, "\u200B")

	locators := regexp.MustCompile(`(?m)^  (\S+):1 `).FindAllStringSubmatch(out, -1)
	require.Len(t, locators, 1, "exactly one record changes")
	assert.Regexp(t, `^2026-08\.jsonl#[0-9a-f]{12}$`, locators[0][1],
		"a shard whose sanitized name collides with a real file on disk must be disambiguated, "+
			"even when that file produced no rewrite")

	// The suffix is only as useful as the operator's ability to read it. Unannotated it
	// looks like part of the filename — which is also the documented residual case — and
	// it cannot be mapped back to a file by eye, since it is a hash of raw bytes that are
	// not shown. The security value of the whole mechanism rests on the operator knowing
	// that a suffix means "this is not the plain name you think it is".
	assert.Contains(t, out, "shard names collide once unprintable runes are stripped",
		"a suffixed listing must say why the names are suffixed")
	assert.Contains(t, out, "#xxxxxx",
		"and name the suffix's form, so the operator can tell it from a real filename")
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

// The listing filter — non-directory entries ending in ".jsonl" — was covered by no
// test: deleting it left the whole ./cli/ suite green. The one existing case that puts
// a non-shard file in the store names it "notes.txt", which cannot collide with the
// shard under test, so it is insensitive to the filter.
//
// This case covers the SUFFIX half. Its decoy is a non-".jsonl" entry whose name is the
// genuine shard's plus a TRAILING zero-width space: the ZWSP is what makes the name fail
// HasSuffix(".jsonl"), while sanitizeLocator strips Cf and so reduces it back to
// "2026-08.jsonl" — a genuine collision that only the suffix half keeps out of the map.
// The trailing position is load-bearing; an extension like ".tmp" would sanitize to a
// different token and collide with nothing, so it would prove nothing.
//
// The IsDir half is covered separately, by
// TestDebtBackfillJustifications_DryRunIgnoresADirectoryNamedLikeTheChangedShard. They
// are split so a mutation removing only one half is still attributable: with both decoys
// in one assertion, either half alone keeps the test red and the other half's coverage is
// unproven. The two halves fail DIFFERENTLY, though — see that test's header; only this
// one is discriminated by the locator assertions below.
//
// The assertion is that the genuine locator prints BARE. The filter's absence is
// fail-SAFE — more names enter the map, so the output gains spurious suffixes rather
// than losing needed ones — which is why the row was non-blocking; it is pinned anyway
// because a future NARROWING of the filter would otherwise be silent, and this is the
// surface an operator approves an in-place rewrite from.
func TestDebtBackfillJustifications_DryRunIgnoresNonShardEntriesWhenDisambiguating(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	// The decoy: not a ".jsonl" by HasSuffix, but the genuine shard's token once Cf is
	// stripped. See the header for why the ZWSP has to be trailing.
	require.NoError(t, os.WriteFile(filepath.Join(store, "2026-08.jsonl\u200B"), []byte("staged\n"), 0o600))

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
		"an entry that is not a .jsonl is not a shard, so the genuine locator must print bare")
	assert.NotRegexp(t, `2026-08\.jsonl#[0-9a-f]{6}`, out,
		"no non-shard entry may push a genuine locator into its disambiguated form")
}

// The IsDir half in isolation: a DIRECTORY whose name reduces to the changed shard's
// token once Cf is stripped. Split from the suffix case above for the reason given in
// that test's header.
//
// It is pinned by the WALK ABORTING, not by the locator assertions — and the difference
// is worth stating, because the obvious reading of this test is wrong. Dropping only
// e.IsDir() does not turn the decoy into a printed collision: os.ReadFile on a directory
// returns "is a directory", so rewriteJustifications fails and the whole backfill exits
// non-zero. Verified by mutation, which fails on require.Equal(t, 0, code, out) with
// `reading shard for backfill: read "2026-08\u200b.jsonl": is a directory`. The locator
// assertions below never get to run in that world.
//
// So the mutation IS detected, and this test is the thing that detects it — but as an
// exit-code regression, not as a disambiguation one. Left as an exit-code assertion on
// purpose: it is the real consequence of dropping the half, and stating it here is
// cheaper than manufacturing a readable decoy that would only re-prove the suffix half.
func TestDebtBackfillJustifications_DryRunIgnoresADirectoryNamedLikeTheChangedShard(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	body := "## Findings\n\nSome preamble.\n\n```\n- internal/thing.go:42 quoted example row\n\n" +
		"- **internal/thing.go:42** the real narrative explaining the defect.\n"
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(body), 0o600))

	// The decoy directory's name reduces to the changed shard's token once Cf is
	// stripped, so without the IsDir half it is a genuine collision — not merely an
	// extra name that happens to differ.
	require.NoError(t, os.MkdirAll(filepath.Join(store, "2026-08\u200B.jsonl"), 0o750))

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
		"a directory is not a shard, so the genuine locator must print bare")
	assert.NotRegexp(t, `2026-08\.jsonl#[0-9a-f]{6}`, out,
		"a directory entry must not push a genuine locator into its disambiguated form")
}

// The whole justification for stripping rather than quoting the shard is that
// `<shard>:<line>` stays ONE unambiguously parseable, copy-pasteable token. A colon
// or a space surviving inside the shard name breaks exactly that: "2026:08.jsonl:1"
// has two candidate splits and "2026 08.jsonl:1" is two tokens on a terminal. Both
// are ordinary POSIX filenames in a world-appendable store directory, so the property
// the comment at cli/debt_backfill.go:112-114 defends has to actually hold.
func TestDebtBackfillJustifications_DryRunLocatorStaysOneParseableToken(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shard string
		want  string
	}{
		{name: "colon", shard: "2026:08.jsonl", want: "2026%3A08.jsonl:1"},
		{name: "space", shard: "2026 08.jsonl", want: "2026%2008.jsonl:1"},
		{name: "percent is escaped first so the encoding is reversible", shard: "2026%3A08.jsonl", want: "2026%253A08.jsonl:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			require.NoError(t, os.WriteFile(filepath.Join(store, tc.shard), []byte(rec+"\n"), 0o600))

			code, out := execCmdCapture(t, "debt", "backfill-justifications",
				"--store", store, "--review-root", reviewRoot, "--dry-run")
			require.Equal(t, 0, code, out)
			require.Contains(t, out, "aaaa1111", "the dry run must have listed the record")
			assert.Contains(t, out, tc.want,
				"the locator must split on exactly one colon, into one shard name and one line number")
		})
	}
}

// locatorNames keys collisions on the sanitized token's BYTES, so it only detects the
// ambiguity its own lossy Cf strip introduces. Two shard names that RENDER identically
// but differ in bytes get distinct keys and no suffix at all — leaving the operator
// reading two rows that look like the same filename, which is exactly the
// which-file-would-be-rewritten ambiguity this function exists to remove.
//
// NFKC folding closes the compatibility-equivalent half of that: NFC e-acute beside
// NFD e+U+0301, and U+00A0 beside a space. It does NOT close visual confusables that
// are not compatibility-equivalent — see the residual case pinned below.
func TestLocatorNames_FoldsCompatibilityEquivalentShardNames(t *testing.T) {
	for _, tc := range []struct{ name, a, b string }{
		{name: "NFC vs NFD e-acute", a: "café.jsonl", b: "café.jsonl"},
		{name: "no-break space vs space", a: "a b.jsonl", b: "a b.jsonl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changes := []localdebt.JustificationChange{
				{Shard: tc.a, Line: 1, ID: "aaaa1111"},
				{Shard: tc.b, Line: 1, ID: "bbbb2222"},
			}
			names := locatorNames([]string{tc.a, tc.b}, changes)
			assert.NotEqual(t, names[tc.a], names[tc.b],
				"two shard names that render alike must never render as one identical locator")
			assert.Contains(t, names[tc.a], "#", "the collision must be marked, not merely survived")
			assert.Contains(t, names[tc.b], "#", "on both rows, or the operator cannot tell which is which")
		})
	}
}

// The residual class, pinned so it is a known limit rather than a surprise: a visual
// confusable that is NOT compatibility-equivalent — here U+2011 NON-BREAKING HYPHEN
// beside an ASCII hyphen-minus — folds to U+2010 under NFKC, not to U+002D, so the two
// names keep distinct keys and print bare. Closing it needs a confusables table, not a
// normalizer. This test documents the boundary; if a future change closes the gap it
// will fail here and should be updated deliberately.
func TestLocatorNames_DoesNotFoldVisualConfusablesThatAreNotCompatibilityEquivalent(t *testing.T) {
	const ascii, nbHyphen = "2026-08.jsonl", "2026‑08.jsonl"
	changes := []localdebt.JustificationChange{
		{Shard: ascii, Line: 1, ID: "aaaa1111"},
		{Shard: nbHyphen, Line: 1, ID: "bbbb2222"},
	}
	names := locatorNames([]string{ascii, nbHyphen}, changes)
	assert.NotContains(t, names[ascii], "#",
		"NFKC maps U+2011 to U+2010, not to U+002D, so this pair is not detected as a collision")
	assert.NotContains(t, names[nbHyphen], "#")
}

// The disambiguating suffix is sha256 of the raw name truncated to a hex prefix, and
// the threat model this file's own comments assume is an attacker who can write to the
// store directory. That attacker controls BOTH planted filenames and can pad either
// with arbitrary Cf runes, which sanitize away — so they are not guessing a hash of a
// name they do not control (the residual case the header dismisses), they are running
// an offline birthday search over a space they choose.
//
// At 6 hex characters that space is 24 bits, and a collision turns up in roughly 2^12
// trials. The pair below was found that way in well under a second; both names reduce
// to "2026-08.jsonl" and their sha256 digests share the prefix "2a0450". A 6-character
// suffix renders them as ONE identical locator — breaking the exact invariant the
// listing exists to uphold, on the surface an operator approves an in-place rewrite
// from.
func TestLocatorNames_SuffixSurvivesAForcedShortPrefixCollision(t *testing.T) {
	const (
		// Written as escapes: a raw U+FEFF in Go source is an illegal byte order mark.
		a = "2026-08\u200c\u200c\u00ad\u200c\ufeff.jsonl"
		b = "2026-08\ufeff\u2060\u00ad\u2060\ufeff.jsonl"
	)
	require.Equal(t, sha256Hex(a)[:6], sha256Hex(b)[:6],
		"fixture premise: these two names share a 6-hex sha256 prefix")
	require.NotEqual(t, a, b, "and they are genuinely different files")

	changes := []localdebt.JustificationChange{
		{ID: "aaaa1111", Shard: a, Line: 1},
		{ID: "bbbb2222", Shard: b, Line: 1},
	}
	names := locatorNames([]string{a, b}, changes)

	assert.NotEqual(t, names[a], names[b],
		"two distinct shard files must never render as one identical locator, "+
			"even when an attacker forces a short-prefix hash collision")
	assert.Regexp(t, `^2026-08\.jsonl#[0-9a-f]{12}$`, names[a],
		"the suffix must carry at least 48 bits, which puts a forced collision out of offline reach")
	assert.Regexp(t, `^2026-08\.jsonl#[0-9a-f]{12}$`, names[b])
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
