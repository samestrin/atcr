# User Story 3: Confidence v2 & Re-emit

**Plan:** [3.0: Adversarial Verification](../plan.md)

## User Story

**As a** downstream consumer of reconciled findings (report rendering, `--fail-on` gate, human reviewers)
**I want** skeptic verdicts to be integrated into reconciled artifacts — confidence tiers recomputed per the v2 model, `verification.json` written, `findings.json` re-emitted with verification blocks, and `manifest.json` updated with the `verify` stage
**So that** the final report reflects a two-axis confidence model (VERIFIED > HIGH > MEDIUM > LOW), refuted findings are demoted but retained for audit, and gate counts exclude refuted findings — giving operators an accurate, verifiable picture of finding trustworthiness

## Story Context

- **Background:** Stories 1 and 2 deliver the selection and invocation infrastructure. Story 1 provides `SelectEligibleSkeptics(finding, n)` which returns eligible skeptics per finding. Story 2 provides `invokeSkeptic` which drives each skeptic through the tool loop and produces a `*reconcile.Verification` with verdict (`confirmed` | `refuted` | `unverifiable`) and reasoning. This story consumes those `Verification` values and integrates them into the existing reconciled artifacts. The `Verification` struct is already reserved at `internal/reconcile/emit.go:36` and embedded as `*Verification` on `JSONFinding` at line 59 (omitempty). The `manifest.json` stages field is written via `payload.WriteManifest` at `internal/payload/manifest.go:86`. The gate counter `CountAtOrAbove` lives at `internal/reconcile/gate.go:57` and operates on `[]reconcile.Merged`. What is missing is the integration layer: confidence recomputation, verification artifact emission, findings re-emission with verification blocks, manifest stage update, and summary verdict counts.
- **Assumptions:**
  - Stories 1 and 2 are complete and provide `SelectEligibleSkeptics`, `invokeSkeptic`, and `aggregateVerdicts`; the input to this story is a final per-finding `*reconcile.Verification`.
  - `reconciled/findings.json` exists and is loadable via `ReadReconciledFindings` at `internal/reconcile/emit.go:145`.
  - `manifest.json` exists in the review directory and is loadable as `payload.Manifest`.
  - `summary.json` exists and is loadable; this story adds a `verdictCounts` field.
- **Constraints:**
  - Refuted findings must **never** be deleted — they are demoted to LOW confidence and retained in `findings.json` with a collapsed "Refuted" section in the report (report rendering is a later story; this story ensures the data is present).
  - `--fail-on` gate counts must exclude refuted findings (this story updates the gate counter; CLI integration is a later story).
  - All artifact writes must be atomic (write to temp file, then rename) to prevent partial writes on crash.
  - All new code must be unit-tested with table-driven tests matching existing patterns.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Story 1 (Skeptic Selection & Role Plumbing) — needs `SelectEligibleSkeptics`; Story 2 (Skeptic Invocation & Verdict Parsing) — needs `invokeSkeptic` to produce `*Verification` values |

## Success Criteria (SMART Format)

