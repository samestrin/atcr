package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// tdTable renders a TD README with one section holding rows. Each row is
// marker, problem cell (which carries any debt:<id> token).
func tdTable(rows ...[2]string) string {
	var b strings.Builder
	b.WriteString("# Technical Debt\n\n## Stats\n\n| Severity | Open |\n|---|---|\n| [ ] | 1 |\n\n")
	b.WriteString("### [2026-10-22] From: fixture\n\n")
	b.WriteString("| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Attempts |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	for i, r := range rows {
		fmt.Fprintf(&b, "| 1 | %s | LOW | f.go:%d | %s | fix | c | 5 | paperclip-builder | 0 |\n", r[0], i+1, r[1])
	}
	return b.String()
}

func writeTD(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "README.md")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// foldedStatus returns the folded status of every id in the store.
func foldedStatus(t *testing.T, dir string) map[string]string {
	t.Helper()
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	out := map[string]string{}
	for _, r := range localdebt.FoldRecords(recs) {
		out[r.ID] = r.Status
	}
	return out
}

func storeRecordCount(t *testing.T, dir string) int {
	t.Helper()
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	return len(recs)
}

func TestDebtSyncOutcomes_RecordsEachMarkerStateAndIsIdempotent(t *testing.T) {
	resolved := openRec("2026-10-01T00:00:00Z", "LOW", "a.go", 1, "resolved one")
	resolvedUpper := openRec("2026-10-01T00:00:00Z", "LOW", "a.go", 2, "resolved upper")
	unrepro := openRec("2026-10-01T00:00:00Z", "LOW", "a.go", 3, "unreproducible one")
	open := openRec("2026-10-01T00:00:00Z", "LOW", "a.go", 4, "still open")
	deferredRow := openRec("2026-10-01T00:00:00Z", "LOW", "a.go", 5, "deferred row")
	dir := writeDebtStore(t, resolved, resolvedUpper, unrepro, open, deferredRow)
	before := storeRecordCount(t, dir)

	td := writeTD(t, tdTable(
		[2]string{"[x]", "fixed it debt:" + resolved.ID},
		[2]string{"[X]", "fixed upper debt:" + resolvedUpper.ID},
		[2]string{"[-]", "could not make it happen debt:" + unrepro.ID},
		[2]string{"[ ]", "open debt:" + open.ID},
		[2]string{"[/]", "deferred debt:" + deferredRow.ID},
		[2]string{"[x]", "closed with no token"},
		[2]string{"[x]", "unknown debt:ffffffffffffffff"},
	))

	out, err := runDebt(t, "sync-outcomes", "--store", dir, "--td", td)
	require.NoError(t, err, out)

	st := foldedStatus(t, dir)
	assert.Equal(t, localdebt.StatusResolved, st[resolved.ID])
	assert.Equal(t, localdebt.StatusResolved, st[resolvedUpper.ID])
	assert.Equal(t, localdebt.StatusUnreproducible, st[unrepro.ID])
	assert.Equal(t, "", st[open.ID], "a [ ] row records nothing")
	assert.Equal(t, "", st[deferredRow.ID], "a [/] row records nothing")
	_, inStore := st["ffffffffffffffff"]
	assert.False(t, inStore, "an unknown id is never created")
	assert.Equal(t, before+3, storeRecordCount(t, dir))

	assert.Contains(t, out, "appended 3")
	assert.Contains(t, out, "line 16: debt:"+open.ID+" marker [ ] is not a terminal outcome")
	assert.Contains(t, out, "line 17: debt:"+deferredRow.ID+" marker [/] is not a terminal outcome")
	assert.Contains(t, out, "line 19: debt:ffffffffffffffff is not in the store")
	assert.Contains(t, out, "1 row(s) carry no debt:<id> token")

	// The unreproducible record carries the reason a non-resolved status requires,
	// naming where it came from.
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	for _, r := range recs {
		if r.ID == unrepro.ID && r.Status == localdebt.StatusUnreproducible {
			assert.Contains(t, r.Justification, "atcr debt sync-outcomes")
			assert.Contains(t, r.Justification, "could not make it happen")
			assert.Equal(t, []string{"claude"}, r.Reviewers, "attribution is carried like debt resolve")
		}
	}

	// A second run appends nothing.
	out2, err := runDebt(t, "sync-outcomes", "--store", dir, "--td", td)
	require.NoError(t, err, out2)
	assert.Equal(t, before+3, storeRecordCount(t, dir), "re-running the sync must append nothing")
	assert.Contains(t, out2, "appended 0")
	assert.Contains(t, out2, "already recorded 3")
}

