package localdebt

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 92 T2: a store holding month shards, event files, or both folds to the same
// effective records through every read path, and compaction never deletes or
// rewrites an event file.

const idZ = "00000000000000dd"

// dualHistory is one logical history, in append order. splitAt is where the mixed
// store opts in to the event layout: records before it land in month shards,
// records from it on land in event files.
var dualHistory = []Record{
	eventRec(idP, "2026-06-14T10:00:00Z", ""),
	eventRec(idX, "2026-06-14T11:00:00Z", ""),
	eventRec(idZ, "2026-06-14T12:00:00Z", ""),
	eventRec(idZ, "2026-07-02T12:00:00Z", ""),
	eventRec(idP, "2026-07-15T10:00:00Z", StatusResolved),
	eventRec(idX, "2026-07-16T11:00:00Z", ""),
	eventRec(idY, "2026-07-17T09:00:00Z", ""),
}

const dualSplitAt = 4

type layout int

const (
	layoutShards layout = iota
	layoutEvents
	layoutMixed
)

func (l layout) String() string {
	return [...]string{"month shards", "event files", "mixed"}[l]
}

// buildStore writes dualHistory into a fresh store in the given layout.
func buildStore(t *testing.T, l layout) string {
	t.Helper()
	dir := DefaultDir(t.TempDir())
	if l == layoutEvents {
		require.NoError(t, EnableEventLayout(dir))
	}
	for i, r := range dualHistory {
		if l == layoutMixed && i == dualSplitAt {
			require.NoError(t, EnableEventLayout(dir))
		}
		require.NoError(t, Append(dir, r))
	}
	return dir
}

func sortedByID[T any](xs []T, id func(T) string) []T {
	out := append([]T(nil), xs...)
	sort.SliceStable(out, func(i, j int) bool { return id(out[i]) < id(out[j]) })
	return out
}

func foldedAll(t *testing.T, dir string) []Record {
	t.Helper()
	recs, err := ReadAll(dir, ReadOpts{})
	require.NoError(t, err)
	return sortedByID(FoldRecords(recs), func(r Record) string { return r.ID })
}

func foldedSummaries(t *testing.T, dir string) []Summary {
	t.Helper()
	sums, err := ReadSummaries(dir, ReadOpts{})
	require.NoError(t, err)
	return sortedByID(FoldSummaries(sums), func(s Summary) string { return s.ID })
}

// snapshotEvents maps each event file name to its bytes.
func snapshotEvents(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(ItemsDir(dir))
	if os.IsNotExist(err) {
		return out
	}
	require.NoError(t, err)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(ItemsDir(dir), e.Name()))
		require.NoError(t, err)
		out[e.Name()] = string(b)
	}
	return out
}

func TestDualLayout_EveryReadPathFoldsTheSame(t *testing.T) {
	want := foldedAll(t, buildStore(t, layoutShards))
	require.Len(t, want, 4, "fixture: four ids")
	wantSums := foldedSummaries(t, buildStore(t, layoutShards))

	for _, l := range []layout{layoutEvents, layoutMixed} {
		t.Run(l.String(), func(t *testing.T) {
			dir := buildStore(t, l)
			assert.Equal(t, want, foldedAll(t, dir), "ReadAll + FoldRecords")
			assert.Equal(t, wantSums, foldedSummaries(t, dir), "StreamSummaries + FoldSummaries")

			files, err := RecordFiles(dir)
			require.NoError(t, err)
			var n int
			for _, f := range files {
				recs, err := ReadRecords(f, ReadOpts{})
				require.NoError(t, err)
				n += len(recs)
			}
			assert.Equal(t, len(dualHistory), n, "RecordFiles lists every file holding a record")
		})
	}
}

