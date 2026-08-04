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
// os.UserConfigDir()/atcr/scorecard/) and in the Record shape (the TD schema in
// record.go, currently v3 — see the SchemaVersion doc block for the version
// history and the tolerant-read contract).
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
// Resolution lifetime depends on the status, because the identity function
// decides it. FindingID hashes file, line, and problem — LINE is part of the id
// — so a genuine fix shifts surrounding lines, the id changes, and the finding
// returns as a fresh item regardless of any suppression rule. The only case a
// terminal record actually suppresses is same file, same line, same problem
// text: after a real fix that means a regression, or a fix that never landed.
//
//	Status     Lifetime                      Rationale
//	--------   ---------------------------   ------------------------------------
//	wontfix    Terminal, permanent           Suppression is the feature; a false
//	                                         positive is stable at a stable
//	                                         location, so its id is stable
//	                                         (Epic 24.0).
//	resolved   Re-opens on re-detection      Same id after a fix means a
//	                                         regression — the thing most worth
//	                                         surfacing, and what the blanket
//	                                         terminal rule used to silence.
//	deferred   Re-surfaces on re-detection   "Not now" is not "never".
//
// Three predicates express this, and `deferred` is the status that separates
// them. IsClosedStatus classifies a RECORD as terminal (all three statuses);
// IsSettledStatus asks whether the ITEM is done (resolved|wontfix), which gates
// closability and the live-backlog count — a deferred item carries a terminal
// marker but is still work, so it must stay closeable; IsSuppressingStatus
// decides whether a terminal state outlives a re-detection (wontfix only).
// FoldRecords implements the table: a
// suppressing record wins unconditionally, otherwise the effective record is the
// latest by timestamp, so a re-detection appended after a resolution is the
// effective record and the id is open again.
//
// # Maintenance invariant (coupling)
//
// The read-side fold and the write-side dedup seed are ONE decision in two
// places, and the fold is unconditional — so changing only the append side is a
// no-op in observable behavior, and widening the seed back to every id in the
// store silently restores permanent closure with both sides' tests still green.
// persistLocalDebt (cli/reconcile.go) therefore folds before seeding and seeds
// only ids whose effective record suppresses or is still open. Change one, change
// the other.
//
// # Which paths write `deferred` (swept 2026-08-04, Plan 35.13 T3)
//
// Exactly one live writer: `atcr debt add --status deferred` (cli/debt_add.go),
// which files a manual record with that status. It is new — T2 created it when it
// rewired `add` onto this store. `atcr debt resolve` cannot write `deferred`
// (resolveStatuses admits resolved|wontfix only), and persistLocalDebt writes an
// empty status (open). The historical `deferred` writers lived in the
// .planning/-scoped store — internal/tdmigrate's Item.Status and
// internal/debt's aggregate classification — and both packages were deleted by
// T2. Everything else matching "deferred" in this package and in cli/ is
// read-side classification (IsClosedStatus, ClosedStatusRank, the dashboard's
// status bucketing) or an enum table, not a writer. Recorded here so the next
// reader does not repeat the search.
//
// # Call-site scope
//
// The reconcile persistence hook (persistLocalDebt) is currently invoked only
// from the CLI `atcr reconcile` path (cli/reconcile.go). The MCP
// `atcr_reconcile` handler intentionally does NOT persist to this store today,
// because the server operates on review artifact directories rather than a
// checked-out repo root and lacks the resolved repo-root guard the hook needs.
// This is a deliberate Story 2 scope boundary (TD-002), not an oversight; MCP
// parity for local-debt persistence is deferred to a follow-up epic. Callers
// should treat localdebt as a CLI-side ledger until that parity work lands.
package localdebt
