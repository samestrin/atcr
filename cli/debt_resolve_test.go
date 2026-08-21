package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/reconcile"
)

// openRec builds an open (no-status) local-debt record with a stable id, mirroring
// what the reconcile persistence hook writes (cli/reconcile.go:181-196).
func openRec(runID, sev, file string, line int, problem string) localdebt.Record {
	r := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion,
		RunID:         runID,
		Timestamp:     runID,
		Severity:      sev,
		File:          file,
		Line:          line,
		Problem:       problem,
		Fix:           "apply the fix",
		Category:      "correctness",
		EstMinutes:    30,
		Evidence:      "evidence",
		Reviewers:     []string{"claude"},
		Confidence:    "HIGH",
	}
	r.StampID()
	return r
}

// writeDebtStore writes fixture records to a temp .atcr/debt-shaped dir and returns it.
func writeDebtStore(t *testing.T, recs ...localdebt.Record) string {
	t.Helper()
	dir := t.TempDir()
	for _, r := range recs {
		require.NoError(t, localdebt.Append(dir, r))
	}
	return dir
}

func TestDebtResolve_RegisteredAndDiscoverable(t *testing.T) {
	cmd := newDebtCmd()
	var hasResolve bool
	for _, c := range cmd.Commands() {
		if c.Name() == "resolve" {
			hasResolve = true
		}
	}
	assert.True(t, hasResolve, "debt must own a resolve subcommand")

	// Discoverable via `atcr debt --help`, per SKILL.md's subcommand convention.
	out, err := runDebt(t, "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "resolve", "`atcr debt --help` must list the resolve subcommand")
}

func TestDebtResolve_UsesLocalStoreNotPlanning(t *testing.T) {
	// The resolve subcommand must read the .atcr/-scoped store; it must NOT expose
	// the .planning/-scoped --items/--readme source flags that list/add/dashboard use.
	var resolve *cobra.Command
	for _, c := range newDebtCmd().Commands() {
		if c.Name() == "resolve" {
			resolve = c
		}
	}
	require.NotNil(t, resolve, "resolve subcommand must exist")
	assert.Nil(t, resolve.Flags().Lookup("items"), "resolve must not use the .planning/ --items flag")
	assert.Nil(t, resolve.Flags().Lookup("readme"), "resolve must not use the .planning/ --readme flag")
}

func TestDebtResolve_ListsOpenItems(t *testing.T) {
	dir := writeDebtStore(t,
		openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom"),
		openRec("2026-07-02T10:00:00Z-b", "LOW", "internal/y/b.go", 34, "leak"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "internal/x/a.go")
	assert.Contains(t, out, "internal/y/b.go")
	assert.Contains(t, out, "HIGH")
	assert.Contains(t, out, "LOW")
}

func TestDebtResolve_NoFlagDefaultsToList(t *testing.T) {
	dir := writeDebtStore(t,
		openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "internal/x/a.go", "no-flag invocation previews the open items")
}

func TestDebtResolve_EmptyStoreMessageExitsZero(t *testing.T) {
	dir := t.TempDir() // no shards written -> ReadAll returns (nil, nil)
	out, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err, "empty store must exit 0, never a non-zero exit")
	assert.Contains(t, strings.ToLower(out), "no items")
}

// The empty-list message names the store it read, the way `debt add` prints its
// resolved dir — an undifferentiated "no open items" from the WRONG store reads
// exactly like an empty backlog, with exit code 0 either way.
func TestDebtResolve_EmptyListMessageNamesTheStore(t *testing.T) {
	dir := t.TempDir()
	out, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "no items")
	assert.Contains(t, out, dir, "the empty-list line names the store it read")
}

func TestDebtResolve_MissingDirIsNotAnError(t *testing.T) {
	out, err := runDebt(t, "resolve", "--dir", t.TempDir()+"/does-not-exist")
	require.NoError(t, err, "a missing .atcr/debt dir is the no-backlog state, not an error")
	assert.Contains(t, strings.ToLower(out), "no items")
}

func TestDebtResolve_JSONOutput(t *testing.T) {
	dir := writeDebtStore(t,
		openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir, "--json")
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &items), "output must be a JSON array")
	require.Len(t, items, 1)
	assert.Equal(t, "internal/x/a.go", items[0]["file"])
	assert.Equal(t, "HIGH", items[0]["severity"])
}

func TestDebtResolve_JSONEmptyStoreIsEmptyArray(t *testing.T) {
	out, err := runDebt(t, "resolve", "--dir", t.TempDir(), "--json")
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	assert.Empty(t, items, "empty store yields an empty JSON array, not null or a stack trace")
}

func TestSelectOpenDebt_AgreesWithFoldRecordsOnReRaisedID(t *testing.T) {
	// The same File/Line/Problem re-raised across two runs yields two OPEN records
	// under one stable id with divergent Severity. selectOpenDebt must display the
	// SAME occurrence FoldRecords keeps (the last), so the resolve list never
	// disagrees with the quality signal (which folds via FoldRecords) about the item.
	first := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	second := first
	second.RunID = "2026-07-02T10:00:00Z-b"
	second.Timestamp = second.RunID
	second.Severity = "LOW"
	require.Equal(t, first.ID, second.ID, "identical File/Line/Problem must share a stable id")

	recs := []localdebt.Record{first, second}
	dir := writeDebtStore(t, recs...)
	got, err := selectOpenDebt(dir, "", 0, localdebt.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, 1, "the two occurrences fold to one open item")

	folded := localdebt.FoldRecords(recs)
	require.Len(t, folded, 1)
	assert.Equal(t, folded[0].Severity, got[0].Severity,
		"selectOpenDebt must display the occurrence FoldRecords keeps (last), not the first")
	assert.Equal(t, "LOW", got[0].Severity, "the most recent open occurrence wins")
}

func TestDebtResolve_SelectionSortsSeverityThenAge(t *testing.T) {
	// Written newest-first and lowest-severity-first to prove the command re-sorts:
	// severity DESC (HIGH before LOW), then ts ASC (oldest first) within a severity.
	dir := writeDebtStore(t,
		openRec("2026-07-05T10:00:00Z-low", "LOW", "z/low.go", 1, "low sev"),
		openRec("2026-07-04T10:00:00Z-h2", "HIGH", "z/high2.go", 2, "high newer"),
		openRec("2026-07-01T10:00:00Z-h1", "HIGH", "z/high1.go", 3, "high older"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	iHigh1 := strings.Index(out, "z/high1.go")
	iHigh2 := strings.Index(out, "z/high2.go")
	iLow := strings.Index(out, "z/low.go")
	require.True(t, iHigh1 >= 0 && iHigh2 >= 0 && iLow >= 0)
	assert.Less(t, iHigh1, iHigh2, "older HIGH item sorts before newer HIGH item")
	assert.Less(t, iHigh2, iLow, "HIGH items sort before LOW items")
}

func TestDebtResolve_SeverityFilter(t *testing.T) {
	dir := writeDebtStore(t,
		openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom"),
		openRec("2026-07-02T10:00:00Z-b", "LOW", "internal/y/b.go", 34, "leak"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir, "--severity", "high")
	require.NoError(t, err)
	assert.Contains(t, out, "internal/x/a.go")
	assert.NotContains(t, out, "internal/y/b.go")
}

func TestDebtResolve_InvalidSeverityIsUsageError(t *testing.T) {
	dir := writeDebtStore(t, openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "x"))
	_, err := runDebt(t, "resolve", "--dir", dir, "--severity", "BOGUS")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err), "an invalid --severity value is a usage error (exit 2)")
}

