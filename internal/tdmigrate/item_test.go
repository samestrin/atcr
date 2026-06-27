package tdmigrate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleItem() Item {
	return Item{
		ID:       "TD-0007",
		Order:    7,
		Section:  "[2026-06-23] From Sprint: 8.0_reconciler_library",
		Date:     "2026-06-23",
		Group:    "2",
		Status:   "deferred",
		Severity: "HIGH",
		File:     "internal/reconcile/discover.go:25",
		Problem:  "First paragraph describing the problem.\n\nA second paragraph with a colon: like this, and a path internal/x.go:42.",
		Fix:      "Step one.\nStep two.",
		Category: "correctness",
		// EstMinutes intentionally "0" to prove exact string preservation.
		EstMinutes:    "0",
		Source:        "execute-sprint",
		Reviewers:     "execute-sprint, claude",
		Confidence:    "MEDIUM",
		HasReviewCols: true,
	}
}

func TestRenderParseItemFile_RoundTrip(t *testing.T) {
	in := sampleItem()

	rendered, err := RenderItemFile(in)
	require.NoError(t, err)
	require.NotEmpty(t, rendered)

	// Frontmatter must be delimited and the multi-line body must survive verbatim.
	assert.True(t, strings.HasPrefix(rendered, "---\n"), "frontmatter must open with ---")
	assert.Contains(t, rendered, "## Problem")
	assert.Contains(t, rendered, "## Fix")
	assert.Contains(t, rendered, "A second paragraph with a colon: like this")

	out, err := ParseItemFile(rendered)
	require.NoError(t, err)
	assert.Equal(t, in, out, "Item must round-trip through file form unchanged")
}

func TestParseItemFile_OmittedReviewerFields(t *testing.T) {
	in := sampleItem()
	in.HasReviewCols = false
	in.Reviewers = ""
	in.Confidence = ""

	rendered, err := RenderItemFile(in)
	require.NoError(t, err)

	out, err := ParseItemFile(rendered)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestFilename_IDAndSlug(t *testing.T) {
	it := Item{ID: "TD-0001", Problem: "execToolPatterns uses substring matching so names reject"}
	name := it.Filename()
	assert.True(t, strings.HasPrefix(name, "TD-0001-"), "filename starts with id: %s", name)
	assert.True(t, strings.HasSuffix(name, ".md"), "filename ends with .md: %s", name)
	assert.NotContains(t, name, " ", "slug must not contain spaces: %s", name)
	slug := strings.TrimSuffix(strings.TrimPrefix(name, "TD-0001-"), ".md")
	assert.Equal(t, strings.ToLower(slug), slug, "slug must be lowercase: %s", slug)
}

func TestFilename_EmptyProblemFallsBackToID(t *testing.T) {
	it := Item{ID: "TD-0002", Problem: ""}
	assert.Equal(t, "TD-0002.md", it.Filename())
}

func TestParseItemFile_RejectsMissingFrontmatter(t *testing.T) {
	_, err := ParseItemFile("no frontmatter here\n## Problem\n\nx\n")
	require.Error(t, err)
}

func TestStatusBoxMapping_RoundTrips(t *testing.T) {
	for status, box := range map[string]string{"open": "[ ]", "resolved": "[x]", "deferred": "[/]"} {
		gotBox, err := statusToBox(status)
		require.NoError(t, err)
		assert.Equal(t, box, gotBox)

		gotStatus, err := boxToStatus(box)
		require.NoError(t, err)
		assert.Equal(t, status, gotStatus)
	}

	_, err := statusToBox("bogus")
	assert.Error(t, err)
	_, err = boxToStatus("[?]")
	assert.Error(t, err)
}
