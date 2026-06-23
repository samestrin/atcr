# Sprint 8.0: Reconciler Library Extraction

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 8.0 step-by-step. Complete each step, check off work immediately. This sprint runs in **gated mode** — `/execute-sprint` stops at each phase boundary after the phase GATE task. Resume to continue to the next phase.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Clarifications

### Phase 1 Clarifications (recorded 2026-06-23)

**Key Decisions:**
- `internal/stream/levenshtein.go` **stays in `internal/stream`** — it is NOT moved into the library. It is consumed only by `stream/suggest.go` (ATCR-internal path validation), not by `dedupe.go` (which uses token-set Jaccard). This is the deliberate 2026-06-23 post-discovery override of original-requirements Q5 (85% "move"), ratified by the maintainer. Moving it would smuggle path-validation machinery into the stdlib-only public module.
- Only `internal/stream/severity.go` (`NormalizeSeverity`/`SeverityRank`) moves into the library (`reconcile/severity.go`).
- Fixed stale drift-check at sprint-plan.md final-phase line: it previously read "severity + levenshtein moved"; corrected to reflect severity-only move.

**Scope Boundaries:**
- IN (library): core reconcile logic, `Verification`+verdicts+`mergeVerification`, library-owned `Finding`, `severity.go`.
- OUT (stays ATCR-internal): `gate.go`, `validate.go`, `emit.go`/`discover.go` file-I/O layers, path-validation fields, **and `levenshtein.go`**.

**Technical Approach:**
- Type/IO split executed types-first, never moving I/O and types in the same commit; compile-check both packages between moves.
- API lifted as-is; `Verification` becomes public; module is stdlib-only (empty `require`).

### Phase 2 Clarifications (recorded 2026-06-23)

Resolves three boundary forks surfaced by the Phase-2 safety check, where the code-level reality contradicts the IN/OUT scope lists written before inspection. All three confirmed by the maintainer (avg confidence 90.7%).

**Key Decisions:**
- **Q1 — path-validation after the `Merged` split (1a):** Library `Merged` embeds the path-free library `Finding` (no `PathValid`/`PathWarning`/`PathSuggestion`). ATCR stamps path-validation on `[]JSONFinding` **after** `JSONFindings()` converts `[]Merged` — NOT on `&Merged.Finding`. `emit.go`'s `writeFindingsList` and `RenderText` are reworked to draw path data from the `JSONFinding` layer. Byte-identical oracle preserved because written files derive from `JSONFinding`.
- **Q2 — disagreement/verification merge stays ATCR-internal (2a):** The `MergeJSONFindings` family (`MergeJSONFindings`, `mergeVerification`, `verdictRank`, `widestDisagreement`, `mergePathFields`, `unionReviewers`, `joinEvidence`) and all of `disagree.go` (`BuildDisagreements`, `DisagreementsFile`, the radar renderers) **stay in `internal/reconcile`** — every one takes/returns the ATCR-internal `JSONFinding` or uses `os`/`html`/markdown rendering, structurally incompatible with the stdlib-only library. **Sprint-plan task 2.2 step 4 ("Move disagree.go → reconcile/disagree.go") is removed.** The library public surface for this sprint EXCLUDES `BuildDisagreements`, `DisagreementsFile`, and the `MergeJSONFindings` family.
- **Q3 — skipped-source bookkeeping stamped one layer up (3a):** Library `Source` stays `{Name, []Finding}` (no `SkippedFiles`). Library `Reconcile` always emits `Summary.SkippedSources = []` / `SkippedSourceCount = 0`; ATCR's `RunReconcile`/adapter layer stamps the real values onto `Result.Summary` from `discover.Source.SkippedFiles` after `Reconcile` returns. summary.json stays byte-identical.

**Factual correction (original-requirements Clarifications Q4):** `mergeVerification` is NOT called from `Merge()` (the core algorithm); it is called only from `MergeJSONFindings` (the ATCR-internal post-reconcile gray-zone judge-ruling merge, consumed by `internal/debate`). It is therefore ATCR-internal, not part of the lifted library surface.

**Scope Boundaries (revised):**
- IN (library, Phase 2): `Reconcile`/`Options`/`Result`/`Summary`, `Merge`/`Merged` (path-free), `dedupe`/`cluster`/`confidence`/`attribution`, `AmbiguousCluster`/`AmbiguousID`/`AmbiguousHash`, severity (`SeverityRank`/`NormalizeSeverity`), `Verification`/verdicts.
- OUT (stays ATCR-internal): `disagree.go` (radar + `BuildDisagreements`), the `MergeJSONFindings` family incl. `mergeVerification`/`verdictRank`, `JSONFinding`, path-validation stamping, `gate.go`/`validate.go`/`discover.go`/`emit.go` I/O, adjudication machinery.

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

Extract ATCR's deterministic reconciler out of `internal/reconcile` into a standalone, embeddable Go module at `./reconcile/` (module path `github.com/samestrin/atcr/reconcile`), consumed by ATCR through a root `replace` directive. The public API is lifted **as-is** — `Reconcile(sources []Source, opts Options) Result` plus `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, and verdict constants — so ATCR keeps byte-identical behavior. A `reconcile-json/v1` JSON adapter, README + runnable godoc example, dual licensing (Apache 2.0 + commercial placeholder), and independent module CI complete the deliverable.

### Why This Matters

The reconciler is ATCR's core architectural moat — deterministic clustering, dedupe, confidence scoring, ambiguity sidecar, and inline disagreement annotation. Today it is buried inside the CLI and cannot be embedded by other tools or licensed. Extracting it makes ATCR the reference implementation, gives the finding format standards gravity, and opens a licensing revenue path without running infrastructure.

