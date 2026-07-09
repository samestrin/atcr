# User Story 7: init/quickstart Roster Reconciliation

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr end user running `atcr init` or `atcr quickstart` online (not `--offline`)
**I want** the community persona fetch-and-pin step to actually install a working set of community personas instead of emitting nothing but skip warnings
**So that** my first-run experience matches what the command promises — a populated `.atcr/personas/` community tier — rather than silently pinning zero personas while printing nine confusing "not found in community index" messages

## Story Context

- **Background:** `cmd/atcr/init.go:96` (`installCommunityPersonas`) is the single shared install routine called from both `init.go:47` and `quickstart.go:102`. Both call sites loop `builtins.Names()` — the 9 embedded, model-agnostic built-in personas (`bruce, greta, kai, mira, dax, sasha, penny, ingrid, otto`, from `personas/personas.go:20`) — against the fetched community index. The shipped `personas/community/index.json` publishes a completely disjoint set: 10 model-indexed library personas (`anthony, sonny, gene, milo, gia, flint, delia, quinn, celeste, glenna`), each bound to a specific provider/model pair. Because the two name sets share no members, every online `init`/`quickstart` run today pins zero community personas and prints all nine of `installCommunityPersonas`'s `%q not found in community index — skipping` warnings (`init.go:129`) — noisy output for a command that silently does nothing. This is TD-011, deferred out of 19.6 as a HIGH because closing it correctly requires deciding what the "community roster" for init/quickstart should actually be, which this story now resolves. The 19.6 postmortem (documented in `documentation/existing-resolver-patterns.md`) also flags that `init.go` and `quickstart.go` have drifted from each other before (TD-006/TD-007) when a fix landed in one call site but not the other — this story must fix the shared `installCommunityPersonas` routine (or its shared caller-side roster input) exactly once, not patch each call site independently.
- **Assumptions:** **LOCKED (see plan.md Clarifications, recorded 2026-07-08):** the reconciliation strategy is Option B — the roster passed to `installCommunityPersonas` is derived from what the community index actually publishes (the fetched `personas/community/index.json` entries) rather than the hardcoded `builtins.Names()` list. Because the roster is derived from the index at fetch time, it self-heals as the index grows — no per-release roster maintenance. Option A (publishing built-in lenses into the community channel behind a model-agnostic gate) was considered and rejected: it would require a null/agnostic-model carve-out in a schema whose entire purpose is model-binding, and it would duplicate content that already ships correctly via the embedded built-in scaffold (`runInit`/`initTargets`), which this decision leaves untouched. Existing on-disk community personas already installed by prior runs must remain untouched (`installCommunityPersonas`'s existing "already installed — leaving it untouched" guard at `init.go:139` must continue to apply unchanged).
- **Constraints:** This story is independent of the resolver/lock machinery built in Stories 1-6 — it shares only the epic's "close 19.6 debt" motivation, not a technical dependency. It must not touch the resolver, binding, or lock format. It must preserve the existing all-or-nothing rollback behavior in `installCommunityPersonas` (`init.go:96`) and the never-overwrite-existing-file guard. Whatever roster/index shape is chosen, both `init.go:47` and `quickstart.go:102` must consume the same shared source of truth so they cannot drift again the way TD-006/TD-007 did.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None (shares the epic's "close 19.6 debt" motivation with Stories 1-6, but has no hard technical dependency on the resolver/lock work) |

## Success Criteria (SMART Format)

- **Specific:** Online `atcr init` and `atcr quickstart` (without `--offline`) install a working, non-empty set of community personas against the real shipped `personas/community/index.json`, and neither command prints a `%q not found in community index — skipping` warning for any name in the roster actually being requested.
- **Measurable:** A test exercising `installCommunityPersonas` (or its reconciled equivalent) against the real `personas/community/index.json` asserts at least one persona is installed and zero skip-warnings are emitted, run for both the `init.go:47` and `quickstart.go:102` call sites so the fix is proven shared rather than duplicated.
- **Achievable:** Option B reuses the existing `installCommunityPersonas` machinery (fetch, rollback, never-overwrite guard) unchanged — the fix is scoped to what roster gets passed in (index-derived instead of `builtins.Names()`), not to the install mechanics themselves.
- **Relevant:** Directly closes TD-011, the deferred 19.6 HIGH, and satisfies AC7 verbatim: a working, non-noisy community persona set on online init/quickstart, backward-compatible with existing on-disk personas.
- **Time-bound:** Deliverable within this sprint as a self-contained fix; it can be sequenced in parallel with or independently of Stories 1-6 since it touches `cmd/atcr/init.go` and `cmd/atcr/quickstart.go` exclusively, not the resolver package.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [07-01](../acceptance-criteria/07-01-working-nonempty-community-roster.md) | Online init/quickstart Install a Working, Non-Empty Community Roster | Integration |
| [07-02](../acceptance-criteria/07-02-no-misleading-skip-warnings.md) | No Misleading "Not Found in Community Index" Warnings | Integration |
| [07-03](../acceptance-criteria/07-03-shared-reconciliation-point-and-backward-compat.md) | Single Shared Reconciliation Point and Backward Compatibility | Integration |

