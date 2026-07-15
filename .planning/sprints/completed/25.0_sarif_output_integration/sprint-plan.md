# Sprint 25.0: SARIF Output Integration

---
executor: /execute-sprint
execution_mode: continuous
context_recovery: On context compaction, read .planning/.temp/execute-sprint/context.env for phase state. Resume at first unchecked phase below.
---

**Directions:** Work through Sprint 25.0 step-by-step. Complete each step, check off work immediately. After completing a phase, proceed to the next without waiting.

Before each phase, review `/CLAUDE.md` (or AGENTS.md).

---

## Sprint Overview

**Metadata:** See [metadata.md](metadata.md) for complete plan and sprint tracking details.

**Original Request:** [Full details in plan/original-requirements.md](plan/original-requirements.md)

### What We're Building

ATCR gains a fourth render target — SARIF 2.1.0 JSON — via a new `--format=sarif` option on `atcr report`. This lets ATCR's reconciled review findings feed GitHub Advanced Security's Code Scanning "Security" tab and GitLab CI's native SAST report widget, the two centralized, cross-repo security surfaces ATCR's existing `atcr github` PR-check/inline-comment integration does not reach.

### Why This Matters

GitHub's Security tab and GitLab's SAST widget only ingest SARIF, not ATCR's existing markdown/JSON output — SARIF export is the missing piece that lets ATCR findings reach these centralized dashboards instead of only PR-level checks/comments.

### Key Deliverables

- SARIF 2.1.0 document structure (`$schema`, `runs[]`, `tool.driver`, `rules[]`, `results[]`) as a new `renderSarif` render target in `internal/report`
- Severity-to-SARIF-`level` mapping (`sarifLevel`) that reuses the canonical `reconcile.NormalizeSeverity`/`SeverityRank` rubric exclusively
- Line-level anchoring plus a synthesized `1,1,1,1` fallback region for file-level findings, so every finding renders in GitHub's Security tab
- CLI flag help text update and CLI/MCP output-parity regression coverage
- `docs/ci-integration.md` CI integration examples for GitHub Code Scanning upload and GitLab CI's SAST report widget

### Success Criteria

- `atcr report --format=sarif` produces valid, schema-conformant SARIF JSON (validated against a local SARIF 2.1.0 schema fixture via `google/jsonschema-go`)
- ATCR severities (CRITICAL, HIGH, MEDIUM, LOW) map deterministically to SARIF levels (`error`/`warning`/`note`) via a single rubric call site, with no second comparison chain
- File paths and line numbers correctly anchor to the git diff, with file-level findings (`Line<=0`) still visible via synthesized fallback coordinates
- A documentation example exists showing both the GitHub Code Scanning upload and the GitLab CI SAST-widget equivalent, clearly distinguished from the already-shipped `atcr github` flow

**CRITICAL REMINDER:** Every task in this sprint must contribute to fulfilling the original request. If a task seems unrelated to what the user actually asked for, STOP and validate before proceeding. Do not add scope beyond the original request.

---

## TDD Strategy

**Mode:** Moderate 🔄 (complexity 6/12) — RED (tests) → GREEN (minimal implementation) → 🎯 ADVERSARIAL (fresh-subagent review) → REFACTOR, per story/element.

**Adversarial:** ENABLED — inline-fix bar `CRITICAL/HIGH`, deferred to tech debt: `MEDIUM/LOW`.

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

| Tier | When | Command Pattern |
|------|------|-----------------|
| T1: Focused | After each small change | `go test ./internal/report/... -run TestSarif` |
| T2: Module | After completing element | `go test ./internal/report/...` |
| T3: Full | DoD validation, pre-commit | `go test ./...` |

### DoD Verification Checklist
1. Tests (T3): All passing
2. Coverage: ≥80%
3. Lint: No errors (`golangci-lint run`)
4. Build: Succeeds (`go build ./...`)
5. Docs: Updated

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

### Implementation Standards
- Black box interfaces: `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error` matches the signature of every existing `render*` sibling — no leaked implementation details.
- Replaceable component: `sarif.go` can be swapped wholesale without touching `render.go` beyond its single dispatch line.
- Primitive-first: `reconcile.JSONFinding` remains the sole input primitive — no new data collection or schema change.

