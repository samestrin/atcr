package stream

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/findings-format.md once said CATEGORY was "Not enforced at parse time — a
// word outside the vocabulary is accepted as written". That was true until epic
// 51.0 put CanonicalCategory on every parse path, and false from then on. This test
// keeps the user docs from saying it again while the canonicalizer is in place.
//
// It lives here rather than beside the taxonomy-table test in internal/reconcile
// because the claim it guards is about this package's parse boundary: if
// canonicalization is ever removed, the precondition below fails first and points
// at the doc to rewrite.
var parseTimeNonEnforcementClaim = regexp.MustCompile(`(?i)not\s+(?:enforced|validated|canonicali[sz]ed|normali[sz]ed)\s+at\s+parse[\s-]time`)

func TestDocs_NoClaimThatCategoryIsUnenforcedAtParseTime(t *testing.T) {
	// Precondition: canonicalization is in place. Without it the claim would be
	// true and this test would be guarding the wrong thing.
	require.Equal(t, "security", CanonicalCategory("  Security "),
		"parse-boundary canonicalization is gone; docs/findings-format.md's CATEGORY row must be rewritten before this test is")
	require.Equal(t, "other", CanonicalCategory("not-a-category"))

	files, err := filepath.Glob("../../docs/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, files, "docs/ not found from internal/stream")
	files = append(files, "../../README.md")

	for _, path := range files {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		for i, line := range strings.Split(string(body), "\n") {
			assert.Falsef(t, parseTimeNonEnforcementClaim.MatchString(line),
				"%s:%d claims CATEGORY is not enforced at parse time, but every parse path canonicalizes it (stream.CanonicalCategory): %s",
				filepath.Base(path), i+1, strings.TrimSpace(line))
		}
	}
}

func TestDocs_FindingsFormatCategoryRowDescribesCanonicalization(t *testing.T) {
	body, err := os.ReadFile("../../docs/findings-format.md")
	require.NoError(t, err)

	var row string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "| 5 | `CATEGORY` |") {
			row = line
			break
		}
	}
	require.NotEmpty(t, row, "the CATEGORY row of the Columns table is missing")

	assert.Contains(t, row, "Canonicalized at parse time, never rejected")
	assert.Contains(t, row, "`other`", "the row must say where an unrecognized word goes")
	// The out-of-vocabulary rate measures reviewer drift on the raw word; the
	// row must not imply the canonicalizer now hides it.
	assert.Contains(t, row, "`out_of_vocabulary_rate`, measured on the word as written")
}
