# Sprint Design: Community Prompt Quality Signal

**Created:** July 17, 2026 11:27:15AM
**Plan:** [Community Prompt Quality Signal](.)
**Plan Type:** Feature (✨)
**Status:** Design Complete

---

## Original User Request

> Close the persona living-library flywheel by teaching the maintainer *which* reviewer prompts need tuning — from **opt-in, aggregate, content-free** signal. Because atcr reviews **private code**, this signal must be collected so that **never a line of code or a finding body leaves the machine**: per-persona+model counters and identifiers only, opt-in, with a visible local preview of exactly what would be sent. This is the **signal half** split out of Epic 19.9 (which owns intake + curation); it depends on 19.9's community personas plus the dismissal signal (24.0) and the transport (28.0), so it is numbered to run after all three.

**Referenced Resources:** None — the original request is self-contained within `original-requirements.md`; no external `@file`/`@url` references were supplied.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Prompt Quality Signal
**Complexity:** 10/12 (VERY COMPLEX)
**Timeline:** 14 days
**Phases:** 7
**Pattern:** Foundation → Gate → Preview → Report → Transport → Docs → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
content-free telemetry allowlist pattern
opt-in gate independent truth table
append-only JSONL schema version migration
persona model aggregate counter grouping
fail-open telemetry transport wiring
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - New patterns (schema version bump, new allowlisted payload type, new independent gate, new subcommand) but each tightly mirrors an existing in-repo precedent (`event.go`, `telemetryGate`, `report.go`) rather than introducing a novel architecture.
- **Integration:** 3/3 - Touches 10+ files across 4 packages (`internal/localdebt`, `internal/telemetry`, `internal/registry`, `internal/scorecard`) and 6 `cmd/atcr` files, including two existing run-completion call sites (`review.go`, `reconcile.go`).
- **Story/Task & Test:** 3/3 - 6 user stories, 21 acceptance criteria, two ACs independently flagged high-complexity (01-02 schema bump/exclusion, 01-04 append-only fold-by-ID).
- **Risk/Unknowns:** 2/3 - The two open forks flagged by codebase discovery (model-attribution mechanism, transport shape) are resolved below in Architecture; residual risk is privacy-critical correctness on a proven, falsifiable pattern (locked allowlist + regression test), not open design uncertainty.

