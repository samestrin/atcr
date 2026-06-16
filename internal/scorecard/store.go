package scorecard

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxLineBytes bounds a single JSONL line on read. Records are ~500 bytes; 1 MiB
// is a generous cap that prevents a corrupt/oversized line from allocating an
// unbounded scanner buffer.
const maxLineBytes = 1 << 20

// Append writes one record as a single JSONL line to the month file derived from
// rec.RunID, under dir (created lazily with 0700 on first write). The line is
// marshaled to one []byte (record + '\n') and emitted in a single Write to a
// file opened O_APPEND. On Linux/macOS a write() to a regular file opened
// O_APPEND atomically seeks to end-of-file and writes contiguously, so two
// processes appending concurrently never interleave or lose a record — the
// guarantee is the per-write() atomic append for regular files, independent of
// record size (it is NOT the PIPE_BUF bound, which governs pipes/FIFOs). No
// bufio.Writer is shared across records — batching multiple records through one
// buffered flush would coalesce them into a single larger write whose atomicity
// is not guaranteed, tearing lines under concurrency (sprint-design "Concurrent
// reconcile runs" risk). One Write per record preserves the guarantee. The file
// is 0600. (Portability caveat for non-POSIX append semantics: TD-004.)
func Append(dir string, rec Record) error {
	month, err := monthFromRunID(rec.RunID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating scorecard dir: %w", err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshaling scorecard record: %w", err)
	}
	line = append(line, '\n')

	path := filepath.Join(dir, month+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening scorecard file: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("appending scorecard record: %w", err)
	}
	return nil
}

// ReadRecords stream-parses a month JSONL file line-by-line. A malformed line is
// logged and skipped (a corrupt line never aborts the read), so a partially
// damaged file still yields its valid records. A record whose schema_version is
// newer than the current SchemaVersion is also logged and skipped, so a
// forward-incompatible record cannot be misread as the current version and
// silently pollute aggregates. A missing file surfaces as the raw os error so
// callers can phrase their own "no records" guidance.
func ReadRecords(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var recs []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			fmt.Fprintf(os.Stderr, "scorecard: skipping malformed record in %s: %v\n", path, err)
			continue
		}
		// Schema-version negotiation: a record from a newer, forward-incompatible
		// schema must not be unmarshaled into this struct and aggregated as if it
		// were the current version — a field rename/semantic change would silently
		// corrupt totals. Warn and skip rather than pollute aggregates. (Older
		// versions remain readable: v1 is the first schema, so there is nothing to
		// migrate yet; an explicit migration shim slots in here when one appears.)
		if r.SchemaVersion > SchemaVersion {
			fmt.Fprintf(os.Stderr, "scorecard: skipping record with unsupported schema_version %d (> %d) in %s\n", r.SchemaVersion, SchemaVersion, path)
			continue
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		return recs, fmt.Errorf("reading scorecard file: %w", err)
	}
	return recs, nil
}

// FindByRunID returns every record in dir carrying the given run_id, unioned
// across the relevant month files. The primary month is derived from the run_id's
// YYYY-MM prefix (a run_id without a valid prefix is a clear error, not an empty
// result). When the run_id timestamp sits on a month boundary (1st or 28th-31st),
// the neighbouring month file is also scanned and merged, because a clock-skewed
// or late write can split one run's records across two month files (AC 02-01
// EC1) — returning only one file's records would silently drop the rest. A hit
// in a neighbouring file is logged to stderr. A missing month file is "no
// records" for that month (skipped), not an error.
func FindByRunID(dir, runID string) ([]Record, error) {
	month, err := monthFromRunID(runID)
	if err != nil {
		return nil, err
	}
	months := monthsToScan(runID, month)

	var matches []Record
	var fromNeighbour bool
	for i, m := range months {
		recs, err := ReadRecords(filepath.Join(dir, m+".jsonl"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, r := range recs {
			if r.RunID == runID {
				matches = append(matches, r)
				if i > 0 {
					fromNeighbour = true
				}
			}
		}
	}
	if fromNeighbour {
		fmt.Fprintf(os.Stderr, "scorecard: run %s spans adjacent month files (clock skew or late write)\n", runID)
	}
	return matches, nil
}

// monthsToScan returns the month stems FindByRunID must read for runID: always
// the primary month, plus a neighbouring month only when the run_id day is on a
// month boundary (so the common mid-month lookup stays a single-file read while
// a boundary-straddling run is still found whole).
func monthsToScan(runID, month string) []string {
	months := []string{month}
	prev, next, ok := adjacentMonths(month)
	if !ok || len(runID) < 10 {
		return months
	}
	day, err := strconv.Atoi(runID[8:10])
	if err != nil {
		return months
	}
	if day <= 1 {
		months = append(months, prev)
	}
	if day >= 28 {
		months = append(months, next)
	}
	return months
}

// ReadAll reads every *.jsonl month file under dir and returns the concatenated
// records (malformed lines skipped per-file by ReadRecords). A missing directory
// is empty (nil, nil), not an error — the leaderboard's "no data yet" state.
// Non-.jsonl files are ignored.
func ReadAll(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading scorecard dir: %w", err)
	}
	var all []Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		recs, err := ReadRecords(filepath.Join(dir, e.Name()))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return all, err
		}
		all = append(all, recs...)
	}
	return all, nil
}

// adjacentMonths returns the YYYY-MM stems on either side of month. ok is false
// for an unparseable month (the caller then scans the primary month only).
func adjacentMonths(month string) (prev, next string, ok bool) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", false
	}
	return t.AddDate(0, -1, 0).Format("2006-01"), t.AddDate(0, 1, 0).Format("2006-01"), true
}
