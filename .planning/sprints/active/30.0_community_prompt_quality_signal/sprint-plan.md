# Sprint 30.0: Community Prompt Quality Signal

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 30.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

An opt-in, aggregate, content-free "quality signal" that tells the maintainer which reviewer prompts (persona+model pairs) are over- or under-reporting — derived entirely from Epic 24.0's dismissal outcomes (`wontfix`/`resolved`) already recorded in `.atcr/debt/`. No code, file path, or finding text ever leaves the machine: only per-persona+model dismissed/confirmed counters and identifiers, gated behind an independent opt-in, with a `--preview` flag that shows exactly what would be sent before anyone ever opts in.

### Why This Matters

Reviewer prompts are tuned in production and drift over time — some hallucinate, over-report, or miss real bugs — but atcr currently has no signal telling the maintainer which prompts need attention. This sprint closes that gap without compromising the privacy guarantee that makes atcr usable against private code, and feeds the persona living-library flywheel (drift detection → hermes drafting → community refinement → this signal → resubmission).

### Key Deliverables

- `internal/localdebt`: `Model` field (`SchemaVersion` 1→2), append-only fold-by-ID, per-(persona, model) `AggregateQualitySignal`
- `internal/telemetry/quality_signal.go`: locked 4-field allowlisted payload type + regression test
- `cmd/atcr`: independent `qualitySignalEnabled`/`qualitySignalGate`, `atcr config set quality_signal <bool>`, `--preview` flag
- `cmd/atcr/telemetry_report.go`: maintainer-facing ranked quality report (`--format md|json`)
- Gated transport wiring at the `review`/`reconcile` completion call sites, fail-open on any transport failure
- `docs/telemetry.md`: field allowlist, opt-in mechanism, `--preview` behavior, and the absolute no-code/no-finding-content guarantee (with the `HashPersonaID` caveat restated)

### Success Criteria

- AC1: quality signal is opt-in, aggregate, content-free; `--preview` shows exactly what would be sent; nothing sent by default
- AC2: signal surfaced to the maintainer in a form that identifies over-reporting prompts via a dismissal-rate-descending ranking (over-reporting candidates at the top, best-calibrated at the bottom). True under-reporting (missed bugs) is structurally unobservable from a content-free dismissal signal and is explicitly out of scope — see AC 04-01's scope note
- AC3: `go test ./...` passes; docs document the exact content-free telemetry contract
- All 21 acceptance criteria across 6 user stories pass; coverage ≥80%; lint and vet clean

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

Complexity 10/12 (VERY COMPLEX) → **Strict 🔒** TDD with **Adversarial 🎯** reviews and **Gated 🚧** phase boundaries for all stories.

| Phase | Focus | Story | TDD Mode |
|-------|-------|-------|----------|
| 1 | Foundation — Aggregation & Schema | 1 | Strict + Adversarial (3 sub-cycles) |
| 2 | Independent Opt-In Gate | 2 | Strict + Adversarial (2 sub-cycles) |
| 3 | Local `--preview` Surface | 3 | Strict + Adversarial |
| 4 | Maintainer-Facing Report | 4 | Strict + Adversarial |
| 5 | Gated Transport Wiring | 6 | Strict + Adversarial (2 sub-cycles) |
| 6 | Documentation | 5 | Adversarial (doc-accuracy) |
| Final | Validation | — | Checklist |

**Gated Mode:** `/execute-sprint` stops at each Phase-Boundary Gate (N.LAST). Review findings, fix any CRITICAL/HIGH issues, then resume.

**Adversarial Reviews:** Fresh subagent spawned per GREEN phase. Subagent has no context of the implementation — intentional bias guard. CRITICAL/HIGH findings fixed inline in REFACTOR; MEDIUM/LOW deferred to `tech-debt-captured.md`.

**Severity substitution:** `$INLINE_FIX_LIST = CRITICAL/HIGH`, `$DEFER_LIST = MEDIUM/LOW` (default; `--severity` was not passed).

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command |
|------|------|---------|
| T1: Focused | After each small change | `go test ./internal/localdebt/... -run TestXxx` |
| T2: Module | After completing story element | `go test ./internal/localdebt/... ./internal/telemetry/... ./cmd/atcr/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist

1. Tests (T3): All passing
2. Coverage: ≥80% (`go test -coverprofile=coverage.out ./...`)
3. Lint: No errors (`golangci-lint run`)
4. Vet: Clean (`go vet ./...`)
5. Build: Succeeds (`go build ./...`)

### DoD Report Template

```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z} ACs
Manual Review: [ ] Code reviewed
```

### Commit Process

Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).

```
git add [specific files] && git commit -m "<type>(<scope>): <message>"
```

Commit types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`

---

## Development Standards

### Architecture Principles

- **Black Box Interfaces:** `internal/localdebt` exposes `AggregateQualitySignal([]Record) []QualityRow` — callers never know the fold-by-ID/grouping internals.
- **Single Responsibility:** Schema bump, fold logic, grouping, gate, preview, report, and transport are each isolated to their own file/phase.
- **Replaceable Components:** The sibling `telemetry.Client.Send`-style transport can be swapped without touching aggregation, gate, preview, or report logic (per sprint-design's resolved transport fork).
- **Primitive-First Design:** `QualityRow{Persona, Model string; DismissedCount, ConfirmedCount int}` and the locked `telemetry.QualitySignal` payload are the core primitives flowing through this feature.

### Coding Standards (Go)

- Packages lowercase, single-word: `localdebt`, `telemetry`, `registry`
- Exported types: `PascalCase` (`QualityRow`, `QualitySignal`)
- Error handling: return `error` as last param; wrap with `fmt.Errorf("context: %w", err)`; fail-safe-to-disabled on malformed config (never fail-open); fail-open on transport errors (never alters exit code/stdout)
- Imports: stdlib → third-party → internal (`github.com/samestrin/atcr/`)
- Formatting: `goimports` before every commit
- No new third-party dependency — stdlib only (`crypto/sha256` via existing `HashPersonaID`, `encoding/json`)
- Table-driven tests co-located as `*_test.go`, mirroring `internal/scorecard/aggregate.go`'s established idiom

### Git Strategy

Branch: `feature/30.0_community_prompt_quality_signal` (create from `main` before first commit)

```bash
git checkout -b feature/30.0_community_prompt_quality_signal
```

Commit format: `type(scope): description`

- `feat(localdebt): add Model field via SchemaVersion 2 bump (green)`
- `feat(telemetry): add allowlisted QualitySignal payload type (green)`
- `refactor(qualitysignal): address adversarial review findings`

---

## External Resources

No external documentation sources identified (`documentation/source.md` — no specification cleared the 0.7 relevance threshold). Refer to:
- [plan/sprint-design.md](plan/sprint-design.md) — architecture decisions and risk analysis
- [plan/original-requirements.md](plan/original-requirements.md) — full epic definition

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — Aggregation & Schema

**Focus:** Story 1 — `localdebt.Record` gains an optional `Model` field behind a `SchemaVersion` 1→2 bump; a new aggregation folds the append-only stream by `ID`, groups by `(persona, model)`, and sums dismissed/confirmed counts; a locked allowlisted payload type ships in `internal/telemetry`. This is the sole data source every later phase depends on — nothing here sends anything.

**AC Links:** [01-01](plan/acceptance-criteria/01-01-per-persona-model-aggregation.md), [01-02](plan/acceptance-criteria/01-02-model-field-schema-bump-and-exclusion.md), [01-03](plan/acceptance-criteria/01-03-multi-persona-reviewers-attribution.md), [01-04](plan/acceptance-criteria/01-04-append-only-record-fold-by-id.md), [01-05](plan/acceptance-criteria/01-05-allowlisted-quality-signal-payload-type.md)

**Note:** Decomposed into three separate RED→GREEN sub-cycles per sprint-design's TDD-specific risk mitigation (schema-bump-first, then fold-logic, then grouping/aggregation) to avoid one oversized cycle.

---

### 1.1 [x] **[Model Field Schema Bump — RED](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

**AC:** 01-02

1. Read `internal/localdebt/record.go` (`SchemaVersion` constant, `Record` struct) and `cmd/atcr/reconcile.go` (`persistLocalDebt`, lines ~228-286)
2. Write failing tests in `internal/localdebt/record_test.go`:
   - `TestRecord_SchemaVersionIsTwo` — assert the package constant is now `2`
   - `TestRecord_ModelFieldOmitempty` — assert `Model string` field exists with `json:"model,omitempty"`
   - `TestReadAll_SchemaV1RecordNoModelKey` — a JSONL line with `"schema_version":1` and no `"model"` key decodes with `Model == ""`, no error, no warning
   - `TestReadAll_SchemaV3ForwardIncompatibleSkip` — existing forward-incompatible-skip behavior is unchanged (regression guard)
3. Write failing test in `cmd/atcr/reconcile_test.go`:
   - `TestPersistLocalDebt_PopulatesModelFromAgentStatus` — assert the persisted record's `Model` equals `fanout.AgentStatus.Model` at write time
4. Verify all new tests fail (RED confirmed)

**Files:** `internal/localdebt/record_test.go`, `cmd/atcr/reconcile_test.go` | **Duration:** 2-3 hours

---

### 1.2 [x] **[Model Field Schema Bump — GREEN](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

1. Bump `SchemaVersion` 1 → 2 in `internal/localdebt/record.go`; document the v1→v2 forward/backward-compat contract on the constant per existing convention
2. Add `Model string` (`json:"model,omitempty"`) to `Record`
3. In `cmd/atcr/reconcile.go`'s `persistLocalDebt`, populate `rec.Model` from the in-scope `fanout.AgentStatus.Model` at the existing record-construction site
4. Run T1 after each change; confirm all tests pass (T2)
5. `git commit -m "feat(localdebt): add Model field via SchemaVersion 2 bump (green)"`

**Files:** `internal/localdebt/record.go`, `cmd/atcr/reconcile.go` | **Duration:** 3-4 hours

---

### 1.2.A [x] **[Model Field Schema Bump — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

**Changed Files:** `internal/localdebt/record.go`, `cmd/atcr/reconcile.go`, `internal/localdebt/record_test.go`, `cmd/atcr/reconcile_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 1.2 schema bump`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 1.2]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure?
    - EDGE CASES: Null, empty, boundaries, concurrent access? v1 records with no model key? Empty-string model on a v2 record?
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| MEDIUM | internal/localdebt/record.go:19 | v1→v2 bump makes new records invisible to pre-30.0 binaries (downgrade drops them) | Deferred → TD-001 |
| LOW | cmd/atcr/reconcile.go:331 | Multi-reviewer merged finding attributes only to first reviewer's model | Deferred → TD-002 |

