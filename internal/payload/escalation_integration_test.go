package payload

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// thrashV1 is the first architecture: a simple sequential dispatcher.
//
// Untouched is byte-identical across all three revisions, so it never appears in
// the net base..head diff — only a HEAD-derived skeleton can name it. That is the
// property TestSkeletonInjection_DescribesHeadNotIntermediateCommits turns on.
const thrashV1 = `package p

func Untouched(s string) error { return nil }

func Dispatch(kind string, n int) int {
	if kind == "a" {
		return n + 1
	}
	if kind == "b" {
		return n + 2
	}
	return 0
}
`

// thrashV2 is an intermediate rewrite — a table-driven form. A reviewer that
// reconstructs the file from a mid-branch commit would describe THIS shape.
const thrashV2 = `package p

func Untouched(s string) error { return nil }

var table = map[string]int{"a": 1, "b": 2}

func Dispatch(kind string, n int) int {
	if delta, ok := table[kind]; ok {
		return n + delta
	}
	return 0
}
`

// thrashV3 is the final, resolved HEAD architecture: the table is gone again and
// a typed handler replaced it. Only a skeleton of HEAD shows Handler/Register.
const thrashV3 = `package p

func Untouched(s string) error { return nil }

type Handler struct {
	Kind  string
	Delta int
}

func Register(h Handler) error {
	if h.Kind == "" {
		return errEmptyKind
	}
	for i := range registry {
		if registry[i].Kind == h.Kind {
			registry[i] = h
			return nil
		}
	}
	registry = append(registry, h)
	return nil
}

func Dispatch(kind string, n int) int {
	for _, h := range registry {
		if h.Kind == kind {
			return n + h.Delta
		}
	}
	return 0
}
`

// thrashingRepo builds a three-commit branch whose net diff is structurally
// confusing: v1 -> v2 rewrites the dispatcher one way, v2 -> v3 rewrites it
// again. base is the pre-branch commit and head is the resolved final state.
func thrashingRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = initRepo(t)
	write(t, dir, "dispatch.go", thrashV1)
	base = commitAll(t, dir, "v1: sequential dispatcher")
	write(t, dir, "dispatch.go", thrashV2)
	commitAll(t, dir, "v2: table-driven rewrite")
	write(t, dir, "dispatch.go", thrashV3)
	head = commitAll(t, dir, "v3: handler registry")
	return dir, base, head
}

// AC7: a reviewer reading the payload of a multi-commit thrashing branch must be
// able to deduce the FINAL state. The skeleton is the mechanism, so the
// assertion is that the injected skeleton describes HEAD exactly — it names the
// declarations that exist at v3 and none of the ones that existed only at v1/v2.
func TestSkeletonInjection_DescribesHeadNotIntermediateCommits(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	skel := skeletonBlockOf(t, entries[0].Body)

	// Present at HEAD.
	require.Contains(t, skel, "type Handler struct")
	require.Contains(t, skel, "func Register(h Handler) error")
	require.Contains(t, skel, "func Dispatch(kind string, n int) int")

	// The load-bearing assertion: Untouched is byte-identical across v1/v2/v3, so
	// it appears in NO added/removed line of the net base..head diff. A skeleton
	// derived from the diff's `+` lines could therefore never name it; only a
	// skeleton built from the HEAD blob can. This is the ONLY assertion here that
	// fails if the skeleton is diff-derived rather than HEAD-derived — the other
	// declarations all appear as `+` lines because the thrashing rewrote them.
	require.Contains(t, skel, "func Untouched(s string) error",
		"the skeleton must name a HEAD declaration the net diff never touched")
	for _, line := range strings.Split(entries[0].Body, "\n") {
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			require.NotContains(t, line, "Untouched",
				"Untouched is unchanged across all commits, so it must not appear in any +/- diff line")
		}
	}
}

// The skeleton must reflect HEAD even though the net diff still contains the
// intermediate state's removed lines. Comparing against a skeleton built from
// the intermediate commit proves the two are genuinely different, so a passing
// assertion above is not a coincidence of both commits sharing declarations.
func TestSkeletonInjection_DiffersFromIntermediateCommitSkeleton(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	headEntries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	headSkel := skeletonBlockOf(t, headEntries[0].Body)

	// Rebuild the same range but ending at the INTERMEDIATE commit.
	mid := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD~1"))
	midEntries, err := BuildEntries(context.Background(), ModeDiff, dir, base, mid)
	require.NoError(t, err)
	midSkel := skeletonBlockOf(t, midEntries[0].Body)

	require.NotEqual(t, midSkel, headSkel,
		"the skeleton must track the resolved HEAD tree, not whatever commit the diff happens to end at")
	require.Contains(t, midSkel, "var table", "sanity: the intermediate commit really did declare the table")
}

