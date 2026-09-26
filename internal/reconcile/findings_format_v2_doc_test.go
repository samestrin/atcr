package reconcile

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/stream"
)

// The v2 section of docs/findings-format.md claims its TOON example is the
// writer's exact output and that its envelope example is readable. These tests
// hold both claims to the code, so a writer or reader change fails here rather
// than leaving the published spec quietly wrong.

func findingsFormatV2Section(t *testing.T) string {
	t.Helper()
	doc := readRepoFile(t, "../../docs/findings-format.md")
	start := strings.Index(doc, "\n## v2 lossless stream")
	require.GreaterOrEqual(t, start, 0, "docs/findings-format.md must keep its v2 lossless stream section")
	rest := doc[start+1:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// v2DocBlocks returns the body of every fenced block in the section that
// starts with the v2 header.
func v2DocBlocks(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile("(?s)```\n(.*?)```").FindAllStringSubmatch(findingsFormatV2Section(t), -1) {
		if strings.HasPrefix(m[1], stream.VersionV2+"\n") {
			out = append(out, m[1])
		}
	}
	return out
}

func TestFindingsFormatDoc_V2TableExampleIsWriterOutput(t *testing.T) {
	var b strings.Builder
	require.NoError(t, stream.WriteSourceV2(&b, []stream.Finding{
		{Severity: "HIGH", File: "internal/fs/open.go", Line: 42, Problem: "os.O_CREATE | os.O_WRONLY drops O_TRUNC", Fix: "Use os.O_WRONLY | os.O_CREATE | os.O_TRUNC", Category: "correctness", EstMinutes: 10, Evidence: "-f, _ := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o644)\n+f, _ := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)", Reviewer: "bruce"},
		{Severity: "LOW", File: "cmd/main.go", Line: 7, Problem: `log says "done" before the write`, Category: "style", EstMinutes: 5, Reviewer: "bruce"},
	}))
	assert.Contains(t, v2DocBlocks(t), b.String(), "the TOON example must be byte-identical to WriteSourceV2's output")

	var empty strings.Builder
	require.NoError(t, stream.WriteSourceV2(&empty, nil))
	assert.Contains(t, findingsFormatV2Section(t), "`"+strings.TrimPrefix(strings.TrimSpace(empty.String()), stream.VersionV2+"\n")+"`", "the empty-body text must match the writer")
}

func TestFindingsFormatDoc_V2ExamplesParse(t *testing.T) {
	blocks := v2DocBlocks(t)
	require.Len(t, blocks, 2, "the v2 section has one TOON and one envelope example")
	for _, blk := range blocks {
		res, err := stream.ParseSource([]byte(blk))
		require.NoError(t, err, blk)
		assert.NotEmpty(t, res.Findings)
		assert.Empty(t, res.Skipped)
	}
	res, err := stream.ParseSource([]byte(blocks[1]))
	require.NoError(t, err)
	assert.Equal(t, []stream.Finding{{
		Severity: "HIGH", File: "scripts/release.sh", Line: 12,
		Problem: "The pipeline returns the exit status of tee, not of the build", Fix: "set -o pipefail\ngo build ./... | tee build.log",
		Category: "correctness", EstMinutes: 10, Evidence: "go build ./... | tee build.log", Reviewer: "host",
	}}, res.Findings, "the envelope example must read as these exact values")
}
