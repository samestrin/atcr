package reconcile_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.17 T5: the correlated-panel case end to end. Five reviewers file one
// predicate finding; it goes through RunReconcile into findings.json and through
// PersistForReconcile into the TD store, on the plain path, through a cluster
// merge, and through the fallback recompute. Each run also carries an
// independently corroborated invariant finding, which must stay HIGH with no
// reason: the discount lowers the class, not the confidence signal generally.
//
// The test is an external package because internal/localdebt imports
// internal/reconcile; it goes through the exported entry points only.

const (
	e2eQuote   = "case b: return a.ID == b.ID && a.Kind == b.Kind"
	e2eProblem = "equal() compares Name in the edited branch but the sibling branch omits it"
	// e2eSibling is the same defect worded differently, filed two lines down, so
	// reconcile keeps it as a second record that a cluster merge then unions.
	e2eSibling = "sibling case of the predicate never checks Name, so two keys differing only in Name compare equal"
	e2eIndep   = "Hash() ignores Kind, so keys equal() tells apart share a bucket"
)

var e2ePanel = []string{"bruce", "greta", "kai", "mira", "otto"}

// e2eLine is one findings.txt row: SEV|file:line|problem|fix|category|est|evidence|reviewer.
func e2eLine(file, line, problem, evidence, reviewer string) string {
	return strings.Join([]string{"MEDIUM", file + ":" + line, problem, "fix it", "invariant", "15", evidence, reviewer}, "|") + "\n"
}

// writeE2EReview writes each reviewer's findings.txt under reviewDir/sources and,
// for a reviewer named in fallback, a status.json reporting a fallback to that
// model. Every reviewer in rows gets a findings file, even an empty one.
func writeE2EReview(t *testing.T, reviewDir string, rows map[string][]string, fallback map[string]string) {
	t.Helper()
	for reviewer, lines := range rows {
		dir := filepath.Join(reviewDir, "sources", reviewer)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		body := "# atcr-findings/v1\n" + strings.Join(lines, "")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "findings.txt"), []byte(body), 0o644))
		if model := fallback[reviewer]; model != "" {
			status := `{"agent":"` + reviewer + `","status":"ok","fallback_used":true,"fallback_model":"` + model + `"}`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "status.json"), []byte(status), 0o644))
		}
	}
}

// independentRows adds the corroborated finding: bruce and kai each quote
// different code for the same defect, which is independent evidence.
func independentRows(rows map[string][]string) {
	rows["bruce"] = append(rows["bruce"], e2eLine("key.go", "90", e2eIndep, "func (k Key) Hash() uint64 { return fnv(k.ID) }", "bruce"))
	rows["kai"] = append(rows["kai"], e2eLine("key.go", "90", e2eIndep, "buckets[k.Hash()%n] = append(buckets[k.Hash()%n], k)", "kai"))
}

// runE2E reconciles reviewDir, then returns the result, the findings.json records
// and the raw findings.json bytes as written to disk.
func runE2E(t *testing.T, reviewDir string) (reconcile.Result, []reconcile.JSONFinding, string) {
	t.Helper()
	res, err := reconcile.RunReconcile(context.Background(), reviewDir, nil, reconcile.Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(reviewDir, "reconciled", reconcile.FindingsJSON))
	require.NoError(t, err)
	var onDisk []reconcile.JSONFinding
	require.NoError(t, json.Unmarshal(data, &onDisk))
	return res, onDisk, string(data)
}

// persistE2E persists res into a fresh TD store and returns its records by ID.
func persistE2E(t *testing.T, reviewDir string, res reconcile.Result) map[string]localdebt.Record {
	t.Helper()
	root := t.TempDir()
	localdebt.PersistForReconcile(reviewDir, res, localdebt.PersistOpts{Root: root})
	recs, err := localdebt.ReadAll(localdebt.DefaultDir(root), localdebt.ReadOpts{})
	require.NoError(t, err)
	byID := make(map[string]localdebt.Record, len(recs))
	for _, r := range recs {
		byID[r.ID] = r
	}
	return byID
}

func byProblem(t *testing.T, findings []reconcile.JSONFinding, problem string) reconcile.JSONFinding {
	t.Helper()
	for _, f := range findings {
		if f.Problem == problem {
			return f
		}
	}
	require.Failf(t, "finding not emitted", "no findings.json record has problem %q", problem)
	return reconcile.JSONFinding{}
}