### Coding Standards (Go)
- Naming: unexported helpers `camelCase` (`sarifLevel`, `sarifLocation`); exported/dispatched constant `PascalCase`-adjacent (`FormatSarif`, matching `FormatMarkdown`/`FormatJSON`/`FormatChecklist`).
- Imports grouped: stdlib → third-party → internal (`github.com/samestrin/atcr/...`), arranged via `goimports`.
- Errors: return as last parameter, wrap with `fmt.Errorf("...: %w", err)`, never ignored.
- Tests: table-driven, `*_test.go` co-located in the same package directory, `t.Run` subtests per the `internal/report/render_test.go` convention.
- Formatting/Linting: `go fmt`/`goimports` before commit; `golangci-lint run` and `go vet ./...` must pass.

### Git Strategy
- Branch: `feature/25.0_sarif_output_integration` from `main`.
- Commits: Conventional Commits (`feat(report): ...`, `test(report): ...`, `docs(ci): ...`, `refactor(report): ...`).
- PR: squash-merge to `main`, CI (`Go CI`: format, vet, lint, unit tests) must pass.

---

## External Resources

- [SARIF 2.1.0 Schema Reference](plan/documentation/sarif-schema-reference.md) — base spec vs. GitHub's region-required-for-display constraint.
- [GitHub Code Scanning SARIF Integration Constraints](plan/documentation/github-code-scanning-integration.md) — `tool.driver.rules[]`/`ruleId` linkage requirement.
- [Schema-Validating SARIF Output with jsonschema-go](plan/documentation/schema-validation-with-jsonschema-go.md) — test-only `google/jsonschema-go` validation path.
- [encoding/json Conventions for renderSarif](plan/documentation/json-encoding-conventions.md) — struct-tree + `json.MarshalIndent` convention.
- [.planning/specifications/packages/jsonschema-go.md](../../../specifications/packages/jsonschema-go.md)
- [.planning/specifications/packages/standard-library.md](../../../specifications/packages/standard-library.md)

---

## Sprint Phases

---

**AGENT INSTRUCTIONS:** You MUST update this file (`sprint-plan.md`) and the corresponding task files in `plan/acceptance-criteria/` immediately upon completing each item. Mark tasks as `[x]`. Do NOT wait for user confirmation to proceed to the next phase. Continue autonomously until human intervention is strictly required.

---

## Phase 1: Foundation — SARIF Document Structure

**Items:** AC [01-01](plan/acceptance-criteria/01-01-format-registration.md) (Format Constant Registration), AC [01-02](plan/acceptance-criteria/01-02-sarif-document-structure.md) (Base Document Structure), AC [01-03](plan/acceptance-criteria/01-03-rules-array-category-linkage.md) (Rules Array / Category Linkage)

### 1.1 [x] **[Story 1: SARIF Formatter Core - RED](plan/user-stories/01-sarif-formatter-core.md)**
   1. Analyze AC 01-01/01-02/01-03, identify testable units: `FormatSarif` registration in `ValidFormat`/`Formats`/`Render`, top-level document shape (`$schema`, `version`, `runs[]`, `tool.driver.name`), `results[]`/`rules[]` nil-slice guard, `rules[]` dedup-by-`Category` in first-seen order with `id`/`shortDescription.text`/`fullDescription.text`.
   2. Write table-driven tests in `internal/report/sarif_test.go`: format registration (`ValidFormat("sarif")`, `Formats()` includes `sarif`, unknown-format error lists `sarif`), document shape assertions, empty/nil findings → `results:[]`/`rules:[]` never `null`, rules dedup ordering, empty-`Category` edge case.
   3. Extend `internal/report/render_test.go`'s `goldenCases` with `{"sarif", FormatSarif, "report.sarif.json"}` (fixture not yet generated — expected to fail).
   4. Verify all new tests fail correctly (compile error or assertion failure — `FormatSarif`/`renderSarif` do not exist yet).
   **Files:** `internal/report/sarif_test.go` (new), `internal/report/render_test.go` (modify) | **Duration:** 0.5 day

