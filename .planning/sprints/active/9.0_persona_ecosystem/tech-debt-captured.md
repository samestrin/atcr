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

## TD-003 — NaN corroboration score breaks matched-partition total order (MEDIUM)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-06-24
**File:** internal/verify/select.go (SelectEligibleSkeptics matched-partition sort)
**Issue:** The matched-partition comparator sorts by `scores[name]` descending. If a future `scorecard.Aggregate()` produces a NaN rate (e.g. a 0/0 corroboration division), the `si != sj` / `si > sj` comparisons are both false for a NaN entry, so the comparator is not a total order and `sort.SliceStable` leaves NaN-scored skeptics in an ill-defined position — silently non-deterministic routing, the exact failure the partition exists to prevent.
**Why accepted:** Not active — the sole production caller (`pipeline.go`) passes `scores=nil` until T6 (Phase 5) wires the scorecard; corroboration rates are bounded 0..1 by construction. Robustness gap, not a live defect; sprint policy defers MEDIUM.
**Fix in:** Phase 5 (T6 score wiring) or a hardening pass — sanitize NaN to the lowest rank (`math.IsNaN(s)` → treat as -Inf) before the sort and add a NaN-score test. Best fixed where `scorecard.Aggregate()` output first enters the map.

## TD-004 — No independence test for returned Skeptic.Config.Language slice (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-06-24
**File:** internal/verify/select_test.go
**Issue:** `Skeptic.Config.Language` aliases the registry's backing array (same read-only contract as `Scope`), but only `Scope` has an independence test (`TestSelectEligibleSkeptics_ScopeSliceIsIndependent`). The new `Language` alias field has no equivalent guard.
**Why accepted:** No live bug — `languageMatches` only reads `Language` and never appends to or returns it for mutation, unlike `Scope` which callers append to during prompt injection. Defensive-test gap only.
**Fix in:** Future test-hardening pass — add a `Language`-slice independence test mirroring the Scope one, or document that `Language` shares the Scope read-only guarantee.

## TD-005 — Score-map keyspace assumption undocumented at the call site (LOW)
**Origin:** Phase 2, task 2.LAST gate review, 2026-06-24
**File:** internal/verify/pipeline.go:162
**Issue:** `SelectEligibleSkeptics`'s `scores` map is keyed by skeptic registry name (`reg.Agents` key), but T6's source `scorecard.Aggregate()` is described as "reviewer-name → rate". These coincide only when a skeptic is also a credited reviewer; a key-space mismatch (skeptic registry name vs reviewer display name) would surface not as a compile error but as silent alphabetical fallback. The call site passes `nil` today, so nothing is wrong yet.
**Why accepted:** Not active — `scores` is `nil` until T6 (Phase 5) wires the scorecard. Routing-only; worst case is loss of score-based tie-break, never a crash or wrong-skeptic security issue.
**Fix in:** Phase 5 (T6) — when wiring `scorecard.Aggregate()` into `pipeline.go:162`, add a one-line comment/assertion that the map is keyed by skeptic registry name (canonicalized per the Phase 5 `strings.ToLower` join convention) so a keyspace mismatch surfaces in review.