- **Specific:** (1) `confidenceV2(v1Confidence string, verdict string) string` recomputes confidence per the v2 model: `confirmed` → `VERIFIED`, `refuted` → `LOW`, `unverifiable` → retains v1 confidence. (2) `WriteVerification(reviewDir string, results []VerificationResult)` writes `reconciled/verification.json` with the schema defined in verification-pipeline.md. (3) `ReEmitFindings(reviewDir string, verdicts map[FindingKey]*reconcile.Verification)` loads `reconciled/findings.json`, populates each finding's `Verification` field, recomputes confidence, and writes the updated file atomically. (4) `UpdateManifestStage(reviewDir string)` loads `manifest.json`, appends `"verify"` to stages (if not already present), and writes atomically. (5) `UpdateSummaryVerdicts(reviewDir string, counts VerdictCounts)` loads `summary.json`, adds/updates the `verdictCounts` field, and writes atomically.
- **Measurable:** (1) `go test ./internal/verify/... ./internal/reconcile/...` passes with >= 95% coverage on new code paths (`confidence_v2.go`, `emit_verification.go`). (2) Confidence v2 tests cover all 4 verdict cases (confirmed, refuted, unverifiable, empty/no-verdict) × 3 v1 confidence levels (HIGH, MEDIUM, LOW). (3) Round-trip test: write verification.json, read it back, verify schema matches. (4) Re-emit test: load findings.json, apply verdicts, verify confidence recomputation and verification block population. (5) Manifest test: load manifest, add verify stage, verify stages array contains "verify" exactly once (idempotent). (6) `go vet` and existing CI checks remain clean.
- **Achievable:** This is integration and emission work. The `Verification` struct is reserved, `JSONFinding.Verification` is embedded, `WriteManifest` exists, and `CountAtOrAbove` is the gate counter. No new infrastructure is needed — only functions that consume `*Verification` values and update artifacts.
- **Relevant:** This is the output stage of Epic 3.0. Without confidence v2 and artifact re-emission, skeptic verdicts have no effect — they sit in memory and are lost. This story makes verification durable: the reconciled artifacts reflect the v2 confidence model, and downstream consumers (report, gate, MCP) can read them.
- **Time-bound:** Expected to complete within weeks 2–3 of the 3–4 week epic (immediately after Story 2).

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-confidence-v2-recomputation.md) | Confidence V2 Recomputation | Unit |
| [03-02](../acceptance-criteria/03-02-verification-json-emission.md) | Verification JSON Emission | Unit |
| [03-03](../acceptance-criteria/03-03-findings-re-emit.md) | Findings Re-Emit with Verification Blocks | Unit |
| [03-04](../acceptance-criteria/03-04-manifest-summary-updates.md) | Manifest Stage & Summary Verdict Updates | Unit |
| [03-05](../acceptance-criteria/03-05-gate-excludes-refuted.md) | Gate Counter Excludes Refuted Findings | Unit |

## Original Criteria Overview

1. `confidenceV2` recomputes confidence per the v2 model: `confirmed` → `VERIFIED`, `refuted` → `LOW`, `unverifiable` → retains v1 confidence. The function is pure and unit-tested for all verdict × v1-confidence combinations.
2. `WriteVerification` writes `reconciled/verification.json` with the schema defined in verification-pipeline.md: `verifiedAt`, `minSeverity`, `fresh`, `thorough`, `findings[]` (with file, line, problem, verdict, skeptic, model, reasoning, durationMs, trippedBudgets), and `verdictCounts`.
3. `ReEmitFindings` loads `reconciled/findings.json`, populates each finding's `Verification` field from the verdict map, recomputes confidence via `confidenceV2`, and writes the updated file atomically. Refuted findings are demoted to LOW but retained in the file.
4. `UpdateManifestStage` loads `manifest.json`, appends `"verify"` to stages (idempotent — if "verify" is already present, do not duplicate), and writes atomically via `payload.WriteManifest`.
5. `UpdateSummaryVerdicts` loads `summary.json`, adds/updates the `verdictCounts` field (confirmed/refuted/unverifiable breakdown), and writes atomically.
6. The `--fail-on` gate counter (`CountAtOrAbove` at `internal/reconcile/gate.go:57`) is updated to `CountAtOrAbove(findings []Merged, threshold string, requireVerified bool) int`. It skips findings whose `Verification.Verdict == "refuted"` and, when `requireVerified` is true, skips findings whose `Confidence` is not `"VERIFIED"`. The CLI/MCP flag plumbing for `requireVerified` is added in Story 5.
7. All artifact writes (verification.json, findings.json, manifest.json, summary.json) are atomic: write to temp file in the same directory, then rename. This prevents partial writes on crash.
8. Table-driven unit tests cover: confidence v2 recomputation (all verdict × v1-confidence combos), verification.json round-trip, findings re-emit with verification block, manifest stage idempotency, summary verdict counts, gate exclusion of refuted findings.

## Technical Considerations

