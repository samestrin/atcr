package cli

import (
	"bytes"
	"encoding/json"
	"os"
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

// --- Plan 35.13 T7: --json, the id column, and the reader root --------------

// AC9 (json): list --json emits the record array a downstream consumer joins on,
// never the human-readable empty message.
func TestDebtList_JSONEmitsRecordArray(t *testing.T) {
	recs := debtSampleRecords()
	dir := writeLocalDebt(t, recs...)

	out, err := runDebt(t, "list", "--dir", dir, "--json")
	require.NoError(t, err)

	var got []localdebt.Record
	require.NoError(t, json.Unmarshal([]byte(out), &got), "output is a JSON array: %s", out)
	require.Len(t, got, len(recs))
	for _, r := range got {
		assert.NotEmpty(t, r.ID, "every emitted record carries its join key")
	}
}

// An empty result is [] — not null, and not "No matching technical-debt items."
// A consumer parsing the stream must never have to special-case the empty store.
func TestDebtList_JSONEmptyStoreEmitsEmptyArray(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".atcr", "debt")

	out, err := runDebt(t, "list", "--dir", dir, "--json")
	require.NoError(t, err)
	assert.Equal(t, "[]", strings.TrimSpace(out))
	assert.NotContains(t, out, "No matching")
}

// --json honors the same filters and ordering the table renders, so a consumer
// diffing successive runs sees content changes rather than order churn.
func TestDebtList_JSONRespectsFiltersAndOrder(t *testing.T) {
	dir := writeLocalDebt(t)

	out, err := runDebt(t, "list", "--dir", dir, "--component", "cmd/atcr", "--json")
	require.NoError(t, err)

	var got []localdebt.Record
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "CRITICAL", got[0].Severity, "default severity sort survives the JSON path")
	assert.Equal(t, "MEDIUM", got[1].Severity)
}

// AC9 (json): one renderer, two callers. A second encoder is exactly how the
// list and resolve shapes would drift apart on indent or field ordering.
func TestRenderDebtJSON_NilAndEmptyEncodeAsArray(t *testing.T) {
	for _, tc := range []struct {
		name string
		recs []localdebt.Record
	}{{"nil", nil}, {"empty", []localdebt.Record{}}} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			require.NoError(t, renderDebtJSON(&b, tc.recs))
			assert.Equal(t, "[]", strings.TrimSpace(b.String()))
		})
	}
}

// The two-space indent is a wire-format detail a downstream parser may already
// depend on; pin it rather than letting an encoder tweak change it silently.
func TestRenderDebtJSON_TwoSpaceIndent(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, renderDebtJSON(&b, debtSampleRecords()[:1]))
	// Array elements indent by one level, their fields by two.
	assert.Contains(t, b.String(), "[\n  {\n    \"schema_version\":")
}

// list --json and resolve --json go through renderDebtJSON, so the same record
// serializes identically on both surfaces.
func TestDebtJSON_ListAndResolveShareOneShape(t *testing.T) {
	rec := debtSampleRecords()[0]
	dir := writeLocalDebt(t, rec)

	listOut, err := runDebt(t, "list", "--dir", dir, "--json")
	require.NoError(t, err)
	resolveOut, err := runDebt(t, "resolve", "--dir", dir, "--json")
	require.NoError(t, err)
	assert.Equal(t, listOut, resolveOut)
}

// AC9 (list): the id is fixed-width and load-bearing, so truncate never touches
// it even when the row's problem text is cut.
func TestRenderDebtTable_IDIsNeverTruncated(t *testing.T) {
	rec := debtSampleRecords()[0]
	rec.Problem = strings.Repeat("a very long problem statement ", 8)
	rec.StampID()

	var b bytes.Buffer
	require.NoError(t, renderDebtTable(&b, []localdebt.Record{rec}))
	assert.Contains(t, b.String(), rec.ID, "the id survives whole")
	assert.Contains(t, b.String(), "…", "the problem text is still truncated")
}

// A hand-edited store is the only way an empty id reaches a renderer. Render a
// literal "-" rather than a blank cell, and never a computed fallback id:
// markDebtResolved matches on the stored ID, so a synthesized display id would
// print a value `resolve` cannot match — a copy-pasteable lie.
func TestRenderDebtTable_EmptyIDRendersDash(t *testing.T) {
	rec := debtSampleRecords()[0]
	rec.ID = ""

	var b bytes.Buffer
	require.NoError(t, renderDebtTable(&b, []localdebt.Record{rec}))
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[1], "-"), "an empty id renders as -, got %q", lines[1])
}

