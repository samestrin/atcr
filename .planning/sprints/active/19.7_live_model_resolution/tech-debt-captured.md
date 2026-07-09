# Tech Debt Captured — Sprint 19.7 Live Model Resolution

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded, `SOURCE=execute-sprint`).

## TD-001 — @stable segment matching must strip :variant suffix + vendor prefix before tokenizing (MEDIUM)
**Origin:** Phase 1, task 1.4 gate review, 2026-07-08
**File:** internal/personas/catalog.go (Phase 3, not yet created) — spec in plan/documentation/openrouter-catalog-api.md
**Issue:** The `@stable` preview-token segment-matching rule is defined only over hyphen delimiters (segment-equals / suffix `-token` / interior `-token-`). It does not specify stripping the `:variant` suffix (e.g. `:free`, `:thinking`) or the vendor prefix before tokenizing, so a preview model carrying a variant suffix (e.g. `...-preview:free`) matches neither suffix `-preview` (string ends in `:free`) nor interior `-preview-` (followed by `:`), and escapes the `@stable` exclusion — a latent false-negative.
**Why accepted:** No live model today carries a preview token combined with a `:variant` suffix (forward-looking rule only). Deferred so the normalization lands in Phase 3's `catalog.go` with its own unit test rather than as Phase-1 prose.
**Fix in:** Phase 3 (task 3.10/3.11, `@stable` channel) — normalize the slug (strip `:suffix` and vendor prefix) before hyphen-segment tokenization; add a `...-preview:free` test case.
**Resolved:** 2026-07-08 — Phase 3 task 3.11 (`0c7e9bd2`/`18f55171`): `slugHasPreviewSegment` strips the `:variant` suffix and vendor prefix, then hyphen-segment-matches `previewTokenSet`; `TestResolveModel_Stable_TD001_StripsVariantSuffix` covers `...-preview:free`.

## TD-002 — deprecation exclusion treats far-future expiration_date sentinels as deprecated (LOW)
**Origin:** Phase 1, task 1.4 gate re-review, 2026-07-08
**File:** internal/personas/catalog.go (Phase 3, not yet created) — spec in plan/documentation/openrouter-catalog-api.md
**Issue:** The `@stable`/`@latest` condition-2 rule excludes ANY non-null `expiration_date`. `z-ai/glm-5v-turbo` carries `2098-12-31` — a far-future sentinel that is effectively "not deprecated" — so treating it as excluded drops an effectively-stable model from the channel. Channel-only impact: no persona pin is affected today (glenna resolves to `z-ai/glm-5.2` regardless).
**Why accepted:** No persona is mis-resolved today; the strict any-non-null rule is safe (fails closed) and simpler. A deprecation-horizon refinement is a design choice, not a correctness fix.
**Fix in:** Phase 3 (task 3.10/3.11) — decide whether to apply a deprecation horizon (exclude only if `expiration_date` is within N days / already past) instead of any-non-null, and document the chosen rule with a far-future-date test case.
**Resolved:** 2026-07-08 — Phase 3 task 3.11 (`18f55171`): DECIDED to keep the any-non-null rule (`isDeprecated`), no horizon window — it fails closed, is simpler, and mis-resolves no persona today (a far-future sentinel is treated as deprecated by design). `TestResolveModel_Stable_TD002_FarFutureExpirationExcluded` documents the decision with the `2098-12-31` sentinel case.

## TD-003 — resolved-lock advance re-fetches/deletes co-located .md via writePersonaUnit (MEDIUM)
**Origin:** Phase 4, task 4.2.A adversarial review + task 4.11 phase gate (two independent reviewers), 2026-07-09
**File:** internal/personas/upgrade.go (upgradeResolvedLock → writePersonaUnit)
**Issue:** The binding/resolution path advances only the local `model:` lock but persists through `writePersonaUnit`, which (a) re-fetches the persona's co-located `<name>.md` from the community base URL (`ATCR_PERSONAS_URL` — a different service than the catalog that produced the slug), (b) on a 404 for that `.md`, DELETES the local `.md`, and (c) aborts the whole advance on any transient non-404 `.md` error. Data-loss path (gate framing): a binding persona with a locally-authored/customized custom prompt but no upstream `.md` loses its prompt on a mere model-slug bump. Also couples a catalog-resolved advance to the personas `.md` endpoint being reachable, and leaves a dry-run/real-run parity gap (dry-run never exercises the `.md` fetch/delete step, so a dry-run can report an advance a real run then fails on).
**Why accepted (deferred, not fixed inline):** Not exploitable today — ZERO personas ship a `binding:` field, so the resolution path (and thus the `.md` re-sync) is exercised only by test fixtures; the data-loss scenario requires a future binding-carrying persona with a locally-customized `.md`. Reusing `writePersonaUnit` is the story's explicit mandate ("writes continue to flow through writePersonaUnit so install and upgrade stay consistent"), and the correct decoupling interacts with how future personas ship bindings + prompts — a design decision, not a hot fix. The misleading `setModelField` doc-comment (over-claimed "only change written to disk") was corrected inline at the 4.11 gate.
**Fix in:** A future robustness pass (or whenever the first binding-carrying persona ships) — give the resolved path a write-only-yaml lock advance (`refuseSymlinkedIntermediate` + `writeFileAtomic(dest, newYAML)`, no `.md` fetch/delete), so a lock advance never touches the prompt or depends on a second network fetch; this also closes the dry-run `.md`-step parity gap. Add tests: a local custom `.md` survives a resolve-path advance; dry-run vs real-run parity across the full write.

## TD-004 — created-timestamp selection vs semver version-advance gate can diverge (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-07-09
**File:** internal/personas/upgrade.go (upgradeResolvedLock gate) + catalog.go (resolveNewestInPrefix)
**Issue:** `resolveNewestInPrefix` selects the newest-by-`created` catalog member, but the lock gate advances only when the extracted semver is a version-advance (`isNewer`). A newer-created-but-lower-semver slug (e.g. a v3.9 hotfix published after v4.0) is selected by the resolver yet blocked by the gate, so the "newest" model is silently not adopted and the run reports unchanged.
**Why accepted:** Surfaced to the maintainer at the Phase 4 safety gate and accepted: the story mandates `isNewer` reuse for the version-advance decision, and the behavior fails safe (never downgrades). The created-vs-semver interaction is intertwined with Story 6's major/minor classification.
**Fix in:** Story 6 (Phase 6, major-bump re-validation gate) — decide the single selection authority (created-order vs semver) or emit a signal when they diverge; cover the divergent hotfix case with a test.
