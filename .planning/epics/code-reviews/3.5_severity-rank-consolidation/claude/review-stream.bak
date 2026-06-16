# Code Review Stream - 3.5_severity-rank-consolidation (Epic)

**Started:** June 16, 2026 12:48:27PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — single exported rank map + NormalizeSeverity in internal/stream
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/severity.go:20` (`var SeverityRank = map[string]int{"CRITICAL": 4, "HIGH": 3, "MEDIUM": 2, "LOW": 1}`), `internal/stream/severity.go:26` (`func NormalizeSeverity`)
- **Notes:** New file. Map + normalizer both exported and documented as the single source of truth. Package imports only stdlib `strings`.

### Criterion: AC2 — reconcile/fanout-postprocess/verify/report consume shared; local maps deleted
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/merge.go:24` (`var SeverityRank = stream.SeverityRank` re-export alias; literal deleted), `internal/fanout/postprocess.go:27-58` (uses `stream.SeverityRank`/`stream.NormalizeSeverity`; local map removed), `internal/verify/severity.go` (local `severityRank` + `normalizeSeverity` removed, uses stream), `internal/report/render.go:419` (`stream.SeverityRank[stream.NormalizeSeverity(s)]`)
- **Notes:** reconcile keeps an exported alias per recorded clarification (Option A) — the local literal `{SevCritical:4,...}` is deleted; the alias is a thin re-export, not an independent map. fanout, verify, report all deleted their local maps/normalizers.

### Criterion: AC3 — casing-asymmetry lookup at merge.go:104 fixed; all consumers normalize identically
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/merge.go:106` (`norm := stream.NormalizeSeverity(f.Severity)`), `internal/reconcile/disagree.go:338` and `:360` (gray-zone + maxSev lookups normalized), all cross-package consumers route through `stream.NormalizeSeverity`
- **Notes:** merge.go now also keys the seen-set by the normalized form so a mixed-case duplicate is one severity, not a spurious disagreement (adversarial hardening beyond the raw fix).

### Criterion: AC4 — deleting local maps leaves the full suite green
- **Verdict:** VERIFIED ✅
- **Evidence:** `go test ./...` — all packages pass (0 failures); new tests `internal/stream/severity_test.go`, `internal/reconcile/severity_consolidation_test.go`, updated `internal/report/render_test.go`. Total coverage 88.8%.
- **Notes:** Suite green after consolidation. stream 93.9%, reconcile 90.3%, verify 94.6%, report 97.6%, fanout 87.9% — all consumer packages well above baseline.

### Criterion: AC5 — no fanout→reconcile import; map in internal/stream (zero internal deps)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/postprocess.go:8` imports only `internal/stream`; `internal/stream/severity.go:3` imports only stdlib `strings`; `internal/boundaries_test.go:38` adds `stream` to verify's allowlist (no reconcile added to fanout)
- **Notes:** boundaries_test enforces the import graph. stream has zero internal deps.

### Criterion: AC6 — no severity value or ordering change (pure consolidation)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/stream/severity.go:20` values `{CRITICAL:4, HIGH:3, MEDIUM:2, LOW:1}` identical to deleted maps; `internal/stream/severity_test.go:8-26` pins canonical ordering CRITICAL>HIGH>MEDIUM>LOW>0
- **Notes:** Pure consolidation — values and ordering preserved exactly.

## Adversarial Analysis (Discovery Mode)

**Mode:** Full hostile review (discovery-only — no sprint-design.md in epic mode)
**Files Reviewed:** 6 (stream/severity.go, fanout/postprocess.go, reconcile/merge.go, reconcile/disagree.go, report/render.go, verify/severity.go)
**Issues Found:** 5 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 5

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 4

### Reviewer-verified disposition
- **All 6 ACs PASS.** The epic delivered its scoped charter; the boundary-sites-only normalization (merge.go:104, disagree.go:338/360) was a deliberate, documented decision (clarification Q2).
- The 5 findings are **residual hardening items**, not AC failures. The dominant theme (caught independently by both adversarial agents) is that normalization is **half-applied** within reconcile/disagree.go and render.go: render.go:122 + grayZoneItem normalize defensively against the documented hand-editable findings.json surface (render.go:116-118), but disagree.go's sibling lookups (248/282/298/322/463), reconcile.go:108, gate.go:45/49, and render.go:204 do not — leaving a latent report-view-vs-radar-JSON casing desync that survives the epic.
- **Filtered out as non-findings:** merge.go "behavior change" (already covered by AC6's documented intent + tests in severity_consolidation_test.go); re-export alias footgun (deliberate Option-A decision with documented read-only contract, no mutation exists); verify/severity.go (confirmed clean, byte-identical consolidation).