// AC9 (round trip): the id a human reads out of the table is the id resolve
// matches. Parsing the RENDERED output rather than the record struct is the
// point — it is what proves the rendered id is the resolvable id.
func TestDebtList_RenderedIDClosesTheItem(t *testing.T) {
	dir := writeLocalDebt(t, debtSampleRecords()[0])

	listed, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	id := firstRenderedID(t, listed)

	_, err = runDebt(t, "resolve", "--dir", dir, "--resolve", id)
	require.NoError(t, err, "the id copied out of the table closes the item")

	open, err := runDebt(t, "list", "--dir", dir, "--status", "open")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(open), "no matching", "the item left the open backlog")

	all, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, all, id, "a bare list still shows the same id")
	assert.Contains(t, all, "resolved")
}

// AC9 (cross-view): list, dashboard, and --json show ONE id for one record.
// Three renderers that disagree would break the join contract the ownership
// model hands downstream.
func TestDebtViews_ShowTheSameIDForOneRecord(t *testing.T) {
	dir := writeLocalDebt(t, debtSampleRecords()[0])

	listed, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	id := firstRenderedID(t, listed)

	dash, err := runDebt(t, "dashboard", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, dash, id, "the dashboard's Top Priority row carries the same id")

	jsonOut, err := runDebt(t, "list", "--dir", dir, "--json")
	require.NoError(t, err)
	var got []localdebt.Record
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &got))
	require.Len(t, got, 1)
	assert.Equal(t, id, got[0].ID)
}

// firstRenderedID copies the id out of the first data row of a rendered table,
// the way a human copies it off the terminal.
func firstRenderedID(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "expected a header and at least one row:\n%s", out)
	id := strings.Fields(lines[1])[0]
	require.NotEmpty(t, id)
	return id
}

// --- TD-020 (reader half): the store readers walk up to the repo root --------

// The writer moved to the manifest-recorded root in T6 while every reader stayed
// CWD-relative, so `atcr debt list` from a subdirectory silently read an empty
// store. The readers now share cli/root.go's existing .git/.atcr marker walk.
func TestDebtStoreDir_DefaultWalksUpToTheRepoRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	sub := filepath.Join(root, "internal", "deep")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	cmd := newDebtListCmd()
	require.NoError(t, cmd.Flags().Parse(nil))
	assert.Equal(t, localdebt.DefaultDir(root), debtStoreDir(cmd),
		"an unset --dir resolves against the repo root, not the working directory")
}

// A linked worktree and a submodule record their root with a .git FILE, not a
// directory. The write side (localdebt.validateRepoRoot) accepts both; the reader
// must too, or the store split this fix closes survives in exactly the checkouts
// where a developer is most likely to be running from a non-root directory.
func TestDebtStoreDir_AcceptsAGitFileMarker(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644))
	sub := filepath.Join(root, "internal", "deep")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	cmd := newDebtListCmd()
	require.NoError(t, cmd.Flags().Parse(nil))
	assert.Equal(t, localdebt.DefaultDir(root), debtStoreDir(cmd))
}

// The broader marker rule is scoped to the debt readers on purpose: it must not
// leak into the shared repoRoot() that config, telemetry consent, history, and
// audit resolve their state through (see TestRepoRoot_GitFileIsNotAMarkerForTheSharedWalk).
// A .git SYMLINK is not a marker in either walk — a link to an arbitrary
// directory must not pass as a repository root.
func TestDebtRepoRoot_GitSymlinkIsNotAMarker(t *testing.T) {
	outer := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outer, ".git"), 0o755))
	inner := filepath.Join(outer, "inner")
	require.NoError(t, os.MkdirAll(inner, 0o755))
	require.NoError(t, os.Symlink("..", filepath.Join(inner, ".git")))
	t.Chdir(inner)

	got, err := debtRepoRoot()
	require.NoError(t, err)
	// Compare symlink-resolved on both sides: t.TempDir and os.Getwd can disagree
	// on /var vs /private/var, which says nothing about which directory was picked.
	gotResolved, err := filepath.EvalSymlinks(got)
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(outer)
	require.NoError(t, err)
	assert.Equal(t, expected, gotResolved, "the walk skips a .git symlink and continues up")
}