// rawRecord returns the JSON object findings.json wrote for the finding with id,
// so a test can assert a key is absent on the wire, not just empty after decode.
func rawRecord(t *testing.T, data, id string) string {
	t.Helper()
	var raw []json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(data), &raw))
	for _, r := range raw {
		if strings.Contains(string(r), `"id": "`+id+`"`) {
			return string(r)
		}
	}
	require.Failf(t, "record not on disk", "findings.json holds no object with id %s", id)
	return ""
}

// assertDiscounted checks one discounted finding end to end: MEDIUM with the
// reason in findings.json, and a TD record whose row states the reason.
func assertDiscounted(t *testing.T, f reconcile.JSONFinding, data string, td map[string]localdebt.Record) {
	t.Helper()
	assert.Equal(t, reconcile.ConfMedium, f.Confidence, "reviewer count alone does not make the class HIGH")
	assert.Equal(t, reconcile.PanelWideReason, f.ConfidenceDiscount)
	assert.Contains(t, rawRecord(t, data, f.ID), `"confidence_discount": "`+reconcile.PanelWideReason+`"`)

	r, ok := td[f.ID]
	require.True(t, ok, "the discounted finding %s has a TD record", f.ID)
	assert.Equal(t, reconcile.ConfMedium, r.Confidence)
	assert.Equal(t, reconcile.PanelWideReason, r.ConfidenceDiscount)
	assert.Equal(t, f.Problem+" (confidence discounted: "+reconcile.PanelWideReason+")", r.RowProblem(),
		"the TD row states the confidence was discounted and why")
}

// assertIndependentHigh checks the corroborated finding: HIGH, no
// confidence_discount key in findings.json, and a TD row that is just Problem.
func assertIndependentHigh(t *testing.T, f reconcile.JSONFinding, data string, td map[string]localdebt.Record) {
	t.Helper()
	assert.ElementsMatch(t, []string{"bruce", "kai"}, f.Reviewers)
	assert.Equal(t, reconcile.ConfHigh, f.Confidence, "independent corroboration still reaches HIGH")
	assert.Empty(t, f.ConfidenceDiscount)
	assert.NotContains(t, rawRecord(t, data, f.ID), "confidence_discount")

	r, ok := td[f.ID]
	require.True(t, ok, "the independent finding %s has a TD record", f.ID)
	assert.Equal(t, reconcile.ConfHigh, r.Confidence)
	assert.Empty(t, r.ConfidenceDiscount)
	assert.Equal(t, f.Problem, r.RowProblem(), "an undiscounted TD row carries no reason")
}

// Out of a cluster merge: the five reviewers' findings collapse in reconcile's
// own merge into one record.
func TestPanelWideE2E_FiveReviewersOneFinding(t *testing.T) {
	reviewDir := t.TempDir()
	rows := map[string][]string{}
	for _, r := range e2ePanel {
		rows[r] = append(rows[r], e2eLine("key.go", "42", e2eProblem, e2eQuote, r))
	}
	independentRows(rows)
	writeE2EReview(t, reviewDir, rows, nil)

	res, onDisk, data := runE2E(t, reviewDir)
	require.Len(t, onDisk, 2, "one predicate finding and one independent finding")
	td := persistE2E(t, reviewDir, res)

	panel := byProblem(t, onDisk, e2eProblem)
	assert.Equal(t, e2ePanel, panel.Reviewers)
	assertDiscounted(t, panel, data, td)
	assertIndependentHigh(t, byProblem(t, onDisk, e2eIndep), data, td)
}

