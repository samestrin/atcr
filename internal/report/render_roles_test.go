package report

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The markdown report names a reviewer with a role as name (role) and keeps a
// reviewer with no persona (host) bare (Epic 83.0 T5).
func TestRenderMarkdownWithRoles_LabelsReviewers(t *testing.T) {
	roles := map[string]string{"greta": "algorithmic correctness reviewer", "otto": "style <b>reviewer</b>"}
	var b strings.Builder
	require.NoError(t, RenderMarkdownWithRoles(&b, sample(), reconcile.DisagreementsFile{}, ContestedReport{}, roles))
	out := b.String()
	assert.Contains(t, out, "reviewers: greta (algorithmic correctness reviewer), host\n")
	assert.Contains(t, out, "reviewers: otto (style &lt;b&gt;reviewer&lt;/b&gt;)\n",
		"a role read from a user prompt file is escaped like every other free-text field")
}

// No roles is the report as before: the same bytes as RenderMarkdownWithContested.
func TestRenderMarkdownWithRoles_NilRolesMatchesPlainReport(t *testing.T) {
	var plain, labelled strings.Builder
	require.NoError(t, RenderMarkdownWithContested(&plain, sample(), reconcile.DisagreementsFile{}, ContestedReport{}))
	require.NoError(t, RenderMarkdownWithRoles(&labelled, sample(), reconcile.DisagreementsFile{}, ContestedReport{}, nil))
	assert.Equal(t, plain.String(), labelled.String())
}
