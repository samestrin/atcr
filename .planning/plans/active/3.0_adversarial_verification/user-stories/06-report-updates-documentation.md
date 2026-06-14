# User Story 6: Report Updates & Documentation

**Plan:** [3.0: Adversarial Verification](../plan.md)

## User Story

**As a** developer reviewing an atcr report or learning how adversarial verification works
**I want** the rendered report to clearly display skeptic verdicts (a dedicated Skeptic section, collapsed Refuted findings at the bottom, v2 confidence tiers), the documentation to explain verification mechanics and the skeptic role, and a fixture corpus with planted true/false findings to exist for end-to-end testing
**So that** I can trust what the report shows about verification, understand the v2 confidence model and gate semantics from the docs, and validate the pipeline against known-good and known-bad findings

## Story Context

- **Background:** Stories 1–5 deliver the verification pipeline end-to-end: skeptic selection, invocation, verdict parsing, confidence v2 re-emission, CLI/MCP integration, and gate semantics. The report renderer (`internal/report/`) currently formats findings using v1 confidence tiers (HIGH/MEDIUM/LOW) with no awareness of verification. The documentation set (registry.md, findings-format.md) does not mention skeptics or the verify stage. No fixture corpus exists for adversarial verification testing. This story closes the visibility and trust gap: developers reading the report must see skeptic verdicts, understand v2 confidence, and have access to documentation and test fixtures that make the system verifiable.
- **Assumptions:**
  - Stories 1–5 are complete. `reconciled/findings.json` now carries per-finding `Verification` blocks with v2 confidence. `reconciled/verification.json` exists with per-finding verdicts. `summary.json` contains `verdictCounts`.
  - The report renderer (`internal/report/`) already has a `Render()` function that produces markdown from `[]reconcile.Merged` or equivalent. The golden file pattern is in place (`internal/report/testdata/`).
  - Existing documentation lives in the project's docs directory; `registry.md` documents agent configuration including the `role` field (validated but previously undocumented for `skeptic`).
- **Constraints:**
  - Report changes must not break existing v1 report consumers — findings without a `Verification` block (pre-Epic 3.0 runs) must render identically to current output.
  - Documentation files are markdown; no build-time generation or tooling required.
  - Fixture corpus files must be self-contained (no external network calls, no real LLM invocations).
  - All new report rendering code must be unit-tested with golden files matching existing patterns.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | M |
| **Dependencies** | Story 3 (Confidence v2 & Re-emit) — needs re-emitted findings.json with verification blocks; Story 5 (Gate Semantics) — needs final gate behavior for documentation accuracy |

## Success Criteria (SMART Format)

- **Specific:** (1) Report renderer gains a **Skeptic** section in the panel table showing per-finding skeptic name, model, verdict, and reasoning (one row per verified finding). (2) Report renderer gains a collapsed **Refuted** section at the bottom listing refuted findings with their skeptic reasoning, hidden behind a `<details>` / `<summary>` toggle (or markdown equivalent). (3) Confidence tiers in the report reflect v2 ordering: VERIFIED > HIGH > MEDIUM > LOW, with VERIFIED rendered distinctly (e.g., badge or label). (4) `docs/verification.md` is created covering: verification mechanics (skeptic selection, different-model rule, verdict envelope), confidence v2 model, gate semantics (`--fail-on`, `--require-verified`). (5) `docs/registry.md` is updated with the `role: skeptic` configuration entry. (6) Fixture corpus at `internal/verify/testdata/` contains `true-finding.json`, `false-finding.json`, and `malformed-response.txt`.
- **Measurable:** (1) Golden file test `TestRenderWithVerification` passes: input `findings-with-verification.json` renders to `report.md` matching the golden file that includes Skeptic section, Refuted section, and VERIFIED tier. (2) Backward compatibility test: `TestRenderV1Findings` passes — input without verification blocks produces identical output to the pre-Epic 3.0 golden file. (3) `go test ./internal/report/...` passes with >= 90% coverage on new rendering code paths. (4) `verification.md` contains sections: Overview, Skeptic Selection, Verdict Envelope, Confidence v2, Gate Semantics, Cost Controls. (5) `registry.md` contains a `role: skeptic` subsection with example YAML. (6) Fixture corpus: `true-finding.json` is parseable as `reconcile.JSONFinding`, `false-finding.json` is parseable as `reconcile.JSONFinding`, `malformed-response.txt` is a non-empty text file. (7) `go vet` and existing CI checks remain clean.
- **Achievable:** This is rendering, documentation, and fixture work. The report renderer already exists; this story adds sections and tier labels. Documentation is additive. Fixtures are static JSON/text files. No new infrastructure is needed.
- **Relevant:** This is the visibility layer of Epic 3.0. Without report updates, developers cannot see skeptic verdicts in the output — the entire verification pipeline is invisible. Without documentation, operators cannot configure skeptics or understand the v2 confidence model. Without fixtures, end-to-end testing (Story 2 verdict parsing, Story 7 integration tests) has no grounded test data.
- **Time-bound:** Expected to complete within weeks 3–4 of the 3–4 week epic (the final story, after all pipeline infrastructure is in place).

