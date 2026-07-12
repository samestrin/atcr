# Local Technical-Debt Store Schema (`.atcr/`)

**Priority: Critical**

## Overview

Epic 20.1 needs a concrete, `.atcr/`-scoped JSONL store format for standalone/public users who have no `.planning/` directory. This document defines the proposed v1 record schema, file layout, and identity/deduplication rules for that store. It is the design input for AC1 ("a local TD store format is defined and documented") and the implementation target for the new `internal/localdebt` package.

The format copies the append-only, month-sharded JSONL mechanics from `internal/scorecard/store.go` and `internal/scorecard/paths.go`, but roots the files under `.atcr/debt/` instead of `os.UserConfigDir()/atcr/scorecard/`. Record identity reuses the stable `history.FindingID(file, line, problem)` construction (`internal/history/record.go:48`) so the same finding shares an ID across review runs even when its severity or line number drifts.

## File Layout

```text
<repo-root>/.atcr/
├── reviews/
│   └── <id>/
│       └── reconciled/
│           └── findings.json
└── debt/
    └── YYYY-MM.jsonl
```

- **Root:** `.atcr/debt/` under the current repo root (mirroring `cmd/atcr/reconcile.go:93` `Root: "."` and the `.atcr/` scope used by the rest of the public skill).
- **Shard strategy:** one JSONL file per calendar month, named from the review run's `run_id` prefix (`YYYY-MM`), identical to `scorecard` and `history`.
- **Permissions:** directory `0700`, file `0600`, created lazily on first write.
- **Do not commit.** `.atcr/` is local state; the store is intentionally outside version control.

> **Open design decision:** month sharding is the default path because it matches the two existing append-only ledgers (`scorecard`, `history`) and avoids the merge-conflict churn that motivated Epic 12.1's sharded-YAML migration. Per-run shards or a single `.atcr/debt.jsonl` are alternatives recorded as integration gaps in `codebase-discovery.json` if the design sprint chooses otherwise.

## Record Schema (v1)

Each line is one JSON object. `schema_version` is `1`. Fields are additive; later versions may add keys, and readers must tolerate unknown keys and skip forward-incompatible `schema_version` values.

### Required fields

| Field | Type | Description |
|-------|------|-------------|
| `schema_version` | int | `1`. Bumped on backward-incompatible changes. |
| `id` | string | Stable 16-hex-char content hash from `history.FindingID(file, line, problem)`. |
| `run_id` | string | `<RFC3339 reconciled_at>-<review-dir base>`, e.g. `2026-06-14T10:00:00Z-abc123`. |
| `ts` | string | RFC3339 timestamp of the reconcile run. |
| `severity` | string | `CRITICAL`, `HIGH`, `MEDIUM`, or `LOW`. |
| `file` | string | Cited file path (relative to repo root). |
| `line` | int | Cited line number. |
| `problem` | string | Problem text, potentially prefixed with a stable symbol anchor `(symbolName)` per `docs/technical-debt-format.md`. |
| `fix` | string | Suggested fix from the reconciled finding. |
| `category` | string | Finding category label. |
| `est_minutes` | int | Estimated resolution effort. |
| `evidence` | string | Supporting evidence (may include disagreement annotation). |
| `reviewers` | []string | Distinct reviewers that contributed to this finding. |
| `confidence` | string | `HIGH`, `MEDIUM`, or `LOW`. |

### Optional fields

| Field | Type | Presence | Description |
|-------|------|----------|-------------|
| `justification` | string | omitempty | Narrative context extracted from `review.md` (`internal/reconcile/justification.go:72`). |
| `source_report` | object | omitempty | Back-reference to the `review.md` section the justification came from. |
| `source_report.path` | string | always (when object present) | Review-dir-relative path, e.g. `sources/host/review.md`. |
| `source_report.line` | int | omitempty | 1-based line in that `review.md`. |
| `source_report.section` | string | omitempty | Nearest enclosing Markdown heading. |
| `status` | string | omitempty | Resolution state for the public pipeline: `open`, `in_progress`, `resolved`, or `wont_fix`. Default when absent is `open`. |
| `resolved_at` | string | omitempty | RFC3339 timestamp when `status` became `resolved` or `wont_fix`. |

### Example record

