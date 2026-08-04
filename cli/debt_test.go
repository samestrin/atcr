package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtSampleRecords is a small, deterministic corpus spanning severities,
// statuses, components, and two dates so the filter/sort/aggregate tests share
// one fixture. It is the localdebt.Record retyping of the corpus the deleted
// internal/debt package's tests used, so the ported assertions stay comparable
// case-for-case.
//
// Open records carry the empty Status the reconcile hook writes — the canonical
// on-disk spelling of "open" — rather than a literal "open", so the filter's
// empty-is-open rule is exercised by the shared corpus rather than by a
// bespoke one.
func debtSampleRecords() []localdebt.Record {
	recs := []localdebt.Record{
		{
			SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-run", Timestamp: "2026-06-13T09:00:00Z",
			Severity: "HIGH", File: "internal/autofix/apply.go", Line: 108,
			Problem: "clobber on create", Fix: "stat first", Category: "correctness", EstMinutes: 60,
		},
		{
			SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-run", Timestamp: "2026-06-13T09:00:00Z",
			Severity: "LOW", File: "internal/autofix/revert.go", Line: 41,
			Problem: "perm loss", Fix: "assert mode", Category: "correctness", EstMinutes: 30,
			Status: "resolved",
		},
		{
			SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-run", Timestamp: "2026-06-26T09:00:00Z",
			Severity: "CRITICAL", File: "cmd/atcr/autofix.go", Line: 248,
			Problem: "remote leftover", Fix: "message op", Category: "docs", EstMinutes: 15,
		},
		{
			SchemaVersion: localdebt.SchemaVersion, RunID: "2026-06-run", Timestamp: "2026-06-26T09:00:00Z",
			Severity: "MEDIUM", File: "cmd/atcr/review.go",
			Problem: "exit gate surprise", Fix: "document it", Category: "docs",
			Status: "deferred",
		},
	}
	for i := range recs {
		recs[i].StampID()
	}
	return recs
}

// writeLocalDebt appends the shared corpus into an isolated .atcr/debt store and
// returns its directory, the --dir value every debt subcommand takes.
func writeLocalDebt(t *testing.T, recs ...localdebt.Record) string {
	t.Helper()
	if len(recs) == 0 {
		recs = debtSampleRecords()
	}
	dir := filepath.Join(t.TempDir(), ".atcr", "debt")
	for _, r := range recs {
		require.NoError(t, localdebt.Append(dir, r))
	}
	return dir
}

func runDebt(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// Default to a non-TTY stdin so add's interactive path is not triggered by a
	// real terminal under the test runner; tests exercising the wizard set their
	// own reader and force debtStdinIsTTY.
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestDebt_CommandWiring(t *testing.T) {
	cmd := newDebtCmd()
	assert.Equal(t, "debt", cmd.Name())

	var hasList bool
	for _, c := range cmd.Commands() {
		if c.Name() == "list" {
			hasList = true
		}
	}
	assert.True(t, hasList, "debt has a list subcommand")

	root := NewRootCmd()
	var registered bool
	for _, c := range root.Commands() {
		if c.Name() == "debt" {
			registered = true
		}
	}
	assert.True(t, registered, "debt is registered on the root command")
}

// AC1: every subcommand resolves its store through --dir, defaulting to the same
// localdebt.DefaultDir(".") that resolve and compact already use. A default that
// diverges between subcommands is exactly the two-store split this plan removes.
func TestDebt_AllSubcommandsShareTheSameStoreDefault(t *testing.T) {
	cmd := newDebtCmd()
	for _, name := range []string{"list", "add", "dashboard", "resolve", "compact"} {
		sub := debtSubcommand(t, cmd, name)
		f := sub.Flags().Lookup("dir")
		require.NotNil(t, f, "%s registers --dir", name)
		assert.Equal(t, defaultDebtResolveDir, f.DefValue,
			"%s --dir defaults to the shared .atcr/debt store", name)
	}
}

// debtSubcommand looks up a named `atcr debt` subcommand, failing the test when
// it is absent.
func debtSubcommand(t *testing.T, cmd *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range cmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("debt has no %q subcommand", name)
	return nil
}

// AC11: the retired .planning/-scoped flags are gone from list. A script passing
// one now fails with cobra's unknown-flag error, which is the correct outcome —
// they pointed at a store the command no longer reads.
func TestDebtList_RetiredFlagsAreGone(t *testing.T) {
	sub := debtSubcommand(t, newDebtCmd(), "list")
	for _, name := range []string{"items", "readme", "sync", "group"} {
		assert.Nil(t, sub.Flags().Lookup(name), "--%s is retired", name)
	}
}

func TestDebtList_RendersTable(t *testing.T) {
	dir := writeLocalDebt(t)
	out, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)

	assert.Contains(t, out, "SEVERITY")
	assert.Contains(t, out, "CRITICAL")
	assert.Contains(t, out, "HIGH")
	assert.Contains(t, out, "cmd/atcr/autofix.go:248")
	// Default sort is severity: CRITICAL row appears before HIGH row.
	assert.Less(t, strings.Index(out, "CRITICAL"), strings.Index(out, "HIGH"))
}

