package localdebt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// maxLineBytes bounds a single JSONL line on read. Records are ~500 bytes; 1 MiB is
// a generous cap that prevents a corrupt/oversized line from allocating an unbounded
// reader buffer.
const maxLineBytes = 1 << 20

// ReadOpts carries read-path options. Writer is the sink for operational
// diagnostics emitted while reading (malformed records, over-long lines); a nil
// Writer defaults to os.Stderr so a zero ReadOpts keeps prior behavior. Writer must
// be safe for the caller's concurrency model; the package does not synchronize
// writes to it.
//
// SECURITY: diagnostics and wrapped errors may embed the store path. Callers follow
// the DefaultDir(".") relative-root convention, so paths are repo-relative today;
// but if an absolute dir is ever passed, a leaked path could contain a username
// (~/…). The read path reduces *os.PathError paths to their base name (basePathErr)
// for this reason, matching the write path. Before routing Writer to any non-local
// sink, scrub absolute paths and avoid echoing raw error strings.
type ReadOpts struct {
	Writer io.Writer
}

// diagWriter resolves a diagnostics sink: the caller-supplied writer, or os.Stderr
// when nil or a typed-nil pointer.
func diagWriter(w io.Writer) io.Writer {
	if w == nil || isNilPointer(w) {
		return os.Stderr
	}
	return w
}

// isNilPointer reports whether w is a non-nil interface wrapping a nil pointer (a
// typed nil handed in as io.Writer). w == nil is false for such a value, yet the
// first Write on it panics — so diagWriter treats it as unset and falls back to
// os.Stderr, preserving the "never panic in a diagnostics path" contract.
func isNilPointer(w io.Writer) bool {
	rv := reflect.ValueOf(w)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// Append writes one record as a single JSONL line to the month file derived from
// rec.RunID, under dir (created lazily with 0700 on first write). The line is
// marshaled to one []byte (record + '\n') and emitted in a single Write to a file
// opened O_APPEND. On Linux/macOS a write() to a regular file opened O_APPEND
// atomically seeks to end-of-file and writes contiguously, so two processes
// appending concurrently never interleave or lose a record — the guarantee is the
// per-write() atomic append for regular files, independent of record size (it is NOT
// the PIPE_BUF bound, which governs pipes/FIFOs). No bufio.Writer is shared across
// records — batching multiple records through one buffered flush would coalesce them
// into a single larger write whose atomicity is not guaranteed, tearing lines under
// concurrency. One Write per record preserves the guarantee. The file is 0600.
// (Portability caveat for non-POSIX append semantics: the accepted TD-004 won't-fix
// stance shared with the other five append-only ledgers.)
func Append(dir string, rec Record) error {
	return withLock(dir, "append", func() error {
		month, err := monthFromRunID(rec.RunID)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating localdebt dir: %w", basePathErr(err))
		}
		line, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("marshaling localdebt record: %w", err)
		}
		line = append(line, '\n')

		path := filepath.Join(dir, month+".jsonl")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("opening localdebt file: %w", basePathErr(err))
		}
		defer func() { _ = f.Close() }()
		if _, err := f.Write(line); err != nil {
			return fmt.Errorf("appending localdebt record: %w", basePathErr(err))
		}
		return nil
	})
}