**Action Taken:** No CRITICAL/HIGH → proceed. MEDIUM + LOW appended to `tech-debt-captured.md` (TD-001, TD-002). The schema bump MEDIUM is a pinned sprint contract (AC 01-02); captured, not reverted.

---

### 1.3 [x] **[Model Field Schema Bump — REFACTOR](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
2. Confirm no code, file path, or finding-content field was introduced alongside `Model` — the schema bump adds exactly one attribution field
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(localdebt): address review + clean up schema bump"`

**Duration:** 1-2 hours

---

### 1.4 [x] **[Append-Only Fold-by-ID — RED](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

**AC:** 01-04

1. Read `cmd/atcr/debt_resolve.go`'s `selectOpenDebt`, `isClosedStatus`, `closedStatusRank`/`higherClosedStatus` fold-by-id pattern
2. Write failing tests in `internal/localdebt/qualitysignal_test.go`:
   - `TestFoldByID_OpenPlusTerminalCountsOnce` — an open record + its later terminal record (same `ID`) folds to exactly one terminal entry
   - `TestFoldByID_DivergentTerminalRecordsResolveByPrecedence` — two terminal records for one `ID` (`resolved` + `wontfix`) resolve to the higher-precedence status (`wontfix` outranks `resolved`), deterministic regardless of read order
   - `TestFoldByID_OpenOnlyContributesNothing` — an `ID` with no terminal record folds to nothing
   - `TestFoldByID_TerminalRecordAttributionFieldsWin` — when open and terminal records diverge in `Reviewers`/`Model`, the terminal record's values are used
3. Verify all new tests fail (RED confirmed)

**Files:** `internal/localdebt/qualitysignal_test.go` | **Duration:** 2-3 hours

---

### 1.5 [x] **[Append-Only Fold-by-ID — GREEN](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

1. Create `internal/localdebt/qualitysignal.go`
2. Implement the fold-by-`ID` pass: map keyed by `ID`, O(n) single scan, mirroring `selectOpenDebt`'s pattern but selecting the terminal (not open) record; divergent terminal records resolve via the mirrored `closedStatusRank`/`higherClosedStatus` precedence
3. Run T1 after each change; confirm all tests pass (T2)
4. `git commit -m "feat(localdebt): fold append-only debt stream by ID (green)"`

**Files:** `internal/localdebt/qualitysignal.go` | **Duration:** 3-4 hours

---

### 1.5.A [x] **[Append-Only Fold-by-ID — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

**Changed Files:** `internal/localdebt/qualitysignal.go`, `internal/localdebt/qualitysignal_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 1.5 fold-by-ID`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 1.5]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Divergent terminal records? IDs with only an open record?
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops? Is fold-by-id O(n), not O(n²)?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH/MEDIUM):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| LOW | qualitysignal_test.go | No `deferred` terminal-path test | Covered within-sprint by task 1.7 AC 01-01 deferred tests |
| LOW | qualitysignal.go:22 | Downstream must handle `deferred` as a third state (neither dismissed nor confirmed) | Handled by task 1.8 grouping (only wontfix/resolved create a row; AC 01-01 EC2) |
| LOW | store.go:231 | FoldRecords collapses empty-ID records (unreachable — StampID always non-empty) | Deferred → TD-003 |

**Action Taken:** No CRITICAL/HIGH → proceed. Fold is confirmed O(n), read-order-independent precedence, terminal-attribution-wins. Deferred-state LOWs resolved within-sprint (1.7/1.8); empty-ID LOW → TD-003.

---

### 1.6 [x] **[Append-Only Fold-by-ID — REFACTOR](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

1. Fix CRITICAL/HIGH issues from 1.5.A (if any)
2. Confirm fold-by-id complexity stays O(n) (no per-id linear scan of the whole stream)
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(localdebt): address review + clean up fold-by-id"`

**Duration:** 1-2 hours

---

### 1.7 [x] **[Grouping, Multi-Persona Attribution & Payload Type — RED](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

**AC:** 01-01, 01-03, 01-05

1. Read `internal/scorecard/aggregate.go`'s `Aggregate()` grouping/sort idiom and `internal/telemetry/event.go`'s no-`omitempty` `Event` pattern plus `internal/telemetry/client_test.go`'s `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`
2. Write failing tests in `internal/localdebt/qualitysignal_test.go`:
   - `TestAggregateQualitySignal_SinglePersonaModelMixedStatuses` — 2 dismissed + 1 confirmed folds to one correct `QualityRow`
   - `TestAggregateQualitySignal_MultiplePersonasAndModels` — deterministic per-`(persona, model)` rows, sorted persona then model ascending
   - `TestAggregateQualitySignal_EmptyInputReturnsNonNilEmptySlice`
   - `TestAggregateQualitySignal_NonTerminalStatusExcluded`
   - `TestAggregateQualitySignal_ExcludesEmptyModelRecords` — v1 or empty-`Model` v2 records excluded from every per-model row
   - `TestAggregateQualitySignal_MultiReviewerAttributesToEveryPersona` — 2+ `Reviewers` each get the increment, not just `Reviewers[0]`
   - `TestAggregateQualitySignal_EmptyReviewersContributesNothing`
   - `TestAggregateQualitySignal_DuplicateReviewerEntryDedupedPerRecord`
   - `TestAggregateQualitySignal_Idempotent` — same input twice produces byte-identical output
3. Write failing tests in `internal/telemetry/quality_signal_test.go`:
   - `TestQualitySignal_PayloadHasExactlyFourAllowlistedKeys` — marshal, unmarshal to `map[string]any`, assert `len(m) == 4` and exact key set
   - `TestQualitySignal_ZeroValueStillSerializesAllFourKeys` — no `omitempty` drops a key
   - `TestQualitySignal_PersonaHashedNotRaw` — constructor hashes via `HashPersonaID`, never carries a raw persona name
4. Verify all new tests fail (RED confirmed)

**Files:** `internal/localdebt/qualitysignal_test.go`, `internal/telemetry/quality_signal_test.go` | **Duration:** 3-4 hours

---

### 1.8 [x] **[Grouping, Multi-Persona Attribution & Payload Type — GREEN](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

1. In `internal/localdebt/qualitysignal.go`, implement `AggregateQualitySignal(records []Record) []QualityRow`:
   - Fold by `ID` first (1.5's logic)
   - Exclude records with empty/unresolved `Model`
   - Iterate every entry in `Record.Reviewers` (dedup per-record, skip empty strings), attribute one dismissed/confirmed outcome to each listed persona's `(persona, model)` group
   - Group by `(persona, model)`, sum `DismissedCount`/`ConfirmedCount`, sort deterministically (persona then model ascending) using the `Aggregate()`-style map-of-key + `sort.SliceStable` idiom
   - Return non-nil empty slice for empty/no-match input
2. Create `internal/telemetry/quality_signal.go`: `QualitySignal` struct with exactly 4 fixed fields (persona identifier hashed via `HashPersonaID`, model, dismissed count, confirmed count), no `omitempty`, plus a construction function analogous to `NewTelemetryPersonaRecord` that hashes the persona at the boundary
3. Run T1 after each change; confirm all tests pass (T2)
4. `git commit -m "feat(localdebt,telemetry): per-persona-model aggregation + allowlisted payload type (green)"`

**Files:** `internal/localdebt/qualitysignal.go`, `internal/telemetry/quality_signal.go` | **Duration:** 4-5 hours

---

### 1.8.A [x] **[Grouping, Multi-Persona Attribution & Payload Type — ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

**Changed Files:** `internal/localdebt/qualitysignal.go`, `internal/telemetry/quality_signal.go`, `internal/localdebt/qualitysignal_test.go`, `internal/telemetry/quality_signal_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.8 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 1.8 aggregation + payload type`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 1.8]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure? Does `QualitySignal` carry any field capable of leaking code/path/finding text?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Duplicate/empty `Reviewers` entries? Tied sort keys?
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops? Is this O(n) + O(k log k), not O(n²)?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH/MEDIUM; privacy invariant confirmed holding):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| LOW | qualitysignal.go:73 | Model excluded on exact `== ""`, not trimmed — a whitespace-only model forms its own group instead of being excluded | Fixed in 1.9 (trim, align with Status normalization) + lock test |
| LOW | qualitysignal.go:88 | Persona skipped on exact `== ""`, not trimmed — whitespace persona forms its own group | Fixed in 1.9 (trim) + lock test |