func TestDebtResolve_MaxCapsSelection(t *testing.T) {
	dir := writeDebtStore(t,
		openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "one"),
		openRec("2026-07-02T10:00:00Z-b", "HIGH", "b.go", 2, "two"),
		openRec("2026-07-03T10:00:00Z-c", "HIGH", "c.go", 3, "three"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir, "--max", "1", "--json")
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	assert.Len(t, items, 1, "--max caps the number of selected items")
}

// A negative --max is rejected like a bad --severity/--status and debt add's
// --est: the flag help documents only "0 = no cap", and without validation
// `if limit > 0` silently treats --max -5 as unlimited — a typo'd cap that
// quietly expands the worklist with exit code 0.
func TestDebtResolve_NegativeMaxIsUsageError(t *testing.T) {
	dir := writeDebtStore(t, openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "x"))
	out, err := runDebt(t, "resolve", "--dir", dir, "--max", "-1")
	require.Error(t, err, "a negative --max must be rejected, not silently treated as no cap")
	assert.Equal(t, exitUsage, exitCode(err), "a negative --max is a usage error (exit 2)")
	assert.Contains(t, out, "invalid --max -1: expected a non-negative number")

	// 0 remains the documented "no cap" value and must stay valid.
	_, err = runDebt(t, "resolve", "--dir", dir, "--max", "0")
	require.NoError(t, err, "--max 0 (no cap) is a documented, valid value")
}

func TestDebtResolve_MarkResolvedRemovesItemFromOpenList(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec,
		openRec("2026-07-02T10:00:00Z-b", "LOW", "internal/y/b.go", 34, "leak"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "resolved")

	// The append-only resolution record must fold the item out of the open list.
	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.NotContains(t, list, "internal/x/a.go", "a resolved item must not appear as open")
	assert.Contains(t, list, "internal/y/b.go", "the other item stays open")
}

func TestDebtResolve_MarkResolvedIsIdempotent(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)

	// A second resolve of the same id must no-op, not append a duplicate record.
	before, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "already closed")
	after, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, after, len(before), "re-resolving must not append another resolution record")
}

func TestDebtResolve_AlreadyClosedReportsActualStatus(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	// Mark as wontfix first.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)

	// A subsequent plain resolve must report the existing terminal status, not
	// hardcode "already resolved".
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "wontfix", "already-closed message must name the actual terminal status")
	assert.NotContains(t, strings.ToLower(out), "already resolved", "must not hardcode 'already resolved' when the item is wontfix")
}

func TestDebtResolve_MarkWontfixIsIdempotentAgainstResolved(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	// First resolve the item normally.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)

	// A subsequent wontfix of the same id must no-op and report the actual status.
	before, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "already closed as resolved")
	after, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, after, len(before), "wontfix after resolved must not append another terminal record")
}

func TestDebtResolve_MarkResolvedIsIdempotentAgainstWontfix(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	// First dismiss the item as wontfix.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)

	// A subsequent resolved of the same id must no-op and report the actual status.
	before, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "already closed as wontfix")
	after, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, after, len(before), "resolved after wontfix must not append another terminal record")
}

func TestDebtResolve_AlreadyClosedPrefersWontfixOverReadOrder(t *testing.T) {
	// The no-lock TD-004 stance lets two concurrent invocations each pass the
	// alreadyClosed check before either appends, so the store can carry divergent
	// terminal records for one id — e.g. one resolved and one wontfix. A later
	// invocation must report the effective status deterministically (wontfix, a
	// permanent dismissal, outranks resolved) rather than by shard read order.
	open := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")

	wontfix := open
	wontfix.RunID = "2026-07-01T11:00:00Z-a-wontfix"
	wontfix.Timestamp = wontfix.RunID
	wontfix.Status = "wontfix"

	resolved := open
	resolved.RunID = "2026-07-01T12:00:00Z-a-resolved"
	resolved.Timestamp = resolved.RunID
	resolved.Status = "resolved"

	// wontfix is written (and thus read) BEFORE resolved, so a last-wins read-order
	// reader would report "resolved"; only a precedence reader reports "wontfix".
	dir := writeDebtStore(t, open, wontfix, resolved)

	out, err := runDebt(t, "resolve", "--dir", dir, open.ID)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "already closed as wontfix",
		"divergent terminal records must report wontfix by precedence, not resolved by read order")
	assert.NotContains(t, strings.ToLower(out), "already closed as resolved",
		"read order must not decide the effective terminal status")
}

func TestDebtResolve_ReasonLengthCapRejectsOversized(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	before, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)

	// A multi-KiB --reason pushes the stored JSONL record toward the store's 1 MiB
	// per-line read cap (internal/localdebt/store.go maxLineBytes), where an over-long
	// line is silently dropped on read. Reject oversized justifications up front so a
	// finding never becomes silently unreadable.
	huge := strings.Repeat("x", 5000)
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", huge)
	require.Error(t, err, "an over-long --reason must be rejected, not stored")
	assert.NotContains(t, strings.ToLower(out), "marked", "must not report success for a rejected oversized reason")

	after, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, after, len(before), "a rejected oversized --reason must not append a record")

	// A reasonable justification still succeeds.
	_, err = runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err, "a normal-length --reason must still be accepted")
}

func TestDebtResolve_MarkResolvedUnknownIDErrors(t *testing.T) {
	dir := writeDebtStore(t, openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "x"))
	_, err := runDebt(t, "resolve", "--dir", dir, "deadbeef")
	require.Error(t, err, "resolving an unknown id must error, not silently no-op")
}

// TD (file-less open record): an item that genuinely exists and is genuinely
// open but carries an empty File must NOT report "no open technical-debt item"
// — it is live work in `debt list` and the dashboard, so calling it nonexistent
// misleads the operator. It gets its own message naming the actual defect: the
// record has no file location to resolve against.
func TestDebtResolve_MarkResolvedFilelessRecordReportsDistinctError(t *testing.T) {
	fileless := openRec("2026-07-01T10:00:00Z-a", "HIGH", "", 0, "no location")
	fileless.StampID()
	dir := writeDebtStore(t, fileless)

	_, err := runDebt(t, "resolve", "--dir", dir, fileless.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no file location and cannot be resolved",
		"an existing-but-file-less open record must not masquerade as a nonexistent id")
	assert.NotContains(t, err.Error(), "no open technical-debt item",
		"the item exists; the unknown-id message would be a lie")

	// Nothing may be appended for the rejected resolve.
	recs := readStoreRecords(t, dir)
	for _, r := range recs {
		assert.Empty(t, r.Status, "a rejected resolve appends no terminal record")
	}
}