- **Implementation Notes:**
  - **Confidence v2 (`internal/verify/confidence_v2.go`):** `confidenceV2(v1Confidence string, verdict string) string` is a pure function implementing the v2 confidence model. The mapping is: `verdict="confirmed"` → `"VERIFIED"`, `verdict="refuted"` → `"LOW"`, `verdict="unverifiable"` → `v1Confidence` (unchanged), `verdict=""` or unknown → `v1Confidence` (no verdict yet). The function does not validate v1Confidence — it assumes the input is a valid v1 confidence (HIGH/MEDIUM/LOW). The constant `"VERIFIED"` is new in Epic 3.0 and should be defined as `const ConfidenceVerified = "VERIFIED"` in this file (or in `internal/reconcile/emit.go` alongside existing confidence constants if they exist).
  - **Verification artifact emission (`internal/verify/emit_verification.go`):** `WriteVerification(reviewDir string, results []VerificationResult) error` writes `reconciled/verification.json`. The `VerificationResult` struct is defined locally in this file and contains: `File string`, `Line int`, `Problem string`, `Verdict string`, `Skeptic string`, `Model string`, `Reasoning string`, `DurationMs int64`, `TrippedBudgets []string`. The function: (1) loads the existing manifest or config to extract `minSeverity`, `fresh`, `thorough` flags (passed as parameters or loaded from the verify config), (2) computes `verdictCounts` by iterating results, (3) constructs the top-level struct with `verifiedAt: time.Now().UTC()`, (4) marshals to JSON with indentation, (5) writes atomically to `reconciled/verification.json` using a temp file + rename pattern.
  - **Findings re-emission (`internal/verify/emit_findings.go`):** `ReEmitFindings(reviewDir string, verdicts map[FindingKey]*reconcile.Verification) error` loads `reconciled/findings.json` via `ReadReconciledFindings`, iterates findings, looks up each finding's verdict by a composite key (file + line + problem hash), populates the `Verification` field, recomputes confidence via `confidenceV2`, and writes the updated findings atomically. The `FindingKey` struct is defined locally: `File string`, `Line int`, `Problem string` (or a hash of the problem text if it is long). The function does not modify findings that have no verdict (they retain their v1 confidence and `Verification` remains nil).
  - **Manifest stage update (`internal/verify/emit_manifest.go`):** `UpdateManifestStage(reviewDir string) error` loads `manifest.json` via `os.ReadFile` + `json.Unmarshal` into `payload.Manifest`, checks if "verify" is already in `Stages` (idempotent), appends "verify" if missing, and writes atomically via `fanout.WriteManifest(reviewDir, &m)` (which delegates to `payload.WriteManifest` at `internal/payload/manifest.go:86`). The function returns nil if "verify" is already present (no-op).
  - **Summary verdict counts (`internal/verify/emit_summary.go`):** `UpdateSummaryVerdicts(reviewDir string, counts VerdictCounts) error` loads `summary.json` via `os.ReadFile` + `json.Unmarshal` into a local struct that embeds the existing summary fields plus a new `VerdictCounts VerdictCounts` field (with `json:"verdictCounts,omitempty"`). The `VerdictCounts` struct is defined locally: `Confirmed int`, `Refuted int`, `Unverifiable int`. The function updates the `VerdictCounts` field and writes atomically.
  - **Gate counter update (`internal/reconcile/gate.go`):** Update `CountAtOrAbove` to `CountAtOrAbove(findings []Merged, threshold string, requireVerified bool) int`. It skips findings where `finding.Verification != nil && finding.Verification.Verdict == "refuted"` and, when `requireVerified` is true, skips findings whose `Confidence` is not `"VERIFIED"`. All existing call sites pass `false` for `requireVerified` (Story 5 adds the user-facing flag and wires it through).
  - **Atomic writes:** All artifact writes follow the pattern: (1) create temp file in the same directory as the target (using `os.CreateTemp(reviewDir, ".tmp-*")`), (2) write content to temp file, (3) close temp file, (4) rename temp file to target (using `os.Rename`). This ensures atomicity on POSIX systems. A helper function `atomicWrite(path string, data []byte) error` can be extracted to avoid repetition.
  - **Integration with Story 2:** Story 2 produces a final `*reconcile.Verification` per finding (after vote aggregation). This story consumes that map. The interface between Story 2 and Story 3 is a `map[FindingKey]*reconcile.Verification` passed to `ReEmitFindings`. This story defines `FindingKey` and expects Story 2 to populate the map.
