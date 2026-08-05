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
// The guarantee has two layers. Mutual exclusion comes first: Append and Compact
// run their critical sections under withLock (lock.go), a mkdir-based
// cross-process lock with staleness reclaim, so two processes never append to or
// rewrite the store at the same time. Line integrity comes second: inside that
// lock, each Append call marshals one record to one []byte and issues exactly one
// os.Write to a file opened O_APPEND. On Linux/macOS a write() to a regular file
// opened O_APPEND atomically appends, so a record can never interleave or tear
// even if the lock is reclaimed stale and two appends overlap. No bufio.Writer is
// shared across records — batching would coalesce records into one larger write
// whose atomicity is not guaranteed, tearing lines under concurrency. The lock is
// taken by Append, appendBatch (one cycle per reconcile run, one Write per
// record inside it), and Compact only: MaybeCompact's threshold stats read runs
// outside any lock (withLock is mkdir-based and not reentrant, so nesting it
// inside Compact would stall for the full lockWait). The portability caveat for
// non-POSIX append semantics is the accepted TD-004 won't-fix stance already
// applied to the other five append-only ledgers (audit, debate, scorecard, tools,
// history).
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
// # Collision rule (origin is not part of the id)
//
// StampID hashes file/line/problem ONLY — origin is deliberately excluded — so a
// manually-added item (`debt add`, OriginManual) and a reconcile finding for the
// same location and problem text collide on ONE id, and the write-time dedup
// above silently drops whichever arrives SECOND. Because the dedup lives only on
// the reconcile hook while `debt add` appends unconditionally, the collision
// resolves asymmetrically today: add-then-reconcile keeps the MANUAL entry (the
// finding is skipped), while reconcile-then-add leaves BOTH records in the
// store. No supersession exists in either direction — a review finding never
// rewrites a manual entry's fields, and a manual entry never rewrites a
// finding's. Whether a review finding MAY supersede a manual entry is an open
// semantics question deferred to T2/T3 (sprint-plan TD-003); this paragraph
// documents the current rule so the drop is deliberate rather than accidental.
// Pinned by TestPersistForReconcile_ManualEntryWinsIDCollision.
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
// The seed is also authoritative only for the INSTANT it was taken. ReadSummaries
// runs with no lock held — the lock is taken and released per-Append afterwards —
// so two concurrent reconciles (a CLI run and an MCP-driven one, both on the
// PersistForReconcile bridge) can each seed from a store that lacks the other's
// records and both append the same ids. The observable damage is bounded to store
// bloat and degraded RecordsBefore accounting: FoldRecords collapses the duplicate
// ids on every read. Under concurrency, duplicates are collapsed by the fold, not
// prevented by the seed.
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
// shard. Compact folds each id to its effective record and rewrites the shards
// atomically, so store size tracks LIVE findings rather than history.
//
// Retention is bounded at two records per id, with one documented exception below.
// retainForCompaction keeps a SECOND record for an id in either of two cases: the resolution TRAIL when the effective
// record is open (preserving the ResolvedAt and the human-typed --reason a
// regression would otherwise erase), and the model DONOR when the effective record
// is settled but carries no attribution (preserving the record
// AggregateQualitySignal recovers a Model from — without it the outcome vanishes
// from the signal entirely). Both are written with their counters zeroed, and the
// donor is emitted BEFORE the effective record so a full timestamp/rank tie still
// folds to the effective one.
//
// The exception: an id ANCHORED to a shard Compact cannot rewrite (one holding a
// line over maxLineBytes) is not compacted at all — every record of it is written
// back untouched, so it retains its full history until that oversized line leaves
// the store. Writing the fold instead would discard the aggregate counters, which
// live only on the record being skipped, while still dropping the sightings they
// summarized. That is the same trade the protected shard itself gets: leaving it
// whole costs disk, rewriting it costs data.
//
// It runs two ways. Manually, via `atcr debt compact`. Automatically, via
// MaybeCompact, called once by PersistForReconcile AFTER its append loop and ONLY
// when that run appended at least one record — a suppressed,
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
// Four writers append to the store: the two reconcile entry points that share the
// PersistForReconcile bridge (cli/reconcile.go's persistLocalDebt wrapper and the
// MCP atcr_reconcile handler in internal/mcp/handlers.go) for detections, plus
// cli/debt_add.go (hand-filed items) and cli/debt_resolve.go (resolutions)
// directly — plus Compact itself,
// and all of them use ONE convention: append with the counters ZERO. The counters
// are derived state and live on an id's effective record alone, stamped there by
// the fold. Three sites must zero them EXPLICITLY, across the two paths that copy
// an existing record wholesale — debt_resolve's resolution, and
// retainForCompaction's retained trail and model donor — since a second carrier in
// a group would let the fold count part of the history twice.
//
// A hand-filed item still counts as a sighting even though it is appended with zero
// counters and may carry a status: isSighting recognises it from its manual origin
// and empty ResolvedAt, which is what separates a filing from a resolution that
// merely inherited the origin by copying.
//
// # Maintenance invariant (coupling)
//
// The read-side fold and the write-side dedup seed are ONE decision in two
// places, and the fold is unconditional — so changing only the append side is a
// no-op in observable behavior, and widening the seed back to every id in the
// store silently restores permanent closure with both sides' tests still green.
// persistLocalDebt (cli/reconcile.go) is only a thin resolve-and-delegate wrapper;
// PersistForReconcile therefore folds before seeding and seeds
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
// TD-002 is CLOSED (Plan 35.13 T6). The reconcile persistence hook is
// PersistForReconcile in this package, and ALL FOUR reconcile call sites use
// it: the CLI `atcr reconcile` path (cli/reconcile.go, via a thin
// resolve-and-delegate persistLocalDebt wrapper), the MCP `atcr_reconcile`
// handler (internal/mcp/handlers.go, immediately after the scorecard emit),
// the one-shot `atcr review --fail-on/--verify/--debate/--auto-fix` inline
// reconcile (cli/review.go), and `atcr resume`'s auto-reconcile
// (cli/resume.go) — the latter two added when their "persists nothing" gap was
// closed, since the one-shot review is the primary CI invocation. One
// implementation is the contract, not an implementation detail — a second copy
// in internal/mcp would satisfy parity on the day it shipped and then freeze
// at that day's dedup, streaming, and compaction behavior while this one moved
// on. localdebt is therefore no longer a CLI-side ledger.
//
// Shared code does NOT by itself make the record sets identical, and claiming
// otherwise would be the more dangerous error. Every entry point now resolves
// the store root BEFORE RunReconcile and validates finding paths against it
// (TD-019/TD-024), so the bridge's path-warned exclusion is reachable
// everywhere — but the resolved roots themselves can still legitimately differ
// across entry points (the CLI has a CWD tier and a --repo flag; the MCP path
// trusts only repo > manifest), and the INPUT finding sets can differ where
// the callers differ upstream. What the shared bridge guarantees is that
// record construction, dedup seeding, and compaction cannot drift.
//
// The store's readers have since moved: `atcr debt list`/`add`/`dashboard`/
// `resolve` resolve the store through debtStoreDir (cli/debt.go), which walks up
// to the repo-root marker. The one remaining CWD-relative reader is the outbound
// quality-signal payload (cli/qualitysignal.go), which calls
// buildQualitySignalPayload("."). The residual mismatch is therefore narrower
// than it was: the readers resolve to the repo root while ResolveStoreRoot tier 3
// resolves to the process CWD, so a reconcile run from a subdirectory with no
// --repo and no manifest root still writes to a store the payload does not read
// (TD-020).
//
// What blocked MCP parity was root resolution, not persistence: the server operates
// on review artifact directories whose process CWD is whatever launched it, so
// DefaultDir(".") would have written to an unrelated place. ResolveStoreRoot
// (root.go) settles it with an ordered precedence:
//
//		explicit > manifest > CWD
//
//	 1. Explicit — an operator-supplied root (`--repo`, or the MCP repo argument).
//	    The CLI's value is checked for existence only: a human typed it, and it
//	    has already been through normalizeRepoFlag. The MCP argument is
//	    model-supplied, so that entry point sets RequireMarker and the tier
//	    re-validates exactly like the manifest tier — a model-named path must
//	    prove repo-ness before it gains a store (TD-023).
//	 2. Manifest — payload.Manifest.Root, recorded at review time when CWD == repo
//	    root is a documented requirement, so it travels with the artifacts. It is a
//	    CLAIM, not a fact, and is RE-VALIDATED before any write: the path must exist,
//	    be a directory, and carry a .git (directory or file, so linked worktrees and
//	    submodules count) or .atcr marker.
//	 3. CWD — byte-for-byte the pre-manifest DefaultDir(".") behavior, and CLI-only.
//	    The MCP handler passes AllowCWD: false because its CWD is meaningless by
//	    construction.
//
// AllowCWD: false bars the CWD as a FALLBACK; it does not prove the resolved root is
// not the CWD. An MCP-created review records Manifest.Root from the engine's own
// root, which cli/serve.go hardcodes to "." — so the manifest can carry the server's
// CWD as an authoritative absolute claim, and that directory then validates, because
// the review artifacts under .atcr/ ARE the marker. The recorded-root design rests on
// review having run with CWD == repo root; where that does not hold, tier 2 inherits
// the error rather than detecting it (TD-025).
//
// An invalid root at any tier is NO-PERSIST-WITH-WARNING. There is no fall-through:
// a root that was named and turned out to be stale is a stop signal, and falling
// through to the next tier would convert a detectable no-op into an undetectable
// write to the wrong store — the precise failure the recorded root exists to
// prevent. A MISSING claim falls through (nobody asserted anything); an INVALID one
// does not. Persistence is best-effort at both entry points: an unresolvable root
// never changes a command's exit code or an MCP tool's result.
package localdebt
