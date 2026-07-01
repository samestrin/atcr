package fanout

import (
	"strings"
	"testing"

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
