package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// --- Epic 36.1 T2: `atcr debt recluster` ---

// foldedByID folds the store and returns the effective record for id.
func foldedByID(t *testing.T, dir, id string) localdebt.Record {
	t.Helper()
	for _, r := range localdebt.FoldRecords(readStoreRecords(t, dir)) {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("id %s not in store", id)
	return localdebt.Record{}
}

// dashboardWontfix runs `atcr debt dashboard` and returns its Wontfix total.
func dashboardWontfix(t *testing.T, dir string) string {
	t.Helper()
	out, err := runDebt(t, "dashboard", "--dir", dir)
	require.NoError(t, err)
	m := regexp.MustCompile(`\*\*Wontfix:\*\* (\d+)`).FindStringSubmatch(out)
	require.NotNil(t, m, "dashboard prints a Wontfix total:\n%s", out)
	return m[1]
}

func TestDebtRecluster_RegisteredBesideResolve(t *testing.T) {
	var names []string
	for _, c := range newDebtCmd().Commands() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "resolve")
	assert.Contains(t, names, "recluster")
}

// The task's success criterion: the annotation is recorded, and the finding's
// status (still open) and the wontfix count are untouched.
func TestDebtRecluster_RecordsAnnotationLeavingStatusAndWontfix(t *testing.T) {
	target := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "two races")
	dismissed := openRec("2026-07-01T10:00:00Z-b", "LOW", "internal/x/b.go", 3, "style nit")
	target.Model, dismissed.Model = "claude-sonnet-4-6", "claude-sonnet-4-6"
	dir := writeDebtStore(t, target, dismissed)
	_, err := runDebt(t, "resolve", "--dir", dir, dismissed.ID, "--status", "wontfix", "--reason", "not worth it")
	require.NoError(t, err)

	beforeWontfix := dashboardWontfix(t, dir)
	beforeSignal := localdebt.AggregateQualitySignal(readStoreRecords(t, dir))
	require.NotEmpty(t, beforeSignal, "the fixture must yield quality-signal rows for the check to bite")
	beforeLen := len(readStoreRecords(t, dir))

	out, err := runDebt(t, "recluster", "--dir", dir, target.ID, "--kind", "wrong-merge", "--reason", "two distinct races")
	require.NoError(t, err)
	assert.Contains(t, out, target.ID)
	assert.Contains(t, out, "wrong-merge")

	recs := readStoreRecords(t, dir)
	require.Len(t, recs, beforeLen+1, "exactly one record appended")
	note := recs[len(recs)-1]
	assert.Equal(t, target.ID, note.ID)
	require.NotNil(t, note.ReconcilerFeedback)
	assert.Equal(t, localdebt.ReconcilerFeedback{Kind: "wrong-merge", Reason: "two distinct races"}, *note.ReconcilerFeedback)
	assert.Empty(t, note.Status, "the annotated record keeps the open status")
	assert.Empty(t, note.ResolvedAt)
	assert.Zero(t, note.Occurrences)

	eff := foldedByID(t, dir, target.ID)
	assert.Empty(t, eff.Status, "the finding is still open")
	require.NotNil(t, eff.ReconcilerFeedback)
	assert.Equal(t, beforeWontfix, dashboardWontfix(t, dir), "wontfix count untouched")
	assert.Equal(t, beforeSignal, localdebt.AggregateQualitySignal(readStoreRecords(t, dir)),
		"the reviewers' quality-signal rows are unchanged (AC2)")

	// Still on the open worklist.
	out, err = runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, target.ID)
}

// Annotating a closed finding records the verdict and leaves it closed: the
// clustering can be wrong whatever was decided about the finding.
func TestDebtRecluster_ClosedFindingKeepsItsStatus(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "by design")
	require.NoError(t, err)
	before := dashboardWontfix(t, dir)

	_, err = runDebt(t, "recluster", "--dir", dir, rec.ID, "--kind", "wrong-category", "--reason", "security, not style")
	require.NoError(t, err)

	eff := foldedByID(t, dir, rec.ID)
	assert.Equal(t, localdebt.StatusWontfix, eff.Status)
	assert.Equal(t, "by design", eff.Justification, "the dismissal's rationale is not replaced by the feedback reason")
	require.NotNil(t, eff.ReconcilerFeedback)
	assert.Equal(t, "wrong-category", eff.ReconcilerFeedback.Kind)
	assert.Equal(t, before, dashboardWontfix(t, dir))
}

func TestDebtRecluster_WrongSplitWithPartner(t *testing.T) {
	a := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "race in Append")
	b := openRec("2026-07-01T10:00:00Z-b", "HIGH", "internal/x/a.go", 14, "race in Append (write)")
	dir := writeDebtStore(t, a, b)

	_, err := runDebt(t, "recluster", "--dir", dir, a.ID, "--kind", "wrong-split", "--with", b.ID, "--reason", "same race")
	require.NoError(t, err)
	fb := foldedByID(t, dir, a.ID).ReconcilerFeedback
	require.NotNil(t, fb)
	assert.Equal(t, localdebt.ReconcilerFeedback{Kind: "wrong-split", With: b.ID, Reason: "same race"}, *fb)
	assert.Nil(t, foldedByID(t, dir, b.ID).ReconcilerFeedback, "the partner is named, not annotated")

	// --with is optional on wrong-split.
	_, err = runDebt(t, "recluster", "--dir", dir, b.ID, "--kind", "wrong-split", "--reason", "same race")
	require.NoError(t, err)
}

