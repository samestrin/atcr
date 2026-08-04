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
	recs, _, err := scanShard(path, opts, nil)
	return recs, err
}

// scanShard is the shared line-scanning core of ReadRecords and the compaction
// read path. When preserve is non-nil it is called with a copy of every line that
// is well-formed but forward-incompatible, in file order, so compaction can carry
// those lines through a rewrite instead of destroying them (see Compact). The copy
// matters: frag aliases the bufio.Reader's internal buffer and is invalidated by
// the next ReadSlice. Malformed lines are never handed to preserve — they are
// corrupt, not merely unreadable by this version.
//
// The second return value reports that the shard contains a line this reader could
// not represent at all: one longer than maxLineBytes, skipped by the buffer guard
// before any decode happens, and therefore not capturable by preserve without
// abandoning the memory bound the guard exists to enforce. Such a shard is not
// safely rewritable — compaction must leave it alone rather than rebuild it from
// the subset it managed to read.
func scanShard(path string, opts ReadOpts, preserve func([]byte)) ([]Record, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	w := diagWriter(opts.Writer)

	var recs []Record
	unrepresentable := false
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
			// without buffering it, warn, and continue with the next line. The line is
			// deliberately not captured — buffering it is exactly what the cap forbids
			// — so the shard is flagged unrepresentable instead, and compaction leaves
			// it whole rather than rewriting it without this line.
			unrepresentable = true
			_, _ = fmt.Fprintf(w, "localdebt: skipping over-long line (> %d bytes) in %s\n", maxLineBytes, path)
			if derr := drainLine(br); derr != nil {
				if derr == io.EOF {
					break
				}
				return recs, unrepresentable, fmt.Errorf("reading localdebt file: %w", derr)
			}
			continue
		}
		if line := bytes.TrimSpace(frag); len(line) > 0 {
			switch r, outcome := decodeRecord(line, path, w); outcome {
			case decodeOK:
				recs = append(recs, r)
			case decodeForwardIncompatible:
				if preserve != nil {
					preserve(bytes.Clone(line))
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return recs, unrepresentable, fmt.Errorf("reading localdebt file: %w", err)
		}
	}
	return recs, unrepresentable, nil
}

// decodeOutcome classifies what happened to one JSONL line on the read path. The
// distinction matters to compaction: a forward-incompatible line is valid data this
// binary cannot yet interpret and must survive a rewrite, whereas a malformed line
// is corrupt and compaction is where it gets cleaned up.
type decodeOutcome int

const (
	// decodeOK: the line parsed into a usable Record.
	decodeOK decodeOutcome = iota
	// decodeMalformed: the line is corrupt or missing a required field.
	decodeMalformed
	// decodeForwardIncompatible: the line is well-formed but carries a
	// schema_version newer than this binary understands.
	decodeForwardIncompatible
)

