package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

// Epic 92 T3: `atcr debt convert` moves month shards to the event layout.

func convertFixture(t *testing.T) string {
	t.Helper()
	a := openRec("2026-06-01T10:00:00Z-a", "HIGH", "internal/x/a.go", 12, "boom")
	b := openRec("2026-07-02T10:00:00Z-b", "LOW", "internal/y/b.go", 3, "leak")
	return writeEventStore(t, 99, a, b) // never reaches 99: month shards only
}

func foldOf(t *testing.T, dir string) []localdebt.Record {
	t.Helper()
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{Writer: io.Discard})
	require.NoError(t, err)
	return localdebt.FoldRecords(recs)
}

func TestDebtConvert_ConvertsAndReports(t *testing.T) {
	dir := convertFixture(t)
	before := foldOf(t, dir)

	out, err := runDebt(t, "convert", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Converted 2 record(s) from 2 month shard(s) to event files (items/); 2 item(s) fold the same before and after.")
	assert.ElementsMatch(t, before, foldOf(t, dir))
	assert.True(t, localdebt.UsesEventLayout(dir))

	out, err = runDebt(t, "convert", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing to convert: the store already uses the event layout (2 item(s)).")
}

func TestDebtConvert_DryRunWritesNothing(t *testing.T) {
	dir := convertFixture(t)
	out, err := runDebt(t, "convert", "--store", dir, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Would convert 2 record(s) from 2 month shard(s)")
	assert.Contains(t, out, "Dry run: the store was not modified.")
	assert.False(t, localdebt.UsesEventLayout(dir))
}

func TestDebtConvert_ReportsKeptFiles(t *testing.T) {
	dir := convertFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.jsonl"), []byte("{not json\n"), 0o600))

	out, err := runDebt(t, "convert", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Left 1 file(s) in place; their records are still read:")
	assert.Contains(t, out, "notes.jsonl: not a month shard")
}

func TestDebtConvert_NoStore(t *testing.T) {
	out, err := runDebt(t, "convert", "--store", filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Contains(t, out, "No local TD store to convert.")
}

func TestDebtConvert_ErrorIsWrapped(t *testing.T) {
	// A pending journal naming something that is not a month shard is refused.
	dir := convertFixture(t)
	blocker := filepath.Join(filepath.Dir(dir), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".convert"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".convert", "COMMIT"), []byte(`["../blocker"]`), 0o600))

	_, err := runDebt(t, "convert", "--store", dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "convert:")
	assert.Contains(t, err.Error(), "not a month shard")
	_, statErr := os.Stat(blocker)
	assert.NoError(t, statErr, "a journal never removes a path outside the store")
}

// TestDebtConvert_SaysTheStoreIsOptedIn (TD row 8f8e6986e9550a96): when every
// month shard is kept, the run still opts the store in to the event layout, and
// the output says so rather than only "Nothing to convert".
func TestDebtConvert_SaysTheStoreIsOptedIn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-06.jsonl"), []byte("{not json\n"), 0o600))

	out, err := runDebt(t, "convert", "--store", dir, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing to convert: no month shard can be carried into event files.")
	assert.Contains(t, out, "A real run opts the store in to the event layout: every later add and resolution writes an event file (items/).")

	out, err = runDebt(t, "convert", "--store", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "The store is now opted in to the event layout: every later add and resolution writes an event file (items/).")
	assert.True(t, localdebt.UsesEventLayout(dir))

	out, err = runDebt(t, "convert", "--store", dir)
	require.NoError(t, err)
	assert.NotContains(t, out, "opted in", "an opted-in store is not reported again")
}

// TestDebtConvert_SaysTheStoreIsOptedInWhenItFails (TD row dc2a27d4d9d88e64): a
// conversion that opted the store in and then failed returns that with its error,
// and the command says so before the error, since every later write is now an
// event file. A dry run, or a run that opted nothing in, says nothing of it.
func TestDebtConvert_SaysTheStoreIsOptedInWhenItFails(t *testing.T) {
	boom := errors.New("simulated failure")
	stub := func(res localdebt.ConvertResult) {
		f := func(string, localdebt.ReadOpts) (localdebt.ConvertResult, error) { return res, boom }
		origConvert, origPlan := debtConvert, debtConvertPlan
		debtConvert, debtConvertPlan = f, f
		t.Cleanup(func() { debtConvert, debtConvertPlan = origConvert, origPlan })
	}
	dir := convertFixture(t)

	stub(localdebt.ConvertResult{StoreFound: true, OptedIn: true, Resumed: true})
	out, err := runDebt(t, "convert", "--store", dir)
	require.ErrorIs(t, err, boom)
	assert.Contains(t, out, "The store is now opted in to the event layout: every later add and resolution writes an event file (items/).")
	assert.Contains(t, out, "Finished an interrupted conversion.")

	out, err = runDebt(t, "convert", "--store", dir, "--dry-run")
	require.ErrorIs(t, err, boom)
	assert.NotContains(t, out, "opted in", "a failed dry run opted nothing in")

	stub(localdebt.ConvertResult{StoreFound: true})
	out, err = runDebt(t, "convert", "--store", dir)
	require.ErrorIs(t, err, boom)
	assert.NotContains(t, out, "opted in", "a run that opted nothing in says nothing of it")
}
