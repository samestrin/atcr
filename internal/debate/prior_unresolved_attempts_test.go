package debate

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/reconcile"
)

// writeDebateFileFixture writes df to dir's reconciled/debate.json.
func writeDebateFileFixture(t *testing.T, dir string, df DebateFile) {
	t.Helper()
	recon := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	raw, err := json.Marshal(df)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(recon, DebateJSON), raw, 0o644))
}

// TestPriorUnresolvedAttempts_CarriesTheWithheldOverflowRecord pins the half of
// priorUnresolvedAttempts that survived mutation: the df.Overflow loop.
//
// The run that WITHHOLDS an item writes no item entry for it — only an overflow
// record carrying OverflowAttemptsExhausted and the count. Reading df.Items alone
// therefore loses the history on the very next run, the item re-enters selection,
// and the three-strike ceiling holds for exactly one run. Deleting the loop leaves
// the rest of the suite green, so this is the only thing standing between that
// loop and a silent removal.
func TestPriorUnresolvedAttempts_CarriesTheWithheldOverflowRecord(t *testing.T) {
	dir := t.TempDir()
	writeDebateFileFixture(t, dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Overflow: []OverflowItem{{
			File: "a.go", Line: 7, Problem: "withheld finding",
			Reason: OverflowAttemptsExhausted, UnresolvedAttempts: maxUnresolvedAttempts,
		}},
	})

	got := priorUnresolvedAttempts(context.Background(), dir)

	key := FindingKey{File: "a.go", Line: 7, Problem: "withheld finding"}
	require.Contains(t, got, key, "a withheld record is the only carrier of its own history")
	assert.GreaterOrEqual(t, got[key], maxUnresolvedAttempts,
		"the ceiling must stay reached, or the item is re-debated forever")
}

// TestPriorUnresolvedAttempts_FloorsAPreCountWithheldRecord covers the floor
// inside the same loop: a record written before UnresolvedAttempts existed
// unmarshals to 0, and reading that literally would re-open an item the ceiling
// already closed.
func TestPriorUnresolvedAttempts_FloorsAPreCountWithheldRecord(t *testing.T) {
	dir := t.TempDir()
	writeDebateFileFixture(t, dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Overflow: []OverflowItem{{
			File: "b.go", Line: 11, Problem: "legacy withheld finding",
			Reason: OverflowAttemptsExhausted, // UnresolvedAttempts absent -> 0
		}},
	})

	got := priorUnresolvedAttempts(context.Background(), dir)

	key := FindingKey{File: "b.go", Line: 11, Problem: "legacy withheld finding"}
	assert.Equal(t, maxUnresolvedAttempts, got[key],
		"a withheld record proves the ceiling was reached even when it carries no count")
}

// TestPriorUnresolvedAttempts_IgnoresACapOverflowRecord is the complement that
// keeps the loop's guard honest: an ordinary max_items cap overflow is not
// evidence anyone tried to debate the item, so it must contribute no count.
func TestPriorUnresolvedAttempts_IgnoresACapOverflowRecord(t *testing.T) {
	dir := t.TempDir()
	writeDebateFileFixture(t, dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Overflow: []OverflowItem{{
			File: "c.go", Line: 3, Problem: "never debated, just over the cap",
		}},
	})

	got := priorUnresolvedAttempts(context.Background(), dir)

	assert.NotContains(t, got, FindingKey{File: "c.go", Line: 3, Problem: "never debated, just over the cap"},
		"a cap overflow records no attempt — counting it would withhold an item nobody tried")
}