**Time Formula:** Σ(per-phase estimate), where estimate ≈ AC count × complexity-weighted days
**Calculation:** Phase1(L, 5 AC, 2 high-complexity)=3d + Phase2(S, 3 AC)=1.5d + Phase3(S, 3 AC)=1.5d + Phase4(M, 4 AC)=2.5d + Phase5(M, 3 AC, 1 critical-risk)=2.5d + Phase6(S, 3 AC, docs)=1d + Phase7(Validation)=2d = **14 days**

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** strong (complexity 10/12 ≥ 10 threshold)
**Suggested command:** `/create-sprint @.planning/plans/active/30.0_community_prompt_quality_signal/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1: Foundation — Aggregation & Schema (Story 1)
**Duration:** 3 days | **Items:** 5 ACs (01-01 … 01-05)
**Focus:** `localdebt.Record` gains an optional `Model` field behind a `SchemaVersion` 1→2 bump, populated at `persistLocalDebt` write time from `fanout.AgentStatus.Model` (not derived from `RunID`, per confirmed discovery finding). A new aggregation reads `.atcr/debt/` via `ReadAll`, folds the append-only stream by `ID` (later terminal record wins, mirroring `selectOpenDebt`), then groups by `(persona, model)` and sums `dismissed`/`confirmed` counts using the `Aggregate()` grouping/sort idiom. Multi-persona `Reviewers` records attribute the outcome to every listed persona. A new `internal/telemetry/quality_signal.go` payload type (fixed field set, no `omitempty`) ships with a locking allowlist regression test mirroring `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`. This is the sole data source every later phase depends on — nothing here sends anything.

### Phase 2: Independent Opt-In Gate (Story 2)
**Duration:** 1.5 days | **Items:** 3 ACs (02-01 … 02-03)
**Focus:** `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool` (opt-**IN** shape — fail-closed on unset, `envEnabled || (cfg != nil && *cfg)`) plus `qualitySignalGate()` I/O wrapper in `cmd/atcr/qualitysignal.go`, structurally mirroring but never sharing state with `telemetryEnabled`/`telemetryGate`. `LoadQualitySignalSetting`/`SetQualitySignalSetting` added to `internal/registry` reusing `withConfigLock`/`configMapping`/`setMappingBool` verbatim. `runConfigSet`'s allowlist (`cmd/atcr/config.go:59`) extended from a single `"telemetry"` literal to an explicit two-key switch. No payload, no network call — pure gate + persistence.

### Phase 3: Local `--preview` Surface (Story 3)
**Duration:** 1.5 days | **Items:** 3 ACs (03-01 … 03-03)
**Focus:** `--preview` bool flag via a new `addQualitySignalFlags`-style helper in `cmd/atcr/flags.go` (chained-`PreRunE`, mirroring `addSyncCloudFlags`). The command's run path builds Phase 1's payload struct first, then branches: `--preview` set → `json.MarshalIndent` to stdout, return before any opt-in check or client construction; unset → proceed to Phase 5's gated send. A regression test proves the preview marshals the identical struct/function the real send uses, so it structurally cannot drift.

### Phase 4: Maintainer-Facing Report (Story 4)
**Duration:** 2.5 days | **Items:** 4 ACs (04-01 … 04-04)
**Focus:** New `cmd/atcr/telemetry_report.go` subcommand mirroring `newReportCmd`/`runReport`'s shape (`--format md|json`), sourcing exclusively from Phase 1's aggregation type (never `internal/reconcile` or raw findings) so content-freedom is enforced at the import-graph level, not just by convention. Ranks rows by dismissal rate descending — a single deterministic metric, no invented threshold. Empty aggregation renders a clean "no data" state (no panic, no misleading empty table). Registered as a distinct subcommand alongside — never modifying — the existing `atcr report`.

### Phase 5: Gated Transport Wiring (Story 6)
**Duration:** 2.5 days | **Items:** 3 ACs (06-01 … 06-03)
**Focus:** Resolves the transport fork flagged by codebase discovery (see Architecture, below): a **sibling payload type + sibling `Send` method on `internal/telemetry.Client`**, independent of `internal/scorecard.Push`/`CloudSyncRecord`. Call sites added adjacent to the existing passive-ping emission in `cmd/atcr/review.go:462` and `cmd/atcr/reconcile.go:186`: `qualitySignalGate()` is resolved fresh per run and checked *before* any payload construction or goroutine spawn; only the enabled branch builds Phase 1's payload via the same constructor Phase 3's `--preview` renders. Fail-open is absolute — a transport failure never changes the run's exit code or stdout, mirroring `client.go`'s documented no-op/panic-safe contract. `--preview` always wins (its branch returns before this phase's send path is reached).

### Phase 6: Documentation (Story 5)
**Duration:** 1 day | **Items:** 3 ACs (05-01 … 05-03)
**Focus:** Pure documentation edit to `docs/telemetry.md` — sequenced last so it reflects shipped field names/keys, not plan-stage design notes. Adds a new section (after the existing "Persona Leaderboard data" / "Cloud sync" sections): the exact allowlisted field table for `quality_signal.go` (pulled from the shipped struct + its allowlist test), the independent opt-in mechanism stated as non-overriding with `telemetry`/`--sync-cloud` (OR'd, no precedence), `--preview`'s exact local-only behavior, and an explicit restatement (not just a cross-reference) of the absolute no-code/no-finding-content guarantee plus the `HashPersonaID` unsalted-hash/dictionary-attack caveat (TD-007).

### Phase 7: Validation
**Duration:** 2 days | **Items:** Cross-cutting
**Focus:** `go test ./...` full pass at ≥80% coverage baseline; `golangci-lint run` + `go vet ./...`; cross-check `docs/telemetry.md` against the shipped `quality_signal.go` struct and its allowlist test for zero discrepancy in either direction; end-to-end walkthrough of the privacy guarantee (`--preview` output byte-identical to a real send's marshaled body; gate resolves disabled with no env/config; a corrupted `quality_signal` config value fails safe); confirm no accidental coupling between the new gate and `telemetryGate`/`resolveSyncCloud`.

---

## Work Decomposition

| Story | Theme | Effort | ACs | Testable Elements |
|-------|-------|--------|-----|--------------------|
| [01](user-stories/01-aggregate-per-persona-model-dismissal-counters.md) | Aggregate per-persona+model dismissed/confirmed counters | L | [01-01](acceptance-criteria/01-01-per-persona-model-aggregation.md), [01-02](acceptance-criteria/01-02-model-field-schema-bump-and-exclusion.md), [01-03](acceptance-criteria/01-03-multi-persona-reviewers-attribution.md), [01-04](acceptance-criteria/01-04-append-only-record-fold-by-id.md), [01-05](acceptance-criteria/01-05-allowlisted-quality-signal-payload-type.md) | `SchemaVersion` 1→2 migration test; `(persona, model)` grouping table-test (multi-reviewer, mixed-schema, both terminal statuses); allowlist regression test on `quality_signal.go` |
| [02](user-stories/02-quality-signal-opt-in-gate.md) | Independent opt-in gate for transmission | S | [02-01](acceptance-criteria/02-01-quality-signal-off-by-default.md), [02-02](acceptance-criteria/02-02-independent-truth-table-no-shared-state.md), [02-03](acceptance-criteria/02-03-config-set-quality-signal-persists.md) | Six-state env×config truth table (unit); `atcr config set quality_signal <bool>` round-trip (integration); fail-safe-to-disabled on corrupt config |
| [03](user-stories/03-local-preview-of-outbound-quality-signal-payload.md) | Local `--preview` of the outbound payload | S | [03-01](acceptance-criteria/03-01-preview-flag-prints-exact-payload.md), [03-02](acceptance-criteria/03-02-preview-bypasses-network-and-optin-gate.md), [03-03](acceptance-criteria/03-03-preview-never-drifts-from-real-send.md) | Zero-HTTP-call assertion at the do-request seam; golden JSON round-trip; marshal-path-identity regression test |
| [04](user-stories/04-maintainer-facing-prompt-quality-report.md) | Maintainer-facing prompt quality report | M | [04-01](acceptance-criteria/04-01-ranked-quality-report-rendering.md), [04-02](acceptance-criteria/04-02-content-free-privacy-guarantee.md), [04-03](acceptance-criteria/04-03-empty-aggregation-no-data-state.md), [04-04](acceptance-criteria/04-04-distinct-subcommand-registration.md) | Ranking-order table-test against hand-computed fixtures; import-graph assertion (no `internal/reconcile` import); empty-aggregation E2E-lite exit-code test |
| [06](user-stories/06-gated-quality-signal-transmission.md) | Gated transmission via the Epic 28.0 transport | M | [06-01](acceptance-criteria/06-01-gate-disabled-short-circuit.md), [06-02](acceptance-criteria/06-02-opted-in-send-transmits-allowlisted-payload.md), [06-03](acceptance-criteria/06-03-transport-failure-fails-open.md) | Gate-disabled → zero payload/zero HTTP (unit); gate-enabled → exactly one send, byte-identical to `--preview` (unit/integration); transport failure → unchanged exit code/stdout (unit) |
| [05](user-stories/05-document-quality-signal-telemetry-contract.md) | Document the telemetry contract | S | [05-01](acceptance-criteria/05-01-document-quality-signal-field-allowlist.md), [05-02](acceptance-criteria/05-02-document-optin-mechanism-and-preview-behavior.md), [05-03](acceptance-criteria/05-03-document-privacy-guarantee-and-persona-hash-caveat.md) | Manual doc-accuracy cross-check against shipped code + doc-lint (no automated test) |

**Sequencing rationale:** Story 1 is the sole hard dependency for Stories 3, 4, and 6 — it must land first. Story 2 has zero dependencies and can theoretically run in parallel with Story 1, but is sequenced second (not first) because Stories 3/4/6 need Story 1's output, and a solo-developer sprint benefits from finishing the foundational data layer before splitting attention. Story 6 needs both Story 1 (payload) and Story 2 (gate), so it runs after Phase 4 rather than immediately after Phase 2. Story 5 (docs) is sequenced last since it must describe shipped field/key names, not planned ones — documenting from a moving target risks exactly the drift its own Constraints section warns against.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located `*_test.go` files (Go convention; confirmed by codebase discovery's `test_patterns` — `go test`, co-located, `*_test.go` naming).

**Test File Placement Examples:**
- `internal/localdebt/record_test.go` — `SchemaVersion` 1→2 migration, mixed-schema read
- `internal/localdebt/qualitysignal_test.go` (or sibling) — `(persona, model)` aggregation, fold-by-ID, multi-reviewer attribution
- `internal/telemetry/quality_signal_test.go` — allowlist regression test, `Send` sibling method
- `cmd/atcr/qualitysignal_test.go` — six-state truth table, gate independence from `telemetryGate`
- `cmd/atcr/config_test.go` — `quality_signal` key allowlist extension, corrupt-value fail-safe
- `cmd/atcr/flags_test.go` — `--preview` flag registration/chaining
- `cmd/atcr/telemetry_report_test.go` — ranking order, no-data state, subcommand registration
- `cmd/atcr/review_test.go` / `cmd/atcr/reconcile_test.go` — gate-first ordering, fail-open on transport failure

**Unit/Integration/E2E:**
- **Unit (16 ACs):** 01-01, 01-03, 01-04, 01-05, 02-01, 02-02, 03-02, 03-03, 04-01, 04-02, 04-03, 04-04, 06-01, 06-03, plus the unit half of 01-02, 02-03
- **Integration (4 ACs, each paired with its unit counterpart):** 01-02 (schema migration), 02-03 (config round-trip), 03-01 (preview CLI render), 06-02 (send byte-identity to preview)
- **E2E-lite (1 AC):** 04-03 (empty-aggregation CLI exit-code assertion)
- **Manual/doc-lint (3 ACs):** 05-01, 05-02, 05-03 — Story 5 is documentation-only

**Test Environment Status:**
- Framework: `go test` (stdlib `testing`, table-driven pattern already established repo-wide) — READY
- Execution: `go test ./...` — READY (existing CI command, no new setup)
- Coverage Tools: `go test -coverprofile=coverage.out ./...`, 80% baseline — READY (existing baseline, no relaxation needed for this scope)

---

## Architecture

**Primitives:**
- `localdebt.Record.Model string` (`omitempty`, new in `SchemaVersion` 2) — model attribution attached at write time, not derived post hoc.
- Internal aggregation row: `{Persona, Model string; DismissedCount, ConfirmedCount int}` per `(persona, model)` group.
- `telemetry.QualitySignal` (name TBD at implementation, working name) — the outbound allowlisted payload: persona identifier (hashed via `HashPersonaID`), model, dismissed count, confirmed count. Fixed field set, no `omitempty`, locked by a dedicated allowlist regression test — its own struct, never an extension of `Event` or `CloudSyncPersona`.
- `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool` — pure, total combining function (opt-**in** shape, fail-closed on unset).

**Module Boundaries:**
- `internal/localdebt` — record schema + append-only read/fold (extended with `Model`, not re-owned).
- `internal/telemetry` — new `quality_signal.go` payload type + a sibling `Send`-style method on the existing fail-open `Client`.
- `internal/registry` — `LoadQualitySignalSetting`/`SetQualitySignalSetting`, mirroring `telemetry_setting.go`'s atomic mkdir-lock write path.
- `cmd/atcr` — `qualitysignal.go` (gate), `flags.go` (`--preview`), `config.go` (allowlist), `telemetry_report.go` (maintainer report), `review.go`/`reconcile.go` (call sites).
- `docs/telemetry.md` — contract documentation only, no code.

**External Dependencies:** None new. Stdlib only (`crypto/sha256` via the existing `HashPersonaID`, `encoding/json`), consistent with Epic 28.0's zero-new-dependency posture.

**Replaceability & the resolved transport fork:** Codebase discovery flagged an open fork — extend `internal/scorecard.CloudSyncRecord`/`CloudSyncPersona` (riding the existing `--sync-cloud` `Push`) vs. a sibling payload type over `internal/telemetry.Client`. **This design resolves it as the sibling-payload path.** Extending `CloudSyncPersona` would mean the quality signal rides `resolveSyncCloud`'s gate (flag + `ATCR_API_KEY`) rather than the new, independent `qualitySignalGate()` — directly violating Story 2's and Story 6's explicit "no shared state, no shared precedence" constraint, and conflating two independently-opted-in features behind one `Push` call. The sibling-`Client.Send` path keeps the transport, the gate, and the payload type each independently swappable: a future transport change touches only Phase 5's call sites, never Phases 1-4's aggregation, gate, preview, or report logic.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|-----------------|---------------------|
| Quality-signal payload construction (`internal/telemetry/quality_signal.go`) | Phase 1 | Field creep exposing finding text, file paths, or code; a future PR adding a "just one more field" without noticing the privacy contract | Locked allowlist struct (no `omitempty`, exactly-fixed fields) + dedicated regression test mirroring `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`; test fails loudly on any unreviewed field addition |
| Opt-in gate (`qualitySignalEnabled`/`qualitySignalGate`) | Phase 2 | Accidental coupling to `telemetryGate`/`resolveSyncCloud`; silent re-enable on corrupt persisted config | Pure, independent combining function with its own test file; fail-safe-to-disabled on malformed config (never fail-open); exhaustive six-state truth-table test |
| Transport send call site (`review.go`/`reconcile.go`) | Phase 5 | Gate checked after payload construction (leak window); a cached/stale gate result reused across runs | Gate resolved fresh per run, checked *before* any payload construction or goroutine spawn — asserted at the do-request seam by AC 06-01 |
| Persona identifier hashing (`HashPersonaID` reuse) | Phases 1, 5 | Unsalted SHA-256 over a small, enumerable persona-name set is dictionary-attackable (known caveat, TD-007) | No new mitigation invented here — reuse the existing primitive as-is and restate the documented caveat explicitly in Phase 6's docs section (not merely cross-referenced) |
| Config persistence (`atcr config set quality_signal`) | Phase 2 | Allowlist bypass via prefix-match or typo; corrupted YAML causing an unintended fail-open | Explicit switch-based key allowlist (not loosened prefix match); atomic mkdir-lock write reused verbatim from `telemetry_setting.go`; corrupt-value regression test |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|----------------|--------|----------|
| Aggregation over `.atcr/debt/` at run completion | Local debt store grows unbounded (append-only JSONL) over a project's life | No material added latency to review/reconcile completion | Reuse `localdebt.ReadAll` + fold-by-ID pattern already proven at existing scale; O(n) in-memory aggregation, no repeated disk scans |
| Maintainer report rendering | Bounded by distinct persona+model pairs (low dozens at most) | Instant render | Reuse `Aggregate()` grouping idiom; no N+1 file reads, single pass over the aggregation output |
| Send call site at run completion | One send per opted-in run | Never block, delay, or alter the run's primary output or exit code | Detached-goroutine, fail-open `Client.Send` contract (existing, proven by Epic 28.0) |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Schema version mismatch | v1 records (no `Model`) mixed with v2 records in the same aggregation run | v1 records excluded from per-model rows (attribution-incomplete), never cause a read or aggregation error |
| Multi-persona `Reviewers` | A record lists 2+ personas (merged multi-agent findings) | Outcome attributed to every listed persona per the documented rule, not just `Reviewers[0]` |
| Empty aggregation | Maintainer report run on a machine that has never opted in / has no dismissal data | Clean "no data" message; no panic, no non-zero unexpected exit code, no misleading empty table |
| Malformed persisted config | Corrupt `.atcr/config.yaml` `quality_signal` value | Fails safe to disabled; surfaces as a loud error; never a silent re-enable |
| Transport failure | Non-2xx, unreachable endpoint, timeout, or a panic inside the send path | Run's exit code and stdout identical to the gate-disabled baseline — fail-open, absolute |
| Duplicate sends | One session runs both `review` and `reconcile`, each hitting the send call site | Payload is a deterministic aggregate of the same underlying records — a duplicate send is idempotent and detectable, not corrupting |
| `--preview` × gate-state interaction | `--preview` requested while opted out, and while opted in | Identical rendering in both cases; preview always short-circuits before any gate check or network work |

### Defensive Measures Required

- **Input Validation:** Allowlist struct enforcement (fixed field set + regression test) on the quality-signal payload; explicit switch-based key allowlist on `config set` (never a loosened prefix match).
- **Error Handling:** Fail-safe-to-disabled on malformed config (never fail-open); fail-open on transport errors (never alters exit code/stdout); schema-version-aware reads that skip rather than error on v1-without-`Model`.
- **Logging/Audit:** No new logging surface beyond existing CLI stdout/error conventions — `--preview`'s printed payload is the only intentional content-revealing output, and it is exactly what would be sent, nothing more.
- **Rate Limiting:** Not applicable — one send per run, no user-facing endpoint; matches Epic 28.0's existing fail-open, no-retry posture.
- **Graceful Degradation:** Empty-aggregation report renders "no data" instead of erroring; gate-disabled paths short-circuit before any payload allocation or goroutine spawn.

---

## Risks

**Technical:**
- Model cannot be derived from existing `RunID` (confirmed non-reversible by codebase discovery) → Mitigation: attach `Model` directly to `localdebt.Record` via a `SchemaVersion` bump populated at write time (`persistLocalDebt`), never attempt RunID reconstruction.
- New payload/aggregation struct accidentally grows a leaking field as the epic evolves → Mitigation: mirror `event.go`'s locked-allowlist + regression-test pattern exactly, from day one of Phase 1.
- Preview and real-send code paths diverge over time (a field added to one but not the other) → Mitigation: both consume the identical payload-construction function/struct instance; AC 03-03 and AC 06-02 jointly lock this from both sides.
- Transport fork resolved inconsistently between preview and send → Mitigation: resolved explicitly in this design (sibling `Client.Send` method, never the `CloudSyncRecord` extension) before Phase 5 begins, removing the ambiguity at design time rather than discovering it mid-implementation.

**TDD-Specific:**
- High-complexity ACs (01-02 schema bump/exclusion, 01-04 append-only fold) risk becoming a single oversized RED→GREEN cycle → Mitigation: decompose Phase 1 into schema-bump-first, then fold-logic, then grouping/aggregation as three separate RED→GREEN sub-cycles within the phase, each with its own table-driven test file.
- Gate independence (Phase 2) is easy to assert positively but easy to under-test negatively → Mitigation: the six-state truth table must include at least one test asserting the new gate's result is unaffected by `telemetry: false` in the same config file, not just that it reaches the correct value in isolation.
- Fail-open behavior (Phase 5, AC 06-03) is hard to verify without a real network — Mitigation: test against the same do-request seam pattern already proven in `TestClient_Send_EmptyEndpointNoOps`, not a live endpoint.

---

**Next:** `/create-sprint @.planning/plans/active/30.0_community_prompt_quality_signal/ --gated`
