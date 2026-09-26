package reconcile

import (
	"strings"
	"testing"
)

// A chunked persona's review.md delimits chunk outputs with fanout's
// chunkBoundaryLine (mirrored in justification.go). Findings are parsed per
// chunk (Result.parseFindings, TD-048), so extractSection's fence/JSON mask and
// bare-value span scans must also run per chunk: a chunk cut off inside a
// ```json block or an unfenced array must not mask the next chunk's prose, or
// that chunk's recovered finding ships with an empty justification.
func TestExtractSection_ChunkBoundaryStopsAMaskedChunk(t *testing.T) {
	lines := []string{
		"## Chunk One",
		"",
		"Findings for a.go:1 follow as JSON:",
		"```json",
		`[{"severity": "HIGH"`, // chunk cut off mid-array, no closer
		"<!-- atcr:chunk-boundary -->",
		"## Chunk Two",
		"",
		"a.go:1 the retry loop never releases the lock on error",
	}

	text, section := extractSection(lines, 8)

	if section != "Chunk Two" {
		t.Errorf("section = %q, want %q — the second chunk's heading must bound its own segment", section, "Chunk Two")
	}
	if !strings.Contains(text, "never releases the lock") {
		t.Errorf("the next chunk's prose was masked by the previous chunk's cut-off ```json block: %q", text)
	}
}

// The marker line itself is never excerpt content: an anchor cannot land on it,
// and a segment scan must treat it as pure structure.
func TestExtractSection_AnchorOnTheBoundaryLineIsSuppressed(t *testing.T) {
	lines := []string{
		"## Chunk One",
		"prose for a.go:1",
		"<!-- atcr:chunk-boundary -->",
		"## Chunk Two",
		"more prose for a.go:1",
	}

	text, _ := extractSection(lines, 2)

	if text != "" {
		t.Errorf("an anchor on the boundary line must yield no narrative, got %q", text)
	}
}

// Pin the mirrored literal: chunkBoundaryLine must not drift from fanout's
// chunkBoundaryLine (internal/fanout/chunker.go), which writes it into merged
// review.md. Kept local to avoid an import cycle (fanout is a consumer, not a
// dependency, of this package), like reviewFileName — so the pin is the literal.
func TestChunkBoundaryLineMatchesFanout(t *testing.T) {
	if chunkBoundaryLine != "<!-- atcr:chunk-boundary -->" {
		t.Fatalf("chunkBoundaryLine drifted from fanout's chunkBoundaryLine: %q", chunkBoundaryLine)
	}
}