// The payload must still round-trip: EntriesFromRenderedPayload attributes every
// section to the right path with a skeleton present. This is the guarantee that
// justified injecting AFTER the entry-start line rather than before it.
func TestSkeletonInjection_RenderedPayloadRoundTripSurvives(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	write(t, dir, "b.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	write(t, dir, "b.go", goFileV2)
	head := commitAll(t, dir, "v2")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)

	flat, err := joinEntries(entries, nil)
	require.NoError(t, err)
	require.Contains(t, flat, skeletonStart, "fixture must actually exercise an injected skeleton")

	got := EntriesFromRenderedPayload(ModeDiff, flat)

	require.Len(t, got, len(entries))
	for i := range entries {
		require.Equal(t, entries[i].Path, got[i].Path, "section %d attributed to the wrong file", i)
	}
}

// Simple one-line changes must stay in diff mode (AC2): the token budget the
// epic exists to protect.
func TestEscalationIntegration_SimpleChangeStaysInDiffMode(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	head := commitAll(t, dir, "v2")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeDiff, entries[0].Mode)
	require.Contains(t, entries[0].Body, "diff --git", "a non-escalated file keeps its diff body")
	require.NotContains(t, entries[0].Body, fileHeaderPrefixForTest(), "it must not have been promoted to files mode")
}

// The growth a skeleton adds to a NON-escalated simple change is part of the
// same token-budget contract (AC2): it must stay bounded by the configured
// line cap. Build the same range with escalation disabled (zero config — no
// skeleton, no promotion) and with defaults, and assert the byte delta is
// bounded rather than relying on the mode label alone.
func TestEscalationIntegration_SimpleChangePayloadGrowthIsBounded(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	head := commitAll(t, dir, "v2")

	disabled, err := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(EscalationConfig{})).BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.Len(t, disabled, 1)
	require.NotContains(t, disabled[0].Body, skeletonStart, "zero config disables skeleton injection")

	cfg := DefaultEscalationConfig()
	enabled, err := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(cfg)).BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.Len(t, enabled, 1)
	require.Contains(t, enabled[0].Body, skeletonStart, "defaults inject a skeleton — the bound must actually be exercised")

	// The skeleton is at most MaxSkeletonLines header lines plus the marker
	// lines; a generous per-line width keeps the bound meaningful without
	// pinning the exact fixture wording.
	const bytesPerLine = 100
	growth := len(enabled[0].Body) - len(disabled[0].Body)
	require.LessOrEqual(t, growth, cfg.MaxSkeletonLines*bytesPerLine,
		"skeleton growth on a non-escalated file must stay bounded by the configured cap")
}

// A file whose net diff is dominated by churn escalates, and the recorded Mode
// reflects what the reviewer actually saw.
func TestEscalationIntegration_ChurnedFileEscalatesAndRecordsMode(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeBlocks, entries[0].Mode,
		"a fully-rewritten file must not be reviewed from hunks alone")
	require.Contains(t, entries[0].Body, "diff --git",
		"blocks mode renders function-context hunks — the body must match the recorded mode")
	require.NotContains(t, entries[0].Body, fileHeaderPrefixForTest(),
		"blocks mode must not render the whole HEAD file")
	// NOTE: the body-shape coupling between mode and render cannot be tested on
	// a whole-file rewrite (the -U10 and function-context renders converge to
	// the same single hunk) — see TestEscalationIntegration_RecordedModeMatchesRenderedBody.
}

// With escalation disabled the payload is byte-identical to the pre-epic output:
// no skeleton, no promotion. This is the escape hatch's contract.
func TestEscalationIntegration_DisabledProducesUnchangedPayload(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	rb := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(EscalationConfig{}))
	entries, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeDiff, entries[0].Mode)
	require.NotContains(t, entries[0].Body, skeletonStart)
}