// AC1/AC9 overlap: the id is the leading column so a listed item is directly
// copy-pasteable into `atcr debt resolve <id>`. The GROUP column it replaces was
// cadence workflow vocabulary and does not survive the seam.
func TestDebtList_RendersIDColumnAndNoGroup(t *testing.T) {
	recs := debtSampleRecords()
	dir := writeLocalDebt(t, recs...)
	out, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.NotEmpty(t, lines)
	assert.True(t, strings.HasPrefix(lines[0], "ID"), "ID is the leading column; got header %q", lines[0])
	assert.NotContains(t, lines[0], "GROUP", "GROUP is retired with the cadence workflow fields")
	for _, r := range recs {
		assert.Contains(t, out, r.ID, "every rendered row carries its full, untruncated id")
	}
}

// The store's canonical open record carries an empty Status; the table must not
// render a blank cell for it.
func TestDebtList_EmptyStatusRendersAsOpen(t *testing.T) {
	dir := writeLocalDebt(t, debtSampleRecords()[0])
	out, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "open")
}

func TestDebtList_SeverityFilter(t *testing.T) {
	dir := writeLocalDebt(t)
	out, err := runDebt(t, "list", "--dir", dir, "--severity", "high")
	require.NoError(t, err)
	assert.Contains(t, out, "HIGH")
	assert.NotContains(t, out, "CRITICAL")
}

func TestDebtList_ComponentFilter(t *testing.T) {
	dir := writeLocalDebt(t)
	out, err := runDebt(t, "list", "--dir", dir, "--component", "cmd/atcr")
	require.NoError(t, err)
	assert.Contains(t, out, "cmd/atcr")
	assert.NotContains(t, out, "internal/autofix")
}

func TestDebtList_EmptyResultMessage(t *testing.T) {
	dir := writeLocalDebt(t)
	out, err := runDebt(t, "list", "--dir", dir, "--category", "no-such-category")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "no matching")
}

// An absent store is the "no backlog yet" state, not an error (localdebt.ReadAll
// returns nil,nil for a missing directory).
func TestDebtList_AbsentStoreIsNotAnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".atcr", "debt")
	out, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "no matching")
}

func TestDebtList_UnknownSortIsUsageError(t *testing.T) {
	dir := writeLocalDebt(t)
	_, err := runDebt(t, "list", "--dir", dir, "--sort", "bogus")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

// list renders the FOLDED effective record per id — the same precedence rule
// selectOpenDebt, Compact, and AggregateQualitySignal share — so a re-raised
// finding is one row, not one row per append.
func TestDebtList_FoldsToTheEffectiveRecordPerID(t *testing.T) {
	base := debtSampleRecords()[0]
	later := base
	later.Timestamp = "2026-07-01T09:00:00Z"
	later.Status = "resolved"
	later.ResolvedAt = later.Timestamp
	dir := writeLocalDebt(t, base, later)

	out, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2, "one header + one folded row; got:\n%s", out)
	assert.Contains(t, lines[1], "resolved")
}

// Status visibility (decisive): list has no default open-only filter, so a
// resolved item still renders with STATUS = resolved. --status open is the
// filter that hides it, and it must also match the empty-status open records.
func TestDebtList_StatusFilterOpenMatchesEmptyStatus(t *testing.T) {
	dir := writeLocalDebt(t)

	all, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, all, "resolved", "a bare list shows terminal items with their status")

	open, err := runDebt(t, "list", "--dir", dir, "--status", "open")
	require.NoError(t, err)
	assert.NotContains(t, open, "resolved")
	assert.NotContains(t, open, "deferred")
	assert.Contains(t, open, "internal/autofix/apply.go:108", "empty-status records match --status open")
}

