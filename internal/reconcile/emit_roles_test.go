package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewerLabels(t *testing.T) {
	roles := map[string]string{"nora": "threat-model mapper", "greta": "algorithmic correctness reviewer"}
	assert.Equal(t,
		[]string{"nora (threat-model mapper)", "host", "greta (algorithmic correctness reviewer)"},
		ReviewerLabels([]string{"nora", "host", "greta"}, roles),
		"a reviewer with a role reads name (role); one with none keeps its bare name")
	assert.Equal(t, []string{"nora", "host"}, ReviewerLabels([]string{"nora", "host"}, nil),
		"no roles map leaves every name bare")
	assert.Equal(t, []string{"kai"}, ReviewerLabels([]string{"kai"}, map[string]string{"kai": "  "}),
		"a blank role is no role")
	assert.Empty(t, ReviewerLabels(nil, roles))
}

// The reconciled report.md names each reviewer as name (role) on the findings
// line, in-scope and out-of-scope alike, and keeps a reviewer with no persona
// (host) bare. findings.json is the machine contract and stays bare-name.
func TestEmit_ReportMDShowsReviewerRoles(t *testing.T) {
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "alpha", "f", "sec", 10, "e", "nora"),
		mf("HIGH", "a.go", 1, "alpha", "f", "sec", 10, "e", "host"),
		mf("LOW", "z.go", 3, "old", "f", CategoryOutOfScope, 10, "e", "nora"),
	}}}
	res := Reconcile(sources, recAt())
	res.ReviewerRoles = map[string]string{"nora": "threat-model mapper"}

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))
	md, err := os.ReadFile(filepath.Join(dir, ReportMD))
	require.NoError(t, err)
	out := string(md)
	assert.Contains(t, out, "reviewers: host, nora (threat-model mapper)\n")
	oos := out[strings.Index(out, "## Out-of-Scope Findings"):]
	assert.Contains(t, oos, "reviewers: nora (threat-model mapper)\n", "the out-of-scope list carries the label too")

	js, err := os.ReadFile(filepath.Join(dir, FindingsJSON))
	require.NoError(t, err)
	assert.NotContains(t, string(js), "threat-model mapper", "findings.json stays bare-name")

	var rendered strings.Builder
	require.NoError(t, RenderMarkdown(&rendered, res))
	assert.Equal(t, out, rendered.String(), "RenderMarkdown and Emit write the same report.md")
}

// With no roles the report.md bytes are those of a plain Reconcile result.
func TestEmit_NoRolesReportMDUnchanged(t *testing.T) {
	sources := []Source{{Name: "pool", Findings: []stream.Finding{
		mf("HIGH", "a.go", 1, "alpha", "f", "sec", 10, "e", "nora"),
	}}}
	plain := Reconcile(sources, recAt())
	empty := Reconcile(sources, recAt())
	empty.ReviewerRoles = map[string]string{}
	var a, b strings.Builder
	require.NoError(t, RenderMarkdown(&a, plain))
	require.NoError(t, RenderMarkdown(&b, empty))
	assert.Equal(t, a.String(), b.String())
	assert.Contains(t, a.String(), "reviewers: nora\n")
}

// RunReconcile threads the roles it is given into the report.md it writes,
// and the Result it returns carries them for a later RenderMarkdown.
func TestRunReconcile_WithReviewerRolesLabelsReportMD(t *testing.T) {
	root, reviewDir := buildPostMortemReplay(t)
	roles := map[string]string{"greta": "algorithmic correctness reviewer"}

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		Root:         root,
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
	}, WithReviewerRoles(roles))
	require.NoError(t, err)
	assert.Equal(t, roles, res.ReviewerRoles)

	md, err := os.ReadFile(filepath.Join(reviewDir, reconciledSubdir, ReportMD))
	require.NoError(t, err)
	assert.Contains(t, string(md), "greta (algorithmic correctness reviewer)")
	assert.NotContains(t, string(md), "mira (", "a reviewer missing from the map stays bare")
}
