// Package localdebt persists reconciled code-review findings into a durable,
// .atcr/-scoped technical-debt backlog for standalone/public atcr users who have
// no .planning/ directory (Epic 20.1).
//
// # Store layout
//
// Records are appended one JSON object per line to a month-sharded JSONL file at
// <repo-root>/.atcr/debt/YYYY-MM.jsonl, the shard chosen from each record's
// run_id prefix. The directory is created lazily (0700) on first write and shard
// files are 0600, so records are never group- or world-readable. .atcr/ is local,
// uncommitted state; the store is intentionally outside version control.
//
// The append/tolerant-read mechanics are a direct structural copy of
// internal/scorecard/store.go — a proven append-only ledger — differing only in
// root-path resolution (per-repo .atcr/debt/ instead of the global
// os.UserConfigDir()/atcr/scorecard/) and in the Record shape (the v1 TD schema in
// record.go).
//
// # Why .atcr/ and not .planning/
//
// This store is deliberately NOT a re-extension of internal/history, whose .atcr/
// root (.atcr/findings-history.jsonl) was superseded by .planning/history/ in Epic
// 19.4 for the private pipeline. localdebt targets a different audience —
// standalone/public users with zero .planning/ directory — and a different query
// pattern (a resolution backlog, not a time-windowed trend history). It imports
// internal/history for the FindingID identity helper ONLY; it does not import or
// extend any of history's .planning/-scoped read/write logic. Reusing FindingID
// keeps record identity consistent with the rest of the codebase's finding-identity
// convention without coupling to history's storage location.
//
// # Concurrency guarantee
//
// Each Append call marshals one record to one []byte and issues exactly one
// os.Write to a file opened O_APPEND. On Linux/macOS a write() to a regular file
// opened O_APPEND atomically appends, so two processes appending concurrently never
// interleave or lose a record. No bufio.Writer is shared across records — batching
// would coalesce records into one larger write whose atomicity is not guaranteed,
// tearing lines under concurrency. The portability caveat for non-POSIX append
// semantics is the accepted TD-004 won't-fix stance already applied to the other
// five append-only ledgers (audit, debate, scorecard, tools, history); no
// cross-process lock is introduced.
//
// # Deduplication contract
//
// The store itself does not dedup on write — Append is unconditional. The settled
// dedup strategy for the reconcile persistence hook (Story 2) is write-time dedup
// by id (history.FindingID(file, line, problem)) using a full-history ReadAll scan
// before each append: the hook skips any finding whose id already exists across all
// shards, and fails open toward append (at-least-once) if the dedup read fails. The
// contract is documented here so downstream callers write against a settled rule.
package localdebt