**Action Taken:** No CRITICAL/HIGH. Privacy invariant confirmed (4 no-omitempty primitive fields, no embedding, persona hashed at boundary). The two LOW whitespace-consistency nits are in the privacy-critical aggregation path and are addressed in 1.9 REFACTOR (trim Model/persona to match the existing Status trimming) with a lock test.

---

### 1.9 [x] **[Grouping, Multi-Persona Attribution & Payload Type — REFACTOR](plan/user-stories/01-aggregate-per-persona-model-dismissal-counters.md)**

1. Fix CRITICAL/HIGH issues from 1.8.A (if any)
2. Confirm `QualitySignal`'s field set cannot structurally carry code/path/finding-content (fixed fields, no embedding of `Record` or `Event`)
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(localdebt,telemetry): address review + clean up aggregation"`

**Duration:** 1-2 hours

---

### 1.10 [x] **Phase 1 DoD Verification**

```
Story-1 DoD Complete
Auto: 5/5 | Story-Specific: 5/5 ACs
Manual Review: [ ] Code reviewed (adversarial 1.2.A + 1.5.A + 1.8.A, REFACTOR 1.3 + 1.6 + 1.9)
```

- [x] T3: `go test ./internal/localdebt/... ./internal/telemetry/... ./cmd/atcr/...` — all passing
- [x] Coverage ≥80% for `internal/localdebt/` (85.1%), `internal/telemetry/` (92.2%)
- [x] `golangci-lint run` — 0 issues
- [x] `go vet ./...` — clean
- [x] Build: `go build ./...` — succeeds
- [x] AC 01-01: `AggregateQualitySignal` groups by `(persona, model)`, sums dismissed/confirmed correctly, deterministic order ✓
- [x] AC 01-02: `SchemaVersion` 2, `Model` field, `persistLocalDebt` populates it, v1 records excluded from per-model rows ✓
- [x] AC 01-03: Multi-persona `Reviewers` attribution to every listed persona, deduped per-record ✓
- [x] AC 01-04: Open+terminal fold-by-ID counts once; divergent terminals resolve by precedence ✓
- [x] AC 01-05: `QualitySignal` has exactly 4 non-`omitempty` fields; persona hashed (byte-equivalent to `HashPersonaID`, test-locked) ✓

---

### 1.11 [x] **Phase 1 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 1 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 1 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: All phase-exit contracts honored (`AggregateQualitySignal` signature, `QualityRow`/`QualitySignal` shapes)?
    - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
    - INTEGRATION: Cross-module calls correct (`persistLocalDebt` → `Record.Model`), no hidden coupling introduced?
    - PHASE-EXIT CONTRACT: Phases 2-6 can consume `AggregateQualitySignal`/`QualitySignal` without rework?
    - REGRESSION: Earlier-phase behavior still intact (v1 records still read without error)?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Gate findings (fresh subagent) — HIGH found, fixed, gate re-run clean:**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| HIGH | reconcile.go resolveRecordModel + qualitysignal.go | Cross-model merged finding credited every persona to the first reviewer's model, corrupting the per-(persona,model) signal | FIXED (commit 8804074a): resolveRecordModel returns "" when reviewers span 2+ distinct models → excluded, not mis-attributed. Locked by TestResolveRecordModel + TestPersistLocalDebt_CrossModelMergeExcludedFromModelAttribution |
| MEDIUM | reconcile.go resolveRecordModel | Known+unrecorded-model merge credits a persona to a sibling's model (pre-existing, not a regression, uncommon) | Deferred → TD-004 (needs per-persona-model schema, bundled with TD-002) |
| LOW | record.go Record doc | "v1 schema" doc drift after the bump | FIXED in commit 8804074a |

**Action Taken:** HIGH fixed before the phase boundary and the gate was re-run by a fresh subagent → "HIGH resolved, tests lock it, no regression." MEDIUM deferred to TD-004. Phase gate passed.

**Duration:** 15-30 min

---

## Phase 2: Independent Opt-In Gate

**Focus:** Story 2 — `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool` (opt-**IN** shape, fail-closed on unset) plus `qualitySignalGate()`, structurally mirroring but never sharing state with `telemetryEnabled`/`telemetryGate`. `LoadQualitySignalSetting`/`SetQualitySignalSetting` reuse `withConfigLock`/`configMapping`/`setMappingBool` verbatim. `runConfigSet`'s allowlist extended to a two-key switch. No payload, no network call.

**AC Links:** [02-01](plan/acceptance-criteria/02-01-quality-signal-off-by-default.md), [02-02](plan/acceptance-criteria/02-02-independent-truth-table-no-shared-state.md), [02-03](plan/acceptance-criteria/02-03-config-set-quality-signal-persists.md)

---

### 2.1 [x] **[Opt-In Gate Truth Table — RED](plan/user-stories/02-quality-signal-opt-in-gate.md)**

**AC:** 02-01, 02-02

1. Read `cmd/atcr/telemetry.go`'s `telemetryEnabled`/`telemetryGate` and `cmd/atcr/cloudsync.go`'s `resolveSyncCloud` as the structural (not semantic) precedent
2. Write failing tests in `cmd/atcr/qualitysignal_test.go`:
   - `TestQualitySignalEnabled_SixCellMatrix` — table test for all six meaningful `{envEnabled: true/false} x {cfg: nil/&true/&false}` cells per the opt-in (OR-enables) truth table
   - `TestQualitySignalGate_DisabledWithNoEnvNoConfig`
   - `TestQualitySignalGate_DisabledWithUnrelatedConfigKeysOnly`
   - `TestQualitySignalGate_IndependentFromTelemetrySetting` — `telemetry: false` + `quality_signal: true` resolves quality-signal enabled, and vice versa
   - `TestQualitySignalGate_IndependentFromSyncCloud` — a valid `ATCR_API_KEY`/`--sync-cloud` state has no bearing on the gate
   - `TestQualitySignalGate_ReEvaluatedFreshPerInvocation` — no stale in-process cache
3. Verify all new tests fail (RED confirmed)

**Files:** `cmd/atcr/qualitysignal_test.go` | **Duration:** 2-3 hours

---

### 2.2 [x] **[Opt-In Gate Truth Table — GREEN](plan/user-stories/02-quality-signal-opt-in-gate.md)**

1. Create `cmd/atcr/qualitysignal.go`
2. Implement `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool = envEnabled || (cfgQualitySignal != nil && *cfgQualitySignal)` — pure, total, opt-in shape
3. Implement `qualitySignalGate() bool` I/O wrapper reading the env var and `registry.LoadQualitySignalSetting(".")`; neither reads nor calls `telemetryGate()`/`resolveSyncCloud()`
4. Run T1 after each change; confirm all tests pass (T2)
5. `git commit -m "feat(qualitysignal): independent opt-in gate truth table (green)"`

**Files:** `cmd/atcr/qualitysignal.go` | **Duration:** 3-4 hours

---

### 2.2.A [x] **[Opt-In Gate Truth Table — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-quality-signal-opt-in-gate.md)**

**Changed Files:** `cmd/atcr/qualitysignal.go`, `cmd/atcr/qualitysignal_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 2.2 opt-in gate`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 2.2]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure? Could the gate ever resolve enabled with no explicit opt-in?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Does the gate share ANY state with `telemetryGate`/`resolveSyncCloud`?
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH; gate logic + independence + fail-safe all verified correct):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| MEDIUM | qualitysignal_test.go | No gate-level malformed-config regression test — only the env path covers fail-safe; the privacy release-gate "corrupt `quality_signal` value → disabled" case was unguarded | Fixed in 2.3 (added `TestQualitySignalGate_MalformedConfigFailsSafeToDisabled`) |
| LOW | qualitysignal.go:37 | "read once per run" doc comment over-promises given the gate is re-evaluated per call | Fixed in 2.3 (comment softened) |

**Action Taken:** No CRITICAL/HIGH → adversarial review passed. The two MEDIUM/LOW findings are a privacy-release-gate test-coverage gap and a doc-comment nit — both cheap and strengthening (not behavior changes), addressed inline in 2.3 REFACTOR rather than deferred, matching the AC 02-03 release-gate coverage the sprint mandates.

---

