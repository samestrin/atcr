# Sprint 31.0: AXI Compliance

---
executor: /execute-sprint
execution_mode: gated
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 31.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

An `--axi` (Agent eXperience Interface) output mode for `atcr review` and `atcr report` that emits a clean, token-dense TOON/JSON payload instead of ANSI-colored/Markdown human output, with a reconciled exit-code contract, deterministic pagination/truncation, and strict stdout/stderr isolation — so `atcr` becomes safely composable as a subprocess inside autonomous agentic workflows.

### Why This Matters

Human-ergonomic CLI output (color, tables, dividers) wastes an LLM agent's context-window tokens and is brittle to parse programmatically. A first-class `--axi` mode lets other autonomous agents (e.g. a sweeper reviewing its own generated code) consume `atcr`'s findings deterministically and cheaply.

### Key Deliverables

- `FormatAXI` TOON/JSON render dispatch in `internal/report`, reconciled with the existing `atcr-findings/v1` schema
- `--axi` gating of `atcr review`/`atcr resume`'s live human-oriented stdout writes, with shared context-mode propagation
- Explicit, documented MCP `atcr_report` format-enum decision for `FormatAXI`
- Reconciled 0/1/2/3 exit-code contract for AXI mode, with new AXI-introduced errors classified into the existing contract
- Shared `internal/report/pagination.go` line-cap wrapper (default 500 lines, `ATCR_AXI_MAX_LINES` override, `truncated` flag) applied uniformly to both AXI code paths
- Stdout gating + ANSI/OSC escape-sequence pinning test guaranteeing clean `--axi` stdout, with a non-`--axi` regression suite
- `docs/agentic-consumption.md` orchestration guide with a worked autonomous-sweeper example, cross-referenced from `docs/ci-integration.md`

### Success Criteria

- `atcr review --axi`/`atcr report --axi` stdout is byte-clean: zero ANSI/OSC escapes, zero Markdown syntax, TOON/JSON only
- Exit codes 0/1/2/3 are unchanged and unambiguous for `--axi` invocations, verified against `atcr verify`'s precedent
- A 500-line default cap deterministically truncates oversized payloads, exposing `truncated` + true total count, overridable via `ATCR_AXI_MAX_LINES`
- No non-`--axi` regression in `review`/`resume` output
- `docs/agentic-consumption.md` published and linked from `docs/ci-integration.md`

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Moderate 🔄 (auto-calculated from complexity 9/12 COMPLEX)
**Adversarial Review:** ENABLED 🎯 — fresh-subagent review after each element's GREEN task; inline-fix bar `CRITICAL/HIGH`, defer `MEDIUM/LOW` to `clarifications/tech-debt-captured.md`
**Gated Execution:** ENABLED 🚧 — a Phase-Boundary Gate integration review runs after every phase's DoD; `/execute-sprint` stops there for a human checkpoint
**Documentation elements** (Story 5, and AC 02-03) use the Pragmatic cycle (combined draft+write, then adversarial accuracy review, then refactor/polish) since there are no automated tests to RED against — Manual/Integration-typed doc ACs per test-planning-matrix.md.

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
- **T1: Focused** — `go test ./internal/report/... -run <TestName>` or `go test ./cmd/atcr/... -run <TestName>` after each small change
- **T2: Module** — `go test ./internal/report/...` or `go test ./cmd/atcr/...` after completing an element
- **T3: Full** — `go test ./...` for DoD validation / pre-commit

### DoD Verification Checklist
1. Tests (T3): All passing
2. Coverage: ≥80% (project baseline, `config.yaml`)
3. Lint: `golangci-lint run` clean, `go vet ./...` clean
4. Build: `go build ./...` succeeds
5. Docs: Updated where the phase touches documented behavior

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

### Implementation Standards (excerpt)
Black-box module interfaces, replaceable components, single-responsibility modules, primitive-first design. Go/MCP specifics: panic-safe goroutines, `defer` cleanup, return concrete types from constructors and accept interfaces, robust JSON-RPC input validation. Full text: `.planning/specifications/implementation-standards.md`.

### Coding Standards (excerpt)
Go naming per standard conventions (`PascalCase` exported, `camelCase` unexported, snake/lowercase files); imports grouped stdlib → third-party → `github.com/samestrin/atcr/...`; errors returned last, wrapped with `fmt.Errorf("...: %w", err)`, never ignored; `context.Context` first param for I/O; table-driven tests colocated as `*_test.go`; `go fmt`/`goimports`, `golangci-lint run`, `go vet ./...` required before commit. Full text: `.planning/specifications/coding-standards.md`.

