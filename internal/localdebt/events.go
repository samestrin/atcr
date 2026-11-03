package localdebt

import (
	"crypto/sha256"
	"encoding/hex"
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
)

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
	return basePathErr(f.Close())
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

// writeEventLocked writes one encoded record as a new event file. The caller holds
// the store lock and has run the write gate. The file is written to a dot-prefixed
// temp name and renamed into place, so a reader never sees a partial event. A
// rename onto an existing name replaces the same bytes: the name is a hash of them.
func writeEventLocked(dir string, rec Record, line []byte) error {
	name, err := eventFileName(rec, line)
	if err != nil {
		return err
	}
	items := ItemsDir(dir)
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
	if err := os.Rename(tmpPath, filepath.Join(items, name)); err != nil {
		return fmt.Errorf("publishing localdebt event: %w", basePathErr(err))
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