- **Integration Points:**
  - `internal/reconcile/emit.go` — `Verification` struct (line 36), `JSONFinding` (line 59), `ReadReconciledFindings` (line 145): input and output shapes.
  - `internal/reconcile/gate.go` — `CountAtOrAbove` (line 57): gate counter modified to exclude refuted findings and accept a `requireVerified` parameter.
  - `internal/payload/manifest.go` — `WriteManifest` (line 86): manifest writer used by `UpdateManifestStage`.
  - `internal/fanout/reviewdir.go` — `WriteManifest` wrapper: delegates to `payload.WriteManifest`.
  - `internal/verify/select.go` (Story 1) — `SelectEligibleSkeptics`: provides skeptic candidates (used by Story 2, indirectly by this story).
  - `internal/verify/invoke.go` (Story 2) — `invokeSkeptic`: produces `*Verification` values consumed by this story.
  - `reconciled/verification.json` — new artifact written by this story.
  - `reconciled/findings.json` — re-emitted by this story with verification blocks.
  - `manifest.json` — updated by this story with "verify" stage.
  - `summary.json` — updated by this story with verdict counts.
- **Data Requirements:**
  - **`reconciled/verification.json` schema:** Defined in verification-pipeline.md. Top-level fields: `verifiedAt` (ISO 8601 timestamp), `minSeverity` (string), `fresh` (bool), `thorough` (bool), `findings` (array of per-finding verdict objects), `verdictCounts` (object with confirmed/refuted/unverifiable counts). Per-finding fields: `file`, `line`, `problem`, `verdict`, `skeptic`, `model`, `reasoning`, `durationMs`, `trippedBudgets`.
  - **`reconciled/findings.json` schema:** Existing schema extended with `verification` block per finding (already defined as `*Verification` on `JSONFinding`). Confidence field is updated to v2 confidence.
  - **`manifest.json` schema:** Existing schema extended with "verify" in `stages` array.
  - **`summary.json` schema:** Existing schema extended with `verdictCounts` object (confirmed/refuted/unverifiable).
  - **`FindingKey` struct:** Defined in `internal/verify/emit_findings.go`. Fields: `File string`, `Line int`, `Problem string`. Used to match verdicts to findings.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Confidence v2 mapping is incorrect (e.g., refuted findings not demoted to LOW) | High — gate counts include refuted findings, report shows false confidence | Unit test all verdict × v1-confidence combinations. Add an integration test that writes a finding with v1=HIGH, applies verdict=refuted, and verifies the re-emitted finding has confidence=LOW. |
| Atomic write fails on non-POSIX systems (Windows) | Medium — partial writes on crash | Use `os.Rename` which is atomic on POSIX. Document that atcr is POSIX-only (Linux, macOS). If Windows support is needed in the future, use `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING`. |
| `FindingKey` collision — two findings with the same file, line, and problem text | Low — verdict applied to wrong finding | Use a composite key (file + line + problem hash) to reduce collision risk. If collisions occur, the reconciler should have already deduplicated findings; document this assumption. |
| `manifest.json` "verify" stage duplicated on re-run | Medium — manifest shows "verify" multiple times | `UpdateManifestStage` checks if "verify" is already in `Stages` before appending (idempotent). Unit test with manifest that already contains "verify". |
| Gate counter excludes refuted findings incorrectly (e.g., all LOW findings excluded, not just refuted) | High — gate misses real findings | The filter condition is `verdict="refuted"` (or `confidence="LOW"` AND `Verification.Verdict="refuted"`), not just `confidence="LOW"`. Unit test with findings: (1) v1=LOW, no verdict → should count, (2) v1=HIGH, verdict=refuted → should NOT count, (3) v1=LOW, verdict=unverifiable → should count. |
| Summary.json schema change breaks existing consumers | Medium — downstream tools fail to parse summary | The `verdictCounts` field is added with `omitempty`, so existing summaries without the field remain valid. Consumers that do not expect the field will ignore it (standard JSON behavior). Document the schema change in release notes. |
| Verification.json round-trip test fails due to floating-point precision in `durationMs` | Low — test flakes | `durationMs` is an integer (int64), not a float. Use `int64` consistently in the struct and test assertions. |
| Import cycle between `internal/verify` and `internal/reconcile` | Medium — build failure | `verify` imports `reconcile` (for `JSONFinding`, `Verification`, `ReadReconciledFindings`), but `reconcile` must not import `verify`. The gate counter update is in `reconcile/gate.go` and does not call `verify` functions. Verify with `go build ./...` after initial scaffolding. |

---

**Created:** June 14, 2026 09:06:20AM
**Status:** AC Generated - Ready for Implementation
