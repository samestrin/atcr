package fanout

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileSeg builds a minimal per-file diff segment for `path` with `body` content
// lines, mirroring the shape splitDiffFiles keys on (a column-0 `diff --git a/`
// header). Every segment ends in a newline so concatenation is lossless.
func fileSeg(path string, body int) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\n")
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	b.WriteString("@@ -1," + itoa(body) + " +1," + itoa(body) + " @@\n")
	for i := 0; i < body; i++ {
		b.WriteString("+line\n")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestSplitDiffFiles(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		assert.Nil(t, splitDiffFiles(""))
	})
	t.Run("splits on file boundaries", func(t *testing.T) {
		diff := fileSeg("a.go", 2) + fileSeg("b.go", 3)
		segs := splitDiffFiles(diff)
		require.Len(t, segs, 2)
		assert.True(t, strings.HasPrefix(segs[0], "diff --git a/a.go"))
		assert.True(t, strings.HasPrefix(segs[1], "diff --git a/b.go"))
		assert.Equal(t, diff, strings.Join(segs, ""), "split is lossless")
	})
	t.Run("preamble attaches to first segment", func(t *testing.T) {
		diff := "warning: preamble line\n" + fileSeg("a.go", 1)
		segs := splitDiffFiles(diff)
		require.Len(t, segs, 1)
		assert.Equal(t, diff, segs[0])
	})
}

func TestSplitDiffFiles_EscalatedAndNonDiffEntriesAreOwnSegments(t *testing.T) {
	// A chunked payload can mix diff-mode files with entries escalated to files
	// mode (`=== FILE: p ===` + raw HEAD content) and with deleted/binary markers,
	// none of which carry a `diff --git` header. Without recognizing those column-0
	// markers, splitDiffFiles glues the following body onto the preceding diff
	// segment and countDiffFiles under-reports the file total (Epic 35.1, mixed case).
	filesEntry := "=== FILE: pkg/big.go ===\npackage pkg\n\nfunc Big() {}\n"
	deletedEntry := "[deleted file: pkg/gone.go]\n"
	binaryEntry := "[binary file changed: assets/logo.png]\n"
	diff := fileSeg("a.go", 2) + filesEntry + deletedEntry + binaryEntry

	segs := splitDiffFiles(diff)
	require.Len(t, segs, 4)
	assert.True(t, strings.HasPrefix(segs[0], "diff --git a/a.go"))
	assert.True(t, strings.HasPrefix(segs[1], "=== FILE: pkg/big.go ==="))
	assert.True(t, strings.HasPrefix(segs[2], "[deleted file: pkg/gone.go]"))
	assert.True(t, strings.HasPrefix(segs[3], "[binary file changed: assets/logo.png]"))
	assert.Equal(t, diff, strings.Join(segs, ""), "split is lossless")
	assert.Equal(t, 4, countDiffFiles(diff), "escalated/non-diff entries each count as a file")
}

// TestChunkDiff_WindowDerivedMaxLines proves F3/AC3: feeding chunkDiff a maxLines
// derived from a 32k model's window opens MORE chunks than one derived from a 128k
// model's window for the identical diff, and both plans reassemble the whole diff
// with zero files dropped. chunkDiff itself is unchanged; only its maxLines source
// (payload.ChunkMaxLines) differs per model.
func TestChunkDiff_WindowDerivedMaxLines(t *testing.T) {
	const outputTokens = 8192 // mirrors defaultMaxTokens
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString(fileSeg("f"+itoa(i)+".go", 200))
	}
	diff := b.String()

	smallML := payload.ChunkMaxLines("unlisted-small-model", nil, outputTokens) // 32768 window
	largeML := payload.ChunkMaxLines("openai/gpt-5.5", nil, outputTokens)       // 128000 window
	require.Less(t, smallML, largeML, "a 32k window must derive fewer lines-per-chunk than a 128k window")

	smallChunks := chunkDiff(diff, smallML)
	largeChunks := chunkDiff(diff, largeML)

	assert.Greater(t, len(smallChunks), len(largeChunks),
		"the 32k window must split the same diff into more chunks than the 128k window")
	// Zero files dropped: both plans reproduce the original diff exactly.
	assert.Equal(t, diff, strings.Join(smallChunks, ""), "32k chunk plan must be lossless")
	assert.Equal(t, diff, strings.Join(largeChunks, ""), "128k chunk plan must be lossless")
	// Respect the 64-chunk ceiling.
	assert.LessOrEqual(t, len(smallChunks), maxChunksPerAgent)
}

