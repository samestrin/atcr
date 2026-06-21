# Code Review Report: 6.0_cross_examination

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 21, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial + Tests)
- **Scope:** PR #64 (merge `9b8b034`) — 42 files, +3331/-43; new `internal/debate/` package

## 2. Acceptance Criteria (Success Criteria) Verification

| # | Criterion | Verdict | Key Evidence |
|---|-----------|---------|--------------|
| 1 | Severity disputes ≥2 tiers + ambiguous clusters route through debate; judge rulings replace severity-max and resolve merge/split | VERIFIED ✅ | `internal/debate/select.go:72-89`, `emit.go:110-112`, `protocol.go:236-247` |
| 2 | Three-distinct-models rule enforced across proposer/challenger/judge | VERIFIED ✅ | `internal/debate/cast.go:65-102,133-156` |
| 3 | Bounded protocol: hard 3-turn cap, per-item budget, `debate.max_items` priority ordering, overflow recorded (never silent) | VERIFIED ✅ | `protocol.go:56-78`, `select.go:81-116`, `emit.go:209-216` |
| 4 | Unattended CI run resolves ambiguous clusters without Skill involvement | VERIFIED ✅ | `debate.go:62-71`, `cmd/atcr/debate.go:38-68`, `mcp/handlers.go:597-624` |
| 5 | Transcripts replayable; rulings auditable in report.md | VERIFIED ✅ | `transcript.go:25-105`, `report/contested.go:53-87` |

## 3. Evidence Map

- **Trigger detection + selection** — `SelectItems` filters the rebuilt disagreement radar by enabled trigger kind (`severity_split`, `gray_zone`, `verification_disagreement`, all default-on), orders by severity-rank desc, applies the `max_items` cap, and records overflow. `internal/debate/select.go:72-116`
- **Distinct-model casting** — `CastRoles` casts proposer (crediting reviewer's agent), challenger (`role:skeptic`, model ≠ proposer), judge (`role:judge`, model ≠ both); insufficient distinct models → unresolved unless `allow_single_model`/`--single-model`. `internal/debate/cast.go:65-156`
- **Bounded 3-turn protocol** — `RunDebate` hard-codes proposer→challenger→judge over the Epic 2.0 tool loop; halted seat → recorded, never dropped. `internal/debate/protocol.go:56-160`
- **Ruling integration** — `applyRulings` writes verdict (`confirmed`/`refuted`) + `challenge_survived`, recomputes confidence (→ VERIFIED on confirmed), overwrites severity only on split; `reconciled/debate.json` + manifest `debate` stage emitted atomically. `internal/debate/emit.go:97-189`
- **Confidence axis** — folded onto existing `VERIFIED` tier (no `DEBATED` tier), per epic Q1. `internal/reconcile/confidence.go:12-31`
- **Auditability** — per-item JSONL transcripts (turns + ruling) and the report's "Contested findings" section with per-ruling rationale + overflow disclosure. `internal/debate/transcript.go`, `internal/report/contested.go`

## 4. Remaining Unchecked Items

No remaining unchecked items — all 5 success criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All 5 success criteria implemented with concrete `file:line` evidence; epic clarifications (Q1–Q4) honored exactly. Hostile review surfaced no critical/high defects; the 16 findings are correctness-hardening and consistency items (4 medium, 12 low) suitable for technical debt.

## 6. Coverage Analysis
- **Coverage:** 89.1% (total) — `internal/debate` 83.2%, reconcile 90.1%, registry 88.3%, report 96.9%, verify 94.2%
- **Baseline:** 80%
- **Delta:** ↑9.1%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Tests | PASSING | `go test ./...` (0 failures) |
| Lint | PASSING | `golangci-lint run` (0 issues) |
| Types | PASSING | `go vet ./...` |
| Format | PASSING | `gofmt -l` (clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 20 source files (3 hostile reviewers, full mode)
- **Issues Found:** 16 (Critical: 0, High: 0, Medium: 4, Low: 12)

### Medium
1. **`applyRulings` clobbers prior Verification block** — `internal/debate/emit.go:113`. A debated `verification_disagreement` finding loses its original multi-voter `Skeptic` list and verify `Notes` (overwritten with the single judge). Audit/provenance regression. Fix: mutate in place / dedicated judge field.
2. **`FindingKey` duplicate matching** — `internal/debate/emit.go:104`. `{File,Line,Problem}` is not guaranteed unique; one ruling can apply to multiple findings and idempotency can drop undebated items. Fix: thread a stable finding identity or dedupe upstream.
3. **Split with no `settled_severity` backfills radar-max severity** — `internal/debate/debate.go:247`. A malformed split unconditionally overwrites severity with the reviewer maximum. Fix: leave severity untouched when the judge settled none.
4. **`--require-verified` CLI/MCP divergence** — `cmd/atcr/review.go:146`. CLI rejects `--debate --require-verified` without `--verify`, but MCP `handleDebate` allows the equivalent. Both produce `confirmed`/VERIFIED. Fix: relax the CLI guard to match MCP.

### Low (12)
Prompt-injection hardening (math/rand 32-bit sentinel `protocol.go:64`; raw newline interpolation in seat prompts `protocol.go:179`), distinct-model by exact model-string only (`cast.go:146`), nil-Providers casts zero-provider seats (`cast.go:162`), no ctx cancellation check between items (`debate.go:128`), findings-write failure skips debate.json emit (`debate.go:146`), duplicated trigger defaulting (`config.go:486` vs `select.go:38`), unbounded unresolved `Reason` in report (`contested.go:69`), overturned shows live severity tag (`contested.go:91`), `challenge_survived` read but never rendered (`contested.go:24`), single-model fallback under-disclosed at summary level (`contested.go:103`), no empty-verdict guard in `applyRulings` (`emit.go:113`).

### Confirmed-correct (could not break)
- `overturn` → `refuted` never blocks the gate (`reconcile/gate.go` IsFailing excludes refuted).
- Confidence: confirmed → VERIFIED, refuted → LOW; folded onto existing axis (no tier proliferation).
- Report Contested section: all free text escaped/truncated; no injection reachable. Transcript JSONL via `json.Marshal` — injection-proof.
- `parseRuling` degrades malformed judge output to `unresolved`, never drops an item; brace matching string/escape-aware.
- `max_items` cap slicing off-by-one-correct; overflow recorded in artifact + report.

## 9. Follow-ups
- Route the 16 adversarial findings to technical debt via `/reconcile-code-review @.planning/epics/completed/6.0_cross_examination.md`.
- The 4 medium items (Verification clobber, FindingKey uniqueness, split-severity backfill, require-verified divergence) are the priority fixes; none block the epic's acceptance.

---
*Generated by /execute-code-review on June 21, 2026 01:38:50PM*
