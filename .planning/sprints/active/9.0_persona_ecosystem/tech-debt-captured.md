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

## TD-006 — Persona count hardcoded in doc-comment prose (LOW)
**Origin:** Phase 3, task 3.2.A adversarial review, 2026-06-24
**File:** personas/personas.go (package + `Names` doc comments)
**Issue:** The package doc comment and `Names`' doc comment state the literal count ("nine embedded ... personas"). If the registry grows or shrinks, the prose drifts silently — no test asserts the comment text. Matches the pre-existing convention (the prior comment said "six").
**Why accepted:** Cosmetic; the authoritative count is asserted by `TestNames_ReturnsAllNine` (length 9 + exact canonical order), so the contract is locked even if the prose drifts. Sprint policy defers LOW.
**Fix in:** Future doc-tidy pass — replace the hardcoded count with a count-agnostic phrasing ("the embedded default reviewer personas") so the comment cannot go stale.

## TD-007 — Unbounded response body read in persona/index fetch (MEDIUM)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-24
**File:** internal/personas/client.go:67
**Issue:** `fetch` reads the community-repo response with an unbounded `io.ReadAll`. A compromised repo or MITM on the HTTPS fetch could return a multi-GB body and OOM the process. Affects Install, Upgrade (persona YAML) and Search (index.json).
**Why accepted:** Source is a configurable, HTTPS, trust-on-first-use repo; the threat requires a compromised host or MITM. Bounded impact; sprint policy defers MEDIUM. No live network in CI (httptest only).
**Fix in:** Phase 6 or follow-up — wrap the body in `io.LimitReader(resp.Body, maxBytes)` (a few MB) before `io.ReadAll` and error on truncation so partial YAML is never validated/written.

## TD-008 — Exported FetchPersonaYAML does not self-validate the name (MEDIUM)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-24
**File:** internal/personas/client.go:71
**Issue:** `FetchPersonaYAML` interpolates the raw `name` into the fetch URL without calling `validatePersonaName`. In-package callers (Install/Upgrade) pre-validate via `personaPath`, but any future external caller of this exported seam could inject `../`, `?`, `#`, or a `scheme://host` into the request path.
**Why accepted:** No current caller is unguarded — every production path validates first. Defense-in-depth gap only; not exploitable today. Sprint policy defers MEDIUM.
**Fix in:** Phase 6 or follow-up — call `validatePersonaName(name)` at the top of `FetchPersonaYAML` and `url.PathEscape` each segment, so the fetch boundary is self-guarding.

## TD-009 — Non-semver version diff treated as upgrade (downgrade masquerade) (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-24
**File:** internal/personas/upgrade.go:77
**Issue:** `isNewer` falls back to `local != remote` for non-semver versions, so any difference (including a downgrade like local `2.0` vs remote `1.0`, both non-semver) reports as an upgrade and overwrites the local file.
**Why accepted:** Matches AC 02-06 Edge Case 1 ("treats any version change as newer when semver parse fails") — this is the specified behavior, not a defect. Valid semver compares correctly. Captured only as a documented sharp edge.
**Fix in:** Future — if non-semver ordering becomes a concern, log that non-semver versions force re-install on any change, or refuse to overwrite when the remote is not parseably newer.

## TD-010 — Non-atomic persona file write (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-24
**File:** internal/personas/install.go:30, internal/personas/upgrade.go:55
**Issue:** Persona YAML is written via `os.WriteFile`. A crash or disk-full mid-write during Upgrade can leave a truncated file replacing a previously valid persona. Validate-before-write protects against invalid content, not partial writes of valid content.
**Why accepted:** Single-user local CLI; crash-during-write is rare and recoverable via re-install/upgrade. Sprint policy defers LOW. The repo already has an `atomicfs`/`atomicwrite` leaf that could back the fix, but `internal/personas` does not import it today (boundary addition).
**Fix in:** Future — write to a temp file in the same dir and `os.Rename` into place (or reuse `internal/atomicfs`) for an atomic replace.

## TD-011 — listCommunity silently degrades on read/parse failure (LOW)
**Origin:** Phase 4, task 4.2.A adversarial review, 2026-06-24
**File:** internal/personas/list.go:71-83
**Issue:** In `listCommunity`, a YAML file that fails `os.ReadFile` or fails to unmarshal is silently listed with `Version: "-"` and no language — a corrupt installed persona is indistinguishable from one legitimately lacking a version field.
**Why accepted:** `list` is a read-only display command; graceful degradation (still show the row by name) is acceptable UX and matches the AC's "exit 0" posture. Sprint policy defers LOW.
**Fix in:** Future — surface per-row read/parse failures as a stderr warning (mirroring the unreadable-dir warning) so a corrupt persona is visibly flagged.

## TD-012 — `atcr personas test` production fixture runner not wired (MEDIUM)
**Origin:** Phase 4, task 4.4.A adversarial review, 2026-06-24
**File:** cmd/atcr/personas.go (personasFixtureRunner = noFixtureRunner{})
**Issue:** The `test` subcommand's default runner (`noFixtureRunner`) always reports `HasFixture: false`, so in production `atcr personas test <name>` prints "No fixture defined" for every persona. AC 02-05 Scenarios 1-3 (actually executing a persona's fixture and mirroring pass/fail in the exit code) require an LLM-backed fixture runner that is not implemented in this phase. Delivered and tested here: the CLI surface, the exit-code-mirroring contract, and the injectable `FixtureRunner` seam (exercised via a stub in `cmd/atcr/personas_test.go`).
**Why accepted:** A real fixture runner needs the review/LLM invocation path (out of Phase 4's file scope — internal/personas + cmd only) and must stay network-free in CI. The injectable seam means wiring the real runner later is additive, no API change. AC 02-05's DoD items that are mechanically verifiable (exit-code mirroring, no-fixture message, injectable/no-live-LLM) pass via the stub.
**Fix in:** A follow-up that reuses Story 1's fixture mechanism (or the verify/fanout invocation path) to build a production `FixtureRunner`, then set it as the `personasFixtureRunner` default. No CLI signature change required.