// ReadRecords stream-parses a month JSONL file line-by-line. A malformed line is
// logged and skipped (a corrupt line never aborts the read), so a partially damaged
// file still yields its valid records. A record whose schema_version is newer than
// the current SchemaVersion is also logged and skipped, so a forward-incompatible
// record cannot be misread as the current version. A missing file surfaces as the
// raw os error so callers can phrase their own "no records" guidance via
// os.IsNotExist.
//
// The read is single-pass line-streaming (a bufio.Reader, never the whole file in
// one buffer). Parsed records are materialized into a returned slice; at the
// documented scale (~500 bytes/record) that is trivially cheap and intentional.
func ReadRecords(path string, opts ReadOpts) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	w := diagWriter(opts.Writer)

	var recs []Record
	// A bufio.Reader (not bufio.Scanner) is used so a single over-long line can be
	// drained and skipped rather than terminating the read: bufio.Scanner's
	// ErrTooLong is terminal and cannot resume, so one oversized line would abort the
	// whole read (and, via ReadAll, every month). The buffer is sized to maxLineBytes
	// so ReadSlice flags only a line past that cap.
	br := bufio.NewReaderSize(f, maxLineBytes)
	for {
		frag, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Line exceeds maxLineBytes: discard the buffered prefix, drain the rest
			// without buffering it, warn, and continue with the next line.
			_, _ = fmt.Fprintf(w, "localdebt: skipping over-long line (> %d bytes) in %s\n", maxLineBytes, path)
			if derr := drainLine(br); derr != nil {
				if derr == io.EOF {
					break
				}
				return recs, fmt.Errorf("reading localdebt file: %w", derr)
			}
			continue
		}
		if line := bytes.TrimSpace(frag); len(line) > 0 {
			if r, ok := decodeRecord(line, path, w); ok {
				recs = append(recs, r)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return recs, fmt.Errorf("reading localdebt file: %w", err)
		}
	}
	return recs, nil
}

// decodeRecord parses one trimmed JSONL line into a Record, applying the
// malformed-skip and schema-version-skip rules. ok is false (with a warning already
// emitted to w) when the line must be skipped.
func decodeRecord(line []byte, path string, w io.Writer) (Record, bool) {
	var r Record
	if err := json.Unmarshal(line, &r); err != nil {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: %v\n", path, err)
		return Record{}, false
	}
	// Schema-version negotiation: a record from a newer, forward-incompatible schema
	// must not be unmarshaled into this struct and treated as current — a field
	// rename/semantic change would silently corrupt the backlog. Warn and skip.
	if r.SchemaVersion > SchemaVersion {
		_, _ = fmt.Fprintf(w, "localdebt: skipping record with unsupported schema_version %d (> %d) in %s\n", r.SchemaVersion, SchemaVersion, path)
		return Record{}, false
	}
	if r.RunID == "" || r.ID == "" {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: missing required field (run_id or id)\n", path)
		return Record{}, false
	}
	return r, true
}

// drainLine discards bytes from br up to and including the next '\n' (or EOF)
// without buffering them, used to skip the remainder of an over-long line. It
// returns nil when a newline was consumed, or io.EOF / a read error otherwise.
func drainLine(br *bufio.Reader) error {
	for {
		_, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			continue
		}
		return err
	}
}

// ReadAll reads every *.jsonl month file under dir and returns the concatenated
// records (malformed lines skipped per-file by ReadRecords). A missing directory is
// empty (nil, nil), not an error — the "no backlog yet" state. Non-.jsonl files are
// ignored. Shard files are read in os.ReadDir order (lexical), so month shards
// aggregate chronologically.
func ReadAll(dir string, opts ReadOpts) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		// A non-ENOENT ReadDir failure (e.g. a permission error) can carry an
		// absolute dir path; reduce it to its base name so a username-bearing path is
		// never embedded (matching the write path). The per-shard ReadRecords error is
		// redacted the same way at the return below: basePathErr rewrites only
		// *os.PathError.Path and preserves the underlying Err, and ReadAll's own ENOENT
		// check runs on the raw error first, so os.IsNotExist stays usable there.
		return nil, fmt.Errorf("reading localdebt dir: %w", basePathErr(err))
	}
	var all []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		recs, err := ReadRecords(filepath.Join(dir, e.Name()), opts)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// Non-ENOENT (the missing-file case continued above): redact the shard
			// path so an EACCES open never leaks an absolute username-bearing path.
			return all, basePathErr(err)
		}
		all = append(all, recs...)
	}
	return all, nil
}