### Git Strategy (excerpt)
GitHub Flow / trunk-based: `feature/<desc>` branches from `main`, Conventional Commit messages (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`), squash-merge, CI (`Go CI`: format/vet/lint/tests) must pass before merge. Full text: `.planning/specifications/git-strategy.md`.

---

## External Resources

**[CRITICAL]**
- [CLI Command & Output Control Patterns (Cobra)](plan/documentation/cli-command-patterns.md) — `PersistentFlags()`, `PersistentPreRunE` context injection, `cmd.OutOrStdout()`/`SetErr()`, single-point `exitCode()` resolution
- [Exit-Code Contract & CLI/MCP Dual-Surface Precedent (Epic 3.0 `atcr verify`)](plan/documentation/exit-code-cli-mcp-precedent.md) — existing 0/1/2/3 contract, `report.FormatList()` → MCP enum precedent

**[IMPORTANT]**
- [Existing Agent-Facing Format & Output-Safety Contracts](plan/documentation/agentic-format-precedents.md) — `atcr-findings/v1` wire format, `truncated`/`files_dropped` naming precedent, `sanitizeDisplay` idiom
- [MCP Tool Schema & Format-Enum Propagation](plan/documentation/mcp-schema-format-propagation.md) — `jsonschema-go`/MCP SDK reflection chain
- [AXI Design Principles (axi.md)](plan/documentation/axi-design-principles.md) — the epic's reference source; principles 1/2/3/4/5/6

**[REFERENCE]**
- [TOON Format Reference](plan/documentation/toon-format-reference.md) — tabular-array header, pipe-delimiter, quoting/escape rules

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — AXI Schema & Render Dispatch (2.5 days)

**Items:** Story 1 (AC 01-01, 01-02)

### 1.1 [x] **[FormatAXI Render Dispatch - RED](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Analyze [AC 01-01](plan/acceptance-criteria/01-01-axi-format-render-dispatch.md); identify testable units: format-enum registration, `Render()` dispatch branch, golden-file fixture, zero-findings empty-array case, pipe/comma/colon/newline/unicode escaping
   2. Write tests: golden-file case (`{"axi", FormatAXI, "report.axi", nil}` in `goldenCases`), zero-findings empty-payload test, escaping edge-case table test, unicode-path preservation test
   3. Verify tests fail correctly (missing `FormatAXI`/`renderAXI`)
   **Files:** `internal/report/render_test.go` | **Duration:** 3h

### 1.2 [x] **[FormatAXI Render Dispatch - GREEN](plan/user-stories/01-axi-token-dense-output-mode.md)**
   Add `FormatAXI = "axi"` to the format-enum block, a `case FormatAXI:` branch in `Render()` dispatching to a new `renderAXI` function, and add it to `ValidFormat`/`FormatList`. Implement `renderAXI` as a TOON tabular-array encoder over the same field set as `renderJSON` (severity, file:line, problem, fix, category, est_minutes, evidence, reviewers, confidence), quoting free-text fields per TOON's must-quote rules. Generate the `report.axi` golden fixture via `go test ./internal/report -update`. Run T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `internal/report/render.go`, `internal/report/testdata/report.axi` | **Duration:** 5h

### 1.2.A [x] **[FormatAXI Render Dispatch - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-axi-token-dense-output-mode.md)**
   **Changed Files:** `internal/report/render.go`, `internal/report/render_test.go`, `internal/report/testdata/report.axi`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: zero `\x1b[` ANSI sequences and zero Markdown table/heading syntax in `renderAXI` output; no existing golden file's byte content changed
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | render.go:178 (`toonMustQuote`) | Omits three TOON must-quote conditions (number-like strings, `true`/`false`/`null` literals, leading-`-`) required by toon-format-reference.md:41. Bare-token emission → conforming TOON parser deserializes wrong type → breaks the round-trip contract. Failing inputs: `Fix:"42"`, `Problem:"null"`, `Fix:"- drop the call"`. | Extend `toonMustQuote` with number/reserved-token/leading-dash conditions. |
   | MEDIUM | render_test.go:456-501 | No AXI test feeds number-like / `true`/`false`/`null` / leading-`-` values, and none parses the payload back — the gap passes CI unnoticed. | Add reserved-token quoting + round-trip assertions. |
   | LOW | render.go:164-200 | `toonQuote`/`toonMustQuote` doc comments enumerate must-quote conditions but omit the missing ones — docs assert compliance the code lacks. | Update comments to match. |

   **Action taken:** HIGH found → fixed in 1.3 (below). This is the same
   reserved-token/number quoting AC 01-02 (task 1.5) scheduled; pulled forward per
   the gate rubric so the branch never carries a known round-trip defect. MEDIUM +
   LOW also resolved in 1.3. Verification/evidence columns + field-count invariant
   remain 1.4/1.5 scope.

### 1.3 [x] **[FormatAXI Render Dispatch - REFACTOR](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(report): clean up AXI render dispatch"`
   **Duration:** 1h

### 1.4 [x] **[AXI Schema TOON/findings-v1 Compatibility - RED](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Analyze [AC 01-02](plan/acceptance-criteria/01-02-axi-schema-toon-findings-v1-compatibility.md); identify testable units: tabular-array pipe-delimiter header shape, field-set superset of `atcr-findings/v1`, `Verification`/`EvidenceExec` block round-tripping, reserved-token quoting, header/row field-count invariant
   2. Write tests: table-driven field-mapping test against the 9-column `atcr-findings/v1` contract; fixture with `Verification` block, `EvidenceExec` block, and a `"true"`-looking string value; a defensive test asserting `len(row) == len(header fields)` for every sample
   3. Verify tests fail correctly
   **Files:** `internal/report/render_test.go` | **Duration:** 2h

### 1.5 [x] **[AXI Schema TOON/findings-v1 Compatibility - GREEN](plan/user-stories/01-axi-token-dense-output-mode.md)**
   Extend `renderAXI`'s header to declare the pipe delimiter (`findings[N|]{...}:`) converging with `atcr-findings/v1`'s grammar; encode `Verification` and `EvidenceExec` as additive nested/sub-object fields when present; quote reserved-token-like values (`"true"`, `"42"`) per TOON's must-quote rule. Record the axi.md Principle 2 (full-width fields retained) and Principle 4 (aggregates via header `N` + run metadata) decisions inline as a code comment. Cross-reference `docs/findings-format.md` with the AXI schema mapping. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `internal/report/render.go`, `docs/findings-format.md` | **Duration:** 4h

### 1.5.A [x] **[AXI Schema TOON/findings-v1 Compatibility - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-axi-token-dense-output-mode.md)**
   **Changed Files:** `internal/report/render.go`, `internal/report/render_test.go`, `docs/findings-format.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: AXI field set is a superset (no silently dropped field) of `atcr-findings/v1`'s 9-column contract; pipe delimiter used; axi.md Principle 2/4 decisions recorded inline
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):**
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | render.go axiRow (evidence cell) | `f.Disagreement` (JSON `disagreement`; v1 folds it into EVIDENCE at emit.go:368) is dropped by AXI — falsifies the "superset, never lossy vs JSON" claim in the render.go comment and docs/findings-format.md. Input: two reviewers at LOW vs MEDIUM → `Disagreement:"LOW vs MEDIUM"` survives in JSON/v1 text, not in AXI. | Add an additive `disagreement` column (omitempty-style) so AXI stays a JSON superset. |
   | MEDIUM | render.go axiRow (verification append) | `verification.*` omits `challenge_survived` (reconcile/verification.go:21) — the field the md renderer uses to relabel the verdict "Judge". An AXI consumer cannot tell a judge-upheld finding from an ordinary skeptic-confirmed one → lossy vs JSON. | Add `verification.challenge_survived` column. |

   **Action taken:** MEDIUM found. Rubric routes MEDIUM→TD, but both findings
   directly contradict the "superset — never a lossy subset of the JSON form"
   guarantee shipped in the render.go comment AND docs/findings-format.md, and AC
   01-02's Story-Specific DoD is literally "no field silently dropped". Deferring a
   false shipped guarantee is worse than the cheap fix, so both are RESOLVED in 1.6
   (add `disagreement` + `verification.challenge_survived` additive columns) rather
   than filed to TD.

### 1.6 [x] **[AXI Schema TOON/findings-v1 Compatibility - REFACTOR](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Fix CRITICAL/HIGH issues from 1.5.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(report): reconcile AXI schema with findings/v1"`
   **Duration:** 1h

### 1.7 [x] **Phase 1 - DoD Validation**
   Run DoD Verification Checklist: T3 (`go test ./internal/report/...`), coverage ≥80% for touched files, `golangci-lint run`, `go build ./...`, docs updated (`docs/findings-format.md` cross-reference).
   Report using the DoD Report Template.

### 1.8 [x] **Phase 1 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 1 (integration-level, not TDD cadence): `internal/report/render.go`, `internal/report/render_test.go`, `internal/report/testdata/report.axi`, `docs/findings-format.md`

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. The subagent has no memory of the phase's implementation — this is intentional, to avoid bias from having built the integration. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 1 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 1 (absolute paths): [LIST as above]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Downstream phases (2-5, which gate stdout writes and MCP behind this `FormatAXI` renderer) can consume outputs without rework?
       - REGRESSION: Earlier-phase behavior still intact (existing `md`/`json`/`checklist`/`sarif` golden files byte-unchanged)?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (run 2026-07-18):** No CRITICAL/HIGH. Verified end-to-end:
   `report.go:35 ValidFormat("axi")` → `report.Render` → `renderAXI`; renderer
   signature matches the other renderers; existing md/json/checklist/sarif goldens
   byte-unchanged (only `report.axi` new); `go test ./...` exit 0; MCP interim
   inclusion documented + reversible. Downstream Phases 2-5 can build on
   `renderAXI`'s shape without rework.
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | MEDIUM | cmd/atcr/report.go:21,25 | `--format` flag help + `Short` still say "md, json, checklist, or sarif" while the error message now lists `axi` — self-contradicting CLI. | TD-001 (fix in Phase 2 task 2.2 where report.go + the pinned main_test.go:342 Short guard are already in scope). |
   | LOW | render.go:222,229 | Additive-block columns mix bare int/bool and quoted-empty cells; round-trip untested vs a real TOON parser. | TD-002 (Phase 4). |
   | LOW | render.go:210 | `reviewers` cell ambiguous if a reviewer name contains a comma (not force-quoted under the pipe delimiter). | TD-003. |

   **Action taken:** No CRITICAL/HIGH → **Phase gate passed.** 1 MEDIUM + 2 LOW
   captured to `tech-debt-captured.md` (TD-001/002/003) per the gate rubric. The
   MEDIUM is deferred (not fixed inline) because its clean fix must edit the
   quality-report story's pinned `reportCmd.Short` guard and report.go, both
   already in Phase 2's scope — fixing now would cross-couple an unrelated story.

---

## Phase 2: Core Integration — Review/Resume Gating, MCP Exclusion, Exit-Code Reconciliation (2.5 days)

**Items:** Story 1 (AC 01-03, 01-04, 01-05), Story 2 (AC 02-01, 02-02, 02-03)

### 2.1 [x] **[`atcr review --axi` Output Gating - RED](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Analyze [AC 01-03](plan/acceptance-criteria/01-03-review-axi-mode-output-gating.md); identify testable units: axi-mode context injection via `PersistentPreRunE`, gating of the six `review.go` `cmd.OutOrStdout()` sites (433, 436, 551, 573, 591, 602), interrupt-path (`reportInterrupt`) gating
   2. Write tests: cobra command execution against a captured `bytes.Buffer` stdout for `--axi` and non-`--axi`; `--verify --debate` chained-stage assertions; all-agents-failed exit-1 path assertion; interrupt-path assertion
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/review_test.go` | **Duration:** 3h

### 2.2 [x] **[`atcr review --axi` Output Gating - GREEN](plan/user-stories/01-axi-token-dense-output-mode.md)**
   Register `--axi` on `atcr review`, thread the mode value through `PersistentPreRunE`'s context injection (mirroring the logger/telemetry client pattern) with an `axi.NewContext`/`axi.FromContext` accessor pair. Gate the six `cmd.OutOrStdout()` writes in `review.go` (433, 436, 551, 573, 591, 602) and the `reportInterrupt` path behind it, replacing them with the `FormatAXI` payload from Phase 1 when active. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/review.go`, `cmd/atcr/main.go` | **Duration:** 5h

### 2.2.A [x] **[`atcr review --axi` Output Gating - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-axi-token-dense-output-mode.md)**
   **Changed Files:** `cmd/atcr/review.go`, `cmd/atcr/main.go`, `cmd/atcr/review_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.2's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: all six listed write sites gated, not just some; interrupt path not an unguarded escape hatch
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** All six fresh-path write sites verified
   gated/unreachable under `--axi`; interrupt path (`reportInterrupt`) is stderr-only.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | cmd/atcr/resume.go:153,170,188,195,259 | `review --resume --axi` is silently accepted but the resume path is entirely ungated (never calls `writeReviewSummaryAXI`, leaks all human lines). | Gate every resume.go stdout write under `axiFromContext` and emit the axi payload on the result path. |
   | LOW | cmd/atcr/review.go:577 | Gate comment says the payload "already carries the counts", but `findings_total` is the raw pre-reconcile fanout metric, not the deduplicated reconciled count (reconciled/verify/debate counts are absent from the payload). | Reword to state the reconciled count is intentionally omitted (agent reads it via `atcr report --axi`). |
   | LOW | cmd/atcr/review.go:454-457 | An axi write error early-returns, bypassing the history/audit ledgers and the `--fail-on` gate; the non-axi branch can't fail, so this is asymmetric. | Acceptable (a broken stdout means the payload can't be delivered anyway, and render-fault→exit-1 satisfies AC 02-02 EC3); document the intent inline. |

   **Action taken:** No CRITICAL. The HIGH is `--resume` gating — precisely
   **element 2's scope** (AC 01-04, tasks 2.4–2.6), the immediate next work in this
   same phase; proceeding to element 2 IS the fix (it lands before the phase gate,
   never ships). Both LOWs resolved inline in 2.3 (accurate comments) — the write-
   error asymmetry is deliberately kept (satisfies AC 02-02 EC3) with an explaining
   comment.

### 2.3 [x] **[`atcr review --axi` Output Gating - REFACTOR](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): clean up review --axi gating"`
   **Duration:** 1h

### 2.4 [x] **[`atcr resume --axi` Context Propagation - RED](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Analyze [AC 01-04](plan/acceptance-criteria/01-04-resume-context-axi-mode-propagation.md); identify testable units: shared `writeReviewSummary` axi-branch, resume's five gated sites (153, 170, 188, 195, 259), `AllComplete()` short-circuit gating, mixed-mode (review without axi / resume with axi) independence
   2. Write tests: cobra command execution for `resume`, asserting identical payload shape to `review --axi`; `AllComplete()` branch assertion; empty-roster usage-error unaffected-by-axi assertion
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/resume_test.go` | **Duration:** 2h

### 2.5 [x] **[`atcr resume --axi` Context Propagation - GREEN](plan/user-stories/01-axi-token-dense-output-mode.md)**
   Add the axi-mode branch once inside shared `writeReviewSummary` (or consistently at both call sites) so `review.go:436` and `resume.go:195` behave identically; gate `resume.go`'s remaining sites (153, 170, 188, 259) via the same context accessor from 2.2 — no second flag parse. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/resume.go`, `cmd/atcr/review_summary.go` | **Duration:** 3h

### 2.5.A [x] **[`atcr resume --axi` Context Propagation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-axi-token-dense-output-mode.md)**
   **Changed Files:** `cmd/atcr/resume.go`, `cmd/atcr/review_summary.go`, `cmd/atcr/resume_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.5's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: `AllComplete()` branch (line 153) is gated; `review --axi`/`resume --axi` payload shapes are byte-identical for equivalent data
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** All resume.go stdout writes verified
   gated (AllComplete announce :158, resuming :177, outcome :208, summary :210,
   reconciled :278 in shared `resumeReconcile`); payload routing confirmed shared
   (review.go:461 and resume.go:204 both call `writeReviewSummaryAXI`) → byte-
   identical shape; flag parsed once, context value survives `correlateAndRedact`.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | cmd/atcr/resume.go:156-170 | AllComplete `resume --axi` gates its human writes but emits NO run-summary payload (no fanout result on this path) → empty stdout on exit 0. | Emit a payload from `prep.ID`/`dir` + `info.Completed` counts + reconciled total. |

   **Action taken:** No CRITICAL/HIGH → **Adversarial review passed.** The single
   MEDIUM is deferred to `tech-debt-captured.md` (TD-004) per the gate rubric: AC
   01-04 EC1 only requires GATING the AllComplete human line (satisfied), no shipped
   guarantee is contradicted ("byte-identical" is scoped "for equivalent data"), and
   a meaningful payload needs agent/reconciled counts plumbed onto a metrics-less
   path. Exit 0 still signals success; findings remain available via `report --axi`.

### 2.6 [x] **[`atcr resume --axi` Context Propagation - REFACTOR](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Fix CRITICAL/HIGH issues from 2.5.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): resume --axi parity cleanup"`
   **Duration:** 1h

### 2.7 [x] **[MCP `FormatAXI` Enum Decision - RED](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Analyze [AC 01-05](plan/acceptance-criteria/01-05-mcp-axi-format-enum-decision.md); identify testable units: `reportInputSchema` enum construction test, `descReport` description-text test, `handleReport` defense-in-depth behavior for `format: "axi"`
   2. Write tests per the sprint-design "excluded" decision ([Design Decisions (Resolved) #3](plan/sprint-design.md)): assert `"axi"` is NOT present in the schema enum, assert `handleReport` rejects `format: "axi"` consistent with the double-layer defense pattern
   3. Verify tests fail correctly
   **Files:** `internal/mcp/tools_test.go` | **Duration:** 1.5h

### 2.8 [x] **[MCP `FormatAXI` Enum Decision - GREEN](plan/user-stories/01-axi-token-dense-output-mode.md)**
   Filter `FormatAXI` out of the MCP-facing enum derivation in `reportInputSchema` (i.e. build the enum from `report.FormatList()` minus `FormatAXI`, not the raw list) with an inline comment explaining the exclusion rationale (Design Decision #3: AXI's value proposition is avoiding MCP's token overhead, so surfacing it through an MCP JSON-RPC envelope would be misleading). `descReport`'s generated text follows automatically. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `internal/mcp/tools.go` | **Duration:** 2h

### 2.8.A [x] **[MCP `FormatAXI` Enum Decision - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-axi-token-dense-output-mode.md)**
   **Changed Files:** `internal/mcp/tools.go`, `internal/mcp/tools_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.8's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.8`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the two files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: the exclusion decision is documented inline with a comment, not an accidental side effect; other existing formats' enum/description behavior is unchanged
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** Exclusion documented inline (tools.go
   helpers, handlers.go reject, render.go const); md/json/checklist/sarif behavior
   unchanged; CLI still advertises axi (`report.ValidFormat/FormatList/Formats`
   retain it); no other `internal/mcp` surface leaks the format list. `axi`/`"AXI"`/
   whitespace all rejected pre-dispatch and in-handler.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | internal/mcp/tools.go (ReportArgs.Format tag) | The struct tag hardcoded a second copy of the format list (already omitted axi, and overwritten by `reportInputSchema` so never served) — dead duplicate doc that could drift. | Reworded to a format-agnostic phrase ("see the format enum"). |

   **Action taken:** No CRITICAL/HIGH/MEDIUM → **Adversarial review passed.** The
   single LOW (dead duplicate format list in the struct tag) was cheaper to fix
   inline than to file — reworded the `ReportArgs.Format` jsonschema tag to be
   format-agnostic so there is one source of truth. Fixed in 2.9.

### 2.9 [x] **[MCP `FormatAXI` Enum Decision - REFACTOR](plan/user-stories/01-axi-token-dense-output-mode.md)**
   1. Fix CRITICAL/HIGH issues from 2.8.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(mcp): finalize FormatAXI enum exclusion"`
   **Duration:** 30min

### 2.10 [x] **[AXI Exit-Code Parity - RED](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   **Note:** Parity tests (`axi_exit_test.go`) pass on arrival — the exit-code
   contract is already mode-agnostic by construction: `exitCode()` (main.go:156) has
   no `--axi` branch, codes are resolved before/independently of output formatting,
   and the only new AXI error paths route through the existing helpers (auto-fix
   combo → `usageError` exit 2; render fault → unwrapped exitFailure 1). They pin
   the 0/1/2/3 contract against non-axi rather than driving new production code.
   1. Analyze [AC 02-01](plan/acceptance-criteria/02-01-axi-exit-code-parity.md); identify testable units: clean-run exit 0, gate-failure exit 1, usage-error exit 2, auth-error exit 3 — all under `--axi`, matching non-`--axi` behavior; partial-success-is-not-failure invariant
   2. Write table-driven tests extending `cmd/atcr/main_test.go`'s existing exit-code pattern to cover `--axi` invocations of `review`, `report`, `reconcile --fail-on`
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/main_test.go` | **Duration:** 2h

### 2.11 [x] **[AXI Exit-Code Parity - GREEN](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   **Confirm (no adjustment needed):** `exitCode()` (main.go:156) has no `--axi`
   branch — generic `errors.As` dispatch + `exitFailure` default. AXI error paths
   already route correctly: flag-combination error (`--axi`+`--auto-fix`) →
   `usageError` (exit 2, review.go:321); internal render fault →
   `fmt.Errorf("axi output rendering failed: %w", ...)` left unwrapped → exitFailure
   (1, review.go:462 / resume.go:210). Parity verified by `axi_exit_test.go`.
   Confirm/adjust `--axi` flag-parsing and rendering error paths to wrap through the existing `usageError()`/`authError()` helpers rather than falling through unwrapped; no new branch in `exitCode()` (`main.go:156`). T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/review.go`, `cmd/atcr/report.go` | **Duration:** 2.5h

### 2.11.A [x] **[AXI Exit-Code Parity - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   **Changed Files:** `cmd/atcr/review.go`, `cmd/atcr/report.go`, `cmd/atcr/main_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.11's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.11`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: no `--axi`-specific branch was added to `exitCode()`; all four codes verified under `--axi`
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** No CRITICAL/HIGH. `exitCode()` has no
   `--axi` branch; render faults unwrapped (exit 1); `--axi`+`--auto-fix` → exit 2;
   auth (exit 3) resolved before axiMode is read, no miswrap. Two LOWs:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW→(AC 02-02 EC3) | cmd/atcr/report.go:118-119 | `report --format axi` render fault is wrapped in `usageError` (exit 2), but AC 02-02 EC3 requires an internal AXI render fault via `atcr report --axi` to be exit 1. | Leave AXI render faults unwrapped in report.go (exit 1). |
   | LOW | cmd/atcr/axi_exit_test.go | No test asserts the NEW AXI render-fault source yields exit 1 (hard to reach — broken stdout / internal encoder bug). | Add a render-fault→exit-1 test via an injected failing writer. |

   **Action taken:** No CRITICAL/HIGH → **Adversarial review passed.** Both LOWs are
   directly AC 02-02's scope (error classification) — folded into **element 5**
   (tasks 2.13/2.14, the immediate next element) rather than TD: 2.14 fixes report.go
   to classify AXI render faults as exit 1, and 2.13 adds the render-fault→exit-1
   test via an injected failing writer.

### 2.12 [x] **[AXI Exit-Code Parity - REFACTOR](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   No code change — no CRITICAL/HIGH from 2.11.A; the two LOWs are folded into
   element 5 (AC 02-02) which owns AXI error classification. Parity confirmed green.
   1. Fix CRITICAL/HIGH issues from 2.11.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): confirm axi exit-code parity"`
   **Duration:** 30min

### 2.13 [x] **[New AXI Error Classification - RED](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   1. Analyze [AC 02-02](plan/acceptance-criteria/02-02-new-axi-error-classification.md); identify testable units: unsupported `--axi` flag-combination → exit 2, internal AXI rendering fault → exit 1, malformed `ATCR_AXI_MAX_LINES` → NOT an error (fail-open, owned by 3.7-3.9)
   2. Write table-driven tests enumerating every new AXI-introduced error source and its expected classification
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/main_test.go` | **Duration:** 1.5h

### 2.14 [x] **[New AXI Error Classification - GREEN](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   Wrap `--axi` flag-combination validation via `usageError()` (existing mutually-exclusive-flag pattern); leave internal AXI rendering faults unwrapped so they default to `exitFailure`. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/review.go` | **Duration:** 2h

### 2.14.A [x] **[New AXI Error Classification - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   **Changed Files:** `cmd/atcr/review.go`, `cmd/atcr/main_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.14's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.14`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the two files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: no new exit code introduced; `ATCR_AXI_MAX_LINES` misconfiguration never surfaces as exit 1 or 2
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** No CRITICAL/HIGH. Every AXI error source
   enumerated + correctly classified (auto-fix combo→2, render faults→1 across
   review/resume/report, `--disagreements --format axi`→2); no new exit code;
   `ATCR_AXI_MAX_LINES` confirmed genuinely absent (Phase 3).
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | cmd/atcr/review.go:200-209 vs guard | `--axi`+`--auto-fix` guard lived only in the fresh path; `--resume` short-circuits before it, so `review --resume --axi --auto-fix` silently dropped --auto-fix (exit 0), contradicting the shipped mutual-exclusion contract. | Hoist the guard above the --resume dispatch (axi-scoped). |

   **Action taken:** No CRITICAL/HIGH. The MEDIUM contradicts the just-shipped
   "--axi + --auto-fix mutually exclusive → exit 2" contract the code + test assert
   (a false shipped guarantee on the resume variant), so — per the 1.5.A precedent —
   **RESOLVED in 2.15** rather than deferred: hoisted the guard above the --resume
   dispatch (axi-scoped, so non-axi `--resume --auto-fix` keeps prior behavior),
   added `TestReviewCmd_AXIAutoFixResumeIsUsageError`.

### 2.15 [x] **[New AXI Error Classification - REFACTOR](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   1. Fix CRITICAL/HIGH issues from 2.14.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): axi error classification cleanup"`
   **Duration:** 30min

### 2.16 [x] **[Document Exit-Code Reconciliation](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   **Mode:** Pragmatic (documentation, no automated tests) | **AC:** [02-03](plan/acceptance-criteria/02-03-document-exit-code-reconciliation.md)
   1. Draft: extend the exit-code comment block in `cmd/atcr/main.go` (lines ~122-130) stating `--axi` reuses the 0/1/2/3 contract unchanged and that the epic's original `2`=internal-error proposal was considered and rejected, with a cross-reference to `documentation/exit-code-cli-mcp-precedent.md` and `atcr verify`
   2. Write: update `docs/ci-integration.md`'s exit-semantics section with the same statement, plus the Story-2 structured-error stream decision (stderr, not stdout, per axi.md Principle 6 reconciliation)
   3. COMMIT: `git commit -m "docs(cli): document AXI exit-code reconciliation"`
   **Files:** `cmd/atcr/main.go`, `docs/ci-integration.md` | **Duration:** 1.5h

### 2.16.A [x] **[Document Exit-Code Reconciliation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   **Changed Files:** `cmd/atcr/main.go`, `docs/ci-integration.md`

   **Spawn a fresh subagent** via the Agent tool. No memory of 2.16's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.16`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the two files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: both locations are mutually consistent (no drift); neither restates the epic's rejected 2=internal-error scheme; the stdout-vs-stderr structured-error decision is explicit
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** **No discrepancies.** Both locations
   mutually consistent; neither restates the rejected 2=internal-error scheme as
   adopted; stdout-vs-stderr decision explicit (ci-integration.md); every classified
   exit code verified against real code (auto-fix combo→2, render faults→1 across
   review/resume/report, no --axi branch in exitCode, diagnostics on ErrOrStderr).
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | None | — | No discrepancies found. | None required. |

   **Action taken:** **Adversarial review passed** — no findings. Proceed.

### 2.17 [x] **[Document Exit-Code Reconciliation - REFACTOR](plan/user-stories/02-reconcile-and-document-axi-exit-code-contract.md)**
   Proofread both locations — consistent, no drift (confirmed by 2.16.A). No change.
   1. Fix CRITICAL/HIGH issues from 2.16.A (if any)
   2. Proofread both locations for consistency, COMMIT: `git commit -m "refactor(docs): tighten exit-code reconciliation notes"`
   **Duration:** 20min

### 2.18 [x] **Phase 2 - DoD Validation**
   Run DoD Verification Checklist: T3 (`go test ./cmd/atcr/... ./internal/mcp/...`), coverage ≥80% for touched files, `golangci-lint run`, `go build ./...`, docs updated (`docs/ci-integration.md`).
   Report using the DoD Report Template.

### 2.19 [x] **Phase 2 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 2: `cmd/atcr/review.go`, `cmd/atcr/resume.go`, `cmd/atcr/review_summary.go`, `cmd/atcr/main.go`, `cmd/atcr/report.go`, `internal/mcp/tools.go`, `docs/ci-integration.md`, plus their `_test.go` files

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 2 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 2 (absolute paths): [LIST as above]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: New config keys documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced (review.go/resume.go context-mode reads, MCP enum exclusion)?
       - PHASE-EXIT CONTRACT: Downstream phases (3-4, which wrap this stdout path in pagination and add the escape-sequence pinning test) can consume outputs without rework?
       - REGRESSION: Earlier-phase (Phase 1 renderer) and non-`--axi` `review`/`resume` behavior still intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (run 2026-07-18):** No CRITICAL/HIGH. Verified: `--axi` stdout is
   a single choke point (`writeReviewSummaryAXI` the only stdout write under axi in
   both review.go:462 & resume.go:209; all human lines gated; auto-fix unreachable;
   interrupt/gate paths stderr-only) — Phase 3 pagination can wrap the findings
   renderer (`renderAXI`/`report --axi`), not this single-row summary. MCP exclusion
   enforced at all 3 layers. Exit contract 0/1/2/3 untouched. Non-axi byte-identical.
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | LOW | internal/report/render.go RenderReviewSummaryAXI | Comment claimed the renderAXI width invariant but did not enforce it (header/row independent). | **Fixed inline** — added `len(row) != len(header)` guard so a future column edit fails deterministically. |
   | LOW | cmd/atcr/resume.go AllComplete | `resume --axi` AllComplete emits empty stdout. | Already **TD-004** (known deferral). |
   | LOW (info) | internal/report/render.go | Two AXI renderers (`RenderReviewSummaryAXI` run-summary vs `renderAXI` findings). | Informational — flagged for Phase 3: pagination wraps `renderAXI`/`report --axi`, not the single-row summary. |

   **Action taken:** No CRITICAL/HIGH → **Phase gate passed.** One LOW (unenforced
   width-invariant comment) fixed inline (cheap correctness, makes a shipped comment
   true); the other two are already-tracked (TD-004) / informational (Phase 3 note).
   **Duration:** 30 min

---

## Phase 3: Pagination & Truncation Guarantees (2 days)

**Items:** Story 3 (AC 03-01, 03-02, 03-03, 03-04)

### 3.1 [x] **[Default Line Cap Deterministic Truncation - RED](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   1. Analyze [AC 03-01](plan/acceptance-criteria/03-01-default-line-cap-deterministic-truncation.md); identify testable units: under-cap pass-through, over-cap row-boundary truncation, exactly-at-cap (not truncated), one-over-cap (truncated), reproducibility across runs, zero-payload no-op
   2. Write table-driven tests over synthetic payloads (0, 120, exactly 500, 501, 1200 lines)
   3. Verify tests fail correctly (`internal/report/pagination.go` does not yet exist)
   **Files:** `internal/report/pagination_test.go` (new) | **Duration:** 2.5h

### 3.2 [x] **[Default Line Cap Deterministic Truncation - GREEN](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   Create `internal/report/pagination.go` implementing the line-cap wrapping post-processor: default 500-line cap, deterministic cut point at row index = cap, no re-parsing/backtracking. Wire it into `Render`'s `FormatAXI` dispatch path. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `internal/report/pagination.go` (new), `internal/report/render.go` | **Duration:** 4h

### 3.2.A [x] **[Default Line Cap Deterministic Truncation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   **Changed Files:** `internal/report/pagination.go`, `internal/report/render.go`, `internal/report/pagination_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 3.2's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops? (must be O(n) single pass)
       - Also verify: truncation never returns an error/non-zero exit; cut point always on a row boundary
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** No CRITICAL. One MEDIUM + three LOW:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | pagination.go:56 | Negative `maxLines` panics (`lines[:maxLines]` slice-bounds), contradicting the docstring's "never returns an error" contract + AC 03-01 Error Scenario 1. Only the upstream env fail-open masks it; `PaginateAXI` is exported/unguarded. | Clamp `maxLines < 1` → `AXIMaxLinesDefault` at entry. |
   | LOW | pagination.go:51-56 | `maxLines == 0` drops the header line (true-total `N`), violating AC 03-02's "header is line 1, never dropped". | Same clamp fix. |
   | LOW | pagination_test.go:107 | No test exercises `maxLines <= 0`. | Add zero/negative cap cases. |
   | LOW | pagination_test.go | `RenderAXIPaginated` (truncated-flag emitter) has no test. | Add under/over-cap `truncated:` assertions. |

   **Action taken:** No CRITICAL/HIGH. The MEDIUM contradicts the just-shipped
   "never returns an error … enforced by the signature itself" docstring guarantee
   (a false shipped guarantee), so — per the 1.5.A / 2.14.A precedent — it is
   **RESOLVED in 3.3** (clamp non-positive `maxLines` to the default, matching the
   AC 03-03 fail-open posture) rather than deferred. All three LOWs also fixed in
   3.3 (they share the same clamp fix + the missing tests). The `RenderAXIPaginated`
   truncated-flag test is added here in 3.3 (its full AC 03-02 contract lands in 3.4/3.5).

### 3.3 [x] **[Default Line Cap Deterministic Truncation - REFACTOR](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(report): clean up pagination cap"`
   **Duration:** 45min

### 3.4 [x] **[`truncated` Flag with True Total Count - RED](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   **Note:** The AC 03-02 contract tests (`TestAXIPayload_*`) pass on arrival — the
   `truncated: <bool>` flag and header-`N` preservation were necessarily established
   when the shared `RenderAXIPaginated` emission entry point was built in 3.2 (a
   shared emitter cannot produce content without committing to its output contract:
   the flag is the AC 03-01 Scenario 2 "closing structure", and the header N is
   line 1, preserved by `PaginateAXI`). Per this sprint's 2.10/2.11 precedent, they
   PIN the contract (truncated present in every payload; header N = true total;
   N > emitted rows when truncated — Risk 3 guard; boundary + zero cases; `truncated`
   field name matches `internal/fanout/status.go`) rather than drive new production code.
   1. Analyze [AC 03-02](plan/acceptance-criteria/03-02-truncated-flag-and-true-total-count.md); identify testable units: `truncated: false` under cap, `truncated: true` + true total `N` over cap, header `N` != emitted row count when truncated, boundary case, zero-findings case
   2. Write tests asserting the header `N` is computed pre-truncation, and `truncated` field is present in every payload
   3. Verify tests fail correctly
   **Files:** `internal/report/pagination_test.go` | **Duration:** 1.5h

### 3.5 [x] **[`truncated` Flag with True Total Count - GREEN](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   **Confirm (no new production code):** `truncated bool` is computed by
   `PaginateAXI` and emitted as `truncated: %t` by `RenderAXIPaginated`
   (pagination.go), using the exact `truncated` field name/true-false semantics of
   `internal/fanout/status.go`'s `Truncated bool json:"truncated"`. The TOON header
   `N` is computed from the pre-truncation element count in `renderAXI` (Phase 1)
   and preserved by `PaginateAXI` (header is line 1, never dropped) — no render.go
   change needed. AC 03-02 tests committed at this GREEN.
   Compute and emit `truncated bool` (reusing `internal/fanout/status.go`'s exact field name/semantics) alongside the cap step; compute the TOON array header's `N` from the pre-truncation element count, independent of emitted row count. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `internal/report/pagination.go`, `internal/report/render.go` | **Duration:** 2h

### 3.5.A [x] **[`truncated` Flag with True Total Count - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   **Changed Files:** `internal/report/pagination.go`, `internal/report/render.go`, `internal/report/pagination_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 3.5's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: header `N` never silently clipped alongside rows; field name matches `internal/fanout/status.go`'s `Truncated bool` exactly
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** 2 CRITICAL (one root cause), 1 HIGH,
   1 MEDIUM, 2 LOW:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | CRITICAL | render.go:93-95 / report.go:118 | `report --axi` routes through base `Render(FormatAXI)`, which caps rows but writes only `out`, discarding `truncated` → silent row-drop on >500 findings, header N > rows, no signal. | Route the CLI findings path through `RenderAXIPaginated`; base `Render` must not silently cap. |
   | CRITICAL | pagination.go:82 | `RenderAXIPaginated` has ZERO production callers — the AC 03-02 mechanism is only reached by tests. | Wire it into `report --axi` (and any review findings path). |
   | HIGH | render.go:86 / pagination.go:12 | `ATCR_AXI_MAX_LINES` (AC 03-03) referenced only in comments; no env resolver exists; both dispatch sites hardcode 500. | Implement the env resolver in cmd/atcr and thread it in. |
   | MEDIUM | pagination.go:62 | A truncated payload is intentionally non-length-round-trippable (header N > physical rows + out-of-band `truncated:` line); a strict TOON length-validator rejects the short array. | Document that consumers must read `truncated` + header N; do not length-validate. |
   | LOW | pagination.go:53 | `total` return derived from line count, discarded by both callers, could diverge from header N if fed a mismatched payload. | Document it equals the pre-truncation row count == header N by construction. |
   | LOW | pagination.go:91 | Shares the `truncated` field NAME with status.go but semantics differ (output row-cap vs byte-budget input truncation); AXI emits a bare TOON bool vs status.go's JSON quoted key. | Note the distinct meaning in the docstring. |

   **Action taken:** The two CRITICALs and the HIGH are the **explicit scope of the
   remaining Phase 3 elements** — wiring both commands through the shared wrapper is
   element 4 (tasks 3.10–3.12, AC 03-04) and requires element 3's `axiMaxLinesFromEnv`
   (task 3.8) first. Per the 2.2.A precedent ("proceeding to the next element IS the
   fix; it lands before the phase gate, never ships"), they are resolved by
   completing elements 3–4 in this same phase, verified at the 3.14 gate — NOT
   deferred to TD. **However**, the silent-truncation the review exposed in base
   `Render(FormatAXI)` — introduced by my 3.2 in-`Render` cap — is a genuine defect
   that must not exist even interim: **fixed in 3.6** by reverting base `Render`/
   `renderAXI` to the pure schema encoder (never silently caps; golden/Phase 1
   stable) and confining pagination + the flag to `RenderAXIPaginated` (the CLI
   dispatch step wired in element 4). This is the correct reading of task 3.2's
   "wire into the FormatAXI dispatch path". The MEDIUM + both LOWs are cheap inline
   doc fixes in 3.6 (the non-round-trip property is mandated by AC 03-02 EC1;
   overlaps the already-captured TD-002).

### 3.6 [x] **[`truncated` Flag with True Total Count - REFACTOR](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(report): truncated flag cleanup"`
   **Duration:** 30min

### 3.7 [x] **[`ATCR_AXI_MAX_LINES` Env Override - RED](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   1. Analyze [AC 03-03](plan/acceptance-criteria/03-03-axi-max-lines-env-override.md); identify testable units: unset→default, valid override, blank→fail-open+warn, non-numeric→fail-open+warn, zero/negative→fail-open+warn, read-once-per-run, exactly-one-warning-per-run
   2. Write tests using `t.Setenv` for isolated env-var manipulation, capturing stderr to count warning lines exactly
   3. Verify tests fail correctly (`axiMaxLinesFromEnv` does not yet exist)
   **Files:** `cmd/atcr/main_test.go` | **Duration:** 2h

### 3.8 [x] **[`ATCR_AXI_MAX_LINES` Env Override - GREEN](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   Add `axiMaxLinesFromEnv() int` mirroring `logLevelFromEnv`/`telemetryEnabledFromEnv`'s exact fail-open structure: read `os.Getenv("ATCR_AXI_MAX_LINES")` once, `strconv.Atoi`, warn-and-default (500) on parse failure/blank/non-positive, exactly one stderr warning. Thread the resolved value into `internal/report/pagination.go` as the cap parameter. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/main.go`, `internal/report/pagination.go` | **Duration:** 3h

### 3.8.A [x] **[`ATCR_AXI_MAX_LINES` Env Override - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   **Changed Files:** `cmd/atcr/main.go`, `internal/report/pagination.go`, `cmd/atcr/main_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 3.8's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.8`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the three files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: never a fatal error/panic/non-zero exit from a malformed env value; warning emitted exactly once per run, not per call site
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** No CRITICAL. 1 HIGH, 1 MEDIUM, 1 LOW:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | HIGH | main.go:360 | `axiMaxLinesFromEnv`/`RenderAXIPaginated`/`PaginateAXI` have no production callers; `report --axi` still emits via base `Render` → the env override + truncation are unwired. | Wire the shared step into `report --axi` (and review/resume findings path). "If wiring is a deferred Phase-3 step, mark pending." |
   | MEDIUM | main.go:370 | No once-per-run guard; "exactly one warning per run" rests on caller-once discipline; the `...FromEnv()` name invites per-emission-point calls. | Resolve once (context/gate) and thread the int, or add a `sync.Once` resolver. |
   | LOW | main.go:365 | `strconv.Atoi` is platform-`int`-width: a value in (2³¹,2³²) is accepted on 64-bit but warns on 32-bit; the `999999999` test hides this. | Use `ParseInt(...,64)`+clamp, or add an arch-aware test above 2³¹. |

   **Action taken:** No CRITICAL/HIGH requiring an inline fix *within element 3*.
   The HIGH (unwired) is **element 4's explicit scope** (tasks 3.10–3.12, AC 03-04) —
   the immediate next element, landing before the 3.14 phase gate, never shipping
   (2.2.A precedent; the reviewer itself allows a deferred wiring step). The MEDIUM
   is deliberately the telemetryEnabledFromEnv shape the AC 03-03 mandated
   ("mirroring … exact fail-open structure") — that precedent has no internal
   once-guard and relies on caller-once via `telemetryGate`; a `sync.Once`+cache
   would break `t.Setenv` testability and diverge from the mandated precedent, so
   once-per-run is honored by **element 4's caller-once wiring** (resolve once,
   thread the int) — the docstring already states this contract. The LOW is
   **accepted**: AC 03-03 explicitly names `strconv.Atoi` and its own example value
   (999999999) is < 2³¹ (arch-safe); a line cap above 2³¹ is nonsensical and
   fails-open harmlessly on 32-bit. No 3.9 code change (confirm-only, 2.12 precedent).

### 3.9 [x] **[`ATCR_AXI_MAX_LINES` Env Override - REFACTOR](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   No code change — no CRITICAL/HIGH from 3.8.A within element 3's scope. The HIGH
   (unwired) is element 4 (3.10–3.12); the MEDIUM (once-per-run) is honored by
   element 4's caller-once wiring per the mandated telemetryEnabledFromEnv precedent;
   the LOW (Atoi arch-width) is accepted (AC-named function; example value arch-safe).
   `axiMaxLinesFromEnv` confirmed green.
   1. Fix CRITICAL/HIGH issues from 3.8.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): axiMaxLinesFromEnv cleanup"`
   **Duration:** 30min

### 3.10 [x] **[Shared Truncation Wrapper Across Commands - RED](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   1. Analyze [AC 03-04](plan/acceptance-criteria/03-04-shared-truncation-wrapper-across-commands.md); identify testable units: `review --axi` and `report --axi` truncate identically for the same payload, env override applies identically to both, live/streaming path capped same as batch path
   2. Write integration tests exercising both `atcr review --axi` and `atcr report --axi` command entry points against an identical fixture exceeding the cap, asserting identical `truncated`/`N`/line-count
   3. Verify tests fail correctly
   **Files:** `cmd/atcr/review_test.go`, `cmd/atcr/report_test.go` | **Duration:** 2h

### 3.11 [x] **[Shared Truncation Wrapper Across Commands - GREEN](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   Wire `atcr review --axi`'s live-output path (Phase 2's gated writes) through the same `internal/report/pagination.go` step `atcr report --axi` uses — no duplicated line-counting/truncation logic in either `cmd/atcr/review.go` or `cmd/atcr/report.go`. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/review.go`, `cmd/atcr/report.go` | **Duration:** 3h

### 3.11.A [x] **[Shared Truncation Wrapper Across Commands - ADVERSARIAL REVIEW (subagent)](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   **Changed Files:** `cmd/atcr/review.go`, `cmd/atcr/report.go`, `cmd/atcr/review_test.go`, `cmd/atcr/report_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 3.11's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.11`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the four files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: no parallel truncation logic exists outside `internal/report/pagination.go`; `review --axi` and `report --axi` are provably identical for equivalent input
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** No CRITICAL/HIGH. The shared-truncation
   architecture verified correct: exactly one truncation implementation
   (`PaginateAXI`), `report --axi` routes through `RenderAXIPaginated`,
   `review`/`resume --axi` emit only the single-row summary (no truncation), exit
   codes preserved (render fault→1 unwrapped, `--disagreements --format axi`→2
   usageError), non-AXI formats unchanged, one-line-per-row invariant holds
   (`toonEscape` escapes `\n`). Two LOWs:
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | LOW | review.go:462 | `--axi` summary-write-error early-return skips bookkeeping (asymmetric with the non-axi branch). | Pre-existing Phase 2 decision — already reviewed/accepted/documented in **2.2.A** (deliberate, satisfies AC 02-02 EC3). Not introduced here; no Phase 3 action. |
   | LOW | pagination.go:105 | `RenderAXIPaginated` discards `PaginateAXI`'s `total` (dead for the sole prod caller). | **Fixed inline in 3.12**: `total` is design.md's specified return + used by tests; add a one-line note that the caller intentionally discards it (header N is authoritative). |

   **Action taken:** No CRITICAL/HIGH → **Adversarial review passed.** LOW #1 is an
   already-accepted Phase 2 decision (2.2.A) outside Phase 3 scope — no action. LOW #2
   is resolved inline in 3.12 (a one-line clarifying comment; the `total` return is
   design.md-specified and test-used, so it is not removed). Neither warrants a TD
   entry.

### 3.12 [x] **[Shared Truncation Wrapper Across Commands - REFACTOR](plan/user-stories/03-axi-pagination-and-truncation-guarantees.md)**
   1. Fix CRITICAL/HIGH issues from 3.11.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): unify axi truncation call sites"`
   **Duration:** 30min

### 3.13 [x] **Phase 3 - DoD Validation**
   **Result (2026-07-18):** Tests T3 all passing (0 FAIL); coverage internal/report
   93.8%, cmd/atcr 86.5% (both ≥80%); `golangci-lint run` 0 issues + `go vet` clean;
   `go build ./...` succeeds; docs N/A this phase (Story 5 owns pagination docs).
   `Story-3 DoD Complete — Auto: 5/5 | Story-Specific: 16/16 | Manual Review: [ ]`.
   Run DoD Verification Checklist: T3 (`go test ./internal/report/... ./cmd/atcr/...`), coverage ≥80% for touched files, `golangci-lint run`, `go build ./...`, docs (none required this phase — Story 5 covers pagination docs).
   Report using the DoD Report Template.

### 3.14 [x] **Phase 3 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 3: `internal/report/pagination.go` (new), `internal/report/pagination_test.go` (new), `internal/report/render.go`, `cmd/atcr/main.go`, `cmd/atcr/review.go`, `cmd/atcr/report.go`, plus their `_test.go` files

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 3 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 3 (absolute paths): [LIST as above]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: `ATCR_AXI_MAX_LINES` documented, defaulted, back-compat?
       - INTEGRATION: Cross-module calls correct (both commands route through one shared wrapper), no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Phase 4 (stdout/escape guarantees) and Phase 5 (docs naming the actual cap/env-var/field) can consume this without rework?
       - REGRESSION: Phases 1-2 behavior still intact; non-truncated payloads unaffected?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (run 2026-07-18):** **PASS** — no CRITICAL/HIGH. Verified:
   `PaginateAXI` never errors (signature + non-positive clamp); `RenderAXIPaginated`
   propagates only `renderAXI`'s error; `report --axi` classifies a render fault as
   exit 1 (unwrapped) — AC 02-02 preserved. `ATCR_AXI_MAX_LINES` defaulted to 500,
   back-compat (unset→default silent), fail-open + one stderr warning, read once
   (main.go:360). Exactly ONE truncation implementation
   (internal/report/pagination.go); sole prod caller report.go:123; `review --axi`
   uses `RenderReviewSummaryAXI` (run-summary, out of scope) with no second
   truncation copy — AC 03-04 satisfied; no duplicate `500`/truncation logic in
   cmd/atcr. `report.axi` + md/json/checklist/sarif goldens byte-unchanged
   (`5dc0b055..HEAD` touches no testdata); base `Render(FormatAXI)`/`renderAXI` stays
   the pure uncapped encoder; non-`--axi` paths untouched. Phase 4 (escape/stdout)
   and Phase 5 (docs) can consume without rework.
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | LOW | pagination.go:83-89 | Truncated payload is intentionally not TOON length-round-trippable (header N > physical rows + out-of-band `truncated:` key) — a strict length-validating parser rejects it. AC-mandated (03-02 EC1), pinned by a test; not a code defect. | **TD-005** — Phase 5 docs must warn consumers to read `truncated` + header N and not length-validate the array. |

   **Action taken:** No CRITICAL/HIGH → **Phase gate passed.** The single LOW is a
   documentation directive for Phase 5 (behavior is AC-mandated and correct) —
   captured to `tech-debt-captured.md` as **TD-005** (overlaps TD-002) per the gate
   rubric, so `/execute-code-review` pre-seeds it and Phase 5's agentic-consumption
   guide addresses it.
   **Duration:** 25 min

---

## Phase 4: Stderr Isolation & Escape-Sequence Guarantee (2 days)

**Items:** Story 4 (AC 04-01, 04-02, 04-03)

### 4.1 [x] **[Gate Human-Oriented Stdout Writes - RED](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   1. Analyze [AC 04-01](plan/acceptance-criteria/04-01-review-resume-stdout-gating.md); identify testable units: confirm the six `review.go` sites and five `resume.go` sites (from Phase 2) contain zero human-text strings under `--axi`, `--verify`/`--debate`/`--auto-fix` chained coverage, all-agents-failed/reconcile-failure error paths
   2. Write captured-stdout assertions for `"agents succeeded"`, `"Total elapsed"`, `"Agents:"`, `"API calls:"`, `"Findings:"`, `"reconciled"`, `"resuming review"` absence under `--axi`, for both fresh-review and resume paths
   3. Verify tests fail correctly (or confirm they already pass if Phase 2 fully covered gating — treat any gap found as the RED signal)
   **Files:** `cmd/atcr/review_test.go`, `cmd/atcr/resume_test.go` | **Duration:** 2.5h

### 4.2 [x] **[Gate Human-Oriented Stdout Writes - GREEN](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   **Confirm (no adjustment needed):** 4.1's gap-check found NO gap — Phase 2 already
   gated all 11 write sites. Verified by code inspection + passing gap-check tests:
   review.go one-line outcome (:466)/summary (:468)/reconcile (:590)/verify (:614)/
   debate (:634) all under `!axiMode`; `orchestrateAutoFix` (:646) is unreachable
   under `--axi` (auto-fix guard at review.go:204 → usageError); `reportInterrupt`
   (run_helpers.go:40) writes only `cmd.ErrOrStderr()`. resume.go announce (:163)/
   resuming (:182)/outcome (:213)/summary (:215) and shared `resumeReconcile` (:283)
   all gated; resume's interrupt path is the same stderr-only `reportInterrupt`. Both
   `writeReviewSummary` callers gated identically (in the `else` of the shared axi
   branch). No production change; test additions only.
   Close any gap 4.1 found in Phase 2's gating (e.g. `orchestrateAutoFix`'s output writer at `review.go:602`, or `reportInterrupt`'s stdout path) so every human-oriented write in `review.go`/`resume.go` is confirmed gated, including both `writeReviewSummary` callers consistently. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/review.go`, `cmd/atcr/resume.go` | **Duration:** 3h

### 4.2.A [x] **[Gate Human-Oriented Stdout Writes - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   **Changed Files:** `cmd/atcr/review.go`, `cmd/atcr/resume.go`, `cmd/atcr/review_test.go`, `cmd/atcr/resume_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 4.2's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the four files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: every listed write site in both files is gated with no missed call site; error-return paths still gated before the error is returned
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** All 11 write sites verified gated or
   unreachable under `--axi`; `reportInterrupt`/`interruptedBeforeFanout` are
   stderr-only and short-circuit before the summary block; error-return paths gate
   before returning; the enumerated human-string slices are complete. No CRITICAL/HIGH.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | review_axi_test.go:181 | The `--axi --verify --debate` gate test's doc-comment references a companion `TestReviewCmd_NonAXIChainedLinesPresent` that did not yet exist, so its `verified `/`debated `/`reconciled` absence assertions were potentially tautological (would pass even if verify/debate emitted nothing). The resume path is properly paired; the review chained path was not. | Add the referenced non-axi companion test asserting those lines are PRESENT without `--axi`. |
   | LOW | resume.go:156-164 | `resume --axi` AllComplete path emits no run-summary payload (byte-empty stdout on exit 0). | Already **TD-004** (known deferral). |

   **Action taken:** No CRITICAL/HIGH → **Adversarial review passed.** The MEDIUM
   makes a shipped code-comment false (references a non-existent test) and leaves the
   review chained-line gate assertions unpaired — per the 1.5.A/2.14.A precedent a
   false shipped claim is RESOLVED, not deferred: `TestReviewCmd_NonAXIChainedLinesPresent`
   added in 4.3 (pulled forward from 4.7's AC 04-03 scope, which owns the full non-axi
   regression set). The LOW is already tracked as **TD-004** (empty AllComplete
   payload needs agent-count plumbing; out of Phase 4 scope) — no new action.

### 4.3 [x] **[Gate Human-Oriented Stdout Writes - REFACTOR](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   1. Fix CRITICAL/HIGH issues from 4.2.A (if any)
   2. Improve code and tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): finalize axi stdout gating"`
   **Duration:** 30min

### 4.4 [x] **[Escape-Sequence Pinning Test - RED](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   1. Analyze [AC 04-02](plan/acceptance-criteria/04-02-escape-sequence-pinning-test.md); identify testable units: zero `\x1b[`/`\x1b]` in captured `--axi` stdout (review + resume), positive-control detection of `osc8()`'s exact byte pattern, crafted-input finding-field escape-injection case
   2. Write the pinning test in the style of `TestDriftLine_StripsControlChars`/`TestRenderPersonaSearch_StripsControlChars`, including the `osc8()` fixture as a known-bad positive control
   3. Verify tests fail correctly (test doesn't yet exist)
   **Files:** `cmd/atcr/axi_escape_test.go` (new) | **Duration:** 2h

### 4.5 [x] **[Escape-Sequence Pinning Test - GREEN](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   **Confirm (no production change):** Implemented the escape-detection helper
   (`axiEscapePattern` regex over CSI `\x1b[` + OSC `\x1b]`, `findEscapeSequence`,
   `requireNoEscapeSequence` with a byte-offset diagnostic) in the new
   `cmd/atcr/axi_escape_test.go` and wired it against captured `--axi` stdout for both
   review and resume. All pass on arrival — the TOON payload is escape-free by
   construction (toonEscape drops raw control bytes), so this is a regression
   backstop, no production change (as the AC anticipated). The osc8() positive
   control proves the detector genuinely fails on a real escape. Element committed at
   4.6 (single adversarial-reviewed commit, mirroring element 1).
   Implement the escape-detection helper (regex over `\x1b\[`/`\x1b\]`) and wire the pinning test to run against captured `--axi` stdout for both review and resume paths. Since the structurally-escape-free TOON/JSON payload (Phase 1) is the primary guarantee, this test acts as a regression backstop — no production code change expected unless the pinning test surfaces a real gap. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/axi_escape_test.go` | **Duration:** 2h

### 4.5.A [x] **[Escape-Sequence Pinning Test - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   **Changed Files:** `cmd/atcr/axi_escape_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 4.5's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the file above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: regex covers both CSI (`\x1b[`) and OSC (`\x1b]`) variants, not just one; the `osc8()` positive control genuinely fails the detector when injected
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** Regex correct + non-tautological, osc8
   positive control matches at offset 0, byte-offset slice cannot panic. No CRITICAL/HIGH.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | axi_escape_test.go:29 | Detector matched only 7-bit ESC CSI (`\x1b[`)/OSC (`\x1b]`), but the file header claims "NO ANSI/OSC escape sequence" — misses 8-bit C1 introducers (`\x9b` CSI, `\x9d` OSC) and bare-ESC forms that `renderAXI`'s `toonEscape`→`unicode.IsControl` actually strips (0x80-0x9f). A regression bypassing toonEscape and leaking a raw `\x9b`/`\x9d` would pass green. | Broaden the pattern to any ESC + C1 range; add a `\x9b`/`\x9d` positive control. |
   | LOW | axi_escape_test.go:100-101 | Crafted-field "surviving text" asserts (`Contains "RED"/"alert"`) pass whether or not the ESC was stripped (the residue `[31mRED[0m` survives either way) → no signal about the strip. | Assert the residual-but-safe form (`Contains "[31mRED"`) to distinguish "ESC stripped, text kept". |
   | LOW | axi_escape_test.go:113-114 | Review escape scan coupled to `--fail-on high`/exit-1 + a live-mock finding; an unrelated mock change breaks the escape-pinning test. | Decouple: run plain `review --axi` (exit 0) and scan independent of the exit code. |

   **Action taken:** No CRITICAL/HIGH → **Adversarial review passed.** All three are
   cheap, strengthen the backstop, and keep every test green — the MEDIUM makes the
   test file's own header comment overclaim (per the 1.5.A/2.14.A resolve-not-defer
   precedent), so all THREE are **RESOLVED in 4.6**, not deferred: (1) broaden
   `axiEscapePattern` to any ESC + C1 range (0x80-0x9f), keeping CSI/OSC covered, and
   add a C1 (`\x9b`/`\x9d`) positive control; (2) assert `Contains "[31mRED"` proving
   the ESC byte specifically was stripped; (3) decouple the review scan to a plain
   `review --axi` (exit 0). Newlines/tabs (C0 whitespace the TOON payload legitimately
   uses as row separators) are NOT in the broadened set, so no false positive.

### 4.6 [x] **[Escape-Sequence Pinning Test - REFACTOR](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   1. Fix CRITICAL/HIGH issues from 4.5.A (if any)
   2. Improve tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): tighten escape-sequence pinning test"`
   **Duration:** 20min

### 4.7 [x] **[Non-AXI Regression Protection - RED](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   1. Analyze [AC 04-03](plan/acceptance-criteria/04-03-non-axi-regression-protection.md); identify testable units: byte-identical non-`--axi` output for `review`/`resume`, `AllComplete()` branch text unchanged, mixed-flag (`--verify --debate`, no `--axi`) full human output retained
   2. Write/extend explicit stdout-content assertions for every write site listed in AC 04-01, for the non-`--axi` case specifically (fills any pre-existing coverage gap)
   3. Verify tests fail correctly (only if a gap exists) or confirm they pass as a baseline snapshot
   **Files:** `cmd/atcr/review_test.go`, `cmd/atcr/resume_test.go` | **Duration:** 1.5h

### 4.8 [x] **[Non-AXI Regression Protection - GREEN](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   **Confirm (no adjustment needed):** 4.7's baseline-snapshot tests all pass on
   arrival — the non-`--axi` path is unchanged. The AC 04-01 gating only wraps the
   pre-existing writes in an `if !axiMode { ... }` / `else` guard whose false (human)
   branch executes the exact same `Fprintf`/`writeReviewSummary` calls as before; no
   production code alters non-axi output. Review side covered by
   `TestReviewCmd_NonAXIChainedLinesPresent` (every write-site string present under
   `--verify --debate`, no `--axi`); resume side by `TestResume_NonAXIAllHumanStringsPresent`
   + `TestResume_NonAXIAllCompletePresent`. Test additions only.
   Fix any gating logic (not tests) found to alter non-`--axi` output from 4.7; the non-`--axi`/default branch must execute the exact same write calls that existed before this story. T1 after each change, verify all pass (T2), COMMIT.
   **Files:** `cmd/atcr/review.go`, `cmd/atcr/resume.go` | **Duration:** 1.5h

### 4.8.A [x] **[Non-AXI Regression Protection - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   **Changed Files:** `cmd/atcr/review.go`, `cmd/atcr/resume.go`, `cmd/atcr/review_test.go`, `cmd/atcr/resume_test.go`

   **Spawn a fresh subagent** via the Agent tool. No memory of 4.8's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 4.8`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): the four files above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
       - Also verify: every pre-existing non-`--axi` test passes unmodified; no test was weakened/deleted to make this pass
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (run 2026-07-18):** No production non-axi write path changed;
   no pre-existing test weakened/deleted; the assertion sets are complete against the
   actual write sites; the new tests exercise the real default path. No CRITICAL/HIGH.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | MEDIUM | review_axi_test.go / resume_axi_test.go non-axi tests | The non-axi regression tests assert `Contains` on isolated fragments while comments imply "byte-for-byte / untouched"; presence does not verify relative ORDER or catch an inserted line, so a reorder/insert regression of the default path could pass. | Add a relative-ordering assertion over the fragments (not just presence) and align the comment wording to what is asserted. |
   | LOW | resume_axi_test.go:46-47 | `resumeHumanStdoutStrings` doc comment cites `resume.go:163` (the AllComplete announce), but that string is deliberately NOT in the slice (asserted separately by `TestResume_NonAXIAllCompletePresent`) — a misleading citation. | Drop `163` from the comment; note it is covered by the AllComplete tests. |

   **Action taken:** No CRITICAL/HIGH → **Adversarial review passed.** Both make a
   test comment overclaim vs what the code asserts — resolved in 4.9 (resolve-not-defer
   precedent): (1) added an `assertOrderedContains` helper pinning the fragments in
   their emitted order (catches reorder/inserted-line regressions), applied to the
   review + resume non-axi regression tests, and aligned comment wording to "in the
   same order and wording"; (2) corrected the `resumeHumanStdoutStrings` citation to
   drop the AllComplete line (:163), noting it is covered separately.

### 4.9 [x] **[Non-AXI Regression Protection - REFACTOR](plan/user-stories/04-axi-stderr-isolation-and-escape-sequence-guarantee.md)**
   1. Fix CRITICAL/HIGH issues from 4.8.A (if any)
   2. Improve tests (T1), validate all tests still pass (T3), COMMIT: `git commit -m "refactor(cmd): non-axi regression coverage cleanup"`
   **Duration:** 20min

### 4.10 [x] **Phase 4 - DoD Validation**
   Run DoD Verification Checklist: T3 (`go test ./cmd/atcr/...`), coverage ≥80% for touched files, `golangci-lint run`, `go build ./...`, docs (none required this phase).
   Report using the DoD Report Template.

   **Story-4 DoD Complete**
   - Auto: 5/5 — T3 `go test ./cmd/atcr/...` PASS; `go build ./...` OK; `go vet ./...` clean;
     `golangci-lint run ./cmd/atcr/...` 0 issues (fixed ST1018: C1 controls now ``/``
     escapes); coverage 86.9% (≥80% baseline).
   - Story-Specific: AC 04-01 (all 11 write sites gated, confirmed + tested; interrupt stderr-only),
     AC 04-02 (escape pinning test with osc8 + C1 positive controls, crafted-field strip, review/resume
     scans), AC 04-03 (non-axi baseline unchanged; ordered-fragment guard) — all met.
   - Docs: none required this phase (Phase 5 owns docs).
   - Manual Review: [ ] Code reviewed (→ /execute-code-review).

### 4.11 [x] **Phase 4 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 4: `cmd/atcr/review.go`, `cmd/atcr/resume.go`, `cmd/atcr/axi_escape_test.go` (new), plus other `_test.go` files touched

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Phase 4 gate review`
   - prompt: Self-contained brief including:
     - Files changed during Phase 4 (absolute paths): [LIST as above]
     - Checklist (pass verbatim, hostile integrator perspective):
       - CONTRACT EXIT: All phase-exit contracts honored (signatures, return shapes, error types)?
       - CONFIG SURFACE: No new config keys this phase — confirm none were introduced accidentally
       - INTEGRATION: Cross-module calls correct, no hidden coupling introduced?
       - PHASE-EXIT CONTRACT: Phase 5's documentation (stderr/stdout separation section) can accurately describe this guarantee?
       - REGRESSION: Phases 1-3 and pre-existing non-`--axi` behavior still fully intact?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Gate findings (run 2026-07-18):** Phase 4 changed ONLY three test files (no
   production change — git-confirmed) so exit/config/integration contracts are
   untouched; Phases 1-3 and non-`--axi` behavior intact; no test weakened. No
   CRITICAL/HIGH. Detector positive controls sound (osc8 + C1 genuinely fail the
   detector).
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | MEDIUM | axi_escape_test.go crafted-field test | Crafted-escape proof drove `renderAXI` (findings table), but `review/resume --axi` actually emit `RenderReviewSummaryAXI` (ID/Dir) — the emitted path had no hostile-content escape test. | **Fixed inline** — added `TestAXIRenderReviewSummary_CraftedFieldEscapeStripped` injecting crafted CSI/OSC into ID/Dir and asserting `requireNoEscapeSequence` (completes AC 04-02 EC1 on the real path). |
   | LOW | review_axi_test.go `assertNoANSIOrMarkdown` | Widely-used helper checked only `\x1b[`/`\x1b]` + `#`/`##`, narrower than its "structural half of the guarantee" comment. | **Fixed inline** — routed the escape half through the shared `findEscapeSequence` (bare ESC + C1), broadened the heading guard to any `#`+space run. |
   | LOW | axi_escape_test.go C1 comment | Comment implied byte-level reach; the C1 arm matches decoded runes (renderAXI re-encodes via WriteRune, so raw invalid-UTF-8 C1 bytes cannot reach stdout). | **Fixed inline** — softened the comment to state it matches well-formed UTF-8 runes, the only form the payload can carry. |

   **Action taken:** No CRITICAL/HIGH → **Phase gate passed.** All three findings are
   within Phase 4's own escape-guarantee test surface — the MEDIUM completes AC 04-02
   Edge Case 1 on the path review/resume actually emit, the LOWs make helper
   comments/coverage match their claims — so all three were **fixed inline** (cheap,
   green, in-scope; consistent with the Phase 1/2 gates fixing LOWs inline) rather than
   deferred to TD. `go test ./cmd/atcr/...` PASS, coverage 86.9%, lint 0 issues,
   `go build`/`go vet ./...` clean.
   **Duration:** 25 min

---

## Phase 5: Documentation & Validation (1 day)

**Items:** Story 5 (AC 05-01, 05-02, 05-03) + cumulative regression sweep

### 5.1 [x] **[Publish Core Content of docs/agentic-consumption.md](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   **Mode:** Pragmatic (documentation, no automated tests) | **AC:** [05-01](plan/acceptance-criteria/05-01-agentic-consumption-doc-content.md)
   1. Verify every concrete detail against Phases 1-4's actual shipped implementation (flag name, `ATCR_AXI_MAX_LINES`, `truncated` field, exit codes 0/1/2/3) — not plan.md's draft language
   2. Draft `docs/agentic-consumption.md` covering: `--axi` invocation on `atcr review`/`atcr report`; the reconciled exit-code contract (linking, not duplicating, `docs/ci-integration.md`); pagination/truncation (default, env var, `truncated` flag, how to retrieve the full payload); the stderr-only-diagnostics/stdout-only-payload guarantee (cross-checked against `docs/logging.md`)
   3. COMMIT: `git commit -m "docs: publish agentic-consumption.md"`
   **Files:** `docs/agentic-consumption.md` (new) | **Duration:** 3h

### 5.1.A [x] **[Publish Core Content - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   **Changed Files:** `docs/agentic-consumption.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. No memory of 5.1's drafting. Do NOT review inline.

   **Subagent findings (run 2026-07-18):** Fresh general-purpose subagent
   cross-checked every named flag/env-var/field/exit-code/payload-shape against the
   shipped code (main.go, review.go, report.go, pagination.go, render.go,
   ci-integration.md, logging.md). Verified: `review --axi` (bool flag) vs `report
   --format axi` (enum) distinction correct — no `report --axi` misspelling;
   `review_summary[1|]{...}` column set exactly matches `reviewSummaryAXIHeader`;
   `findings[N|]{...}` + `truncated:` line matches `renderAXI`/`RenderAXIPaginated`
   (incl. `"file:line"` force-quoted); 500 default + `ATCR_AXI_MAX_LINES` fail-open
   matches `axiMaxLinesFromEnv`; exit 0/1/2/3 links to `ci-integration.md#exit-semantics`
   without duplicating the table and marks the epic's "2=internal error" scheme
   rejected; `--axi --auto-fix`→2, render fault→1; no invented exit 4 / sub-flags;
   `review --axi` correctly emits only the run summary, not findings. All four
   required topics present; all cross-referenced docs and anchors resolve.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | None | — | No discrepancies found. | None required. |

   **Action taken:** No CRITICAL/HIGH/MEDIUM/LOW → **Adversarial review passed.** Proceed.

### 5.2 [x] **[Publish Core Content - REFACTOR](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   No CRITICAL/HIGH from 5.1.A; prose already tight. Markdown verified clean (10
   fenced blocks balanced, no trailing whitespace, invocation table well-formed).
   No change → no separate commit (same no-change REFACTOR precedent as 2.12/2.17).
   1. Fix CRITICAL/HIGH issues from 5.1.A (if any)
   2. Polish prose, verify Markdown renders cleanly, COMMIT: `git commit -m "refactor(docs): tighten agentic-consumption.md"`
   **Duration:** 30min

