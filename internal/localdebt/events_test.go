package localdebt

import (
	"context"
	"encoding/json"
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

// eventLine marshals rec exactly as appendLocked does: one JSON line with its
// trailing newline, the bytes whose hash names the event file.
func eventLine(t *testing.T, rec Record) []byte {
	t.Helper()
	line, err := json.Marshal(rec)
	require.NoError(t, err)
	return append(line, '\n')
}

// TestAppend_EventLayoutSweepsCrashLeftovers pins the crash-debris fix: a SIGKILL
// between CreateTemp and rename skips the deferred remove, so the next locked
// write must reap the dot-prefixed .tmp- leftover instead of letting it
// accumulate in items/ (row 1a57b0e633a1a9a6). Lookalikes — the marker, a foreign
// file, a directory — must survive the sweep.
func TestAppend_EventLayoutSweepsCrashLeftovers(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	items := ItemsDir(dir)

	// An event published by an earlier process, written by hand so this process
	// has not yet touched the store's write path.
	old := eventRec(idP, "2026-06-14T10:00:00Z", "")
	line := eventLine(t, old)
	oldName, err := eventFileName(old, line)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(items, oldName), line, 0o600))

	// The SIGKILL leftover: the exact shape CreateTemp produces, half-written.
	leftover := filepath.Join(items, "."+oldName+".tmp-123456")
	require.NoError(t, os.WriteFile(leftover, []byte("{partial"), 0o600))
	// Lookalikes the sweep must not touch.
	require.NoError(t, os.Mkdir(filepath.Join(items, ".dir.json.tmp-1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(items, "notes.txt"), []byte("hi"), 0o600))

	require.NoError(t, Append(dir, eventRec(idX, "2026-06-15T10:00:00Z", "")))

	assert.NoFileExists(t, leftover, "the next locked write reaps the crash leftover")
	assert.FileExists(t, filepath.Join(items, oldName), "a published event is never swept")
	assert.FileExists(t, filepath.Join(items, eventLayoutMarker), "the layout marker is never swept")
	assert.FileExists(t, filepath.Join(items, "notes.txt"), "a foreign file is never swept")
	assert.DirExists(t, filepath.Join(items, ".dir.json.tmp-1"), "a directory is never swept")
	assert.Len(t, eventFiles(t, dir), 2, "the old and the new event both read back")
}

// TestAppend_EventLayoutPreExistingStoreGetsGitignore pins the second half of the
// fix: a store opted in before this change has no items/.gitignore, so the first
// event write must ship one. Without it a crash leftover between now and the next
// write is staged by `git add -A` in a shared store.
func TestAppend_EventLayoutPreExistingStoreGetsGitignore(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	items := ItemsDir(dir)
	// An opted-in store as an older binary left it: directory and marker only.
	require.NoError(t, os.MkdirAll(items, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(items, eventLayoutMarker), nil, 0o600))

	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))

	b, err := os.ReadFile(filepath.Join(items, ".gitignore"))
	require.NoError(t, err, "the first event write ships items/.gitignore")
	assert.Contains(t, string(b), ".*.tmp-*", "the ignore pattern covers the CreateTemp names")
}

// Epic 122 F2(a): an existing ignore file is never truncated. EnableEventLayout
// used to rewrite it on every call, dropping any line a clone had added.
func TestEnableEventLayout_ExistingGitignoreNotTruncated(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	gi := filepath.Join(ItemsDir(dir), itemsGitignore)
	custom := itemsGitignoreBody + "local-notes.txt\n"
	require.NoError(t, os.WriteFile(gi, []byte(custom), 0o600))

	require.NoError(t, EnableEventLayout(dir))
	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))

	b, err := os.ReadFile(gi)
	require.NoError(t, err)
	assert.Equal(t, custom, string(b))
}