func TestChunkDiff(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		assert.Nil(t, chunkDiff("", 100))
	})
	t.Run("maxLines<=0 disables chunking", func(t *testing.T) {
		diff := fileSeg("a.go", 5) + fileSeg("b.go", 5)
		got := chunkDiff(diff, 0)
		require.Len(t, got, 1)
		assert.Equal(t, diff, got[0])
	})
	t.Run("single file under limit is one chunk", func(t *testing.T) {
		diff := fileSeg("a.go", 3)
		got := chunkDiff(diff, 100)
		require.Len(t, got, 1)
		assert.Equal(t, diff, got[0])
	})
	t.Run("bin-packs multiple small files into fewer chunks", func(t *testing.T) {
		// 3 files, each ~7 lines; a 50-line budget fits all three in one chunk —
		// demonstrably fewer requests than naive per-file (3) chunking.
		diff := fileSeg("a.go", 3) + fileSeg("b.go", 3) + fileSeg("c.go", 3)
		got := chunkDiff(diff, 50)
		require.Len(t, got, 1, "bin packing reduces request count vs per-file")
		assert.Equal(t, diff, strings.Join(got, ""), "lossless")
	})
	t.Run("starts a new chunk when the next file would exceed the budget", func(t *testing.T) {
		a := fileSeg("a.go", 4) // ~8 lines
		b := fileSeg("b.go", 4) // ~8 lines
		// Budget fits one file (~8) but not two (~16).
		got := chunkDiff(a+b, 10)
		require.Len(t, got, 2)
		assert.Equal(t, a, got[0])
		assert.Equal(t, b, got[1])
	})
	t.Run("oversized single file becomes its own chunk", func(t *testing.T) {
		small := fileSeg("a.go", 2)   // ~6 lines
		huge := fileSeg("big.go", 50) // ~54 lines, exceeds budget alone
		got := chunkDiff(small+huge, 10)
		require.Len(t, got, 2)
		assert.Equal(t, small, got[0])
		assert.Equal(t, huge, got[1], "a file larger than the budget is never split")
		assert.Greater(t, countLines(got[1]), 10, "oversized chunk is preserved whole")
	})
	t.Run("all chunks concatenate back to the original diff", func(t *testing.T) {
		diff := fileSeg("a.go", 9) + fileSeg("b.go", 2) + fileSeg("c.go", 20) + fileSeg("d.go", 1)
		got := chunkDiff(diff, 15)
		assert.Equal(t, diff, strings.Join(got, ""))
		for _, c := range got {
			assert.NotEmpty(t, c)
		}
	})
}

func TestChunkDiffBoundsChunkCount(t *testing.T) {
	// A synthetic multi-thousand-file diff with a tiny per-chunk budget would,
	// absent a ceiling, produce ~one chunk per file — thousands of slots,
	// goroutines, and provider calls (a cost/DoS vector on the exact feature meant
	// to control cost). chunkDiff must cap the slot count at maxChunksPerAgent,
	// coalescing the overflow into the final chunk while staying lossless.
	var b strings.Builder
	const files = 5000
	for i := 0; i < files; i++ {
		b.WriteString(fileSeg("f"+itoa(i)+".go", 1))
	}
	diff := b.String()
	// maxLines=1 forces a new chunk per file were there no cap. 64 is the
	// maxChunksPerAgent ceiling GREEN introduces; current (uncapped) code returns
	// ~5000 chunks and fails this bound.
	got := chunkDiff(diff, 1)
	assert.LessOrEqual(t, len(got), 64, "chunk count must be bounded regardless of diff size")
	assert.Equal(t, diff, strings.Join(got, ""), "coalescing the overflow is still lossless")
}

func TestCountDiffFiles(t *testing.T) {
	assert.Equal(t, 0, countDiffFiles(""))
	assert.Equal(t, 1, countDiffFiles(fileSeg("a.go", 1)))
	assert.Equal(t, 2, countDiffFiles(fileSeg("a.go", 1)+fileSeg("b.go", 1)))
}

