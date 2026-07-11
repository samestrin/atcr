# Tech Debt Captured — Sprint 19.10 (Reviewer Payload Sizing)

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, `SOURCE=execute-sprint`).

## TD-001 — on_overflow config key missing from docs/registry.md (LOW)
**Origin:** Phase 1, task 1.4 gate review, 2026-07-10
**File:** docs/registry.md:122,164
**Issue:** The new `on_overflow` config key is documented in the `.atcr/config.yaml` scaffold and struct comments but not in the canonical reference `docs/registry.md`. The mirrored `review_strategy` key has both a settings-table row and a slot in the shared-review-settings precedence list; `on_overflow` (identical registry→project precedence) appears in neither.
**Why accepted:** Documentation-surface only; below the sprint's CRITICAL/HIGH inline-fix bar. Code-level docs (scaffold comment, struct doc-comments) already describe the key, values, and default, so the feature is discoverable to config authors.
**Fix in:** Phase 5 (final documentation sweep) or a follow-up docs pass — add an `on_overflow` row to the settings table (default `chunk`, enum chunk/truncate/fallback/fail, note fallback/fail recognized-but-gated, registry+project tiers only, no CLI flag) and to the shared-settings precedence list.
