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
// Both modes apply only to paths this call CREATES, and that holds for the
// directory as much as the file. os.MkdirAll returns nil without touching a
// directory that already exists, so a shard dir left at 0755 by something else
// stays 0755 for good — Append never tightens it, exactly as localdebt's
// ensureStoreDir never tightens its own store dir. An existing ledger likewise
// keeps its mode, so a repo that accrued history under the old 0644 default is
// untouched.
//
// This is why docs/history.md's migration recipe uses `install -d -m 700` and
// `install -m 600` rather than mkdir/cp: a hand-migrated ledger is created by the
// operator, not by this function, so nothing here will ever correct it.
//
// The whole batch is serialized to memory first, then written in a single
// f.Write call. The unit that matters for tearing is therefore the BATCH, not the
// record: the loop below encodes one line per finding for the entire run into one
// buffer, so a wide review submits hundreds of records — 100KB and up — in one
// call. Do not reason about this as a small write.
//
// A single O_APPEND write() to a regular file on a local filesystem is appended
// atomically, so two concurrent `atcr review` runs interleave only at batch
// boundaries. Two things narrow that guarantee. First, os.File.Write loops on
// short writes, so a buffer this size can be split across several write() calls
// and a concurrent append may land between them, tearing a JSONL line. Second,
// the guarantee is local-filesystem-only: on NFS or SMB, O_APPEND is emulated
// client-side and does not hold at all, whatever the buffer size.
//
// No external lock is taken here, and that is a deliberate trade rather than an
// oversight. internal/localdebt does ship one (withLock in lock.go, mkdir-based),
// but it guards read-modify-write over a whole file, where a lost update destroys
// committed data. This ledger is append-only and read tolerantly — reader.go skips
// a blank or malformed line instead of failing the read — so the worst outcome
// here is one dropped record in a trend report, not a corrupt store. A caller
// needing a hard per-line guarantee must serialize appends itself.
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