// decodeRecord parses one trimmed JSONL line into a Record, applying the
// malformed-skip and schema-version-skip rules. The outcome is decodeOK when the
// returned Record is usable; otherwise a warning has already been emitted to w and
// the caller decides what to do with the raw line (see decodeOutcome).
func decodeRecord(line []byte, path string, w io.Writer) (Record, decodeOutcome) {
	var r Record
	if err := json.Unmarshal(line, &r); err != nil {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: %v\n", path, err)
		return Record{}, decodeMalformed
	}
	// Schema-version negotiation: a record from a newer, forward-incompatible schema
	// must not be unmarshaled into this struct and treated as current — a field
	// rename/semantic change would silently corrupt the backlog. Warn and skip.
	if r.SchemaVersion > SchemaVersion {
		_, _ = fmt.Fprintf(w, "localdebt: skipping record with unsupported schema_version %d (> %d) in %s\n", r.SchemaVersion, SchemaVersion, path)
		return Record{}, decodeForwardIncompatible
	}
	if r.RunID == "" || r.ID == "" {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: missing required field (run_id or id)\n", path)
		return Record{}, decodeMalformed
	}
	return r, decodeOK
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

// shardRead is readAllPreserving's result: every decodable record across the store,
// plus the per-shard bookkeeping compaction needs to rewrite shards without losing
// what it could not read.
type shardRead struct {
	// records is the concatenation of every decodable record, in shard order —
	// identical to what ReadAll returns.
	records []Record
	// preserved maps a month shard to the forward-incompatible raw lines read from
	// it, in file order, to be written back into that same shard.
	preserved map[string][][]byte
	// protected is the set of month shards holding a line this reader could not
	// represent (over-long). They must be neither rewritten nor removed.
	protected map[string]bool
}

// readAllPreserving is ReadAll for the compaction path: same records in the same
// order, plus the per-shard detail Compact needs to avoid destroying anything it
// could not read.
//
// The shard association is why compaction cannot reuse ReadAll. A preserved line's
// schema is by definition unknown to this binary, so its run_id — and therefore the
// month it belongs to — cannot be derived the way a decoded record's can. Writing it
// back to the file it came from is the only placement that is correct without
// understanding it.
//
// Only monthRe-matching shard names are tracked. A non-month .jsonl file is still
// read for records (matching ReadAll), but Compact never rewrites or removes it, so
// its contents are already safe. Note the pre-existing consequence, unchanged here:
// records in such a file are read AND re-emitted into their run_id's month shard,
// so they exist twice on disk afterwards and inflate the next run's RecordsBefore.
// The duplicate is harmless — the fold collapses it on read.
func readAllPreserving(dir string, opts ReadOpts) (shardRead, error) {
	out := shardRead{
		preserved: map[string][][]byte{},
		protected: map[string]bool{},
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("reading localdebt dir: %w", basePathErr(err))
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		month := strings.TrimSuffix(e.Name(), ".jsonl")
		isMonthShard := monthRe.MatchString(month)
		var collect func([]byte)
		if isMonthShard {
			collect = func(line []byte) { out.preserved[month] = append(out.preserved[month], line) }
		}

		recs, unrepresentable, err := scanShard(filepath.Join(dir, e.Name()), opts, collect)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return out, basePathErr(err)
		}
		if unrepresentable && isMonthShard {
			out.protected[month] = true
		}
		out.records = append(out.records, recs...)
	}
	return out, nil
}

// FoldRecords folds a stream of records by ID to their effective state. Callers
// must supply records with non-empty IDs — an invariant the read path already
// enforces (decodeRecord rejects empty-id records as malformed, and StampID
// always yields a non-empty content hash): records sharing an empty ID would
// collapse into a single fold group and silently lose all but one.
//
// # Precedence
//
// The rule is RECENCY, with one unconditional exception:
//
//  1. If any record for the id suppresses (wontfix — see IsSuppressingStatus),
//     the effective record is chosen among the SUPPRESSING records alone, by
//     ClosedStatusRank then latest timestamp. A dismissal is permanent, so no
//     later re-detection can displace it.
//  2. Otherwise the effective record is the latest by timestamp across open and
//     non-suppressing-terminal records alike. An equal timestamp is broken by
//     ClosedStatusRank (a terminal record outranks an open one, so a resolution
//     appended in the same second as the finding still closes it), and a full
//     tie by append order (last wins).
//
// Rule 2 is what makes a resolved-or-deferred id RE-OPEN when it is detected
// again: the fresh open record is newer than the resolution, so it wins. That is
// deliberate and is the point of the split — because line number is part of the
// finding id, a re-detection at the same file/line/problem after a fix means a
// regression, not a duplicate.
//
// # Timestamp comparison
//
// Timestamps are compared LEXICOGRAPHICALLY, never parsed. That is sound only
// because every producer writes UTC RFC3339: reconcile records use
// res.Summary.ReconciledAt (reconcile/reconcile.go, .UTC().Format(time.RFC3339))
// and resolution/manual records use time.Now().UTC().Format(time.RFC3339)
// (cli/debt_resolve.go, cli/debt_add.go). An offset-bearing value would break the
// ordering — "2026-01-01T01:00:00+05:00" is really 2025-12-31T20:00:00Z yet sorts
// after "2026-01-01T00:00:00Z" — so a writer that does not normalize to UTC is a
// silent ordering bug. Normalize at the write site.
//
// # Maintenance invariant
//
// This read-side fold and cli/reconcile.go's persistLocalDebt write-side dedup
// seeding are ONE decision in two places. The fold is unconditional, so widening
// the dedup seed back to every id in the store would make a regressed finding
// never re-append and silently restore the old permanent-closure behavior with
// every test here still passing. Change one, change the other.
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

		// Rule 1: a suppressing record wins unconditionally. Among several, rank
		// then recency decides — preserving the read-order independence divergent
		// terminal records already relied on.
		var suppressing []Record
		for _, r := range group {
			if IsSuppressingStatus(r.Status) {
				suppressing = append(suppressing, r)
			}
		}
		if len(suppressing) > 0 {
			folded = append(folded, bestByRankThenRecency(suppressing))
			continue
		}

		// Rule 2: recency across open and non-suppressing-terminal records alike,
		// so a re-detection newer than a resolution re-opens the id.
		if len(group) > 0 {
			folded = append(folded, bestByRankThenRecency(group))
		}
	}
	return folded
}