// Under the file cap — and with the feature disabled outright — a run is NOT a
// degradation: EscalationDegraded must stay false so the manifest does not cry
// wolf. (The above-cap reporting path is pinned by
// TestEscalationIntegration_ManyFilesTripTheCap below.)
func TestEscalationIntegration_UnderCapAndDisabledAreNotDegradations(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	cfg := DefaultEscalationConfig()
	cfg.MaxFiles = 0 // any change set exceeds a disabled cap
	rb := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(cfg))
	_, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.False(t, rb.EscalationDegraded(), "a disabled feature is not a degradation")

	cfg.MaxFiles = 1
	rb2 := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(cfg))
	_, err = rb2.BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.False(t, rb2.EscalationDegraded(), "one changed file is within a cap of 1")
}

func TestEscalationIntegration_ManyFilesTripTheCap(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	write(t, dir, "b.go", goFileV1)
	base := commitAll(t, dir, "v1")
	write(t, dir, "a.go", goFileV2)
	write(t, dir, "b.go", goFileV2)
	head := commitAll(t, dir, "v2")

	cfg := DefaultEscalationConfig()
	cfg.MaxFiles = 1
	rb := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(cfg))
	entries, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)

	require.True(t, rb.EscalationDegraded(), "two changed files exceed a cap of 1")
	for _, e := range entries {
		require.Equal(t, ModeDiff, e.Mode)
		require.NotContains(t, e.Body, skeletonStart)
	}
}

// skeletonBlockOf extracts the skeleton block from a rendered file body.
func skeletonBlockOf(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, skeletonStart)
	require.GreaterOrEqual(t, start, 0, "no skeleton block found in body:\n%s", body)
	end := strings.Index(body[start:], skeletonEnd)
	require.GreaterOrEqual(t, end, 0, "skeleton block not terminated")
	return body[start : start+end+len(skeletonEnd)]
}

func fileHeaderPrefixForTest() string { return "=== FILE: " }

// A payload holding any full-file body must carry the wider files-mode scope
// rule, even when the run's configured mode is diff or blocks (Epic 35.1).
func TestScopeRuleForPayload_EscalatedFileWidensTheRule(t *testing.T) {
	escalated := "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-x\n+y\n=== FILE: b.go ===\npackage p\n"

	require.Equal(t, ScopeRule(ModeFiles), ScopeRuleForPayload(ModeDiff, escalated))
	require.Equal(t, ScopeRule(ModeFiles), ScopeRuleForPayload(ModeBlocks, escalated))
}

func TestScopeRuleForPayload_PlainDiffKeepsTheNarrowRule(t *testing.T) {
	plain := "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-x\n+y\n"

	require.Equal(t, ScopeRule(ModeDiff), ScopeRuleForPayload(ModeDiff, plain))
	require.Equal(t, ScopeRule(ModeBlocks), ScopeRuleForPayload(ModeBlocks, plain))
	require.Equal(t, ScopeRule(ModeDiff), ScopeRuleForPayload(ModeDiff, ""))
}

// A diff whose CONTENT mentions the files-mode header must not widen the rule:
// inside a unified diff every content line carries a +/-/space prefix, so a
// header can never sit at column 0 unless a file really was rendered in files
// mode. This is the property the text-based detection rests on.
func TestScopeRuleForPayload_HeaderInsideDiffContentDoesNotWiden(t *testing.T) {
	tricky := "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-old\n+\tfileHeaderFmt = \"=== FILE: %s ===\"\n"

	require.Equal(t, ScopeRule(ModeDiff), ScopeRuleForPayload(ModeDiff, tricky))
}

func TestScopeRuleForPayload_FilesModeIsUnchanged(t *testing.T) {
	require.Equal(t, ScopeRule(ModeFiles), ScopeRuleForPayload(ModeFiles, "=== FILE: a.go ===\npackage p\n"))
	require.Equal(t, ScopeRule(ModeFiles), ScopeRuleForPayload(ModeFiles, ""))
}

func TestHigherContextMode_PicksTheMostContext(t *testing.T) {
	require.Equal(t, ModeFiles, HigherContextMode(ModeDiff, ModeFiles))
	require.Equal(t, ModeFiles, HigherContextMode(ModeFiles, ModeDiff))
	require.Equal(t, ModeBlocks, HigherContextMode(ModeDiff, ModeBlocks))
	require.Equal(t, ModeBlocks, HigherContextMode(ModeBlocks, ModeBlocks))
	// An unknown/empty mode ranks lowest and never wins over a real one.
	require.Equal(t, ModeBlocks, HigherContextMode(PayloadMode(""), ModeBlocks))
}

