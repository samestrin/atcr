# Code Review Stream - 7.5_syntax-guard-refinements (Epic)

**Started:** June 22, 2026 05:55:49PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — Unfenced multi-line JSON/config with block braces is NOT flagged invalid_syntax
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/syntaxguard.go:177-178`, `internal/verify/syntaxguard.go:190-192`; tests `internal/verify/syntaxguard_test.go:226-242`
- **Notes:** `looksLikeGoCode` now consults `looksLikeNonGoBraces` ahead of the block-brace signal and returns `false` for quoted-key JSON object shapes. Covered by three NoError tests (flat object, nested object, JSON array of objects).

### Criterion: AC2 — Detection only suppresses flagging; no previously-unflagged input becomes flagged
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/syntaxguard.go:104-110` (looksLikeGoCode consulted only after parseGoFix fails), `internal/verify/syntaxguard.go:173-181` (new branch only adds a `return false` path)
- **Notes:** Structurally guaranteed: the only added control-flow is a `return false`. No new `return true` path exists, so nothing newly becomes flagged. Negative guard tests confirm brace-structured broken Go and quoted `case` labels remain flagged (`syntaxguard_test.go:248-262`).

### Criterion: AC3 — All existing Epic 7.1 syntax-guard tests pass unchanged
- **Verdict:** VERIFIED ✅ (pending Phase 4 test run)
- **Evidence:** Diff is purely additive — the existing `looksLikeGoCode` boolean expression is preserved as the fallthrough (`syntaxguard.go:180`); no existing test was modified (diff only appends after line 217 of the test file).
- **Notes:** Confirmed by the green test run in Phase 4.

### Criterion: AC4 — Conservative-recall policy preserved and documented
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/syntaxguard.go:166-172`, `internal/verify/syntaxguard.go:183-189`; characterization test `internal/verify/syntaxguard_test.go:264-276`
- **Notes:** The accepted false negative (broken string-keyed Go map suppressed) is documented in the function comments and locked by an explicit characterization test that warns against "fixing" it.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (hostile full review)
**Files Reviewed:** 2 (internal/verify/syntaxguard.go, internal/verify/syntaxguard_test.go)
**Issues Found:** 3 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic — no sprint-design.md)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 3

### Notes
- AC2 safety property (suppression can only turn the block-brace signal true->false, never add flagging) was structurally and empirically confirmed by the reviewer — could not be broken.
- Headline finding: a single-key JSON object whose only key contains an escaped quote (`"a\"b": 1`) is still spuriously flagged (jsonKeyLineRe is not escape-aware). Independently reproduced: validateGoFixSyntax returns "illegal label declaration". LOW — multi-key objects (the common case) are unaffected.
- Remaining two: a regex-boundary test gap, and a comment that over-promises the keyword check's precision.
- Accepted trade-offs (NOT defects): broken string-keyed Go map suppression (AC4), parseGoFix OR-strategies (WONTFIX), multi-fence validation (WONTFIX).
