package scorecard

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// maxLineBytes bounds a single JSONL line on read. Records are ~500 bytes; 1 MiB
// is a generous cap that prevents a corrupt/oversized line from allocating an
// unbounded scanner buffer.
const maxLineBytes = 1 << 20

// Append writes one record as a single JSONL line to the month file derived from
// rec.RunID, under dir (created lazily with 0700 on first write). The line is
// marshaled to one []byte (record + '\n') and emitted in a single O_APPEND Write
// syscall: each record is well under PIPE_BUF, so concurrent same-file appends
// are atomic and non-interleaved. No bufio.Writer is shared across records —
// batching could merge writes past PIPE_BUF and tear lines under concurrency
// (sprint-design "Concurrent reconcile runs" risk). The file is 0600.
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
// damaged file still yields its valid records. A missing file surfaces as the
// raw os error so callers can phrase their own "no records" guidance.
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
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		return recs, fmt.Errorf("reading scorecard file: %w", err)
	}
	return recs, nil
}