func TestCountLinesTrailingPartialLine(t *testing.T) {
	assert.Equal(t, 0, countLines(""))
	assert.Equal(t, 1, countLines("a\n"))
	assert.Equal(t, 2, countLines("a\nb"), "final line without trailing newline must be counted")
	assert.Equal(t, 2, countLines("a\nb\n"))
	assert.Equal(t, 1, countLines("a"), "single line without newline is one line")
}

// diffPrefixLines is the subtraction the oversize warning applies, so both of its
// arms need pinning in both directions: the exact preamble count (not one more,
// not one less) and the no-marker default (which must stay 0, not any other
// number). Asserting only that a subtraction HAPPENS leaves the arithmetic free
// to be wrong.
func TestDiffPrefixLines(t *testing.T) {
	tests := []struct {
		name  string
		chunk string
		want  int
	}{
		{
			name:  "preamble of known length before the first marker",
			chunk: strings.Repeat("ledger line\n", 7) + fileSeg("a.go", 2),
			want:  7,
		},
		{
			name:  "single preamble line",
			chunk: "ledger line\n" + fileSeg("a.go", 1),
			want:  1,
		},
		{
			name:  "chunk starting at a marker has no preamble",
			chunk: fileSeg("a.go", 3),
			want:  0,
		},
		{
			name:  "escalated marker also ends the preamble",
			chunk: strings.Repeat("ledger line\n", 4) + "=== FILE: a.go ===\nbody\n",
			want:  4,
		},
		{
			name:  "no marker anywhere yields the zero default",
			chunk: "ledger line\nledger line\nledger line\n",
			want:  0,
		},
		{
			name:  "empty chunk",
			chunk: "",
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, diffPrefixLines(tt.chunk))
		})
	}
}

// noPrefixSeg builds a minimal diff segment produced by `git diff --no-prefix`
// (or diff.noprefix=true), where the header omits the a/ and b/ prefixes.
func noPrefixSeg(path string, body int) string {
	var b strings.Builder
	b.WriteString("diff --git " + path + " " + path + "\n")
	b.WriteString("--- " + path + "\n")
	b.WriteString("+++ " + path + "\n")
	b.WriteString("@@ -1," + itoa(body) + " +1," + itoa(body) + " @@\n")
	for i := 0; i < body; i++ {
		b.WriteString("+line\n")
	}
	return b.String()
}

func TestNoPrefixDiff(t *testing.T) {
	diff := noPrefixSeg("a.go", 1) + noPrefixSeg("b.go", 1)
	assert.Equal(t, 2, countDiffFiles(diff), "no-prefix headers should be counted")
	segs := splitDiffFiles(diff)
	require.Len(t, segs, 2)
	assert.True(t, strings.HasPrefix(segs[0], "diff --git a.go"))
	assert.True(t, strings.HasPrefix(segs[1], "diff --git b.go"))
	assert.Equal(t, diff, strings.Join(segs, ""), "split is lossless")
	// Verify chunkDiff also respects the no-prefix boundary.
	chunks := chunkDiff(diff, 50)
	require.Len(t, chunks, 1)
	assert.Equal(t, diff, chunks[0])
}

// combinedSeg builds a minimal combined-diff segment (`diff --cc <path>`) as
// produced by merge commits. Combined/merge-format diffs use a different header
// prefix than unified diffs but still mark per-file boundaries at column 0.
func combinedSeg(path string, body int) string {
	var b strings.Builder
	b.WriteString("diff --cc " + path + "\n")
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	b.WriteString("@@@ -1,1 -1,1 +1,1 @@@\n")
	for i := 0; i < body; i++ {
		b.WriteString("+line\n")
	}
	return b.String()
}

// combinedLongSeg builds a minimal `diff --combined <path>` segment, the long
// form of the combined-diff header.
func combinedLongSeg(path string, body int) string {
	var b strings.Builder
	b.WriteString("diff --combined " + path + "\n")
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	b.WriteString("@@@ -1,1 -1,1 +1,1 @@@\n")
	for i := 0; i < body; i++ {
		b.WriteString("+line\n")
	}
	return b.String()
}