// An explicit --dir still wins: it is the escape hatch every test and script uses.
func TestDebtStoreDir_ExplicitFlagWins(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	t.Chdir(root)

	cmd := newDebtListCmd()
	require.NoError(t, cmd.Flags().Parse([]string{"--dir", "/tmp/elsewhere/debt"}))
	assert.Equal(t, "/tmp/elsewhere/debt", debtStoreDir(cmd))
}

// No marker anywhere up the tree is the pre-existing behavior repoRoot already
// defines: fall back to the working directory. That is what keeps the debt suite
// (which chdirs into a bare temp dir) reading the store it just wrote.
func TestDebtStoreDir_NoMarkerFallsBackToWorkingDir(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	cmd := newDebtListCmd()
	require.NoError(t, cmd.Flags().Parse(nil))
	got := debtStoreDir(cmd)
	// The fallback root is the working directory itself unless an ancestor of the
	// temp tree happens to carry a marker; either way the path must be absolute
	// and end at the store subdirectory, never a stale process-start directory.
	assert.True(t, filepath.IsAbs(got), "the resolved store path is absolute, got %q", got)
	assert.Equal(t, localdebt.DefaultDir(filepath.Dir(filepath.Dir(got))), got)
}

// An id filed by add from a SUBDIRECTORY is visible to list from that same
// subdirectory — AC1's headline claim, which was true only at the repo root
// before this fix.
func TestDebtAddThenList_FromASubdirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	sub := filepath.Join(root, "internal", "deep")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)

	_, err := runDebt(t, "add",
		"--severity", "HIGH", "--file", "internal/x/y.go:12",
		"--problem", "unbounded retry loop on 5xx", "--fix", "cap retries",
		"--category", "correctness", "--est", "30")
	require.NoError(t, err)

	out, err := runDebt(t, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "unbounded retry loop on 5xx",
		"list reads the store add just wrote, from the same subdirectory")
}

// --- Plan 35.13 T3 ripple: list and dashboard share the fold ----------------

// list and dashboard are the third consumer of FoldRecords, so the resolution
// lifetimes reach them too: a regressed id renders as open, not resolved, and the
// dashboard counts it in the live backlog.
func TestDebtList_RegressedIDRendersAsOpenNotResolved(t *testing.T) {
	base := debtSampleRecords()[0]
	resolved := base
	resolved.Timestamp = "2026-07-02T09:00:00Z"
	resolved.Status = "resolved"
	resolved.ResolvedAt = resolved.Timestamp
	regressed := base
	regressed.Timestamp = "2026-07-03T09:00:00Z"
	dir := writeLocalDebt(t, base, resolved, regressed)

	open, err := runDebt(t, "list", "--dir", dir, "--status", "open")
	require.NoError(t, err)
	assert.Contains(t, open, base.ID, "the regressed id is open again")

	closed, err := runDebt(t, "list", "--dir", dir, "--status", "resolved")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(closed), "no matching",
		"the superseded resolution is no longer the effective record")

	dash, err := runDebt(t, "dashboard", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, dash, "**Total:** 1")
	assert.Contains(t, dash, "**Open:** 1")
	assert.Contains(t, dash, "**Resolved:** 0")
}

// A wontfix id is the exception at every consumer: it stays dismissed in the
// table and in the dashboard's counts no matter how often it is re-detected.
func TestDebtList_WontfixIDStaysDismissedAfterRedetection(t *testing.T) {
	base := debtSampleRecords()[0]
	dismissed := base
	dismissed.Timestamp = "2026-07-02T09:00:00Z"
	dismissed.Status = "wontfix"
	dismissed.Justification = "accepted pattern"
	regressed := base
	regressed.Timestamp = "2026-07-03T09:00:00Z"
	dir := writeLocalDebt(t, base, dismissed, regressed)

	open, err := runDebt(t, "list", "--dir", dir, "--status", "open")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(open), "no matching")

	dash, err := runDebt(t, "dashboard", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, dash, "**Wontfix:** 1")
	assert.Contains(t, dash, "**Open:** 0")
}
