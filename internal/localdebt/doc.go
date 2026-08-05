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
// by id (history.FindingID(file, line, problem)) seeded from a full-history scan
// before each append: the hook skips any finding whose id is already seeded, and
// fails open toward append (at-least-once) if the dedup read fails. The contract is
// documented here so downstream callers write against a settled rule.
//
// The seed is SCOPED, not the whole store — see the Resolution contract below and
// the Maintenance invariant: only ids whose EFFECTIVE (folded) record suppresses or
// is still open are seeded, so a re-detected resolved/deferred id re-appends.
//
// The scan is a minimal-decode STREAMING read (streaming.go: StreamSummaries /
// ReadSummaries / FoldSummaries), not ReadAll. The seed needs an id's effective
// status and nothing else, so decoding into Summary rather than Record keeps peak
// memory an order of magnitude below the full-record path at the scale the
// auto-compaction threshold allows. ReadAll remains the read path for every
// consumer that renders records. Both decoders apply the same skip gates in the
// same order — a line visible to one and invisible to the other would either leave
// an id in the store but absent from the seed (re-appending a duplicate every run)
// or seed an id no rendering path can display (hiding the finding permanently).
// decodeSummary documents the one residual asymmetry and why it is accepted.
//
// The fail-open path is ALL-OR-NOTHING: on a read error the seed is left empty and
// the run appends without dedup. ReadSummaries does return the summaries it decoded
// before the error, but seeding from that partial set is NOT safe and must not be
// reintroduced as an optimization. The seed keys on an id's EFFECTIVE status, and
// the fold can only compute that from the id's COMPLETE history — drop the shard
// holding an id's resolution and the id folds to `open`, gets seeded as
// outstanding, and the re-detection that should have re-opened it is skipped. The
// partial result is safe only for questions that do not depend on the fold.
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
// # Compaction contract
//
// Compaction is what bounds the store's growth. Month sharding gives file-size
// hygiene and archival boundaries but no bound: every operation reads every shard,
// because dedup needs every id ever seen and a resolution may live in any later
// shard. Compact folds each id to its effective record (plus the resolution trail
// when that record is open — retainForCompaction) and rewrites the shards
// atomically, so store size tracks LIVE findings rather than history.
//
// It runs two ways. Manually, via `atcr debt compact`. Automatically, via
// MaybeCompact, called once by cli/reconcile.go's persistLocalDebt AFTER its append
// loop and ONLY when that run appended at least one record — a suppressed,
// zero-finding, or fully-deduped reconcile pays no extra I/O at all. The automatic
// call is best-effort in the same sense as the persistence hook itself: any failure
// is logged to the diagnostics writer and never changes the reconcile's return
// value or the gate's exit code.
//
// Two conditions gate the automatic trigger, and both are load-bearing:
//
//  1. A THRESHOLD is tripped — DefaultAutoCompactMaxRecords lines or
//     DefaultAutoCompactMaxBytes bytes, whichever first. Measured with no JSON
//     decode — stat for bytes, a newline count for lines — and the byte clause is
//     satisfied by stat alone, so it short-circuits the line scan entirely.
//     StoreStats reports the same two numbers for any caller that wants them.
//  2. The store has GROWN materially (50%) past the size the last compaction left
//     behind, recorded in the .compact-watermark file. This is not belt-and-braces:
//     because compaction retains up to two records per id, a store's
//     post-compaction floor can sit ABOVE the threshold, and a bare absolute
//     threshold would then re-fire on every single append forever — taking the
//     cross-process lock and rewriting every shard to drop nothing. The watermark
//     degrades to zero when absent or unreadable, so it can delay a redundant
//     compaction but never prevent a needed one. Compact writes it, not
//     MaybeCompact, so a manual `atcr debt compact` refreshes the baseline too and
//     cannot leave a stale one behind; a no-op fold records a baseline as well,
//     without which the trigger would re-fire forever on a store of only malformed
//     or forward-incompatible lines.
//
// MaybeCompact takes no lock; Compact acquires withLock internally and withLock is
// not reentrant, so nesting would stall for the full lockWait and fail.
//
// # Aggregate counters
//
// Because resolution is re-openable, how many times a finding has come back is real
// signal — and compaction would otherwise destroy it along with the superseded
// records. FoldRecords therefore stamps the effective record with Occurrences and
// FirstSeen aggregated across the whole id group, preserving that signal at O(1)
// size instead of O(history). Regression count is Occurrences-1. The rule is
// idempotent: re-compacting an already-compacted store leaves both values
// unchanged. See aggregateCounters (store.go) for the rule and the lifecycle walk.
//
// There are exactly three writers of the store, and each has its own counter
// convention. cli/reconcile.go's detection append leaves both fields ZERO, so the
// fold counts it as a fresh sighting. cli/debt_resolve.go's resolution append and
// retainForCompaction's retained trail/donor entries must ZERO them explicitly —
// both copy an existing record, counters and all, and a second carrier in a group
// would let the fold count part of the history twice. cli/debt_add.go does the
// opposite deliberately and stamps Occurrences 1, because a hand-filed item is its
// own first sighting and may carry a status the counting rule would otherwise never
// treat as a detection. Outside that one case the counters live on an id's
// effective record alone.
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