// A newly added file's diff already contains every line, so its churn ratio is
// definitionally 1.0. Escalating on that would promote every added file while
// showing the reviewer nothing it did not already have.
func TestEscalationIntegration_AddedFileDoesNotEscalateOnChurn(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "seed.go", goFileV1)
	base := commitAll(t, dir, "seed")
	// A simple added file: every line is "changed", but complexity is trivial.
	write(t, dir, "added.go", goFileV1)
	head := commitAll(t, dir, "add a file")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "added.go", entries[0].Path)

	require.Equal(t, ModeDiff, entries[0].Mode,
		"an added file must not escalate on churn alone — its diff is already the whole file")
}

// A multi-megabyte file must be skipped by the analysis pass: no AST parse, no
// skeleton, and — critically — its HEAD blob must not be retained, or a 50-file
// change set of large generated files would hold them all in memory at once.
func TestEscalationIntegration_OversizedFileSkipsAnalysis(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "gen.go", "package p\n\nfunc Small() int { return 1 }\n")
	base := commitAll(t, dir, "v1")

	var huge strings.Builder
	huge.WriteString("package p\n\nfunc Small() int { return 2 }\n")
	for i := 0; huge.Len() < (1<<20)+4096; i++ {
		fmt.Fprintf(&huge, "var Gen%d = %d\n", i, i)
	}
	write(t, dir, "gen.go", huge.String())
	head := commitAll(t, dir, "v2 (oversized)")

	rb := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(DefaultEscalationConfig()))
	entries, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeDiff, entries[0].Mode, "an oversized file must not escalate")
	require.NotContains(t, entries[0].Body, skeletonStart, "an oversized file must get no skeleton")

	_, retained := rb.g.forRange(base, head).headSrc["gen.go"]
	require.False(t, retained, "an oversized file's HEAD blob must not be retained")
}

// Files mode is the top of the ladder and suppresses the skeleton, so the
// analysis pass must not run: no extra git process, no AST parse per file.
func TestEscalationIntegration_FilesModeSkipsAnalysisEntirely(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	rb := NewRangeBuilder(context.Background(), dir, base, head)
	entries, err := rb.BuildEntries(ModeFiles)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeFiles, entries[0].Mode)
	require.NotContains(t, entries[0].Body, skeletonStart,
		"files mode already carries the whole file; a skeleton would be duplication")
}

// A file that escalates INTO files mode must read its HEAD blob once, not twice
// (once to measure churn, once to render). The memo is what makes that true.
func TestChurnLines_UsesMaxNotSumSoModifiedLinesCountOnce(t *testing.T) {
	// numstat reports a MODIFIED line as one addition + one deletion, so summing
	// them double-counts churn: 30 modified lines in a ~101-line file would read
	// as 60/101 ≈ 0.59 and fire the 0.5 churn ratio, though only ~30% of HEAD
	// lines are actually touched. The churn measure must be max(added, deleted)
	// so it is a true fraction of HEAD lines (Epic 35.1 TD).
	dir := initRepo(t)
	var v1, v2 strings.Builder
	v1.WriteString("package p\n")
	v2.WriteString("package p\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&v1, "var V%d = %d\n", i, i)
		if i < 30 {
			fmt.Fprintf(&v2, "var V%d = %d\n", i, i+1000) // modified line
		} else {
			fmt.Fprintf(&v2, "var V%d = %d\n", i, i) // unchanged
		}
	}
	write(t, dir, "f.go", v1.String())
	base := commitAll(t, dir, "v1")
	write(t, dir, "f.go", v2.String())
	head := commitAll(t, dir, "v2")

	g := newGitRunner(context.Background(), dir)
	n, ok, err := g.churnLines(base, head, "f.go")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 30, n, "churn must be max(added, deleted), not their sum (60)")
}

func TestEscalationIntegration_EscalatedFileReadsHeadBlobOnce(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	rb := NewRangeBuilder(context.Background(), dir, base, head)
	before := rb.g.execCount
	_, err := rb.BuildEntries(ModeDiff)
	require.NoError(t, err)
	afterFirst := rb.g.execCount

	// Pin the absolute cost of the first build, not just the delta between the
	// two builds: 7 = name-status + numstat + the whole-range diff variants this
	// build populates + exactly ONE `git show` of the HEAD blob (shared by the
	// analysis pass and any later render via the memo). An unmemoized second
	// read inside this build would cost 8. If a legitimate pipeline change
	// alters the count, update the number deliberately.
	require.Equal(t, 7, afterFirst-before,
		"the first build must read the HEAD blob exactly once alongside the cached whole-range diffs")

	// Re-render the same range in files mode on the same runner: the HEAD blob is
	// already memoized, so rendering must not re-spawn `git show` for it.
	_, err = rb.BuildEntries(ModeFiles)
	require.NoError(t, err)

	require.Equal(t, afterFirst, rb.g.execCount,
		"the files-mode render must reuse the memoized HEAD blob rather than re-reading it")
}

