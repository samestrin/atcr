package payload

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The AC4 gate for Epic 35.4's threshold tuning.
//
// The replay harness in escalation_replay_test.go measures the escalation rate
// against LIVE repo history, which is the right instrument for choosing a
// threshold but the wrong one for guarding it: the number drifts as new commits
// land and the measurement cannot run on a shallow clone. So the harness is
// report-only and the binding assertions live here, on committed synthetic
// fixtures that isolate ONE signal each and assert a fixed PayloadMode — the
// same git-fixture pattern escalation_integration_test.go uses.
//
// Each fixture is built so exactly one signal is in play: churn stays far below
// the ratio, complexity fixtures have a single hunk, and hunk-count fixtures use
// trivial one-line functions.

// trivialFunc renders a 4-line function block (declaration, body, brace, blank)
// returning n plus a constant. Editing only the constant produces exactly one
// one-line hunk per edited function, and consecutive blocks put 3 unchanged
// lines between neighbouring hunks.
func trivialFunc(idx, konst int) string {
	return fmt.Sprintf("func F%02d(n int) int {\n\treturn n + %d\n}\n\n", idx, konst)
}

// scatteredFile renders a file of count trivial functions, where every function
// whose index is in edited returns a different constant.
func scatteredFile(count int, edited map[int]bool) string {
	var b strings.Builder
	b.WriteString("package p\n\n")
	for i := 0; i < count; i++ {
		k := 0
		if edited[i] {
			k = 7
		}
		b.WriteString(trivialFunc(i, k))
	}
	return b.String()
}

// branchyFunc renders a single function whose McCabe score is branches+1, with
// a leading accumulator line that is the only thing the fixture edits — so the
// diff is one hunk inside this function and the complexity signal is the only
// one in play.
func branchyFunc(branches, seed int) string {
	var b strings.Builder
	b.WriteString("package p\n\nfunc Branchy(n int) int {\n")
	fmt.Fprintf(&b, "\tr := %d\n", seed)
	for i := 0; i < branches; i++ {
		fmt.Fprintf(&b, "\tif n == %d {\n\t\tr++\n\t}\n", i)
	}
	b.WriteString("\treturn r\n}\n")
	return b.String()
}

// fixtureMode commits before, then after, and returns the mode the payload
// builder rendered the file in for a diff-configured run.
func fixtureMode(t *testing.T, before, after string) PayloadMode {
	t.Helper()
	dir := initRepo(t)
	write(t, dir, "fixture.go", before)
	base := commitAll(t, dir, "base")
	write(t, dir, "fixture.go", after)
	head := commitAll(t, dir, "head")

	entries, err := BuildEntries(context.Background(), ModeDiff, dir, base, head)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	return entries[0].Mode
}

// TestTunedDefaults_FourScatteredHunksStayInDiff pins the min_hunks lever. Four
// separate one-line edits is the ordinary shape of a rename or a signature
// change rippling through a file — it is not the architectural thrashing the
// signal exists to catch, and at the old threshold of 4 it promoted every such
// file out of the mode the operator configured.
func TestTunedDefaults_FourScatteredHunksStayInDiff(t *testing.T) {
	edited := map[int]bool{0: true, 1: true, 2: true, 3: true}
	got := fixtureMode(t, scatteredFile(16, nil), scatteredFile(16, edited))
	require.Equal(t, ModeDiff, got, "4 scattered hunks must no longer promote")
}

// TestTunedDefaults_SevenScatteredHunksStayInDiff pins the exact boundary. The
// four-hunk and eight-hunk cases alone would still pass if a future edit moved
// the effective threshold by one, so the fixture immediately below the line is
// what actually holds min_hunks at 8.
func TestTunedDefaults_SevenScatteredHunksStayInDiff(t *testing.T) {
	edited := map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true, 5: true, 6: true}
	got := fixtureMode(t, scatteredFile(16, nil), scatteredFile(16, edited))
	require.Equal(t, ModeDiff, got, "7 scattered hunks is one below the threshold and must not promote")
}