## Original Criteria Overview

1. Online `init`/`quickstart` install a non-empty, working set of community personas against the real `personas/community/index.json`, by aligning the fetch-and-pin roster with what the index actually publishes (LOCKED: Option B — see plan.md Clarifications).
2. No misleading `%q not found in community index — skipping` warning is printed for any name in the roster actually requested by online init/quickstart against the real index.
3. The reconciliation is implemented once in a shared location consumed by both `init.go:47` and `quickstart.go:102` (or their shared `installCommunityPersonas` callee), so the two call sites cannot drift the way TD-006/TD-007 did; existing on-disk personas and the current all-or-nothing rollback/never-overwrite behavior remain unchanged.

## Technical Considerations

- **Implementation Notes:** **LOCKED: Option B.** The fix is scoped to what roster `installCommunityPersonas` (`cmd/atcr/init.go:96`) is asked to reconcile against, not to its fetch/rollback/never-overwrite mechanics, which stay unchanged. The roster passed to `installCommunityPersonas` at both call sites (`init.go:47`, `quickstart.go:102`) changes from the hardcoded `builtins.Names()` to the set the fetched community index actually publishes — e.g. by extracting names from the `[]PersonaIndexEntry` `FetchIndex` already returns inside `installCommunityPersonas`, rather than requiring a caller-supplied static list. This is naturally self-healing (a future 11th community persona installs automatically, no code change) and requires no `personas/community/index.json` schema change. The now-unreachable skip-warning path (`init.go:129`) simply stops firing in practice, since every roster member is by construction present in the index; the warning code itself can stay as defensive dead-path handling for a fetch that races with a catalog update mid-run. Must be expressed once — as a shared roster-derivation point both call sites consume — not duplicated per call site, to avoid repeating the TD-006/TD-007 drift pattern.
- **Integration Points:**
  - `cmd/atcr/init.go:47` — `init`'s call to `installCommunityPersonas`.
  - `cmd/atcr/quickstart.go:102` — `quickstart`'s call to `installCommunityPersonas`.
  - `cmd/atcr/init.go:96` — the shared `installCommunityPersonas` routine both call sites invoke; the skip-warning at `init.go:129` and the never-overwrite guard at `init.go:139`.
  - `personas/personas.go:19` — `builtins.Names()`, the 9 embedded model-agnostic built-ins.
  - `personas/community/index.json` — the shipped 10-entry model-indexed community catalog.
- **Data Requirements:** No new persisted state or lock format, and no change to `personas/community/index.json`'s contents or schema; this story only changes what set of names is reconciled against the index (from a hardcoded list to the index's own fetched entries). Existing on-disk `.atcr/personas/` community-tier files and their pins are unaffected.

### References

- [Existing Codebase Patterns to Reuse](../documentation/existing-resolver-patterns.md) — the two-call-site drift risk (TD-006/TD-007) and why AC7 must be fixed in one shared location.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Aligning the roster with the index silently changes what persona set a fresh `init`/`quickstart` installs (the 10 model-indexed personas, not the 9 familiar built-in names), surprising users who expected the built-in names as their community tier. | Medium | Document the roster change in the command's help text/README and in the CHANGELOG entry for this sprint; cover the new expected roster with an explicit test asserting the installed name set. |
| Deriving the roster from the fetched index at call time (rather than a static list) means the installed set can change between runs if the index changes upstream, which is a deliberate self-healing property but is still a behavior change from today's (broken) fixed-roster attempt. | Low | Document that the community tier tracks the published index; cover with a test that adding/removing an index entry changes the next run's installed set without a code change. |
| Fixing only one of `init.go`/`quickstart.go` and not the other repeats the exact TD-006/TD-007 drift pattern the 19.6 postmortem already flagged. | High | Implement the reconciliation as a single shared roster/index source consumed by both call sites (or fixed inside `installCommunityPersonas` itself), and add a test that runs the same assertion against both `init` and `quickstart` code paths. |
| A test asserting "zero skip-warnings" against the real index accidentally masks a legitimate future warning (e.g., a genuinely missing persona added to the roster by mistake) if the test is too loose. | Low | Scope the test to the exact roster this story installs by design, not to "any roster," so a future intentional warning is not silently swallowed by an overly broad assertion. |

---

**Created:** July 08, 2026 06:01:13PM
**Status:** Draft - Awaiting Acceptance Criteria