// A binary file must never have its blob pulled in to measure churn: it renders
// as a one-line marker in every mode, so no measurement can change the outcome.
func TestEscalationIntegration_BinaryFileIsNotAnalyzed(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "a.go", goFileV1)
	base := commitAll(t, dir, "v1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0, 1, 2, 0, 3, 4, 0}, 0o644))
	head := commitAll(t, dir, "add binary")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)

	var bin *FileEntry
	for i := range entries {
		if entries[i].Path == "blob.bin" {
			bin = &entries[i]
		}
	}
	require.NotNil(t, bin, "binary file must still appear in the payload")
	require.Equal(t, ModeDiff, bin.Mode, "a binary file must not escalate")
	require.NotContains(t, bin.Body, skeletonStart)
	require.Contains(t, bin.Body, "[binary file changed: blob.bin]")
}

// Skipping the analysis pass because the mode cannot benefit from it is NOT a
// degradation. Reporting one would tell an operator their change set blew the
// file cap when it did not.
func TestEscalationIntegration_FilesModeIsNotADegradation(t *testing.T) {
	dir, base, head := thrashingRepo(t)

	rb := NewRangeBuilder(context.Background(), dir, base, head)
	_, err := rb.BuildEntries(ModeFiles)
	require.NoError(t, err)

	require.False(t, rb.EscalationDegraded(),
		"files mode skips analysis by design; that is not a cap degradation")
}

// A long file of many trivial functions must NOT escalate on a one-line edit.
// The McCabe threshold is a per-function convention; measuring the whole-file
// branch sum against it would flag every long file and break AC2.
func TestEscalationIntegration_LongSimpleFileDoesNotEscalate(t *testing.T) {
	var v1 strings.Builder
	v1.WriteString("package p\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&v1, "\nfunc F%d(n int) int {\n\tif n > 0 {\n\t\treturn %d\n\t}\n\treturn 0\n}\n", i, i)
	}
	dir := initRepo(t)
	write(t, dir, "many.go", v1.String())
	base := commitAll(t, dir, "v1")
	// Change exactly one line.
	write(t, dir, "many.go", strings.Replace(v1.String(), "return 7", "return 77", 1))
	head := commitAll(t, dir, "v2")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeDiff, entries[0].Mode,
		"a one-line edit to a long file of simple functions must stay in diff mode (AC2)")
}

// A single genuinely branchy function must still escalate — the per-function
// measure must not have traded the false positives for false negatives.
func TestEscalationIntegration_BranchyFunctionEscalates(t *testing.T) {
	var body strings.Builder
	body.WriteString("package p\n\nfunc Branchy(n int) int {\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&body, "\tif n == %d {\n\t\treturn %d\n\t}\n", i, i)
	}
	body.WriteString("\treturn 0\n}\n")
	src := body.String()

	dir := initRepo(t)
	write(t, dir, "branchy.go", src)
	base := commitAll(t, dir, "v1")
	write(t, dir, "branchy.go", strings.Replace(src, "return 0\n}", "return -1\n}", 1))
	head := commitAll(t, dir, "v2")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeBlocks, entries[0].Mode,
		"a function with 20 branches is too branchy to review from hunks alone")
}

