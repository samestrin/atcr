package localdebt

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The event layout (Epic 92) stores each add and each resolution as its own file,
// <store>/items/<finding id>.<event ts>-<short hash>.json, holding one JSON record.
// Month shards ("2026-06.jsonl") are append-only files that two clones both extend,
// so a git merge of two clones conflicts on every shard both touched. An event file
// is written once and never changed, and two clones never write the same name
// unless they wrote the same event, so the merge has nothing to conflict on and the
// whole history is kept.
//
// The layout is opt-in: a new store keeps month shards (a standalone user sees no
// change) until EnableEventLayout creates the items directory, which is what
// `debt convert` or a config opt-in calls. The directory itself is the switch, so
// a clone of an opted-in store is opted in too.
const (
	// itemsSubdir is the event directory under the store dir.
	itemsSubdir = "items"
	// eventExt is the extension every event file carries.
	eventExt = ".json"
	// eventLayoutMarker is an empty file EnableEventLayout writes in the items
	// directory. Git does not track an empty directory, so without it a store
	// opted in before its first event would clone as a month-shard store.
	eventLayoutMarker = ".event-layout"
	// eventTimeLayout is the event timestamp in a file name: UTC, fixed width and
	// free of the colons RFC3339 carries (not legal in a Windows file name), so
	// the names of one item's events sort chronologically.
	eventTimeLayout = "20060102T150405.000000000Z"
	// eventHashLen is the length of the short content hash in a file name, in hex
	// characters. It tells apart two different events of one item at the same
	// instant; the same event written twice gets the same name.
	eventHashLen = 12
	// itemsGitignore is the ignore file shipped inside items/. A SIGKILL between
	// CreateTemp and rename skips writeEventLocked's deferred remove, and the
	// next locked write may be far away; until then `git add -A` in a shared
	// store would stage the leftover. The pattern covers exactly the CreateTemp
	// names ("."+name+".tmp-*"); the .event-layout marker stays tracked.
	itemsGitignore = ".gitignore"
)

// itemsGitignoreBody ignores in-flight and crash-leftover event temp files.
const itemsGitignoreBody = "# In-flight and crash-leftover event temp files (events.go).\n.*.tmp-*\n"

// ItemsDir returns the event directory of the store at dir: <dir>/items.
func ItemsDir(dir string) string {
	return filepath.Join(dir, itemsSubdir)
}

// UsesEventLayout reports whether the store at dir writes event files: its items
// directory exists. A missing or unreadable store reports false, the month-shard
// default.
func UsesEventLayout(dir string) bool {
	info, err := os.Stat(ItemsDir(dir))
	return err == nil && info.IsDir()
}

// EnableEventLayout opts the store at dir in to the event layout by creating its
// items directory and marker. Every later Append writes an event file. Existing
// month shards are left as they are; moving them is `debt convert`'s job. It is
// idempotent.
func EnableEventLayout(dir string) error {
	return withLock(dir, "enable-events", func() error {
		return enableEventLayoutLocked(dir)
	})
}

// enableEventLayoutLocked is EnableEventLayout's body without the lock: the caller
// must already hold the store lock (withLock), as Convert does.
func enableEventLayoutLocked(dir string) error {
	items := ItemsDir(dir)
	if err := os.MkdirAll(items, 0o700); err != nil {
		return fmt.Errorf("creating localdebt items dir: %w", basePathErr(err))
	}
	f, err := os.OpenFile(filepath.Join(items, eventLayoutMarker), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("writing localdebt event-layout marker: %w", basePathErr(err))
	}
	if err := f.Close(); err != nil {
		return basePathErr(err)
	}
	if err := ensureItemsGitignore(items); err != nil {
		return fmt.Errorf("writing localdebt items .gitignore: %w", err)
	}
	return nil
}

// errGitignoreNotRegular refuses an items/.gitignore that is a symlink, directory
// or other non-regular file.
var errGitignoreNotRegular = errors.New("items .gitignore is not a regular file; refusing to write through it")