func TestCombinedDiffMarkers(t *testing.T) {
	t.Run("short form --cc", func(t *testing.T) {
		diff := combinedSeg("a.go", 1) + combinedSeg("b.go", 1)
		assert.Equal(t, 2, countDiffFiles(diff), "diff --cc headers should be counted")
		segs := splitDiffFiles(diff)
		require.Len(t, segs, 2)
		assert.True(t, strings.HasPrefix(segs[0], "diff --cc a.go"))
		assert.True(t, strings.HasPrefix(segs[1], "diff --cc b.go"))
		assert.Equal(t, diff, strings.Join(segs, ""), "split is lossless")
	})
	t.Run("long form --combined", func(t *testing.T) {
		diff := combinedLongSeg("a.go", 1) + combinedLongSeg("b.go", 1)
		assert.Equal(t, 2, countDiffFiles(diff), "diff --combined headers should be counted")
		segs := splitDiffFiles(diff)
		require.Len(t, segs, 2)
		assert.True(t, strings.HasPrefix(segs[0], "diff --combined a.go"))
		assert.True(t, strings.HasPrefix(segs[1], "diff --combined b.go"))
		assert.Equal(t, diff, strings.Join(segs, ""), "split is lossless")
	})
	t.Run("chunkDiff respects combined boundaries", func(t *testing.T) {
		diff := combinedSeg("a.go", 1) + combinedSeg("b.go", 1)
		chunks := chunkDiff(diff, 50)
		require.Len(t, chunks, 1)
		assert.Equal(t, diff, chunks[0])
	})
}

func TestMergeResultGroupFallbackFromDistinct(t *testing.T) {
	g := []Result{
		{Agent: "reviewer", Status: StatusOK, FallbackUsed: true, FallbackFrom: "primary-a"},
		{Agent: "reviewer", Status: StatusOK, FallbackUsed: true, FallbackFrom: "primary-b"},
	}
	merged := mergeResultGroup(g, nil)
	assert.True(t, merged.FallbackUsed)
	assert.Contains(t, merged.FallbackFrom, "primary-a", "first fallback source should be recorded")
	assert.Contains(t, merged.FallbackFrom, "primary-b", "second fallback source should be recorded")
}

// TestMergeResultGroup_FallbackModelModal covers the F5 collapse-key contract:
// FallbackModel is reconcile's signal that two personas were served by the same
// net model. When a chunked persona's chunks fell back to DIFFERENT models,
// joining them comma-separated ("model-a,model-b") produces a composite key that
// never matches another persona's single-model key, so the persona escapes the
// intended de-weighting. The merged result must report ONE representative model
// (the modal/most-frequent fallback) instead.
func TestMergeResultGroup_FallbackModelModal(t *testing.T) {
	g := []Result{
		{Agent: "reviewer", Status: StatusOK, FallbackUsed: true, FallbackModel: "model-a"},
		{Agent: "reviewer", Status: StatusOK, FallbackUsed: true, FallbackModel: "model-b"},
		{Agent: "reviewer", Status: StatusOK, FallbackUsed: true, FallbackModel: "model-a"},
	}
	merged := mergeResultGroup(g, nil)
	assert.Equal(t, "model-a", merged.FallbackModel, "modal fallback model should be the F5 collapse key")
	assert.NotContains(t, merged.FallbackModel, ",", "composite FallbackModel breaks F5 collapse")
}