### 1.2 [x] **[Story 1: SARIF Formatter Core - GREEN](plan/user-stories/01-sarif-formatter-core.md)**
   Add `FormatSarif = "sarif"` next to the existing format constants in `internal/report/render.go`; extend `ValidFormat()`, `Formats()`, and add a `case FormatSarif: return renderSarif(w, findings)` arm to `Render()`. Create `internal/report/sarif.go` defining the SARIF struct tree (`sarifLog`, `sarifRun`, `sarifTool`, `sarifDriver`, `sarifRule`, `sarifResult`) and `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error`, applying `renderJSON`'s nil-slice guard to both `results[]` and `rules[]`, `json.MarshalIndent(..., "", "  ")` + trailing newline. Rule collection iterates findings once, deduping `Category` in first-seen order. Generate golden fixture `internal/report/testdata/report.sarif.json` (≥2 distinct categories) via `go test ./internal/report -update` once tests pass (T1 after each change, T2 once the story is complete). COMMIT: `git commit -m "feat(report): add SARIF 2.1.0 formatter core"`.
   **Files:** `internal/report/sarif.go` (new), `internal/report/render.go` (modify), `internal/report/testdata/report.sarif.json` (new) | **Duration:** 1 day

### 1.2.A [x] **[Story 1: SARIF Formatter Core - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-sarif-formatter-core.md)**
   **Changed Files:** `internal/report/sarif.go`, `internal/report/render.go`, `internal/report/sarif_test.go`, `internal/report/render_test.go`, `internal/report/testdata/report.sarif.json`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 1.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 1.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): as listed above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose subagent, 2026-07-14):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | MEDIUM | sarif.go:181 | Empty `File` → `artifactLocation.uri:""` may break GitHub ingestion | Captured TD-001 (AC 03-01 EC3 mandates pass-through) |
   | LOW | sarif.go:143 | Empty `Category` → blank `ruleId`/rule `id` | Captured TD-002 (AC 01-03 EC2 mandates pass-through) |
   | LOW | sarif.go:164 | Unknown severity → `"warning"` (above LOW's `"note"`) | Rejected — AC 02-01 EC3/4/5 explicitly mandate `"warning"` fallback |

   No CRITICAL/HIGH. Security (JSON injection) clean — encoding/json escaping sufficient for a JSON sink. Error propagation (marshal + write) covered. Dedup is O(n).

   **Action Taken:** No CRITICAL/HIGH → proceed to 1.3. MEDIUM/LOW genuine risks appended to `tech-debt-captured.md` (TD-001, TD-002); the third finding contradicts AC 02-01 and was rejected with rationale.

### 1.3 [x] **[Story 1: SARIF Formatter Core - REFACTOR](plan/user-stories/01-sarif-formatter-core.md)**
   1. Fix CRITICAL/HIGH issues from 1.2.A (if any) — none found.
   2. Improve code and tests (T1) — confirmed `sarifRules`/`results[]` dedup is O(n) (seen-set + ordered slice), no O(n²) scans; code already reads cleanly.
   3. Validate all tests still pass (T3): `go test ./...` → ALL PASS.
   4. COMMIT: skipped — no code changes to commit (no refactor needed; empty commit avoided).
   **Duration:** 0.25 day

---

## Phase 2: Severity & Anchoring

**Items:** AC [02-01](plan/acceptance-criteria/02-01-severity-level-mapping.md) (Severity-to-SARIF-Level Mapping), AC [03-01](plan/acceptance-criteria/03-01-line-level-anchoring.md) (Line-Level Anchoring), AC [03-02](plan/acceptance-criteria/03-02-file-level-fallback-anchoring.md) (File-Level Fallback Anchoring)

### 2.1 [x] **[Story 2+3: Severity Mapping & Line/File Anchoring - RED](plan/user-stories/02-severity-to-sarif-level-mapping.md)**
   1. Analyze AC 02-01/03-01/03-02, identify testable units: `sarifLevel(severity string) string` (CRITICAL/HIGH→`error`, MEDIUM→`warning`, LOW→`note`, unrecognized→`warning` fallback), `sarifLocation(f reconcile.JSONFinding) ...` (`Line>0` → real line + non-zero synthesized columns; `Line<=0` including `Line==0` and negative → synthesized `1,1,1,1`).
   2. Write table-driven tests in `internal/report/sarif_test.go`: all four canonical severities plus an unrecognized/edge-case token (empty, lowercase, whitespace-padded); `Line>0` normal anchoring, `Line==0` fallback, `Line<0` fallback (as a distinct row, not collapsed with `Line==0`); `Line==1` boundary case asserted as the real-line path, not the fallback.
   3. Verify tests fail correctly (`sarifLevel`/`sarifLocation` do not exist yet).
   **Files:** `internal/report/sarif_test.go` (extend) | **Duration:** 0.5 day

### 2.2 [x] **[Story 2+3: Severity Mapping & Line/File Anchoring - GREEN](plan/user-stories/02-severity-to-sarif-level-mapping.md)**
   Add `sarifLevel(severity string) string` to `internal/report/sarif.go`, normalizing via `reconcile.NormalizeSeverity`/`reconcile.SeverityRank` exclusively — no local redefinition of the severity comparison. Add `sarifLocation(f reconcile.JSONFinding) ...` building `artifactLocation.uri` from `f.File` unmodified; for `Line>0` sets `region.startLine=region.endLine=f.Line` with synthesized `startColumn=endColumn=1` (columns are not tracked in ATCR's finding pipeline, so a fixed `1` is synthesized — matching the file-level fallback's column convention); for `Line<=0` synthesizes `region:{1,1,1,1}`. Wire both helpers into every `renderSarif` result. Extend the golden fixture (`internal/report/testdata/report.sarif.json`) with at least one file-level (`Line<=0`) finding via `go test ./internal/report -update`. Verify all pass (T1 after each change, T2 once complete). COMMIT: `git commit -m "feat(report): add SARIF severity mapping and line/file anchoring"`.
   **Files:** `internal/report/sarif.go` (extend), `internal/report/testdata/report.sarif.json` (extend) | **Duration:** 1 day

### 2.2.A [x] **[Story 2+3: Severity Mapping & Line/File Anchoring - ADVERSARIAL REVIEW (subagent)](plan/user-stories/02-severity-to-sarif-level-mapping.md)**
   **Changed Files:** `internal/report/sarif.go`, `internal/report/sarif_test.go`, `internal/report/testdata/report.sarif.json`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 2.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 2.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): as listed above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent result (fresh general-purpose subagent, 2026-07-14):** No findings.

   Confirmed: `sarifLevel` is the sole severity-comparison site and reuses the canonical `reclib.SeverityRank`/`NormalizeSeverity` rubric (no local map); the `Line <= 0` boundary correctly routes negatives to the 1,1,1,1 fallback (no `startLine <= 0` leak); no integer-overflow or determinism issue.

   **Action Taken:** Adversarial review passed → proceed to 2.3.

