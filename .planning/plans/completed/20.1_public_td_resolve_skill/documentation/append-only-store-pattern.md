# Append-Only JSONL Store Pattern

**Priority: Critical**

## Overview

Epic 20.1 needs a durable, `.atcr/`-scoped local technical-debt store for standalone/public atcr users who have no `.planning/` directory. The codebase already has two append-only JSONL stores that are structurally close to what this epic requires, and the discovery snapshot for this plan identifies exactly which parts of each to copy and which parts to avoid.

`internal/scorecard/store.go` is the primary model: a lazily-created, cross-run, append-only JSONL store rooted outside any project-specific directory (`os.UserConfigDir()`), with a concurrency-safe single-write-per-record append path and a tolerant read path that skips malformed or forward-incompatible lines instead of aborting. `internal/history` is a cautionary counter-example: it began life as a `.atcr/`-scoped append-only ledger but was deliberately migrated to `.planning/`-scoped sharded storage in Epic 19.4 for the private, version-controlled pipeline. Epic 20.1's new store inverts that migration on purpose — it targets standalone/public users who have no `.planning/` at all, so a `.atcr/`-scoped design is appropriate here even though it was intentionally abandoned for the private pipeline's own ledger.

The new package (recommended name `internal/localdebt`) should therefore copy the atomic-append and malformed-line-skip mechanics from `internal/scorecard/store.go` verbatim, adapt the path-resolution logic to root at `.atcr/debt/` (or similar) instead of `os.UserConfigDir()`, and reuse the stable content-hash ID construction from `internal/history/record.go` for item identity/dedup. The design must also explicitly state its concurrency guarantee and its relationship to the accepted TD-004 won't-fix stance on cross-process O_APPEND locking, since both existing stores document this tradeoff explicitly rather than leaving it implicit.

## Key Concepts

### Atomic single-write-per-record append

`internal/scorecard/store.go:89` `Append` lazily creates its directory (mode 0700), marshals one record to JSON, and issues exactly one `os.OpenFile(O_APPEND)` + `Write` per record so concurrent writers never tear a line, relying on the POSIX atomic-append guarantee for regular files.

> Source: [codebase-discovery.json: existing_patterns[0].description]

The new local TD store's Append implementation should copy this atomic-append-per-record behavior verbatim, changing only the root directory resolution.

> Source: [codebase-discovery.json: existing_patterns[0].follow_for]

### Tolerant, streaming read path

`ReadRecords`/`ReadAll` stream-parse line-by-line via `bufio.Reader` (not `Scanner`, specifically to survive an over-long line), skipping malformed lines and forward-incompatible `schema_version` records with a warning rather than aborting the whole read.

> Source: [codebase-discovery.json: existing_patterns[0].description]

### Global vs. repo-local root: the one structural difference

The scorecard store lives at `os.UserConfigDir()/atcr/scorecard/YYYY-MM.jsonl` — global and per-user. This is described as "the ONE structural difference from what this epic needs (per-repo `.atcr/`)." `internal/scorecard/paths.go:23` `DefaultDir` resolves the user-config path, and `monthFromRunID` derives the shard name.

> Source: [codebase-discovery.json: existing_patterns[0].description]

Because scorecard is deliberately global/per-user rather than per-repo, it should not be imported directly — the pattern should be copied into a new package rooted at `.atcr/debt/`.

> Source: [codebase-discovery.json: build_from.suggested_approach; reusable_components: "scorecard.Append / scorecard.ReadRecords / scorecard.ReadAll pattern"]

### `internal/history`: a superseded `.atcr/`-scoped design, now read-only fallback

`internal/history` originally used a `.atcr/findings-history.jsonl` append-only ledger (`internal/history/paths.go` `LegacyLedgerPath` still returns this path), but Epic 19.4 deliberately moved primary writes to `.planning/history/YYYY-MM.jsonl` (`internal/history/paths.go` `ShardDir`) because the private pipeline wanted version-controlled, sharded history. The legacy `.atcr/` path is now read-only fallback for pre-19.4 data.

> Source: [codebase-discovery.json: existing_patterns[1].description]

This is called out as the inverse of what Epic 20.1 needs: a *primary*, not legacy-fallback, `.atcr/`-scoped store with zero `.planning/` dependency for standalone/public users. `internal/history` should not be extended or re-routed for this purpose — it is intentionally private-pipeline-oriented post-19.4.

> Source: [codebase-discovery.json: existing_patterns[1].follow_for]

Even so, its `Record` struct (stable content-hash ID via `FindingID`, a small intentional field set) and its `Append()` (single-batch-buffer-then-one-Write, `MkdirAll` for parent dirs) remain a good API shape to mirror in the new package.

> Source: [codebase-discovery.json: existing_patterns[1].follow_for]

### Stable content-hash identity (`FindingID`)

`history.FindingID(file, line, problem)` — a SHA-256 hash over NUL-separated fields, first 8 bytes hex-encoded — mirrors `internal/debate.itemID` and lives at `internal/history/record.go:48`. The new TD store's dedup/identity key can reuse this exact construction so IDs stay consistent with the rest of the codebase's finding-identity conventions.

