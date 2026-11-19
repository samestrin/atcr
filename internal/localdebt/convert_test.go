package localdebt

import (
	"encoding/json"
	"errors"
	"fmt"
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
	for name, body := range snapshotEvents(t, dir) {
		// Dot-prefixed names are not events (IsEventEntry): the empty layout
		// marker and items/.gitignore, which Convert now ships. Every actual
		// event body — including a hypothetical empty one — is still compared.
		if strings.HasPrefix(name, ".") {
			continue
		}
		got = append(got, body)
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

// fullTie appends two open records of idP that tie on id, timestamp and status and
// differ in model and justification, ordered so the event layout would pick the
// other one: the store cannot convert as it is.
func fullTie(t *testing.T) string {
	t.Helper()
	a := eventRec(idP, "2026-06-14T10:00:00Z", "")
	a.Model, a.Justification = "model-a", "reason a"
	b := a
	b.Model, b.Justification = "model-b", "reason b"
	nameOf := func(r Record) string {
		n, err := eventFileName(r, []byte(mustLine(t, r)))
		require.NoError(t, err)
		return n
	}
	first, last := a, b
	if nameOf(b) > nameOf(a) {
		first, last = b, a
	}
	dir := DefaultDir(t.TempDir())
	require.NoError(t, Append(dir, first))
	require.NoError(t, Append(dir, last))
	return dir
}

// TestConvert_CompactClearsAFullTie (TD row 3679acc34b7a08d8): the refusal names
// the way out, and `debt compact` folds a tied item to one record, after which the
// store converts with the fold unchanged.
func TestConvert_CompactClearsAFullTie(t *testing.T) {
	dir := fullTie(t)
	before := foldedAll(t, dir)

	_, err := Convert(dir, quiet)
	require.ErrorIs(t, err, ErrFoldChanged)
	assert.Contains(t, err.Error(), "run `atcr debt compact`, then convert again")

	_, err = Compact(dir, quiet)
	require.NoError(t, err)
	res, err := Convert(dir, quiet)
	require.NoError(t, err, "compaction cleared the tie")
	assert.Equal(t, []string{"2026-06.jsonl"}, res.Shards)
	assert.Equal(t, before, foldedAll(t, dir), "the fold is the one the store had before compact and convert")
}

// TestConvert_CompactKeepsTiedResolutionsWithDifferentReasons: compaction keeps
// every resolution reason, so such a tie survives it, and whether the store then
// converts depends on how the rewritten lines hash. When it does not, the refusal
// says compaction will not help rather than sending the user round the same loop.
func TestConvert_CompactKeepsTiedResolutionsWithDifferentReasons(t *testing.T) {
	refused := false
	for i := 0; i < 8 && !refused; i++ {
		a := eventRec(idP, "2026-06-14T10:00:00Z", StatusResolved)
		a.Model, a.Justification = "model-a", fmt.Sprintf("reason a%d", i)
		b := a
		b.Model, b.Justification = "model-b", fmt.Sprintf("reason b%d", i)
		for _, order := range [][2]Record{{a, b}, {b, a}} {
			dir := DefaultDir(t.TempDir())
			require.NoError(t, Append(dir, order[0]))
			require.NoError(t, Append(dir, order[1]))
			_, err := Compact(dir, quiet)
			require.NoError(t, err)
			recs, err := readShardRecords(dir, quiet)
			require.NoError(t, err)
			assert.Len(t, recs, 2, "compaction keeps both reasons, so the tie survives")

			if _, err = ConvertPlan(dir, quiet); err != nil {
				refused = true
				require.ErrorIs(t, err, ErrFoldChanged)
				assert.Contains(t, err.Error(), "resolutions with different reasons survive compaction")
			}
		}
	}
	require.True(t, refused, "fixture: some tie still refuses after compaction")
}

// isolateCache points the user cache dir and the OS temp dir of this test at
// fresh directories and returns them.
func isolateCache(t *testing.T) (cache, tmp string) {
	t.Helper()
	home, tmp := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "cache"))
	t.Setenv("TMPDIR", tmp)
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	return cache, tmp
}