func TestRecordFiles_ShardsThenEventsAndMissingStore(t *testing.T) {
	files, err := RecordFiles(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Empty(t, files, "a missing store is the no-backlog state")

	dir := buildStore(t, layoutMixed)
	// Non-record entries in both directories are skipped.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(ItemsDir(dir), ".x.json.tmp-1"), nil, 0o600))
	files, err = RecordFiles(dir)
	require.NoError(t, err)
	var rel []string
	for _, f := range files {
		r, err := filepath.Rel(dir, f)
		require.NoError(t, err)
		rel = append(rel, filepath.ToSlash(r))
	}
	require.Len(t, rel, 2+len(dualHistory)-dualSplitAt)
	assert.Equal(t, []string{"2026-06.jsonl", "2026-07.jsonl"}, rel[:2], "month shards first, in lexical order")
	for _, r := range rel[2:] {
		assert.Regexp(t, `^items/`+eventNameRe.String()[1:], r, "then event files, in name order")
	}
	assert.True(t, sort.StringsAreSorted(rel[2:]))
}

func TestCompact_EventOnlyStoreIsAnExplainedNoOp(t *testing.T) {
	for _, dry := range []bool{false, true} {
		dir := buildStore(t, layoutEvents)
		before := snapshotEvents(t, dir)
		fold := foldedAll(t, dir)

		run := Compact
		if dry {
			run = CompactPlan
		}
		res, err := run(dir, ReadOpts{})
		require.NoError(t, err)
		assert.True(t, res.StoreFound, "an event-only store is a store (dry=%v)", dry)
		assert.Equal(t, len(dualHistory), res.RecordsBefore)
		assert.Equal(t, len(dualHistory), res.RecordsAfter)
		assert.Zero(t, res.Dropped)
		assert.Equal(t, len(dualHistory), res.EventRecords, "the result says the records are event files")

		assert.Equal(t, before, snapshotEvents(t, dir), "no event file is removed or rewritten (dry=%v)", dry)
		shards, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		require.NoError(t, err)
		assert.Empty(t, shards, "compaction creates no month shard on an event-only store")
		assert.Equal(t, fold, foldedAll(t, dir))
	}
}

func TestCompact_MixedStoreCompactsMonthShardsOnly(t *testing.T) {
	for _, dry := range []bool{false, true} {
		dir := buildStore(t, layoutMixed)
		before := snapshotEvents(t, dir)
		fold := foldedAll(t, dir)

		run := Compact
		if dry {
			run = CompactPlan
		}
		res, err := run(dir, ReadOpts{})
		require.NoError(t, err)
		assert.Equal(t, len(dualHistory), res.RecordsBefore)
		assert.Equal(t, len(dualHistory)-dualSplitAt, res.EventRecords)
		assert.Equal(t, before, snapshotEvents(t, dir), "event files are never touched (dry=%v)", dry)
		assert.Equal(t, fold, foldedAll(t, dir), "the fold is unchanged (dry=%v)", dry)
		// idZ lives only in month shards and was seen twice: it is the one id the
		// shard fold can drop a superseded record of.
		assert.Equal(t, 1, res.Dropped, "only the shard-only id compacts (dry=%v)", dry)
	}

	// The real run actually rewrote the shards: idZ's superseded sighting is gone,
	// while the shard records of ids with events stay whole.
	dir := buildStore(t, layoutMixed)
	_, err := Compact(dir, ReadOpts{})
	require.NoError(t, err)
	shardOnly, err := readShardRecords(dir, ReadOpts{})
	require.NoError(t, err)
	var z, carried int
	for _, r := range shardOnly {
		switch r.ID {
		case idZ:
			z++
		case idP, idX:
			carried++
		}
	}
	assert.Equal(t, 1, z, "idZ folded to one record")
	assert.Equal(t, 2, carried, "ids with event files keep their shard records unchanged")
}

func TestStoreStats_CountsMonthShardsOnly(t *testing.T) {
	// The automatic trigger measures what compaction can shrink. Event files are
	// never compacted, so counting them would re-trip it forever on no-op runs.
	n, _, err := StoreStats(buildStore(t, layoutEvents))
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestBackfillJustifications_LeavesEventFilesAlone(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "debt")
	require.NoError(t, EnableEventLayout(store))
	reviewRoot := filepath.Join(root, "reviews")
	rd := filepath.Join(reviewRoot, "sprint-a", "multi-agent", "sources", "pool", "raw", "agent", "dax")
	require.NoError(t, os.MkdirAll(rd, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(rd, "review.md"), []byte(danglingReview), 0o600))

	stale := `{"schema_version":3,"id":"aaaa1111","run_id":"2026-08-01T00:00:00Z-multi-agent","ts":"2026-08-01T00:00:00Z",` +
		`"severity":"HIGH","file":"internal/thing.go","line":42,"problem":"p","fix":"f","category":"correctness",` +
		`"est_minutes":10,"evidence":"e","reviewers":["dax"],"confidence":"HIGH",` +
		`"justification":"- **internal/thing.go:42** the real narrative explaining the defect.",` +
		`"source_report":{"path":"sources/pool/raw/agent/dax/review.md","line":8}}`
	evPath := filepath.Join(ItemsDir(store), "aaaa1111.20260801T000000.000000000Z-000000000000.json")
	require.NoError(t, os.WriteFile(evPath, []byte(stale+"\n"), 0o600))

	for _, dry := range []bool{true, false} {
		res, err := BackfillJustifications(store, reviewRoot, dry)
		require.NoError(t, err)
		assert.Equal(t, 1, res.SkippedEventFiles, "the event-held id is read, then skipped (dry=%v)", dry)
		assert.Zero(t, res.Scanned)
		assert.Zero(t, res.Rewritten, "never Rewritten > 0 with RewrittenLines == 0")
		assert.Zero(t, res.RewrittenLines)
		b, err := os.ReadFile(evPath)
		require.NoError(t, err)
		assert.Equal(t, stale+"\n", string(b), "an event file is written once and never rewritten")
	}
}

// An unreadable items directory or event file fails every read path, like an
// unreadable shard, and the error never carries the absolute store path.
func TestDualLayout_UnreadableEventsFailEveryReadPathRedacted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied read cannot be provoked as root")
	}
	reads := map[string]func(dir string) error{
		"ReadAll": func(dir string) error { _, err := ReadAll(dir, ReadOpts{}); return err },
		"ReadSummaries": func(dir string) error {
			_, err := ReadSummaries(dir, ReadOpts{})
			return err
		},
		"ReadEventRecords": func(dir string) error { _, err := ReadEventRecords(dir, ReadOpts{}); return err },
		"CompactPlan":      func(dir string) error { _, err := CompactPlan(dir, ReadOpts{}); return err },
		"Compact":          func(dir string) error { _, err := Compact(dir, ReadOpts{}); return err },
		"Backfill": func(dir string) error {
			_, err := BackfillJustifications(dir, t.TempDir(), true)
			return err
		},
	}
	lock := map[string]func(t *testing.T, dir string) string{
		"items dir": func(t *testing.T, dir string) string { return ItemsDir(dir) },
		"event file": func(t *testing.T, dir string) string {
			return filepath.Join(ItemsDir(dir), eventFiles(t, dir)[0])
		},
	}
	for what, target := range lock {
		for name, read := range reads {
			t.Run(what+"/"+name, func(t *testing.T) {
				dir := buildStore(t, layoutMixed)
				p := target(t, dir)
				require.NoError(t, os.Chmod(p, 0o000))
				t.Cleanup(func() { _ = os.Chmod(p, 0o700) })
				err := read(dir)
				require.Error(t, err)
				assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
			})
		}
	}
}