// A deletion-driven rewrite is the epic's core scenario and must escalate. The
// head-side changed ranges cannot see it — a pure deletion marks no head lines —
// so churn is counted from --numstat and hunks include deletion-only hunks.
func TestEscalationIntegration_DeletionHeavyRewriteEscalates(t *testing.T) {
	var v1 strings.Builder
	v1.WriteString("package p\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&v1, "\nfunc G%d() int { return %d }\n", i, i)
	}
	dir := initRepo(t)
	write(t, dir, "gutted.go", v1.String())
	base := commitAll(t, dir, "v1: 60 functions")
	// Gut the file down to almost nothing: nearly all deletions.
	write(t, dir, "gutted.go", "package p\n\nfunc G0() int { return 0 }\n")
	head := commitAll(t, dir, "v2: gutted")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeBlocks, entries[0].Mode,
		"a rewrite that mostly DELETES code must escalate; deletions are churn too")
	require.Contains(t, entries[0].Body, "diff --git",
		"blocks mode renders function-context hunks — the body must match the recorded mode")
	require.NotContains(t, entries[0].Body, fileHeaderPrefixForTest(),
		"blocks mode must not render the whole HEAD file")
	// NOTE: the body-shape coupling between mode and render cannot be tested on
	// a whole-file rewrite (the -U10 and function-context renders converge to
	// the same single hunk) — see TestEscalationIntegration_RecordedModeMatchesRenderedBody.
}

// The recorded Mode must describe the body the reviewer actually received —
// the coupling the manifest's per_file_payload rests on. A whole-file rewrite
// cannot test this (its -U10 and function-context renders converge to the
// same single hunk), so this fixture escalates via the hunk-count signal
// instead: four one-line edits scattered across one large function. The
// function-context render then contains the WHOLE function while the plain
// -U10 render holds only four narrow hunks, so the two modes are
// distinguishable line-by-line — and rendering `mode` instead of the
// escalated `fileMode` (the regression the churn/deletion tests cannot see)
// fails the Contains assertion below.
func TestEscalationIntegration_RecordedModeMatchesRenderedBody(t *testing.T) {
	var v1 strings.Builder
	v1.WriteString("package p\n\nfunc Big() int {\n\ttotal := 0\n")
	editLines := map[int]bool{5: true, 30: true, 55: true, 80: true}
	for i := 0; i < 110; i++ {
		if i == 67 {
			v1.WriteString("\ttotal += 999 // MIDPOINT-UNCHANGED-MARKER\n")
			continue
		}
		fmt.Fprintf(&v1, "\ttotal += %d // line-%03d\n", i, i)
	}
	v1.WriteString("\treturn total\n}\n")

	dir := initRepo(t)
	write(t, dir, "big.go", v1.String())
	base := commitAll(t, dir, "v1")

	v2 := strings.Split(v1.String(), "\n")
	for i, line := range v2 {
		if strings.Contains(line, "// line-") {
			var n int
			if _, err := fmt.Sscanf(line[strings.LastIndex(line, "line-"):], "line-%d", &n); err == nil && editLines[n] {
				v2[i] = strings.Replace(line, "total += ", "total += 1000+", 1)
			}
		}
	}
	write(t, dir, "big.go", strings.Join(v2, "\n"))
	head := commitAll(t, dir, "v2: four scattered edits")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, ModeBlocks, entries[0].Mode,
		"four scattered hunks escalate via the hunk-count signal")
	require.Contains(t, entries[0].Body, "MIDPOINT-UNCHANGED-MARKER",
		"a blocks render includes the whole enclosing function — if the builder rendered the unescalated mode, this marker (>10 lines from every edit) would be absent")

	// Guard the fixture itself: the plain -U10 render must NOT contain the
	// marker, or the fixture cannot distinguish the two modes at all.
	plain, err := NewRangeBuilder(context.Background(), dir, base, head, WithEscalation(EscalationConfig{})).BuildEntries(ModeDiff)
	require.NoError(t, err)
	require.Len(t, plain, 1)
	require.NotContains(t, plain[0].Body, "MIDPOINT-UNCHANGED-MARKER",
		"fixture invariant: the -U10 render holds only narrow hunks")
}

// Pure-deletion hunks must be counted for adjacency and hunk-count purposes but
// must contribute zero head lines, so the two uses of a hunk list stay correct.
func TestParseAllHunkRanges_IncludesPureDeletionsAsEmptyRanges(t *testing.T) {
	chunk := "@@ -10,3 +9,0 @@\n-a\n-b\n-c\n@@ -20,1 +18,2 @@\n-x\n+y\n+z\n"

	got := parseAllHunkRanges(chunk)

	require.Len(t, got, 2, "the pure-deletion hunk must not be dropped")
	require.Equal(t, lineRange{start: 10, end: 9}, got[0], "a deletion is an empty range at its head anchor")
	require.Equal(t, lineRange{start: 18, end: 19}, got[1])
	// parseHeadRanges, used for grounding, still drops the deletion.
	require.Len(t, parseHeadRanges(chunk), 1)
}
