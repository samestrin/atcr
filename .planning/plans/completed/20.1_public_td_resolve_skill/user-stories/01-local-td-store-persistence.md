# User Story 1: Local TD Store Persistence

**Plan:** [20.1: Public TD Resolve Skill](../plan.md)

## User Story

**As a** standalone/public atcr user with no `.planning/` directory
**I want** reconciled review findings written to a durable, `.atcr/`-scoped local technical-debt store
**So that** findings from one `atcr review` run survive to accumulate alongside findings from later runs, instead of being lost once the review's own directory is cleaned up or ignored

## Story Context

- **Background:** The private pipeline persists reconciled findings into `.planning/technical-debt/README.md`, giving it a durable cross-run backlog. Standalone/public atcr users have no `.planning/` directory and no equivalent — reconciled findings currently live only inside a single review run's own directory (`.atcr/reviews/<id>/reconciled/findings.json`), so they disappear from view once that run is superseded. This story builds the foundational data layer that every other story in this plan (the `atcr reconcile` persistence hook, and the `/atcr debt resolve` skill route) depends on: the store format itself, and the package that reads/writes it.
- **Assumptions:**
  - The store is scoped to `.atcr/debt/` at the current repo root, mirroring the `.atcr/` scope already used by the rest of the public skill and `cmd/atcr/reconcile.go`'s `Root: "."` convention.
  - `internal/scorecard/store.go`'s Append/ReadRecords/ReadAll pattern (one `os.Write` per record under `O_APPEND`, tolerant streaming read that skips malformed/forward-incompatible lines) is the correct mechanical pattern to copy, differing only in root-path resolution (per-repo `.atcr/debt/` instead of global `os.UserConfigDir()/atcr/scorecard/`).
  - `history.FindingID(file, line, problem)` (`internal/history/record.go:48`, SHA-256 over NUL-separated fields, first 8 hex bytes) is reused verbatim for record identity/dedup, so IDs stay consistent with the rest of the codebase's finding-identity conventions.
  - This store is deliberately **not** a re-extension of `internal/history`, whose `.atcr/` root was intentionally superseded by `.planning/history/` in Epic 19.4 for the private pipeline. This new store targets a different audience (standalone/public, zero `.planning/`) and must document that distinction rather than appear to silently contradict the 19.4 decision.
  - Record schema is documented in `documentation/local-td-store-schema.md` (v1, `schema_version: 1`) — required fields (id, run_id, ts, severity, file, line, problem, fix, category, est_minutes, evidence, reviewers, confidence) plus optional fields (`justification`, `source_report.{path,line,section}`, `status`, `resolved_at`).
- **Constraints:**
  - Stdlib-only implementation (`encoding/json`, `os`, `bufio`, `crypto/sha256`), matching every existing append-only ledger in the codebase — no new dependency.
  - Must work with zero `.planning/` directory present; must not read or write anything under `.planning/`.
  - Must state its concurrency guarantee explicitly (one `Append` call = one `os.Write`, no cross-record batching) and reference the accepted TD-004 won't-fix stance on cross-process `O_APPEND` locking rather than leaving it implicit or inventing new locking.
  - File/directory permissions match the scorecard precedent: directory `0700`, file `0600`, created lazily on first write.
  - `.atcr/debt/` is local, uncommitted state (same as the rest of `.atcr/`) — no version-control interaction is in scope.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** A new `internal/localdebt` package provides `Append(dir string, rec Record) error`, `ReadRecords(path string, opts ReadOpts) ([]Record, error)`, and `ReadAll(dir string, opts ReadOpts) ([]Record, error)` functions operating on month-sharded JSONL files rooted at `.atcr/debt/`, plus a `Record` struct matching the v1 schema and an `ID` field populated via `history.FindingID`.
- **Measurable:** Unit tests cover: a record appended and read back byte-for-byte equivalent; two records appended across separate calls do not tear or interleave; a malformed line and a record with `schema_version` greater than current are both skipped on read (with a warning) rather than aborting the read; `ReadAll` on a missing `.atcr/debt/` directory returns `(nil, nil)` rather than an error.
- **Achievable:** The implementation is a direct structural copy of `internal/scorecard/store.go`'s Append/ReadRecords/ReadAll (already proven in production) with only the root-path resolution and record shape changed — no novel concurrency mechanism required.
- **Relevant:** Without this durable store, `atcr reconcile` (Story 2) has nowhere to persist findings across runs, and `/atcr debt resolve` (Story 3) has no backlog to read from — this is the plan's foundational data layer per the theme assignment.
- **Time-bound:** Deliverable within this sprint's first implementation phase, ahead of the reconcile persistence hook and skill route stories that depend on it.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-package-structure-and-store-operations.md) | Package Structure and Store Operations | Unit |
| [01-02](../acceptance-criteria/01-02-record-identity-via-findingid-reuse.md) | Record Identity via FindingID Reuse | Unit |
| [01-03](../acceptance-criteria/01-03-tolerant-read-path.md) | Tolerant Read Path (Malformed Lines and Schema Versioning) | Unit |
| [01-04](../acceptance-criteria/01-04-concurrency-guarantee-and-package-documentation.md) | Concurrency Guarantee and Package Documentation | Unit |