// TestPriorUnresolvedAttempts_DoesNotFloorAnEnvironmentalRecord closes the
// second half of the same defect, one file over.
//
// Once runDebate declines to count an environmental reason, it writes
// UnresolvedAttempts 0 on that item. The floor here was written when a recorded
// zero could only mean "a record older than the field", and read literally it
// hands that attempt straight back — so a Ctrl-C'd run would still walk the item
// to the ceiling, through the reader instead of the writer. The floor must apply
// only to a reason that counts.
func TestPriorUnresolvedAttempts_DoesNotFloorAnEnvironmentalRecord(t *testing.T) {
	for _, reason := range []string{
		ReasonContextCancelled, ReasonHarnessUnavailable, ReasonInsufficientModels, ReasonNoProposer,
	} {
		t.Run(reason, func(t *testing.T) {
			dir := t.TempDir()
			writeDebateFileFixture(t, dir, DebateFile{
				SchemaVersion: DebateSchemaVersion,
				Items: []ItemResult{{
					File: "a.go", Line: 7, Problem: "interrupted finding",
					Outcome: OutcomeUnresolved, Reason: reason, UnresolvedAttempts: 0,
				}},
			})

			got := priorUnresolvedAttempts(context.Background(), dir)
			assert.Zero(t, got[FindingKey{File: "a.go", Line: 7, Problem: "interrupted finding"}],
				"an environmental failure spent no attempt — the reader must not grant one")
		})
	}
}

// And the floor must still do its original job for an ITEM-EVIDENCE record whose
// count predates the field: that zero really does mean "one attempt, unrecorded".
func TestPriorUnresolvedAttempts_StillFloorsAnItemEvidenceRecord(t *testing.T) {
	dir := t.TempDir()
	writeDebateFileFixture(t, dir, DebateFile{
		SchemaVersion: DebateSchemaVersion,
		Items: []ItemResult{
			{File: "a.go", Line: 7, Problem: "legacy record", Outcome: OutcomeUnresolved, Reason: ReasonSeatSilent},
			{File: "b.go", Line: 9, Problem: "legacy record, no reason at all", Outcome: OutcomeUnresolved},
		},
	})

	got := priorUnresolvedAttempts(context.Background(), dir)
	assert.Equal(t, 1, got[FindingKey{File: "a.go", Line: 7, Problem: "legacy record"}],
		"a real attempt that predates the field still counts as the one it provably was")
	assert.Equal(t, 1, got[FindingKey{File: "b.go", Line: 9, Problem: "legacy record, no reason at all"}],
		"an absent reason defaults to counting, exactly as the writer's deny-list does")
}

// A malformed debate.json must be VISIBLE. Direction is safe — returning nil
// resets every attempt counter, so the next run re-debates the items and
// self-heals after at most three extra debates — but this is the one tolerant read
// in the stage that does not warn, while debate.go warns for ambiguous.json and
// priorDebateRulings documents the same tolerant posture. Silently resetting the
// counters also silently disables the ceiling the read exists to enforce.
func TestPriorUnresolvedAttempts_SkipsMalformedFileWithAnError(t *testing.T) {
	dir := t.TempDir()
	recon := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(recon, DebateJSON), []byte("{not json"), 0o644))

	// The absent-file case stays silent: a review that never ran the debate stage is
	// the routine shape, not a corruption.
	empty := t.TempDir()
	assert.Nil(t, priorUnresolvedAttempts(context.Background(), empty),
		"an absent debate.json is 'never ran the stage', not a parse failure")
}

// The parse-error warning is the point: assert it reaches the operator at Warn
// (not Debug), so a corrupted artifact is diagnosable at the default level. Driven
// through runDebate rather than the helper directly, because the warning is the
// stage's operator surface and only the run path installs the context logger.
func TestPriorUnresolvedAttemptsWarnsOnMalformedFile(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := log.NewContext(context.Background(), logger)

	dir := reviewDirWith(t, []reconcile.JSONFinding{splitFinding()})
	recon := filepath.Join(dir, reconciledSubdir)
	require.NoError(t, os.MkdirAll(recon, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(recon, DebateJSON), []byte("{not json"), 0o644))

	cc := &fakeChatCompleter{turns: []chatTurn{
		{content: "proposer defends"},
		{content: "the attack stands"},
		{content: `{"outcome":"uphold","settled_severity":"HIGH","reasoning":"evidence holds"}`},
	}}
	_, err := runDebate(ctx, dir, debateRoster(), Options{}, harness(cc))
	require.NoError(t, err)

	assert.Contains(t, buf.String(), "level=WARN",
		"a corrupted debate.json must be visible at the default log level, like the sibling tolerant reads")
	assert.Contains(t, buf.String(), DebateJSON,
		"and the warning must name the artifact that could not be read")
}