// Each refusal exits non-zero with a usage error and writes nothing.
func TestDebtRecluster_Refusals(t *testing.T) {
	a := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	b := openRec("2026-07-01T10:00:00Z-b", "HIGH", "internal/x/b.go", 3, "bang")
	const absent = "0123456789abcdef"
	cases := map[string]struct {
		args []string
		want string
	}{
		"unknown id":                  {[]string{absent, "--kind", "wrong-merge", "--reason", "r"}, absent},
		"no id":                       {[]string{"--kind", "wrong-merge", "--reason", "r"}, "accepts 1 arg"},
		"blank id":                    {[]string{" ", "--kind", "wrong-merge", "--reason", "r"}, "non-empty id"},
		"unknown kind":                {[]string{a.ID, "--kind", "split", "--reason", "r"}, "--kind"},
		"non-canonical kind":          {[]string{a.ID, "--kind", "Wrong-Merge", "--reason", "r"}, "--kind"},
		"missing kind":                {[]string{a.ID, "--reason", "r"}, "--kind"},
		"with on wrong-merge":         {[]string{a.ID, "--kind", "wrong-merge", "--with", b.ID, "--reason", "r"}, "--with"},
		"with on wrong-category":      {[]string{a.ID, "--kind", "wrong-category", "--with", b.ID, "--reason", "r"}, "--with"},
		"empty reason":                {[]string{a.ID, "--kind", "wrong-merge", "--reason", ""}, "--reason"},
		"blank reason":                {[]string{a.ID, "--kind", "wrong-merge", "--reason", " \t "}, "--reason"},
		"missing reason":              {[]string{a.ID, "--kind", "wrong-merge"}, "--reason"},
		"oversized reason":            {[]string{a.ID, "--kind", "wrong-merge", "--reason", strings.Repeat("x", maxReasonBytes+1)}, "--reason"},
		"malformed partner":           {[]string{a.ID, "--kind", "wrong-split", "--with", "nope", "--reason", "r"}, "--with"},
		"empty partner":               {[]string{a.ID, "--kind", "wrong-split", "--with", "", "--reason", "r"}, "--with"},
		"unknown partner":             {[]string{a.ID, "--kind", "wrong-split", "--with", absent, "--reason", "r"}, absent},
		"self partner":                {[]string{a.ID, "--kind", "wrong-split", "--with", a.ID, "--reason", "r"}, "--with"},
		"resolve's flag on recluster": {[]string{a.ID, "--kind", "wrong-merge", "--reason", "r", "--status", "wontfix"}, "unknown flag"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writeDebtStore(t, a, b)
			before := readStoreRecords(t, dir)
			_, err := runDebt(t, append([]string{"recluster", "--dir", dir}, tc.args...)...)
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err), "%v", err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, before, readStoreRecords(t, dir), "a refusal writes nothing")
		})
	}
}

// `--help` on each subcommand lists its own flags only.
func TestDebtRecluster_HelpListsOwnVerbsOnly(t *testing.T) {
	recluster, err := runDebt(t, "recluster", "--help")
	require.NoError(t, err)
	for _, f := range []string{"--kind", "--with", "--reason", "wrong-merge", "wrong-split", "wrong-category"} {
		assert.Contains(t, recluster, f)
	}
	for _, f := range []string{"--status", "--severity", "--max", "--json"} {
		assert.NotContains(t, recluster, f)
	}

	resolve, err := runDebt(t, "resolve", "--help")
	require.NoError(t, err)
	assert.Contains(t, resolve, "--status")
	for _, f := range []string{"--kind", "--with", "wrong-merge", "wrong-split", "wrong-category"} {
		assert.NotContains(t, resolve, f)
	}
}

// The annotated record copies the finding verbatim and adds the feedback, so a
// finding whose encoded record sits just under the store's read cap tips the
// annotation OVER it. Unguarded, the line is written and then skipped by every
// read path: the command reports success while the feedback is lost. It must
// be refused before the append, as `debt resolve` refuses an oversized
// resolution (TestDebtResolve_ResolutionRecordOverReadCapIsRejected).
func TestDebtRecluster_AnnotatedRecordOverReadCapIsRejected(t *testing.T) {
	base := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "")
	empty, err := json.Marshal(base)
	require.NoError(t, err)
	base.Problem = strings.Repeat("x", localdebt.MaxRecordBytes-len(empty)-1-64)
	openLine, err := json.Marshal(base)
	require.NoError(t, err)
	require.LessOrEqual(t, len(openLine)+1, localdebt.MaxRecordBytes,
		"fixture: the open record itself must still be readable")

	reason := strings.Repeat("r", 2048)
	note := localdebt.FoldRecords([]localdebt.Record{base})[0]
	note.RunID = "2026-08-05T12:00:00Z-recluster"
	note.Timestamp = "2026-08-05T12:00:00Z"
	note.Occurrences, note.FirstSeen, note.CountedThrough = 0, "", ""
	note.ReconcilerFeedback = &localdebt.ReconcilerFeedback{Kind: localdebt.FeedbackWrongMerge, Reason: reason}
	noteLine, err := json.Marshal(note)
	require.NoError(t, err)
	require.Greater(t, len(noteLine)+1, localdebt.MaxRecordBytes,
		"fixture: the annotated record must exceed the read cap")

	dir := writeDebtStore(t, base)
	before := readStoreRecords(t, dir)

	out, err := runDebt(t, "recluster", "--dir", dir, base.ID, "--kind", "wrong-merge", "--reason", reason)
	require.Error(t, err, "an annotated record over the read cap must be refused, not written invisibly")
	assert.Equal(t, exitUsage, exitCode(err), "%v", err)
	assert.NotContains(t, out, "Recorded", "must not report success for feedback that cannot be read back")
	assert.Len(t, readStoreRecords(t, dir), len(before), "a refused annotation must not append a record")
}
