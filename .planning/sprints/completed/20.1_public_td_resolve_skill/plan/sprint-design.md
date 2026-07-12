# Sprint Design: Plan 20.1: Public TD Resolve Skill

**Created:** July 12, 2026
**Plan:** [Plan 20.1: Public TD Resolve Skill](/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/20.1_public_td_resolve_skill/)
**Plan Type:** Feature Development ✨
**Status:** Design Complete

---

## Original User Request

> Standalone/public `atcr` users have no way to persist findings across review runs into a durable technical-debt backlog, and no autonomous way to resolve them — both capabilities the private workflow already has via `.planning/technical-debt/` + `/resolve-td`. Without this, "public atcr" is a review-only tool while "private atcr" is a review-and-fix tool, which undercuts the goal of releasing atcr as a complete, standalone product.
>
> Proposed solution: (1) design a local, `.atcr/`-scoped technical-debt store informed by `internal/scorecard`'s append-only, month-rotated JSONL pattern; (2) extend the single dispatcher skill with a new `/atcr debt resolve` route adapting `/resolve-td`'s RED→GREEN→ADVERSARIAL→REFACTOR cycle to a `.planning/`-free context; (3) persist reconciled findings across runs via `atcr reconcile`; (4) document the capability in `docs/skill-usage.md`. Refinement added a 5th objective: extract shared skill boilerplate into `skill/CONVENTIONS.md` per Epic 20.0's addendum, since this is the second public skill in the repo.