// In a cluster merge: the same five reviewers word the defect two ways, so
// reconcile emits two records (three reviewers and two), each discounted, which
// a judge's "merge" ruling then unions with MergeJSONFindings. The union of five
// correlated reviewers stays MEDIUM with the reason, and its ID is the first
// member's, which names a discounted TD row: persistence runs before the
// cluster merge and stores the members.
func TestPanelWideE2E_ClusterMergeOfTheFiveReviewers(t *testing.T) {
	reviewDir := t.TempDir()
	rows := map[string][]string{}
	for _, r := range e2ePanel[:3] {
		rows[r] = append(rows[r], e2eLine("key.go", "42", e2eProblem, e2eQuote, r))
	}
	for _, r := range e2ePanel[3:] {
		rows[r] = append(rows[r], e2eLine("key.go", "44", e2eSibling, e2eQuote, r))
	}
	independentRows(rows)
	writeE2EReview(t, reviewDir, rows, nil)

	res, onDisk, data := runE2E(t, reviewDir)
	require.Len(t, onDisk, 3, "two predicate members and one independent finding")
	td := persistE2E(t, reviewDir, res)

	first, second := byProblem(t, onDisk, e2eProblem), byProblem(t, onDisk, e2eSibling)
	assertDiscounted(t, first, data, td)
	assertDiscounted(t, second, data, td)
	indep := byProblem(t, onDisk, e2eIndep)
	assertIndependentHigh(t, indep, data, td)

	merged := reconcile.MergeJSONFindings([]reconcile.JSONFinding{first, second})
	assert.Equal(t, e2ePanel, merged.Reviewers)
	assert.Equal(t, reconcile.ConfMedium, merged.Confidence, "five correlated reviewers do not earn HIGH by union")
	assert.Equal(t, reconcile.PanelWideReason, merged.ConfidenceDiscount)
	b, err := json.Marshal(merged)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"confidence_discount":"`+reconcile.PanelWideReason+`"`)
	assert.Equal(t, first.ID, merged.ID)
	assert.Equal(t, reconcile.PanelWideReason, td[merged.ID].ConfidenceDiscount,
		"the merged record's ID names a TD row that states the discount")

	// The control: a merge ruling over the independent finding and a discounted
	// member is not panel-wide (distinct evidence), so it stays HIGH unreasoned.
	ctrl := reconcile.MergeJSONFindings([]reconcile.JSONFinding{indep, second})
	assert.Equal(t, reconcile.ConfHigh, ctrl.Confidence)
	assert.Empty(t, ctrl.ConfidenceDiscount)
}

// The fallback path: three of the five reviewers ran on one fallback model, so
// they are one voice and the predicate finding has three distinct voices.
// recomputeFallbackConfidence re-derives its confidence, and the discount
// survives because two or more voices remain. The independent finding's
// reviewers ran on their own models, so its HIGH stands.
func TestPanelWideE2E_FallbackKeepsTheDiscount(t *testing.T) {
	reviewDir := t.TempDir()
	rows := map[string][]string{}
	for _, r := range e2ePanel {
		rows[r] = append(rows[r], e2eLine("key.go", "42", e2eProblem, e2eQuote, r))
	}
	independentRows(rows)
	writeE2EReview(t, reviewDir, rows, map[string]string{"greta": "net", "mira": "net", "otto": "net"})

	res, onDisk, data := runE2E(t, reviewDir)
	require.Len(t, onDisk, 2)
	td := persistE2E(t, reviewDir, res)

	panel := byProblem(t, onDisk, e2eProblem)
	assert.Equal(t, map[string]string{"greta": "net", "mira": "net", "otto": "net"}, panel.FallbackReviewers,
		"the fallback recompute ran on this record")
	assertDiscounted(t, panel, data, td)
	assertIndependentHigh(t, byProblem(t, onDisk, e2eIndep), data, td)
}

// The fallback edge: every reviewer ran on one fallback model, so the five are
// one voice. One voice is MEDIUM on count alone, so the reason is dropped rather
// than claiming a discount that changed nothing, and the TD row carries none.
func TestPanelWideE2E_FallbackToOneVoiceDropsTheReason(t *testing.T) {
	reviewDir := t.TempDir()
	rows := map[string][]string{}
	fallback := map[string]string{}
	for _, r := range e2ePanel {
		rows[r] = append(rows[r], e2eLine("key.go", "42", e2eProblem, e2eQuote, r))
		fallback[r] = "net"
	}
	writeE2EReview(t, reviewDir, rows, fallback)

	res, onDisk, data := runE2E(t, reviewDir)
	require.Len(t, onDisk, 1)
	td := persistE2E(t, reviewDir, res)

	panel := onDisk[0]
	assert.Equal(t, reconcile.ConfMedium, panel.Confidence)
	assert.Empty(t, panel.ConfidenceDiscount)
	assert.NotContains(t, data, "confidence_discount")
	assert.Equal(t, panel.Problem, td[panel.ID].RowProblem())
}