// TestMergeResultGroup_ModelIsTheModalServingModel pins the merged Model to the
// model that served most of the persona's successful chunks, not chunk 0's. When
// chunk 0 failed over to a backup and later chunks ran on the primary, inheriting
// g[0].Model recorded the backup's model for the whole persona, so its trust prior
// was scored against the wrong model's history. A disagreement still names ONE
// model rather than none: cost pricing reads this field, and an empty model would
// price the persona's real tokens at $0.
func TestMergeResultGroup_ModelIsTheModalServingModel(t *testing.T) {
	t.Run("backup chunk 0 does not outvote primary chunks", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, Model: "backup-model", FallbackUsed: true, FallbackModel: "backup-model"},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model"},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model"},
		}
		assert.Equal(t, "primary-model", mergeResultGroup(g, nil).Model)
	})
	t.Run("case-only differences are one model", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, Model: "other-model"},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model"},
			{Agent: "reviewer", Status: StatusOK, Model: "Primary-Model"},
		}
		assert.Equal(t, "primary-model", mergeResultGroup(g, nil).Model)
	})
	t.Run("failed chunks do not vote", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusFailed, Model: "backup-model"},
			{Agent: "reviewer", Status: StatusTimeout, Model: "backup-model"},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model"},
		}
		assert.Equal(t, "primary-model", mergeResultGroup(g, nil).Model)
	})
	t.Run("a tie keeps the first serving model", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, Model: "backup-model"},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model"},
		}
		assert.Equal(t, "backup-model", mergeResultGroup(g, nil).Model)
	})
	t.Run("a tie prefers the model a chunk reached without failing over", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, Model: "backup-model", FallbackUsed: true, FallbackModel: "backup-model"},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model"},
		}
		assert.Equal(t, "primary-model", mergeResultGroup(g, nil).Model)
	})
	t.Run("the window and reservation move with the model", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, Model: "backup-model", ResolvedWindow: 32768, ReservedOutputTokens: 4096, ResolvedMaxTokens: 4096},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model", ResolvedWindow: 200000, ReservedOutputTokens: 8192, ResolvedMaxTokens: 16384},
			{Agent: "reviewer", Status: StatusOK, Model: "primary-model", ResolvedWindow: 200000, ReservedOutputTokens: 8192, ResolvedMaxTokens: 16384},
		}
		out := mergeResultGroup(g, nil)
		assert.Equal(t, "primary-model", out.Model)
		assert.Equal(t, 200000, out.ResolvedWindow)
		assert.Equal(t, 8192, out.ReservedOutputTokens)
		assert.Equal(t, 16384, out.ResolvedMaxTokens)
	})
	t.Run("no successful chunk keeps chunk 0's model", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusFailed, Model: "primary-model"},
			{Agent: "reviewer", Status: StatusFailed, Model: "backup-model"},
		}
		assert.Equal(t, "primary-model", mergeResultGroup(g, nil).Model)
	})
}

// A chunk after the first that emitted prose no parser could use must still
// mark the persona: status.json's unparseable_response is the only signal that
// separates it from a clean review (bruce, live panel run 5, 2026-09-25).
func TestMergeResultGroup_AggregatesUnparseableResponse(t *testing.T) {
	for _, c := range []struct {
		name  string
		flags []bool
		want  bool
	}{
		{"later chunk unparseable", []bool{false, true}, true},
		{"first chunk unparseable", []bool{true, false}, true},
		{"third chunk unparseable", []bool{false, false, true}, true},
		{"none unparseable", []bool{false, false}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var g []Result
			for _, f := range c.flags {
				g = append(g, Result{Agent: "reviewer", Status: StatusOK, UnparseableResponse: f})
			}
			assert.Equal(t, c.want, mergeResultGroup(g, nil).UnparseableResponse)
		})
	}
}

func TestMergeResultGroup_AggregatesResponseTruncated(t *testing.T) {
	t.Run("later chunk truncated is preserved", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, ResponseTruncated: false},
			{Agent: "reviewer", Status: StatusOK, ResponseTruncated: true},
		}
		merged := mergeResultGroup(g, nil)
		assert.True(t, merged.ResponseTruncated, "any truncated chunk must mark the whole persona as truncated")
	})
	t.Run("first chunk truncated is preserved", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, ResponseTruncated: true},
			{Agent: "reviewer", Status: StatusOK, ResponseTruncated: false},
		}
		merged := mergeResultGroup(g, nil)
		assert.True(t, merged.ResponseTruncated, "any truncated chunk must mark the whole persona as truncated")
	})
	t.Run("no truncation stays false", func(t *testing.T) {
		g := []Result{
			{Agent: "reviewer", Status: StatusOK, ResponseTruncated: false},
			{Agent: "reviewer", Status: StatusOK, ResponseTruncated: false},
		}
		merged := mergeResultGroup(g, nil)
		assert.False(t, merged.ResponseTruncated, "clean chunks should not fabricate a truncated marker")
	})
}

