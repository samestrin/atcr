package debate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
)

// harness wraps a scripted completer + fake dispatcher as a harnessFunc, bypassing
// the real snapshot/provider.
func harness(cc fanout.ChatCompleter) harnessFunc {
	return func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return cc, &fakeDispatcher{}, nil, nil
	}
}

func errorHarness(err error) harnessFunc {
	return func() (fanout.ChatCompleter, Dispatcher, func(), error) {
		return nil, nil, nil, err
	}
}

// reviewDirWith builds a reviewDir fixture (reconciled/findings.json + manifest)
// from the given findings and returns its path.
func reviewDirWith(t *testing.T, findings []reconcile.JSONFinding) string {
	t.Helper()
	dir := t.TempDir()
	recon := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	data, err := json.MarshalIndent(findings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, reconcile.FindingsJSON), append(data, '\n'), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile),
		[]byte(`{"base":"a","head":"deadbeef","stages":["review","verify"]}`), 0o644))
	return dir
}

// debateRoster registers a full distinct roster (reviewer/skeptic/judge).
func debateRoster() *registry.Registry {
	reg := rosterReg(map[string][2]string{
		"alice": {"model-a", registry.RoleReviewer},
		"bob":   {"model-b", registry.RoleSkeptic},
		"carol": {"model-c", registry.RoleJudge},
	})
	// SupportsFC so the tool loop runs Chat and consumes scripted turns in order.
	for n, a := range reg.Agents {
		a.SupportsFC = true
		reg.Agents[n] = a
	}
	return reg
}

// A severity_split finding: MEDIUM-vs-HIGH disagreement, one crediting reviewer.
func splitFinding() reconcile.JSONFinding {
	return reconcile.JSONFinding{
		Severity: "HIGH", File: "a.go", Line: 10, Problem: "nil deref", Fix: "guard",
		Category: "correctness", Reviewers: []string{"alice"}, Confidence: "HIGH",
		Disagreement: "MEDIUM vs HIGH",
	}
}

func TestRunDebate_UpholdWritesConfirmedVerdict(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"uphold","settled_severity":"HIGH","reasoning":"evidence holds"}`},
	}}

	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Selected)
	assert.Equal(t, 1, res.Upheld)

	f := readFindings(t, dir)
	require.Len(t, f, 1)
	require.NotNil(t, f[0].Verification)
	assert.Equal(t, reconcile.VerdictConfirmed, f[0].Verification.Verdict)
	assert.True(t, f[0].Verification.ChallengeSurvived)
	assert.Equal(t, reconcile.ConfidenceVerified, f[0].Confidence)
	assert.Equal(t, "carol", f[0].Verification.Skeptic) // judge attributed
}

func TestRunDebate_OverturnRefutesAndDemotes(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"overturn","reasoning":"false positive"}`},
	}}

	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Overturned)

	f := readFindings(t, dir)
	assert.Equal(t, reconcile.VerdictRefuted, f[0].Verification.Verdict)
	assert.False(t, f[0].Verification.ChallengeSurvived)
	assert.Equal(t, reconcile.ConfLow, f[0].Confidence)
}

func TestRunDebate_SplitOverwritesSeverity(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"split","settled_severity":"MEDIUM","reasoning":"real but minor"}`},
	}}

	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Split)

	f := readFindings(t, dir)
	assert.Equal(t, "MEDIUM", f[0].Severity) // severity-max replaced by the judge ruling
	assert.Equal(t, reconcile.VerdictConfirmed, f[0].Verification.Verdict)
	assert.True(t, f[0].Verification.ChallengeSurvived)
}

// TestRunDebate_SplitWithNoSettledSeverityRecordsNone: a split ruling that gives
// no settled_severity settled nothing. It must NOT backfill the original severity
// into the record (which would mask a no-op ruling as a legitimate adjustment);
// the finding's severity is left untouched and debate.json records no settled
// severity for it.
func TestRunDebate_SplitWithNoSettledSeverityRecordsNone(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()}) // HIGH
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"split","reasoning":"real but cannot settle the level"}`}, // no settled_severity
	}}

	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Split)

	// Finding severity untouched.
	f := readFindings(t, dir)
	assert.Equal(t, "HIGH", f[0].Severity)

	// debate.json must record no settled severity for a split that settled none.
	var df DebateFile
	raw, err := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &df))
	require.Len(t, df.Items, 1)
	assert.Empty(t, df.Items[0].SettledSeverity,
		"a split with no settled_severity must record none, not echo the original")
}

