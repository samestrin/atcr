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
// Directories are created 0700 and new ledger files 0600, matching
// internal/localdebt: since Epic 35.14 the ledger lives under .atcr/ — local,
// uncommitted state — and a trend ledger names which packages accrue which
// findings, so it is not readable by every local account. "Uncommitted" is not an
// assumption: `atcr init` lists history/ in .atcr/.gitignore (atcrGitignore in
// cli/init.go), which is what keeps these modes meaningful — a committed ledger
// comes back 0644/0755 on a fresh clone, because git carries no mode beyond +x.
// The mode applies only to paths this call creates; an existing ledger keeps
// whatever mode it already has, so a repo that accrued history under the old 0644
// default is untouched.
//
// The whole batch is serialized to memory first, then written in a single
// f.Write call. In practice a small batch is emitted by one O_APPEND write()
// syscall, which the kernel appends atomically, so two concurrent `atcr review`
// runs usually interleave only at batch boundaries. That is not guaranteed,
// though: os.File.Write loops on short writes, so a large buffer can be split
// across several write() syscalls and a concurrent append may land between them,
// tearing a JSONL line. Records are small, so the risk is low in practice; a
// caller needing a hard guarantee must serialize appends with an external file
// lock (intentionally not done here).
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating history dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
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