// TestTunedDefaults_EightScatteredHunksStillPromote is the counterweight: the
// tuning damps the signal, it does not disable it. Eight separate edit sites in
// one file is genuinely scattered.
func TestTunedDefaults_EightScatteredHunksStillPromote(t *testing.T) {
	edited := map[int]bool{0: true, 1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true}
	got := fixtureMode(t, scatteredFile(16, nil), scatteredFile(16, edited))
	require.Equal(t, ModeBlocks, got, "8 scattered hunks must still promote")
}

// TestTunedDefaults_HunksThreeLinesApartStayInDiff pins the hunk_gap lever. Two
// edits three unchanged lines apart is what a single logical change looks like
// under --unified=0; at the old gap of 10 that fired as "the same region was
// churned twice", which is the measured worst offender behind a diff-configured
// agent losing diff mode.
func TestTunedDefaults_HunksThreeLinesApartStayInDiff(t *testing.T) {
	edited := map[int]bool{0: true, 1: true}
	got := fixtureMode(t, scatteredFile(16, nil), scatteredFile(16, edited))
	require.Equal(t, ModeDiff, got, "hunks 3 unchanged lines apart must no longer promote")
}

// TestTunedDefaults_TouchingHunksStillPromote is the adjacency counterweight:
// two edits with a single unchanged line between them really is the same region
// churned twice, and must still promote.
func TestTunedDefaults_TouchingHunksStillPromote(t *testing.T) {
	const before = `package p

func Tight(n int) int {
	a := n + 1
	b := a * 2
	c := b - 3
	return c
}
`
	const after = `package p

func Tight(n int) int {
	a := n + 9
	b := a * 2
	c := b - 8
	return c
}
`
	require.Equal(t, ModeBlocks, fixtureMode(t, before, after), "hunks 1 unchanged line apart must still promote")
}

// TestTunedDefaults_ModeratelyBranchyChangeStaysInDiff pins the min_cyclomatic
// lever. McCabe 17 is a busy function, but now that the score is scoped to the
// CHANGED function it is reachable by ordinary validation code, and promoting on
// it spent payload budget without buying the reviewer anything a diff plus
// skeleton did not already show.
func TestTunedDefaults_ModeratelyBranchyChangeStaysInDiff(t *testing.T) {
	got := fixtureMode(t, branchyFunc(16, 0), branchyFunc(16, 5))
	require.Equal(t, ModeDiff, got, "a changed function at McCabe 17 must no longer promote")
}

// TestTunedDefaults_JustBelowCyclomaticThresholdStaysInDiff pins the exact
// complexity boundary, for the same reason the seven-hunk fixture exists: a
// threshold is only held by the case immediately below it.
func TestTunedDefaults_JustBelowCyclomaticThresholdStaysInDiff(t *testing.T) {
	got := fixtureMode(t, branchyFunc(18, 0), branchyFunc(18, 5))
	require.Equal(t, ModeDiff, got, "a changed function at McCabe 19 is one below the threshold and must not promote")
}

// TestTunedDefaults_AtCyclomaticThresholdPromotes pins the firing side of the
// same boundary: McCabe 20 is exactly the floor, and `>=` must include it.
func TestTunedDefaults_AtCyclomaticThresholdPromotes(t *testing.T) {
	got := fixtureMode(t, branchyFunc(19, 0), branchyFunc(19, 5))
	require.Equal(t, ModeBlocks, got, "a changed function at McCabe 20 is at the threshold and must promote")
}

// TestTunedDefaults_VeryBranchyChangeStillPromotes is the complexity
// counterweight: McCabe 25 in a changed function is genuinely unreadable from
// hunks alone.
func TestTunedDefaults_VeryBranchyChangeStillPromotes(t *testing.T) {
	got := fixtureMode(t, branchyFunc(24, 0), branchyFunc(24, 5))
	require.Equal(t, ModeBlocks, got, "a changed function at McCabe 25 must still promote")
}