// TestMergeResultGroup_InvalidatesMemoOnRebuild reproduces the memo-drift bug at
// chunker.go:284: mergeResultGroup byte-copies the memoized parsedFindingCount/
// parsedFindingCountSet from chunk[0] (out := g[0]) but rebuilds out.Content from
// ALL chunks. When a chunked persona's FIRST chunk truncated to zero findings —
// the epic failover gate having cached its count as 0/set — that stale zero memo
// rides into the merged result, so ParsedFindingCount short-circuits and
// findingsFor silently drops EVERY finding the persona's other chunks produced.
func TestMergeResultGroup_InvalidatesMemoOnRebuild(t *testing.T) {
	// chunk[0]: truncated to zero findings; the failover gate demoted it and cached
	// the zero count (parsedFindingCountSet=true, parsedFindingCount=0).
	chunk0 := Result{Agent: "reviewer", Status: StatusFailed, Content: "truncated ramble, no findings", ResponseTruncated: true}
	chunk0.parsedFindingCount = 0
	chunk0.parsedFindingCountSet = true

	// chunk[1]: produced a real finding.
	chunk1 := Result{Agent: "reviewer", Status: StatusOK, Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|reviewer"}

	merged := mergeResultGroup([]Result{chunk0, chunk1}, nil)

	assert.Equal(t, 1, merged.ParsedFindingCount(),
		"merged result must re-count findings from the rebuilt content, not inherit chunk[0]'s stale zero memo")

	fr := findingsFor(merged, nil)
	assert.Len(t, fr.Findings, 1,
		"merged persona must retain the finding from chunk[1], not short-circuit findingsFor on chunk[0]'s stale zero memo")
}

// TD-048: each chunk's output is parsed on its own. Parsing the newline-joined
// Content let a chunk cut off inside a ```json block or an unfenced array swallow
// every later chunk, silently, because the count stayed above zero.
func TestMergeResultGroup_CutOffChunkDoesNotSwallowTheNext(t *testing.T) {
	obj := func(loc string) string {
		return `{"severity":"HIGH","file_line":"` + loc + `","problem":"p","fix":"f","category":"c","est_minutes":1,"evidence":"e"}`
	}
	cases := []struct {
		name   string
		chunks []string
	}{
		{"cut off inside a json fence", []string{
			"```json\n[" + obj("a.go:1") + ",\n{\"severity\":\"LOW\",\"fi",
			"[" + obj("b.go:2") + "]",
			"LOW|c.go:3|p|f|c|1|e",
		}},
		{"cut off inside an unfenced array", []string{
			"[" + obj("a.go:1") + ",\n{\"severity\":\"LOW\",\"fi",
			"[" + obj("b.go:2") + "]",
			"LOW|c.go:3|p|f|c|1|e",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var g []Result
			for _, content := range c.chunks {
				g = append(g, Result{Agent: "reviewer", Status: StatusOK, Content: content})
			}
			merged := mergeResultGroup(g, nil)
			assert.Equal(t, 3, merged.ParsedFindingCount())
			fr := findingsFor(merged, nil)
			var got []string
			for _, f := range fr.Findings {
				got = append(got, f.File)
			}
			assert.Equal(t, []string{"a.go", "b.go", "c.go"}, got)
		})
	}
}

// A chunked persona is unparseable only when it has zero parseable findings in
// total, the flag's documented meaning. One garbled chunk beside a chunk with
// findings is counted in UnparseableChunks instead, so the persona scores as
// "findings" and stays eligible for trust.
func TestMergeResultGroup_UnparseableFlagIsPersonaWide(t *testing.T) {
	good := Result{Agent: "reviewer", Status: StatusOK, Content: "HIGH|a.go:1|bug|fix|correctness|5|ev"}
	garbled := Result{Agent: "reviewer", Status: StatusOK, Content: "I looked at it.", UnparseableResponse: true}

	merged := mergeResultGroup([]Result{good, garbled}, nil)
	fr := findingsFor(merged, nil)
	st := statusFor(merged, fr)
	assert.False(t, st.UnparseableResponse, "the persona produced a parseable finding")
	assert.Equal(t, 1, st.UnparseableChunks)
	assert.Equal(t, "findings", ReviewerOutcome(st, len(fr.Findings)))

	merged = mergeResultGroup([]Result{garbled, garbled}, nil)
	fr = findingsFor(merged, nil)
	st = statusFor(merged, fr)
	assert.True(t, st.UnparseableResponse, "no chunk produced a parseable finding")
	assert.Equal(t, 2, st.UnparseableChunks)
	assert.Equal(t, "unparseable", ReviewerOutcome(st, len(fr.Findings)))
}