### 2.3 [x] **[Opt-In Gate Truth Table — REFACTOR](plan/user-stories/02-quality-signal-opt-in-gate.md)**

1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
2. Confirm zero shared boolean/precedence table with `telemetryGate`/`resolveSyncCloud` (structural, not just behavioral, independence)
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(qualitysignal): address review + clean up gate"`

**Duration:** 1-2 hours

---

### 2.4 [x] **[Config Persistence — RED](plan/user-stories/02-quality-signal-opt-in-gate.md)**

**AC:** 02-03

1. Read `internal/registry/telemetry_setting.go`'s `LoadTelemetrySetting`/`SetTelemetrySetting`/`withConfigLock`/`configMapping`/`setMappingBool` and `cmd/atcr/config.go`'s `runConfigSet` allowlist (line ~59)
2. Write failing tests in `internal/registry/quality_signal_setting_test.go`:
   - `TestLoadQualitySignalSetting_AbsentFileReturnsNilNil`
   - `TestSetQualitySignalSetting_RoundTrip`
   - `TestSetQualitySignalSetting_SiblingKeyPreserved` — setting `quality_signal` leaves `telemetry` untouched, and vice versa
   - `TestLoadQualitySignalSetting_MalformedValueFailsSafeToDisabled`
   - `TestSetQualitySignalSetting_SymlinkRejected`
3. Write failing tests in `cmd/atcr/config_test.go`:
   - `TestConfigSetQualitySignal_PersistsTrueFalse`
   - `TestConfigSetQualitySignal_UnknownKeyStillRejected` — allowlist now `{"telemetry", "quality_signal"}`
   - `TestConfigSetQualitySignal_NonBooleanValueRejected`
   - `TestConfigSetQualitySignal_ResolvesRepoRoot`
4. Verify all new tests fail (RED confirmed)

**Files:** `internal/registry/quality_signal_setting_test.go`, `cmd/atcr/config_test.go` | **Duration:** 2-3 hours

---

### 2.5 [x] **[Config Persistence — GREEN](plan/user-stories/02-quality-signal-opt-in-gate.md)**

1. Create `internal/registry/quality_signal_setting.go`: `LoadQualitySignalSetting(root string) (*bool, error)` / `SetQualitySignalSetting(root string, enabled bool) error`, reusing `withConfigLock`/`configMapping`/`setMappingBool` verbatim (same atomic mkdir-lock write path, symlink rejection)
2. Extend `cmd/atcr/config.go`'s `runConfigSet` key allowlist from the single `"telemetry"` literal to an explicit two-key switch (`"telemetry"`, `"quality_signal"`); update `newConfigSetCmd`'s `Long` help text
3. Run T1 after each change; confirm all tests pass (T2)
4. `git commit -m "feat(registry,config): persist quality_signal config key (green)"`

**Files:** `internal/registry/quality_signal_setting.go`, `cmd/atcr/config.go` | **Duration:** 3-4 hours

---

### 2.5.A [x] **[Config Persistence — ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-quality-signal-opt-in-gate.md)**

**Changed Files:** `internal/registry/quality_signal_setting.go`, `cmd/atcr/config.go`, `internal/registry/quality_signal_setting_test.go`, `cmd/atcr/config_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 2.5 config persistence`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 2.5]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure? Is the key allowlist an explicit switch, not a loosened prefix match?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Empty config file? Symlinked config?
    - ERROR HANDLING: Missing catches, swallowed errors? Does a malformed value fail safe to disabled, never silently re-enable?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH; faithful mirror of SetTelemetrySetting confirmed — verbatim shared helpers, exact-match allowlist, shared lock, malformed→disabled):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| MEDIUM | quality_signal_setting_test.go | The documented missing-file I/O-error contract for `SetQualitySignalSetting` is untested (only telemetry's is) | Fixed in 2.6 (added `TestSetQualitySignalSetting_MissingFileIsError`) |
| LOW | quality_signal_setting_test.go | `Set`-side empty-config synthesize path uncovered for quality_signal | Fixed in 2.6 (added empty-config subtest to round-trip) |
| LOW | config.go:65-97 | Two independent `switch key` blocks can drift — a future key added to the allowlist but not the dispatch would silently persist nothing | Fixed in 2.6 (added `default` panic-guard to dispatch switch so drift fails loudly) |

**Action Taken:** No CRITICAL/HIGH → adversarial review passed. All three findings are cheap, strengthening improvements (two coverage additions on documented contracts + a latent-trap guard), addressed inline in 2.6 REFACTOR under the step's "clean up" remit.

---

### 2.6 [x] **[Config Persistence — REFACTOR](plan/user-stories/02-quality-signal-opt-in-gate.md)**

1. Fix CRITICAL/HIGH issues from 2.5.A (if any)
2. Confirm the malformed-value path fails safe to disabled and surfaces a loud error, never a silent re-enable
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(registry,config): address review + clean up config persistence"`

**Duration:** 1-2 hours

---

### 2.7 [x] **Phase 2 DoD Verification**

```
Story-2 DoD Complete
Auto: 5/5 | Story-Specific: 3/3 ACs
Manual Review: [ ] Code reviewed (adversarial 2.2.A + 2.5.A, REFACTOR 2.3 + 2.6)
```

- [x] T3: `go test ./cmd/atcr/... ./internal/registry/...` — all passing
- [x] Coverage ≥80% — `cmd/atcr` 85.8%, `internal/registry` 89.8%
- [x] `golangci-lint run ./...` — 0 issues
- [x] `go vet ./...` — clean
- [x] Build: `go build ./...` — succeeds
- [x] AC 02-01: gate resolves `false` with no env var and no persisted config key ✓
- [x] AC 02-02: six-cell truth table correct; independence from `telemetryGate`/`resolveSyncCloud` proven ✓
- [x] AC 02-03: `atcr config set quality_signal <bool>` persists atomically, sibling keys untouched, malformed value fails safe ✓

---

### 2.8 [x] **Phase 2 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 2 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 2 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 2 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: All phase-exit contracts honored (`qualitySignalGate()` signature, config key semantics)?
    - CONFIG SURFACE: `quality_signal` key documented, defaulted, back-compat with existing `telemetry` key?
    - INTEGRATION: `runConfigSet`'s two-key switch correct, no cross-key interference?
    - PHASE-EXIT CONTRACT: Phase 3 (`--preview`) and Phase 5 (transport) can consume `qualitySignalGate()` without rework?
    - REGRESSION: Existing `atcr config set telemetry <bool>` behavior byte-identical?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Gate findings (fresh subagent, hostile integrator — no CRITICAL/HIGH/MEDIUM; CONTRACT-EXIT / CONFIG-SURFACE back-compat / INTEGRATION / INDEPENDENCE / FAIL-SAFE / REGRESSION all PASS):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| LOW | project.go DefaultProjectConfigYAML | `atcr init` template surfaces telemetry but not the quality_signal key (discoverability parity) | Deferred → TD-005 |
| LOW | qualitysignal.go qualitySignalGate | Gate reads config cwd-relative while config-set writes repo-root — a faithful mirror of the pre-existing telemetryGate asymmetry; fails safe to OFF for the opt-in | Deferred → TD-006 |

**Action Taken:** No CRITICAL/HIGH/MEDIUM → **Phase gate passed.** Adding `QualitySignal *bool` to the strict ProjectConfig is the necessary forward-compat fix (a persisted `quality_signal` key now passes `KnownFields(true)` roster load instead of erroring); absent key → nil → default-DISABLED; no existing config carried the key, so no load regression. Existing `config set telemetry` tests untouched and green. Two LOW findings deferred to `tech-debt-captured.md` (TD-005, TD-006).

**Duration:** 15-30 min

---

## Phase 3: Local `--preview` Surface

**Focus:** Story 3 — `--preview` bool flag via a new `addQualitySignalFlags`-style helper (chained-`PreRunE`, mirroring `addSyncCloudFlags`). The run path builds Phase 1's payload first, then branches: `--preview` set → `json.MarshalIndent` to stdout, return before any opt-in check or client construction. A regression test proves the preview marshals the identical struct/function the real send uses.

**AC Links:** [03-01](plan/acceptance-criteria/03-01-preview-flag-prints-exact-payload.md), [03-02](plan/acceptance-criteria/03-02-preview-bypasses-network-and-optin-gate.md), [03-03](plan/acceptance-criteria/03-03-preview-never-drifts-from-real-send.md)

---

### 3.1 [x] **[Local `--preview` Surface — RED](plan/user-stories/03-local-preview-of-outbound-quality-signal-payload.md)**

**AC:** 03-01, 03-02, 03-03

1. Read `cmd/atcr/flags.go`'s `addSyncCloudFlags`/`addRangeFlags` chained-`PreRunE` pattern and `internal/telemetry/client_test.go`'s `SetDoRequestForTest`/`TestClient_Send_EmptyEndpointNoOps` seam
2. Write failing tests in `cmd/atcr/qualitysignal_test.go`:
   - `TestPreview_PrintsAllowlistedJSONPayload` — pretty-printed JSON with exactly the allowlisted fields, exit 0
   - `TestPreview_IncludesNotSentMarker` — human-readable "nothing was transmitted" line, distinct from the JSON
   - `TestPreview_EmptyAggregationPrintsEmptyPayloadNotError`
   - `TestPreview_TakesPrecedenceOverSyncCloud` — `--preview` + `--sync-cloud` together → only the payload prints, no cloud push
   - `TestPreview_ZeroHTTPCalls_GateDisabled` — via `SetDoRequestForTest` counter, gate disabled (default)
   - `TestPreview_ZeroHTTPCalls_GateEnabled` — same counter, gate enabled
   - `TestPreview_WorksWithNoAPIKey`
   - `TestPreview_UnaffectedByMalformedConfig` — identical behavior regardless of a malformed persisted `quality_signal` value
   - `TestPreview_ByteIdenticalToRealSendMarshal` — shared-helper equivalence test (table-driven, 3+ fixtures)
   - `TestPreview_GoldenRoundTrip` — unmarshal preview output back → `reflect.DeepEqual` to the original struct
3. Verify all new tests fail (RED confirmed)

**Files:** `cmd/atcr/qualitysignal_test.go` | **Duration:** 3-4 hours

---

### 3.2 [x] **[Local `--preview` Surface — GREEN](plan/user-stories/03-local-preview-of-outbound-quality-signal-payload.md)**

1. Add an `addQualitySignalFlags`-style helper in `cmd/atcr/flags.go` registering `--preview` via chained `PreRunE`
2. In both host commands' run paths (`cmd/atcr/review.go` and `cmd/atcr/reconcile.go` — `--preview` is registered on both, matching Story 6's two call sites), add a single shared payload-construction helper; branch: `--preview` set → `json.MarshalIndent` to stdout + "not sent" marker line, return before any `qualitySignalGate()` check or transport/client construction
3. Run T1 after each change; confirm all tests pass (T2)
4. `git commit -m "feat(flags,qualitysignal): add --preview surface (green)"`