// FoldRecords folds a stream of records by ID to their effective state. Callers
// must supply records with non-empty IDs — an invariant the read path already
// enforces (decodeRecord rejects empty-id records as malformed, and StampID
// always yields a non-empty content hash): records sharing an empty ID would
// collapse into a single fold group and silently lose all but one.
func FoldRecords(recs []Record) []Record {
	order := []string{}
	seen := map[string]bool{}

	byID := map[string][]Record{}
	for _, r := range recs {
		if !seen[r.ID] {
			seen[r.ID] = true
			order = append(order, r.ID)
		}
		byID[r.ID] = append(byID[r.ID], r)
	}

	var folded []Record
	for _, id := range order {
		group := byID[id]

		var openRecs []Record
		var termRecs []Record
		for _, r := range group {
			if IsClosedStatus(r.Status) {
				termRecs = append(termRecs, r)
			} else {
				openRecs = append(openRecs, r)
			}
		}

		if len(termRecs) > 0 {
			best := termRecs[0]
			for _, r := range termRecs[1:] {
				if ClosedStatusRank(r.Status) > ClosedStatusRank(best.Status) {
					best = r
				} else if ClosedStatusRank(r.Status) == ClosedStatusRank(best.Status) {
					if r.Timestamp >= best.Timestamp {
						best = r
					}
				}
			}
			folded = append(folded, best)
		} else if len(openRecs) > 0 {
			best := openRecs[len(openRecs)-1]
			folded = append(folded, best)
		}
	}
	return folded
}

// Compact reads all records in dir, folds them by ID to keep only the effective
// records (dropping superseded ones), and rewrites the sharded monthly JSONL
// files atomically. Shards that no longer have any active records are deleted.
// Compact runs within a cross-process lock to prevent races with concurrent Appends.
func Compact(dir string, opts ReadOpts) error {
	return withLock(dir, "compact", func() error {
		recs, err := ReadAll(dir, opts)
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			return nil
		}

		folded := FoldRecords(recs)

		byMonth := map[string][]Record{}
		for _, r := range folded {
			month, err := monthFromRunID(r.RunID)
			if err != nil {
				return err
			}
			byMonth[month] = append(byMonth[month], r)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("reading localdebt dir for compaction: %w", basePathErr(err))
		}
		existingMonths := map[string]bool{}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				month := strings.TrimSuffix(e.Name(), ".jsonl")
				if monthRe.MatchString(month) {
					existingMonths[month] = true
				}
			}
		}

		for month, monthRecs := range byMonth {
			var buf bytes.Buffer
			for _, r := range monthRecs {
				line, err := json.Marshal(r)
				if err != nil {
					return fmt.Errorf("marshaling localdebt record: %w", err)
				}
				buf.Write(line)
				buf.WriteByte('\n')
			}

			path := filepath.Join(dir, month+".jsonl")

			tmp, err := os.CreateTemp(dir, "."+month+".jsonl.tmp-*")
			if err != nil {
				return fmt.Errorf("creating temp file for compaction: %w", basePathErr(err))
			}
			tmpName := tmp.Name()
			cleanup := true
			defer func() {
				if cleanup {
					_ = os.Remove(tmpName)
				}
			}()

			if _, err := tmp.Write(buf.Bytes()); err != nil {
				_ = tmp.Close()
				return fmt.Errorf("writing compacted records: %w", basePathErr(err))
			}
			if err := tmp.Chmod(0o600); err != nil {
				_ = tmp.Close()
				return fmt.Errorf("setting compacted file permissions: %w", basePathErr(err))
			}
			if err := tmp.Close(); err != nil {
				return fmt.Errorf("closing compacted temp file: %w", basePathErr(err))
			}

			if err := os.Rename(tmpName, path); err != nil {
				return fmt.Errorf("renaming compacted file: %w", basePathErr(err))
			}
			cleanup = false

			delete(existingMonths, month)
		}

		for month := range existingMonths {
			path := filepath.Join(dir, month+".jsonl")
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing empty shard file %s: %w", path, basePathErr(err))
			}
		}

		return nil
	})
}
