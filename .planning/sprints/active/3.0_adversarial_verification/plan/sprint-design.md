# Sprint Design: Adversarial Verification

**Created:** June 14, 2026
**Plan:** [3.0: Adversarial Verification](.planning/plans/active/3.0_adversarial_verification/)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> Add a verification stage in which skeptic agents — different models from the finders, with tool access — attempt to refute each unique finding before it reaches the final report. Verdicts feed a second confidence axis, refuted findings are demoted (never deleted), and `--fail-on` counts only non-refuted findings. This is the stage that makes the CI gate trustworthy enough to block merges.

**Referenced Resources:**
- [Verification Pipeline Architecture](documentation/verification-pipeline.md)
  - **Summary**: Core verification mechanics: skeptic selection with different-model rule, verdict envelope parsing, confidence v2 model, re-emit, and gate semantics.
  - **Key Points**: Defines `Verification` struct at `emit.go:36`, verdict enum (`confirmed|refuted|unverifiable`), confidence v2 tiers (VERIFIED > HIGH > MEDIUM > LOW), and artifact schemas.
- [CLI & MCP Integration](documentation/cli-mcp-integration.md)
  - **Summary**: `atcr verify` subcommand, `atcr_verify` MCP tool, `--verify` chaining flag, and gate logic updates.
  - **Key Points**: Cobra command pattern in `cmd/atcr/verify.go`, MCP handler in `handlers.go`, gate counter updates at `gate.go:57` and `handlers.go:339`.
- [LLM Integration & Tool Loop](documentation/llm-tool-loop.md)
  - **Summary**: Skeptic invocation via `invokeToolLoop` at `loop.go:81`, per-finding prompt construction, budget controls.
  - **Key Points**: Reuses Epic 2.0 tool loop unchanged; skeptics get per-finding scope with adversarial framing; budgets are per-finding (MaxTurns=10, ToolBudgetBytes=1MB, Timeout=60s).

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Adversarial Verification Sprint
**Complexity:** 8/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 5
**Pattern:** Foundation → Core Items → Advanced → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
skeptic selection different-model rule
verdict parsing tool loop invocation
confidence v2 re-emit artifact writes
gate semantics fail-on require-verified
verification pipeline skeptic registry
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - New `internal/verify/` package with 6+ files; confidence model v2 adds VERIFIED tier; gate semantics update existing `CountAtOrAbove`
- **Integration:** 2/3 - Integrates with fanout (tool loop), registry (role filtering), reconcile (findings/gate), payload (manifest), MCP (handlers) — 5+ packages
- **Story/Task & Test:** 3/3 - 6 stories, 28 ACs, 15 unit + 11 integration tests; complex verdict parsing (7 cases), vote aggregation, gate matrix (12+ scenarios)
- **Risk/Unknowns:** 1/3 - LLM skeptic accuracy is the main unknown; mechanics are well-specified in documentation; fixture corpus mitigates

**Time Formula:** Base 8 days for COMPLEX + (Integration score + Test score - 2) * 0.5 days adjustment
**Calculation:** 8 + (2 + 3 - 2) * 0.5 = 8 + 1.5 ≈ 10 days

---

## Recommended Flags

**Adversarial:** true (complexity >= 6 AND phases >= 3)
**Gated:** true (complexity >= 8)
**Recommendation strength:** false (complexity < 10)
**Suggested command:** `/create-sprint @.planning/plans/active/3.0_adversarial_verification/ --gated`

---

## Phase Structure

### Phase 1: Foundation (2 days)
**Focus:** Role plumbing, verify package scaffolding, primitives

**Items:**
- Story 1: Skeptic Selection & Role Plumbing (all 5 ACs)
  - [01-01] AgentsByRole Filtering
  - [01-02] Different-Model Exclusion Rule
  - [01-03] Empty Selection → Unverifiable
  - [01-04] Empty-Role Backward Compatibility
  - [01-05] Test Coverage Requirements

**Files:**
- CREATE: `internal/verify/select.go`
- CREATE: `internal/verify/select_test.go`
- MODIFY: `internal/registry/config.go` (add `AgentsByRole` method)