### 2.3 [x] **[Story 2+3: Severity Mapping & Line/File Anchoring - REFACTOR](plan/user-stories/02-severity-to-sarif-level-mapping.md)**
   1. Fix CRITICAL/HIGH issues from 2.2.A (if any) — none found.
   2. Improve code and tests (T1) — confirmed via grep that `sarifLevel` is the ONLY severity-comparison site in `sarif.go` (call site at :104, rubric at :170-179); no second comparison chain.
   3. Validate all tests still pass (T3): `go test ./...` → ALL PASS.
   4. COMMIT: skipped — no code changes to commit (no refactor needed).
   **Duration:** 0.25 day

---

## Phase 3: Integration — CLI/MCP Parity & CI Documentation

**Items:** AC [01-04](plan/acceptance-criteria/01-04-cli-flag-and-mcp-parity.md) (CLI Flag Help Text and MCP Parity), AC [04-01](plan/acceptance-criteria/04-01-github-code-scanning-upload-example.md) (GitHub Code Scanning Upload Example), AC [04-02](plan/acceptance-criteria/04-02-gitlab-sast-widget-example.md) (GitLab CI SAST Widget Example)

### 3.1 [x] **[Story 1 (AC 01-04): CLI/MCP Parity - RED](plan/user-stories/01-sarif-formatter-core.md)**
   1. Analyze AC 01-04: `--format` help text mentions `sarif`; CLI output for `--format=sarif` is byte-identical to calling `report.Render` directly; `internal/mcp/handlers.go`'s `handleReport` produces SARIF parity with no code change.
   2. Write a CLI-vs-`report.Render` byte-identical parity test in `cmd/atcr/report_test.go` and a `handleReport` SARIF parity regression test in `internal/mcp/handlers_test.go` — both comparing bytes, not just asserting "no error".
   3. Verify tests fail correctly (help text not yet updated / parity not yet exercised).
   **Files:** `cmd/atcr/report_test.go` (extend), `internal/mcp/handlers_test.go` (extend) | **Duration:** 0.25 day