func TestRunDebate_WritesDebateJSONAndManifestStage(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"},
		{content: `{"outcome":"uphold","settled_severity":"HIGH"}`},
	}}

	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	// debate.json exists and records the item.
	var df DebateFile
	raw, err := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &df))
	require.Len(t, df.Items, 1)
	assert.Equal(t, OutcomeUphold, df.Items[0].Outcome)
	assert.Equal(t, "carol", df.Items[0].Judge)

	// manifest stages now include "debate".
	mraw, err := os.ReadFile(filepath.Join(dir, manifestFile))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(mraw, &m))
	assert.Contains(t, m["stages"], "debate")
}

func TestRunDebate_UnresolvedWhenNoDistinctModelsAndNoOptIn(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	// Only a reviewer role -> distinct path fails, no opt-in -> unresolved.
	reg := rosterReg(map[string][2]string{"alice": {"model-a", registry.RoleReviewer}})
	cc := &fakeChatCompleter{}

	res, err := runDebate(context.Background(), dir, reg, Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Unresolved)

	// Finding is untouched (no verdict written).
	f := readFindings(t, dir)
	assert.Nil(t, f[0].Verification)

	var df DebateFile
	raw, _ := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, json.Unmarshal(raw, &df))
	assert.Equal(t, ReasonInsufficientModels, df.Items[0].Reason)
}

func TestRunDebate_SingleModelOptInResolves(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	reg := rosterReg(map[string][2]string{"alice": {"model-a", registry.RoleReviewer}})
	a := reg.Agents["alice"]
	a.SupportsFC = true
	reg.Agents["alice"] = a
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"},
		{content: `{"outcome":"uphold","settled_severity":"HIGH"}`},
	}}

	res, err := runDebate(context.Background(), dir, reg, Options{SingleModel: true}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Upheld)

	var df DebateFile
	raw, _ := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, json.Unmarshal(raw, &df))
	assert.True(t, df.Items[0].SingleModel)
}

func TestRunDebate_MissingFindingsErrors(t *testing.T) {
	dir := t.TempDir()
	cc := &fakeChatCompleter{}
	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	assert.ErrorIs(t, err, ErrNoReconciledFindings)
}

func TestRunDebate_NoDisputesIsClean(t *testing.T) {
	// A consensus finding (2 reviewers, no disagreement) yields no radar items.
	f := splitFinding()
	f.Disagreement = ""
	f.Reviewers = []string{"alice", "bob"}
	dir := reviewDirWith(t, []reconcile.JSONFinding{f})
	cc := &fakeChatCompleter{}
	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Selected)
}

func TestRunDebate_JudgeHaltedIsUnresolved(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"}, {err: errContext()},
	}}
	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Unresolved)

	f := readFindings(t, dir)
	assert.Nil(t, f[0].Verification) // halted judge writes no verdict

	var df DebateFile
	raw, _ := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, json.Unmarshal(raw, &df))
	assert.Equal(t, "judge_halted", df.Items[0].Reason)
}

func TestRunDebate_OverflowRecorded(t *testing.T) {
	f1 := splitFinding()
	f2 := splitFinding()
	f2.File, f2.Line, f2.Severity = "b.go", 20, "CRITICAL"
	dir := reviewDirWith(t, []reconcile.JSONFinding{f1, f2})
	reg := debateRoster()
	one := 1
	reg.Debate = registry.DebateConfig{MaxItems: &one}
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"}, {content: `{"outcome":"uphold","settled_severity":"CRITICAL"}`},
	}}
	res, err := runDebate(context.Background(), dir, reg, Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Selected)
	assert.Equal(t, 1, res.Overflow)

	var df DebateFile
	raw, _ := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, json.Unmarshal(raw, &df))
	require.Len(t, df.Overflow, 1)
	// The CRITICAL item is debated first (severity priority); the HIGH overflows.
	assert.Equal(t, "HIGH", df.Overflow[0].Severity)
}