func TestDebtList_SanitizesCellWhitespace(t *testing.T) {
	rec := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-07-run", Timestamp: "2026-07-01T09:00:00Z",
		Severity: "HIGH", File: "pkg/x.go\npkg/y.go", Line: 1,
		Problem: "boom\tkaboom", Fix: "fix", Category: "cor\trectness", EstMinutes: 15,
	}
	rec.StampID()
	dir := writeLocalDebt(t, rec)

	out, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)

	// Header plus exactly one data row: a literal newline or tab in any cell
	// must not tear the row into extra lines or split its columns.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 2, "each record must render as a single table row; got:\n%s", out)
	assert.Contains(t, lines[1], "HIGH")
	assert.Contains(t, lines[1], "pkg/x.go pkg/y.go:1")
	assert.Contains(t, lines[1], "cor rectness")
	assert.Contains(t, lines[1], "boom kaboom")
}

// ---------------------------------------------------------------------------
// Filter / sort — ported case-for-case from the deleted internal/debt package's
// debt_test.go. The Group cases do not survive: group is cadence workflow
// vocabulary excluded from the schema by the atcr<->cadence seam (AC2).
// ---------------------------------------------------------------------------

func TestDebtFilter_Match(t *testing.T) {
	recs := debtSampleRecords()

	cases := []struct {
		name string
		f    debtFilter
		want int
	}{
		{"zero filter passes all", debtFilter{}, 4},
		{"severity case-insensitive", debtFilter{Severity: "high"}, 1},
		{"status open matches empty status", debtFilter{Status: "open"}, 2},
		{"status resolved", debtFilter{Status: "resolved"}, 1},
		{"status deferred", debtFilter{Status: "deferred"}, 1},
		{"category substring", debtFilter{Category: "doc"}, 2},
		{"component prefix", debtFilter{Component: "internal/autofix"}, 2},
		{"component prefix cmd", debtFilter{Component: "cmd/atcr"}, 2},
		{"combined severity+component", debtFilter{Severity: "CRITICAL", Component: "cmd/atcr"}, 1},
		{"no match", debtFilter{Severity: "HIGH", Component: "cmd/atcr"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyDebtFilter(recs, tc.f)
			assert.Len(t, got, tc.want)
		})
	}
}

func TestDebtFilter_ComponentBoundary(t *testing.T) {
	recs := []localdebt.Record{
		{Severity: "HIGH", File: "cmd/atcr/review.go", Line: 1, Problem: "p"},
		{Severity: "HIGH", File: "cmder/other.go", Line: 1, Problem: "p"},
	}
	// A bare "cmd" component must not also match the unrelated "cmder" prefix.
	got := applyDebtFilter(recs, debtFilter{Component: "cmd"})
	require.Len(t, got, 1)
	assert.Equal(t, "cmd/atcr/review.go", got[0].File)
}

func TestApplyDebtFilter_ReturnsNonNilEmpty(t *testing.T) {
	got := applyDebtFilter(nil, debtFilter{Severity: "HIGH"})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestSortDebt_Severity(t *testing.T) {
	recs := debtSampleRecords()
	require.NoError(t, sortDebt(recs, sortKeySeverity))
	got := []string{recs[0].Severity, recs[1].Severity, recs[2].Severity, recs[3].Severity}
	assert.Equal(t, []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}, got)
}

func TestSortDebt_Age_OldestFirst(t *testing.T) {
	recs := debtSampleRecords()
	require.NoError(t, sortDebt(recs, sortKeyAge))
	assert.Equal(t, "2026-06-13", debtRecordDate(recs[0]))
	assert.Equal(t, "2026-06-26", debtRecordDate(recs[3]))
}

func TestSortDebt_Est_LargestFirst(t *testing.T) {
	recs := debtSampleRecords()
	require.NoError(t, sortDebt(recs, sortKeyEst))
	assert.Equal(t, 60, recs[0].EstMinutes)
	assert.Equal(t, 0, recs[3].EstMinutes)
}

func TestSortDebt_File_LexicographicWithLineTiebreak(t *testing.T) {
	recs := []localdebt.Record{
		{File: "b.go", Line: 1, Timestamp: "2026-06-01T00:00:00Z"},
		{File: "a.go", Line: 9, Timestamp: "2026-06-01T00:00:00Z"},
		{File: "a.go", Line: 2, Timestamp: "2026-06-01T00:00:00Z"},
	}
	require.NoError(t, sortDebt(recs, sortKeyFile))
	assert.Equal(t, []string{"a.go", "a.go", "b.go"}, []string{recs[0].File, recs[1].File, recs[2].File})
	assert.Equal(t, 2, recs[0].Line, "equal files break the tie on line, keeping the order total")
}

func TestSortDebt_UnknownKeyIsError(t *testing.T) {
	err := sortDebt(debtSampleRecords(), "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sort key")
}

func TestTruncate_GuardNonPositiveN(t *testing.T) {
	assert.Equal(t, "", truncate("abc", 0), "n==0 must not panic")
	assert.Equal(t, "", truncate("abc", -1), "negative n must not panic")
}