## Original Criteria Overview

1. `internal/localdebt` package exists with `Record`, `Append`, `ReadRecords`, and `ReadAll`, rooted at `.atcr/debt/` and following the v1 schema documented in `documentation/local-td-store-schema.md`.
2. Record identity/dedup key is `history.FindingID(file, line, problem)`, reused (not reimplemented) from `internal/history/record.go`.
3. The store's concurrency guarantee (single `os.Write` per `Append` call, no cross-process locking) is documented in package-level doc comments, explicitly referencing the accepted TD-004 won't-fix tradeoff already applied to the other five append-only ledgers (audit, debate, scorecard, tools, history).
4. Malformed lines and forward-incompatible `schema_version` records are skipped with a warning on read, never aborting the whole read, matching the scorecard precedent.
5. Package-level documentation explains why `.atcr/` is the correct root for this store despite `internal/history`'s Epic 19.4 migration away from `.atcr/` for the private pipeline.

## Technical Considerations

- **Implementation Notes:**
  - New files: `internal/localdebt/store.go` (Append/ReadRecords/ReadAll, copied structurally from `internal/scorecard/store.go`) and `internal/localdebt/record.go` (the `Record` struct and any local schema-version constant).
  - Path resolution: root at `<cwd>/.atcr/debt/`, month-sharded as `YYYY-MM.jsonl` derived from the record's `run_id` prefix, mirroring `internal/scorecard/paths.go`'s `monthFromRunID`. Repo-root detection follows `cmd/atcr/reconcile.go`'s existing `Root: "."` (CWD) convention; this was the settled path-resolution decision during design sprint refinement.
  - Reuse `history.FindingID` by importing `internal/history` for the ID function only — do not import or extend any of `internal/history`'s `.planning/`-scoped read/write logic.
  - Read path uses `bufio.Reader` (not `bufio.Scanner`) to survive an over-long line without aborting the read, matching `internal/scorecard/store.go`'s `ReadRecords`.
- **Integration Points:**
  - `cmd/atcr/reconcile.go` (Story 2) will call `localdebt.Append` per reconciled finding after the existing scorecard emit block.
  - `/atcr debt resolve` (Story 3) will call `localdebt.ReadAll` to load the backlog.
  - `docs/scorecard.md` is the reference style to mirror for the eventual `docs/`-level documentation of this store (out of scope for this story; covered by Story 5).
- **Data Requirements:** v1 record schema per `documentation/local-td-store-schema.md` — required fields (`schema_version`, `id`, `run_id`, `ts`, `severity`, `file`, `line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewers`, `confidence`) and optional fields (`justification`, `source_report.{path,line,section}`, `status`, `resolved_at`). The dedup strategy is resolved as write-time dedup by `id` (`history.FindingID`) using a full-history `ReadAll` scan before each append; see AC 02-03 and `documentation/local-td-store-schema.md` for the full contract.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Store appears to reintroduce the exact `.atcr/findings-history.jsonl` design Epic 19.4 moved away from, causing review confusion or a push to unify the two stores | Medium | Document explicitly (in package doc comments) that this store targets a different audience (standalone/public, zero `.planning/`) than `internal/history`'s now-`.planning/`-scoped design; do not import or extend `internal/history`'s storage logic, only its `FindingID` helper |
| Concurrent `atcr reconcile` runs appending to the same month shard could tear a JSONL line | Low | Adopt the same accepted TD-004 won't-fix stance as the other five ledgers: one `os.Write` per record under `O_APPEND`, no cross-process lock, documented explicitly rather than left implicit |
| Dedup strategy left undecided causes duplicate or conflicting records once Story 2 starts writing on every reconcile run | Medium | Closed during design: document write-time dedup by `id` (`history.FindingID`) using a full-history `ReadAll` scan before each append. The `internal/localdebt` package comments must state this contract so Story 2 has a settled interface. |

---

**Created:** July 11, 2026
**Status:** Draft - Awaiting Acceptance Criteria
