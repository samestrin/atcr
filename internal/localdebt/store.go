package localdebt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// maxLineBytes bounds a single JSONL line on read. Records are ~500 bytes; 1 MiB is
// a generous cap that prevents a corrupt/oversized line from allocating an unbounded
// reader buffer.
const maxLineBytes = 1 << 20

// MaxRecordBytes is maxLineBytes exported for WRITERS: the largest encoded record
// (JSON plus its newline) any reader of this store will ever see. A writer that
// exceeds it produces a line every read path skips — the record is written,
// invisible forever, and its shard is permanently protected from compaction
// (Compact preserves shards holding an unreadable line). Callers that accept
// unbounded free text — `atcr debt add`, whose wizard scanner is itself
// configured for 1 MiB lines — check the encoded size against this before
// appending, the way `debt resolve --reason` bounds its justification.
const MaxRecordBytes = maxLineBytes

// ReadOpts carries read-path options. Writer is the sink for operational
// diagnostics emitted while reading (malformed records, over-long lines); a nil
// Writer defaults to os.Stderr so a zero ReadOpts keeps prior behavior. Writer must
// be safe for the caller's concurrency model; the package does not synchronize
// writes to it.
//
// SECURITY: diagnostics and wrapped errors may embed the store path. The read path
// reduces *os.PathError paths to their base name (basePathErr) for this reason,
// matching the write path. Before routing Writer to any non-local sink, scrub
// absolute paths and avoid echoing raw error strings.
//
// The contract binds RAW PATH STRINGS too, not only *os.PathError: a diagnostic that
// formats a path it holds as a plain string reduces it to its base name explicitly
// (withLock's lock path, lock.go:69-72; the root-resolution warnings that render a
// manifest-recorded root, root.go).
//
// The "paths are repo-relative today" assumption this note used to rest on NO LONGER
// HOLDS. Since Plan 35.13 T6 the store dir is DefaultDir(ResolveStoreRoot(...)), and
// both the explicit and manifest tiers resolve to ABSOLUTE paths — so a leaked path
// can now contain a username (~/…) in ordinary operation, not only hypothetically.
// The diagnostics that format a raw `path` string directly (the malformed-record,
// schema-version and over-long-line warnings here and in streaming.go) are therefore
// handed a base name rather than the path: scanShard and streamSummaryFile reduce it
// once per shard and pass that display name to the decoders, which never see the
// absolute path at all.
type ReadOpts struct {
	Writer io.Writer
}

// diagWriter resolves a diagnostics sink: the caller-supplied writer, or os.Stderr
// when nil or a typed nil of any kind.
func diagWriter(w io.Writer) io.Writer {
	if w == nil || isTypedNil(w) {
		return os.Stderr
	}
	return w
}

// isTypedNil reports whether w is a non-nil interface wrapping a nil value. w == nil
// is false for such a value, yet writing to it fails — a panic for a pointer, func,
// map or slice receiver, and a permanent block for a chan — so diagWriter treats it
// as unset and falls back to os.Stderr, upholding the "never fail in a diagnostics
// path" contract.
//
// EVERY nil-able kind is covered, not just Pointer (TD
// internal/localdebt/store.go:57): the guard used to test reflect.Pointer alone while
// its doc claimed the whole contract, so a nil func- or map-kinded writer walked
// straight past it. The bound is still only what reflect can SEE — a non-nil value
// whose Write panics for its own reasons is a caller bug this cannot catch, and the
// doc no longer implies otherwise.
func isTypedNil(w io.Writer) bool {
	switch rv := reflect.ValueOf(w); rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
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
		return appendLocked(dir, rec)
	})
}