**TDD:** RED → GREEN → REFACTOR for each AC

---

### Phase 2: Core — Skeptic Invocation (3 days)
**Focus:** Prompt construction, verdict parsing, tool loop invocation, vote aggregation

**Items:**
- Story 2: Skeptic Invocation & Verdict Parsing (all 7 ACs)
  - [02-01] Skeptic Prompt Construction
  - [02-02] Verdict Parsing
  - [02-03] Skeptic Invocation via Tool Loop
  - [02-04] Failure Isolation
  - [02-05] Budget Forwarding
  - [02-06] Test Coverage
  - [02-07] Verify Minimum Severity Registry Config

**Files:**
- CREATE: `internal/verify/skeptic.go`
- CREATE: `internal/verify/verdict.go`
- CREATE: `internal/verify/invoke.go`
- CREATE: `internal/verify/votes.go`
- CREATE: `internal/verify/skeptic_test.go`
- CREATE: `internal/verify/verdict_test.go`
- CREATE: `internal/verify/invoke_test.go`
- CREATE: `internal/verify/votes_test.go`
- CREATE: `internal/verify/testdata/true-finding.json`
- CREATE: `internal/verify/testdata/false-finding.json`
- CREATE: `internal/verify/testdata/malformed-response.txt`

**TDD:** RED → GREEN → REFACTOR for each AC

---

### Phase 3: Advanced — Confidence v2 & Re-emit (2 days)
**Focus:** Confidence recomputation, artifact emission, gate counter update

**Items:**
- Story 3: Confidence v2 & Re-emit (all 5 ACs)
  - [03-01] Confidence V2 Recomputation
  - [03-02] Verification JSON Emission
  - [03-03] Findings Re-Emit with Verification Blocks
  - [03-04] Manifest & Summary Updates
  - [03-05] Gate Excludes Refuted

**Files:**
- CREATE: `internal/verify/confidence_v2.go`
- CREATE: `internal/verify/emit_verification.go`
- CREATE: `internal/verify/emit_findings.go`
- CREATE: `internal/verify/emit_manifest.go`
- CREATE: `internal/verify/emit_summary.go`
- CREATE: `internal/verify/confidence_v2_test.go`
- CREATE: `internal/verify/emit_test.go`
- MODIFY: `internal/reconcile/gate.go` (update `CountAtOrAbove` signature)

**TDD:** RED → GREEN → REFACTOR for each AC

---

### Phase 4: Integration — CLI, MCP, Gate (2 days)
**Focus:** User-facing interfaces, gate semantics, flag plumbing

**Items:**
- Story 4: CLI Command & MCP Tool (all 5 ACs)
  - [04-01] `atcr verify` CLI Subcommand
  - [04-02] `atcr review --verify` Chaining
  - [04-03] `atcr_verify` MCP Tool
  - [04-04] Artifact Consistency & Error Handling
  - [04-05] Skip Already-Verified Findings Unless `--fresh`
- Story 5: Gate Semantics (all 2 ACs)
  - [05-01] Gate Filtering with `--fail-on` and `--require-verified`
  - [05-02] MCP Handler Parity & Fixture Matrix Tests

**Files:**
- CREATE: `cmd/atcr/verify.go`
- CREATE: `cmd/atcr/verify_test.go`
- MODIFY: `cmd/atcr/main.go` (register verify subcommand)
- MODIFY: `cmd/atcr/review.go` (add `--verify` flag)
- MODIFY: `cmd/atcr/reconcile.go` (add `--require-verified` flag)
- MODIFY: `internal/mcp/server.go` (register `atcr_verify`)
- MODIFY: `internal/mcp/handlers.go` (add `handleVerify`, update `failingFindings`)
- CREATE: `internal/mcp/handlers_verify_test.go`
- CREATE: `internal/reconcile/gate_matrix_test.go`

**TDD:** RED → GREEN → REFACTOR for each AC

---

### Phase 5: Validation — Report, Docs, Fixtures (1 day)
**Focus:** Report rendering, documentation, fixture corpus, integration tests

