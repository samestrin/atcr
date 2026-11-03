package localdebt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/gitexec"
)

// eventRec builds a minimal record that passes the write gate, with the given
// finding id, timestamp and status.
func eventRec(id, ts, status string) Record {
	return Record{
		SchemaVersion: SchemaVersion,
		ID:            id,
		RunID:         ts + "-manual",
		Timestamp:     ts,
		Severity:      "medium",
		File:          "a.go",
		Line:          1,
		Problem:       "problem " + id,
		Status:        status,
	}
}

const (
	idP = "00000000000000aa"
	idX = "00000000000000bb"
	idY = "00000000000000cc"
)

// eventNameRe is the documented event file name:
// <finding id>.<event ts>-<short hash>.json.
var eventNameRe = regexp.MustCompile(`^[0-9a-f]{16}\.\d{8}T\d{6}\.\d{9}Z-[0-9a-f]{12}\.json$`)

func eventFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(ItemsDir(dir))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if IsEventEntry(e) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestAppend_NewStoreKeepsMonthShards(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))

	assert.False(t, UsesEventLayout(dir), "a new store must not opt in by itself")
	_, err := os.Stat(ItemsDir(dir))
	assert.True(t, os.IsNotExist(err), "a month-shard store has no items directory")
	_, err = os.Stat(filepath.Join(dir, "2026-06.jsonl"))
	assert.NoError(t, err, "the record lands in its month shard")
}

func TestAppend_EventLayoutWritesOneFilePerEvent(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	require.True(t, UsesEventLayout(dir))

	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))
	require.NoError(t, Append(dir, eventRec(idP, "2026-06-15T10:00:00Z", StatusResolved)))

	names := eventFiles(t, dir)
	require.Len(t, names, 2, "the add and the resolution are separate files")
	for _, n := range names {
		assert.Regexp(t, eventNameRe, n)
		assert.True(t, strings.HasPrefix(n, idP+"."), "file name starts with the finding id")
	}
	assert.True(t, strings.HasPrefix(names[0], idP+".20260614T100000.000000000Z-"))
	assert.True(t, strings.HasPrefix(names[1], idP+".20260615T100000.000000000Z-"))

	shards, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	assert.Empty(t, shards, "an event-layout store writes no month shard")

	recs, err := ReadEventRecords(dir, ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2, "history is kept: both events read back")
	assert.Equal(t, "", recs[0].Status)
	assert.Equal(t, StatusResolved, recs[1].Status)
	folded := FoldRecords(recs)
	require.Len(t, folded, 1)
	assert.Equal(t, StatusResolved, folded[0].Status)
}

func TestAppend_EventLayoutSameInstantDistinctEvents(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	a := eventRec(idP, "2026-06-14T10:00:00Z", StatusResolved)
	b := a
	b.Status = StatusWontfix
	require.NoError(t, Append(dir, a))
	require.NoError(t, Append(dir, b))
	assert.Len(t, eventFiles(t, dir), 2, "two events at one instant differ by hash, neither overwrites the other")

	// The same event written twice is the same file: the name is content-addressed.
	require.NoError(t, Append(dir, a))
	assert.Len(t, eventFiles(t, dir), 2)
}

func TestAppend_EventLayoutRejectsUnsafeID(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	for _, id := range []string{"../../escape0000", "abc", "00000000000000AA", "0000000000000.aa"} {
		err := Append(dir, eventRec(id, "2026-06-14T10:00:00Z", ""))
		assert.Error(t, err, "id %q must not become a file name", id)
	}
	assert.Empty(t, eventFiles(t, dir))
}

func TestAppend_EventLayoutRejectsBadTimestamp(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	r := eventRec(idP, "2026-06-14T10:00:00Z", "")
	r.Timestamp = "yesterday"
	assert.Error(t, Append(dir, r))
	assert.Empty(t, eventFiles(t, dir))
}

func TestEnableEventLayout_IdempotentAndTracked(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	require.NoError(t, EnableEventLayout(dir))
	// The marker keeps the items directory in git even before the first event, so
	// a clone of an opted-in store stays opted in.
	_, err := os.Stat(filepath.Join(ItemsDir(dir), eventLayoutMarker))
	assert.NoError(t, err)
}

func TestReadEventRecords_SkipsTempsAndForeignFiles(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))
	items := ItemsDir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(items, ".x.json.tmp-123"), []byte("{"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(items, "notes.txt"), []byte("hi"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(items, "sub.json"), 0o700))

	recs, err := ReadEventRecords(dir, ReadOpts{})
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestReadEventRecords_MissingDirIsEmpty(t *testing.T) {
	recs, err := ReadEventRecords(DefaultDir(t.TempDir()), ReadOpts{})
	require.NoError(t, err)
	assert.Empty(t, recs)
}