func TestDebtResolve_WontfixStatusFoldsItemOutOfOpenList(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	// A terminal wontfix record for the same id must fold the finding out of the
	// open backlog exactly like a resolved record (Epic 24.0 AC #2).
	wontfix := rec
	wontfix.RunID = "2026-07-01T11:00:00Z-a-wontfix"
	wontfix.Timestamp = wontfix.RunID
	wontfix.Status = "wontfix"
	dir := writeDebtStore(t, rec, wontfix,
		openRec("2026-07-02T10:00:00Z-b", "LOW", "internal/y/b.go", 34, "leak"),
	)

	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.NotContains(t, list, "internal/x/a.go", "a wontfix item must not appear as open")
	assert.Contains(t, list, "internal/y/b.go", "the unrelated open item stays open")

	// The JSON view folds the wontfix item out too.
	js, err := runDebt(t, "resolve", "--dir", dir, "--json")
	require.NoError(t, err)
	assert.NotContains(t, js, "internal/x/a.go", "a wontfix item must not appear in --json")
}

func TestDebtResolve_MarkWontfixSetsStatusAndFoldsOut(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec,
		openRec("2026-07-02T10:00:00Z-b", "LOW", "internal/y/b.go", 34, "leak"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "wontfix")

	// AC #4: the dismissal state is durable — a wontfix status record is appended
	// for the finding's stable id.
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var wontfixRec *localdebt.Record
	for i := range recs {
		if recs[i].ID == rec.ID && recs[i].Status == "wontfix" {
			wontfixRec = &recs[i]
		}
	}
	require.NotNil(t, wontfixRec, "a wontfix status record must be appended for the id")

	// AC #2: the wontfix item folds out of the open list; the other stays.
	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.NotContains(t, list, "internal/x/a.go", "a wontfix item must not appear as open")
	assert.Contains(t, list, "internal/y/b.go", "the other item stays open")
}

func TestDebtResolve_DefaultStatusStaysResolved(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var found bool
	for _, r := range recs {
		if r.ID == rec.ID && r.Status == "resolved" {
			found = true
		}
	}
	assert.True(t, found, "omitting --status must default to a resolved record, unchanged")
}

func TestDebtResolve_InvalidStatusIsUsageError(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "bogus")
	require.Error(t, err, "an unrecognized --status must be a usage error, not a silently non-folding record")

	// The error must report the canonical lowercase form, not the user's casing.
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "BOGUS")
	require.Error(t, err)
	assert.Contains(t, out, `invalid --status "bogus"`, "error must show canonical lowercase status")
	assert.NotContains(t, out, `invalid --status "BOGUS"`, "error must not echo user's uppercase input")
}

func TestDebtResolve_WontfixRequiresReasonOrJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	// wontfix with no --reason and no pre-existing justification must be rejected.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix")
	require.Error(t, err, "wontfix without a reason or existing justification must be a usage error")
	assert.Equal(t, exitUsage, exitCode(err))

	// wontfix with a --reason is allowed.
	_, err = runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)
}

func TestDebtResolve_ReasonPopulatesJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID,
		"--status", "wontfix", "--reason", "accepted pattern, reviewer hallucination")
	require.NoError(t, err)

	// AC #1 + #4: the reason is recorded in Justification on the durable terminal record.
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var terminal *localdebt.Record
	for i := range recs {
		if recs[i].ID == rec.ID && recs[i].Status == "wontfix" {
			terminal = &recs[i]
		}
	}
	require.NotNil(t, terminal, "a wontfix record must be appended")
	assert.Equal(t, "accepted pattern, reviewer hallucination", terminal.Justification,
		"--reason must populate the record's Justification field")
}

func TestDebtResolve_WhitespaceReasonPreservesExistingJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	rec.Justification = "original enrichment note"
	dir := writeDebtStore(t, rec)

	// A whitespace-only --reason is treated as empty and must preserve the existing
	// justification, just like omitting --reason entirely.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "   ")
	require.NoError(t, err)
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var terminal *localdebt.Record
	for i := range recs {
		if recs[i].ID == rec.ID && recs[i].Status == "wontfix" {
			terminal = &recs[i]
		}
	}
	require.NotNil(t, terminal)
	assert.Equal(t, "original enrichment note", terminal.Justification,
		"whitespace-only --reason must preserve the item's existing justification")
}

func TestDebtResolve_ReasonReplacesExistingJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	rec.Justification = "original enrichment note"
	dir := writeDebtStore(t, rec)

	// A supplied --reason replaces any pre-existing justification on the resolved
	// record (documented behavior); it does not merge with it.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--reason", "replacement note")
	require.NoError(t, err)
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var terminal *localdebt.Record
	for i := range recs {
		if recs[i].ID == rec.ID && recs[i].Status == "resolved" {
			terminal = &recs[i]
		}
	}
	require.NotNil(t, terminal)
	assert.Equal(t, "replacement note", terminal.Justification,
		"a supplied --reason must replace the item's existing justification")
}

func TestDebtResolve_NoReasonWithEmptyJustificationStaysEmpty(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	rec.Justification = ""
	dir := writeDebtStore(t, rec)

	// Omitting --reason when the original record has no justification must leave
	// the terminal record's Justification as the empty string (zero value), not
	// unset/missing. Use resolved status so the item-6 wontfix-reason guard does
	// not interfere with this empty-justification edge case.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var terminal *localdebt.Record
	for i := range recs {
		if recs[i].ID == rec.ID && recs[i].Status == "resolved" {
			terminal = &recs[i]
		}
	}
	require.NotNil(t, terminal)
	assert.Equal(t, "", terminal.Justification,
		"omitting --reason on a record with empty justification must leave it empty")
}

func TestDebtResolve_NoReasonPreservesExistingJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	rec.Justification = "original enrichment note"
	dir := writeDebtStore(t, rec)
	// Omitting --reason must not blank an existing justification carried on the item.
	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix")
	require.NoError(t, err)
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var terminal *localdebt.Record
	for i := range recs {
		if recs[i].ID == rec.ID && recs[i].Status == "wontfix" {
			terminal = &recs[i]
		}
	}
	require.NotNil(t, terminal)
	assert.Equal(t, "original enrichment note", terminal.Justification,
		"omitting --reason must preserve the item's existing justification")
}