**Items:**
- Story 6: Report Updates & Documentation (all 4 ACs)
  - [06-01] Report Rendering with Verification Sections
  - [06-02] Backward Compatibility with V1 Findings
  - [06-03] Verification Documentation
  - [06-04] Verification Fixture Corpus

**Files:**
- MODIFY: `internal/report/render.go` (add Skeptic section, Refuted section, VERIFIED tier)
- CREATE: `internal/report/testdata/findings-with-verification.json`
- CREATE: `internal/report/testdata/report-v2.md` (golden file)
- CREATE: `internal/report/render_verification_test.go`
- CREATE: `docs/verification.md`
- MODIFY: `docs/registry.md` (add `role: skeptic` subsection)
- MODIFY: `docs/findings-format.md` (document verification block)

**TDD:** RED → GREEN → REFACTOR for each AC

---

## Work Decomposition

### Story 1: Skeptic Selection & Role Plumbing (M)

**Testable Elements:**
1. `Registry.AgentsByRole(role)` returns filtered map by role constant
2. Empty `Role` defaults to `RoleReviewer` for backward compatibility
3. `SelectEligibleSkeptics(finding, n)` enforces different-model rule
4. No eligible skeptic → empty slice → caller produces `unverifiable`
5. Model comparison is exact string match on `AgentConfig.Model`

**AC Links:** 01-01, 01-02, 01-03, 01-04, 01-05

---

### Story 2: Skeptic Invocation & Verdict Parsing (L)

**Testable Elements:**
1. `buildSkepticPrompt` constructs deterministic prompt with adversarial framing
2. `parseVerdict` handles 7 cases: confirmed, refuted, unverifiable, malformed JSON, invalid enum, empty response, extra fields
3. `invokeSkeptic` drives tool loop via `fanout.Engine.Run`; failures → `unverifiable`
4. `aggregateVerdicts` applies majority rule; disagreeing → `unverifiable`
5. Per-finding budgets forwarded to tool loop

**AC Links:** 02-01, 02-02, 02-03, 02-04, 02-05, 02-06, 02-07

---

### Story 3: Confidence v2 & Re-emit (L)

**Testable Elements:**
1. `confidenceV2` maps: confirmed→VERIFIED, refuted→LOW, unverifiable→v1 unchanged
2. `WriteVerification` writes `verification.json` with correct schema
3. `ReEmitFindings` populates verification blocks and recomputes confidence
4. `UpdateManifestStage` appends "verify" idempotently
5. `UpdateSummaryVerdicts` adds `verdictCounts` to summary
6. Gate counter excludes refuted; supports `requireVerified`

**AC Links:** 03-01, 03-02, 03-03, 03-04, 03-05

---

### Story 4: CLI Command & MCP Tool (M)

**Testable Elements:**
1. `atcr verify` subcommand exists with `--fresh`, `--thorough`, `--min-severity` flags
2. `atcr review --verify` chains review → reconcile → verify
3. `atcr_verify` MCP tool registered and returns structured result
4. All three entry points produce identical artifacts for same input
5. Error handling: missing reconciled findings → clear error message

**AC Links:** 04-01, 04-02, 04-03, 04-04, 04-05

---

### Story 5: Gate Semantics (S)

**Testable Elements:**
1. `--fail-on` excludes findings with `verdict=="refuted"`
2. `--require-verified` counts only `confidence=="VERIFIED"`
3. MCP `failingFindings` mirrors CLI gate logic
4. Fixture matrix: 12+ scenarios (3 verdicts × 3 severities × 2 flag states)

**AC Links:** 05-01, 05-02

---

### Story 6: Report Updates & Documentation (M)

**Testable Elements:**
1. Report renders Skeptic section for verified findings
2. Report renders collapsed Refuted section at bottom
3. Confidence v2 tiers: VERIFIED rendered distinctly
4. Backward compat: v1 findings (no verification) render identically
5. `docs/verification.md` covers mechanics, confidence v2, gate semantics
6. Fixture corpus: `true-finding.json`, `false-finding.json`, `malformed-response.txt`