### 3.2 [x] **[Story 1 (AC 01-04): CLI/MCP Parity - GREEN](plan/user-stories/01-sarif-formatter-core.md)**
   Update `cmd/atcr/report.go`'s `--format` flag help text (`cmd/atcr/report.go:25`, `"output format: md, json, or checklist"`) AND the command's `Short` description (`cmd/atcr/report.go:21`, `"Render md, json, or checklist views over reconciled findings"`) to include `sarif` — both enumerate the format list and both surface in `atcr report --help`, so leaving `Short` stale would omit `sarif` from the help summary line (no new flag wiring — `report.ValidFormat`/`report.Render` already generalize). Verify all pass (T1, T2). COMMIT: `git commit -m "feat(cli): document sarif in --format help text"`.
   **Files:** `cmd/atcr/report.go` (modify) | **Duration:** 0.25 day

### 3.2.A [x] **[Story 1 (AC 01-04): CLI/MCP Parity - ADVERSARIAL REVIEW (subagent)](plan/user-stories/01-sarif-formatter-core.md)**
   **Changed Files:** `cmd/atcr/report.go`, `cmd/atcr/report_test.go`, `internal/mcp/handlers_test.go`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.2 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.2`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): as listed above
     - Checklist (pass verbatim):
       - SECURITY: Auth bypass, injection, data exposure?
       - EDGE CASES: Null, empty, boundaries, concurrent access?
       - ERROR HANDLING: Missing catches, swallowed errors?
       - PERFORMANCE: N+1, leaks, blocking ops?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose subagent, 2026-07-14):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | LOW | report_test.go | `--disagreements --format=sarif` incompatibility (report.go:96) is unpinned by any test | Fixed in 3.3 — added `TestReportCmd_DisagreementsWithSarifIsUsageError` (AC 01-04 Error Scenario 1 explicitly asks to verify this) |

   No CRITICAL/HIGH. Verified: both CLI parity tests assert genuine byte equality vs an independent `report.Render`; SARIF output is deterministic (no timestamps); the MCP in-process parity test is non-tautological; no `--help`/Short golden regression. The unchanged MCP transport enum is intentional/documented scope (see TD-003 below).

   **Action Taken:** No CRITICAL/HIGH → proceed. The LOW is an AC-required coverage gap → fixed inline in 3.3 (not deferred).

### 3.3 [x] **[Story 1 (AC 01-04): CLI/MCP Parity - REFACTOR](plan/user-stories/01-sarif-formatter-core.md)**
   1. Fix CRITICAL/HIGH issues from 3.2.A (if any) — none; addressed the LOW coverage gap instead.
   2. Improve code and tests (T1) — added `TestReportCmd_DisagreementsWithSarifIsUsageError` (AC 01-04 Error Scenario 1). Validated (T3): `go test ./cmd/atcr/... ./internal/mcp/...` → PASS.
   3. COMMIT: `refactor(cli): pin --disagreements/--format=sarif incompatibility` (c11e83f3).
   **Duration:** 0.25 day

### 3.4 [x] **[Story 4: SARIF CI Integration Documentation - RED](plan/user-stories/04-sarif-ci-integration-docs.md)**
   1. Analyze AC 04-01/04-02: define the validation approach for documentation-only work — `yamllint` on both fenced YAML snippets, `markdown-link-check` on the new subsection's links, manual review confirming an explicit distinguishing sentence vs. `atcr github`'s already-shipped PR check/inline-comment flow.
   2. Confirm `docs/ci-integration.md`'s existing "Maintained PR Action" subsection structure (lead-in sentence + fenced snippet + doc link) as the template to mirror.
   3. Note: no automated `go test` coverage for this story (E2E/manual-static per Test Strategy) — `yamllint`/`markdown-link-check` are the T1/T2 equivalent here.
   **Files:** `docs/ci-integration.md` (read, no edit yet) | **Duration:** 0.25 day

### 3.5 [x] **[Story 4: SARIF CI Integration Documentation - GREEN](plan/user-stories/04-sarif-ci-integration-docs.md)**
   Add a new "SARIF Upload for Code Scanning" subsection to `docs/ci-integration.md` beneath "Maintained PR Action": a fenced GitHub Actions YAML snippet (`atcr review && atcr reconcile && atcr report --format=sarif > results.sarif` → `codeql-action/upload-sarif@v3`) and a fenced `.gitlab-ci.yml` snippet (`artifacts: reports: sast: results.sarif`), each with an explicit sentence distinguishing this path from `atcr github`'s PR check/inline-comment flow, linking to `docs/github-action.md`. Run `yamllint`/`markdown-link-check` on the new content. COMMIT: `git commit -m "docs(ci): add SARIF upload examples for GitHub Code Scanning and GitLab SAST"`.
   **Files:** `docs/ci-integration.md` (modify) | **Duration:** 0.5 day

### 3.5.A [x] **[Story 4: SARIF CI Integration Documentation - ADVERSARIAL REVIEW (subagent)](plan/user-stories/04-sarif-ci-integration-docs.md)**
   **Changed Files:** `docs/ci-integration.md`

   **Spawn a fresh subagent** via the Agent tool to perform this review. The subagent has no memory of the implementation in 3.5 — this is intentional, to avoid "I wrote it, it's good" bias. Do NOT review inline.

   Use the Agent tool:
   - subagent_type: `general-purpose`
   - description: `Adversarial review: 3.5`
   - prompt: Self-contained brief including:
     - Files to review (absolute paths): `docs/ci-integration.md`
     - Checklist (pass verbatim, adapted for documentation accuracy):
       - ACCURACY: Does the GitHub Actions snippet correctly pipe `atcr review && atcr reconcile && atcr report --format=sarif` into `codeql-action/upload-sarif@v3`? Does the GitLab snippet use GitLab-native `artifacts:reports:sast` terminology only?
       - CLARITY: Is the distinction from `atcr github`'s PR check/inline-comment flow explicit and near the top of the subsection?
       - CONSISTENCY: Does the new subsection mirror the existing "Maintained PR Action" structure without duplicating/contradicting `docs/github-action.md`?
       - COMPLETENESS: Both YAML snippets syntactically valid and present?
     - Severity rubric: CRITICAL / HIGH / MEDIUM / LOW
     - Required output: ONLY the findings table below (markdown), no prose

   **Subagent findings (fresh general-purpose subagent, 2026-07-14):**
   | Severity | File:Line | Issue | Disposition |
   |----------|-----------|-------|-------------|
   | MEDIUM | ci-integration.md GitHub snippet | `security-events: write` required by `upload-sarif` was in prose only, not in the copy-pasteable fence | Fixed in 3.6 — snippet restructured into a self-contained job with a `permissions:` block (AC 04-01 Edge Case 1 requires this detail not be silently omitted) |

   No CRITICAL/HIGH. Accuracy confirmed: GitHub snippet correctly pipes `atcr review && atcr reconcile && atcr report --format=sarif` into `codeql-action/upload-sarif@v3`; GitLab snippet uses only native `artifacts:reports:sast` (no invented upload action); distinction from `atcr github` is explicit at the top and links to github-action.md.

   **Action Taken:** No CRITICAL/HIGH → proceed. The MEDIUM is an AC-required copy-paste-correctness detail → fixed inline in 3.6 (not deferred).

### 3.6 [x] **[Story 4: SARIF CI Integration Documentation - REFACTOR](plan/user-stories/04-sarif-ci-integration-docs.md)**
   1. Fix CRITICAL/HIGH issues from 3.5.A (if any) — none; fixed the MEDIUM copy-paste-correctness gap instead.
   2. Restructured the GitHub snippet into a self-contained job with a `permissions: security-events: write` block; re-ran `yamllint` (relaxed) + strict YAML parse (both snippets clean) and `markdown-link-check` (3 links OK).
   3. COMMIT: `refactor(docs): make SARIF GitHub snippet self-contained with security-events permission` (b6d664e7).
   **Duration:** 0.25 day

---

## Final Phase: Validation

### 4.1 [x] **Schema Conformance Validation**
   Validate `renderSarif` output against a local `internal/report/testdata/sarif-schema-2.1.0.json` fixture via `google/jsonschema-go` (`Schema.UnmarshalJSON` → `Resolve` → `Validate`), test-only — no production dependency added. Source the fixture from the canonical SARIF 2.1.0 schema — SchemaStore's `sarif-2.1.0-rtm.5.json` (`https://json.schemastore.org/sarif-2.1.0-rtm.5.json`), the variant GitHub Code Scanning validates against — not an arbitrary OASIS/csd01 copy. Its `$ref`s are internal (`#/definitions/...`), so `Resolve` needs only a `BaseURI` and no external `Loader`; `Schema.UnmarshalJSON` supports the schema's Draft-07 dialect (confirmed in `plan/documentation/schema-validation-with-jsonschema-go.md`). Add this as a test case in `internal/report/sarif_test.go`.
   **Files:** `internal/report/testdata/sarif-schema-2.1.0.json` (new), `internal/report/sarif_test.go` (extend) | **Duration:** 0.5 day

