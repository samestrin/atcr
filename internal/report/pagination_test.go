package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// synthAXI builds a synthetic AXI findings payload with rows data rows plus the
// one array-header line, mirroring renderAXI's shape (one physical line per row,
// trailing newline). Total physical line count is rows+1. Rows are distinct so a
// truncation cut point can be located exactly.
func synthAXI(rows int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "findings[%d|]{severity|file:line|problem}:\n", rows)
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, "  LOW|a.go:%d|problem-%d\n", i, i)
	}
	return []byte(b.String())
}

// physLines counts \n-terminated physical lines in an AXI payload (renderAXI
// always terminates every line, so this equals the emitted line count).
func physLines(p []byte) int {
	return bytes.Count(p, []byte("\n"))
}

// TestPaginateAXI_UnderCapPassThrough is AC 03-01 Scenario 1: a payload under the
// cap is emitted unmodified — byte-identical to the unwrapped renderer output,
// no lines dropped, not truncated.
func TestPaginateAXI_UnderCapPassThrough(t *testing.T) {
	in := synthAXI(119) // 119 rows + 1 header = 120 physical lines
	require.Equal(t, 120, physLines(in))
	out, truncated, _ := PaginateAXI(in, AXIMaxLinesDefault)
	assert.False(t, truncated, "a 120-line payload under the 500-line cap must not truncate")
	assert.Equal(t, in, out, "under-cap output must be byte-identical to the unwrapped renderer output")
}

// TestPaginateAXI_OverCapTruncatesAtRowBoundary is AC 03-01 Scenario 2 + Edge
// Case 4: a payload over the cap emits exactly cap physical lines, and every
// emitted line is a complete row (no row split mid-line).
func TestPaginateAXI_OverCapTruncatesAtRowBoundary(t *testing.T) {
	in := synthAXI(1199) // 1199 rows + 1 header = 1200 physical lines
	require.Equal(t, 1200, physLines(in))
	out, truncated, _ := PaginateAXI(in, AXIMaxLinesDefault)
	assert.True(t, truncated, "a 1200-line payload over the 500-line cap must truncate")
	assert.Equal(t, AXIMaxLinesDefault, physLines(out), "exactly cap physical lines of content emitted")
	// Every emitted line is a complete row: it ends at a newline and no line is a
	// partial fragment of the next row. Splitting on newline yields no empty
	// interior segment and the header stays intact as line 1.
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	assert.True(t, strings.HasPrefix(lines[0], "findings[1199|]{"), "header row preserved as line 1")
	for i, l := range lines[1:] {
		assert.Truef(t, strings.HasPrefix(l, "  LOW|a.go:"), "emitted data line %d is a whole row, not a fragment: %q", i, l)
	}
}

// TestPaginateAXI_Deterministic is AC 03-01 Scenario 3: truncating the same
// payload twice yields byte-identical output (same rows dropped, same cut point).
func TestPaginateAXI_Deterministic(t *testing.T) {
	in := synthAXI(1199)
	out1, tr1, tot1 := PaginateAXI(in, AXIMaxLinesDefault)
	out2, tr2, tot2 := PaginateAXI(in, AXIMaxLinesDefault)
	assert.Equal(t, out1, out2, "repeated truncation of identical input must be byte-identical")
	assert.Equal(t, tr1, tr2)
	assert.Equal(t, tot1, tot2)
}

// TestPaginateAXI_ExactlyAtCapNotTruncated is AC 03-01 Edge Case 1: a payload of
// exactly cap physical lines is NOT truncated (inclusive boundary).
func TestPaginateAXI_ExactlyAtCapNotTruncated(t *testing.T) {
	in := synthAXI(AXIMaxLinesDefault - 1) // 499 rows + 1 header = 500 physical lines
	require.Equal(t, AXIMaxLinesDefault, physLines(in))
	out, truncated, _ := PaginateAXI(in, AXIMaxLinesDefault)
	assert.False(t, truncated, "exactly cap lines must not truncate (inclusive boundary)")
	assert.Equal(t, in, out, "exactly-at-cap output is byte-identical")
}

// TestPaginateAXI_OneOverCapTruncated is AC 03-01 Edge Case 2: a payload one line
// over the cap truncates to exactly cap physical lines.
func TestPaginateAXI_OneOverCapTruncated(t *testing.T) {
	in := synthAXI(AXIMaxLinesDefault) // 500 rows + 1 header = 501 physical lines
	require.Equal(t, AXIMaxLinesDefault+1, physLines(in))
	out, truncated, _ := PaginateAXI(in, AXIMaxLinesDefault)
	assert.True(t, truncated, "one line over the cap must truncate")
	assert.Equal(t, AXIMaxLinesDefault, physLines(out), "exactly cap physical lines emitted")
}