**AC Links:** 06-01, 06-02, 06-03, 06-04

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located with source (`*_test.go` alongside source files in `internal/verify/`, `internal/reconcile/`, `internal/mcp/`, `internal/report/`)

**Test File Placement Examples:**
- `internal/verify/select_test.go` — role filtering, different-model rule
- `internal/verify/verdict_test.go` — verdict parsing (7 cases)
- `internal/verify/invoke_test.go` — skeptic invocation, failure isolation
- `internal/verify/confidence_v2_test.go` — confidence recomputation
- `internal/verify/emit_test.go` — artifact emission (verification.json, findings.json, manifest.json, summary.json)
- `internal/reconcile/gate_matrix_test.go` — gate fixture matrix (12+ scenarios)
- `internal/mcp/handlers_verify_test.go` — MCP handler integration tests
- `internal/report/render_verification_test.go` — report rendering with verification

**Unit/Integration/E2E:**
- **Unit:** 17 ACs require unit tests (table-driven, >= 95% coverage on new code)
- **Integration:** 11 ACs require integration tests (CLI invocation, MCP handler, artifact round-trips)
- **E2E:** 0 ACs require E2E tests (fixture corpus enables end-to-end validation without real LLM calls)
- **Tools:** `go test ./...`, `testify/assert`, `testify/require`

**Test Environment Status:**
- Framework: Go standard `testing` + `testify`
- Execution: `go test ./...`
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (baseline: 80%)

---

## Architecture

**Primitives:**
- `Verdict` enum: `confirmed | refuted | unverifiable`
- `Confidence` tiers v2: `VERIFIED > HIGH > MEDIUM > LOW`
- `Severity` levels: `HIGH > MEDIUM > LOW`
- `Verification` struct: `{Verdict, Skeptic, Notes}` (reserved at `emit.go:36`)
- `FindingKey`: `{File, Line, Problem}` — composite key for verdict-to-finding matching

**Module Boundaries:**
- `internal/verify/select.go` — role filtering, different-model rule (pure functions, no I/O)
- `internal/verify/skeptic.go` — prompt construction (pure function, deterministic)
- `internal/verify/verdict.go` — verdict parsing (pure function, handles malformed input)
- `internal/verify/invoke.go` — skeptic invocation via tool loop (I/O, wraps fanout.Engine)
- `internal/verify/votes.go` — vote aggregation (pure function)
- `internal/verify/confidence_v2.go` — confidence recomputation (pure function)
- `internal/verify/emit_*.go` — artifact emission (I/O, atomic writes)
- `internal/verify/pipeline.go` — orchestration (calls select, invoke, emit)

**External Dependencies:**
- `fanout.Engine.Run` — tool loop driver (wrapped, not modified)
- `registry.Registry` — agent configuration (read-only)
- `reconcile.ReadReconciledFindings` — input loader (read-only)
- `payload.WriteManifest` — manifest writer (wrapped)
- OpenAI-compatible LLM API via `llmclient.Client` (wrapped by fanout)

