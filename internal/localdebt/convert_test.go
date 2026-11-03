package localdebt

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 92 T3: Convert moves month shards to the event layout, one way, and no
// record is lost: the store folds to the same effective records before and after.

// snapshotTree maps every file under dir (slash-separated relative path) to its
// bytes, so a test can assert a run left the store exactly as it was.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	require.NoError(t, err)
	return out
}

func shardNames(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := shardFilePaths(dir)
	require.NoError(t, err)
	var names []string
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	return names
}

var quiet = ReadOpts{Writer: io.Discard}

func TestConvert_FoldIsUnchangedAndShardsAreGone(t *testing.T) {
	for _, l := range []layout{layoutShards, layoutMixed} {
		t.Run(l.String(), func(t *testing.T) {
			dir := buildStore(t, l)
			before := foldedAll(t, dir)
			shardRecs, err := readShardRecords(dir, quiet)
			require.NoError(t, err)

			res, err := Convert(dir, quiet)
			require.NoError(t, err)

			assert.Equal(t, before, foldedAll(t, dir), "no record lost: the fold is the same before and after")
			assert.Empty(t, shardNames(t, dir), "every month shard was converted and removed")
			assert.True(t, UsesEventLayout(dir), "a converted store writes event files from now on")
			assert.True(t, res.StoreFound)
			assert.Equal(t, len(shardRecs), res.Records)
			assert.Len(t, res.Shards, 2, "2026-06 and 2026-07")
			assert.Empty(t, res.Kept)
			assert.Equal(t, len(before), res.Items)

			events, err := ReadEventRecords(dir, quiet)
			require.NoError(t, err)
			assert.Len(t, events, len(dualHistory), "every record of the history is an event file")
			_, err = os.Stat(filepath.Join(dir, convertSubdir))
			assert.True(t, os.IsNotExist(err), "the staging directory is removed")
		})
	}
}

func TestConvert_EventFilesAreTheShardLinesByteForByte(t *testing.T) {
	dir := buildStore(t, layoutShards)
	var lines []string
	for _, name := range shardNames(t, dir) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			lines = append(lines, l+"\n")
		}
	}

	_, err := Convert(dir, quiet)
	require.NoError(t, err)

	var got []string
	for _, body := range snapshotEvents(t, dir) {
		if body != "" { // the empty marker
			got = append(got, body)
		}
	}
	assert.ElementsMatch(t, lines, got, "an event holds the shard line it came from, so no field is dropped")
}

func TestConvert_AppendAfterConvertWritesAnEvent(t *testing.T) {
	dir := buildStore(t, layoutShards)
	_, err := Convert(dir, quiet)
	require.NoError(t, err)
	require.NoError(t, Append(dir, eventRec(idY, "2026-08-01T00:00:00Z", StatusResolved)))
	assert.Empty(t, shardNames(t, dir), "the store stays in the event layout")
}

func TestConvert_IsIdempotentOnAnEventStore(t *testing.T) {
	for _, l := range []layout{layoutShards, layoutEvents} {
		t.Run(l.String(), func(t *testing.T) {
			dir := buildStore(t, l)
			_, err := Convert(dir, quiet)
			require.NoError(t, err)
			snap := snapshotTree(t, dir)

			res, err := Convert(dir, quiet)
			require.NoError(t, err)
			assert.True(t, res.StoreFound)
			assert.Zero(t, res.Records)
			assert.Empty(t, res.Shards)
			assert.Equal(t, 4, res.Items)
			assert.Equal(t, snap, snapshotTree(t, dir), "a second run changes nothing")
		})
	}
}

func TestConvert_NoStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	res, err := Convert(dir, quiet)
	require.NoError(t, err)
	assert.False(t, res.StoreFound)
	assert.False(t, UsesEventLayout(dir), "converting nothing does not opt a store in")

	res, err = ConvertPlan(dir, quiet)
	require.NoError(t, err)
	assert.False(t, res.StoreFound)
}

func TestConvertPlan_WritesNothingAndReportsTheRealRun(t *testing.T) {
	dir := buildStore(t, layoutMixed)
	snap := snapshotTree(t, dir)

	plan, err := ConvertPlan(dir, quiet)
	require.NoError(t, err)
	assert.Equal(t, snap, snapshotTree(t, dir), "a dry run writes nothing")

	res, err := Convert(dir, quiet)
	require.NoError(t, err)
	assert.Equal(t, res, plan, "the dry run reports what the real run does")
}