// bestByRankThenRecency picks the effective record from a non-empty fold group:
// latest timestamp wins; an equal timestamp is broken by ClosedStatusRank so a
// terminal record outranks an open one (rank 0), and a full tie by append order
// (>= keeps the last).
func bestByRankThenRecency(group []Record) Record {
	best := group[0]
	for _, r := range group[1:] {
		switch {
		case r.Timestamp > best.Timestamp:
			best = r
		case r.Timestamp == best.Timestamp && ClosedStatusRank(r.Status) > ClosedStatusRank(best.Status):
			best = r
		case r.Timestamp == best.Timestamp && ClosedStatusRank(r.Status) == ClosedStatusRank(best.Status):
			best = r // full tie: append order, last wins
		}
	}
	return best
}

// sweepStaleTemps removes compaction temp files (.<month>.jsonl.tmp-*) leaked by a
// Compact killed between CreateTemp and rename (a SIGKILL skips the deferred
// os.Remove). It runs at the start of every Compact — under the same cross-process
// lock and before any new temp is created — so crash debris is reaped by the next
// run instead of accumulating in the store dir. Removal is best-effort: a failed
// remove is retried by the next Compact, and ReadAll ignores the temps regardless
// (they lack the .jsonl suffix), so a sweep failure never blocks compaction.
func sweepStaleTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name := e.Name(); strings.HasPrefix(name, ".") && strings.Contains(name, ".jsonl.tmp-") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// CompactResult reports what a Compact call did so a caller can tell a real fold
// from a no-op. StoreFound is false when there was nothing to compact (the store is
// missing or holds no decodable records). RecordsBefore is the total records read;
// RecordsAfter is the folded (live) count; Dropped is RecordsBefore-RecordsAfter,
// the superseded records removed. Preserved counts forward-incompatible lines
// carried through untouched — they are not records this binary can fold, so they
// are excluded from the other three counts and explain any gap between the reported
// fold and the store's actual on-disk size. All counts are zero on a no-op except
// Preserved, which is reported even then.
type CompactResult struct {
	StoreFound    bool
	RecordsBefore int
	RecordsAfter  int
	Dropped       int
	Preserved     int
}