### Key Deliverables

- `./reconcile/` nested Go module with own `go.mod` (stdlib-only) and root `replace` directive.
- Lifted-as-is public API; all 9 ATCR consumer packages re-import the library; byte-identical fixtures preserved.
- `reconcile/adapter/json` `reconcile-json/v1` adapter (decode single/array → `[]Source`; encode `Result` → versioned envelope).
- `reconcile/README.md` with godoc, runnable `ExampleReconcile()`, install + quickstart, licensing section.
- `reconcile/LICENSE` (Apache 2.0) + `reconcile/LICENSE-COMMERCIAL.md` placeholder; no enforcement code.
- Independent module CI (`reconcile-module.yml` on tag push + PR-time `./reconcile` job in `ci.yml`); `docs/scorecard.md` citation.

### Success Criteria

- Root `go test ./...` and `go test ./reconcile/...` both green; zero behavioral change confirmed.
- `findings.json`, `ambiguous.json`, `disagreements.json` zero-diff against pre-extraction baseline.
- `go doc github.com/samestrin/atcr/reconcile` shows the full lifted public surface.
- JSON adapter round-trips with byte stability and no path-validation fields leaking into output.
- Dual CI proves the boundary gap is closed (deliberate `./reconcile` break caught by PR job, not by root `go test ./...`).

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Moderate 🔄 (RED → GREEN → ADVERSARIAL → REFACTOR) — derived from complexity 9/12.

**Mechanical-move discipline (critical for this sprint):** Most of Stories 1–2 is a mechanical relocation of existing, already-tested code. The oracle for those moves is the **existing test corpus passing** and **byte-identical fixtures**, NOT new RED tests. Resist writing new RED tests for moved behavior.

**NEW RED tests apply only to genuinely new behavior:**
- `TestBoundaryAdapter_FindingConversionRoundTrip` — `stream.Finding` → `reconcile.Finding` → `JSONFinding` with path-validation fields preserved (Phase 2).
- `TestDecode_SingleSourceObject`, `TestDecode_ArrayOfSources`, `TestEncode_VersionedEnvelope`, `TestByteStability_IdenticalOutput`, `TestNoPathValidationFieldsInOutput` (Phase 4).

**Adversarial review:** ENABLED 🎯 — inline-fix severities **CRITICAL/HIGH**; **MEDIUM/LOW** deferred to tech debt. Each element's review runs in a **fresh subagent** with no memory of the implementation.

**Gated execution:** A Phase-Boundary GATE (subagent integration review) runs as the last task of every phase. `/execute-sprint` stops at each boundary.

---

## About This Document

| Document | Purpose |
|----------|---------|
| [sprint-design.md](plan/sprint-design.md) | Architecture, decomposition, test strategy |
| [original-requirements.md](plan/original-requirements.md) | User's actual request (source of truth) |
| [user-stories/](plan/user-stories/) | Feature requirements |
| [acceptance-criteria/](plan/acceptance-criteria/) | Validation requirements with DoD |
| [documentation/](plan/documentation/) | Go module, API, JSON adapter, testing, CI references |

---

## Sprint Conventions

