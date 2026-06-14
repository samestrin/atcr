# Tech Debt Captured — Sprint 3.0 Adversarial Verification

Items deferred during `/execute-sprint`. Read by `/execute-code-review` (Phase 1) and pre-seeded into the adversarial TD stream (SOURCE=execute-sprint).

---

## TD-001 — SelectEligibleSkeptics returns aliased reference fields (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-14
**File:** internal/verify/select.go:65
**Issue:** Returned `registry.AgentConfig` values are shallow copies — embedded reference fields (`Scope []string`, `*Temperature`, `*TimeoutSecs`, `*MaxTurns`, `*ToolBudgetBytes`, `*MaxFindings`) alias the registry's backing memory. A caller mutating `out[i].Scope[0]` or writing through a pointer field would corrupt the shared registry. Current consumers (Phase 2 skeptic invocation) treat configs read-only, so no live bug.
**Why accepted:** No consumer mutates returned configs; deep-copying every selection would be premature for a read-only access pattern. Behavior is correct for the sprint's needs.
**Fix in:** Epic 3.0 follow-up or whenever a mutating consumer appears — either deep-copy reference fields in `AgentsByRole`/`SelectEligibleSkeptics`, or add an explicit read-only contract to the godoc plus a test that mutates a slice/pointer field and asserts the registry is unchanged.

## TD-002 — namesOf test helper relies on structural equality (LOW) — RESOLVED
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-14
**Issue:** The original `namesOf` test helper reverse-mapped configs to names via `reflect.DeepEqual`, which would mis-attribute names if two fixture configs were identical.
**Resolved:** 2026-06-14 — the Phase 1 gate (1.5) drove `SelectEligibleSkeptics` to return `[]Skeptic{Name, Config}`, so tests now read `Skeptic.Name` directly. The DeepEqual reverse-lookup is deleted. No debt remains.