// countItemsReads swaps the readItemsDir seam for one that counts reads of the
// store's items directory, restoring it at cleanup.
func countItemsReads(t *testing.T, dir string) *int {
	t.Helper()
	n := new(int)
	items := ItemsDir(dir)
	orig := readItemsDir
	readItemsDir = func(name string) ([]os.DirEntry, error) {
		if name == items {
			*n++
		}
		return orig(name)
	}
	t.Cleanup(func() { readItemsDir = orig })
	return n
}

// Epic 122 F2(b): the crash-leftover sweep read items/ on every event write, so a
// bulk append of N records into a store of M event files was O(N x M) dirent
// reads under the store lock. It must run once per lock hold.
func TestAppendBatch_EventLayoutReadsItemsOncePerLockHold(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	reads := countItemsReads(t, dir)

	recs := []Record{
		eventRec(idP, "2026-06-14T10:00:00Z", ""),
		eventRec(idX, "2026-06-14T10:00:01Z", ""),
		eventRec(idY, "2026-06-14T10:00:02Z", ""),
		eventRec(idP, "2026-06-14T10:00:03Z", StatusResolved),
	}
	written, err := appendBatch(dir, recs)
	require.NoError(t, err)
	require.Equal(t, len(recs), written)
	assert.Equal(t, 1, *reads, "one batch is one lock hold: items/ is swept once")
	assert.Len(t, eventFiles(t, dir), len(recs))

	// Each Append is its own lock hold, so each still sweeps (and reaps a leftover).
	leftover := filepath.Join(ItemsDir(dir), ".x.json.tmp-1")
	require.NoError(t, os.WriteFile(leftover, []byte("{"), 0o600))
	require.NoError(t, Append(dir, eventRec(idX, "2026-06-15T10:00:00Z", "")))
	assert.Equal(t, 2, *reads)
	assert.NoFileExists(t, leftover)
}

// TestEventLayout_GitAddNeverStagesCrashLeftover is the harm the row names, end to
// end: in a real git repository holding an event-layout store, a crash leftover
// planted after the last commit must never be staged by `git add -A`, while a
// real new event still stages normally.
func TestEventLayout_GitAddNeverStagesCrashLeftover(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required for the staging test: %v", err)
	}
	repo := t.TempDir()
	mustGit(t, repo, "init", "-q")
	store := DefaultDir(repo)
	require.NoError(t, EnableEventLayout(store))
	require.NoError(t, Append(store, eventRec(idP, "2026-06-14T10:00:00Z", "")))
	commitAll(t, repo, "base: add P")

	// The SIGKILL leftover, planted after the commit and before any next write.
	leftover := "." + idX + ".20260615T100000.000000000Z-0123456789ab.json.tmp-42"
	require.NoError(t, os.WriteFile(filepath.Join(ItemsDir(store), leftover), []byte("{"), 0o600))
	mustGit(t, repo, "add", "-A")
	assert.NotContains(t, mustGit(t, repo, "diff", "--cached", "--name-only"), ".tmp-",
		"a crash leftover must never be staged by git add -A")

	// A real new event still stages: the ignore file is narrow, not the whole dir.
	require.NoError(t, Append(store, eventRec(idX, "2026-06-15T10:00:00Z", "")))
	mustGit(t, repo, "add", "-A")
	assert.Contains(t, mustGit(t, repo, "diff", "--cached", "--name-only"), idX+".",
		"a published event still stages normally")
}

// TestAppend_EventLayoutPublishFailsOnDirectoryAtEventName injects the publish
// fault: a directory squatting on the exact event name makes the final rename
// fail. Append must report the publish error and the deferred remove must still
// reap the temp file, leaving no debris.
func TestAppend_EventLayoutPublishFailsOnDirectoryAtEventName(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	rec := eventRec(idP, "2026-06-14T10:00:00Z", "")
	name, err := eventFileName(rec, eventLine(t, rec))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(ItemsDir(dir), name), 0o700))

	err = Append(dir, rec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publishing localdebt event")
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")

	entries, err := os.ReadDir(ItemsDir(dir))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-", "the failed publish leaves no temp debris: %s", e.Name())
	}
	assert.DirExists(t, filepath.Join(ItemsDir(dir), name), "the squatting directory is left alone")
}