// TestConvert_KeepsShardsItCannotCarry: a shard holding a line this binary cannot
// turn into an event (corrupt, written by a newer atcr, or a record whose id or
// timestamp cannot name an event file) is left whole, and so is a .jsonl file that
// is not a month shard. Everything else converts and the fold is unchanged.
func TestConvert_KeepsShardsItCannotCarry(t *testing.T) {
	dir := buildStore(t, layoutShards)
	extra := map[string]string{
		"2025-01.jsonl": "{not json\n" + mustLine(t, eventRec(idY, "2025-01-03T00:00:00Z", "")),
		"2025-02.jsonl": `{"schema_version":99,"id":"00000000000000ee","run_id":"2025-02-01T00:00:00Z-r","ts":"2025-02-01T00:00:00Z"}` + "\n",
		"2025-03.jsonl": mustLine(t, eventRec("legacy-id", "2025-03-01T00:00:00Z", "")),
		"2025-04.jsonl": mustLine(t, Record{SchemaVersion: SchemaVersion, ID: idX, RunID: "2025-04-01T00:00:00Z-r", Timestamp: "yesterday"}),
		"notes.jsonl":   mustLine(t, eventRec(idY, "2025-05-01T00:00:00Z", "")),
	}
	for name, body := range extra {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	before := foldedAll(t, dir)

	res, err := Convert(dir, quiet)
	require.NoError(t, err)

	assert.Equal(t, before, foldedAll(t, dir), "the fold is unchanged")
	assert.ElementsMatch(t, []string{"2026-06.jsonl", "2026-07.jsonl"}, res.Shards)
	var kept []string
	for _, k := range res.Kept {
		kept = append(kept, k.Name)
		assert.NotEmpty(t, k.Reason, k.Name)
		b, err := os.ReadFile(filepath.Join(dir, k.Name))
		require.NoError(t, err)
		assert.Equal(t, extra[k.Name], string(b), "a kept file is left byte for byte")
	}
	assert.ElementsMatch(t, []string{"2025-01.jsonl", "2025-02.jsonl", "2025-03.jsonl", "2025-04.jsonl", "notes.jsonl"}, kept)
}

func mustLine(t *testing.T, r Record) string {
	t.Helper()
	b, err := json.Marshal(r)
	require.NoError(t, err)
	return string(b) + "\n"
}

// TestConvert_RefusesWhenTheFoldWouldChange: two records of one id with the same
// timestamp and status are a full tie, which the month shard breaks by line order
// and the event layout by file name. When the two disagree the fold would change,
// so the run refuses and the store is left exactly as it was.
func TestConvert_RefusesWhenTheFoldWouldChange(t *testing.T) {
	a := eventRec(idP, "2026-06-14T10:00:00Z", "")
	a.Model = "model-a"
	b := a
	b.Model = "model-b"
	nameOf := func(r Record) string {
		line := []byte(mustLine(t, r))
		n, err := eventFileName(r, line)
		require.NoError(t, err)
		return n
	}
	// The shard's last line wins the tie; make it the one whose event name sorts
	// first, so the event layout would pick the other.
	first, last := a, b
	if nameOf(b) > nameOf(a) {
		first, last = b, a
	}
	dir := DefaultDir(t.TempDir())
	require.NoError(t, Append(dir, first))
	require.NoError(t, Append(dir, last))
	require.Equal(t, last.Model, foldedAll(t, dir)[0].Model, "fixture: the shard's last line wins")
	snap := snapshotTree(t, dir)

	_, err := ConvertPlan(dir, quiet)
	require.ErrorIs(t, err, ErrFoldChanged)
	assert.Contains(t, err.Error(), idP, "the error names the item that would change")

	_, err = Convert(dir, quiet)
	require.ErrorIs(t, err, ErrFoldChanged)
	assert.Equal(t, snap, snapshotTree(t, dir), "a refused conversion leaves the store as it was")
}

// TestConvert_FinishesAnInterruptedConversion: a run that dies after the plan was
// verified and committed is finished by the next run, so a crash between
// publishing the events and removing the shards does not leave both copies.
func TestConvert_FinishesAnInterruptedConversion(t *testing.T) {
	dir := buildStore(t, layoutShards)
	before := foldedAll(t, dir)

	crash := errors.New("simulated crash")
	convertAfterCommit = func() error { return crash }
	_, err := Convert(dir, quiet)
	convertAfterCommit = nil
	require.ErrorIs(t, err, crash)
	require.NotEmpty(t, shardNames(t, dir), "fixture: the crash left the shards in place")
	_, err = os.Stat(filepath.Join(dir, convertSubdir, convertJournal))
	require.NoError(t, err, "fixture: the committed plan is on disk")

	_, err = ConvertPlan(dir, quiet)
	require.Error(t, err, "a dry run does not finish someone else's conversion")
	assert.Contains(t, err.Error(), "interrupted")

	res, err := Convert(dir, quiet)
	require.NoError(t, err)
	assert.True(t, res.Resumed)
	assert.Empty(t, shardNames(t, dir))
	assert.Equal(t, before, foldedAll(t, dir), "no record lost and none duplicated")
	_, err = os.Stat(filepath.Join(dir, convertSubdir))
	assert.True(t, os.IsNotExist(err))
}

// TestConvert_DiscardsAnUncommittedStaging: a run that dies before its plan was
// committed left only unverified staging, which the next run throws away.
func TestConvert_DiscardsAnUncommittedStaging(t *testing.T) {
	dir := buildStore(t, layoutShards)
	before := foldedAll(t, dir)
	staging := filepath.Join(dir, convertSubdir)
	require.NoError(t, os.MkdirAll(staging, 0o700))
	stray := mustLine(t, eventRec(idY, "2026-09-01T00:00:00Z", StatusResolved))
	require.NoError(t, os.WriteFile(filepath.Join(staging, idY+".20260901T000000.000000000Z-000000000000.json"), []byte(stray), 0o600))

	res, err := Convert(dir, quiet)
	require.NoError(t, err)
	assert.False(t, res.Resumed)
	assert.Equal(t, before, foldedAll(t, dir), "the uncommitted staged event is not published")
}

func TestFoldDiff_NamesEveryChangedMissingAndNewID(t *testing.T) {
	before := []Record{eventRec(idP, "2026-06-01T00:00:00Z", ""), eventRec(idX, "2026-06-01T00:00:00Z", "")}
	changed := eventRec(idP, "2026-06-01T00:00:00Z", "")
	changed.Model = "other"
	after := []Record{changed, eventRec(idY, "2026-06-01T00:00:00Z", "")}
	assert.Equal(t, []string{idP, idX, idY}, foldDiff(before, after))
	assert.Empty(t, foldDiff(before, before))

	ids := []string{"1", "2", "3", "4", "5", "6", "7"}
	err := foldChangedError(ids)
	require.ErrorIs(t, err, ErrFoldChanged)
	assert.Contains(t, err.Error(), "for 7 item(s): 1, 2, 3, 4, 5 and 2 more; nothing was changed")
}
