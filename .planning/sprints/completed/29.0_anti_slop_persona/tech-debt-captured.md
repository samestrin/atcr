# Tech Debt Captured — Sprint 29.0 Anti-Slop Persona

## TD-001 — simon.md output CATEGORY not pinned to `bloat` (MEDIUM)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-16
**File:** personas/community/simon.md:50
**Issue:** The `## Output Format` block constrains CATEGORY to "one lowercase word" (the shared house style across all 13 personas) rather than pinning it to `bloat`. At runtime the model could emit a category word colliding with an already-claimed lens (e.g. a redundant type guard emitted as `type`, an over-abstraction as `complexity`), muddying aggregated multi-persona output.
**Why accepted:** All 13 existing personas use the identical generic "one lowercase word" phrasing with a category-flavored example; pinning simon alone would deviate from the established registry convention. No test gate requires the emitted CATEGORY to equal the persona's registry category, and the differentiating word `bloat` is already authored into the template (Role + Example) as the gate requires.
**Fix in:** Future persona-authoring polish sprint — decide house-wide whether single-lens personas should pin their output CATEGORY column, and apply uniformly across the roster rather than to simon in isolation.