// TestEnableEventLayout_ItemsAsRegularFileIsError injects the CreateTemp-side
// fault at opt-in: items exists but is a regular file, so MkdirAll must fail with
// the items-dir error rather than panic or silently continue.
func TestEnableEventLayout_ItemsAsRegularFileIsError(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(ItemsDir(dir), []byte("not a dir"), 0o600))

	err := EnableEventLayout(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating localdebt items dir")
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
}

// TestWriteEventLocked_ItemsAsRegularFileIsError injects the same fault at the
// write path: CreateTemp under a regular-file items must fail with the
// creating-event error (the sweep and gitignore steps must not mask it).
func TestWriteEventLocked_ItemsAsRegularFileIsError(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(ItemsDir(dir), []byte("not a dir"), 0o600))

	rec := eventRec(idP, "2026-06-14T10:00:00Z", "")
	err := writeEventLocked(dir, rec, eventLine(t, rec), &itemsPrep{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating localdebt event")
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
}

// TestEnableEventLayout_MarkerBlockedIsError covers the marker-write branch: a
// directory squatting on the marker name makes OpenFile fail.
func TestEnableEventLayout_MarkerBlockedIsError(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(ItemsDir(dir), eventLayoutMarker), 0o700))

	err := EnableEventLayout(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing localdebt event-layout marker")
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
}

// TestReadEventRecords_UnreadableEventFileReturnsWhatItReadRedacted pins the
// partial-result contract of the EACCES branch: the events read before the
// unreadable one still come back, the error is redacted, and the unreadable file
// is not silently dropped. (The error-and-redaction shape is also covered by
// TestDualLayout_UnreadableEventsFailEveryReadPathRedacted; the partial records
// are new here.)
func TestReadEventRecords_UnreadableEventFileReturnsWhatItReadRedacted(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied read cannot be provoked as root")
	}
	dir := DefaultDir(t.TempDir())
	require.NoError(t, EnableEventLayout(dir))
	require.NoError(t, Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", "")))
	require.NoError(t, Append(dir, eventRec(idP, "2026-06-15T10:00:00Z", StatusResolved)))
	names := eventFiles(t, dir)
	require.Len(t, names, 2)
	blocked := filepath.Join(ItemsDir(dir), names[1])
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o600) })

	recs, err := ReadEventRecords(dir, ReadOpts{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
	require.Len(t, recs, 1, "the readable first event still comes back with the error")
	assert.Equal(t, "2026-06-14T10:00:00Z", recs[0].Timestamp)
}

// TestEnableEventLayout_GitignoreBlockedIsError covers the ignore-file write
// branch: a directory squatting on the .gitignore name makes WriteFile fail.
func TestEnableEventLayout_GitignoreBlockedIsError(t *testing.T) {
	dir := DefaultDir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(ItemsDir(dir), ".gitignore"), 0o700))

	err := EnableEventLayout(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing localdebt items .gitignore")
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
}

// TestAppend_EventLayoutReadOnlyItemsIsError pins that the best-effort prepare
// (gitignore write, sweep) never masks the canonical CreateTemp error: under a
// read-only items directory the append fails with the creating-event error and
// the store is left untouched.
func TestAppend_EventLayoutReadOnlyItemsIsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a permission-denied write cannot be provoked as root")
	}
	dir := DefaultDir(t.TempDir())
	items := ItemsDir(dir)
	// A pre-existing store without the ignore file, so the prepare step's own
	// gitignore write is attempted and fails too — both failures stay silent.
	require.NoError(t, os.MkdirAll(items, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(items, eventLayoutMarker), nil, 0o600))
	require.NoError(t, os.Chmod(items, 0o500))
	t.Cleanup(func() { _ = os.Chmod(items, 0o700) })

	err := Append(dir, eventRec(idP, "2026-06-14T10:00:00Z", ""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating localdebt event")
	assert.NotContains(t, err.Error(), dir, "the absolute store path is redacted")
	assert.Empty(t, eventFiles(t, dir))
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