**Files:** `cmd/atcr/flags.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go` | **Duration:** 4-5 hours

---

### 3.2.A [x] **[Local `--preview` Surface — ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-local-preview-of-outbound-quality-signal-payload.md)**

**Changed Files:** `cmd/atcr/flags.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `cmd/atcr/qualitysignal_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 3.2 --preview surface`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 3.2]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure? Can `--preview` ever trigger a real network call under any gate/flag combination?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Empty aggregation? `--preview` + `--sync-cloud` together?
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH; privacy invariant confirmed — 4 allowlisted fields, persona hashed at boundary, no network/gate/key on the preview path, empty store → `[]`):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| MEDIUM | review.go:174 / flags.go:16-34 | Docstring claims `--preview` runs "before any precondition", but `addRangeFlags`' `PreRunE` (a pure flag-relationship check — no I/O, no network, no credentials) still runs before `RunE` under real `Execute()` | Fixed in 3.3 (reworded docstrings to not overstate; range validation still applies and violates no AC — no network/gate/key) |
| MEDIUM | qualitysignal_test.go:229 | Every preview test drove `cmd.RunE` directly, bypassing the real `PreRunE`/`Execute()` path | Fixed in 3.3 (`TestPreview_EndToEndThroughExecute` runs `review`/`reconcile --preview` and `--preview --sync-cloud` through the full root `ExecuteContext`) |
| LOW | qualitysignal_test.go | `expectedQualityPayload` re-derives `buildQualitySignalPayload`, making the equivalence tests near-tautological | Fixed in 3.3 (`TestPreview_PayloadHashesPersonaIndependently` computes the SHA-256 independently via `crypto/sha256` and asserts the raw name never appears) |
| LOW | qualitysignal.go:136 | A non-ENOENT debt-store read failure surfaces on the preview path as exit 1, not the surrounding exit-2 usage-error convention | Fixed in 3.3 (wrapped with `usageError`) |

**Action Taken:** No CRITICAL/HIGH → adversarial review passed. All four MEDIUM/LOW findings are cheap, strengthening (doc accuracy + coverage + exit-code consistency), addressed inline in 3.3 REFACTOR rather than deferred, matching the prior phases' pattern.

---

### 3.3 [x] **[Local `--preview` Surface — REFACTOR](plan/user-stories/03-local-preview-of-outbound-quality-signal-payload.md)**

1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
2. Confirm the `--preview` branch short-circuits before any `net/http` client construction or DNS resolution, under every gate state
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(qualitysignal): address review + clean up --preview"`

**Duration:** 1-2 hours

---

### 3.4 [x] **Phase 3 DoD Verification**

```
Story-3 DoD Complete
Auto: 5/5 | Story-Specific: 3/3 ACs
Manual Review: [ ] Code reviewed (adversarial 3.2.A, REFACTOR 3.3)
```

- [x] T3: `go test ./cmd/atcr/...` — all passing (full `go test ./...` also green: 42 pkgs)
- [x] Coverage ≥80% — `cmd/atcr` 86.3%
- [x] `golangci-lint run` — 0 issues
- [x] `go vet ./...` — clean
- [x] Build: `go build ./...` — succeeds (pre-commit gate passed)
- [x] AC 03-01: `--preview` prints exact allowlisted JSON + "not sent" marker ✓
- [x] AC 03-02: zero HTTP calls under gate-disabled and gate-enabled states; no API key needed ✓
- [x] AC 03-03: byte-identical to real-send marshal path; golden round-trip passes ✓

---

### 3.5 [x] **Phase 3 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 3 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 3 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 3 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: `--preview` flag registration and short-circuit ordering honored?
    - CONFIG SURFACE: No new config keys introduced by this phase; confirm none were added accidentally
    - INTEGRATION: Shared payload-construction helper genuinely shared, not duplicated between preview and send paths?
    - PHASE-EXIT CONTRACT: Phase 5 (transport) can call the same shared helper without rework?
    - REGRESSION: `--sync-cloud`'s own scorecard push (unrelated to quality-signal) unaffected?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Gate findings (fresh subagent, hostile integrator — no CRITICAL/HIGH; CONTRACT-EXIT / CONFIG-SURFACE (no new key) / INTEGRATION (single-source helper) / REGRESSION (non-preview paths + --sync-cloud scorecard push untouched) all PASS):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| MEDIUM | flags.go addSyncCloudFlags | `--preview --sync-cloud` still printed the sync-cloud placeholder warning to stderr (PreRunE fires before the RunE preview short-circuit) — misleading noise on a pure, side-effect-free render | FIXED before boundary (commit `9b347904`): warning suppressed when `--preview` is set (`previewFlagSet`); locked by `TestPreview_EndToEndThroughExecute` review+reconcile `--sync-cloud` no-warning subtests |
| LOW | qualitysignal_test.go | Execute-path coverage tested `review --preview --sync-cloud` but not `reconcile --preview --sync-cloud` | FIXED in same commit (added the reconcile Execute-path subtest) |
| LOW | flags.go addQualitySignalFlags | Chained PreRunE re-invokes prev and adds no validation — reads like a dead wrapper | No change: the chained-`PreRunE` helper is AC-mandated (AC 03-01 "mirroring `addSyncCloudFlags`"); the existing doc comment documents it as an intentional extension seam |
| LOW | review.go runReview | `--preview` silently overrides `--resume`/`--force`/`--auto-fix`/positional arg with no diagnostic | Deferred → TD-008 (intended preview-precedence; discoverability-only, no functional/privacy impact) |

**Action Taken:** No CRITICAL/HIGH → **Phase gate passed.** The one MEDIUM (misleading sync-cloud warning on the pure preview path) was fixed before the phase boundary and the gate re-validated (build/vet/lint/tests green); the accompanying LOW coverage gap was closed in the same commit. The AC-mandated PreRunE wrapper is correct as-is; the preview-precedence discoverability LOW is deferred to `tech-debt-captured.md` (TD-008).

**Duration:** 15-30 min

---

## Phase 4: Maintainer-Facing Report

**Focus:** Story 4 — New `cmd/atcr/telemetry_report.go` subcommand mirroring `newReportCmd`/`runReport`'s shape (`--format md|json`), sourcing exclusively from Phase 1's `[]QualityRow` aggregation (never `internal/reconcile` or raw findings). Ranks rows by dismissal rate descending. Empty aggregation renders a clean "no data" state. Registered as a distinct subcommand alongside — never modifying — `atcr report`.

**AC Links:** [04-01](plan/acceptance-criteria/04-01-ranked-quality-report-rendering.md), [04-02](plan/acceptance-criteria/04-02-content-free-privacy-guarantee.md), [04-03](plan/acceptance-criteria/04-03-empty-aggregation-no-data-state.md), [04-04](plan/acceptance-criteria/04-04-distinct-subcommand-registration.md)

---

### 4.1 [x] **[Maintainer-Facing Report — RED](plan/user-stories/04-maintainer-facing-prompt-quality-report.md)**

**AC:** 04-01, 04-02, 04-03, 04-04