func TestDebtResolve_StatusOrReasonWithoutResolveIsUsageError(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	errorLine := func(out string) string {
		parts := strings.SplitN(out, "\n", 2)
		return strings.ToLower(strings.TrimSpace(parts[0]))
	}

	// --status without an id must not silently fall through to the list view:
	// it would drop the user's dismissal intent (and skip status validation).
	out, err := runDebt(t, "resolve", "--dir", dir, "--status", "wontfix")
	require.Error(t, err, "--status without an id must be a usage error, not a silent list")
	el := errorLine(out)
	assert.Contains(t, el, "--status", "error must mention only the supplied flag")
	assert.NotContains(t, el, "--reason", "error must not mention --reason when only --status was supplied")

	// The explicit default value must also be rejected without an id; this
	// path is distinct from a non-default status and locks the guard behavior.
	out, err = runDebt(t, "resolve", "--dir", dir, "--status", "resolved")
	require.Error(t, err, "--status resolved without an id must be a usage error")
	el = errorLine(out)
	assert.Contains(t, el, "--status")
	assert.NotContains(t, el, "--reason")

	// --reason without an id is the same footgun.
	out, err = runDebt(t, "resolve", "--dir", dir, "--reason", "some note")
	require.Error(t, err, "--reason without an id must be a usage error")
	el = errorLine(out)
	assert.Contains(t, el, "--reason", "error must mention only the supplied flag")
	assert.NotContains(t, el, "--status", "error must not mention --status when only --reason was supplied")

	// An explicitly empty --reason without --resolve must also be rejected; it
	// should be governed by Changed("reason"), not by the trimmed value.
	out, err = runDebt(t, "resolve", "--dir", dir, "--reason", "")
	require.Error(t, err, "explicit --reason=\"\" without an id must be a usage error")
	el = errorLine(out)
	assert.Contains(t, el, "--reason")
	assert.NotContains(t, el, "--status")

	// An explicitly empty --status is invalid status anyway, but it must also be
	// rejected at the guard before falling through to the list view.
	_, err = runDebt(t, "resolve", "--dir", dir, "--status", "")
	require.Error(t, err, "explicit --status=\"\" without an id must be a usage error")

	// A plain no-arg list (no --status/--reason) still works untouched.
	_, err = runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err, "a plain list must not be affected by the new guard")
}

func TestDebtResolve_SelectionWorksWithoutOptionalFields(t *testing.T) {
	// A record missing justification and source_report must still be selectable.
	rec := openRec("2026-07-01T10:00:00Z-a", "MEDIUM", "internal/x/a.go", 12, "boom")
	rec.Justification = ""
	rec.SourceReport = nil
	dir := writeDebtStore(t, rec)
	out, err := runDebt(t, "resolve", "--dir", dir, "--json")
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "internal/x/a.go", items[0]["file"])
}

func TestDebtResolve_ListFlagsWithResolveAreUsageError(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	errorLine := func(out string) string {
		parts := strings.SplitN(out, "\n", 2)
		return strings.ToLower(strings.TrimSpace(parts[0]))
	}

	// --json/--severity/--max only affect the list renderer, which the mark branch
	// never reaches; combined with an id they would be silently ignored. They
	// must be rejected as usage errors, symmetric with --status/--reason without
	// an id. Assert against the first output line only — cobra's usage dump on
	// error lists every flag name and would false-positive a Contains on full output.
	cases := []struct {
		name string
		args []string
		want []string // flag names that must appear on the error line
	}{
		{"json", []string{"--json"}, []string{"--json"}},
		{"severity", []string{"--severity", "HIGH"}, []string{"--severity"}},
		{"max", []string{"--max", "5"}, []string{"--max"}},
		{"combined", []string{"--json", "--max", "5"}, []string{"--json", "--max"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"resolve", "--dir", dir, rec.ID}, tc.args...)
			out, err := runDebt(t, args...)
			require.Error(t, err, "%s with an id must be a usage error, not silently ignored", tc.name)
			assert.Equal(t, exitUsage, exitCode(err), "exit code must be 2 (usage), like --status without an id")
			el := errorLine(out)
			for _, flag := range tc.want {
				assert.Contains(t, el, flag, "error must name the rejected list-only flag")
			}
			assert.NotContains(t, strings.ToLower(out), "marked", "a rejected invocation must not report a resolution")
		})
	}

	// No terminal record may be appended by any of the rejected invocations.
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	for _, r := range recs {
		assert.False(t, r.ID == rec.ID && r.Status != "", "rejected invocations must not append a terminal record")
	}

	// A bare id (no list-only flags) still marks the item, unchanged.
	_, err = runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err, "a plain id without list-only flags must still succeed")
}

func TestDebtResolve_ExplicitEmptyIDIsUsageError(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	// An explicit empty positional id is a mark attempt with no id: routing it to
	// the list path would silently discard the user's mark intent. It must be a
	// usage error (exit 2), not the empty value falling through to list.
	out, err := runDebt(t, "resolve", "--dir", dir, "")
	require.Error(t, err, `an explicit "" id must be a usage error, not a silent list`)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Contains(t, strings.ToLower(out), "non-empty id", "error must name the missing id")
	assert.NotContains(t, out, "internal/x/a.go", "must not fall through to the list view")

	// Omitting the id entirely still lists, untouched by the new guard.
	out, err = runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err, "omitting the id must still list open items")
	assert.Contains(t, out, "internal/x/a.go")
}

func TestDebtResolve_TerminalRecordUnionsReviewersAcrossOpenRecords(t *testing.T) {
	// The same File/Line/Problem finding raised by two separate reconcile runs yields
	// two distinct open records under one stable id, each with its own reviewer/model
	// attribution. AggregateQualitySignal reads only the terminal record
	// (foldTerminalByID), so stamping just the first open record's Reviewers would
	// deny the later run's personas their dismissed/confirmed credit.
	first := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	first.Reviewers = []string{"bruce", "greta"}
	first.Model = "claude-sonnet-4-6"
	second := first
	second.RunID = "2026-07-02T10:00:00Z-b"
	second.Timestamp = second.RunID
	second.Reviewers = []string{"ingrid", "bruce"}
	second.Model = "gpt-5.2"
	require.Equal(t, first.ID, second.ID, "identical File/Line/Problem must yield the same stable id")
	dir := writeDebtStore(t, first, second)

	_, err := runDebt(t, "resolve", "--dir", dir, first.ID)
	require.NoError(t, err)

	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var terminal *localdebt.Record
	for i := range recs {
		if recs[i].ID == first.ID && recs[i].Status == "resolved" {
			terminal = &recs[i]
		}
	}
	require.NotNil(t, terminal, "a terminal record must be appended")
	assert.Equal(t, []string{"bruce", "greta", "ingrid"}, terminal.Reviewers,
		"terminal record must credit every persona that raised the finding (deduped, first-seen order)")
	assert.Equal(t, "gpt-5.2", terminal.Model,
		"Model follows the most recent open record's non-empty value so the latest run's model gets the credit")

	// End-to-end: the aggregated signal must confirm every raising persona, bucketed
	// under the most recent model.
	confirmed := map[string]int{}
	for _, row := range localdebt.AggregateQualitySignal(recs) {
		confirmed[row.Persona+"/"+row.Model] = row.ConfirmedCount
	}
	assert.Equal(t, 1, confirmed["bruce/gpt-5.2"], "first-run persona still credited")
	assert.Equal(t, 1, confirmed["greta/gpt-5.2"], "first-run persona still credited")
	assert.Equal(t, 1, confirmed["ingrid/gpt-5.2"], "later-run persona must not lose credit")
	assert.Zero(t, confirmed["bruce/claude-sonnet-4-6"], "the stale earlier model must not split the bucket")
}