// TestConvertPlan_StagesUnderTheUserCacheDir (TD row dc8c372ee0c8ae31): a dry run
// stages its copy of the records in a 0700 directory of the user cache dir, never
// the shared OS temp dir, and removes it when done.
func TestConvertPlan_StagesUnderTheUserCacheDir(t *testing.T) {
	cache, tmp := isolateCache(t)
	dir := buildStore(t, layoutShards)

	var staged string
	convertAfterStage = func(stageDir string) {
		staged = stageDir
		info, err := os.Stat(stageDir)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "the staged records are private to the user")
		entries, err := os.ReadDir(stageDir)
		require.NoError(t, err)
		assert.NotEmpty(t, entries, "fixture: the dry run staged events")
	}
	defer func() { convertAfterStage = nil }()

	_, err := ConvertPlan(dir, quiet)
	require.NoError(t, err)
	require.NotEmpty(t, staged, "the dry run staged its events")
	rel, err := filepath.Rel(cache, staged)
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(rel, ".."), "staged under the user cache dir, got %s", staged)
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing staged in the OS temp dir")
	_, err = os.Stat(staged)
	assert.True(t, os.IsNotExist(err), "the staging is removed when the dry run ends")
}

// TestConvert_RemovesTheCopyOfACrashedDryRun: a dry run that dies leaves its staged
// copy behind (no deferred cleanup runs); the next run on that store, dry or real,
// removes it.
func TestConvert_RemovesTheCopyOfACrashedDryRun(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry-run", false: "real"}[dryRun], func(t *testing.T) {
			isolateCache(t)
			dir := buildStore(t, layoutShards)
			stale, err := dryRunStaging(dir)
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(stale, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(stale, idY+".20260901T000000.000000000Z-000000000000.json"),
				[]byte(mustLine(t, eventRec(idY, "2026-09-01T00:00:00Z", ""))), 0o600))

			run := Convert
			if dryRun {
				run = ConvertPlan
			}
			_, err = run(dir, quiet)
			require.NoError(t, err)
			_, err = os.Stat(stale)
			assert.True(t, os.IsNotExist(err), "the stale copy is removed")
		})
	}
}

// TestConvert_ReportsTheOptIn (TD row 8f8e6986e9550a96): a run that converts no
// shard still opts the store in to the event layout, and says so; a store already
// opted in is not reported again. The dry run reports what the real run would do.
func TestConvert_ReportsTheOptIn(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.jsonl"), []byte(mustLine(t, eventRec(idY, "2025-05-01T00:00:00Z", ""))), 0o600))

	plan, err := ConvertPlan(dir, quiet)
	require.NoError(t, err)
	assert.True(t, plan.OptedIn, "the dry run reports the opt-in the real run makes")
	assert.False(t, UsesEventLayout(dir), "the dry run does not opt in")

	res, err := Convert(dir, quiet)
	require.NoError(t, err)
	assert.Empty(t, res.Shards, "fixture: nothing converts")
	assert.True(t, res.OptedIn)
	assert.True(t, UsesEventLayout(dir))
	assert.Equal(t, res, plan)

	res, err = Convert(dir, quiet)
	require.NoError(t, err)
	assert.False(t, res.OptedIn, "an opted-in store is not opted in again")
}

// TestConvert_WithNoUserCacheDir: a dry run needs the user cache dir to stage in
// and says so when there is none; a real run stages in the store and does not.
func TestConvert_WithNoUserCacheDir(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LocalAppData", "")
	if _, err := os.UserCacheDir(); err == nil {
		t.Skip("this platform names a user cache dir without HOME")
	}
	dir := buildStore(t, layoutShards)

	_, err := ConvertPlan(dir, quiet)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging localdebt conversion")

	_, err = Convert(dir, quiet)
	require.NoError(t, err)
	assert.Empty(t, shardNames(t, dir))
}

// halfFinishedConversion leaves the store as a run that died inside finishConversion
// does: the plan committed, its staged events published into items/, and the month
// shards it names not yet removed. Every record is then on disk twice.
func halfFinishedConversion(t *testing.T) (dir string, before []Record) {
	t.Helper()
	dir = buildStore(t, layoutShards)
	before = foldedAll(t, dir)

	crash := errors.New("simulated crash")
	convertAfterCommit = func() error { return crash }
	_, err := Convert(dir, quiet)
	convertAfterCommit = nil
	require.ErrorIs(t, err, crash)
	staging := filepath.Join(dir, convertSubdir)
	staged, err := os.ReadDir(staging)
	require.NoError(t, err)
	for _, e := range staged {
		if IsEventEntry(e) {
			require.NoError(t, os.Rename(filepath.Join(staging, e.Name()), filepath.Join(ItemsDir(dir), e.Name())))
		}
	}
	require.NotEmpty(t, shardNames(t, dir), "fixture: the shards are still in place")
	_, err = os.Stat(filepath.Join(staging, convertJournal))
	require.NoError(t, err, "fixture: the committed plan is on disk")
	return dir, before
}

