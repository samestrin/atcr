package audit

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
// The whole batch is serialized to memory first, then written in a single
// f.Write call. A single review run appends exactly one small record, which the
// kernel emits with one O_APPEND write() syscall and appends atomically, so two
// concurrent `atcr review` runs interleave only at record boundaries. That is
// not a hard guarantee — os.File.Write loops on short writes, so a large buffer
// could split across several syscalls and a concurrent append could land between
// them — but audit records are small (one per run), so the risk is negligible;
// a caller needing a hard guarantee must serialize appends with an external file
// lock (intentionally not done here, matching internal/history).
func Append(path string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range records {
		if err := enc.Encode(records[i]); err != nil {
			return fmt.Errorf("encoding audit record: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating audit dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening audit ledger: %w", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing audit ledger: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing audit ledger: %w", err)
	}
	return nil
}