### Testing Tiers

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./reconcile/ -run <TestName>` |
| T2: Module | After completing an element | `go test ./reconcile/...` or `go test ./internal/<pkg>/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` (root) **and** `go test ./reconcile/...` (library) — root does NOT cross the nested `go.mod` boundary |

### DoD Verification Checklist
1. Tests (T3): All passing in **both** modules (root + `./reconcile`)
2. Coverage: ≥80% in both modules
3. Lint: `golangci-lint run ./...` and `golangci-lint run ./reconcile/...` clean; `gofmt -l` empty
4. Build: `go build ./...` and `go build ./reconcile/...` succeed
5. Docs: README/godoc/example updated where the phase touches them

### DoD Report Template
```
Story-{N} DoD Complete
Auto: {X}/5 | Story-Specific: {Y}/{Z}
Manual Review: [ ] Code reviewed
```

### Commit Process
Stage only files changed by this phase — do NOT use `git add .` or `git add -A` (other sessions may have uncommitted work).
`git add [specific files] && git commit -m "<type>(<scope>): <message>"`

---

## Development Standards

**References:** [implementation-standards.md](../../../specifications/implementation-standards.md), [coding-standards.md](../../../specifications/coding-standards.md), [git-strategy.md](../../../specifications/git-strategy.md).

- **Git:** GitHub Flow on `feature/8.0_reconciler_library`. Conventional Commits (`type(scope): description`). Small atomic commits. Squash-and-merge to `main` via PR; CI must pass (fmt, vet, lint, tests).
- **Go:** Go 1.25 toolchain. Library is **stdlib-only** in non-test files; `testify` confined to `*_test.go`. `go mod tidy` in `./reconcile/` must yield an empty `require` block.
- **Determinism is a contract:** preserve `sortMerged` total order (severity desc → file → line); keep integer cross-multiply Jaccard thresholds (`inter*10 vs union*N`) — no float conversions. Output must remain byte-identical.
- **Boundary hygiene:** never move I/O and types in the same commit — relocate types first, compile-check both packages, then relocate I/O.

---

## External Resources

From [plan/documentation/](plan/documentation/):

- **[CRITICAL]** [Go Module & Standard Library](plan/documentation/go-module-stdlib.md) — nested module + `replace`, stdlib-only constraint.
- **[CRITICAL]** [Reconciler Public API & Verification Interface](plan/documentation/reconciler-api-verification.md) — lifted-as-is public surface; public/private boundary.
- **[CRITICAL]** [JSON Format Adapter (reconcile-json/v1)](plan/documentation/json-adapter.md) — decode/encode schema.
- **[IMPORTANT]** [Testing with testify](plan/documentation/testing-testify.md) — table-driven tests, runnable godoc example, byte-identical fixture mandate.
- **[IMPORTANT]** [Linting & CI/CD](plan/documentation/linting-ci.md) — golangci-lint config, dual-coverage module CI. **Resolve golangci-lint version drift (`ci.yml` v2.6.2 vs target v2.12.2) before Phase 5.**

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation & Scaffold (2 days)

**Stories:** 2 (partial), 1 (start) | **Focus:** Create the nested module, wire the root `replace` directive, split `emit.go`/`discover.go` into types-only (library) vs I/O (ATCR), stub the boundary adapter package.

### 1.1 [x] **[Module scaffold & replace wiring - RED](plan/user-stories/02-public-api-embeddability.md)**
   **AC:** [02-01](plan/acceptance-criteria/02-01-nested-module-scaffold.md), [01-01](plan/acceptance-criteria/01-01-root-replace-directive-consumption.md)
   1. Analyze AC 02-01/01-01: identify testable units (module builds, `replace` resolves, root `go build` succeeds).
   2. Write/identify failing checks: `go build ./reconcile/...` (module does not yet exist → fails); a scaffold smoke test `reconcile/doc_test.go` referencing the package.
   3. Verify they fail correctly (package missing).
   **Files:** `reconcile/doc_test.go` | **Duration:** 1-2h

### 1.2 [x] **[Module scaffold, replace directive & type/IO split - GREEN](plan/user-stories/02-public-api-embeddability.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT. Execute mechanically — **types first, then I/O, never same commit**:
   1. Create `./reconcile/go.mod`: `module github.com/samestrin/atcr/reconcile`, `go 1.25`, no `require` block.
   2. Add `replace github.com/samestrin/atcr/reconcile => ./reconcile` to root `go.mod`.
   3. Split `internal/reconcile/emit.go`: move `Verification`, `VerdictConfirmed/Refuted/Unverifiable`, and the library `Finding` type into `reconcile/`; keep the file-I/O layer ATCR-internal. (AC 01-03, 02-04)
   4. Split `internal/reconcile/discover.go`: move the `Source` type into `reconcile/`; keep `Discover()` file-reading ATCR-internal. (AC 01-03, 02-04)
   5. Create stub `internal/reconcile/adapter/adapter.go` — the package boundary for `stream.Finding` ↔ `reconcile.Finding` (signatures only, completed in Phase 2). (AC 02-04)
   6. Verify `go build ./reconcile/...` succeeds with no non-stdlib imports (AC 02-03); `go build ./...` still compiles.
   7. COMMIT: `git commit -m "feat(reconcile): scaffold nested module + replace directive + type/IO split (green)"`
   **Files:** `reconcile/go.mod, reconcile/verification.go, reconcile/source.go, go.mod, internal/reconcile/emit.go, internal/reconcile/discover.go, internal/reconcile/adapter/adapter.go` | **Duration:** 0.5-1d

### 1.2.A [x] **[Foundation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-public-api-embeddability.md)**
   **Changed Files:** reconcile/{go.mod,doc.go,verification.go,finding.go,source.go,doc_test.go}, go.mod, internal/reconcile/emit.go, internal/reconcile/adapter/adapter.go

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 1.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - BOUNDARY: Any `os`/`io` import leaked into library files? `replace` directive correct and resolvable? Type split leaves dangling references?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh-context review, 2026-06-23):** No findings.

   Verified by the subagent: library is stdlib-only (empty `require`, no `go.sum`, only `testing` in tests); both modules build/vet/test clean; full internal corpus passes uncached; `Verification` struct + verdict constants byte-identical to prior inline defs (git diff confirmed); type aliases preserve identical types + JSON serialization; library `Finding` excludes ATCR path-validation fields; `replace` resolves; adapter panic-stubs unreferenced by non-test code (cannot be silently called).

   **✅ Adversarial review passed** — proceeding.

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 1.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 1.3 [x] **[Foundation - REFACTOR](plan/user-stories/02-public-api-embeddability.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any). → None reported.
   2. Improve code and tests (T1), validate (T3 both modules). → Code already minimal; lint/gofmt clean, both module corpora green. No speculative changes added.
   3. COMMIT: no refactor commit (no code changes — review passed clean).
   **Duration:** 2-4h

### 1.4 [x] **Phase 1 - DoD Validation**
   - [x] `go build ./reconcile/...` and `go build ./...` succeed
   - [x] `go test ./reconcile/...` green; `go test ./...` green (no behavioral change — full root corpus passed)
   - [x] No `os`/`io` imports in library non-test files (zero imports — fully stdlib-only)
   - [x] `replace` directive resolves; root module consumes library (emit.go + adapter/adapter.go import it)
   - [x] Lint clean (both modules — 0 issues each)
   - Emit DoD report (template above).

### 1.5 [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence)

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase's implementation — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Library exports the types Phase 2 will populate (`Verification`, `Verdict*`, `Source`, `Finding`)?
       - CONFIG SURFACE: `go.mod`/`replace` correct, no stray `require`, back-compat for root build?
       - INTEGRATION: ATCR still compiles against the split; no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Phase 2 can move pure logic into the module without rework?
       - REGRESSION: Existing ATCR behavior intact (corpus green)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent gate findings (fresh-context hostile integrator, 2026-06-23):**

   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | HIGH (fwd) | merge.go:56, cluster.go, dedupe.go, reconcile.go:64, disagree.go | Phase 2 is a PORT not a verbatim move — logic hard-coupled to `stream.Finding`; verbatim move forces forbidden `reconcile→internal/stream` import | TD-001; Phase-2 carry-forward note added |
   | HIGH (fwd) | discover.go:25 | Two `Source` types (internal discovery vs library) must be bridged at the boundary in Phase 2/3 | TD-002 (already in Phase-1 design) |
   | MEDIUM (fwd) | reconcile/finding.go | Path-validation fields are a lossy library boundary; adapter must re-stamp | TD-003 (documented design; covered by 2.1) |
   | LOW | (task list) | `AmbiguousCluster` is in `dedupe.go` not `ambiguous.go` | TD-004 |

   **Phase-1 verdict:** Phase 1 code is CORRECT (both modules build/vet/test green; aliases preserve JSON+pointer semantics; stdlib-only; go.mod clean + back-compatible). All findings are **forward-looking Phase-2 hazards**, NOT in-phase defects — nothing to fix in Phase 1, so the "CRITICAL/HIGH → fix before boundary" rule (which targets in-phase defects) does not apply. Captured as TD-001..TD-004 in `tech-debt-captured.md` and carried into the Phase 2 header. **✅ Phase gate passed.**

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

   **🚧 GATED STOP:** Phase 1 complete. Stop here. Resume `/execute-sprint` to begin Phase 2.

---

## Phase 2: Core Extraction (3 days)

**Stories:** 1, 2 (completion) | **Focus:** Mechanically move all pure reconcile logic into the library; migrate severity canonical ownership; preserve `sortMerged` total order and integer cross-multiply dedupe thresholds exactly. Complete the boundary adapter.

> **⚠️ Phase 1 GATE carry-forward (see `tech-debt-captured.md` TD-001..TD-004):** This is a **PORT, not a verbatim move.** The logic files (`merge.go`/`cluster.go`/`dedupe.go`/`reconcile.go`/`disagree.go`) embed/consume `stream.Finding`; moving them verbatim forces a forbidden `reconcile → internal/stream` import. Rewrite `stream.Finding → reconcile.Finding` per file, relocate `SeverityRank`/`NormalizeSeverity` (and eliminate the `merge.go:30` copy), and bridge the two `Source` types + path-validation fields in the adapter. `AmbiguousCluster` lives in `dedupe.go` (not `ambiguous.go`). Byte-identical fixtures remain the oracle after the field-name swap.

### 2.1 [x] **[Boundary adapter conversion - RED](plan/user-stories/01-reference-implementation-preservation.md)**
   **AC:** [01-02](plan/acceptance-criteria/01-02-boundary-adapter-finding-conversion.md)
   1. **Capture the byte-identical baseline first:** snapshot `findings.json`, `ambiguous.json`, `disagreements.json` from a pre-extraction run; commit as test fixtures (oracle for Phase 3).
   2. Write the **new-behavior** RED test `TestBoundaryAdapter_FindingConversionRoundTrip` in `internal/reconcile/adapter/adapter_test.go`: `stream.Finding` → `reconcile.Finding` → `JSONFinding` preserving all 9 wire fields AND `PathValid`/`PathWarning`/`PathSuggestion`; assert `*Verification` pointer-identity preserved.
   3. Verify it fails correctly (adapter is still a stub).
   **Files:** `internal/reconcile/adapter/adapter_test.go`, fixture baselines | **Duration:** 3-5h

### 2.2 [x] **[Core logic move + severity migration + adapter completion - GREEN](plan/user-stories/01-reference-implementation-preservation.md)**
   Minimal code to pass (T1 per move), verify all pass (T2), COMMIT in mechanical batches. **No new RED tests for moved behavior — the existing corpus is the oracle.**
   1. Move `reconcile.go` → `reconcile/reconcile.go` (`Reconcile` entry + `Options`/`Result`/`Summary`; preserve `sortMerged`). (AC 02-02)
   2. Move `merge.go` → `reconcile/merge.go` — **only** `Merge`/`Merged` (path-free) + the pure helpers (`mergeSeverity`, `distinctReviewers`, `longestField`, `modalCategory`, `maxEstMinutes`, `mergeEvidence`, `confidenceFor`) + severity/confidence/category consts. **The `MergeJSONFindings` family (`MergeJSONFindings`, `mergeVerification`, `verdictRank`, `widestDisagreement`, `mergePathFields`, `unionReviewers`, `joinEvidence`) STAYS in `internal/reconcile` (Phase 2 Clarification Q2 — operates on ATCR-internal `JSONFinding`).** (AC 02-02)
   3. Move `dedupe.go` → `reconcile/dedupe.go` (Jaccard integer cross-multiply thresholds preserved exactly). (AC 02-02)
   4. ~~Move `disagree.go` → `reconcile/disagree.go`~~ **REMOVED (Phase 2 Clarification Q2):** `disagree.go` (`BuildDisagreements`, `DisagreementsFile`, radar renderers) stays ATCR-internal — consumes `JSONFinding` and uses `os`/`html`/markdown rendering, incompatible with the stdlib-only library.
   5. Move `confidence.go` → `reconcile/confidence.go` (`ConfidenceForVerdict`/`ConfidenceAtOrAbove`). (AC 02-02)
   6. Move `cluster.go` → `reconcile/cluster.go` (FILE, LINE±3 single-linkage). (AC 02-02)
   7. Move `ambiguous.go` → `reconcile/ambiguous.go` (`AmbiguousID`/`AmbiguousHash`/`AmbiguousCluster`). (AC 02-02)
   8. Move `attribution.go` → `reconcile/attribution.go` (`EvidenceSep`/`FixAttribution`). (AC 02-02)
   9. Move `internal/stream/severity.go` (`NormalizeSeverity`/`SeverityRank`) → `reconcile/severity.go`; eliminate the `merge.go:30` init-copy (library becomes canonical owner). (AC 02-05)
   10. Move pure-logic tests with their code (e.g. `TestReconcile_TwoReviewersAgreeHighConfidence` → `reconcile/reconcile_test.go`). (AC 02-02)
   11. Complete `internal/reconcile/adapter/adapter.go`: `stream.Finding` ↔ `reconcile.Finding` conversion + path-validation stamping on `JSONFinding` → makes 2.1 RED green. (AC 01-02)
   12. Enforce stdlib-only: `golangci-lint run ./reconcile/...` clean; testify confined to `*_test.go`; `go mod tidy ./reconcile` yields empty `require`. (AC 02-03)
   13. Verify `go test ./reconcile/...` green and `go test ./...` green after each batch; COMMIT per logical batch: `git commit -m "feat(reconcile): move <unit> into library module (green)"`
   **Files:** `reconcile/*.go`, `reconcile/*_test.go`, `internal/reconcile/adapter/adapter.go`, `internal/stream/severity.go` | **Duration:** 1.5-2d

### 2.2.A [ ] **[Core Extraction - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-reference-implementation-preservation.md)**
   **Changed Files:** [all files modified in 2.2 — absolute paths]

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 2.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Empty `[]Source`, single source, identical findings, verdict mutation across boundary?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - DETERMINISM: `sortMerged` total order preserved? Jaccard kept as integer cross-multiply (no float drift)? Severity dual-ownership fully eliminated (no second source of truth)?
       - BOUNDARY: `*Verification` pointer-identity preserved through the adapter? Library still stdlib-only?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 2.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 2.3 [ ] **[Core Extraction - REFACTOR](plan/user-stories/01-reference-implementation-preservation.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any).
   2. Improve code and tests (T1), validate (T3 both modules).
   3. COMMIT: `git commit -m "refactor(reconcile): address review + clean up extracted core"`
   **Duration:** 3-5h

### 2.4 [ ] **Phase 2 - DoD Validation**
   - [ ] `go test ./reconcile/...` green (library corpus moved + passing)
   - [ ] `go test ./...` green (ATCR corpus, no behavioral change)
   - [ ] `go doc github.com/samestrin/atcr/reconcile` shows `Reconcile`, `Source`, `Finding`, `Merged`, `Options{ReconciledAt,Partial,Merges,Root}`, `Result`, `Summary`, `Verification`, `Verdict*`
   - [ ] `go mod tidy ./reconcile` → empty `require`; golangci-lint clean (both)
   - [ ] `TestBoundaryAdapter_FindingConversionRoundTrip` green
   - Emit DoD report.

### 2.5 [ ] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Public API surface matches the lifted-as-is contract Phase 3 consumers will import?
       - CONFIG SURFACE: Library `go.mod` clean (empty require), severity ownership single-sourced?
       - INTEGRATION: Boundary adapter converts both directions correctly; `*Verification` pointer-identity holds?
       - PHASE-EXIT CONTRACT: Phase 3 can flip all 9 consumers against this surface without further library edits?
       - REGRESSION: Determinism invariants (`sortMerged`, Jaccard thresholds) intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

   **🚧 GATED STOP:** Phase 2 complete. Stop here. Resume `/execute-sprint` to begin Phase 3.

---

## Phase 3: Consumer Import-Flip (2 days)

**Stories:** 1 (completion) | **Focus:** Rewire all 9 consumer packages to import `github.com/samestrin/atcr/reconcile`; grep-verify no residual `internal/reconcile` type re-declarations; confirm byte-identical fixtures. **Oracle = existing corpus + zero-diff fixtures; no new RED tests.**

### 3.1 [ ] **[Consumer flip readiness - RED](plan/user-stories/01-reference-implementation-preservation.md)**
   **AC:** [01-04](plan/acceptance-criteria/01-04-consumer-package-import-flip.md), [01-05](plan/acceptance-criteria/01-05-byte-identical-fixtures.md)
   1. Confirm the fixture baseline captured in 2.1 is committed and diff-able.
   2. Establish the failing condition: a grep assertion that any package still importing `internal/reconcile` for moved types is a failure; current state (pre-flip) "fails" this assertion → that is the RED signal.
   3. No new unit tests written — the corpus + fixture-diff is the oracle.
   **Files:** (none new; baseline confirmation) | **Duration:** 1h

### 3.2 [ ] **[Flip all 9 consumer packages - GREEN](plan/user-stories/01-reference-implementation-preservation.md)**
   Flip imports package-by-package; after each, `go build ./...` + targeted T2; COMMIT per package. (AC 01-04)
   1. `cmd/atcr` (github.go, reconcile.go, report.go, resume.go, review.go, verify.go) → import library.
   2. `internal/debate` (debate.go, emit.go, envelope.go, select.go) → import library.
   3. `internal/verify` (votes.go, severity.go) → import library.
   4. `internal/report` (disagree.go, render.go) → import library.
   5. `internal/ghaction` (render.go) → import library.
   6. `internal/mcp` (handlers.go) → import library.
   7. `internal/fanout` (metrics.go, postprocess.go) → import library (`NormalizeSeverity`/`SeverityRank`).
   8. `internal/scorecard` (reconcile.go) → import library.
   9. `internal/registry` (config.go) → import library severity helpers.
   10. Grep-verify: no `internal/reconcile` severity/verdict re-declarations remain in any ATCR package. (AC 01-04)
   11. Diff `findings.json`, `ambiguous.json`, `disagreements.json` against the pre-extraction baseline — must be **zero diff**. (AC 01-05)
   12. `go test ./...` green in root module (full ATCR corpus). (AC 01-05, 01-06)
   13. COMMIT per package: `git commit -m "refactor(<pkg>): import reconcile library (green)"`
   **Files:** the 9 packages above | **Duration:** 1-1.5d

### 3.2.A [ ] **[Consumer Flip - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-reference-implementation-preservation.md)**
   **Changed Files:** [all files modified in 3.2 — absolute paths]

   **Spawn a fresh subagent** via the Agent tool. No memory of 3.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 3.2]
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - COMPLETENESS: Any consumer missed (re-declares a severity/verdict constant locally)? Any residual `internal/reconcile` import for a moved type? Fixtures truly zero-diff (not "close")?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 3.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 3.3 [ ] **[Consumer Flip - REFACTOR](plan/user-stories/01-reference-implementation-preservation.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any).
   2. Improve clarity (T1), validate (T3 both modules).
   3. COMMIT: `git commit -m "refactor(reconcile): finalize consumer import-flip"`
   **Duration:** 2-4h

### 3.4 [ ] **Phase 3 - DoD Validation**
   - [ ] All 9 packages compile importing the library; no local re-declarations
   - [ ] Grep finds zero residual `internal/reconcile` moved-type imports in non-adapter code
   - [ ] `findings.json`/`ambiguous.json`/`disagreements.json` zero-diff vs baseline
   - [ ] `go test ./...` green (root); `go test ./reconcile/...` green
   - [ ] Lint clean (both modules)
   - Emit DoD report.

### 3.5 [ ] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Every consumer uses the library's public types; no shadow copies remain?
       - CONFIG SURFACE: No leftover `internal/reconcile` type definitions that should be gone?
       - INTEGRATION: Cross-package calls correct against the new import; `gate.go`/`validate.go` still ATCR-internal and functional?
       - PHASE-EXIT CONTRACT: Extraction behaviorally complete — Phase 4 only adds adapter/docs/licensing?
       - REGRESSION: Byte-identical fixtures + full corpus green?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

   **🚧 GATED STOP:** Phase 3 complete. Stop here. Resume `/execute-sprint` to begin Phase 4.

---

## Phase 4: Adapter, Docs & Licensing (2 days)

**Stories:** 3, 4, 5 | **Focus:** JSON adapter with `reconcile-json/v1` schema, README + godoc example, Apache 2.0 and commercial license files.

### 4.1 [ ] **[JSON adapter - RED](plan/user-stories/03-json-format-adapter.md)**
   **AC:** [03-01](plan/acceptance-criteria/03-01-decode-single-and-array-sources.md), [03-02](plan/acceptance-criteria/03-02-encode-result-to-versioned-envelope.md), [03-03](plan/acceptance-criteria/03-03-byte-stability-and-omitempty.md), [03-04](plan/acceptance-criteria/03-04-path-validation-isolation-and-schema-independence.md)
   1. Write failing tests in `reconcile/adapter/json/adapter_test.go`:
      - `TestDecode_SingleSourceObject` — single `{}` → `[]Source` (len 1).
      - `TestDecode_ArrayOfSources` — `[...]` → `[]Source`; unknown fields ignored.
      - `TestEncode_VersionedEnvelope` — output has `"version":"reconcile-json/v1"`, RFC3339 `reconciled_at`, `findings[]`, `summary`, `ambiguous[]`.
      - `TestByteStability_IdenticalOutput` — two encodes of same `Result` → identical bytes; absent `disagreement`/`verification` → no keys.
      - `TestNoPathValidationFieldsInOutput` — no `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`; `"version"` never `"atcr-findings/v1"`.
   2. Verify all fail correctly (adapter not implemented).
   **Files:** `reconcile/adapter/json/adapter_test.go` | **Duration:** 4-6h

### 4.2 [ ] **[JSON adapter, README, example, licenses - GREEN](plan/user-stories/03-json-format-adapter.md)**
   Minimal code to pass (T1), verify all pass (T2), COMMIT per deliverable.
   1. `reconcile/adapter/json/adapter.go` decode: sniff first byte (`[` → array, else single-wrap) → `[]reconcile.Source`; ignore unknown fields; validate required `severity`/`file`/`line`. (AC 03-01)
   2. `reconcile/adapter/json/adapter.go` encode: `reconcile.Result` → versioned `reconcile-json/v1` envelope. (AC 03-02)
   3. Apply `omitempty` to `Disagreement`/`*Verification`; byte-stability holds. (AC 03-03)
   4. Confirm no path-validation fields in encoded output (round-trip integration test). (AC 03-04)
   5. Write `reconcile/README.md`: public API surface (lifted-as-is), behavior (clustering/Jaccard/confidence/disagreement), `go get` install + quickstart, licensing pointer. (AC 04-01, 04-02)
   6. Write `reconcile/example_test.go`: runnable `ExampleReconcile()` — two sources, overlapping findings, merged `Result` with confidence + disagreement; deterministic output via `sortMerged`; `go test ./reconcile/...` runs it green. (AC 04-03)
   7. Add `reconcile/LICENSE` — verbatim Apache 2.0, copyright Sam Estrin 2026. (AC 04-04)
   8. Add `reconcile/LICENSE-COMMERCIAL.md` — dual-license pairing statement, reachable contact path, explicit no-enforcement/no-payment-gating declaration. (AC 05-01, 05-02)
   9. Add licensing section to `reconcile/README.md` cross-referencing both license files. (AC 05-03)
   10. Code scan: grep `./reconcile/` finds zero license-check or payment-gating code. (AC 05-03)
   11. COMMIT per deliverable: `git commit -m "feat(reconcile): json adapter | docs | licensing (green)"`
   **Files:** `reconcile/adapter/json/adapter.go`, `reconcile/README.md`, `reconcile/example_test.go`, `reconcile/LICENSE`, `reconcile/LICENSE-COMMERCIAL.md` | **Duration:** 1-1.5d

### 4.2.A [ ] **[Adapter/Docs/Licensing - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-json-format-adapter.md)**
   **Changed Files:** [all files modified in 4.2 — absolute paths]

   **Spawn a fresh subagent** via the Agent tool. No memory of 4.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 4.2]
     - Checklist (pass verbatim):
       - SECURITY: Malformed JSON, deep nesting, decode DoS? License placeholder language creating inadvertent commercial commitment?
       - EDGE CASES: Empty input, single vs array, missing optional fields, unknown fields?
       - ERROR HANDLING: Decode/encode errors propagated (never swallowed)? Required-field validation present?
       - PERFORMANCE: Unbounded input handling?
       - SCHEMA: `"version"` correct and never `atcr-findings/v1`? `omitempty` correct? No path-validation fields leak? Example output deterministic?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 4.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 4.3 [ ] **[Adapter/Docs/Licensing - REFACTOR](plan/user-stories/03-json-format-adapter.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any).
   2. Improve adapter + docs quality (T1), validate (T3).
   3. COMMIT: `git commit -m "refactor(reconcile): address review + polish adapter/docs/licensing"`
   **Duration:** 2-4h

### 4.4 [ ] **Phase 4 - DoD Validation**
   - [ ] All 5 RED tests from 4.1 green
   - [ ] `ExampleReconcile()` runs green; `go doc` renders it
   - [ ] README documents full public surface (cross-checked vs `go doc`), install, quickstart, licensing
   - [ ] `reconcile/LICENSE` (Apache 2.0) + `reconcile/LICENSE-COMMERCIAL.md` present; no enforcement code
   - [ ] `go test ./reconcile/...` green; lint clean
   - Emit DoD report.

### 4.5 [ ] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: Adapter is a thin stdlib wrapper, replaceable; schema independently versioned?
       - CONFIG SURFACE: License files complete and non-committal; README licensing section accurate?
       - INTEGRATION: Adapter imports only library public API + `encoding/json`; no ATCR coupling?
       - PHASE-EXIT CONTRACT: Phase 5 can wire CI against a stable, documented module?
       - REGRESSION: Library + ATCR corpus still green?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to phase stop
   **Duration:** 15-30 min

   **🚧 GATED STOP:** Phase 4 complete. Stop here. Resume `/execute-sprint` to begin Phase 5.

---

## Phase 5: CI, Leaderboard & Validation (1 day)

**Stories:** 6 + final validation | **Focus:** Wire independent module CI (tag-push + PR-time); add `docs/scorecard.md` citation; prove dual CI jobs green.

### 5.1 [ ] **[CI boundary-gap proof - RED](plan/user-stories/06-independent-module-ci-leaderboard-citation.md)**
   **AC:** [06-01](plan/acceptance-criteria/06-01-tag-push-release-gate-workflow.md), [06-02](plan/acceptance-criteria/06-02-pr-time-module-test-job-boundary-gap.md), [06-03](plan/acceptance-criteria/06-03-scorecard-methodology-reference-citation.md)
   1. Establish the failing condition: deliberately break `./reconcile` (e.g. a failing assertion) and confirm root `go test ./...` does **NOT** catch it — this is the gap the PR-time job must close. Document the broken-state expectation.
   2. CI is configuration; the boundary-gap proof is the test. Revert the deliberate break after capturing the RED signal.
   **Files:** (temporary local break only) | **Duration:** 1-2h

### 5.2 [ ] **[Module CI, scorecard citation, boundary-gap proof - GREEN](plan/user-stories/06-independent-module-ci-leaderboard-citation.md)**
   Minimal config to pass; verify in CI; COMMIT.
   1. Create `.github/workflows/reconcile-module.yml`: triggers on tag push (scope tag filter to module release convention, not ATCR app tags); `cd ./reconcile && gofmt -l . && golangci-lint run --timeout 5m && go test -race ./...`; reuse `[gauntlet]` runner + Go 1.25. (AC 06-01)
   2. Add a PR/push job in `.github/workflows/ci.yml`: `cd ./reconcile && go test ./...`; inline comment `# root go test ./... does NOT cross ./reconcile's go.mod boundary`. (AC 06-02)
   3. **Resolve golangci-lint version drift:** pin `v2.12.2` in `reconcile-module.yml`; reconcile against `ci.yml` (currently `v2.6.2/@v8` vs target `v2.12.2/@v3`) to a single pinned version; document in `.golangci.yml`. (AC 06-01)
   4. Update `docs/scorecard.md`: additive note citing `github.com/samestrin/atcr/reconcile` as the standalone reference implementation backing every scorecard record. (AC 06-03)
   5. Prove the boundary gap is closed: re-introduce the deliberate `./reconcile` break and verify the PR-time `ci.yml` job catches it while root `go test ./...` does not; then revert. (AC 06-02)
   6. COMMIT: `git commit -m "ci(reconcile): independent module CI + scorecard citation (green)"`
   **Files:** `.github/workflows/reconcile-module.yml`, `.github/workflows/ci.yml`, `.golangci.yml`, `docs/scorecard.md` | **Duration:** 0.5d

### 5.2.A [ ] **[CI & Validation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/06-independent-module-ci-leaderboard-citation.md)**
   **Changed Files:** [all files modified in 5.2 — absolute paths]

   **Spawn a fresh subagent** via the Agent tool. No memory of 5.2 — intentional. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 5.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): [LIST FROM 5.2]
     - Checklist (pass verbatim):
       - SECURITY: Workflow secrets/permissions over-scoped? Untrusted runner exposure?
       - EDGE CASES: Tag filter fires on ATCR app tags? PR job skips on path filters it shouldn't?
       - ERROR HANDLING: CI fails loudly on lint/test/race failure (no silent pass)?
       - CONSISTENCY: golangci-lint version single-pinned across `ci.yml`/`reconcile-module.yml`/`.golangci.yml`?
       - PROOF: Boundary-gap proof genuinely demonstrates root `go test ./...` misses `./reconcile` breakage?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.3, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.3 [ ] **[CI & Validation - REFACTOR](plan/user-stories/06-independent-module-ci-leaderboard-citation.md)**
   1. Fix CRITICAL/HIGH issues from 5.2.A (if any).
   2. Tidy workflow YAML + scorecard wording (T1), validate.
   3. COMMIT: `git commit -m "refactor(ci): address review + finalize module CI"`
   **Duration:** 2-3h

### 5.4 [ ] **Phase 5 - DoD Validation + Final Sprint Validation**
   - [ ] `reconcile-module.yml` exists; triggers on tag; runs gofmt + golangci-lint + `go test -race`
   - [ ] `ci.yml` has `./reconcile` job; deliberate break caught by PR job, NOT by root `go test ./...`
   - [ ] `docs/scorecard.md` cites `github.com/samestrin/atcr/reconcile`
   - [ ] golangci-lint version single-pinned across all configs
   - [ ] **Final:** `go test ./reconcile/...` green AND `go test ./...` green (root) — zero behavioral change confirmed (AC 01-06)
   - Emit DoD report.

### 5.5 [ ] **Phase 5 - GATE: Final Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5 + full-sprint integration

   **Spawn a fresh subagent** via the Agent tool. No memory of the phase. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 5 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 5 (absolute paths): [LIST]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All 8 epic acceptance criteria satisfied (module + CI + adapter + docs + licensing + scorecard)?
       - CONFIG SURFACE: Dual CI complete; version drift resolved?
       - INTEGRATION: Both modules green independently; boundary gap proven closed?
       - PHASE-EXIT CONTRACT: Sprint deliverable matches original-requirements.md scope (no scope creep, no deferred-epic API reshape pulled in)?
       - REGRESSION: Byte-identical fixtures + full corpus green end-to-end?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Paste the subagent's findings table here (delete rows if none):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | | | |
   | HIGH | | | |

   **Action Required:**
   - CRITICAL/HIGH found → Fix before phase boundary, do NOT stop. Re-run gate.
   - MEDIUM/LOW found → Append to `tech-debt-captured.md`
   - None found → Note "Phase gate passed" and proceed to Final Phase
   **Duration:** 15-30 min

   **🚧 GATED STOP:** Phase 5 complete. Stop here. Resume `/execute-sprint` for Final Phase validation.

---

## Final Phase: Validation

### Validation Checklist
- [ ] All tests passing (T3): `go test ./...` (root) AND `go test ./reconcile/...` (library)
- [ ] Coverage meets threshold (≥80%) in both modules
- [ ] Lint/format clean: `golangci-lint run ./...` + `./reconcile/...`; `gofmt -l` empty
- [ ] Build succeeds: `go build ./...` + `go build ./reconcile/...`
- [ ] Byte-identical fixtures: `findings.json`/`ambiguous.json`/`disagreements.json` zero-diff vs baseline
- [ ] `go doc github.com/samestrin/atcr/reconcile` shows full lifted public surface
- [ ] Dual CI green; boundary-gap proof demonstrated

### Optional: Targeted Mutation Testing
MUTATION_TOOL = UNAVAILABLE (no stryker/mutmut/cargo-mutants detected; Go project). Skip automated mutation. If a Go mutation tool (e.g. `gremlins`) is later installed, target ONLY high-risk changed files — `reconcile/dedupe.go`, `reconcile/merge.go`, `reconcile/reconcile.go` (the determinism-critical paths).
**WARNING:** Do NOT run full-codebase mutation — it can take hours. Target specific files only.

### Drift Analysis
Compare the delivered sprint against [plan/original-requirements.md](plan/original-requirements.md):
- [ ] All 8 epic acceptance criteria met (module + own CI; lifted public API; ATCR imports + tests pass; JSON adapter; godoc + runnable example; Apache 2.0 + commercial placeholder; independent tag-push CI; scorecard citation).
- [ ] Confirm decisions from original-requirements Clarifications honored: nested module at `./reconcile/`, module path `github.com/samestrin/atcr/reconcile`, API lifted **as-is** (no `(*Result, error)` reshape), `Verification` public, severity moved to library (`reconcile/severity.go`); levenshtein stays in `internal/stream` (path-validation only, ATCR-internal).
- [ ] No out-of-scope work pulled in (no HTTP/gRPC, no SDKs, no SARIF, no enforcement code, no clean-API reshape).