func TestDebtSyncOutcomes_NeverGuesses(t *testing.T) {
	ts := "2026-10-01T00:00:00Z"
	otherStatus := openRec(ts, "LOW", "b.go", 1, "closed differently")
	conflict := openRec(ts, "LOW", "b.go", 2, "two rows disagree")
	dup := openRec(ts, "LOW", "b.go", 3, "two rows agree")
	two1 := openRec(ts, "LOW", "b.go", 4, "first of two")
	two2 := openRec(ts, "LOW", "b.go", 5, "second of two")
	regressed := openRec(ts, "LOW", "b.go", 6, "resolved then re-detected")

	closedOther := otherStatus
	closedOther.Status, closedOther.RunID, closedOther.Timestamp = localdebt.StatusWontfix, "2026-10-02T00:00:00Z", "2026-10-02T00:00:00Z"
	closedOther.Justification = "accepted pattern"
	regressedClosed := regressed
	regressedClosed.Status, regressedClosed.RunID, regressedClosed.Timestamp = localdebt.StatusResolved, "2026-10-02T00:00:00Z", "2026-10-02T00:00:00Z"
	regressedAgain := regressed
	regressedAgain.RunID, regressedAgain.Timestamp = "2026-10-03T00:00:00Z", "2026-10-03T00:00:00Z"

	dir := writeDebtStore(t, otherStatus, closedOther, conflict, dup, two1, two2, regressed, regressedClosed, regressedAgain)
	before := storeRecordCount(t, dir)

	td := writeTD(t, tdTable(
		[2]string{"[x]", "debt:" + otherStatus.ID},
		[2]string{"[x]", "debt:" + conflict.ID},
		[2]string{"[-]", "debt:" + conflict.ID},
		[2]string{"[x]", "debt:" + dup.ID},
		[2]string{"[x]", "dup again debt:" + dup.ID},
		[2]string{"[x]", "debt:" + two1.ID + " debt:" + two2.ID},
		[2]string{"[x]", "debt:" + regressed.ID},
		[2]string{"[x]", "debt:12345"},
		[2]string{"[?]", "debt:" + two1.ID},
	))

	out, err := runDebt(t, "sync-outcomes", "--store", dir, "--td", td)
	require.NoError(t, err, out)

	st := foldedStatus(t, dir)
	assert.Equal(t, localdebt.StatusWontfix, st[otherStatus.ID], "an id closed as another status is left alone")
	assert.Equal(t, "", st[conflict.ID], "rows that disagree on an id record nothing")
	assert.Equal(t, localdebt.StatusResolved, st[dup.ID], "rows that agree record once")
	assert.Equal(t, "", st[two1.ID], "a row naming two ids records neither")
	assert.Equal(t, "", st[two2.ID])
	assert.Equal(t, "", st[regressed.ID], "a re-detection after the recorded outcome is not re-closed")
	assert.Equal(t, before+1, storeRecordCount(t, dir), "only the agreeing duplicate appends")

	assert.Contains(t, out, "debt:"+otherStatus.ID+" is already closed as wontfix; the row says resolved")
	assert.Contains(t, out, "debt:"+conflict.ID+" rows disagree (lines 14, 15)")
	assert.Contains(t, out, "line 18: names more than one id")
	assert.Contains(t, out, "debt:"+regressed.ID+" was re-detected after it was recorded resolved")
	assert.Contains(t, out, "line 20: malformed token debt:12345")
	assert.Contains(t, out, "line 21: unknown marker [?]")
}

