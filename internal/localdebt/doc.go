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
// Symlink following is an accepted won't-fix on the same footing. Append opens the
// shard with O_CREATE|O_WRONLY|O_APPEND (no O_NOFOLLOW) and MkdirAll follows symlinked
// path components, so a pre-planted symlink at .atcr/debt/YYYY-MM.jsonl or a parent
// could redirect appends to a target outside the store. Exploiting it requires
// pre-existing local write access to the store path (LOW severity), and the identical
// exposure is systemic across the sibling ledgers (scorecard, history, audit, debate,
// tools) — hardening only localdebt would be inconsistent and give false security. A
// repo-wide GOOS-guarded O_NOFOLLOW pass (precedent: internal/tools/open_unix.go /
// open_other.go) is the correct venue if it is ever pursued; localdebt deliberately
// does not diverge from its siblings here.
//
// # Deduplication contract
//
// The store itself does not dedup on write — Append is unconditional. The settled
// dedup strategy for the reconcile persistence hook (Story 2) is write-time dedup
// by id (history.FindingID(file, line, problem)) using a full-history ReadAll scan
// before each append: the hook skips any finding whose id already exists across all
// shards, and fails open toward append (at-least-once) if the dedup read fails. The
// contract is documented here so downstream callers write against a settled rule.
//
// # Resolution contract
//
// Resolution is currently terminal. A `resolved` or `deferred` status record for
// an id causes selectOpenDebt to fold that id out of the open backlog permanently,
// regardless of recency. Re-detecting the same file/line/problem later will not
// re-open the item; the id is considered closed forever. This is the deliberate
// v1 design (TD-004); a re-openable resolution mode that re-appends regressed
// findings as fresh open records is deferred to a follow-up epic.
//
// The append side respects the same terminal design. persistLocalDebt seeds its
// write-time dedup set from a full-history ReadAll that includes terminal resolution
// records, so a resolved-then-regressed finding (same id) is not re-appended and does
// not re-enter the open backlog. Re-opening on regression is therefore a single
// coupled decision spanning both the selectOpenDebt read-side fold and the
// persistLocalDebt dedup seeding — the read-side fold alone is unconditional and
// irreversible, so changing only the append side would be a no-op in observable
// behavior — and both are deferred together as a unit.
//
// # Call-site scope
//
// The reconcile persistence hook (persistLocalDebt) is currently invoked only
// from the CLI `atcr reconcile` path (cmd/atcr/reconcile.go). The MCP
// `atcr_reconcile` handler intentionally does NOT persist to this store today,
// because the server operates on review artifact directories rather than a
// checked-out repo root and lacks the resolved repo-root guard the hook needs.
// This is a deliberate Story 2 scope boundary (TD-002), not an oversight; MCP
// parity for local-debt persistence is deferred to a follow-up epic. Callers
// should treat localdebt as a CLI-side ledger until that parity work lands.
package localdebt