// TestPaginateAXI_EmptyPayloadNoOp is AC 03-01 Edge Case 3: a well-formed
// zero-findings payload passes through unmodified and is not truncated.
func TestPaginateAXI_EmptyPayloadNoOp(t *testing.T) {
	in := []byte("findings[0]:\n")
	out, truncated, total := PaginateAXI(in, AXIMaxLinesDefault)
	assert.False(t, truncated, "empty payload must not truncate")
	assert.Equal(t, in, out, "empty payload passes through byte-identical")
	assert.Equal(t, 0, total, "empty payload has zero true elements")
}

// TestPaginateAXI_NonPositiveMaxLinesClampsToDefault covers the defensive
// contract (3.2.A adversarial): a non-positive cap must never panic or drop the
// header — it clamps to AXIMaxLinesDefault (fail-open, mirroring AC 03-03).
func TestPaginateAXI_NonPositiveMaxLinesClampsToDefault(t *testing.T) {
	in := synthAXI(1199) // 1200 physical lines, over the default cap
	for _, cap := range []int{0, -1, -1000} {
		t.Run(fmt.Sprintf("cap=%d", cap), func(t *testing.T) {
			require.NotPanics(t, func() {
				out, truncated, total := PaginateAXI(in, cap)
				assert.True(t, truncated, "over-default payload truncates under the clamped default")
				assert.Equal(t, AXIMaxLinesDefault, physLines(out), "clamped to the default cap")
				assert.Equal(t, 1199, total, "true total preserved regardless of cap")
				assert.True(t, strings.HasPrefix(string(out), "findings[1199|]{"), "header line never dropped")
			})
		})
	}
	// A small payload with a non-positive cap must still pass through untouched.
	small := synthAXI(3)
	out, truncated, _ := PaginateAXI(small, 0)
	assert.False(t, truncated)
	assert.Equal(t, small, out, "under-default payload is byte-identical even with a non-positive cap")
}

// TestRenderAXIPaginated_EmitsTruncatedFlag covers the shared CLI emission step
// (3.2.A adversarial): the `truncated: <bool>` closing line is appended in every
// payload, false when under the cap and true when the content was capped. The
// full AC 03-02 header-N contract is pinned in the 03-02 suite (task 3.4).
func TestRenderAXIPaginated_EmitsTruncatedFlag(t *testing.T) {
	under := sample() // 2 findings, well under any cap
	var b strings.Builder
	require.NoError(t, RenderAXIPaginated(&b, under, AXIMaxLinesDefault))
	assert.True(t, strings.HasSuffix(b.String(), "truncated: false\n"), "under-cap payload ends with truncated: false")
	assert.Contains(t, b.String(), "findings[2|]{", "findings payload precedes the flag")

	// Over-cap: many findings with a tiny cap forces truncation → truncated: true.
	many := make([]reconcile.JSONFinding, 50)
	for i := range many {
		many[i] = reconcile.JSONFinding{Severity: "LOW", File: "a.go", Line: i, Problem: "p", Confidence: "LOW"}
	}
	var b2 strings.Builder
	require.NoError(t, RenderAXIPaginated(&b2, many, 10))
	assert.True(t, strings.HasSuffix(b2.String(), "truncated: true\n"), "over-cap payload ends with truncated: true")
	// The content (excluding the trailing flag line) is capped to exactly maxLines.
	content := strings.TrimSuffix(b2.String(), "truncated: true\n")
	assert.Equal(t, 10, physLines([]byte(content)), "content capped to exactly maxLines, flag is the closing structure")
}

// TestPaginateAXI_NeverErrorsRegardlessOfSize is AC 03-01 Error Scenario 1: the
// cap is a bounded, unconditionally-succeeding transform — its signature returns
// no error, so no payload size can fail the run. This test documents that
// contract by exercising a large payload and asserting a clean result.
func TestPaginateAXI_NeverErrorsRegardlessOfSize(t *testing.T) {
	in := synthAXI(20000)
	out, truncated, total := PaginateAXI(in, AXIMaxLinesDefault)
	assert.True(t, truncated)
	assert.Equal(t, AXIMaxLinesDefault, physLines(out))
	assert.Equal(t, 20000, total, "true total reflects the pre-truncation element count")
}