func TestDebtResolve_TerminalRecordModelFallsBackToEarlierNonEmpty(t *testing.T) {
	// When the most recent open record has no model attribution (v1, or unresolved
	// attribution), the terminal record must keep an earlier open record's non-empty
	// Model rather than blanking it — an empty Model is excluded from every
	// AggregateQualitySignal row, so blanking would silently drop all credit.
	first := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	first.Reviewers = []string{"bruce"}
	first.Model = "claude-sonnet-4-6"
	second := first
	second.RunID = "2026-07-02T10:00:00Z-b"
	second.Timestamp = second.RunID
	second.Reviewers = []string{"ingrid"}
	second.Model = ""
	dir := writeDebtStore(t, first, second)

	_, err := runDebt(t, "resolve", "--dir", dir, first.ID)
	require.NoError(t, err)

	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var terminal *localdebt.Record
	for i := range recs {
		if recs[i].ID == first.ID && recs[i].Status == "resolved" {
			terminal = &recs[i]
		}
	}
	require.NotNil(t, terminal, "a terminal record must be appended")
	assert.Equal(t, "claude-sonnet-4-6", terminal.Model,
		"most recent NON-EMPTY model wins; a later attribution-less record must not blank it")
	assert.Equal(t, []string{"bruce", "ingrid"}, terminal.Reviewers)
}

// --- Plan 35.13 T3: resolution semantics split by status --------------------

// redetect appends a fresh open record for rec's id, timestamped AFTER every
// parseable record already in the store — simulating a later reconcile run that
// finds the same file/line/problem again. The timestamp is derived rather than
// hard-coded because markDebtResolved stamps its resolution with the wall clock,
// so a literal date would silently stop being "later" and the test would assert
// nothing.
func redetect(t *testing.T, dir string, rec localdebt.Record) {
	t.Helper()
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var latest time.Time
	for _, r := range recs {
		if ts, err := time.Parse(time.RFC3339, r.Timestamp); err == nil && ts.After(latest) {
			latest = ts
		}
	}
	next := latest.Add(time.Hour).UTC().Format(time.RFC3339)

	out := rec
	out.Status = ""
	out.ResolvedAt = ""
	out.RunID = next + "-redetect"
	out.Timestamp = next
	require.NoError(t, localdebt.Append(dir, out))
}

// AC3(b): a resolved id re-detected at the same file/line/problem is a
// REGRESSION (or a fix that never landed) — the case most worth surfacing — so
// it returns to the open backlog.
func TestDebtResolve_ResolvedThenRegressedReopens(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)

	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	require.NotContains(t, list, "internal/x/a.go", "the resolution closes it first")

	// A later reconcile re-detects the identical finding.
	redetect(t, dir, rec)

	list, err = runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, list, "internal/x/a.go", "a regressed resolved id returns to the open backlog")
}

// AC3(a): wontfix is the mirror image — a stable id at a stable location is
// exactly what makes permanent suppression work, so re-detection changes nothing.
func TestDebtResolve_WontfixThenRegressedStaysClosed(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)

	redetect(t, dir, rec)

	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(list), "no items",
		"a dismissed finding must stay dismissed when it is re-detected")
}

// AC3(c): "not now" is not "never" — a deferred id re-surfaces on re-detection.
// The writer is `atcr debt add --status deferred` (see the doc.go sweep note).
func TestDebtResolve_DeferredThenRegressedResurfaces(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	deferred := rec
	deferred.RunID = "2026-07-02T10:00:00Z-a"
	deferred.Timestamp = "2026-07-02T10:00:00Z"
	deferred.Status = "deferred"
	dir := writeDebtStore(t, rec, deferred)

	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(list), "no items", "a deferred item is out of the backlog while it stands")

	redetect(t, dir, rec)

	list, err = runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, list, "internal/x/a.go", "a re-detected deferred id re-surfaces")
}

// TD: a deferred item is LIVE to the dashboard (debtIsLive -> IsSettledStatus)
// and OFF the resolve worklist (selectOpenIDs -> IsClosedStatus), so it renders as
// a top-priority row while `debt resolve` reports nothing to do. The
// divergence is deliberate — deferring means "not now", which keeps the item on
// the backlog view and off the fix worklist — but nothing asserted it, so a
// future edit to either predicate could collapse the two views onto one answer
// silently. This pins all three views at once.
func TestDebt_DeferredIsLiveToTheDashboardAndOffTheResolveWorklist(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "CRITICAL", "internal/x/a.go", 12, "boom")
	deferred := rec
	deferred.RunID = "2026-07-02T10:00:00Z-a"
	deferred.Timestamp = "2026-07-02T10:00:00Z"
	deferred.Status = "deferred"
	dir := writeDebtStore(t, rec, deferred)

	worklist, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(worklist), "no items",
		"deferring takes the item off the fix worklist")

	dash, err := runDebt(t, "dashboard", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, dash, "internal/x/a.go",
		"the same item stays on the dashboard's live backlog")

	listed, err := runDebt(t, "list", "--dir", dir, "--status", "deferred")
	require.NoError(t, err)
	assert.Contains(t, listed, "internal/x/a.go",
		"and is still listable and closeable by id")
}

// The already-closed guard must test the FOLDED effective status, not the mere
// presence of a terminal record somewhere in history. Scanning all history would
// refuse to close a regressed id a second time — permanently, since a resolution
// record for it always exists.
func TestDebtResolve_CanReResolveAfterARegression(t *testing.T) {
	// Seeded with fixed past timestamps rather than driven through two mark runs
	// calls: markDebtResolved stamps the wall clock, so two resolutions in one test
	// share a timestamp and leave no room for a regression to fall between them.
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	resolved := rec
	resolved.RunID = "2026-07-02T10:00:00Z-a-resolved"
	resolved.Timestamp = resolved.RunID
	resolved.Status = "resolved"
	regressed := rec
	regressed.RunID = "2026-07-03T10:00:00Z-a"
	regressed.Timestamp = regressed.RunID
	dir := writeDebtStore(t, rec, resolved, regressed)

	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, list, "internal/x/a.go", "the regression re-opened the id")

	before, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	out, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(out), "already closed",
		"the regression re-opened the id, so it is closeable again")
	assert.Contains(t, strings.ToLower(out), "marked")

	after, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, after, len(before)+1, "a second resolution record is appended")

	list, err = runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(list), "no items")
}

// An item filed as deferred through `atcr debt add` is the one live localdebt
// writer of that status (T3 Step 1 sweep), so its re-surfacing is asserted
// end-to-end through the command surface rather than only at the fold.
func TestDebtNamespace_AddedDeferredItemResurfacesOnRedetection(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir, "--status", "deferred",
		"--severity", "HIGH", "--file", "a.go:3", "--problem", "P", "--fix", "F", "--category", "correctness")
	require.NoError(t, err)

	list, err := runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(list), "no items")

	filed := readDebtStore(t, dir)
	require.Len(t, filed, 1)
	redetect(t, dir, filed[0])

	list, err = runDebt(t, "resolve", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, list, "a.go", "the deferred item re-surfaces when it is detected again")
}