```json
{
  "schema_version": 1,
  "id": "a3f7c9d2e8b10567",
  "run_id": "2026-06-14T10:00:00Z-abc123",
  "ts": "2026-06-14T10:00:00Z",
  "severity": "HIGH",
  "file": "internal/scorecard/store.go",
  "line": 89,
  "problem": "(Append) Concurrent writers may tear JSONL lines if writes are batched",
  "fix": "Issue exactly one os.Write per record under O_APPEND",
  "category": "correctness",
  "est_minutes": 30,
  "evidence": "Scorecard comment notes POSIX atomic-append guarantee",
  "reviewers": ["bruce", "host"],
  "confidence": "HIGH",
  "justification": "The Append function marshals one record and writes it in a single Write call so concurrent appends do not interleave.",
  "source_report": {
    "path": "sources/bruce/review.md",
    "line": 42,
    "section": "Concurrency concerns"
  }
}
```

## Identity and Deduplication

- **ID construction:** `history.FindingID(file, line, problem)` — SHA-256 over `file\x00line\x00problem`, first 8 bytes hex-encoded. This matches `internal/debate.itemID` and `internal/history/record.go:48`.
- **Severity is intentionally not part of the ID.** Severity can be re-settled by debate/verify, so keying on it would mint duplicate IDs for the same underlying issue.
- **Problem text includes the symbol anchor.** Epic 18.1 stamps `(symbolName)` at the start of `problem`. Because the anchor is part of `problem`, a finding whose line shifts but whose enclosing block name stays the same keeps the same ID only if the stored `problem` is identical. If the resolver updates the stored `line` after a fix, the ID will change unless dedup is relaxed; this is the same line-drift tradeoff the private pipeline manages.
- **Dedup strategy (open integration gap):** the persistence hook may either (a) append every reconciled finding and let readers deduplicate by `id`, or (b) check the existing shard for the `id` and skip duplicates at write time. Option (b) is friendlier to append-only readers but requires a read-before-write per record. The design sprint must decide and document the choice.

## Concurrency and Persistence Guarantees

- **Atomic append:** each `Append` call marshals one record to one `[]byte` (with trailing `\n`) and issues exactly one `os.Write` to a file opened `O_APPEND`, following `internal/scorecard/store.go:89`.
- **Single-write-per-record:** no `bufio.Writer` is shared across records, because batching would coalesce multiple records into one write whose atomicity is not guaranteed.
- **Cross-process behavior:** concurrent `atcr reconcile` runs on the same repo rely on POSIX `O_APPEND` atomicity for regular files. This is the same accepted tradeoff documented as TD-004 for the other five append-only ledgers (audit, debate, scorecard, tools, history). The new store must state this explicitly rather than leaving it implicit.
- **Read path:** stream-parse with `bufio.Reader` (not `bufio.Scanner`), skip malformed lines with a warning, and skip records whose `schema_version` is greater than the reader understands.

## Relationship to Other Stores

| Store | Path | Scope | Purpose |
|-------|------|-------|---------|
| `internal/scorecard` | `os.UserConfigDir()/atcr/scorecard/` | global, per-user | Reviewer-quality metrics; pattern to copy. |
| `internal/history` | `.planning/history/` | per-repo, private pipeline | Time-windowed finding history; `.atcr/findings-history.jsonl` is legacy read-only. |
| `internal/localdebt` (new) | `.atcr/debt/` | per-repo, public/standalone | Durable TD backlog for users without `.planning/`. |

The new store is deliberately **not** an extension of `internal/history`. History's `.atcr/` path was superseded by `.planning/history/` in Epic 19.4 for the private pipeline; Epic 20.1's `.atcr/debt/` targets a different audience (standalone users) and a different query pattern (resolution backlog, not time-windowed trend history).

## CLI Contract

The persistence hook is triggered from `atcr reconcile` after the scorecard emit block (`cmd/atcr/reconcile.go:110-111`) and is suppressible with `--no-local-debt`, mirroring `--no-scorecard`. The `atcr debt resolve` subcommand (if implemented as a CLI verb) reads `.atcr/debt/*.jsonl` and drives the resolution loop. Alternatively, the skill may read the store directly; that decision is recorded as an integration gap in `codebase-discovery.json`.

## Related Documentation

- `internal/scorecard/store.go` — atomic-append and tolerant read pattern to copy.
- `internal/scorecard/paths.go` — `DefaultDir` and `monthFromRunID` shard derivation.
- `internal/history/record.go` — `FindingID` content-hash construction (`line 48`).
- `internal/reconcile/emit.go` — `JSONFinding` and `SourceReport` fields the record is derived from.
- `internal/reconcile/justification.go` — `stampJustifications` source of `justification`/`source_report`.
- `docs/technical-debt-format.md` — symbol-anchor contract the resolver consumes.
- `docs/scorecard.md` — reference style for local JSONL store documentation.