### 4.2 [x] **Cross-Cutting Regression**
   Run `go test ./...`, `golangci-lint run`, `go vet ./...` for the full project. Run `yamllint`/`markdown-link-check` on `docs/ci-integration.md`.
   Results (2026-07-14): `go test ./...` → ALL PASS; `golangci-lint run` → 0 issues; `go vet ./...` → clean; `yamllint` (both snippets) → clean; `markdown-link-check` → 3 links OK. `internal/report` coverage 97.6%.

### Validation Checklist
- [x] All tests passing (T3) — `go test ./...` ALL PASS
- [x] Coverage meets threshold (≥80%) — `internal/report` 97.6%
- [x] Lint/format clean (`golangci-lint run`) — 0 issues
- [x] Build succeeds (`go build ./...`) — OK

### 4.3 [x] **Manual Smoke Test** — PASSED (2026-07-14)
   Uploaded real `atcr report --format=sarif` output to `github.com/samestrin/scratch`'s Code Scanning via `POST /code-scanning/sarifs`. Processing completed with no errors; both alerts rendered with `tool.driver.name=atcr`:
   - Alert #1 [security/error] → `vuln.go:8` — line-level anchoring confirmed.
   - Alert #2 [maintainability/warning] → `config.go:1` (col 1-1) — **file-level `Line<=0` → `1,1,1,1` fallback region renders in the Security tab** (plan.md's primary AC1 risk mitigation, confirmed live).
   Severity→level mapping (HIGH→error, MEDIUM→warning) and `rules[]`/`ruleId` category linkage both verified live. URLs: security/code-scanning/1 and /2 on samestrin/scratch.
   Note: both findings carried non-empty `File`/`Category`, so the degenerate cases behind TD-001 (empty `File`) and TD-002 (empty `Category`) remain live-untested — they stay as documented tech debt.

### Optional: Targeted Mutation Testing
Mutation tool detection: `UNAVAILABLE` in this environment — skip. If a mutation tool becomes available later, target only `internal/report/sarif.go` (changed file), not the full codebase.
**WARNING:** Do NOT run full codebase mutation - it can take hours. Target specific files.

### Drift Analysis
Compare final implementation against `plan/original-requirements.md`:
- [x] `atcr report --format=sarif` produces valid SARIF JSON — confirmed via Phase 1/4 (renderSarif + SARIF 2.1.0 schema conformance test).
- [x] SARIF output correctly maps ATCR severities to SARIF levels — confirmed via Phase 2 (`sarifLevel`, sole rubric site).
- [x] File paths and line numbers correctly anchor to the git diff — confirmed via Phase 2 (`sarifLocation`, Line>0 real-line, Line<=0 → 1,1,1,1 fallback).
- [x] Documentation example exists for GitHub Code Scanning upload and GitLab CI SAST-widget equivalent — confirmed via Phase 3 (`docs/ci-integration.md`).
- [x] No scope creep into `atcr github`'s already-shipped direct-API PR check/inline-comment flow — the SARIF doc explicitly distinguishes the two paths; no `cmd/atcr/github.go` or `action.yml` changes.