// Resolving a deferred item must carry its reviewer attribution onto the
// resolution record. The union loop skips SETTLED records, not merely closed
// ones — skipping the deferred record would resolve it with an empty union and
// deny every persona that raised the finding its confirmed credit.
func TestDebtResolve_ResolvingADeferredItemKeepsReviewerAttribution(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	deferred := rec
	deferred.RunID = "2026-07-02T10:00:00Z-a-deferred"
	deferred.Timestamp = deferred.RunID
	deferred.Status = "deferred"
	deferred.Reviewers = []string{"security", "performance"}
	deferred.Model = "claude-sonnet-4-6"
	dir := writeDebtStore(t, rec, deferred)

	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID)
	require.NoError(t, err)

	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	var resolution *localdebt.Record
	for i := range recs {
		if recs[i].Status == "resolved" {
			resolution = &recs[i]
		}
	}
	require.NotNil(t, resolution)
	assert.ElementsMatch(t, []string{"claude", "security", "performance"}, resolution.Reviewers,
		"attribution unions across every LIVE record, deferred included")
	assert.Equal(t, "claude-sonnet-4-6", resolution.Model)
}

// --- T4: two-pass select-then-hydrate (AC6 second call site) ----------------

// summarize projects records the way localdebt's streaming read would, so the
// pure pass-1 selection can be exercised without touching disk.
func summarize(recs ...localdebt.Record) []localdebt.Summary {
	sums := make([]localdebt.Summary, 0, len(recs))
	for _, r := range recs {
		sums = append(sums, localdebt.Summary{
			ID: r.ID, Status: r.Status, Severity: r.Severity,
			Timestamp: r.Timestamp, HasFile: r.File != "",
		})
	}
	return sums
}

// TestSelectOpenIDs_ExcludesClosedAndFilelessIDs ports the exclusion half of the
// old selectOpenDebt assertions onto the pure pass-1 function.
func TestSelectOpenIDs_ExcludesClosedAndFilelessIDs(t *testing.T) {
	open := openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "still open")
	resolved := openRec("2026-07-01T10:00:00Z-b", "HIGH", "b.go", 2, "already fixed")
	resolved.Status = "resolved"
	dismissed := openRec("2026-07-01T10:00:00Z-c", "HIGH", "c.go", 3, "false positive")
	dismissed.Status = "wontfix"
	deferred := openRec("2026-07-01T10:00:00Z-d", "HIGH", "d.go", 4, "not now")
	deferred.Status = "deferred"
	fileless := openRec("2026-07-01T10:00:00Z-e", "HIGH", "", 0, "no location")
	fileless.StampID()

	got := selectOpenIDs(summarize(open, resolved, dismissed, deferred, fileless), "", 0)
	assert.Equal(t, []string{open.ID}, got,
		"closed (incl. deferred, which is off the fix worklist) and location-less ids are excluded")
}

// TestSelectOpenIDs_SeverityFilterAndOrdering ports the filter/sort/cap half.
func TestSelectOpenIDs_SeverityFilterAndOrdering(t *testing.T) {
	low := openRec("2026-07-05T10:00:00Z-low", "LOW", "z/low.go", 1, "low sev")
	highNewer := openRec("2026-07-04T10:00:00Z-h2", "HIGH", "z/high2.go", 2, "high newer")
	highOlder := openRec("2026-07-01T10:00:00Z-h1", "HIGH", "z/high1.go", 3, "high older")
	sums := summarize(low, highNewer, highOlder)

	assert.Equal(t, []string{highOlder.ID, highNewer.ID, low.ID}, selectOpenIDs(sums, "", 0),
		"severity DESC, then ts ASC within a severity")
	assert.Equal(t, []string{highOlder.ID, highNewer.ID}, selectOpenIDs(sums, "HIGH", 0),
		"the severity filter is applied before the cap")
	assert.Equal(t, []string{highOlder.ID}, selectOpenIDs(sums, "", 1), "--max caps the selection")
	assert.Len(t, selectOpenIDs(sums, "", 0), 3, "limit 0 means no cap")
	assert.Empty(t, selectOpenIDs(nil, "", 0), "an empty store selects nothing")
}

// TestHydrateOpenDebt_RetainsOnlySelectedIDsInPassOneOrder pins pass 2: it
// materializes records for the selected ids ONLY, and returns them in the order
// pass 1 chose rather than the store's read order.
func TestHydrateOpenDebt_RetainsOnlySelectedIDsInPassOneOrder(t *testing.T) {
	first := openRec("2026-07-01T10:00:00Z-a", "LOW", "a.go", 1, "first")
	second := openRec("2026-07-02T10:00:00Z-b", "HIGH", "b.go", 2, "second")
	unselected := openRec("2026-07-03T10:00:00Z-c", "HIGH", "c.go", 3, "not selected")
	dir := writeDebtStore(t, first, second, unselected)

	got, err := hydrateOpenDebt(dir, []string{second.ID, first.ID}, localdebt.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, 2, "only the selected ids are hydrated")
	assert.Equal(t, second.ID, got[0].ID, "pass 1's order is authoritative, not the store's")
	assert.Equal(t, first.ID, got[1].ID)
	assert.Equal(t, "second", got[0].Problem, "full record fields are present after hydration")
}