## Acceptance Criteria Overview

1. Report renderer displays a Skeptic section in the panel table for each verified finding, showing skeptic name, model, verdict, and reasoning.
2. Report renderer displays a collapsed Refuted section at the bottom of the report, listing refuted findings with skeptic reasoning, hidden behind a collapsible toggle.
3. Confidence tiers in the report reflect v2 ordering (VERIFIED > HIGH > MEDIUM > LOW), with VERIFIED rendered distinctly from v1 tiers.
4. `docs/verification.md` exists and covers: verification mechanics (skeptic selection, different-model rule, verdict envelope), confidence v2 model (tier table, transition rules), gate semantics (`--fail-on` excludes refuted, `--require-verified` counts only VERIFIED), and cost controls (`min_severity`, budgets, `--fresh`).
5. `docs/registry.md` is updated with a `role: skeptic` configuration subsection, including example YAML and the different-model rule explanation.
6. Fixture corpus at `internal/verify/testdata/` contains `true-finding.json` (planted true finding, parseable as `reconcile.JSONFinding`), `false-finding.json` (planted false finding, parseable as `reconcile.JSONFinding`), and `malformed-response.txt` (non-parseable skeptic response text).
7. Golden file test `TestRenderWithVerification` passes with a `findings-with-verification.json` input fixture and updated `report.md` golden file.
8. Backward compatibility test `TestRenderV1Findings` passes — findings without verification blocks produce identical output to pre-Epic 3.0 golden files.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/3.0_adversarial_verification/`_

## Technical Considerations

- **Implementation Notes:**
  - **Skeptic section rendering (`internal/report/render.go`):** Add a new section to the panel table (or a separate section adjacent to it) that renders per-finding skeptic information. For each finding with a non-nil `Verification` block: display skeptic name (`Verification.Skeptic`), model (looked up from the registry or included in the Verification struct if extended), verdict (`Verification.Verdict`), and reasoning (`Verification.Notes`). The section is only rendered for findings that have been verified; unverified findings are unchanged. The section header is "Skeptic" or "Verification" and appears in the panel table or as a labeled block below the finding description.
  - **Collapsed Refuted section (`internal/report/render.go`):** After the main findings table, render a collapsed section titled "Refuted Findings" containing all findings where `Verification.Verdict == "refuted"`. Each refuted finding entry shows: file, line, problem text, original confidence (before demotion), skeptic name, and reasoning. The collapsible toggle uses HTML `<details><summary>Refuted Findings (N)</summary>...</details>` (or markdown-compatible equivalent). If no findings are refuted, the section is omitted entirely.
  - **Confidence v2 tier display:** The report currently renders confidence as a label (e.g., `[HIGH]`, `[MEDIUM]`, `[LOW]`). Add `[VERIFIED]` as a new tier label, rendered distinctly — for example, with a different marker or annotation (e.g., `[VERIFIED ✓]` or bold). The tier ordering in any summary/count display is: VERIFIED, HIGH, MEDIUM, LOW (highest to lowest).
  - **Backward compatibility:** The renderer must check `if finding.Verification != nil` before rendering skeptic or refuted sections. Findings without verification (from pre-Epic 3.0 runs) render identically to current output. The golden file test `TestRenderV1Findings` uses the existing `testdata/findings.json` (no verification blocks) and compares against the unchanged golden `report.md`.
  - **Golden file update (`internal/report/testdata/`):** Create `findings-with-verification.json` — a copy of the existing `findings.json` with added verification blocks covering all verdict types (confirmed, refuted, unverifiable). Update `report.md` golden file to include the Skeptic section, Refuted section, and VERIFIED tier. Keep the existing `findings.json` and pre-Epic 3.0 `report.md` as `report-v1.md` for backward compatibility testing, or use a separate test function.
  - **`docs/verification.md` structure:** Create a new documentation file with the following sections: (1) Overview — what verification does, when it runs (after reconcile). (2) Skeptic Selection — role-based filtering, different-model rule, `no_eligible_skeptic` fallback. (3) Verdict Envelope — confirmed/refuted/unverifiable, parsing, malformed output fallback. (4) Confidence v2 — tier table, transition rules, comparison to v1. (5) Gate Semantics — `--fail-on` excludes refuted, `--require-verified` counts only VERIFIED, interaction with v2 tiers. (6) Cost Controls — `verify.min_severity`, per-finding budgets, `--fresh` flag, `--thorough` majority voting. (7) Artifacts — verification.json schema, findings.json verification block, manifest stages, summary verdictCounts.
  - **`docs/registry.md` update:** Add a subsection under the agent configuration documentation for `role: skeptic`. Include: (1) YAML example showing a skeptic agent entry with `role: skeptic` and `model: claude-sonnet-4-6`. (2) Explanation of the different-model rule (skeptic cannot share a model with any reviewer credited on the finding). (3) Note that empty `role` defaults to `reviewer` for backward compatibility. (4) Reference to `docs/verification.md` for full mechanics.
  - **Fixture corpus (`internal/verify/testdata/`):** Create three files: (1) `true-finding.json` — a deliberately plausible and correct finding (e.g., "JWT signature not verified before claims are read" pointing to real code pattern). The JSON must be parseable as `reconcile.JSONFinding` with all required fields. (2) `false-finding.json` — a deliberately plausible but incorrect finding (e.g., "nil pointer dereference on line 42" where the code actually checks for nil). The JSON must be parseable as `reconcile.JSONFinding`. (3) `malformed-response.txt` — a text file containing a malformed skeptic response (invalid JSON, e.g., `{"verdict": "confirmed", "reasoning": "missing closing brace"`). Used by verdict parsing tests (Story 2) and end-to-end tests.
- **Integration Points:**
  - `internal/report/render.go` — existing `Render()` function modified to add Skeptic section, Refuted section, and VERIFIED tier.
  - `internal/report/testdata/` — golden files: new `findings-with-verification.json`, updated `report.md` (or new variant), backward compatibility golden file.
  - `internal/reconcile/emit.go` — `JSONFinding.Verification` (read by renderer), `Verification` struct fields.
  - `internal/registry/config.go` — `RoleSkeptic` constant (referenced in documentation).
  - `docs/verification.md` — new file, no existing integration.
  - `docs/registry.md` — existing file, additive update.
  - `internal/verify/testdata/` — new directory, fixture files.
- **Data Requirements:**
  - **`findings-with-verification.json` test fixture:** Contains 4+ findings covering all verdict types: (1) v1=HIGH, verdict=confirmed → VERIFIED, (2) v1=HIGH, verdict=refuted → LOW (refuted), (3) v1=MEDIUM, verdict=unverifiable → MEDIUM, (4) v1=LOW, no verdict → LOW. Each finding with a populated `verification` block.
  - **`true-finding.json` fixture:** A `reconcile.JSONFinding`-compatible JSON object describing a real code issue. Fields: file, line, problem, severity, reviewers, confidence.
  - **`false-finding.json` fixture:** A `reconcile.JSONFinding`-compatible JSON object describing a plausible but incorrect code issue. Same schema.
  - **`malformed-response.txt` fixture:** Plain text containing invalid JSON that resembles a skeptic response.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Report renderer changes break existing v1 report output | High — downstream consumers (CI comments, PR summaries) produce different output | Guard all new rendering behind `if finding.Verification != nil`. Add backward compatibility test `TestRenderV1Findings` that uses the existing fixture (no verification blocks) and asserts identical output. |
| Golden file cascade — updating `report.md` breaks other tests that depend on the same golden file | Medium — test failures in unrelated test functions | Check for other tests that read `testdata/report.md`. If found, update them or use a separate golden file name (e.g., `report-v2.md`) for the v2 variant. |
| `<details>` HTML tag not rendered correctly in all markdown consumers (e.g., GitHub, GitLab) | Low — Refuted section appears as raw HTML | `<details>/<summary>` is supported by GitHub markdown. Test rendering in a GitHub-style markdown preview. If unsupported, fall back to a plain section with a clear heading. |
| Fixture findings are not representative of real atcr output | Medium — end-to-end tests pass with fixtures but fail with real findings | Base fixture JSON on actual `reconcile.JSONFinding` schema. Validate fixtures parse correctly with `reconcile.ReadReconciledFindings` in a test. |
| `verification.md` duplicates content from `verification-pipeline.md` (planning docs) | Low — documentation drift | `verification-pipeline.md` is a planning document (`.planning/plans/active/...`). `docs/verification.md` is user-facing documentation. Content overlap is acceptable but the user-facing doc should be concise and reference the codebase, not planning artifacts. |
| Fixture corpus grows unbounded as new test scenarios are added | Low — maintenance burden | Keep fixtures minimal: one true finding, one false finding, one malformed response. Additional scenarios use inline test data in `_test.go` files, not new fixture files. |

---

**Created:** June 14, 2026 09:06:20AM
**Status:** Draft - Awaiting Acceptance Criteria
