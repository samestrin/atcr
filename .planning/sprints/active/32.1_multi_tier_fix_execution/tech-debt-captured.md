# Tech Debt Captured — Sprint 32.1 (Multi-Tier Fix Execution)

## TD-001 — anyFixEligible ignores complexity ceilings (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-20
**File:** internal/verify/executor.go:67-75
**Issue:** `anyFixEligible` gates whether the executor harness (snapshot + client) is built using only the confidence + severity-floor check; it does not consult the new `withinComplexityCeiling`/`withinSeverityCeiling` bounds. For a single-executor config where every floor-eligible finding is over-ceiling, the full harness is built and then `generateFixes` skips all findings, so the ceiling's cost-avoidance intent is bypassed one layer up. No incorrect output — purely an efficiency gap.
**Why accepted:** Benign in the intended multi-tier flow (over-ceiling findings are meant for a later tier, and the harness/client are shared across the run). Out of Story 2's scope, which owns the per-finding skip mechanics, not harness-build gating. Fixing it here would broaden the change surface beyond the AC.
**Fix in:** A later optimization pass (or a follow-up sprint) — extend `anyFixEligible` to also require `withinComplexityCeiling`/`withinSeverityCeiling` so a single-tier all-over-ceiling config skips the snapshot/client build entirely; alternatively document that the harness build intentionally ignores ceilings.
