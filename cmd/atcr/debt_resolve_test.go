package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// openRec builds an open (no-status) local-debt record with a stable id, mirroring
// what the reconcile persistence hook writes (cmd/atcr/reconcile.go:181-196).
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
	out, err := runDebt(t, "resolve", "--dir", dir, "--list")
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
	out, err := runDebt(t, "resolve", "--dir", dir, "--list")
	require.NoError(t, err, "empty store must exit 0, never a non-zero exit")
	assert.Contains(t, strings.ToLower(out), "no items")
}

func TestDebtResolve_MissingDirIsNotAnError(t *testing.T) {
	out, err := runDebt(t, "resolve", "--dir", t.TempDir()+"/does-not-exist", "--list")
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

func TestDebtResolve_SelectionSortsSeverityThenAge(t *testing.T) {
	// Written newest-first and lowest-severity-first to prove the command re-sorts:
	// severity DESC (HIGH before LOW), then ts ASC (oldest first) within a severity.
	dir := writeDebtStore(t,
		openRec("2026-07-05T10:00:00Z-low", "LOW", "z/low.go", 1, "low sev"),
		openRec("2026-07-04T10:00:00Z-h2", "HIGH", "z/high2.go", 2, "high newer"),
		openRec("2026-07-01T10:00:00Z-h1", "HIGH", "z/high1.go", 3, "high older"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir, "--list")
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

func TestDebtResolve_MarkResolvedRemovesItemFromOpenList(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec,
		openRec("2026-07-02T10:00:00Z-b", "LOW", "internal/y/b.go", 34, "leak"),
	)
	out, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "resolved")

	// The append-only resolution record must fold the item out of the open list.
	list, err := runDebt(t, "resolve", "--dir", dir, "--list")
	require.NoError(t, err)
	assert.NotContains(t, list, "internal/x/a.go", "a resolved item must not appear as open")
	assert.Contains(t, list, "internal/y/b.go", "the other item stays open")
}

func TestDebtResolve_MarkResolvedIsIdempotent(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID)
	require.NoError(t, err)

	// A second resolve of the same id must no-op, not append a duplicate record.
	before, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	out, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID)
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
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)

	// A subsequent plain resolve must report the existing terminal status, not
	// hardcode "already resolved".
	out, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "wontfix", "already-closed message must name the actual terminal status")
	assert.NotContains(t, strings.ToLower(out), "already resolved", "must not hardcode 'already resolved' when the item is wontfix")
}

func TestDebtResolve_MarkResolvedUnknownIDErrors(t *testing.T) {
	dir := writeDebtStore(t, openRec("2026-07-01T10:00:00Z-a", "HIGH", "a.go", 1, "x"))
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", "deadbeef")
	require.Error(t, err, "resolving an unknown id must error, not silently no-op")
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

	list, err := runDebt(t, "resolve", "--dir", dir, "--list")
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
	out, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
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
	list, err := runDebt(t, "resolve", "--dir", dir, "--list")
	require.NoError(t, err)
	assert.NotContains(t, list, "internal/x/a.go", "a wontfix item must not appear as open")
	assert.Contains(t, list, "internal/y/b.go", "the other item stays open")
}

func TestDebtResolve_DefaultStatusStaysResolved(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID)
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
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "bogus")
	require.Error(t, err, "an unrecognized --status must be a usage error, not a silently non-folding record")

	// The error must report the canonical lowercase form, not the user's casing.
	out, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "BOGUS")
	require.Error(t, err)
	assert.Contains(t, out, `invalid --status "bogus"`, "error must show canonical lowercase status")
	assert.NotContains(t, out, `invalid --status "BOGUS"`, "error must not echo user's uppercase input")
}

func TestDebtResolve_WontfixRequiresReasonOrJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)

	// wontfix with no --reason and no pre-existing justification must be rejected.
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "wontfix")
	require.Error(t, err, "wontfix without a reason or existing justification must be a usage error")
	assert.Equal(t, exitUsage, exitCode(err))

	// wontfix with a --reason is allowed.
	_, err = runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "wontfix", "--reason", "accepted pattern")
	require.NoError(t, err)
}

func TestDebtResolve_ReasonPopulatesJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	dir := writeDebtStore(t, rec)
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID,
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
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "wontfix", "--reason", "   ")
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

func TestDebtResolve_NoReasonPreservesExistingJustification(t *testing.T) {
	rec := openRec("2026-07-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	rec.Justification = "original enrichment note"
	dir := writeDebtStore(t, rec)
	// Omitting --reason must not blank an existing justification carried on the item.
	_, err := runDebt(t, "resolve", "--dir", dir, "--resolve", rec.ID, "--status", "wontfix")
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

	// --status without --resolve must not silently fall through to the list view:
	// it would drop the user's dismissal intent (and skip status validation).
	out, err := runDebt(t, "resolve", "--dir", dir, "--status", "wontfix")
	require.Error(t, err, "--status without --resolve must be a usage error, not a silent list")
	el := errorLine(out)
	assert.Contains(t, el, "--status", "error must mention only the supplied flag")
	assert.NotContains(t, el, "--reason", "error must not mention --reason when only --status was supplied")

	// The explicit default value must also be rejected without --resolve; this
	// path is distinct from a non-default status and locks the guard behavior.
	out, err = runDebt(t, "resolve", "--dir", dir, "--status", "resolved")
	require.Error(t, err, "--status resolved without --resolve must be a usage error")
	el = errorLine(out)
	assert.Contains(t, el, "--status")
	assert.NotContains(t, el, "--reason")

	// --reason without --resolve is the same footgun.
	out, err = runDebt(t, "resolve", "--dir", dir, "--reason", "some note")
	require.Error(t, err, "--reason without --resolve must be a usage error")
	el = errorLine(out)
	assert.Contains(t, el, "--reason", "error must mention only the supplied flag")
	assert.NotContains(t, el, "--status", "error must not mention --status when only --reason was supplied")

	// An explicitly empty --reason without --resolve must also be rejected; it
	// should be governed by Changed("reason"), not by the trimmed value.
	out, err = runDebt(t, "resolve", "--dir", dir, "--reason", "")
	require.Error(t, err, "explicit --reason=\"\" without --resolve must be a usage error")
	el = errorLine(out)
	assert.Contains(t, el, "--reason")
	assert.NotContains(t, el, "--status")

	// An explicitly empty --status is invalid status anyway, but it must also be
	// rejected at the guard before falling through to the list view.
	_, err = runDebt(t, "resolve", "--dir", dir, "--status", "")
	require.Error(t, err, "explicit --status=\"\" without --resolve must be a usage error")

	// Plain --list (no --status/--reason) still works untouched.
	_, err = runDebt(t, "resolve", "--dir", dir, "--list")
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