**Referenced Resources:**
- [Agent Skills Format & Progressive Disclosure](documentation/agent-skills-format.md) — Markdown Agent Skill on-demand secondary-file loading pattern (mirrors `skill/host-review.md`).
- [Append-Only JSONL Store Pattern](documentation/append-only-store-pattern.md) — `internal/scorecard/store.go`'s atomic-append/tolerant-read mechanics to copy into the new `internal/localdebt` package; `internal/history`'s Epic 19.4 migration as the cautionary counter-example.
- [Local TD Store Schema](documentation/local-td-store-schema.md) — concrete v1 record schema, file layout (`.atcr/debt/YYYY-MM.jsonl`), identity/dedup rules, and concurrency guarantee for the new store.
- [CLI Integration Points](documentation/cli-integration-points.md) — the `--no-scorecard`-style opt-out precedent, the existing `atcr debt` Cobra family as the extension point, and confirmation that `Justification`/`SourceReport` are already live.
- [Skill Dispatcher & CONVENTIONS.md Extraction](documentation/skill-dispatcher-conventions.md) — how `/atcr debt resolve` extends (not replaces) the `atcr debt` dispatcher row, and the CONVENTIONS.md extraction shape.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Public TD Resolve Skill
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 5
**Pattern:** Foundation → Core Items → Advanced → Integration & Docs → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
append-only JSONL store Go
Cobra CLI subcommand extension pattern
Markdown agent skill dispatcher design
local technical debt backlog persistence
TDD RED GREEN ADVERSARIAL REFACTOR cycle
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - New patterns throughout (new `internal/localdebt` package, new `skill/debt-resolve/SKILL.md`, new `skill/CONVENTIONS.md`), but every new pattern is a structural copy of an already-proven in-repo precedent (`internal/scorecard/store.go`, `skill/host-review.md`, `/resolve-td`'s stage cycle) — not a major overhaul of existing systems.
- **Integration:** 2/3 - 3+ integration points: `cmd/atcr/reconcile.go` (persistence hook), `cmd/atcr/debt.go` (new `resolve` subcommand), the `skill/skill.go`/`skill/skill_test.go` Go-embed harness plus `skill/SKILL.md` dispatcher table, and `docs/skill-usage.md`. All in-process/same-repo — no cross-service or network integration, so short of "complex multi-system."
- **Story/Task & Test:** 3/3 - 5 stories, 19 ACs spanning unit (13), integration (3), and E2E scenario-walkthrough (3) tests; Story 3 alone carries 6 ACs including the highest-complexity items in the plan (the autonomous resolution cycle, item selection, and branch-safety/outcome persistence).
- **Risk/Unknowns:** 2/3 - Some unknowns remain (fixture-repo E2E walkthrough feasibility for the RGR cycle, drift-relocation ambiguity, cumulative-adversarial-pass sequencing), but the plan's own AC set has already resolved the previously-open design questions explicitly (write-time dedup by `FindingID`, CLI-subcommand-not-direct-file-read, append-only mark-resolved semantics), substantially de-risking implementation.

**Time Formula:** TOTAL_DAYS = Σ(per-story effort days, scaled from S/M/L estimates) + adversarial/gated validation buffer
**Calculation:** Story 1 (M, 1.5d) + Story 2 (M, 1.5d) + Story 3 (L, 3.5d) + Story 4 (S, 0.5d) + Story 5 (S, 1d) + cumulative adversarial pass + DoD validation (2d) = **10 days**

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** false (not strong — score is 9/12, strong threshold is ≥10)
**Suggested command:** `/create-sprint @.planning/plans/active/20.1_public_td_resolve_skill/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3 (met: 9/12, 5 phases); gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days (met: 9/12, 10 days); strong gated at complexity >= 10/12 (not met).

---

## Phase Structure

### Phase 1: Foundation (1.5 days)
**Items:** Story 1 — Local TD Store Persistence
**Focus:** Build the append-only `internal/localdebt` package (`Record`, `Append`, `ReadRecords`, `ReadAll`) that every other story depends on. Nothing downstream can start meaningfully until this store's contract (schema, dedup key, concurrency guarantee, tolerant read) is locked in.

### Phase 2: Core Items (2 days, parallelizable)
**Items:** Story 2 — Reconcile-Time Persistence Hook; Story 4 — Shared Skill Conventions Extraction
**Focus:** Story 2 wires `atcr reconcile` into the Phase 1 store with write-time dedup and a `--no-local-debt` opt-out. Story 4 is independent of Stories 1-2 (pure `skill/SKILL.md` → `skill/CONVENTIONS.md` extraction) and runs in parallel so Story 3 has a finished CONVENTIONS.md to reference rather than a placeholder, per Story 4's own stated sequencing dependency.

### Phase 3: Advanced (3.5 days)
**Items:** Story 3 — `/atcr debt resolve` Skill Route
**Focus:** The centerpiece and highest-complexity story: the `atcr debt resolve` CLI subcommand (read + mark-resolved write path), the new `skill/debt-resolve/SKILL.md` documenting the four-stage RED→GREEN→ADVERSARIAL→REFACTOR cycle adapted from `/resolve-td`, item selection consuming `justification`/`SourceReport`, and branch-safety/outcome-persistence. Gated on Phases 1-2 landing (needs a real store to read and a stable CONVENTIONS.md to reference).

### Phase 4: Integration & Documentation (1.5 days)
**Items:** Story 5 — Document Debt-Resolve in skill-usage.md; cross-cutting dispatcher-table consistency check
**Focus:** Extend `docs/skill-usage.md` with the new capability (Storage/CLI Usage/Privacy Model sections mirroring `docs/scorecard.md`) plus the explicit public/local-vs-private-`.planning/`-scoped disambiguation. Verify `skill/SKILL.md`'s `atcr debt` row, `skill/skill_test.go`'s structural assertions, and the CONVENTIONS.md references across both skill files are internally consistent now that Stories 1-4 have landed.

### Phase 5: Validation (1.5 days)
**Focus:** Cumulative adversarial review across the full diff (per `/resolve-td`'s Phase 2 Step 6 precedent this plan explicitly grounds against), full `go test ./...` + coverage run against the 80% baseline, lint/vet gates, Definition of Done verification per AC, and confirmation that zero `.planning/` references leaked into any new public-facing code path.

---

## Work Decomposition

### Story 1: Local TD Store Persistence (M, No dependencies)
**Testable elements:**
- `internal/localdebt.Append` — one record in, byte-identical record out on read (Unit — [01-01](acceptance-criteria/01-01-package-structure-and-store-operations.md))
- `internal/localdebt` record identity via `history.FindingID` reuse, not reimplementation (Unit — [01-02](acceptance-criteria/01-02-record-identity-via-findingid-reuse.md))
- Tolerant read path: malformed line skipped with warning; forward-incompatible `schema_version` skipped; missing directory → `(nil, nil)` (Unit — [01-03](acceptance-criteria/01-03-tolerant-read-path.md))
- Concurrency guarantee (one `Append` = one `os.Write`, TD-004 won't-fix referenced explicitly) documented in package doc comments (Unit — [01-04](acceptance-criteria/01-04-concurrency-guarantee-and-package-documentation.md))

### Story 2: Reconcile-Time Persistence Hook (M, depends on Story 1)
**Testable elements:**
- `atcr reconcile` persists the run's findings (with `Justification`/`SourceReport` when present) after the scorecard emit block (Integration — [02-01](acceptance-criteria/02-01-persist-reconciled-findings.md))
- `--no-local-debt` opt-out flag mirrors `--no-scorecard`'s shape and best-effort/non-fatal contract (Integration — [02-02](acceptance-criteria/02-02-no-local-debt-opt-out-flag.md))
- Cross-run accumulation with write-time dedup by `FindingID`, dedup-read scoped to full history (`ReadAll`, not just current-month shard), fail-open on a dedup-read error (Integration — [02-03](acceptance-criteria/02-03-cross-run-accumulation-and-dedup.md))

### Story 3: `/atcr debt resolve` Skill Route (L, depends on Stories 1-2; should land alongside/after Story 4)
**Testable elements:**
- `skill/SKILL.md`'s `atcr debt` row documents the new route without inventing subcommand names beyond what's implemented (Unit — [03-01](acceptance-criteria/03-01-skill-md-dispatcher-documentation.md))
- `atcr debt resolve` CLI subcommand: reads via `internal/localdebt.ReadAll`, never touches `.planning/`, discoverable via `atcr debt --help` (Unit — [03-02](acceptance-criteria/03-02-debt-resolve-cli-subcommand.md))
- Item selection consumes `justification`/`SourceReport` when present, deterministic selection rule, CLI-subcommand-only access (not direct file read) (Integration — [03-03](acceptance-criteria/03-03-item-selection-and-justification-consumption.md))
- Four-stage RED→GREEN→ADVERSARIAL→REFACTOR cycle adapted from `/resolve-td`, `llm_support_diff_smell` hard-verdict non-overridable, symbol-anchor relocation for drifted findings, cumulative adversarial pass across the run (E2E — [03-04](acceptance-criteria/03-04-red-green-adversarial-refactor-cycle.md))
- Resolution outcome persisted via append-only mark-resolved record; dedicated `debt-resolve/<date>` branch created only when starting from the default branch; `NEEDS_REVIEW` items never marked resolved (E2E — [03-05](acceptance-criteria/03-05-resolution-outcome-persistence-and-branch-safety.md))
- `skill/skill.go` embeds the new file(s); `skill/skill_test.go` asserts all four stage names present (Unit — [03-06](acceptance-criteria/03-06-go-embed-wiring-and-test-coverage.md))

### Story 4: Shared Skill Conventions Extraction (S, independent of Stories 1-2; should land alongside/before Story 3)
**Testable elements:**
- `skill/CONVENTIONS.md` created with binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules (Unit — [04-01](acceptance-criteria/04-01-conventions-md-creation.md))
- `skill/SKILL.md`'s Prerequisites section rewritten to a pointer, no coverage lost (Unit — [04-02](acceptance-criteria/04-02-skill-md-prerequisites-pointer.md))
- `ConventionsMD` embedded in `skill/skill.go`, added to the existing no-`.claude`/no-absolute-path test list (Unit — [04-03](acceptance-criteria/04-03-go-embed-and-test-coverage.md))

### Story 5: Document Debt-Resolve in skill-usage.md (S, depends on Stories 1-3)
**Testable elements:**
- `/atcr debt resolve` route documentation (purpose, invocation, behavior) (Unit — [05-01](acceptance-criteria/05-01-debt-resolve-route-documentation.md))
- Local `.atcr/`-scoped TD store documentation (location, population, `--no-local-debt` flag) mirroring `docs/scorecard.md` (Unit — [05-02](acceptance-criteria/05-02-local-td-store-storage-section.md))
- Explicit public/local vs. private `.planning/`-scoped `atcr debt` disambiguation, cross-linked to `docs/technical-debt.md` (Unit — [05-03](acceptance-criteria/05-03-public-private-debt-disambiguation.md))

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located `*_test.go` files alongside source (Go convention already used throughout the codebase — `cmd/atcr/reconcile_test.go`, `cmd/atcr/debt_test.go`, `skill/skill_test.go`, `internal/scorecard/*_test.go`).

**Test File Placement Examples:**
- `internal/localdebt/store_test.go`, `internal/localdebt/record_test.go` (new, Story 1)
- `cmd/atcr/reconcile_test.go` (extended, Story 2 — `TestRunReconcile_LocalDebtAccumulatesAcrossRuns`, `TestRunReconcile_LocalDebtDedupsSameFinding`)
- `cmd/atcr/debt_resolve_test.go` (new, Story 3 — flag parsing, empty-store, JSON/table output)
- `skill/skill_test.go` (extended, Stories 3-4 — stage-name assertions, `ConventionsMD` non-empty/referenced checks)

**Unit/Integration/E2E:**
- **Unit (13 ACs):** `internal/localdebt` package behavior; `skill/skill_test.go` structural/embed assertions; doc-presence checks mirroring `internal/scorecard`'s doc test pattern. Run via `go test ./...`.
- **Integration (3 ACs):** Multi-invocation `atcr reconcile` CLI tests against a shared temp `.atcr/debt/` directory, following `cmd/atcr/reconcile_test.go`'s existing style — no mocking, real temp files under `t.TempDir()`.
- **E2E (3 ACs, Story 3 only):** Agent-driven scenario walkthroughs against a fixture repo with a seeded local-store record and a deliberately reproducible bug — this is Markdown-driven agent behavior (`skill/debt-resolve/SKILL.md`), not compiled Go, so `go test` cannot exercise the cycle itself. `skill/skill_test.go` covers only structural/content assertions on the embedded file; the actual RED→GREEN→ADVERSARIAL→REFACTOR walkthrough is validated during Phase 5 as a live scenario run, not a unit test.

**Test Environment Status:**
- Framework: `go test` + `testify/require` (established, per `.planning/.config/config.yaml`'s `default` component)
- Execution: `go test ./...` (project convention command)
- Coverage Tools: `go test -coverprofile=coverage.out ./...`, baseline 80%

---

## Architecture

**Primitives:**
- `internal/localdebt.Record` — v1 JSONL record (required: `schema_version`, `id`, `run_id`, `ts`, `severity`, `file`, `line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewers`, `confidence`; optional: `justification`, `source_report.{path,line,section}`, `status`, `resolved_at`).
- `history.FindingID(file, line, problem)` — reused verbatim (SHA-256, first 8 hex bytes) as the store's identity/dedup key.

**Module Boundaries:**
- `internal/localdebt` — new package, exposes `Append(dir string, rec Record) error`, `ReadRecords(path string, opts ReadOpts) ([]Record, error)`, `ReadAll(dir string, opts ReadOpts) ([]Record, error)`. Imports `internal/history` only for `FindingID`, never its `.planning/`-scoped read/write logic.
- `cmd/atcr/debt.go` / `cmd/atcr/debt_resolve.go` — new `resolve` subcommand extending the existing `newDebtCmd()` family alongside `list`/`add`/`dashboard`.
- `skill/debt-resolve/SKILL.md` + `skill/CONVENTIONS.md` — new on-demand secondary skill files, embedded via `skill/skill.go`, following the exact `host-review.md` pattern.

**External Dependencies:** None new — stdlib only (`encoding/json`, `os`, `bufio`, `crypto/sha256` via `internal/history`), matching every existing append-only ledger (audit, debate, scorecard, tools, history).

**Replaceability:** `internal/localdebt` is a structural sibling to `internal/scorecard`, not a dependent of it — either can be swapped independently since neither imports the other. `skill/debt-resolve/SKILL.md` is loaded on demand exactly like `host-review.md`, so it can be revised without touching `skill/SKILL.md`'s ~500-line budget.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| `.atcr/debt/` file permissions | `internal/localdebt` directory/file creation | Local privilege escalation via world-readable TD records containing code snippets/paths | Directory `0700`, file `0600`, created lazily, matching the scorecard precedent |
| Branch name construction | `skill/debt-resolve/SKILL.md` branch-safety logic | Ref-injection via unsanitized `problem`/`fix` text used to build a branch name | Branch names derived from a fixed template (`debt-resolve/<date>`) plus validated date formatting only — never built from finding text |
| Fix-application scope | RED→GREEN→ADVERSARIAL→REFACTOR cycle | Autonomous edits writing outside the repo working tree, or treating `problem`/`fix`/`justification` text as executable | Every write stays within CWD-rooted repo; text fields are treated as data describing a change, never executed |
| Store path traversal | `atcr debt resolve` flag parsing | A crafted `--severity` or path-like flag value escaping `.atcr/debt/` | Store path stays rooted under `.atcr/debt/` relative to CWD; flag values validated against a fixed enum set |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| `internal/localdebt.ReadAll` dedup check (Story 2) | Hundreds of records/month, full-history scan per reconcile run | < 100ms | `bufio.Reader` streaming read, no full-file buffering; accepted O(existing records) scan at documented ledger scale |
| `atcr debt resolve --list` read | Up to several thousand records | < 1 second | Same streaming read path; no index/database introduced |
| `internal/localdebt.Append` write | One `os.Write` per record | No measurable overhead beyond marshal cost | Single-write-per-record, no cross-record batching (matches TD-004 accepted tradeoff) |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Missing/empty store | No prior `atcr reconcile` run; missing `.atcr/debt/` directory | `ReadAll` returns `(nil, nil)`; CLI prints "no items" and exits 0, never a stack trace |
| Malformed/forward-incompatible records | Corrupt JSONL line; `schema_version` newer than the build understands | Skipped with a warning on read, never aborts the whole read |
| Cross-run dedup boundaries | Same finding recurs across a month boundary; two findings in one run collapse to the same `id`; `problem` text changes after a partial fix | Full-history (`ReadAll`) dedup scope, not per-shard; in-run dedup to at most one write; changed `problem` text is treated as a new record (documented, not a bug) |
| Drifted finding location | Stored `line` no longer matches code after earlier fixes | Relocate via `(symbolName)` anchor; `NEEDS_REVIEW`/skip on ambiguous or impossible relocation rather than guessing |
| Branch starting state | Default branch vs. existing feature branch vs. existing `debt-resolve/*` branch | New branch created only from default branch; resolves in place on an existing non-default branch |
| Adversarial gate unavailable | `llm_support_diff_smell` errors (older binary, non-git tree) | Skip the deterministic gate, proceed to self-review checklist (FIT check still applies) rather than halting the run |

### Defensive Measures Required

- **Input Validation:** CLI flag values (`--severity`, etc.) validated against fixed enums already used elsewhere in `cmd/atcr/debt.go`; no user-supplied path escapes `.atcr/debt/`.
- **Error Handling:** Persistence failures (Story 2 hook, Story 3 mark-resolved write) are best-effort and non-fatal — logged via `cmd.ErrOrStderr()`, never fail `runReconcile`'s exit code or roll back an already-applied fix.
- **Logging/Audit:** Every append is a discrete, timestamped (`ts`/`run_id`) JSONL record — the store itself is the audit trail; no separate logging subsystem needed.
- **Rate Limiting:** N/A — local CLI tool, no network surface.
- **Graceful Degradation:** Missing `.atcr/debt/`, malformed lines, unavailable adversarial-gate tooling, and dedup-read failures all degrade to a documented fallback (empty result, skip-with-warning, fail-open) rather than crashing or blocking the primary `atcr reconcile`/`atcr review` flow.

---

## Risks

**Technical:**
- New `.atcr/`-scoped store visually resembles the `.atcr/findings-history.jsonl` design Epic 19.4 moved away from → Mitigation: package-level doc comments in `internal/localdebt` explicitly state the differing audience (standalone/public vs. `.planning/`-scoped private pipeline); `internal/history` is imported only for `FindingID`, never its storage logic.
- Concurrent `atcr reconcile` runs could tear a JSONL line → Mitigation: adopt the already-accepted TD-004 won't-fix stance explicitly (one `os.Write` per record, no cross-process lock), documented rather than left implicit.
- `/atcr debt resolve`'s adapted cycle could diverge from `/resolve-td`'s proven behavior → Mitigation: Story 3's AC 03-04 grounds every stage explicitly against `/resolve-td`'s documented stage-by-stage semantics, including the non-overridable `llm_support_diff_smell` hard-verdict rule.

**TDD-Specific:**
- Story 3's E2E ACs (03-03, 03-04, 03-05) describe agent-driven Markdown behavior that `go test` cannot exercise directly → Mitigation: `skill/skill_test.go` covers structural/content assertions only (all four stage names present, dispatcher documentation present); the actual cycle is verified via a fixture-repo scenario walkthrough during Phase 5, not a compiled test — sprint execution must budget this as a distinct verification activity, not skip it because "tests pass."
- Sequencing risk: Story 4 (CONVENTIONS.md) must land alongside or before Story 3 so Story 3's new `skill/debt-resolve/SKILL.md` references a finished file rather than a placeholder → Mitigation: Phase 2 bundles Story 4 with Story 2 (parallel, independent), ensuring it's done before Phase 3 begins.

---

**Next:** `/create-sprint @.planning/plans/active/20.1_public_td_resolve_skill/`