**Replaceability:**
- Each `internal/verify/*.go` file is independently replaceable via its public API
- `invokeSkeptic` depends on `fanout.Engine` via interface (`ChatCompleter`) — swap implementations for testing
- Verdict parsing is a pure function — replaceable without affecting invocation
- Emission functions are isolated — replaceable without affecting pipeline logic

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Skeptic code access | Tool loop grants read access to codebase | Sensitive data exposure (credentials, secrets in code) | Skeptics run in same sandbox as reviewers (Epic 2.0); no write access; transcript logging for audit |
| Prompt injection | Finding descriptions crafted by reviewer LLMs | Malicious finding text could manipulate skeptic behavior | Adversarial framing instructs skeptic to verify evidence; skeptic uses tools to check actual code, not just trust the finding description |
| Verdict manipulation | Skeptic output parsed into verdict | Malformed JSON could bypass validation | Strict enum validation (`confirmed|refuted|unverifiable`); malformed output → `unverifiable` with raw text preserved; never drops the finding |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Per-finding skeptic invocation | 50 findings × 1 skeptic (default) = 50 LLM calls per review | < 5 min total verification time | Per-finding budgets (MaxTurns=10, Timeout=60s); `min_severity` floor skips LOW findings; `--fresh` skips already-verified |
| Tool loop token usage | Each skeptic reads code context (file bodies) | < 100K tokens per finding | Pre-truncate file entries before prompt construction; payload context size is caller's responsibility |
| Artifact writes | 4 files written per verify run (verification.json, findings.json, manifest.json, summary.json) | < 1s total write time | Atomic writes (temp file + rename); no locking needed (single-writer assumption) |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| No eligible skeptics | All skeptics share models with reviewers; no skeptics registered | `verdict="unverifiable"`, `notes="no_eligible_skeptic"`; finding retained with v1 confidence |
| Skeptic failure | Provider error, timeout, malformed output | `verdict="unverifiable"` with explanatory `notes`; finding never dropped; run never fails |
| Concurrent verification | Two `atcr verify` runs on same review directory | Last writer wins (atomic rename); no locking; document this as known limitation |
| Idempotency | Re-run `atcr verify` on already-verified findings | Skip findings with existing verdict unless `--fresh`; manifest "verify" stage not duplicated |
| Gate edge cases | Naturally-LOW finding (not refuted); v1-only finding (no verification block) | Naturally-LOW findings count if at/above threshold; v1-only findings count as non-refuted, non-VERIFIED |

### Defensive Measures Required

- **Input Validation:** Verdict enum validated against `{confirmed, refuted, unverifiable}`; malformed JSON → `unverifiable`; invalid enum → `unverifiable` with raw text preserved
- **Error Handling:** Skeptic failures (timeout, provider error) captured in `Verification.Notes`; never propagated to caller; pipeline completes for all findings
- **Logging/Audit:** Structured logging at invocation site (skeptic name, finding ID, error class); transcript recording via tool loop (already implemented in Epic 2.0)
- **Rate Limiting:** Per-finding budgets (MaxTurns, ToolBudgetBytes, Timeout) act as rate limits; `min_severity` floor skips low-priority findings
- **Graceful Degradation:** Skeptic failure → `unverifiable` (not crash); no eligible skeptic → `unverifiable` (not crash); all findings retained regardless of verdict

---

## Risks

**Technical:**
| Risk | Mitigation |
|------|------------|
| Import cycle between `internal/verify` and `internal/reconcile` | `verify` imports `reconcile` (for `JSONFinding`), but `reconcile` must not import `verify`. Gate counter update is in `reconcile/gate.go` and does not call `verify` functions. Verify with `go build ./...` after scaffolding. |
| Atomic write fails on non-POSIX systems | Use `os.Rename` (atomic on POSIX). Document POSIX-only support (Linux, macOS). |
| `FindingKey` collision (same file+line+problem) | Use composite key (file+line+problem hash); reconciler should have deduplicated; document assumption. |
| Manifest "verify" stage duplicated on re-run | `UpdateManifestStage` checks for existing "verify" before appending (idempotent). Unit test with manifest containing "verify". |

**TDD-Specific:**
| Risk | Mitigation |
|------|------------|
| Verdict parsing tests miss edge cases (e.g., markdown-fenced JSON) | Test 7 explicit cases from testing-fixtures.md; add fenced/unfenced variants; scan for `{...}` before falling back to `unverifiable`. |
| Gate matrix tests incomplete (miss empty verdict, naturally-LOW) | Matrix includes: 3 verdicts × 3 severities × 2 flag states = 12 scenarios; add v1-only finding test case. |
| Golden file cascade (updating `report.md` breaks other tests) | Use separate golden file name (`report-v2.md`) for v2 variant; check for other tests reading `testdata/report.md`. |
| Mock skeptic does not match real LLM behavior | Fixture corpus includes `true-finding.json` and `false-finding.json` for scripted mock skeptic tests; document that real LLM behavior may differ. |

---

**Next:** `/create-sprint @.planning/plans/active/3.0_adversarial_verification/ --gated`