func TestDebtSyncOutcomes_FlagValidation(t *testing.T) {
	dir := t.TempDir()
	_, err := runDebt(t, "sync-outcomes", "--store", dir)
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))

	td := writeTD(t, tdTable())
	_, err = runDebt(t, "sync-outcomes", "--store", dir, "--td", td, "--report")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))

	_, err = runDebt(t, "sync-outcomes", "--store", dir, "--td", filepath.Join(dir, "missing.md"))
	require.Error(t, err)
}

// gateStore writes n personas with outcomes terminal records each. Persona p0's
// outcomes are split across two models, which the report must fold.
func gateStore(t *testing.T, n, outcomes int) string {
	t.Helper()
	dir := t.TempDir()
	statuses := []string{localdebt.StatusResolved, localdebt.StatusWontfix, localdebt.StatusUnreproducible, localdebt.StatusAttemptsExhausted}
	for p := 0; p < n; p++ {
		for i := 0; i < outcomes; i++ {
			r := openRec("2026-10-01T00:00:00Z", "LOW", fmt.Sprintf("p%d.go", p), i+1, "finding")
			r.Reviewers = []string{fmt.Sprintf("p%d", p)}
			r.Model = "m1"
			if p == 0 && i%2 == 1 {
				r.Model = "m2"
			}
			r.Status = statuses[i%len(statuses)]
			require.NoError(t, localdebt.Append(dir, r))
		}
	}
	// A persona under the floor and a deferred outcome, neither of which counts.
	low := openRec("2026-10-01T00:00:00Z", "LOW", "low.go", 1, "finding")
	low.Reviewers, low.Model, low.Status = []string{"lowcount"}, "m1", localdebt.StatusResolved
	require.NoError(t, localdebt.Append(dir, low))
	def := openRec("2026-10-01T00:00:00Z", "LOW", "def.go", 1, "finding")
	def.Reviewers, def.Model, def.Status = []string{"lowcount"}, "m1", localdebt.StatusDeferred
	require.NoError(t, localdebt.Append(dir, def))
	return dir
}

func TestDebtSyncOutcomes_ReportGate(t *testing.T) {
	t.Run("10 personas at 20 meets the gate", func(t *testing.T) {
		dir := gateStore(t, 10, 20)
		before := storeRecordCount(t, dir)
		out, err := runDebt(t, "sync-outcomes", "--store", dir, "--report")
		require.NoError(t, err, out)
		assert.Contains(t, out, "GATE MET")
		assert.NotContains(t, out, "GATE NOT MET")
		assert.Regexp(t, regexp.MustCompile(`(?m)^p0\s+20\s+5\s+5\s+5\s+5$`), out, "p0's two models fold into one row")
		assert.Regexp(t, regexp.MustCompile(`(?m)^lowcount\s+1\s`), out)
		assert.Equal(t, before, storeRecordCount(t, dir), "--report is read-only")
	})
	t.Run("9 personas at 20 does not", func(t *testing.T) {
		dir := gateStore(t, 9, 20)
		out, err := runDebt(t, "sync-outcomes", "--store", dir, "--report")
		require.NoError(t, err, "--report exits 0 when the gate is not met")
		assert.Contains(t, out, "GATE NOT MET: 9 of 10 personas at 20")
	})
	t.Run("an empty store reports zero", func(t *testing.T) {
		out, err := runDebt(t, "sync-outcomes", "--store", t.TempDir(), "--report")
		require.NoError(t, err)
		assert.Contains(t, out, "GATE NOT MET: 0 of 10 personas at 20")
	})
}

// The gate floor is the scorecard's minConfirmationOutcomes, which is unexported
// there. Pin the copy here to the literal in trust.go so the two cannot drift.
func TestDebtSyncOutcomes_GateFloorMatchesTheScorecard(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "internal", "scorecard", "trust.go"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^const minConfirmationOutcomes = (\d+)$`).FindSubmatch(src)
	require.NotNil(t, m, "trust.go must still declare minConfirmationOutcomes as a literal")
	assert.Equal(t, string(m[1]), fmt.Sprint(gateMinOutcomes))
	assert.Equal(t, 10, gateMinPersonas, "trust.go's re-measurement trigger names 10+ distinct personas")
}