// appendLocked is Append's body without the lock: the caller must already hold
// the store lock (withLock).
func appendLocked(dir string, rec Record) error {
	month, err := monthFromRunID(rec.RunID)
	if err != nil {
		return err
	}
	if err := ensureStoreDir(dir); err != nil {
		return fmt.Errorf("creating localdebt dir: %w", err)
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
}

// appendBatch appends recs under ONE withLock acquisition — the per-RUN
// granularity PersistForReconcile needs (TD internal/localdebt/reconcile.go:204).
// Calling Append per record wraps every record in its own lock cycle — an
// os.MkdirAll, a lock-dir mkdir, an owner-file write, and an os.RemoveAll per
// record, roughly 6 syscalls each — and under contention each of N records can
// independently spin up to lockWait, stalling one reconcile N x 45s instead of
// 45s once. The per-record write atomicity the concurrency contract requires is
// unchanged: one marshaled []byte and one os.Write per record, all issued inside
// the same critical section (withLock is not reentrant, so this must NOT call
// Append). Failures are per-record and non-fatal to the batch — mirroring
// PersistForReconcile's continue-on-error loop — and the written count is
// returned so the caller can distinguish a no-op run from a partial one.
func appendBatch(dir string, recs []Record) (int, error) {
	var errs []error
	written := 0
	lockErr := withLock(dir, "append", func() error {
		for _, rec := range recs {
			if err := appendLocked(dir, rec); err != nil {
				errs = append(errs, err)
				continue
			}
			written++
		}
		return nil
	})
	if lockErr != nil {
		return 0, lockErr
	}
	return written, errors.Join(errs...)
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
	// Diagnostics name the shard by its BASE NAME only. The store dir is now
	// DefaultDir(ResolveStoreRoot(...)) and both the explicit and manifest tiers
	// resolve to absolute paths, so formatting `path` verbatim prints a
	// username-bearing path into a sink the MCP path routes to server stderr
	// (TD internal/localdebt/store.go:203). The base name is what a human needs
	// anyway — WHICH shard is damaged.
	shard := filepath.Base(path)

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
			_, _ = fmt.Fprintf(w, msgOverLongLine, maxLineBytes, shard)
			if derr := drainLine(br); derr != nil {
				if derr == io.EOF {
					break
				}
				return recs, unrepresentable, fmt.Errorf("reading localdebt file: %w", derr)
			}
			continue
		}
		if line := bytes.TrimSpace(frag); len(line) > 0 {
			switch r, outcome := decodeRecord(line, shard, w); outcome {
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
//
// shard is a DISPLAY name, already reduced to its base name by the caller — it is
// only ever formatted into a warning, and the store path is absolute in ordinary
// operation (see the SECURITY note on ReadOpts).
func decodeRecord(line []byte, shard string, w io.Writer) (Record, decodeOutcome) {
	var r Record
	if err := json.Unmarshal(line, &r); err != nil {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: %v\n", shard, err)
		return Record{}, decodeMalformed
	}
	// Schema-version negotiation: a record from a newer, forward-incompatible schema
	// must not be unmarshaled into this struct and treated as current — a field
	// rename/semantic change would silently corrupt the backlog. Warn and skip.
	if r.SchemaVersion > SchemaVersion {
		_, _ = fmt.Fprintf(w, "localdebt: skipping record with unsupported schema_version %d (> %d) in %s\n", r.SchemaVersion, SchemaVersion, shard)
		return Record{}, decodeForwardIncompatible
	}
	if r.RunID == "" || r.ID == "" {
		_, _ = fmt.Fprintf(w, "localdebt: "+MsgMalformedSkip+" in %s: missing required field (run_id or id)\n", shard)
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
	// shards[i] is the PHYSICAL file records[i] was read from, e.g. "2026-06.jsonl".
	//
	// It is tracked rather than derived because the two can disagree. Everything
	// about where a record may be written follows from its run_id, but everything
	// about whether its current copy is safe follows from the file it is actually
	// in — and a hand-edited store, an external writer, or a rename puts a record
	// in a file its run_id does not name. Judging protection by the derived month
	// is what let Compact write a second copy of such a record while the original
	// survived in a shard that is never rewritten (TD internal/localdebt/store.go:774).
	shards []string
	// preserved maps a month shard to the forward-incompatible raw lines read from
	// it, in file order, to be written back into that same shard.
	preserved map[string][][]byte
	// frozen is the set of physical shard FILES Compact must neither rewrite nor
	// remove: a month shard holding a line this reader could not represent
	// (over-long), and every non-month .jsonl file, which Compact never rewrites by
	// construction (TD internal/localdebt/store.go:298).
	frozen map[string]bool
}

// frozenIDs is the set of finding ids with at least one record sitting in a frozen
// shard. Those ids are not compactable: their frozen copies stay on disk verbatim,
// so folding them elsewhere would either duplicate a record (the fold written beside
// a surviving original) or strand the aggregate counters, which live only on the
// folded record. Their remaining records are instead written back unchanged and the
// next read recomputes the identical fold.
func (s shardRead) frozenIDs() map[string]bool {
	if len(s.frozen) == 0 {
		return nil
	}
	ids := map[string]bool{}
	for i, r := range s.records {
		if s.frozen[s.shards[i]] {
			ids[r.ID] = true
		}
	}
	return ids
}

// planCompaction is what a compaction run would write: the retained fold over every
// compactable record, plus the untouched records of any id anchored to a frozen
// shard. Records that are THEMSELVES in a frozen shard are never emitted — they are
// already on disk and stay there.
//
// Splitting the fold this way is what keeps the two halves of the store consistent.
// A frozen id's history is preserved in full (the price of an unrewritable shard,
// paid until the offending line leaves the store), while every other id folds to at
// most two records as usual.
// compactionCounts reports what a run reads, leaves behind, and drops.
//
// RecordsAfter counts what is ON DISK when the run finishes, which is the planned
// writes PLUS the records sitting in frozen shards — those are never written by this
// run precisely because they are already there. Counting only the writes would report
// every frozen record as Dropped, i.e. as data this run removed, which is the one
// thing compaction must never be wrong about.
func compactionCounts(read shardRead, planned []Record) (before, after, dropped int) {
	before = len(read.records)
	after = len(planned)
	for i := range read.records {
		if read.frozen[read.shards[i]] {
			after++
		}
	}
	return before, after, before - after
}

func planCompaction(read shardRead) []Record {
	frozen := read.frozenIDs()
	if len(frozen) == 0 {
		return retainForCompaction(read.records)
	}
	var compactable, carried []Record
	for i, r := range read.records {
		switch {
		case read.frozen[read.shards[i]]:
			// Already on disk in a file this run will not touch.
		case frozen[r.ID]:
			carried = append(carried, r)
		default:
			compactable = append(compactable, r)
		}
	}
	return append(retainForCompaction(compactable), carried...)
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
// A non-month .jsonl file is read for records (matching ReadAll) and marked FROZEN:
// Compact never rewrites or removes such a file, so re-emitting its records into
// their run_id's month shard left them on disk twice — read from one file, written
// to another, with neither copy ever reconciled (TD internal/localdebt/store.go:298).
// Reading them still matters — the fold must account for them — so they are read and
// then held in place rather than ignored outright.
func readAllPreserving(dir string, opts ReadOpts) (shardRead, error) {
	out := shardRead{
		preserved: map[string][][]byte{},
		frozen:    map[string]bool{},
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
		name := e.Name()
		month := strings.TrimSuffix(name, ".jsonl")
		isMonthShard := monthRe.MatchString(month)
		var collect func([]byte)
		if isMonthShard {
			collect = func(line []byte) { out.preserved[month] = append(out.preserved[month], line) }
		} else {
			out.frozen[name] = true
		}

		recs, unrepresentable, err := scanShard(filepath.Join(dir, name), opts, collect)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return out, basePathErr(err)
		}
		if unrepresentable {
			out.frozen[name] = true
		}
		for _, r := range recs {
			out.records = append(out.records, r)
			out.shards = append(out.shards, name)
		}
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
//
// # Aggregate counters
//
// The winning record is returned with Occurrences and FirstSeen stamped from the
// WHOLE id group, so the regression signal that re-openable resolution creates
// survives compaction at O(1) size instead of O(history). See aggregateCounters
// for the rule and why it is idempotent.
func FoldRecords(recs []Record) []Record {
	folded, byID := foldByID(recs)
	for i := range folded {
		aggregateCounters(&folded[i], byID[folded[i].ID])
	}
	return folded
}

// aggregateCounters stamps eff with the id group's Occurrences and FirstSeen.
//
// It lives here rather than inside foldByID deliberately: Summary (streaming.go)
// instantiates the same generic and carries neither field, so a generic
// aggregation would not compile — and a summary has no use for the count.
//
// # The counting rule
//
//	carrier  := the record with the highest Occurrences (the last fold's
//	            aggregate), or none when no record carries a count
//	carried  := carrier.Occurrences
//	boundary := carrier.CountedThrough (its own Timestamp when absent)
//	fresh    := sightings with no count of their own and a Timestamp strictly
//	            after the boundary. Every sighting counts when there is no carrier.
//	Occurrences    = max(carried+fresh, 1)
//	CountedThrough = the newest sighting timestamp now accounted for
//
// A record is a SIGHTING (isSighting) rather than a status marker in two cases:
// it carries no Status at all — what cli/reconcile.go's persistence hook and a
// plain `atcr debt add` write — or it is a hand-filed item, which is a sighting
// even when it carries a status like `deferred`. Only sightings are occurrences of
// the finding; a resolution is a marker on one.
//
// # Why the boundary is persisted, not inferred
//
// The carrier already counts every sighting up to its boundary, INCLUDING itself,
// so those must not be counted again. Two cheaper-looking tests both fail, and both
// fail the same way — they infer "already counted" from a record's SHAPE, when the
// real question is what a previous fold actually accounted for:
//
//   - "does it still carry a zero count?" — false for any sighting that SURVIVED
//     compaction. Compact deliberately leaves records on disk in two cases: a shard
//     holding a line longer than maxLineBytes is left whole rather than rewritten,
//     and a non-month .jsonl file is read but never rewritten. Those survivors stay
//     zero-count forever, so this test re-counts them on every fold.
//   - "is it older than the carrier's own Timestamp?" — false whenever the carrier
//     is not the group's newest record, which is exactly what rule 1 produces: a
//     suppressing (wontfix) record wins unconditionally, so a survivor NEWER than it
//     is re-counted on every fold.
//
// Either way Occurrences climbs by one per compaction, without bound, silently, and
// unattended now that compaction is automatic. CountedThrough records the boundary
// as a FACT the previous fold wrote down, so neither survivorship nor fold
// precedence can move it.
//
// The comparison is strict (>), so a sighting appended in the very same second as
// the boundary is not counted. That costs at most one occurrence in a case the
// reconcile hook cannot produce anyway (it dedups by id within a run), and it is
// the safe direction: undercounting once beats drifting upward forever.
//
// Walk the lifecycle (ct = CountedThrough):
//
//	[open(0)@t1]                             no carrier, fresh 1        -> 1, ct=t1
//	resolve   [open(0)@t1, resolved(0)@t2]   no carrier, fresh 1        -> 1, ct=t1
//	                                         (the resolution is appended with its
//	                                         counters zeroed — cli/debt_resolve.go)
//	compact   [resolved(1,ct=t1)@t2]         carried 1, fresh 0         -> 1, ct=t1
//	regress   [resolved(1,ct=t1), open(0)@t3] carried 1, fresh 1 (t3>t1) -> 2, ct=t3
//	compact   [open(2,ct=t3)@t3, resolved(0)] carried 2, fresh 0         -> 2, ct=t3
//	                                         (the retained trail is zeroed too)
//	compact   (unchanged)                    carried 2, fresh 0         -> 2  idempotent
//
// And the rule-1 case the boundary exists for:
//
//	[wontfix(0)@t1, open(0)@t2 in an unrewritable shard]
//	                                         no carrier, fresh 1        -> 1, ct=t2
//	compact   wontfix wins rule 1 and carries the count; open@t2 survives on disk
//	refold    carried 1, boundary t2, fresh 0 (t2 is not > t2)          -> 1  stable
//
// Regression count is Occurrences-1. The floor of 1 covers a group whose only
// record carries a status and no count.
//
// FirstSeen is the earliest sighting across the group: each record contributes its
// own FirstSeen when it carries one (a compacted carrier does) and otherwise its
// Timestamp. Empty values are skipped rather than compared, because "" sorts before
// every RFC3339 value and a naive minimum would return it and lose the real first
// sighting.
func aggregateCounters(eff *Record, group []Record) {
	// The carrier is the record holding the highest count, the latest one winning a
	// tie. Records with no count are not carriers.
	var carrier *Record
	for i := range group {
		r := &group[i]
		switch {
		case r.Occurrences == 0:
		case carrier == nil,
			r.Occurrences > carrier.Occurrences,
			r.Occurrences == carrier.Occurrences && r.Timestamp > carrier.Timestamp:
			carrier = r
		}
	}
	carried, boundary := 0, ""
	if carrier != nil {
		carried = carrier.Occurrences
		// A carrier written before CountedThrough existed falls back to its own
		// Timestamp, which is correct for it: the fold that stamped it always
		// selected the group's newest record under rule 2.
		if boundary = carrier.CountedThrough; boundary == "" {
			boundary = carrier.Timestamp
		}
	}

	fresh, first, counted := 0, "", boundary
	for _, r := range group {
		if isSighting(r) {
			if r.Occurrences == 0 && (carried == 0 || r.Timestamp > boundary) {
				fresh++
			}
			if r.Timestamp > counted {
				counted = r.Timestamp
			}
		}
		seen := r.FirstSeen
		if seen == "" {
			seen = r.Timestamp
		}
		if seen == "" {
			continue
		}
		if first == "" {
			first = seen
			continue
		}
		first = earlierTimestamp(first, seen)
	}

	occ := carried + fresh
	if occ < 1 {
		occ = 1
	}
	eff.Occurrences = occ
	eff.FirstSeen = first
	eff.CountedThrough = counted
}

// isSighting reports whether a record is an OCCURRENCE of the finding rather than
// a status marker placed on one.
//
// A record with no Status is always a sighting: that is what the reconcile
// persistence hook writes for a detection, and what `atcr debt add` writes when no
// --status is given.
//
// A hand-filed item is a sighting too, even when it carries a status. Filing
// something straight to `deferred` is still a human saying "this exists here" —
// treating it as a mere marker would leave the item contributing nothing to carry
// forward, so its first genuine re-detection would report zero regressions.
// Manual origin alone is not enough to identify one, because cli/debt_resolve.go
// copies the folded record wholesale and so inherits Origin from a manually filed
// item; a resolution always stamps ResolvedAt, and a filing never does, which
// separates the two exactly.
func isSighting(r Record) bool {
	if r.Status == "" {
		return true
	}
	return r.EffectiveOrigin() == OriginManual && strings.TrimSpace(r.ResolvedAt) == ""
}

// earlierTimestamp returns whichever of two non-empty timestamps is earlier.
//
// Both are parsed as RFC3339 and compared as instants when they parse. Every writer
// in this repo emits UTC RFC3339, so the lexical comparison FoldRecords uses for
// precedence is sound — but FirstSeen is durable, carried forward across every
// future compaction, so a single hand-edited or third-party record with an offset
// would silently invert it forever. Parsing costs nothing at fold scale and makes
// that impossible. A value that does not parse falls back to the byte comparison,
// which is what the rest of the package does.
func earlierTimestamp(a, b string) string {
	ta, aerr := time.Parse(time.RFC3339, a)
	tb, berr := time.Parse(time.RFC3339, b)
	if aerr == nil && berr == nil {
		if tb.Before(ta) {
			return b
		}
		return a
	}
	if b < a {
		return b
	}
	return a
}

// foldable is the minimum a value must expose to take part in the fold: its
// identity, its status, and its timestamp. It exists so FoldRecords and
// FoldSummaries (streaming.go) are two instantiations of ONE precedence rule.
//
// Duplicating the rule instead was explicitly rejected: cli/debt_resolve.go states
// the invariant that resolve, Compact and AggregateQualitySignal "share ONE
// precedence rule", and a second copy would silently diverge the next time the
// status semantics change — with each copy's own tests still green, because each
// would be testing its own behavior.
//
// Record-specific aggregation (Occurrences/FirstSeen) must NOT move in here:
// Summary carries no such fields, so it belongs in FoldRecords wrapping this call.
type foldable interface {
	foldID() string
	foldStatus() string
	foldTimestamp() string
}

func (r Record) foldID() string        { return r.ID }
func (r Record) foldStatus() string    { return r.Status }
func (r Record) foldTimestamp() string { return r.Timestamp }

// foldByID is FoldRecords' precedence rule, generic over anything foldable. See
// FoldRecords for the rule itself and the reasoning behind it. Insertion order is
// preserved: an id appears in the output at the position of its FIRST occurrence
// in the input, so the fold is deterministic for a given read order.
//
// It returns the per-id groups alongside the fold so a caller that needs them —
// FoldRecords, for the counter aggregation — does not rebuild an identical map on
// the package's hottest read path.
func foldByID[T foldable](items []T) ([]T, map[string][]T) {
	order := []string{}
	seen := map[string]bool{}

	byID := map[string][]T{}
	for _, it := range items {
		id := it.foldID()
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
		byID[id] = append(byID[id], it)
	}

	var folded []T
	for _, id := range order {
		group := byID[id]

		// Rule 1: a suppressing record wins unconditionally. Among several, rank
		// then recency decides — preserving the read-order independence divergent
		// terminal records already relied on.
		var suppressing []T
		for _, it := range group {
			if IsSuppressingStatus(it.foldStatus()) {
				suppressing = append(suppressing, it)
			}
		}
		if len(suppressing) > 0 {
			folded = append(folded, latestItem(suppressing))
			continue
		}

		// Rule 2: recency across open and non-suppressing-terminal records alike,
		// so a re-detection newer than a resolution re-opens the id.
		if len(group) > 0 {
			folded = append(folded, latestItem(group))
		}
	}
	return folded, byID
}

// retainForCompaction is what compaction folds to: the effective record for each
// id, PLUS that id's highest-ranked terminal record whenever the effective record
// is open.
//
// The second half exists because resolution became re-openable. Before that a
// terminal record always won its fold group, so compaction could keep the fold
// alone and lose nothing. Now a regressed id folds to the newer OPEN record, and
// keeping only that would permanently delete the superseded resolution — along
// with its ResolvedAt and, decisively, the Justification holding the human-typed
// `--resolve --reason` text, which exists nowhere else in the tree. Compaction is
// meant to bound growth by dropping SUPERSEDED occurrences, not to erase the
// record that a human once closed this finding and why.
//
// Retention is bounded at two records per id, so growth stays O(live findings):
// the effective record, and at most one terminal record. It is also fold-stable
// and idempotent — FoldRecords over {open(t3), resolved(t2)} still selects
// open(t3), so every reader sees exactly what it saw before compaction, and a
// second Compact retains the same pair.
//
// A suppressing (wontfix) id never reaches the second half: rule 1 makes the
// wontfix record itself the effective one, so its justification is retained by
// the first half.
func retainForCompaction(recs []Record) []Record {
	byID := map[string][]Record{}
	for _, r := range recs {
		byID[r.ID] = append(byID[r.ID], r)
	}

	effective := FoldRecords(recs)
	out := make([]Record, 0, len(effective))
	for _, eff := range effective {
		if IsSettledStatus(eff.Status) {
			// The effective record IS the resolution, so there is no rationale to
			// preserve — but there may still be ATTRIBUTION. AggregateQualitySignal
			// recovers a missing Model from an earlier same-id terminal record
			// (qualitysignal.go, foldTerminalByID's donor index), which is how a
			// wontfix that
			// outranks an earlier attributed resolution still reports a dismissal.
			// Dropping that donor makes the whole outcome vanish from the signal —
			// silently and permanently, and now unattended, since compaction runs
			// automatically inside the same reconcile that emits the signal.
			//
			// ORDER IS LOAD-BEARING: the donor is emitted BEFORE the effective
			// record. Both are terminal, so they can tie on both timestamp and
			// ClosedStatusRank, and latestItem breaks a full tie by append order —
			// last wins. Emitting the donor last would hand it the fold, silently
			// swapping which record readers see as effective and, on the NEXT
			// compaction, deleting the displaced one along with the human-typed
			// --reason only it carried. Writing it first keeps eff the winner and
			// compaction fold-stable.
			if donor := modelDonor(byID[eff.ID], eff); donor != nil {
				out = append(out, *donor)
			}
			out = append(out, eff)
			continue
		}
		out = append(out, eff)
		// SETTLED, not merely closed, on both sides. An effective `deferred` record
		// is not a resolution — it carries no justification, and treating it as one
		// would discard an earlier `resolved` record and the --reason text only that
		// record holds. Selecting only settled candidates also excludes the effective
		// record from its own trail without an identity comparison, since a record
		// that reached this line is by definition not settled.
		var resolutions []Record
		for _, r := range byID[eff.ID] {
			if IsSettledStatus(r.Status) {
				resolutions = append(resolutions, r)
			}
		}
		if len(resolutions) > 0 {
			trail := highestRankedTerminal(resolutions)
			// The retained record is a TRAIL entry, not an occurrence: the id's
			// aggregate counters live solely on the effective record. Zeroing them
			// keeps compaction idempotent — a carrier accounts for every detection up
			// to its own timestamp, so a second carrier in the same group would let
			// the fold count part of that history twice (aggregateCounters).
			//
			// Zeroing FirstSeen is safe because the aggregation skips empty values
			// rather than comparing them: "" sorts before every RFC3339 value, so a
			// naive minimum across the retained pair would return it and lose the
			// first sighting. The effective record still carries the real value.
			trail.Occurrences = 0
			trail.FirstSeen = ""
			trail.CountedThrough = ""
			out = append(out, trail)
		}
	}
	return out
}

// modelDonor returns the terminal record whose Model AggregateQualitySignal would
// recover for this id, or nil when the effective record already carries one or no
// donor exists.
//
// Selection matches foldTerminalByID's donor index (qualitysignal.go) exactly —
// the most recent terminal record with a non-empty Model, last-wins on ties —
// because the point is that the recovery returns the SAME model before and after
// compaction. A different rule here would make the signal depend on whether the
// store had been compacted yet.
//
// Retention stays bounded at two records per id: this branch is reached only when
// the effective record is settled, which is exactly the branch that retains no
// resolution trail.
//
// Settled — not merely closed — is the right gate, and not by luck. An id whose
// effective record is OPEN is filtered out of the quality signal by
// foldTerminalByID (an unsettled finding is not an outcome). A `deferred` one is
// admitted by that filter and even has its Model recovered, but
// AggregateQualitySignal's status switch maps anything that is neither wontfix nor
// resolved to no counter and no group, so it emits no row whatever its attribution.
// Only a settled effective record can produce a row, so only it can lose one.
func modelDonor(group []Record, eff Record) *Record {
	if strings.TrimSpace(eff.Model) != "" {
		return nil
	}
	var best *Record
	for i := range group {
		r := group[i]
		if !IsClosedStatus(r.Status) || strings.TrimSpace(r.Model) == "" {
			continue
		}
		if best == nil || r.Timestamp >= best.Timestamp {
			// The donor is a TRAIL entry, not an occurrence — same rule as the
			// resolution trail below: an id's aggregate counters live solely on its
			// effective record, or a second carrier would let the fold count part of
			// the history twice.
			r.Occurrences = 0
			r.FirstSeen = ""
			r.CountedThrough = ""
			donor := r
			best = &donor
		}
	}
	return best
}

// highestRankedTerminal picks which terminal record to retain for an id whose
// effective record is open: rank first, recency only as a tiebreak — the inverse
// of latestRecord's ordering, and deliberately so. The point of retention is the
// resolution trail, and a `resolved` record can carry a human-typed --reason
// while a `deferred` one cannot (nothing writes a justification with it), so a
// later deferral must not displace an earlier resolution and its rationale.
func highestRankedTerminal(terminals []Record) Record {
	best := terminals[0]
	for _, r := range terminals[1:] {
		switch {
		case ClosedStatusRank(r.Status) > ClosedStatusRank(best.Status):
			best = r
		case ClosedStatusRank(r.Status) == ClosedStatusRank(best.Status) && r.Timestamp >= best.Timestamp:
			best = r
		}
	}
	return best
}

// latestItem picks the effective item from a non-empty fold group: the latest
// timestamp wins; an equal timestamp is broken by ClosedStatusRank so a terminal
// record outranks an open one (rank 0), and a full tie by append order (the last
// wins). Recency first, rank only as a tiebreak — the inverse of
// highestRankedTerminal.
//
// Timestamps are compared as strings; see FoldRecords' "Timestamp comparison"
// section for why that is sound and what breaks it.
func latestItem[T foldable](group []T) T {
	best := group[0]
	for _, it := range group[1:] {
		switch {
		case it.foldTimestamp() > best.foldTimestamp():
			best = it
		case it.foldTimestamp() == best.foldTimestamp() && ClosedStatusRank(it.foldStatus()) > ClosedStatusRank(best.foldStatus()):
			best = it
		case it.foldTimestamp() == best.foldTimestamp() && ClosedStatusRank(it.foldStatus()) == ClosedStatusRank(best.foldStatus()):
			best = it // full tie: append order, last wins
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
// from a no-op.
//
// StoreFound answers ONE question — is there a store here — and is true whenever
// the directory holds at least one shard file, whatever this binary could make of
// its contents. It used to be false for a store that existed and held records
// (all newer-schema, or all malformed), which forced every caller to special-case
// the other counts to avoid printing "no store" over existing records. Whether
// anything was FOLDABLE is RecordsBefore's question, and what could not be
// decoded is Preserved's; an empty directory holds no records either way, so it
// is not found.
//
// RecordsBefore is the total records read;
// RecordsAfter is the number of records the store holds afterwards — the folded
// (live) count, plus the full history of any id anchored to a shard this run could
// not rewrite, whether that history was written back or simply left in the frozen
// shard it already occupied (planCompaction, compactionCounts); Dropped is
// RecordsBefore-RecordsAfter, the superseded records removed. Preserved counts forward-incompatible lines
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

// hasShardFiles reports whether dir holds at least one .jsonl shard. It answers
// CompactResult.StoreFound, so it deliberately does not parse anything: a shard
// this binary cannot decode is still a store, and reporting otherwise is what let
// a caller print "No local TD store" over records sitting on disk. A missing or
// unreadable directory is simply not a store.
func hasShardFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			return true
		}
	}
	return false
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
// A fourth case follows from the third: an id with ANY record inside such a shard is
// not compacted at all. Writing the fold would drop the aggregate counters that live
// only on the folded record, or leave that fold beside a surviving original, so every
// record of that id is instead left exactly where it is — written back unchanged when
// it lives in a rewritable shard, untouched when it lives in the frozen one — and the
// next read recomputes the identical fold (planCompaction). Such an id retains its
// full history, not the usual at-most-two records, until the oversized line leaves
// the store.
//
// Membership in that fourth case is decided by the PHYSICAL file a record was read
// from, never by the month derived from its run_id. The two disagree for a
// hand-edited store, an external writer, or a rename, and judging by the derived
// month is what wrote a second copy of such a record while the original survived
// (TD internal/localdebt/store.go:774). A non-month .jsonl file is frozen for the
// same reason: Compact never rewrites one, so re-emitting its records into their
// run_id month shard left them on disk twice (TD internal/localdebt/store.go:298).
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
// CompactPlan reports what Compact WOULD do, writing nothing. The fold is pure —
// it is a read, a group-by-id, and a count — so the preview is the same
// computation the real run performs, not a re-derivation that could disagree with
// it. `atcr debt compact --dry-run` is the caller: compaction is the only
// irreversible operation in the debt namespace and its multi-shard rewrite is
// acknowledged non-atomic, so a user needs a way to see what a run would drop
// before it drops it.
//
// It takes the same lock as Compact, so the counts cannot be torn by a concurrent
// append.
func CompactPlan(dir string, opts ReadOpts) (CompactResult, error) {
	var result CompactResult
	storeFound := hasShardFiles(dir)
	err := withLock(dir, "compact-plan", func() error {
		result.StoreFound = storeFound
		read, err := readAllPreserving(dir, opts)
		if err != nil {
			return err
		}
		for _, lines := range read.preserved {
			result.Preserved += len(lines)
		}
		if len(read.records) == 0 {
			return nil
		}
		result.RecordsBefore, result.RecordsAfter, result.Dropped = compactionCounts(read, planCompaction(read))
		return nil
	})
	if err != nil {
		return CompactResult{}, err
	}
	return result, nil
}

// stagedShard is a fully-written replacement for one month shard, sitting in a temp
// file beside it and not yet visible to any reader.
//
// Staging is separated from publishing so a multi-shard rewrite writes EVERY shard
// before it replaces ANY (TD internal/localdebt/store.go:316). Interleaving the two
// meant a failure partway through — ENOSPC, EACCES, a destination that cannot be
// replaced — left some shards rewritten and others not, with the trailing
// empty-shard removal never running. Cross-shard atomicity is not reachable without
// a journal, but the exposure shrinks from "the whole rewrite" to "the rename
// sequence": any failure before the first publish leaves the store untouched.
type stagedShard struct {
	tmpName string
	path    string
}

// publish swaps the staged content in. After it returns nil the temp no longer
// exists and discard must not be called for this shard.
func (s stagedShard) publish() error {
	if err := os.Rename(s.tmpName, s.path); err != nil {
		return fmt.Errorf("renaming compacted file: %w", basePathErr(err))
	}
	return nil
}

// discard removes an unpublished temp. Best-effort: a leftover temp is inert to
// every read path (it carries no .jsonl suffix) and the next Compact's
// sweepStaleTemps reaps it.
func (s stagedShard) discard() {
	if s.tmpName != "" {
		_ = os.Remove(s.tmpName)
	}
}

// stageShard writes month's replacement content — the folded records followed by the
// forward-incompatible lines carried through verbatim — to a temp file beside the
// destination, and returns it unpublished.
//
// It is a function rather than an inlined loop body so its file handle and its
// failure paths are scoped to ONE shard: the previous inline version deferred its
// cleanup to the end of the whole compaction closure, so every shard's temp handle
// accumulated until the last one finished.
func stageShard(dir, month string, recs []Record, preserved [][]byte) (stagedShard, error) {
	var buf bytes.Buffer
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			return stagedShard{}, fmt.Errorf("marshaling localdebt record: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	// Carry forward-incompatible lines through byte-for-byte. They are never
	// re-marshaled: this binary's Record cannot round-trip fields it does not
	// declare, so marshaling a decoded copy is precisely how the data would be lost.
	for _, line := range preserved {
		buf.Write(line)
		buf.WriteByte('\n')
	}

	tmp, err := os.CreateTemp(dir, "."+month+".jsonl.tmp-*")
	if err != nil {
		return stagedShard{}, fmt.Errorf("creating temp file for compaction: %w", basePathErr(err))
	}
	staged := stagedShard{tmpName: tmp.Name(), path: filepath.Join(dir, month+".jsonl")}

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		staged.discard()
		return stagedShard{}, fmt.Errorf("writing compacted records: %w", basePathErr(err))
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		staged.discard()
		return stagedShard{}, fmt.Errorf("setting compacted file permissions: %w", basePathErr(err))
	}
	if err := tmp.Close(); err != nil {
		staged.discard()
		return stagedShard{}, fmt.Errorf("closing compacted temp file: %w", basePathErr(err))
	}
	return staged, nil
}

func Compact(dir string, opts ReadOpts) (CompactResult, error) {
	var result CompactResult
	// Presence is sampled BEFORE the lock: withLock MkdirAlls the store directory,
	// so inside it every store exists and StoreFound could only ever mean
	// "something was foldable" — the ambiguity this contract had.
	storeFound := hasShardFiles(dir)
	err := withLock(dir, "compact", func() error {
		result.StoreFound = storeFound
		sweepStaleTemps(dir)
		read, err := readAllPreserving(dir, opts)
		if err != nil {
			return err
		}
		recs, preservedByShard := read.records, read.preserved
		// Frozen is keyed by physical FILE; the destination checks below ask about a
		// MONTH, so translate once here rather than at each use.
		frozenMonth := func(month string) bool { return read.frozen[month+".jsonl"] }
		preservedCount := 0
		for _, lines := range preservedByShard {
			preservedCount += len(lines)
		}
		if len(recs) == 0 {
			// Nothing foldable. Report any preserved lines so the caller can explain
			// the no-op, but touch no file: rewriting from an empty fold is exactly
			// the destruction this guard exists to prevent. RecordsBefore stays 0 —
			// that, not StoreFound, is what says "nothing was folded".
			result.Preserved = preservedCount
			return nil
		}

		planned := planCompaction(read)
		result = CompactResult{
			StoreFound: storeFound,
			Preserved:  preservedCount,
		}
		result.RecordsBefore, result.RecordsAfter, result.Dropped = compactionCounts(read, planned)

		byMonth := map[string][]Record{}
		for _, r := range planned {
			month, err := monthFromRunID(r.RunID)
			if err != nil {
				return err
			}
			if frozenMonth(month) {
				// The shard is left whole, superseded records and all. Writing into it
				// is the one thing that must not happen — it is not rewritten, so a
				// record placed here would be a second copy beside a surviving original.
				continue
			}
			byMonth[month] = append(byMonth[month], r)
		}
		// A shard with only preserved lines still has to be rewritten (rather than
		// skipped and later removed), so seed it with an empty record set.
		for month := range preservedByShard {
			if frozenMonth(month) {
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
				// A frozen shard is never a removal candidate: it is excluded here
				// rather than deleted from the set later, so no path can reach os.Remove
				// for it.
				if monthRe.MatchString(month) && !frozenMonth(month) {
					existingMonths[month] = true
				}
			}
		}

		// Phase 1: stage every shard. Nothing on disk changes yet, so a failure here
		// leaves the store exactly as it was.
		staged := make([]stagedShard, 0, len(byMonth))
		months := make([]string, 0, len(byMonth))
		for month, monthRecs := range byMonth {
			s, err := stageShard(dir, month, monthRecs, preservedByShard[month])
			if err != nil {
				for _, done := range staged {
					done.discard()
				}
				return err
			}
			staged = append(staged, s)
			months = append(months, month)
		}

		// Phase 2: publish. A failure here can still leave a partial rewrite — that
		// is irreducible without a journal — but every shard's content is already on
		// disk, so the window is a sequence of renames rather than the whole rewrite.
		for i, s := range staged {
			if err := s.publish(); err != nil {
				for _, rest := range staged[i:] {
					rest.discard()
				}
				return err
			}
			delete(existingMonths, months[i])
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
	if err != nil {
		// Report nothing. The counts describe a fold that was planned, not one that
		// landed, and a caller reading RecordsBefore/Dropped off a failed run reads
		// them as work that happened (TD internal/localdebt/store.go:316). CompactPlan
		// already zeroes on error; this is the same contract for the real thing.
		return CompactResult{}, err
	}

	// Record what this compaction left behind, so the automatic trigger has a
	// baseline to measure growth against (see MaybeCompact).
	//
	// It lives HERE, not in MaybeCompact, for three reasons. A manual
	// `atcr debt compact` refreshes the baseline too, so it cannot leave a stale
	// watermark that provokes a redundant automatic compaction moments later. A
	// no-op fold (StoreFound false — a store of only malformed or
	// forward-incompatible lines) still establishes a baseline, without which
	// the trigger re-fires on every append forever. And the measurement is taken
	// on the same terms the trigger uses: real on-disk lines and bytes via
	// StoreStats, not the semantic fold count, which excludes lines left in
	// protected shards and would make the two disagree.
	//
	// Best-effort, and measured OUTSIDE the lock: the rewrite has already
	// committed, and the no-op-fold path returns early from the locked closure,
	// so writing here covers both exits with one statement. A concurrent Append
	// between the unlock and the stat INFLATES the recorded baseline, and the
	// growth gate then asks for 50% growth past an inflated number — so the
	// effect is a DELAYED compaction, not a spurious one. It is self-correcting:
	// the next compaction that does run rewrites the baseline accurately.
	if records, size, serr := StoreStats(dir); serr == nil {
		writeCompactWatermark(dir, compactWatermark{Records: records, Bytes: size}, opts.Writer)
	}
	return result, nil
}

// Automatic-compaction thresholds. Compaction is what bounds the store's growth —
// month sharding buys file-size hygiene and archival boundaries but no bound, since
// every operation reads every shard. Leaving compaction manual (`atcr debt compact`)
// means an unattended repo that only ever runs `atcr reconcile` grows without limit.
//
// The values are sized on Plan 35.13 Decision 5: with the minimal-decode streaming
// read (streaming.go) a store at either threshold is a sub-second scan, and one
// compaction returns it to live-findings scale — bounded by real debt, not by
// history.
const (
	DefaultAutoCompactMaxRecords = 100_000
	DefaultAutoCompactMaxBytes   = 100 << 20 // 100 MiB
)

// autoCompactGrowthNum / autoCompactGrowthDen express the growth a store must show
// past its last post-compaction watermark before automatic compaction fires again:
// 3/2, i.e. 50%.
//
// This is the damping the thresholds alone cannot provide. Compact retains up to
// TWO records per id (retainForCompaction: the effective record plus the resolution
// trail when the effective one is open), so a store's post-compaction floor can sit
// above an absolute threshold — and then every single append re-trips it, taking
// the cross-process lock and rewriting every shard to drop nothing, forever. The
// watermark turns "above the threshold" into "above the threshold AND materially
// bigger than last time it was compacted", which is the condition that actually
// predicts there is something to drop.
const (
	autoCompactGrowthNum = 3
	autoCompactGrowthDen = 2
)

// CompactPolicy bounds a store before automatic compaction runs. A zero value means
// the production defaults, so a caller that has no opinion passes CompactPolicy{}.
// Tests shrink it instead of writing 100k records; a separate assertion pins the
// defaults so a shrunken policy cannot mask a wrong production constant.
type CompactPolicy struct {
	MaxRecords int   // <= 0 uses DefaultAutoCompactMaxRecords
	MaxBytes   int64 // <= 0 uses DefaultAutoCompactMaxBytes
}

func (p CompactPolicy) maxRecords() int {
	if p.MaxRecords <= 0 {
		return DefaultAutoCompactMaxRecords
	}
	return p.MaxRecords
}

func (p CompactPolicy) maxBytes() int64 {
	if p.MaxBytes <= 0 {
		return DefaultAutoCompactMaxBytes
	}
	return p.MaxBytes
}

// shardPaths lists the store's *.jsonl shards and their total size on disk, using
// the same filter and order as ReadAll. A missing directory is the "no backlog yet"
// state: no shards, no bytes, no error.
func shardPaths(dir string) ([]string, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("reading localdebt dir: %w", basePathErr(err))
	}
	var paths []string
	var size int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue // shard removed between ReadDir and stat
			}
			return nil, 0, fmt.Errorf("stating localdebt shard: %w", basePathErr(err))
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
		size += info.Size()
	}
	return paths, size, nil
}

// countLines counts '\n' bytes across the given shards with one reusable buffer and
// no decode, so the threshold check never pays for JSON parsing.
func countLines(paths []string) (int, error) {
	buf := make([]byte, 64<<10)
	total := 0
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return total, fmt.Errorf("reading localdebt shard: %w", basePathErr(err))
		}
		for {
			n, rerr := f.Read(buf)
			total += bytes.Count(buf[:n], []byte{'\n'})
			if rerr != nil {
				_ = f.Close()
				if rerr == io.EOF {
					break
				}
				return total, fmt.Errorf("reading localdebt shard: %w", basePathErr(rerr))
			}
		}
	}
	return total, nil
}

// StoreStats reports the store's size: the number of newline-terminated lines
// across every *.jsonl shard, and their total bytes on disk.
//
// This is a SIZE HEURISTIC, not a semantic record count. It counts lines, so
// malformed and forward-incompatible lines are included — which is what an
// automatic-compaction trigger wants, since those bytes are on disk regardless of
// whether this binary can interpret them. It performs no JSON decode.
//
// A missing directory returns (0, 0, nil), matching ReadAll's "no backlog yet".
func StoreStats(dir string) (records int, size int64, err error) {
	paths, size, err := shardPaths(dir)
	if err != nil {
		return 0, 0, err
	}
	records, err = countLines(paths)
	if err != nil {
		return records, size, err
	}
	return records, size, nil
}

// compactWatermarkFile is the store-local record of what the last compaction left
// behind. The name deliberately carries no .jsonl suffix, so ReadAll, Compact's
// shard enumeration, and readAllPreserving all skip it; and it does not match
// sweepStaleTemps' ".jsonl.tmp-" pattern, so crash-debris reaping leaves it alone.
const compactWatermarkFile = ".compact-watermark"

// compactWatermark is the store's size immediately after the last successful
// automatic compaction — the baseline the growth gate measures against.
type compactWatermark struct {
	Records int   `json:"records"`
	Bytes   int64 `json:"bytes"`
}

// readCompactWatermark loads the watermark, degrading to a ZERO watermark on any
// problem — absent, unreadable, truncated, or corrupt.
//
// Degrading to zero is the safe direction, and deliberately so: a zero watermark
// makes the growth gate pass, so the thresholds alone decide and compaction still
// happens. The guard can therefore delay a redundant compaction but can never
// prevent a needed one, which is the only failure mode worth designing for here.
func readCompactWatermark(dir string) compactWatermark {
	var w compactWatermark
	b, err := os.ReadFile(filepath.Join(dir, compactWatermarkFile))
	if err != nil {
		return compactWatermark{}
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return compactWatermark{}
	}
	if w.Records < 0 || w.Bytes < 0 {
		return compactWatermark{}
	}
	return w
}

// writeCompactWatermark records the store's post-compaction size. It is
// best-effort: a failure is reported to the caller's diagnostics sink and never
// fails the compaction that already succeeded, because the only consequence is that
// the next append re-evaluates the thresholds without a baseline.
func writeCompactWatermark(dir string, w compactWatermark, diag io.Writer) {
	b, err := json.Marshal(w)
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, compactWatermarkFile), b, 0o600); err != nil {
		_, _ = fmt.Fprintf(diagWriter(diag), "localdebt: could not record compaction watermark: %v\n", basePathErr(err))
	}
}

// grewPast reports whether a measured dimension has grown at least 50% past its
// watermark. A zero watermark (never compacted, or an unreadable one) always
// passes, so a first-ever compaction fires at the threshold exactly.
//
// The comparison is cross-multiplied rather than dividing the watermark: integer
// division truncates, which at small watermarks demands materially more than 50%
// growth (a watermark of 3 would not fire until 5, i.e. 66%). Irrelevant at the
// production threshold, but every test policy lives in exactly that regime, so a
// test expectation would otherwise hinge on truncation instead of the documented
// rule. int64 operands make overflow unreachable at any real store size.
func grewPast(current, watermark int64) bool {
	if watermark <= 0 {
		return current > 0
	}
	return current*autoCompactGrowthDen > watermark*autoCompactGrowthNum
}

// MaybeCompact compacts dir when the store has outgrown policy, and reports whether
// it did. It is the automatic counterpart to the manual `atcr debt compact`; Compact
// itself is unchanged and does all the rewriting.
//
// Two conditions must both hold:
//
//  1. A THRESHOLD is tripped — policy.MaxRecords lines or policy.MaxBytes bytes,
//     whichever trips first. The byte check comes first and short-circuits the line
//     scan, since it is satisfied by stat alone.
//  2. The store has GROWN at least 50% past the watermark the last compaction left
//     (see autoCompactGrowthNum). Without this, a store whose post-compaction floor
//     already exceeds the threshold re-compacts on every single append forever,
//     dropping nothing each time.
//
// # Locking
//
// MaybeCompact takes NO lock, and the stats read runs outside any lock. Compact
// acquires withLock internally, and withLock is mkdir-based and NOT reentrant — a
// nested acquire would spin for the full lockWait and then fail, turning every
// over-threshold reconcile into a multi-second stall. A benign stats race just means
// compaction happens one run later.
func MaybeCompact(dir string, policy CompactPolicy, opts ReadOpts) (CompactResult, bool, error) {
	paths, size, err := shardPaths(dir)
	if err != nil {
		return CompactResult{}, false, err
	}

	// records stays -1 when the byte threshold trips first and the line scan is
	// skipped; the growth gate then measures the dimension it actually has.
	records := -1
	tripped := size >= policy.maxBytes()
	if !tripped {
		records, err = countLines(paths)
		if err != nil {
			return CompactResult{}, false, err
		}
		tripped = records >= policy.maxRecords()
	}
	if !tripped {
		return CompactResult{}, false, nil
	}

	// Growth is measured in the dimension that actually tripped, not in either —
	// a store can double in bytes while barely moving in records (and vice versa),
	// and unlocking on the untripped dimension would defeat the damping.
	w := readCompactWatermark(dir)
	grew := grewPast(size, w.Bytes)
	if records >= 0 {
		grew = grewPast(int64(records), int64(w.Records))
	}
	if !grew {
		return CompactResult{}, false, nil
	}

	res, err := Compact(dir, opts)
	if err != nil {
		return res, true, err
	}
	return res, true, nil
}