### 5.3 [x] **[Worked Orchestration Example](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   **Mode:** Pragmatic (documentation, no automated tests) | **AC:** [05-02](plan/acceptance-criteria/05-02-worked-orchestration-example.md)
   1. Draft a near-runnable shell/pseudocode example modeled on the epic's autonomous-sweeper scenario: subprocess invocation of `atcr review --axi`, payload parsing (checking `truncated`), exit-code branching (0/1/2/3, real `case $? in ... esac` style), and explicit stderr-vs-stdout capture
   2. Spot-check the shell portion against a built `atcr` binary where feasible
   3. COMMIT: `git commit -m "docs: add worked orchestration example"`
   **Files:** `docs/agentic-consumption.md` | **Duration:** 2h

### 5.3.A [x] **[Worked Orchestration Example - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   **Changed Files:** `docs/agentic-consumption.md`

   **Spawn a fresh subagent** via the Agent tool. No memory of 5.3's drafting. Do NOT review inline.

   **Subagent findings (run 2026-07-18):** Fresh general-purpose subagent verified
   the worked example against shipped code (built and probed the binary too). All
   flags/fields/exit codes/TOON row shapes match: `--axi`, `--fail-on high`
   (lowercase valid — ParseSeverity uppercases), `report --format axi`, render-fault
   exit 1, `ATCR_AXI_MAX_LINES` fail-open, `truncated: %t`, two-space-indented rows,
   quoted `"file:line"`. Parse logic correctly skips header + `truncated:` line. No
   secrets, no invented flags/fields/exit codes. Two LOWs on exit-code accuracy:
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | LOW | agentic-consumption.md case 3 branch | Exit 3 (auth) fires only on a `--sync-cloud` auth failure; the shown command has no `--sync-cloud`, so a missing provider key surfaces as exit 2 (`preflightAPIKeys`→`usageError`), never 3. Doc's "every exit code is real" overstated for this exact invocation. | Note that exit 3 applies only under `--sync-cloud`; keep the branch as defensive handling. |
   | LOW | agentic-consumption.md report-failure handler | Handler hardcodes `"(exit 1)"`/`exit 1`, but `atcr report --format axi` returns exit 2 for usage/config faults (bad anchor, `--output` validation, absent findings.json). Only an internal render fault yields exit 1. | Capture and echo the real `$?` instead of asserting exit 1. |

   **Action taken:** No CRITICAL/HIGH. Both LOWs **RESOLVED in 5.4** (not deferred) —
   consistent with the 1.5.A/2.19/Phase-4-gate precedent of fixing inline any LOW
   that makes a *shipped claim false* when the fix is cheap and in-scope: finding #1
   directly contradicts the doc's own "every ... exit code in it is real" line, and
   both are trivial doc-accuracy edits squarely inside 5.4's REFACTOR scope.

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.4, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.4 [x] **[Worked Orchestration Example - REFACTOR](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   Both 5.3.A LOWs fixed: (1) added a comment clarifying exit 3 is the `--sync-cloud`
   auth failure (unreachable for the shown command; a missing provider key → exit 2)
   and softened the intro to "a real part of atcr's contract"; (2) the report-failure
   handler now captures `rc=$?` and propagates the real exit (2 for usage/config, 1
   for render fault) instead of hardcoding exit 1. `bash -n` clean.
   1. Fix CRITICAL/HIGH issues from 5.3.A (if any)
   2. Polish snippet formatting, COMMIT: `git commit -m "refactor(docs): tighten worked orchestration example"`
   **Duration:** 20min

### 5.5 [x] **[CI-Integration Cross-Reference](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   **Mode:** Pragmatic (documentation, no automated tests) | **AC:** [05-03](plan/acceptance-criteria/05-03-ci-integration-cross-reference.md)
   1. Add a single additive Markdown link from `docs/ci-integration.md` to `docs/agentic-consumption.md` (style precedent: the existing `github-action.md` "see also" link), with anchor text signaling agentic/orchestration relevance
   2. Verify the link target exists (sequenced after 5.1) and no existing table/heading/anchor was reordered or duplicated
   3. COMMIT: `git commit -m "docs(ci): cross-reference agentic-consumption.md"`
   **Files:** `docs/ci-integration.md` | **Duration:** 30min

### 5.5.A [x] **[CI-Integration Cross-Reference - ADVERSARIAL REVIEW (subagent)](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   **Changed Files:** `docs/ci-integration.md`

   **Spawn a fresh subagent** via the Agent tool. No memory of 5.5's edit. Do NOT review inline.

   **Subagent findings (run 2026-07-18):** Fresh general-purpose subagent inspected
   the actual `git show HEAD` diff. Single additive pointer only — no reordering,
   rewriting, or duplication of the exit-semantics table; link target
   `docs/agentic-consumption.md` exists; anchor text clearly signals agentic/
   orchestration relevance distinct from the CI-gate content.
   | Severity | File:Line | Issue | Fix |
   |----------|-----------|-------|-----|
   | None | — | No discrepancies found. | None required. |

   **Action taken:** No findings → **Adversarial review passed.** Proceed.

   **Action Required:**
   - CRITICAL/HIGH found → List issues for 5.6, do NOT proceed until fixed
   - MEDIUM/LOW found → Append to `clarifications/tech-debt-captured.md`
   - None found → Note "Adversarial review passed" and proceed

### 5.6 [x] **[CI-Integration Cross-Reference - REFACTOR](plan/user-stories/05-publish-agentic-consumption-guide.md)**
   No CRITICAL/HIGH from 5.5.A; edit is already a minimal additive pointer (no scope
   creep to trim). No change → no separate commit.
   1. Fix CRITICAL/HIGH issues from 5.5.A (if any)
   2. Trim back to a minimal additive edit if scope crept, COMMIT: `git commit -m "refactor(docs): trim ci-integration cross-reference"`
   **Duration:** 15min

### 5.7 [x] **Phase 5 - DoD Validation + Cumulative Regression Sweep**
   **Result (2026-07-18):** `go test ./...` full suite PASS; coverage 89.1% (≥80%);
   `golangci-lint run` 0 issues; `go vet ./...` + `gofmt -l` clean; `go build ./...`
   rc=0. Non-`--axi` regression: md/json/checklist/sarif goldens byte-unchanged
   (only new `report.axi` fixture added, Phase 1's deliverable). DoD 7/7 auto+story.
   Run DoD Verification Checklist: T3 (`go test ./...` — full suite, all phases), coverage ≥80% overall, `golangci-lint run`, `go build ./...`, docs (this phase's own deliverable). Additionally run the full non-`--axi` regression pass and golden-file re-verification across all formats (`md`/`json`/`checklist`/`sarif`/`axi`) per the Phase 5 focus in sprint-design.md.
   Report using the DoD Report Template.

### 5.8 [x] **Phase 5 - GATE: Integration & Exit Review (subagent)**
   **Scope:** All files changed during Phase 5: `docs/agentic-consumption.md` (new), `docs/ci-integration.md`, `docs/README.md` (index entry)

   **Spawn a fresh subagent** via the Agent tool to perform this integration review. No memory of the phase's implementation. Do NOT review inline.

   **Gate findings (run 2026-07-18):** No CRITICAL/HIGH. Fresh hostile-integrator
   subagent built + probed the binary and read the shipped code. Verified: no
   TODO/placeholder/invented flags/fields/exit codes (CONTRACT EXIT); all
   cross-references + anchors (`ci-integration.md#exit-semantics`,
   `findings-format.md`, `logging.md`, self `#pagination-and-truncation`) resolve
   and the exit table is linked not restated (INTEGRATION); all three Story-5 ACs
   (05-01/05-02/05-03) traceable to committed docs (PHASE-EXIT); docs-only, no
   prior-phase disturbance (REGRESSION). Two LOW prose-accuracy corrections:
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | LOW | agentic-consumption.md:50 | `review_summary` row prose implied 7 integer columns; `findings_total` IS the 6th of six counts (reviewSummaryAXIHeader). Example row `…\|3\|3\|0\|0\|9\|2` was already correct — only prose double-counted. | **Fixed inline** — reworded to "six bare-integer counts — the last, `findings_total`, …". |
   | LOW | agentic-consumption.md:107 | "500 physical lines of findings" overstated by one row: `PaginateAXI` caps TOTAL physical lines incl. the line-1 header, so a default-truncated payload holds ≤499 finding rows. | **Fixed inline** — "500 physical lines — the array header plus up to 499 finding rows (cap counts total physical lines, header inclusive)". |

   **Action taken:** No CRITICAL/HIGH → **Phase gate passed.** Both LOWs fixed inline
   (not deferred) — same precedent as the Phase 1/2/4 gates fixing doc/comment-accuracy
   LOWs inline: they corrected a factual overstatement in this phase's own deliverable,
   the fix was cheap and in-scope, and CONTRACT EXIT literally requires the doc to
   reflect the shipped contract. Committed `2fc9ae10`; fences balanced, no residual
   overstatement.
   **Duration:** 20 min

---

## Final Phase: Validation

### Validation Checklist
- [x] All tests passing (T3): `go test ./...`
- [x] Coverage meets threshold: ≥80% (`config.yaml` `coverage_baseline`)
- [x] Lint/format clean: `golangci-lint run`, `go vet ./...`, `gofmt -l .` empty
- [x] Build succeeds: `go build ./...`

### Optional: Targeted Mutation Testing
Mutation testing tooling (`stryker-mutator`/`mutmut`/`cargo-mutants`) is **UNAVAILABLE** in this environment — skip this step. If a Go mutation tool becomes available later, target only `internal/report/render.go`'s `renderAXI` and `internal/report/pagination.go` (the highest-risk new logic), never the full codebase.

### Drift Analysis
Compare final implementation against [plan/original-requirements.md](plan/original-requirements.md):
- [x] `atcr review --axi` outputs clean, machine-readable (TOON/JSON) payload free of ANSI codes or markdown tables
- [x] Stderr strictly used for progress bars/logs; stdout contains only the final payload
- [x] Exit codes deterministically reflect the review outcome (0=clean, 1=findings, 2=usage-error, 3=auth-error — reconciled per Story 2, superseding the epic's original 0/1/2 draft)
- [x] `atcr` docs include an "Agentic Consumption" section explaining orchestration in larger swarms
- [x] Line-cap/truncation guidance (default 500, `ATCR_AXI_MAX_LINES` override) implemented and documented
- [x] No extended-scope drift beyond the plan's Components Touched (`internal/cli`/`internal/formatters` mapped onto the real `internal/report`/`cmd/atcr`/`internal/mcp` structure per the epic's Advisory Observations)
