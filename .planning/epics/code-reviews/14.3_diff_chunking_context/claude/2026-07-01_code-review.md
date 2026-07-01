# Code Review Report: 14.3_diff_chunking_context

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 4 / 4 acceptance criteria
- **Approval Status:** Approved
- **Review Date:** July 01, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial + Tests)

All four acceptance criteria are fully implemented and verified against the merged code (commit d19999a2). Quality gates are green. Adversarial review surfaced 12 follow-up findings (0 critical, 1 high, 4 medium, 7 low) — none block the epic's acceptance; they are captured for `/reconcile-code-review`.

## 2. Acceptance Criteria Verified
- **AC1 — bulk/chunked toggle** — `[ ]` → `[x]`
  - Evidence: `internal/registry/project.go:21`, `internal/registry/config.go:402`, `internal/registry/precedence.go:127,156,165,227`, `internal/registry/review_strategy.go:10-18`
- **AC2 — `max_context_lines` on AgentConfig w/ default** — `[ ]` → `[x]`
  - Evidence: `internal/registry/config.go:346`, `:36` (default 1500), `:714-716` (validation), `:942-947` (EffectiveMaxContextLines)
- **AC3 — bin-packing without exceeding `max_context_lines`** — `[ ]` → `[x]`
  - Evidence: `internal/fanout/chunker.go:82-110`, `:49-70`, `internal/fanout/review.go:820-864`
- **AC4 — Reconciler merges multi-chunk findings from one persona** — `[ ]` → `[x]`
  - Evidence: `internal/fanout/chunker.go:122-226`, `internal/fanout/review.go:570` (before `writePool` at `:625`)

## 3. Evidence Map
- **Run-wide strategy resolution** resolves registry → project → embedded, defaults `bulk`, with defense-in-depth revalidation of the resolved value (`precedence.go:227`).
- **Per-agent chunk budget** is a `*int` (nil inherits `DefaultMaxContextLines=1500`), validated `1..MaxContextLinesCap` at load.
- **Chunker** splits on `diff --git a/` boundaries, greedy next-fit, never splits a file, sends a lone oversized file as its own chunk with a stderr warning.
- **Same-persona aggregation** (Option A) keeps `Reviewer=<persona>` so the merged 14.2 consensus filter counts the persona once; merge precedes artifact write.

## 4. Remaining Unchecked Items
No remaining unchecked items — all 4 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** Implementation matches the epic's Proposed Solution, Technical Constraints, and recorded clarifications (Option A attribution, default 1500, run-wide toggle, no 14.2 changes). Code is well-documented, tested (fanout 86.9%, registry 90.0% coverage), and clean on lint/vet/format.

## 6. Coverage Analysis
- **Coverage:** internal/fanout 86.9%, internal/registry 90.0% (changed packages)
- **Baseline:** 80%
- **Delta:** ↑ (both above baseline)
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |
| Tests | PASSING | go test ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 6 (chunker.go, review.go, review_strategy.go, config.go, precedence.go, project.go)
- **Issues Found:** 12 (Critical: 0, High: 1, Medium: 4, Low: 7); 1 raw finding dropped as duplicate of existing TD README line 69

### Issues by Severity
**HIGH**
- `internal/fanout/chunker.go:212` — `mergeResultGroup` reports `StatusOK` when ANY chunk succeeds; partial chunk failure leaves part of the diff unreviewed with only a stderr signal (no summary.json/manifest/status), so a CI gate can pass green over unreviewed code.

**MEDIUM**
- `internal/fanout/chunker.go:19` — hardcoded `diff --git a/` marker breaks under `git diff.noprefix=true` and diverges from `internal/payload`'s `diff --git ` convention; chunking silently degrades to bulk. (New `--no-prefix` angle on the marker already tracked for combined diffs.)
- `internal/fanout/review.go:840` — with `payload_mode: files` the payload has no `diff --git` line, so chunked mode is a silent no-op and the oversize warning mislabels the whole multi-file payload as one file (`countDiffFiles <= 1`).
- `internal/fanout/review.go:941` — `buildAgent` is dead production code after the `renderAgent` extraction (only test callers); duplicate mode-resolution logic risks drift.
- `internal/fanout/chunker.go:82` — no ceiling on chunk count; `payload_byte_budget: 0` (unlimited) + `chunked` + huge diff yields unbounded chunks/slots/goroutines/API calls — a cost/DoS vector on the cost-control feature.

**LOW** (7)
- `chunker.go:25` countLines undercounts a final line without trailing newline (affects bin-packing boundary + oversize warning precision).
- `chunker.go:182` `FallbackFrom` overwritten by last fallback chunk (non-deterministic telemetry).
- `review.go:847` redundant O(n) scans per chunk (`EffectiveMaxContextLines`/`countDiffFiles`/`countLines`).
- `project.go:47` `review_strategy` not validated in `LoadProjectConfig` (unlike `payload_mode`); invalid value surfaces late without file-path context.
- `config.go:712` comment rationale for the `max_context_lines > 0` check is factually inverted vs the chunker's `<=0` disable-chunking sentinel.
- `config.go:942` `EffectiveMaxContextLines` returns raw value with no re-validation for directly-constructed structs (inconsistent with retry-bounds defense-in-depth).
- `precedence.go:153` `review_strategy` lacks a CLI override tier despite the "mirrors payload_mode" claim and the documented CLI>project>registry>embedded chain (by-design per clarification; doc/consistency gap).

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/14.3_diff_chunking_context.md` to merge these 12 findings into `.planning/technical-debt/README.md` with reviewer attribution (several already overlap epic-captured items).
- Prioritize the HIGH partial-chunk-coverage finding — it undermines the trustworthy-findings goal of the 14.x milestone.

---
*Generated by /execute-code-review on July 01, 2026 10:44:57AM*