func requireConversionFinished(t *testing.T, dir string) {
	t.Helper()
	assert.Empty(t, shardNames(t, dir), "the committed plan was finished: its shards are gone")
	_, err := os.Stat(filepath.Join(dir, convertSubdir))
	assert.True(t, os.IsNotExist(err), "and so is its staging")
}

// TestReadPaths_FinishACommittedConversion (TD row 53b3fcda9fbe0a08): a conversion
// that committed and then died is finished by the next read, not left for the next
// `atcr debt convert`; until then every record is read twice and an item's
// Occurrences reads high.
func TestReadPaths_FinishACommittedConversion(t *testing.T) {
	t.Run("ReadAll", func(t *testing.T) {
		dir, before := halfFinishedConversion(t)
		assert.Equal(t, before, foldedAll(t, dir), "no record is counted twice")
		requireConversionFinished(t, dir)
	})
	t.Run("ReadSummaries", func(t *testing.T) {
		dir, before := halfFinishedConversion(t)
		got := foldedSummaries(t, dir)
		require.Len(t, got, len(before))
		for i, s := range got {
			assert.Equal(t, before[i].ID, s.ID)
			assert.Equal(t, before[i].Status, s.Status)
		}
		requireConversionFinished(t, dir)
	})
	t.Run("Compact", func(t *testing.T) {
		dir, before := halfFinishedConversion(t)
		res, err := Compact(dir, quiet)
		require.NoError(t, err)
		assert.Equal(t, len(dualHistory), res.RecordsBefore, "compaction read each record once")
		requireConversionFinished(t, dir)
		assert.Equal(t, before, foldedAll(t, dir))
	})
	t.Run("CompactPlan", func(t *testing.T) {
		dir, _ := halfFinishedConversion(t)
		snap := snapshotTree(t, dir)
		_, err := CompactPlan(dir, quiet)
		require.Error(t, err, "a dry run neither finishes the conversion nor counts its records twice")
		assert.Contains(t, err.Error(), "interrupted")
		assert.Equal(t, snap, snapshotTree(t, dir), "a dry run writes nothing")
	})
	t.Run("BackfillJustifications", func(t *testing.T) {
		dir, before := halfFinishedConversion(t)
		snap := snapshotTree(t, dir)
		_, err := BackfillJustifications(dir, t.TempDir(), true)
		require.Error(t, err, "a dry run does not finish the conversion")
		assert.Contains(t, err.Error(), "interrupted")
		assert.Equal(t, snap, snapshotTree(t, dir), "a dry run writes nothing")

		_, err = BackfillJustifications(dir, t.TempDir(), false)
		require.NoError(t, err)
		requireConversionFinished(t, dir)
		assert.Equal(t, before, foldedAll(t, dir))
	})
}

// TestReadAll_WarnsWhenACommittedConversionCannotBeFinished: a read that cannot
// finish the plan still reads, and says why its counts may be high.
func TestReadAll_WarnsWhenACommittedConversionCannotBeFinished(t *testing.T) {
	dir, _ := halfFinishedConversion(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, convertSubdir, convertJournal), []byte("{not json"), 0o600))

	var diag strings.Builder
	recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
	require.NoError(t, err)
	assert.Len(t, recs, 2*len(dualHistory), "the store is read as it stands")
	assert.Contains(t, diag.String(), "interrupted conversion")
	assert.Contains(t, diag.String(), "atcr debt convert")
	assert.NotContains(t, diag.String(), dir, "the warning carries no absolute path")
}

// committedConversionWithoutStaging leaves the store as one whose COMMIT arrived
// without its staged events (a partial `git add`, a merge that kept COMMIT, a synced
// shared store): the plan is committed, nothing is staged, and the month shards it
// names are the only copy of their records.
func committedConversionWithoutStaging(t *testing.T) (dir string, before []Record) {
	t.Helper()
	dir = buildStore(t, layoutShards)
	before = foldedAll(t, dir)

	crash := errors.New("simulated crash")
	convertAfterCommit = func() error { return crash }
	_, err := Convert(dir, quiet)
	convertAfterCommit = nil
	require.ErrorIs(t, err, crash)
	staging := filepath.Join(dir, convertSubdir)
	staged, err := os.ReadDir(staging)
	require.NoError(t, err)
	for _, e := range staged {
		if IsEventEntry(e) {
			require.NoError(t, os.Remove(filepath.Join(staging, e.Name())))
		}
	}
	_, err = os.Stat(filepath.Join(staging, convertJournal))
	require.NoError(t, err, "fixture: the committed plan is on disk")
	require.NotEmpty(t, shardNames(t, dir), "fixture: the shards are still in place")
	return dir, before
}

