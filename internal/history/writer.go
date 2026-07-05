package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Append writes one JSON line per record to the ledger at path, creating parent
// directories as needed. The file is opened in append mode so the ledger
// accumulates across runs — an existing ledger is never truncated. An empty
// records slice is a no-op (no file is created).
//
// The whole batch is serialized to memory first, then written with a single
// O_APPEND write() call. On a local POSIX filesystem that append is atomic, so
// two concurrent `atcr review` runs writing the same ledger interleave at batch
// boundaries (never mid-line) rather than corrupting a JSON line.
func Append(path string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range records {
		if err := enc.Encode(records[i]); err != nil {
			return fmt.Errorf("encoding history record: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating history dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening history ledger: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing history ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing history ledger: %w", err)
	}
	return nil
}