// ensureItemsGitignore creates items/.gitignore if nothing is at that name. In a
// shared store the name can be repository-supplied, so it is never written
// through: a symlink (even a dangling one) or other non-regular entry is refused
// with errGitignoreNotRegular, and the create is O_EXCL|O_NOFOLLOW so a link
// planted after the Lstat fails the open instead of redirecting it. An existing
// regular file is kept as it is, never truncated. Errors are path-redacted.
func ensureItemsGitignore(items string) error {
	gi := filepath.Join(items, itemsGitignore)
	info, err := os.Lstat(gi)
	if err == nil {
		if info.Mode().IsRegular() {
			return nil
		}
		return errGitignoreNotRegular
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return basePathErr(err)
	}
	return createItemsGitignore(gi)
}

// createItemsGitignore creates the ignore file at gi with O_EXCL|O_NOFOLLOW: any
// entry already at the name, a symlink planted after ensureItemsGitignore's
// Lstat included, fails the open instead of being truncated or followed.
func createItemsGitignore(gi string) error {
	f, err := os.OpenFile(gi, os.O_CREATE|os.O_EXCL|os.O_WRONLY|openNoFollow, 0o600)
	if err != nil {
		return basePathErr(err)
	}
	if _, err := f.WriteString(itemsGitignoreBody); err != nil {
		_ = f.Close()
		_ = os.Remove(gi)
		return basePathErr(err)
	}
	return basePathErr(f.Close())
}

// readItemsDir is the items/ read prepareItemsLocked sweeps with; a var so a test
// can count the reads.
var readItemsDir = os.ReadDir

// isEventTempName reports whether a name in items/ is an in-flight or
// crash-leftover event temp: dot-prefixed with the ".tmp-" infix CreateTemp
// leaves. The marker and .gitignore carry no infix and never match.
func isEventTempName(name string) bool {
	return strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-")
}

// itemsPrep tracks whether prepareItemsLocked has run during one hold of the
// store lock. The sweep reads all of items/, so running it per event made a bulk
// append of N records into a store of M event files O(N x M) dirent reads under
// the lock; every temp write happens under that same lock, so once per hold is
// enough. The zero value has not run; a new hold takes a new itemsPrep.
type itemsPrep struct{ done bool }

// ensure runs prepareItemsLocked on the first call of the hold and does nothing
// after. A failed prepare is not retried within the hold: it is best-effort, and
// the next hold tries again.
func (p *itemsPrep) ensure(items string) {
	if p.done {
		return
	}
	p.done = true
	prepareItemsLocked(items)
}

// prepareItemsLocked ships items/.gitignore if missing and sweeps crash
// leftovers before a new temp is created. The caller holds the store lock, and
// every temp write happens under that same lock, so on the unix and Windows
// kernel locks any leftover seen here belongs to a dead process (a SIGKILL skips
// the deferred remove). The legacy mkdir lock can be reclaimed from a live
// holder, so there the sweep keeps temps younger than eventTempSweepMinAge. Like
// sweepStaleTemps (store.go), it runs on every lock hold that writes an event
// (itemsPrep) and removal is best-effort: readers skip temps regardless, and a
// failed remove is retried by the next hold. Failures are left to CreateTemp to
// report, so a broken items directory surfaces its own canonical error instead of
// a sweep error; a symlinked or otherwise non-regular .gitignore is left alone.
func prepareItemsLocked(items string) {
	_ = ensureItemsGitignore(items)
	sweepEventTemps(items, eventTempSweepMinAge)
}

// eventTempSweepMinAge is the age an event temp must reach before
// prepareItemsLocked reaps it. It is zero for the unix and Windows kernel locks,
// whose holder cannot lose the lock while it writes. The legacy mkdir lock
// (lock_other.go) sets it to lockStale in an init: a waiter reclaims that lock
// once its owner epoch is lockStale old, so a live holder's temp, created after
// it took the lock, is always younger than that and survives the reclaimer's
// sweep.
var eventTempSweepMinAge time.Duration