// TestHydrateOpenDebt_MissingStoreIsEmpty matches ReadAll's "no backlog yet"
// convention rather than surfacing an error for a repo that never reconciled.
func TestHydrateOpenDebt_MissingStoreIsEmpty(t *testing.T) {
	got, err := hydrateOpenDebt(t.TempDir()+"/nope", []string{"deadbeef"}, localdebt.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TD (pass-2 shortfall): an id pass 1 selected can vanish before pass 2 re-reads
// the store — a concurrent `debt compact` renaming shards makes ReadRecords hit
// os.IsNotExist and continue. Dropping it silently makes a short WORKLIST read
// exactly like a short BACKLOG, with exit code 0 either way. Pass 2 must say on
// stderr how many selected ids it could not hydrate.
func TestHydrateOpenDebt_NotesSelectedIDsNotFoundOnReRead(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "boom")
	dir := writeDebtStore(t, rec)

	var stderr bytes.Buffer
	got, err := hydrateOpenDebt(dir, []string{rec.ID, "deadbeef-gone"},
		localdebt.ReadOpts{Writer: &stderr})
	require.NoError(t, err)
	require.Len(t, got, 1, "the surviving id is still hydrated")
	assert.Equal(t, rec.ID, got[0].ID)
	assert.Contains(t, stderr.String(), "1 selected item(s)",
		"the note names how many selected ids were not hydrated")
	assert.Contains(t, stderr.String(), "note:")

	// A nil Writer must not panic: the note is best-effort, never a failure.
	got, err = hydrateOpenDebt(dir, []string{"deadbeef-gone"}, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TD (renderResolveList): every interpolated column must pass through the same
// sanitizers renderDebtTable uses. A tab or bare CR in File/Severity would tear
// the tabwriter row into extra lines, and an empty id must render as the literal
// "-" the command's contract documents — never a blank first column.
func TestRenderResolveList_SanitizesEveryColumn(t *testing.T) {
	dirty := openRec("2026-07-01T10:00:00Z-a", "HI\tGH", "pkg/x.go\tpkg/y.go", 1, "boom")
	emptyID := openRec("2026-07-02T10:00:00Z-b", "LOW", "b.go", 2, "leak")
	emptyID.ID = "" // only reachable from a hand-edited store

	var b bytes.Buffer
	require.NoError(t, renderResolveList(&b, t.TempDir(), []localdebt.Record{dirty, emptyID}))
	out := b.String()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 3, "header + one row per record; a raw tab/CR must not tear a row:\n%q", out)
	assert.NotContains(t, lines[1], "\t", "no raw tab survives into the rendered row")
	assert.Contains(t, lines[1], "HI GH")
	assert.Contains(t, lines[1], "pkg/x.go pkg/y.go")
	assert.True(t, strings.HasPrefix(lines[2], "-"), "an empty id renders as -, got %q", lines[2])
}

// TestDebtResolve_TwoPassSelectionEndToEnd runs the real command against a store
// holding every excluded shape, and asserts the rendered rows carry the full
// Severity/File/Line/Problem values — i.e. that hydration actually happened and
// the minimal decode did not leak into the output.
func TestDebtResolve_TwoPassSelectionEndToEnd(t *testing.T) {
	open := openRec("2026-07-01T10:00:00Z-a", "CRITICAL", "internal/x/a.go", 42, "still open")
	resolved := openRec("2026-07-01T10:00:00Z-b", "HIGH", "internal/x/b.go", 7, "already fixed")
	resolved.Status = "resolved"
	dismissed := openRec("2026-07-01T10:00:00Z-c", "HIGH", "internal/x/c.go", 8, "false positive")
	dismissed.Status = "wontfix"
	fileless := openRec("2026-07-01T10:00:00Z-d", "HIGH", "", 0, "no location")
	fileless.StampID()
	dir := writeDebtStore(t, open, resolved, dismissed, fileless)

	out, err := runDebt(t, "resolve", "--dir", dir, "--json")
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &items))
	require.Len(t, items, 1, "only the open, located item is on the worklist")
	assert.Equal(t, open.ID, items[0]["id"])
	assert.Equal(t, "CRITICAL", items[0]["severity"])
	assert.Equal(t, "internal/x/a.go", items[0]["file"])
	assert.Equal(t, float64(42), items[0]["line"])
	assert.Equal(t, "still open", items[0]["problem"])
	assert.Equal(t, "apply the fix", items[0]["fix"], "a hydrated record carries fields Summary never decodes")
}

// TestHydrateOpenDebt_ErrorDoesNotLeakAbsolutePath keeps pass 2 inside the store
// package's SECURITY contract. The read this replaced went through
// localdebt.ReadAll, which redacts a surfaced *os.PathError to its base name; a
// hand-rolled shard walk that returns the raw error regresses that.
func TestHydrateOpenDebt_ErrorDoesNotLeakAbsolutePath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied open cannot be provoked as root")
	}
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "boom")
	dir := writeDebtStore(t, rec)
	shard := filepath.Join(dir, "2026-07.jsonl")
	require.NoError(t, os.Chmod(shard, 0o000))
	t.Cleanup(func() { _ = os.Chmod(shard, 0o600) })

	_, err := hydrateOpenDebt(dir, []string{rec.ID}, localdebt.ReadOpts{Writer: io.Discard})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), dir,
		"a shard read error must not embed the absolute (username-bearing) store path")
}

// TestSelectOpenDebt_CrossShardDisplaysLastOccurrence pins the display rule across
// a shard boundary: the two passes enumerate shards independently, so an id whose
// occurrences straddle two months must still render the occurrence FoldRecords
// keeps (the last), not whichever one pass 2 happened to read first.
func TestSelectOpenDebt_CrossShardDisplaysLastOccurrence(t *testing.T) {
	first := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	second := first
	second.RunID = "2026-07-01T10:00:00Z-b"
	second.Timestamp = "2026-07-01T10:00:00Z"
	second.Severity = "LOW"
	require.Equal(t, first.ID, second.ID, "identical File/Line/Problem share a stable id")

	dir := writeDebtStore(t, first, second)
	require.FileExists(t, filepath.Join(dir, "2026-06.jsonl"))
	require.FileExists(t, filepath.Join(dir, "2026-07.jsonl"), "the occurrences must straddle two shards")

	got, err := selectOpenDebt(dir, "", 0, localdebt.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "LOW", got[0].Severity,
		"the LAST occurrence wins even when it lives in a later shard")
}

// TestDebtAdd_DoesNotStampCountersOnTheFiledRecord pins the cli half of the
// counting contract. Stamping Occurrences here would make the filed record a
// counting carrier, and its timestamp the boundary for every earlier sighting of
// the same id — silently DECREASING the count when a user files something that
// hashes to an id the store already holds.
func TestDebtAdd_DoesNotStampCountersOnTheFiledRecord(t *testing.T) {
	dir := t.TempDir()
	_, err := runDebt(t, "add", "--dir", dir,
		"--severity", "HIGH", "--file", "a.go:12", "--problem", "boom",
		"--fix", "fix it", "--category", "correctness", "--status", "deferred")
	require.NoError(t, err)

	recs := readStoreRecords(t, dir)
	require.Len(t, recs, 1)
	assert.Zero(t, recs[0].Occurrences, "a filed item is appended with no carried count")
	assert.Empty(t, recs[0].FirstSeen)
	assert.Empty(t, recs[0].CountedThrough)

	// And the counting rule still treats it as a sighting, via origin + no ResolvedAt.
	folded := localdebt.FoldRecords(recs)
	require.Len(t, folded, 1)
	assert.Equal(t, 1, folded[0].Occurrences)
}

// TestDebtResolve_ResolutionRecordCarriesNoCounters pins the other cli half: the
// resolution copies the FOLDED record, which carries the id's aggregate, so it must
// zero the counters or the fold counts that history twice.
func TestDebtResolve_ResolutionRecordCarriesNoCounters(t *testing.T) {
	first := openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "counted twice?")
	second := first
	second.RunID = "2026-07-02T10:00:00Z-b"
	second.Timestamp = "2026-07-02T10:00:00Z"
	dir := writeDebtStore(t, first, second)
	require.Equal(t, 2, localdebt.FoldRecords(readStoreRecords(t, dir))[0].Occurrences)

	_, err := runDebt(t, "resolve", "--dir", dir, first.ID, "--reason", "fixed")
	require.NoError(t, err)

	recs := readStoreRecords(t, dir)
	var resolution *localdebt.Record
	for i := range recs {
		if recs[i].Status == "resolved" {
			resolution = &recs[i]
		}
	}
	require.NotNil(t, resolution)
	assert.Zero(t, resolution.Occurrences, "the appended resolution must not carry the folded aggregate")
	assert.Empty(t, resolution.FirstSeen)
	assert.Empty(t, resolution.CountedThrough)

	assert.Equal(t, 2, localdebt.FoldRecords(recs)[0].Occurrences,
		"resolving does not add a sighting, so the count is unchanged")
}

