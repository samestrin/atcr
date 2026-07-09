# Tech Debt Captured — Sprint 19.7 Live Model Resolution

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded, `SOURCE=execute-sprint`).

## TD-001 — @stable segment matching must strip :variant suffix + vendor prefix before tokenizing (MEDIUM)
**Origin:** Phase 1, task 1.4 gate review, 2026-07-08
**File:** internal/personas/catalog.go (Phase 3, not yet created) — spec in plan/documentation/openrouter-catalog-api.md
**Issue:** The `@stable` preview-token segment-matching rule is defined only over hyphen delimiters (segment-equals / suffix `-token` / interior `-token-`). It does not specify stripping the `:variant` suffix (e.g. `:free`, `:thinking`) or the vendor prefix before tokenizing, so a preview model carrying a variant suffix (e.g. `...-preview:free`) matches neither suffix `-preview` (string ends in `:free`) nor interior `-preview-` (followed by `:`), and escapes the `@stable` exclusion — a latent false-negative.
**Why accepted:** No live model today carries a preview token combined with a `:variant` suffix (forward-looking rule only). Deferred so the normalization lands in Phase 3's `catalog.go` with its own unit test rather than as Phase-1 prose.
**Fix in:** Phase 3 (task 3.10/3.11, `@stable` channel) — normalize the slug (strip `:suffix` and vendor prefix) before hyphen-segment tokenization; add a `...-preview:free` test case.

## TD-002 — deprecation exclusion treats far-future expiration_date sentinels as deprecated (LOW)
**Origin:** Phase 1, task 1.4 gate re-review, 2026-07-08
**File:** internal/personas/catalog.go (Phase 3, not yet created) — spec in plan/documentation/openrouter-catalog-api.md
**Issue:** The `@stable`/`@latest` condition-2 rule excludes ANY non-null `expiration_date`. `z-ai/glm-5v-turbo` carries `2098-12-31` — a far-future sentinel that is effectively "not deprecated" — so treating it as excluded drops an effectively-stable model from the channel. Channel-only impact: no persona pin is affected today (glenna resolves to `z-ai/glm-5.2` regardless).
**Why accepted:** No persona is mis-resolved today; the strict any-non-null rule is safe (fails closed) and simpler. A deprecation-horizon refinement is a design choice, not a correctness fix.
**Fix in:** Phase 3 (task 3.10/3.11) — decide whether to apply a deprecation horizon (exclude only if `expiration_date` is within N days / already past) instead of any-non-null, and document the chosen rule with a far-future-date test case.