1. Read `cmd/atcr/report.go`'s `newReportCmd`/`runReport` shape and `cmd/atcr/main.go`'s `root.AddCommand(...)` registration list
2. Write failing tests in `cmd/atcr/telemetry_report_test.go`:
   - `TestQualityReport_RankedByDismissalRateDescending` — hand-computed fixture, md format
   - `TestQualityReport_JSONFormatMatchesMDRankOrder`
   - `TestQualityReport_TiedRatesTieBreakDeterministic` — persona then model ascending
   - `TestQualityReport_UnsupportedFormatUsageError` — exit 2, before any data read
   - `TestQualityReport_UnderlyingReadErrorExitsOne` — wrapped error, not a panic
   - `TestQualityReport_EmptyAggregationNoDataMessage_MD`
   - `TestQualityReport_EmptyAggregationNoDataMessage_JSON` — well-formed `[]`, not `null`
   - `TestQualityReport_EmptyDoesNotConflateWithReadFailure`
   - `TestQualityReport_SubsequentRunWithDataRendersFullTable`
3. Write failing test in `cmd/atcr/telemetry_report_import_test.go`:
   - `TestTelemetryReport_NoReconcileImport` — static import-graph assertion (no `internal/reconcile` import, no `readReconciledFindings` call)
4. Write failing test in `cmd/atcr/main_test.go`:
   - `TestCommandTree_QualityReportDistinctFromReport` — no name collision, `atcr report` output byte-identical to before
5. Verify all new tests fail (RED confirmed)

**Files:** `cmd/atcr/telemetry_report_test.go`, `cmd/atcr/telemetry_report_import_test.go`, `cmd/atcr/main_test.go` | **Duration:** 3-4 hours

---

### 4.2 [x] **[Maintainer-Facing Report — GREEN](plan/user-stories/04-maintainer-facing-prompt-quality-report.md)**

1. Create `cmd/atcr/telemetry_report.go`: `newQualityReportCmd()`/`runQualityReport()` — `localdebt.ReadAll` + `AggregateQualitySignal`, rank by dismissal rate (`Dismissed / (Dismissed + Confirmed)`) descending, tie-break persona then model ascending; `--format md|json`; `len(rows) == 0` guard renders a clear "no data" message (exit 0) in both formats, distinct from a genuine read error (exit 1)
2. Register the new subcommand in `cmd/atcr/main.go`'s `root.AddCommand(...)` alongside `newReportCmd()`, with cross-referencing `Short`/`Long` help text
3. Run T1 after each change; confirm all tests pass (T2)
4. `git commit -m "feat(telemetry_report): add maintainer-facing quality report subcommand (green)"`

**Files:** `cmd/atcr/telemetry_report.go`, `cmd/atcr/main.go` | **Duration:** 4-5 hours

---

### 4.2.A [x] **[Maintainer-Facing Report — ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-maintainer-facing-prompt-quality-report.md)**

**Changed Files:** `cmd/atcr/telemetry_report.go`, `cmd/atcr/main.go`, `cmd/atcr/telemetry_report_test.go`, `cmd/atcr/telemetry_report_import_test.go`, `cmd/atcr/main_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 4.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 4.2 maintainer report`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 4.2]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure? Does the render path import or reach `internal/reconcile`/raw findings by any path?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Single row? Tied rates? Divide-by-zero in dismissal rate?
    - ERROR HANDLING: Missing catches, swallowed errors? Read failure conflated with "no data"?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH; content-free invariant confirmed — render imports only encoding/json/fmt/io/sort/strings/localdebt/cobra, no internal/reconcile/readReconciledFindings, QualityRow reads only Reviewers/Model/Status; exit-code classes, empty-vs-read-failure, divide-by-zero guard, and tie-break determinism all verified correct):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| LOW | telemetry_report.go:152 | Markdown table cells interpolate persona/model via raw `%s` with no `\|`/newline escaping — a slug containing a pipe or newline would break table structure. Not a privacy leak (persona/model are allowlisted, content-free) and unreachable today (catalog-controlled slugs), but the render layer does not hold the aggregate's "enforced structurally, not left to input hygiene" line | Fixed in 4.3 (escape `\|`/newline in md cells + lock test), matching prior phases' inline-strengthening pattern |

**Action Taken:** No CRITICAL/HIGH → adversarial review passed. The single LOW is a cheap, privacy-adjacent defense-in-depth strengthening on the markdown render layer, addressed inline in 4.3 REFACTOR (consistent with 1.9/2.3/2.6/3.3) rather than deferred.

---

### 4.3 [x] **[Maintainer-Facing Report — REFACTOR](plan/user-stories/04-maintainer-facing-prompt-quality-report.md)**

1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
2. Confirm `atcr report`'s output and behavior are byte-for-byte unchanged after this story
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(telemetry_report): address review + clean up report command"`

**Duration:** 1-2 hours

---

### 4.4 [x] **Phase 4 DoD Verification**

```
Story-4 DoD Complete
Auto: 5/5 | Story-Specific: 4/4 ACs
Manual Review: [ ] Code reviewed (adversarial 4.2.A, REFACTOR 4.3)
```

- [x] T3: `go test ./cmd/atcr/...` — all passing (full `go test ./...` also green)
- [x] Coverage ≥80% — `cmd/atcr` 86.0%
- [x] `golangci-lint run` — 0 issues
- [x] `go vet ./...` — clean
- [x] Build: `go build ./...` — succeeds
- [x] AC 04-01: ranked by dismissal rate descending, md+json parity, deterministic tie-break ✓
- [x] AC 04-02: renders only allowlisted fields; static import test blocks `internal/reconcile` ✓
- [x] AC 04-03: empty aggregation → clean "no data" (exit 0), never conflated with read failure (exit 1) ✓
- [x] AC 04-04: distinct subcommand, `atcr report` unchanged, no name collision ✓

---

### 4.5 [x] **Phase 4 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 4 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 4 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 4 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: `newQualityReportCmd`/`runQualityReport` contract stable for future consumers?
    - CONFIG SURFACE: No new config keys introduced
    - INTEGRATION: Command registration correct, no collision with existing subcommands (`report`, `review`, `reconcile`, etc.)?
    - PHASE-EXIT CONTRACT: Phase 6 docs can describe this command's shipped `--format`/output shape without further changes?
    - REGRESSION: `atcr report` fully unaffected?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Gate findings (fresh subagent, hostile integrator — no CRITICAL/HIGH; CONTRACT-EXIT / CONFIG-SURFACE (no new key) / INTEGRATION (24 commands, no collision, content-free source enforced by import test) / PHASE-EXIT / REGRESSION (`atcr report` untouched, Short asserted byte-identical) all PASS):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| LOW | skill/skill_test.go:133 | `atcr quality-report` routing row added to SKILL.md but not to `dispatcherCommands`, despite the SKILL.md convention (lines 84-87) requiring both be updated together | FIXED before boundary: added `"quality-report"` to `dispatcherCommands`; skill routing tests green |
| LOW | skill/SKILL.md:3 | Frontmatter `description` enumeration omits `quality-report` (and pre-existing `config`) — outside the documented drift-invariant (routing row + dispatcherCommands only) | Deferred → TD-009 |

**Action Taken:** No CRITICAL/HIGH → **Phase gate passed.** The one LOW that violated a documented same-file update invariant (dispatcherCommands) was fixed before the phase boundary and the skill routing tests re-run green; the frontmatter-prose LOW (outside that invariant, with a pre-existing `config` omission) is deferred to `tech-debt-captured.md` (TD-009).

**Duration:** 15-30 min

---

## Phase 5: Gated Transport Wiring

