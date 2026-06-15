# Code Review Report: 2.2_code_review_fanout_hardening

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 7 / 7
- **Approval Status:** Approved
- **Review Date:** June 14, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests
- **Note:** Post-merge verification review (epic merged in commit `6c0e2f0`, archived to `completed/`). The diff under review is the epic's merge commit.

## 2. Checklist Changes Applied

All 7 acceptance criteria verified against the merged implementation:

- **AC1** – Fan-out stamps registry agent key onto REVIEWER column
  - Evidence: `internal/fanout/artifacts.go:148-150`, `internal/stream/parser.go:101-109`
- **AC2** – Fan-out reads min_severity and drops findings below threshold
  - Evidence: `internal/fanout/postprocess.go:33-46`, `internal/fanout/review.go:538`
- **AC3** – Fan-out reads max_findings and truncates output
  - Evidence: `internal/fanout/postprocess.go:49-56`
- **AC4** – Fan-out logs warnings on drop/truncate
  - Evidence: `internal/fanout/postprocess.go:43,55`
- **AC5** – Registry parser accepts scope/min_severity/max_findings
  - Evidence: `internal/registry/config.go:108-112,295-305,344-347`
- **AC6** – Reconcile attributes findings to registry agent name
  - Evidence: `internal/stream/parser.go:206`
- **AC7** – Example config with scope/min_severity/max_findings
  - Evidence: `docs/registry.md:241-247`

## 3. Evidence Map

- **REVIEWER hardening (AC1/AC6):** `ParseModelOutput` reads exactly 7 persona columns; any 8th+ field is folded into EVIDENCE (`parser.go:101-109`), making model self-attribution impossible. The engine stamps `findings[i].Reviewer = r.Agent` after parsing (`artifacts.go:149-150`), so attribution is intact before any enforcement runs.
- **min_severity / max_findings enforcement (AC2/AC3/AC4):** `enforceConstraints` applies the severity floor first, then a stable severity-sorted truncation, logging dropped/truncated counts to stderr. Order guarantees the cap can never re-admit a sub-floor finding. Counters persist to `status.json` (`DroppedByMinSeverity` / `TruncatedByMaxFindings`).
- **Registry schema (AC5):** `AgentConfig` gains `Scope []string`, `MinSeverity string`, `MaxFindings *int`, all optional/backward-compatible, validated at load and normalized in `applyDefaults`.
- **scope soft injection:** `payload.ScopeFocus` appends a non-binding "Review Focus" hint to the persona prompt; never hard-drops out-of-category findings (matches the epic clarification: scope is soft-only).

## 4. Remaining Unchecked Items

No remaining unchecked items - all 7 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria implemented with cited evidence and backed by dedicated tests (`postprocess_test.go`, `scope_inject_test.go`, `config_review_constraints_test.go`, `scope_focus_test.go`). Full suite passes; quality gates green. Adversarial findings are all MEDIUM/LOW hardening items, none blocking.

## 6. Coverage Analysis
- **Coverage:** 87.9%
- **Baseline:** 80%
- **Delta:** ↑7.9%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 11
- **Issues Found:** 14 (Critical: 0, High: 0, Medium: 4, Low: 10)
- **Mode:** Discovery-only (no sprint-design.md risk profile)

### Issues by Severity

**MEDIUM**
- `internal/fanout/postprocess.go:53` — `enforceConstraints` has no self-defense against `max_findings <= 0`; a direct caller would silently drop ALL findings as "truncated to 0". (error-handling)
- `internal/fanout/postprocess.go:17` — severity-rank rubric duplicated across 4+ packages with casing-normalization divergence (fan-out uppercases at lookup, reconcile trusts raw). (maintainability)
- `internal/fanout/review.go:600` — a fallback-only agent's own scope/min_severity/max_findings are silently discarded (constraints follow the slot); deliberate but untested. (testing)
- `internal/fanout/postprocess_test.go:60` — missing boundary (`max_findings == len`), unknown-severity, and mixed-case-severity finding test cases. (testing)

**LOW**
- `internal/fanout/postprocess.go:35` — unknown `min_severity` floor fails open (silently no-ops). (error-handling)
- `internal/fanout/postprocess.go:37` — in-place slice mutation (`findings[:0]` + sort) is a latent aliasing trap. (correctness)
- `internal/fanout/engine.go:284` — serial-lane cancellation Result omits MinSeverity/MaxFindings (inconsistent with other paths; benign). (correctness)
- `internal/payload/scope.go:22` — unescaped scope concatenation into prompt, no length cap, undocumented trust boundary (trusted config). (security)
- `internal/registry/config.go:283` — no upper bound on `max_findings`, inconsistent with other bounded fields. (correctness)
- `internal/registry/config.go:317` — scope entries not trimmed/stored, asymmetric with min_severity normalization. (correctness)
- `internal/registry/config.go:344` — `AgentsByRole` Scope aliasing contract documented but untested. (testing)
- `internal/payload/scope_focus_test.go:16` — weak assertions (soft-wording unchecked, fragile blank-skip). (testing)
- `internal/fanout/scope_inject_test.go:28` — unscoped `Invocation.Prompt` not asserted. (testing)
- `internal/registry/config_review_constraints_test.go:75` — no whitespace-only scope entry test. (testing)

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/2.2_code_review_fanout_hardening.md` to merge these 14 findings into the TD README with reviewer attribution.
- Highest-value items to address: the `max_findings <= 0` self-defense (postprocess.go:53) and the duplicated severity-rank rubric (postprocess.go:17) — both MEDIUM, both with broad blast radius if a future caller or severity-level change lands.

---
*Generated by /execute-code-review on June 14, 2026 09:31:12PM*
