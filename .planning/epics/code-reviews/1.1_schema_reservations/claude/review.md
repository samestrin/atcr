# Code Review Stream - 1.1_schema_reservations (Epic)

**Started:** June 12, 2026 12:36:32PM
**Mode:** [Acceptance Criteria (Success Criteria)] [+ Adversarial Review] [+ Tests]
**Scope:** epic merge commit `8377a2b` (source/doc surface only; planning-doc churn excluded)
**Note:** epic uses `## Success Criteria` as its acceptance-criteria source (user-confirmed substitution).

---

## Acceptance Criteria Findings

### Criterion: Registry loader parses and validates the reserved fields (type errors fail at load) but no v1 code path acts on them
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/config.go:62-65` (reserved `Tools`/`MaxTurns`/`ToolBudgetBytes`/`Role` fields), `internal/registry/config.go:187-196` (`validate()`: `roleValid`, `max_turns > 0`, `tool_budget_bytes >= 0`), `internal/registry/config_test.go` (+101 lines)
- **Notes:** Fields parse under the strict `KnownFields(true)` decoder; bad values fail the load (tested). `applyDefaults()` untouched, so fields stay zero/unset — genuinely inert in 1.x. Adversarial trace confirmed no v1 code path reads them.

### Criterion: findings.json schema documents the optional `verification` block; `atcr report` renders findings identically with or without it
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/emit.go:25-32` (`Verification` struct), `internal/reconcile/emit.go:52` (`JSONFinding.Verification *Verification` omitempty), `internal/report/render_test.go` (`TestRender_IdenticalWithAndWithoutVerification`), `docs/findings-format.md`
- **Notes:** Pointer + omitempty ⇒ absent from 1.x JSON; the render test asserts markdown/checklist output is byte-identical with and without the block and only adds the round-tripped key to JSON.

### Criterion: manifest.json carries `stages: ["review"]`
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/manifest.go:29-32` (`Stages []string`), `internal/fanout/review.go:195` (`Stages: []string{"review"}`)
- **Notes:** Set at the single construction site in `PrepareReview`; emitted non-omitted and preserved across the `ExecuteReview` finalization rewrite.

### Criterion: Format docs list all reserved fields with their owning future epic
- **Verdict:** VERIFIED ✅
- **Evidence:** `docs/registry.md` reserved-fields table (`tools`/`max_turns`/`tool_budget_bytes`/`role` with planned defaults + Stage 2/3/4 owners), `docs/findings-format.md` (`verification` → Epic 3.0; companion-artifacts table: `stages` → Epics 3.0–5.0, status `turns`/`tool_calls`/`tool_bytes` → Epic 2.0)
- **Notes:** Docs explicitly frame every reserved field as "parsed/validated, not yet acted on" with its owning epic.

### Criterion: No new dependencies; all tests green
- **Verdict:** VERIFIED ✅
- **Evidence:** `go.mod`/`go.sum` unchanged in `8377a2b`; full suite green, total coverage 87.2%
- **Notes:** Schema-reservation work used only stdlib + existing types.

---

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (Full hostile review)
**Files Reviewed:** 5 source files (`config.go`, `manifest.go`, `review.go`, `status.go`, `emit.go`)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic has no sprint-design.md)

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 6

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 6

All six are future-stage reserved-field hygiene nits (no 1.x correctness defect): MaxTurns missing an upper bound; ToolBudgetBytes 0-sentinel semantics undocumented; Tools value-bool vs the pointer "unset-vs-explicit" rationale; an emit.go comment citing a nonexistent `reconcile/load.go`; Verification.Verdict lacking read-side enum validation; `stages` omitempty resting the 1.x guarantee on the writer. The adversarial agent confirmed the `*int`→`*int64` and YAML-omitempty concerns were already resolved in follow-up commits `376041c`/`2ace208`.