func TestRunDebate_GrayZoneRecordedNotApplied(t *testing.T) {
	// Two near-duplicate findings + an ambiguous.json gray-zone cluster pairing
	// them. The judge's cluster decision is recorded but no per-finding verdict is
	// written (cluster merge/separate goes through the adjudication path).
	f1 := reconcile.JSONFinding{Severity: "MEDIUM", File: "a.go", Line: 5, Problem: "leak A", Reviewers: []string{"alice"}, Confidence: "MEDIUM"}
	f2 := reconcile.JSONFinding{Severity: "MEDIUM", File: "a.go", Line: 5, Problem: "leak B", Reviewers: []string{"carol"}, Confidence: "MEDIUM"}
	dir := reviewDirWith(t, []reconcile.JSONFinding{f1, f2})
	writeAmbiguous(t, dir, `[{"id":"amb-1","file":"a.go","line":5,"similarity":0.9,"findings":[
	  {"Severity":"MEDIUM","File":"a.go","Line":5,"Problem":"leak A","Reviewer":"alice"},
	  {"Severity":"MEDIUM","File":"a.go","Line":5,"Problem":"leak B","Reviewer":"carol"}]}]`)

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"}, {content: `{"outcome":"uphold","settled_severity":"MEDIUM","cluster_decision":"merge"}`},
	}}
	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Selected, 1)

	// No finding got a verdict (cluster handled via adjudication path, not here).
	for _, f := range readFindings(t, dir) {
		assert.Nil(t, f.Verification)
	}
	var df DebateFile
	raw, _ := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, json.Unmarshal(raw, &df))
	var gray *ItemResult
	for i := range df.Items {
		if df.Items[i].Kind == reconcile.KindGrayZone {
			gray = &df.Items[i]
		}
	}
	require.NotNil(t, gray, "expected a gray_zone item recorded")
	assert.Equal(t, ClusterMerge, gray.ClusterDecision)
}

func writeAmbiguous(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, reconciledSubdir, reconcile.AmbiguousJSON), []byte(content), 0o644))
}

func errContext() error { return context.DeadlineExceeded }

// TestRunDebate_IdempotentReRun: a finding already upheld by a prior debate (carries
// ChallengeSurvived) is not re-debated on a second run, so re-runs do not re-bill
// settled findings.
func TestRunDebate_IdempotentReRun(t *testing.T) {
	f := splitFinding()
	f.Verification = &reconcile.Verification{Verdict: reconcile.VerdictConfirmed, Skeptic: "carol", ChallengeSurvived: true}
	f.Confidence = reconcile.ConfidenceVerified
	dir := reviewDirWith(t, []reconcile.JSONFinding{f})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"}, {content: `{"outcome":"uphold","settled_severity":"HIGH"}`},
	}}
	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Selected, "an already-upheld finding must not be re-debated")
}

func TestRunDebate_ContextCancelled_StopsLoop(t *testing.T) {
	f1 := splitFinding()
	f2 := splitFinding()
	f2.File, f2.Line, f2.Severity = "b.go", 20, "CRITICAL"
	dir := reviewDirWith(t, []reconcile.JSONFinding{f1, f2})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"}, {content: `{"outcome":"uphold","settled_severity":"HIGH"}`},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := runDebate(ctx, dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 2, res.Unresolved)
	assert.Zero(t, cc.idx, "no provider calls should be issued after cancellation")

	var df DebateFile
	raw, _ := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, json.Unmarshal(raw, &df))
	require.Len(t, df.Items, 2)
	for _, item := range df.Items {
		assert.Equal(t, OutcomeUnresolved, item.Outcome)
		assert.Equal(t, "context_cancelled", item.Reason)
	}
}