**Focus:** Story 6 — Resolves the transport fork (sibling payload type + sibling `Send` method on `internal/telemetry.Client`, independent of `internal/scorecard.Push`/`CloudSyncRecord`, per sprint-design's resolution). Call sites added adjacent to the existing passive-ping emission in `cmd/atcr/review.go:462` and `cmd/atcr/reconcile.go:186`: `qualitySignalGate()` resolved fresh per run, checked *before* any payload construction or goroutine spawn. Fail-open is absolute. `--preview` always wins.

**AC Links:** [06-01](plan/acceptance-criteria/06-01-gate-disabled-short-circuit.md), [06-02](plan/acceptance-criteria/06-02-opted-in-send-transmits-allowlisted-payload.md), [06-03](plan/acceptance-criteria/06-03-transport-failure-fails-open.md)

---

### 5.1 [x] **[Gate-Disabled Short-Circuit — RED](plan/user-stories/06-gated-quality-signal-transmission.md)**

**AC:** 06-01

1. Read `cmd/atcr/review.go:462-467` and `cmd/atcr/reconcile.go:186-191`'s passive-ping call-site idiom (`if telemetryGate() { ... }`) and `internal/telemetry/client.go`'s `New`/`isHTTPS`/detached `Send` contract
2. Write failing tests in `cmd/atcr/qualitysignal_send_test.go`:
   - `TestQualitySignalSend_GateDisabled_ZeroRequests_Review` — `httptest` request-counting server, gate disabled (no env/config), review run
   - `TestQualitySignalSend_GateDisabled_ZeroRequests_Reconcile` — same, reconcile run
   - `TestQualitySignalSend_GateDisabled_NoPayloadConstruction` — asserted via payload-constructor seam, not just absent requests
   - `TestQualitySignalSend_ExplicitlyDisabledConfig_ZeroRequests` — `quality_signal: false` persisted
   - `TestQualitySignalSend_UnrelatedTelemetrySurfacesUnaffected` — passive ping / `--sync-cloud` behavior unchanged with the new call site present
   - `TestQualitySignalSend_EndpointReachableButGateDisabled_ZeroRequests`
   - `TestQualitySignalSend_PreviewWinsOverGateDisabled` — `--preview` still renders locally
   - `TestQualitySignalSend_GateReEvaluatedFreshPerRun` — no cross-run cache
3. Verify all new tests fail (RED confirmed)

**Files:** `cmd/atcr/qualitysignal_send_test.go` | **Duration:** 2-3 hours

---

### 5.2 [x] **[Gate-Disabled Short-Circuit — GREEN](plan/user-stories/06-gated-quality-signal-transmission.md)**

1. Add the quality-signal call site adjacent to the passive-ping emission in `cmd/atcr/review.go` (`:462-467`) and `cmd/atcr/reconcile.go` (`:186-191`): `if qualitySignalGate() { ... }`, resolved fresh per run, checked before any payload construction or goroutine spawn
2. Wire the `--preview` branch (Phase 3) to remain evaluated first, unconditionally
3. Run T1 after each change; confirm all tests pass (T2)
4. `git commit -m "feat(review,reconcile): wire quality-signal gate-first call sites (green)"`

**Files:** `cmd/atcr/review.go`, `cmd/atcr/reconcile.go` | **Duration:** 3-4 hours

---

### 5.2.A [x] **[Gate-Disabled Short-Circuit — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-gated-quality-signal-transmission.md)**

**Changed Files:** `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `cmd/atcr/qualitysignal_send_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 5.2 gate-first call sites`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 5.2]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure? Is the gate check strictly BEFORE payload construction/goroutine spawn, with zero leak window?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Endpoint reachable but gate disabled?
    - ERROR HANDLING: Missing catches, swallowed errors?
    - PERFORMANCE: N+1, leaks, blocking ops?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Subagent findings (fresh general-purpose agent, no CRITICAL/HIGH; gate-first ordering, opt-in-only enable, malformed→false fail-safe, and independence from telemetryGate/resolveSyncCloud all verified):**
| Severity | File:Line | Issue | Disposition |
|----------|-----------|-------|-----|
| LOW | qualitysignal.go maybeSendQualitySignal | On an opted-in run the payload is built synchronously on the completion path (interim state, send unwired) | Resolves in 5.5 — the send moves to the transport's detached goroutine, matching the passive-ping build-sync/send-async pattern; build cost is bounded (<10ms, AC 06-02) |
| LOW | qualitysignal.go maybeSendQualitySignal | Doc claimed panics are swallowed but there was no `recover()`; the inline reconcile call site would propagate a build/transport panic, contradicting AC 06-03 fail-open | FIXED in 5.3 — added `defer recover()` (debug-logs the value) so the fail-open guarantee is enforced now, not only after 5.5 |

**Action Taken:** No CRITICAL/HIGH → adversarial review passed. Both LOWs are addressed within-sprint (panic-recover added inline in 5.3 REFACTOR; synchronous-build resolves structurally in 5.5), consistent with prior phases' inline-strengthening pattern — neither deferred to tech-debt.

---

### 5.3 [x] **[Gate-Disabled Short-Circuit — REFACTOR](plan/user-stories/06-gated-quality-signal-transmission.md)**

1. Fix CRITICAL/HIGH issues from 5.2.A (if any)
2. Confirm zero goroutine spawn, zero HTTP client construction, and zero payload allocation on the disabled path
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(review,reconcile): address review + clean up gate-first wiring"`

**Duration:** 1-2 hours

---

### 5.4 [ ] **[Send + Fail-Open — RED](plan/user-stories/06-gated-quality-signal-transmission.md)**

**AC:** 06-02, 06-03

1. Read `internal/telemetry/client.go`'s documented fail-open contract and `internal/telemetry/client_test.go`'s `TestClient_Send_EmptyEndpointNoOps` pattern
2. Write failing tests in `cmd/atcr/qualitysignal_send_test.go`:
   - `TestQualitySignalSend_EnabledViaEnv_ExactlyOneRequest_CorrectCounts` — capture-server body unmarshals to Phase 1's struct with hand-computed counts
   - `TestQualitySignalSend_EnabledViaConfig_SameSingleSendBehavior`
   - `TestQualitySignalSend_SentBytesEqualPreviewBytes` — same fixture, both paths, byte-identical
   - `TestQualitySignalSend_PlaintextOrEmptyEndpointNoTransmission`
   - `TestQualitySignalSend_EmptyAggregation_DefinedBehaviorNoError`
   - `TestQualitySignalSend_500Response_RunOutcomeUnchanged`
   - `TestQualitySignalSend_DNSFailure_RunOutcomeUnchanged`
   - `TestQualitySignalSend_TimeoutDoesNotBlockRunCompletion`
   - `TestQualitySignalSend_PanicInSendPathContained`
   - `TestQualitySignalSend_FailureOnOneRunDoesNotAffectNext` — no circuit-breaker/retry state carried across runs
   - `TestQualitySignalSend_FailureDiagnosticsNeverIncludePayloadBody`
3. Verify all new tests fail (RED confirmed)

**Files:** `cmd/atcr/qualitysignal_send_test.go` | **Duration:** 3-4 hours

---

### 5.5 [ ] **[Send + Fail-Open — GREEN](plan/user-stories/06-gated-quality-signal-transmission.md)**

1. Add a sibling `Send`-style method on `internal/telemetry.Client` for the `QualitySignal` payload type (per sprint-design's resolved fork: sibling, never an extension of `CloudSyncRecord`), preserving the detached-goroutine, HTTPS-only, nil/empty-endpoint-no-op, panic-safe contract
2. In the enabled branch of the Phase 5.2 call sites, build the payload via the same shared constructor `--preview` uses, then invoke the sibling `Send`
3. Confirm fail-open: any transport failure (non-2xx, DNS, timeout, panic) leaves the run's exit code and stdout unchanged
4. Run T1 after each change; confirm all tests pass (T2)
5. `git commit -m "feat(telemetry,review,reconcile): gated quality-signal send with fail-open transport (green)"`

**Files:** `internal/telemetry/client.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go` | **Duration:** 4-5 hours

---

### 5.5.A [ ] **[Send + Fail-Open — ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-gated-quality-signal-transmission.md)**

**Changed Files:** `internal/telemetry/client.go`, `cmd/atcr/review.go`, `cmd/atcr/reconcile.go`, `cmd/atcr/qualitysignal_send_test.go`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 5.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 5.5 send + fail-open`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): [LIST FROM 5.5]
  - Checklist (pass verbatim):
    - SECURITY: Auth bypass, injection, data exposure? Does any failure diagnostic log the payload body?
    - EDGE CASES: Null, empty, boundaries, concurrent access? Review+reconcile in the same session — duplicate/idempotent sends?
    - ERROR HANDLING: Missing catches, swallowed errors? Is fail-open truly absolute (500, DNS, timeout, panic)?
    - PERFORMANCE: N+1, leaks, blocking ops? Does the timeout scenario block run completion?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| CRITICAL | | | |
| HIGH | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 5.6, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 5.6 [ ] **[Send + Fail-Open — REFACTOR](plan/user-stories/06-gated-quality-signal-transmission.md)**

1. Fix CRITICAL/HIGH issues from 5.5.A (if any)
2. Confirm no retry, queue, or circuit-breaker state is carried across runs; confirm sent bytes remain byte-identical to `--preview` after any cleanup
3. Validate all tests still pass (T3)
4. `git commit -m "refactor(telemetry,review,reconcile): address review + clean up send path"`

**Duration:** 1-2 hours

---

### 5.7 [ ] **Phase 5 DoD Verification**

```
Story-6 DoD Complete
Auto: 5/5 | Story-Specific: 3/3 ACs
Manual Review: [ ] Code reviewed (adversarial 5.2.A + 5.5.A, REFACTOR 5.3 + 5.6)
```

- [ ] T3: `go test ./internal/telemetry/... ./cmd/atcr/...` — all passing
- [ ] Coverage ≥80%
- [ ] `golangci-lint run` — no errors
- [ ] `go vet ./...` — clean
- [ ] Build: `go build ./...` — succeeds
- [ ] AC 06-01: gate disabled → zero requests, zero payload construction, on both review and reconcile ✓
- [ ] AC 06-02: gate enabled → exactly one request, correct counts, byte-identical to `--preview` ✓
- [ ] AC 06-03: 500 / DNS / timeout / panic all leave run outcome identical to gate-disabled baseline ✓

---

### 5.8 [ ] **Phase 5 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 5 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 5 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 5 (absolute paths): [LIST]
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: Sibling `Send` method contract stable; fail-open contract honored absolutely?
    - CONFIG SURFACE: No new config keys introduced by this phase
    - INTEGRATION: `review.go`/`reconcile.go` call sites correctly gated, no coupling to `telemetryGate`/`resolveSyncCloud`?
    - PHASE-EXIT CONTRACT: Phase 6 docs can describe the shipped send behavior without further code changes?
    - REGRESSION: Passive-ping and `--sync-cloud` behavior fully intact; `--preview` still always wins?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| CRITICAL | | | |
| HIGH | | | |

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
- MEDIUM/LOW found → Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
- None found → Note "Phase gate passed" and proceed to phase stop
**Duration:** 15-30 min

---

## Phase 6: Documentation

**Focus:** Story 5 — Pure documentation edit to `docs/telemetry.md`, sequenced last so it reflects shipped field names/keys, not plan-stage design notes. Adds a new section (after "Persona Leaderboard data" / "Cloud sync"): the exact allowlisted field table for `quality_signal.go`, the independent opt-in mechanism (OR'd, no precedence with `telemetry`/`--sync-cloud`), `--preview`'s exact behavior, and an explicit restatement of the absolute no-code/no-finding-content guarantee plus the `HashPersonaID` unsalted-hash caveat (TD-007). No source code changes.

**AC Links:** [05-01](plan/acceptance-criteria/05-01-document-quality-signal-field-allowlist.md), [05-02](plan/acceptance-criteria/05-02-document-optin-mechanism-and-preview-behavior.md), [05-03](plan/acceptance-criteria/05-03-document-privacy-guarantee-and-persona-hash-caveat.md)

---

### 6.1 [ ] **📝 Document the Quality-Signal Telemetry Contract**

**Task:** Add a new "Community prompt quality signal" section to `docs/telemetry.md`, re-reading the actually-shipped `internal/telemetry/quality_signal.go` struct, its allowlist regression test, `cmd/atcr/qualitysignal.go`'s gate, and the `--preview` flag's real output immediately before writing — never from plan-stage placeholder names.

**Priority:** Medium | **Effort:** S

1. Re-read the shipped `QualitySignal` struct field names/types and its allowlist test's asserted key set
2. Write the field-allowlist table (`Field | Type | Example | Meaning`, mirroring the existing "Usage ping schema" table); state explicitly the payload is its own separately-tested allowlist, not an extension of `Event`; note zero-value counts always serialize as `0`
3. Write the opt-in subsection: exact env var and `atcr config set quality_signal <bool>` names, the shipped six-cell truth table, and an explicit "no precedence, independent of `telemetry`/`--sync-cloud`" statement
4. Write the `--preview` subsection: exact flag name/host command, payload+"not sent" marker output shape, no-credential-required, precedence over `--sync-cloud`
5. Write the standalone "no code, no finding content, ever" guarantee plus the restated `HashPersonaID` unsalted-hash/dictionary-attack (TD-007) caveat, consistent with — never contradicting — the existing "Persona Leaderboard data" section
6. Verify markdown renders without syntax errors (tables, headers, links); no broken internal links

**Success Criteria:** Field table matches the shipped struct exactly (no more, no fewer fields); truth table matches `qualitySignalEnabled`'s six cells exactly; independence and privacy guarantees explicitly stated and self-sufficient without requiring the reader to have read earlier sections first.

**Files:** `docs/telemetry.md` | **Duration:** 2-3 hours

---

### 6.1.A [ ] **📝 Document the Quality-Signal Telemetry Contract — ADVERSARIAL REVIEW (subagent)**

**Changed Files:** `docs/telemetry.md`

**Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 6.1 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Adversarial review: 6.1 telemetry docs`
- prompt: Self-contained brief including:
  - Files to review (absolute paths): `docs/telemetry.md`, plus for cross-check: `internal/telemetry/quality_signal.go`, `internal/telemetry/quality_signal_test.go`, `cmd/atcr/qualitysignal.go`, `cmd/atcr/flags.go`
  - Checklist (pass verbatim, doc-accuracy focus):
    - ACCURACY: Does the field table match the shipped `QualitySignal` struct exactly (field-for-field, no invented or missing fields)?
    - ACCURACY: Does the truth table match the shipped `qualitySignalEnabled` six-cell matrix exactly?
    - CONSISTENCY: Does the new section contradict or weaken the existing "Persona Leaderboard data" `HashPersonaID` caveat (e.g. drop "pseudonymous, not anonymous")?
    - COMPLETENESS: Is the "no code, no finding content, ever" statement standalone (readable without cross-referencing another section)?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| CRITICAL | | | |
| HIGH | | | |

**Action Required:**
- CRITICAL/HIGH found → List issues for 6.2, do NOT proceed until fixed
- MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
- None found → Note "Adversarial review passed" and proceed

---

### 6.2 [ ] **📝 Documentation — REFACTOR**

1. Fix CRITICAL/HIGH issues from 6.1.A (if any)
2. Final field-by-field diff against the shipped struct and allowlist test; confirm zero discrepancies in either direction
3. `git commit -m "docs(telemetry): document quality-signal contract, opt-in, --preview, and privacy guarantee"`

**Duration:** 1 hour

---

### 6.3 [ ] **Phase 6 DoD Verification**

```
Story-5 DoD Complete
Auto: 3/3 | Story-Specific: 3/3 ACs
Manual Review: [ ] Code reviewed (adversarial 6.1.A, REFACTOR 6.2)
```

- [ ] Markdown renders without syntax errors (tables, headers, links); no broken internal links
- [ ] `go build ./...` and `go test ./...` still pass (no source changed by this phase)
- [ ] AC 05-01: field-allowlist table matches shipped struct exactly ✓
- [ ] AC 05-02: opt-in mechanism + `--preview` behavior documented and matching shipped code ✓
- [ ] AC 05-03: standalone no-code/no-finding-content guarantee + restated persona-hash caveat, consistent with existing docs ✓

---

### 6.4 [ ] **Phase 6 — GATE: Integration & Exit Review (subagent)**

**Scope:** All files changed during Phase 6 (integration-level, not TDD cadence)

**Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

Use the Agent tool:
- subagent_type: `general-purpose`
- description: `Phase 6 gate review`
- prompt: Self-contained brief including:
  - Files changed during Phase 6 (absolute paths): `docs/telemetry.md`
  - Checklist (pass verbatim, hostile integrator perspective):
    - CONTRACT EXIT: Doc accurately reflects the shipped Phases 1-5 contracts, not plan-stage placeholders?
    - CONFIG SURFACE: No new config keys introduced or implied beyond what Phase 2 shipped
    - INTEGRATION: New section does not alter or contradict the existing usage-ping / cloud-sync sections?
    - PHASE-EXIT CONTRACT: Final Phase validation can cross-check this doc against shipped code with zero discrepancies?
    - REGRESSION: Existing `docs/telemetry.md` content (usage-ping schema, opt-out table, persona-hash caveat, cloud-sync section) unchanged?
  - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
  - Required output: ONLY the findings table below (markdown), no prose

**Paste the subagent's findings table here (delete rows if none):**
| Severity | File:Line | Issue | Fix |
|----------|-----------|-------|-----|
| CRITICAL | | | |
| HIGH | | | |

**Action Required:**
- CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
- MEDIUM/LOW found → Append to `tech-debt-captured.md` (same pipeline as N.X.A findings)
- None found → Note "Phase gate passed" and proceed to phase stop
**Duration:** 15-30 min

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...`
- [ ] Coverage meets threshold: `go test -coverprofile=coverage.out ./...` ≥80%
- [ ] Lint/format clean: `golangci-lint run` (0 issues), `go vet ./...` (clean), `goimports` applied
- [ ] Build succeeds: `go build ./...`
- [ ] All 21 acceptance criteria across 6 user stories verified against shipped code
- [ ] `docs/telemetry.md` cross-checked against the shipped `quality_signal.go` struct and its allowlist test for zero discrepancy in either direction
- [ ] End-to-end walkthrough: `--preview` output byte-identical to a real send's marshaled body; gate resolves disabled with no env/config; a corrupted `quality_signal` config value fails safe
- [ ] Confirm no accidental coupling between the new `qualitySignalGate()` and `telemetryGate`/`resolveSyncCloud`

### Optional: Targeted Mutation Testing

Mutation testing tool unavailable in this environment (no `stryker-mutator` in package.json, no `mutmut`/`cargo-mutants` on PATH) — skip. If a tool becomes available, target only `internal/localdebt/qualitysignal.go` and `cmd/atcr/qualitysignal.go` (privacy-critical allowlist/gate logic) — do NOT run full-codebase mutation testing.

### Drift Analysis

Compare final implementation against `plan/original-requirements.md`:
- [ ] AC1 (opt-in, aggregate, content-free, preview, nothing sent by default) — delivered by Phases 1-3, 5
- [ ] AC2 (signal surfaced to maintainer, closes loop to 19.8/19.9) — delivered by Phase 4
- [ ] AC3 (`go test ./...` passes; docs document the exact contract) — delivered by Phase 6 + this Final Phase
- [ ] No scope creep: the submit flow/curation (19.9), the dismissal signal source (24.0), the telemetry transport itself (28.0), and the hermes drafting agent (19.8) remain untouched — this sprint only consumes them
