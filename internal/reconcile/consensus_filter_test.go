package reconcile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsensusFilter_EndToEnd proves the Epic 14.2 AC2 contract at the emit
// boundary: on a >=3-source panel an uncorroborated stylistic singleton is removed
// from findings.json, isolated into ambiguous.json, and surfaced in summary.json /
// report.md, while a corroborated consensus finding stays.
func TestConsensusFilter_EndToEnd(t *testing.T) {
	sources := []Source{
		{Name: "a", Findings: []stream.Finding{
			mf("HIGH", "foo.go", 10, "token never expires unchecked here", "guard it", "correctness", 15, "a saw it", "a"),
		}},
		{Name: "b", Findings: []stream.Finding{
			mf("HIGH", "foo.go", 10, "token never expires unchecked here", "guard it", "correctness", 15, "b saw it", "b"),
		}},
		{Name: "c", Findings: []stream.Finding{
			mf("LOW", "bar.go", 20, "unused import lingers in this file", "remove it", "style", 5, "c only", "c"),
		}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.Equal(t, 1, res.Summary.ConsensusFiltered, "one singleton filtered")

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))

	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err, "read %s", name)
		return string(b)
	}

	assert.NotContains(t, read(FindingsJSON), "unused import lingers",
		"the uncorroborated singleton is removed from findings.json")
	assert.Contains(t, read(AmbiguousJSON), "unused import lingers",
		"the singleton is routed to the ambiguous.json sidecar")
	assert.Contains(t, read(FindingsJSON), "token never expires",
		"the corroborated consensus finding stays in findings.json")
	assert.Contains(t, read(SummaryJSON), "\"consensus_filtered\": 1",
		"summary.json records the consensus-filtered count")
	assert.Contains(t, read(ReportMD), "Consensus filtered: 1",
		"report.md surfaces the consensus-filtered count")
}

// TestConsensusFilter_FlattenedPoolEndToEnd mirrors production discovery, where all
// pool personas land in ONE "pool" source distinguished only by REVIEWER. The filter
// must still activate off the distinct-reviewer count (3 here) even though there is a
// single source directory — the scenario a len(sources) gate would wrongly skip.
func TestConsensusFilter_FlattenedPoolEndToEnd(t *testing.T) {
	sources := []Source{
		{Name: "pool", Findings: []stream.Finding{
			mf("HIGH", "foo.go", 10, "token never expires unchecked here", "guard it", "correctness", 15, "bruce saw it", "bruce"),
			mf("HIGH", "foo.go", 10, "token never expires unchecked here", "guard it", "correctness", 15, "greta saw it", "greta"),
			mf("LOW", "bar.go", 20, "unused import lingers in this file", "remove it", "style", 5, "kai only", "kai"),
		}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	require.Equal(t, 1, res.Summary.ConsensusFiltered, "filter activates on 3 distinct reviewers in one source")

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))
	b, err := os.ReadFile(filepath.Join(dir, FindingsJSON))
	require.NoError(t, err)
	assert.NotContains(t, string(b), "unused import lingers",
		"the singleton is dropped from a single flattened pool source")
}

// TestConsensusFilter_TwoSourcePanelUnfiltered guards the panel-size floor at the
// emit boundary: with only two sources the filter is inert, matching the documented
// single-API-key (host + 1 pool) workflow.
func TestConsensusFilter_TwoSourcePanelUnfiltered(t *testing.T) {
	sources := []Source{
		{Name: "a", Findings: []stream.Finding{
			mf("LOW", "bar.go", 20, "unused import lingers in this file", "remove it", "style", 5, "a only", "a"),
		}},
		{Name: "b", Findings: []stream.Finding{
			mf("HIGH", "foo.go", 10, "token never expires unchecked here", "guard it", "correctness", 15, "b saw it", "b"),
		}},
	}
	res := Reconcile(sources, Options{ReconciledAt: time.Unix(1700000000, 0).UTC()})
	assert.Equal(t, 0, res.Summary.ConsensusFiltered, "filter inert below 3 sources")

	dir := t.TempDir()
	require.NoError(t, Emit(dir, res))
	b, err := os.ReadFile(filepath.Join(dir, FindingsJSON))
	require.NoError(t, err)
	assert.Contains(t, string(b), "unused import lingers",
		"the singleton stays in findings.json on a 2-source panel")
}