// Compact reads all records in dir, folds them by ID to keep only the effective
// records (dropping superseded ones), and rewrites the sharded monthly JSONL
// files atomically. Shards that no longer have any active records are deleted.
// Compact runs within a cross-process lock to prevent races with concurrent Appends.
//
// # Forward-incompatible records are preserved, never destroyed
//
// A rewrite-from-what-we-could-read is destructive by construction: every line the
// read path skipped would vanish, so an older binary compacting a store written by
// a newer one would permanently delete the newer records rather than merely failing
// to display them — and would delete outright any shard holding only such records.
// Compact therefore reads per shard, retaining forward-incompatible lines verbatim
// (including keys this binary has no field for) and writing them back into the shard
// they came from. A shard is removed only when it holds neither folded records nor
// preserved lines.
//
// A shard containing a line too long to buffer (> maxLineBytes) is a third case:
// the line cannot be captured without abandoning the memory bound that cap exists to
// enforce, so instead the whole shard is left untouched — neither rewritten nor
// removed. It simply goes uncompacted. That costs disk; rewriting it would cost
// data, and a future schema embedding a blob, diff, or log makes oversized records
// ordinary rather than exotic.
//
// Malformed lines are deliberately NOT preserved. A forward-incompatible or
// over-long line is valid data this version cannot interpret or even buffer; a
// corrupt line is corrupt, and compaction remains where it is cleaned up. Carrying
// corrupt bytes forward would grow the store without bound.
//
// Preserved lines are appended after the folded records within their shard rather
// than at their original offsets. Compaction already reorders records (the fold is
// global while the write is per-month), so within-shard position is not a contract;
// what is guaranteed is that the bytes survive and stay in their own relative order.
func Compact(dir string, opts ReadOpts) (CompactResult, error) {
	var result CompactResult
	err := withLock(dir, "compact", func() error {
		sweepStaleTemps(dir)
		read, err := readAllPreserving(dir, opts)
		if err != nil {
			return err
		}
		recs, preservedByShard, protected := read.records, read.preserved, read.protected
		preservedCount := 0
		for _, lines := range preservedByShard {
			preservedCount += len(lines)
		}
		if len(recs) == 0 {
			// Nothing foldable. Report any preserved lines so the caller can explain
			// the no-op, but touch no file: rewriting from an empty fold is exactly
			// the destruction this guard exists to prevent.
			result.Preserved = preservedCount
			return nil // otherwise result stays zero: StoreFound false (no-op)
		}

		folded := FoldRecords(recs)
		result = CompactResult{
			StoreFound:    true,
			RecordsBefore: len(recs),
			RecordsAfter:  len(folded),
			Dropped:       len(recs) - len(folded),
			Preserved:     preservedCount,
		}

		byMonth := map[string][]Record{}
		for _, r := range folded {
			month, err := monthFromRunID(r.RunID)
			if err != nil {
				return err
			}
			if protected[month] {
				// The shard is left whole, superseded records and all. Its records still
				// took part in the global fold above, so any copy of this id living in
				// another shard is folded correctly; the stale copy left here is
				// collapsed by the fold on the next read.
				continue
			}
			byMonth[month] = append(byMonth[month], r)
		}
		// A shard with only preserved lines still has to be rewritten (rather than
		// skipped and later removed), so seed it with an empty record set.
		for month := range preservedByShard {
			if protected[month] {
				continue
			}
			if _, ok := byMonth[month]; !ok {
				byMonth[month] = nil
			}
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("reading localdebt dir for compaction: %w", basePathErr(err))
		}
		existingMonths := map[string]bool{}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				month := strings.TrimSuffix(e.Name(), ".jsonl")
				// A protected shard is never a removal candidate: it is excluded here
				// rather than deleted from the set later, so no path can reach os.Remove
				// for it.
				if monthRe.MatchString(month) && !protected[month] {
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
			// Carry forward-incompatible lines through byte-for-byte. They are never
			// re-marshaled: this binary's Record cannot round-trip fields it does not
			// declare, so marshaling a decoded copy is precisely how the data would be
			// lost.
			for _, line := range preservedByShard[month] {
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
				// basePathErr only redacts the wrapped *os.PathError; the raw %s of path
				// would still leak the absolute (username-bearing) shard path, so reduce
				// it to the base name too (SECURITY contract, store.go:26-31).
				return fmt.Errorf("removing empty shard file %s: %w", filepath.Base(path), basePathErr(err))
			}
		}

		return nil
	})
	return result, err
}
