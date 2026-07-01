# Code Review Stream - 14.1_verification_grounding (Epic)

**Started:** June 30, 2026 06:28:12PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — Persona prompts strictly require file and line (or equivalent)
- **Verdict:** VERIFIED ✅
- **Evidence:** `personas/_base.md:17` (Grounding section: "Every finding MUST cite an exact FILE:LINE that appears in the diff below"), `personas/_base.md:42` (output-format rule: "FILE:LINE must be a real, exact location copied from the diff — never approximate, guess, or invent it")
- **Notes:** The shared base persona (inherited by every reviewer) mandates an exact, in-diff FILE:LINE per finding, satisfying AC1.

### Criterion: AC2 — A validation step drops any TD item whose file:line cannot be found in the diff
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/grounding.go:36` (`BuildChangedLines` parses the patch into per-file changed ranges + text); `internal/fanout/grounding.go:28,56,62` (`groundFindings`/`isGrounded` drop findings not anchored in the patch — `return false // file not in the patch: ungrounded`); `internal/fanout/artifacts.go:185` (`findingsFor` applies the gate before the reconciler, logging the per-agent drop count at :187); `internal/fanout/review.go:386,604` + `internal/fanout/resume.go:326,370` (grounding data threaded into WritePool on both fresh and resumed reviews)
- **Notes:** The drop runs in the fan-out stage before the reconciler (Technical Constraint satisfied), on both fresh and resume paths.

### Criterion: AC3 — Prompts explicitly reject analyzing code not part of the + or - lines
- **Verdict:** VERIFIED ✅
- **Evidence:** `personas/_base.md:17` ("Do not report, speculate about, or analyze code that is not part of the changed lines"); `internal/payload/scope.go:40` (changed-only scope rule: a finding outside the changed lines "will be discarded before it reaches the report")
- **Notes:** Both the base persona and the injected per-mode scope rule forbid out-of-patch commentary, with `CATEGORY out-of-scope` the sole sanctioned escape hatch.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic)
**Files Reviewed:** 6 (internal/fanout/{grounding,artifacts,review,resume}.go, internal/payload/{grounding,scope}.go)
**Issues Found:** 12 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 12

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 4
- Low: 8

### Notes
- The two hostile reviewers independently confirmed the **wiring is correct**: grounding runs on both the fresh-review and resume paths, before `enforceConstraints` (so `max_findings` ranks only grounded findings) and before the reconciler; determinism, panic-safety, header-exclusion in `parseFileChange`, `line 0`/binary fail-open, and the `±3` boundary all match their documented contracts.
- The 12 findings are precision/observability/edge-case hardening of an already-shipped, fail-safe gate — none are release-blockers. Themes: (1) the evidence-fallback floor (12) is low enough that common boilerplate like `if err != nil {` grounds a finding, and its comment overstates the protection; (2) `scopeFiles` doesn't warn files-mode reviewers of the hard-drop; (3) drops are stderr-only with no status.json audit trail; (4) an empty-but-non-nil changed map would drop all findings run-wide (should fail open like nil).
- Both agents' "HIGH" self-ratings were downgraded to MEDIUM after verification (fail-safe design + clarification-accepted evidence-fallback leniency).