// TestFinishConversion_KeepsAShardWhoseEventsAreAbsent (epic 122 F1): a committed
// plan whose staged events are neither staged nor in items/ removes no shard, and
// the error names the missing events. Before, the next read removed every shard
// the journal named and lost their records.
func TestFinishConversion_KeepsAShardWhoseEventsAreAbsent(t *testing.T) {
	t.Run("finishConversion", func(t *testing.T) {
		dir, before := committedConversionWithoutStaging(t)
		shards := shardNames(t, dir)
		plan, err := planConversion(dir)
		require.NoError(t, err)

		err = finishConversion(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), plan.events[0].name, "the error names a missing event")
		assert.Contains(t, err.Error(), shards[0], "and the shard it keeps")
		assert.Contains(t, err.Error(), convertSubdir, "and how to abandon the conversion")
		assert.NotContains(t, err.Error(), dir, "the error carries no absolute path")
		assert.Equal(t, shards, shardNames(t, dir), "no shard is removed")
		assert.Equal(t, before, foldedAll(t, dir), "no record is lost")
	})
	t.Run("ReadAll", func(t *testing.T) {
		dir, before := committedConversionWithoutStaging(t)
		shards := shardNames(t, dir)
		var diag strings.Builder
		recs, err := ReadAll(dir, ReadOpts{Writer: &diag})
		require.NoError(t, err)
		assert.Equal(t, before, sortedByID(FoldRecords(recs), func(r Record) string { return r.ID }), "no record is lost")
		assert.Contains(t, diag.String(), "interrupted conversion")
		assert.Equal(t, shards, shardNames(t, dir), "no shard is removed")
	})
	t.Run("Convert", func(t *testing.T) {
		dir, before := committedConversionWithoutStaging(t)
		shards := shardNames(t, dir)
		_, err := Convert(dir, quiet)
		require.Error(t, err)
		assert.Equal(t, shards, shardNames(t, dir), "no shard is removed")
		assert.Equal(t, before, foldedAll(t, dir), "no record is lost")
	})
}

// TestFinishConversion_KeepsAShardChangedSinceTheCommit: a shard that gained a line
// after the plan was committed holds a record no staged event carries, so it is kept.
func TestFinishConversion_KeepsAShardChangedSinceTheCommit(t *testing.T) {
	dir := buildStore(t, layoutShards)
	crash := errors.New("simulated crash")
	convertAfterCommit = func() error { return crash }
	_, err := Convert(dir, quiet)
	convertAfterCommit = nil
	require.ErrorIs(t, err, crash)

	shards := shardNames(t, dir)
	late := mustLine(t, eventRec(idY, "2026-07-30T09:00:00Z", StatusResolved))
	f, err := os.OpenFile(filepath.Join(dir, shards[len(shards)-1]), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(late)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	err = finishConversion(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), shards[len(shards)-1])
	assert.Contains(t, shardNames(t, dir), shards[len(shards)-1], "the changed shard is kept")
}

// TestFinishConversion_EventsSplitBetweenStagingAndItems: a run that died midway
// through publishing left some events staged and the rest in items/; the next run
// finishes the plan.
func TestFinishConversion_EventsSplitBetweenStagingAndItems(t *testing.T) {
	dir := buildStore(t, layoutShards)
	before := foldedAll(t, dir)
	crash := errors.New("simulated crash")
	convertAfterCommit = func() error { return crash }
	_, err := Convert(dir, quiet)
	convertAfterCommit = nil
	require.ErrorIs(t, err, crash)
	staging := filepath.Join(dir, convertSubdir)
	staged, err := os.ReadDir(staging)
	require.NoError(t, err)
	moved := 0
	for i, e := range staged {
		if IsEventEntry(e) && i%2 == 0 {
			require.NoError(t, os.Rename(filepath.Join(staging, e.Name()), filepath.Join(ItemsDir(dir), e.Name())))
			moved++
		}
	}
	require.NotZero(t, moved, "fixture: some events were published")

	require.NoError(t, finishConversion(dir))
	requireConversionFinished(t, dir)
	assert.Equal(t, before, foldedAll(t, dir))
}

// TestFinishConversion_AShardAlreadyRemoved: a run that died after removing some of
// the named shards left nothing of them to check; the next run removes the rest.
func TestFinishConversion_AShardAlreadyRemoved(t *testing.T) {
	dir, before := halfFinishedConversion(t)
	shards := shardNames(t, dir)
	require.Len(t, shards, 2, "fixture: two month shards")
	require.NoError(t, os.Remove(filepath.Join(dir, shards[0])))

	require.NoError(t, finishConversion(dir))
	requireConversionFinished(t, dir)
	assert.Equal(t, before, foldedAll(t, dir))
}
