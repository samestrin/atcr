# Tech Debt Captured — Sprint 9.0 Persona Ecosystem

> Surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded, SOURCE=execute-sprint).

## TD-001 — Language tokens accept silent-no-match junk (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-24
**File:** internal/registry/config.go (NormalizeLanguageToken)
**Issue:** `NormalizeLanguageToken` only trims surrounding whitespace and strips a single leading dot; interior whitespace or extra dots (`"g o"`, `".."` → `"."`) store as junk tokens that silently never match any finding extension. A misconfigured persona `language` entry fails with no diagnostic.
**Why accepted:** Routing-only (tokens are compared in skeptic selection, never interpolated into a prompt), so not a security or correctness risk — at worst the persona is simply not preferred. Silent-no-match is an acceptable contract for forgiving third-party authoring (no allow-list by design).
**Fix in:** Phase 6 docs (or later) — document in `docs/personas-authoring.md` that only surrounding-whitespace + single-leading-dot are canonicalized; interior-space tokens are accepted verbatim and will not route. No code change required.

## TD-002 — Canonicalization idempotency tested for ASCII only (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-24
**File:** internal/registry/config_test.go (TestAgentConfig_LanguageField_Canonicalization)
**Issue:** The idempotency assertion covers only ASCII inputs (`.Go`, ` .TS `, `GO`). `strings.ToLower` is not idempotent for every Unicode codepoint, and the no-allow-list policy invites arbitrary extensions.
**Why accepted:** Realistic file-extension domain is ASCII; non-ASCII extensions are vanishingly rare and routing-only. Lowest-priority test-thoroughness nit.
**Fix in:** Future test-hardening pass — add a Unicode case-fold idempotency case to lock the canonicalization contract.
