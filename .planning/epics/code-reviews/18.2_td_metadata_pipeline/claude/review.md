# Code Review Stream - 18.2_td_metadata_pipeline (Epic)

**Started:** July 04, 2026 02:52:31PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — `atcr reconcile` extracts the relevant narrative section from each source's `review.md` for a finding, when available.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/justification.go:68` (stampJustifications), `:136` (matchNarrative), `:201` (anchorTier), `:303` (extractSection)
- **Notes:** Collects every `sources/**/review.md`, matches by file:line (tier ≥2 required, ±3 proximity), extracts the enclosing Markdown block. Wired into the reconcile path at `internal/reconcile/gate.go:255`.

### Criterion: AC2 — `reconciled/findings.json` carries the extracted narrative as a `justification` field per finding.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/emit.go:141` (Justification field, `json:"justification,omitempty"`), `internal/reconcile/gate.go:255-256` (stamp + cache to res.jsonFindings)
- **Notes:** Stamped after merge/path-validation/symbol-anchoring, before Emit, so the field rides into findings.json via RenderJSON. omitempty keeps pre-18.2 output byte-identical.

### Criterion: AC3 — Back-reference (source path + section/line) included in the reconciled JSON; never in README `Problem` cell.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/emit.go:147-160` (SourceReport{Path,Line,Section}, `json:"source_report,omitempty"`), stamped at `gate.go:255`
- **Notes:** Path is review-dir-relative; stamped only onto JSONFinding records (jf). No write path touches the TD README table.

### Criterion: AC4 — `docs/findings-format.md` documents new fields in both `## JSON form` and `## Source discovery` sections.
- **Verdict:** VERIFIED ✅
- **Evidence:** `docs/findings-format.md:128-138` (JSON form: justification/source_report subsection), `docs/findings-format.md:90` (Source discovery: review.md now read)
- **Notes:** Both sections updated; fields documented as additive/backward-compatible, distinct from verification.notes.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md risk profile)
**Files Reviewed:** 4 (justification.go, emit.go, gate.go, justification_test.go)
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 6

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 5

Positive verification: JSON encoding neutralizes markdown/HTML injection from justification text into findings.json (never rendered to report.md this epic); filepath.Rel + IsRegular() reject symlinked/cross-volume review.md (no path traversal into source_report.path); parseLineRange is overflow-safe (strconv.Atoi errors cleanly, no panic); the isPathChar suffix guard prevents `y.go` matching `internal/x/y.go:42`; tiebreak ordering (tier → reviewer → narrative index → line) is deterministic with a stable relPath sort.