func TestDebtResolve_ResolutionRecordOverReadCapIsRejected(t *testing.T) {
	// The resolution copies the finding verbatim and ADDS terminal fields, so a
	// finding whose encoded record sits just under the store's read cap
	// (localdebt.MaxRecordBytes) tips its resolution line OVER it. An unguarded
	// append writes a line every read path silently skips: the command reports
	// "Marked <id> resolved." and exits 0 while the item stays open forever. The
	// resolution must be rejected BEFORE the append, the way `debt add` bounds its
	// encoded record and `--reason` bounds its justification.
	base := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "")
	empty, err := json.Marshal(base)
	require.NoError(t, err)
	// Pad Problem so the OPEN record encodes 64 bytes under the cap...
	base.Problem = strings.Repeat("x", localdebt.MaxRecordBytes-len(empty)-1-64)
	openLine, err := json.Marshal(base)
	require.NoError(t, err)
	require.LessOrEqual(t, len(openLine)+1, localdebt.MaxRecordBytes,
		"fixture: the open record itself must still be readable")
	// ...while the resolution record the command builds from it — folded record
	// with counters zeroed plus the terminal fields and a within-cap --reason —
	// exceeds the cap. maxReasonBytes bounds the reason STRING, not the record.
	reason := strings.Repeat("r", 2048)
	eff := localdebt.FoldRecords([]localdebt.Record{base})[0]
	res := eff
	res.RunID = "2026-08-05T12:00:00Z-resolved"
	res.Timestamp = res.RunID
	res.Status = "resolved"
	res.ResolvedAt = res.RunID
	res.Occurrences = 0
	res.FirstSeen = ""
	res.CountedThrough = ""
	res.Justification = reason
	resLine, err := json.Marshal(res)
	require.NoError(t, err)
	require.Greater(t, len(resLine)+1, localdebt.MaxRecordBytes,
		"fixture: the resolution record must exceed the read cap")

	dir := writeDebtStore(t, base)
	before := readStoreRecords(t, dir)

	out, err := runDebt(t, "resolve", "--dir", dir, base.ID, "--reason", reason)
	require.Error(t, err, "a resolution record over the read cap must be rejected, not written invisibly")
	assert.NotContains(t, out, "Marked", "must not report success for a resolution that cannot be read back")

	after := readStoreRecords(t, dir)
	assert.Len(t, after, len(before), "a rejected oversized resolution must not append a record")
}

func TestDebtResolve_AlreadyClosedPrintsNormalizedStatus(t *testing.T) {
	// The already-closed gate (localdebt.IsSettledStatus) lowercases and trims
	// before comparing, so a stored status of "  ReSoLvEd  " settles the item.
	// The message must print the canonical status the gate matched — store text
	// is untrusted (the world-appendable .atcr/debt/ store) and must never be
	// echoed verbatim to the terminal.
	open := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	term := open
	term.RunID = "2026-07-01T11:00:00Z-a-resolved"
	term.Timestamp = term.RunID
	term.Status = "  ReSoLvEd  "
	dir := writeDebtStore(t, open, term)

	out, err := runDebt(t, "resolve", "--dir", dir, open.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "already closed as resolved",
		"the message must print the normalized status the gate matched")
	assert.NotContains(t, out, "ReSoLvEd", "raw store text must not be echoed to the terminal")
}

// readStoreRecords reads every record from a test store dir.
func readStoreRecords(t *testing.T, dir string) []localdebt.Record {
	t.Helper()
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	return recs
}

// TestCollectDebtIDRecords_RetainsRawRecordsForWantedIDsOnly pins the shared
// shard-walk markDebtResolved now uses instead of localdebt.ReadAll (TD
// cli/debt_resolve.go:357): it returns the RAW, unfolded records — an open
// record AND its terminal sibling for one id both come back, because the
// caller folds locally — and only for the wanted ids, so peak memory is one
// shard plus the wanted ids' history rather than the whole store.
func TestCollectDebtIDRecords_RetainsRawRecordsForWantedIDsOnly(t *testing.T) {
	open := openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "boom")
	term := open
	term.RunID = "2026-07-01T11:00:00Z-a-resolved"
	term.Timestamp = term.RunID
	term.Status = "resolved"
	other := openRec("2026-07-01T12:00:00Z-b", "LOW", "b.go", 2, "leak")
	dir := writeDebtStore(t, open, term, other)

	retained, err := collectDebtIDRecords(dir, []string{open.ID}, localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, retained, 2, "the open record AND its terminal sibling are both retained, unfolded")
	for _, r := range retained {
		assert.Equal(t, open.ID, r.ID, "only the wanted id's records are retained")
	}

	retained, err = collectDebtIDRecords(dir, []string{"no-such-id"}, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Empty(t, retained)

	retained, err = collectDebtIDRecords(dir, nil, localdebt.ReadOpts{})
	require.NoError(t, err)
	assert.Empty(t, retained, "no wanted ids is a no-op, not a full scan")
}

// The wontfix guard accepts ANY non-empty Justification as the recorded rationale
// for a permanent, backlog-suppressing dismissal. A justification consisting only of
// atcr's own elision placeholder is 23 bytes of TOOL-generated text carrying zero
// reviewer content — the reconcile enrichment path could produce exactly that from a
// finding anchored inside a fenced quote. Accepting it appends a terminal wontfix
// whose entire audit trail is a marker meaning "a quote was dropped here", and
// localdebt preserves that record through compaction on the stated grounds that it
// holds human-typed --reason text which exists nowhere else in the tree.
func TestDebtResolve_WontfixRejectsAPlaceholderOnlyJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	rec.Justification = reconcile.ElidedQuotePlaceholder
	rec.StampID()
	dir := writeDebtStore(t, rec)

	_, err := runDebt(t, "resolve", "--dir", dir, rec.ID, "--status", "wontfix")
	require.Error(t, err, "a justification made only of elision placeholders is not a recorded rationale")
	assert.Equal(t, exitUsage, exitCode(err))

	// Repeated placeholders (a multi-fence excerpt) are the same nothing.
	rec2 := openRec("2026-07-01T10:00:00Z-b", "HIGH", "internal/x/b.go", 12, "boom")
	rec2.Justification = reconcile.ElidedQuotePlaceholder + "\n" + reconcile.ElidedQuotePlaceholder
	rec2.StampID()
	dir2 := writeDebtStore(t, rec2)

	_, err = runDebt(t, "resolve", "--dir", dir2, rec2.ID, "--status", "wontfix")
	require.Error(t, err, "more placeholders is not more rationale")
	assert.Equal(t, exitUsage, exitCode(err))

	// A justification with real prose beside a placeholder still counts.
	rec3 := openRec("2026-07-01T10:00:00Z-c", "HIGH", "internal/x/c.go", 12, "boom")
	rec3.Justification = "the reviewer's actual reasoning\n" + reconcile.ElidedQuotePlaceholder
	rec3.StampID()
	dir3 := writeDebtStore(t, rec3)

	_, err = runDebt(t, "resolve", "--dir", dir3, rec3.ID, "--status", "wontfix")
	require.NoError(t, err, "an excerpt carrying real reviewer prose is still a rationale")
}