// git runs one git command pinned to the fixture repository dir, with no global or
// system git config, so it can never write into the developer's own checkout.
func git(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := gitexec.FixtureCommandContext(context.Background(), dir, append([]string{
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main",
	}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(t, dir, args...)
	require.NoError(t, err, "git %v: %s", args, out)
	return out
}

func commitAll(t *testing.T, repo, msg string) {
	t.Helper()
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-q", "-m", msg)
}

// divergedClones makes repository A holding one open item P, copies it to B (init
// and fetch: a fixture command is pinned to an existing repository, which a
// `git clone` target is not yet), and
// then on each side independently adds a new item and resolves P. It returns the
// two repositories and A's store dir, ready for B to be merged into A.
func divergedClones(t *testing.T, events bool) (repoA, repoB string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for the merge test: %v", err)
	}
	root := t.TempDir()
	repoA = filepath.Join(root, "a")
	repoB = filepath.Join(root, "b")
	require.NoError(t, os.Mkdir(repoA, 0o700))
	mustGit(t, repoA, "init", "-q")
	storeA := DefaultDir(repoA)
	if events {
		require.NoError(t, EnableEventLayout(storeA))
	}
	require.NoError(t, Append(storeA, eventRec(idP, "2026-06-14T10:00:00Z", "")))
	commitAll(t, repoA, "base: add P")

	require.NoError(t, os.Mkdir(repoB, 0o700))
	mustGit(t, repoB, "init", "-q")
	mustGit(t, repoB, "fetch", "-q", repoA, "main")
	mustGit(t, repoB, "reset", "-q", "--hard", "FETCH_HEAD")
	storeB := DefaultDir(repoB)

	require.NoError(t, Append(storeA, eventRec(idX, "2026-06-15T09:00:00Z", "")))
	require.NoError(t, Append(storeA, eventRec(idP, "2026-06-15T10:00:00Z", StatusResolved)))
	commitAll(t, repoA, "a: add X, resolve P")

	require.NoError(t, Append(storeB, eventRec(idY, "2026-06-15T09:30:00Z", "")))
	require.NoError(t, Append(storeB, eventRec(idP, "2026-06-15T11:00:00Z", StatusWontfix)))
	commitAll(t, repoB, "b: add Y, resolve P")
	return repoA, repoB
}

// TestEventLayout_GitMergeIsConflictFree is the epic 92 T1 success test: two
// repositories each add an item and resolve the same shared item, and a real
// `git merge` of one into the other exits cleanly with every record and every
// event of both histories present.
func TestEventLayout_GitMergeIsConflictFree(t *testing.T) {
	repoA, repoB := divergedClones(t, true)

	mustGit(t, repoA, "fetch", "-q", repoB, "main")
	out, err := git(t, repoA, "merge", "--no-edit", "FETCH_HEAD")
	require.NoError(t, err, "git merge must exit conflict-free: %s", out)
	assert.Empty(t, strings.TrimSpace(mustGit(t, repoA, "diff", "--name-only", "--diff-filter=U")))

	recs, err := ReadEventRecords(DefaultDir(repoA), ReadOpts{})
	require.NoError(t, err)
	byID := map[string][]string{}
	for _, r := range recs {
		byID[r.ID] = append(byID[r.ID], r.Timestamp+" "+r.Status)
	}
	assert.Equal(t, map[string][]string{
		idP: {"2026-06-14T10:00:00Z ", "2026-06-15T10:00:00Z resolved", "2026-06-15T11:00:00Z wontfix"},
		idX: {"2026-06-15T09:00:00Z "},
		idY: {"2026-06-15T09:30:00Z "},
	}, byID, "both records and both event histories survive the merge")

	folded := FoldRecords(recs)
	ids := make([]string, 0, len(folded))
	for _, r := range folded {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	assert.Equal(t, []string{idP, idX, idY}, ids, "no record lost")
}

// TestMonthShards_GitMergeConflicts pins why the event layout exists: the same
// history in month shards conflicts, so the conflict-free test above is not
// passing for a reason that has nothing to do with the layout.
func TestMonthShards_GitMergeConflicts(t *testing.T) {
	repoA, repoB := divergedClones(t, false)

	mustGit(t, repoA, "fetch", "-q", repoB, "main")
	_, err := git(t, repoA, "merge", "--no-edit", "FETCH_HEAD")
	assert.Error(t, err, "appending to one month shard on both sides must conflict")
	assert.Contains(t, mustGit(t, repoA, "diff", "--name-only", "--diff-filter=U"), "2026-06.jsonl")
}