func TestRunDebate_GroupWriteIsAtomic(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	// Corrupt manifest.json so computeManifestStageBytes fails before the group
	// write. Because the three files are flushed via WriteGroup, no partial
	// artifact (debate.json or findings.json) should land.
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile), []byte("not json"), 0o644))

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "p"}, {content: "c"}, {content: `{"outcome":"uphold","settled_severity":"HIGH"}`},
	}}

	_, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.Error(t, err)

	_, err = os.Stat(filepath.Join(dir, reconciledSubdir, DebateJSON))
	assert.True(t, os.IsNotExist(err), "debate.json must not be written when group write fails")
	f, _ := reconcile.ReadReconciledFindings(dir)
	assert.Empty(t, f[0].Verification, "findings.json must not be mutated when group write fails")
}

func TestRunDebate_HarnessFailure_RecordsUnavailable(t *testing.T) {
	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})

	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, errorHarness(errors.New("harness unavailable")))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Selected)
	assert.Equal(t, 1, res.Unresolved)

	var df DebateFile
	raw, _ := os.ReadFile(filepath.Join(dir, reconciledSubdir, DebateJSON))
	require.NoError(t, json.Unmarshal(raw, &df))
	require.Len(t, df.Items, 1)
	assert.Equal(t, OutcomeUnresolved, df.Items[0].Outcome)
	assert.Equal(t, "harness_unavailable", df.Items[0].Reason)
}

func TestReadDebateFile(t *testing.T) {
	// Absent file → found=false, no error.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, reconciledSubdir), 0o755))
	_, found, err := ReadDebateFile(dir)
	require.NoError(t, err)
	assert.False(t, found)

	// Present file → parsed.
	require.NoError(t, writeDebateFile(dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Items:         []ItemResult{{File: "a.go", Line: 1, Kind: reconcile.KindSeveritySplit, Outcome: OutcomeUphold, Judge: "carol"}},
	}))
	df, found, err := ReadDebateFile(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, df.Items, 1)
	assert.Equal(t, OutcomeUphold, df.Items[0].Outcome)

	// Malformed file → error.
	require.NoError(t, os.WriteFile(filepath.Join(dir, reconciledSubdir, DebateJSON), []byte("{not json"), 0o644))
	_, _, err = ReadDebateFile(dir)
	assert.Error(t, err)
}

func readFindings(t *testing.T, dir string) []reconcile.JSONFinding {
	t.Helper()
	f, err := reconcile.ReadReconciledFindings(dir)
	require.NoError(t, err)
	return f
}

func TestDeduplicateFindings_KeepsFirstOccurrence(t *testing.T) {
	f1 := reconcile.JSONFinding{File: "a.go", Line: 10, Problem: "nil deref", Severity: "HIGH"}
	f2 := reconcile.JSONFinding{File: "a.go", Line: 10, Problem: "nil deref", Severity: "MEDIUM"}
	f3 := reconcile.JSONFinding{File: "b.go", Line: 20, Problem: "leak", Severity: "LOW"}

	got := deduplicateFindings([]reconcile.JSONFinding{f1, f2, f3})
	require.Len(t, got, 2)
	assert.Equal(t, "HIGH", got[0].Severity, "first occurrence of a duplicate triple must be kept")
	assert.Equal(t, "b.go", got[1].File)
}

func TestRunDebate_DuplicateFindingKeyMutatesOnlyOne(t *testing.T) {
	f1 := splitFinding()
	f2 := splitFinding()
	f2.Severity = "MEDIUM" // same {File,Line,Problem} as f1
	dir := reviewDirWith(t, []reconcile.JSONFinding{f1, f2})
	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "challenger attacks"},
		{content: `{"outcome":"uphold","settled_severity":"HIGH","reasoning":"evidence holds"}`},
	}}

	res, err := runDebate(context.Background(), dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Selected, "duplicate triple should collapse to one debate item")

	f := readFindings(t, dir)
	require.Len(t, f, 1, "findings.json should be deduplicated on the triple")
	require.NotNil(t, f[0].Verification)
	assert.Equal(t, reconcile.VerdictConfirmed, f[0].Verification.Verdict)
	assert.True(t, f[0].Verification.ChallengeSurvived)
}