> Source: [codebase-discovery.json: reusable_components: "history.FindingID stable content-hash ID"; build_from.suggested_approach]

### Why `.atcr/` instead of `.planning/` for this epic

The architecture notes explicitly flag that a `.atcr/`-scoped design was tried before (`internal/history`) and then intentionally superseded for the *private* pipeline. Epic 20.1's `.atcr/`-scoped design targets a different audience (standalone/public users with zero `.planning/`), so the earlier superseding logic does not automatically apply — but the design should explicitly document why `.atcr/` is being chosen over `.planning/` for this store.

> Source: [codebase-discovery.json: architecture_notes[0]]

### Concurrency guarantee must be stated explicitly

Concurrent-write safety is a recurring, explicitly-documented concern in both existing stores — scorecard's long comment on O_APPEND atomicity, and `internal/history/writer.go`'s caveat about `os.File.Write` looping on short writes for large buffers. The new store's design must state its concurrency guarantee explicitly (likely: one `Append` call = one `os.Write`, no batching across concurrent `atcr reconcile` invocations) rather than leaving it implicit, and should reference the accepted TD-004 won't-fix stance.

> Source: [codebase-discovery.json: architecture_notes[1]]

The new local TD store becomes atcr's 6th append-only ledger (joining audit, debate, scorecard, tools, history). The project already has an accepted, documented won't-fix on cross-process O_APPEND locking (TD-004); the new store should state the same tradeoff explicitly rather than silently diverging.

> Source: [codebase-discovery.json: architecture_notes[2]]

### Resolved design decisions

**Repo-root detection:** settled on CWD (`Root: "."`), matching `cmd/atcr/reconcile.go:93` and the public skill's existing `.atcr/` scoping. The store path is therefore `<cwd>/.atcr/debt/YYYY-MM.jsonl`. No `git rev-parse` helper is introduced; standalone/public users may run `atcr` from any directory and the store follows CWD, just as scorecard emission and finding-path validation already do.

> Source: [codebase-discovery.json: integration_gaps: "Local store path resolution"; AC 01-01]

**Shard strategy:** settled on month-sharded `YYYY-MM.jsonl` derived from the record's `run_id` prefix, identical to `internal/scorecard` and `internal/history`. This avoids the single-file merge-conflict churn that motivated Epic 12.1's sharded-YAML migration while keeping the read path simple (one shard per month).

> Source: [codebase-discovery.json: integration_gaps: "Shard/deduplication strategy"; documentation/local-td-store-schema.md]

**Deduplication strategy:** settled on write-time dedup by `history.FindingID(file, line, problem)` using a full-history `ReadAll` scan before each append. The persistence hook skips a finding if any existing record (across all shards) shares the same `id`. This is friendlier to append-only readers than append-always + reader-side dedup, at the cost of an O(total records) read-before-write per reconcile run — acceptable at the documented ledger scale (hundreds of records/month). See AC 02-03 for the full contract.

> Source: [AC 02-03; documentation/local-td-store-schema.md "Identity and Deduplication"]

## Quick Reference

| Store | Root path | Scope | Status | Shard strategy | Notes |
|---|---|---|---|---|---|
| `internal/scorecard` | `os.UserConfigDir()/atcr/scorecard/` | Global, per-user | Active — primary model to copy | `YYYY-MM.jsonl` via `monthFromRunID` | Atomic single-write-per-record append; tolerant malformed/schema-version skip on read |
| `internal/history` | `.planning/history/` (primary, post-19.4); `.atcr/findings-history.jsonl` (legacy fallback) | Per-repo, private pipeline | Active for `.planning/`; `.atcr/` path is read-only legacy fallback | `YYYY-MM.jsonl` via `ShardDir` | Deliberately migrated off `.atcr/` in Epic 19.4 — do not extend/re-route; `Record`/`FindingID` shape still worth mirroring |
| `internal/localdebt` (new, Epic 20.1) | `<cwd>/.atcr/debt/` | Per-repo, standalone/public (zero `.planning/` dependency) | To be implemented | `YYYY-MM.jsonl` via `run_id` prefix; write-time dedup by `FindingID` | Must state its own concurrency guarantee explicitly and reference TD-004 won't-fix |

## Related Documentation

- `internal/scorecard/store.go` — Append/ReadRecords/ReadAll pattern to copy
- `internal/scorecard/paths.go` — `DefaultDir` and `monthFromRunID` path/shard resolution
- `internal/history/record.go` — `Record` shape and `FindingID` stable content-hash ID (line 48)
- `internal/history/paths.go` — `LegacyLedgerPath` (`.atcr/` fallback) and `ShardDir` (`.planning/history/` primary)
- [`local-td-store-schema.md`](local-td-store-schema.md) — concrete v1 record schema and file layout for the new `.atcr/debt/` store
- `docs/scorecard.md` — scorecard store documentation