// sweepEventTemps removes the event temps in items whose modification time is at
// least minAge old; a zero minAge removes every temp without a stat. A temp whose
// age cannot be read is kept. Removal is best-effort, as in prepareItemsLocked.
func sweepEventTemps(items string, minAge time.Duration) {
	entries, err := readItemsDir(items)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !isEventTempName(e.Name()) {
			continue
		}
		if minAge > 0 {
			info, err := e.Info()
			if err != nil || time.Since(info.ModTime()) < minAge {
				continue
			}
		}
		_ = os.Remove(filepath.Join(items, e.Name()))
	}
}

// IsEventEntry reports whether a directory entry in the items directory is an
// event file: a non-directory whose name ends in ".json" and does not start with
// "." (the marker and in-flight temp files do).
func IsEventEntry(e fs.DirEntry) bool {
	name := e.Name()
	return !e.IsDir() && strings.HasSuffix(name, eventExt) && !strings.HasPrefix(name, ".")
}

// eventFileName builds the file name for one encoded event. The finding id must be
// well formed (16 lowercase hex characters) because it becomes part of a path: an
// id holding a dot or a slash would break the name's parsing or escape the items
// directory. The timestamp is the record's own, so the name is deterministic.
func eventFileName(rec Record, line []byte) (string, error) {
	if !IsWellFormedFindingID(rec.ID) {
		return "", fmt.Errorf("writing localdebt event: id %q is not a finding id (16 lowercase hex characters)", rec.ID)
	}
	ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return "", fmt.Errorf("writing localdebt event %q: timestamp %q is not RFC3339", rec.ID, rec.Timestamp)
	}
	sum := sha256.Sum256(line)
	return rec.ID + "." + ts.UTC().Format(eventTimeLayout) + "-" + hex.EncodeToString(sum[:])[:eventHashLen] + eventExt, nil
}

// beforeEventRename runs between writeEventLocked's temp close and its rename,
// with the temp's path. It is a var so a test can hold a writer mid-write;
// production leaves it empty.
var beforeEventRename = func(string) {}

// writeEventLocked writes one encoded record as a new event file. The caller holds
// the store lock and has run the write gate. The file is written to a dot-prefixed
// temp name and renamed into place, so a reader never sees a partial event. A
// rename onto an existing name replaces the same bytes: the name is a hash of them.
// Before the hold's first temp it ships items/.gitignore and sweeps crash
// leftovers (prep.ensure), so a SIGKILL'd write can neither be committed by
// `git add -A` nor accumulate in items/.
func writeEventLocked(dir string, rec Record, line []byte, prep *itemsPrep) error {
	name, err := eventFileName(rec, line)
	if err != nil {
		return err
	}
	items := ItemsDir(dir)
	prep.ensure(items)
	tmp, err := os.CreateTemp(items, "."+name+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating localdebt event: %w", basePathErr(err))
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(line); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing localdebt event: %w", basePathErr(err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing localdebt event: %w", basePathErr(err))
	}
	beforeEventRename(tmpPath)
	if err := os.Rename(tmpPath, filepath.Join(items, name)); err != nil {
		// os.Rename raises an *os.LinkError, which basePathErr passes through
		// untouched; quotedPathErr redacts both of its paths (backfill.go).
		return fmt.Errorf("publishing localdebt event: %w", quotedPathErr(err))
	}
	return nil
}

// ReadEventRecords reads every event file in the store's items directory, in name
// order (by finding id, then event time), with the same malformed and
// schema-version skips as a month shard. A missing items directory is empty, not
// an error. It reads the event layout only; ReadAll reads both.
func ReadEventRecords(dir string, opts ReadOpts) ([]Record, error) {
	files, err := eventFilePaths(dir)
	if err != nil {
		return nil, err
	}
	return readRecordFiles(files, opts)
}
