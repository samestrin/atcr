# User Story 4: Reproducible Upgrade with Before→After Lock Reporting

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr maintainer running `atcr personas upgrade` (interactively or from a CI/cron job)
**I want** the command to re-resolve each persona's family/channel binding, advance its lock only when a newer slug is confirmed, and print the exact before→after resolved slug per persona (e.g. `anthony: opus-4.8 → 5.0`)
**So that** I can advance model versions deliberately and transparently, and trust that no persona's model ever changes outside this one explicit, user-initiated command

## Story Context

- **Background:** Epic 19.6 locked the reproducibility posture (Clarification C3): fetch-and-pin freezes the version, and upgrade is explicit. `internal/personas/upgrade.go:27` (`Upgrade()`) already re-fetches, validates, and version-compares before writing a persona update, and `cmd/atcr/personas.go` already implements `atcr personas upgrade` with per-name and `--all`/`--dry-run` flags, reporting a before→after *version* string today. This story extends that existing command to also resolve and report the before→after resolved *slug*, using Story 3's hybrid resolver (alias / created-timestamp / explicit-pin) and Story 2's lock schema — and it is the only place in the entire system where the resolver call and the OpenRouter models endpoint are ever touched.
- **Assumptions:** Story 2 (family/channel binding schema + resolved lock) and Story 3 (hybrid resolver) are complete and available as library calls `Upgrade()` can invoke synchronously. The existing `isNewer`/semver-compare and paired-write (`writePersonaUnit()`, `internal/personas/unit.go:95`) machinery in `upgrade.go` remains the write path; this story adds the resolver call and richer reporting in front of it, not a parallel write mechanism.
- **Constraints:** **Non-negotiable, inherited from Epic 19.6's locked C3 posture: the models endpoint and the resolver are invoked ONLY inside `atcr personas upgrade` (and its constituent `Upgrade()` call) — never on the review hot path, never as a side effect of any other command, and never automatically on a timer or on persona load.** A clean PR must never sprout new findings from an unchanged diff because a model silently floated underneath it. Every upgrade run (`--dry-run` or real) must report the resolved slug transparently, per persona, before any lock write occurs; `--dry-run` must show what would change without writing the lock. Major-version jumps additionally gate on the persona's fixture still passing (Story 6) before the lock advances — this story must not bypass that gate when it lands.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 2 (Family/Channel Binding & Resolved Lock), User Story 3 (Hybrid Resolver) |

## Success Criteria (SMART Format)

- **Specific:** Running `atcr personas upgrade <name>` (or `--all`) re-resolves that persona's family/channel binding via Story 3's resolver, compares the resolved slug against the current lock, writes the new lock only when the resolved slug differs and passes the version-advance rule, and prints a `name: old-slug → new-slug` line for every persona it examined (unchanged personas are reported as unchanged, not silently omitted).
- **Measurable:** 100% of upgrade invocations (single-name, `--all`, and `--dry-run`) produce a before→after slug line per examined persona in their output; zero test or manual run exercises resolution or a models-endpoint call from any code path other than `Upgrade()`.
- **Achievable:** Extends the existing, already-tested `Upgrade()`/`isNewer()`/`writePersonaUnit()` machinery in `internal/personas/upgrade.go` and `unit.go` with one new resolver call and one reporting change, per the documented insertion point at `upgrade.go:27`.
- **Relevant:** Directly satisfies Proposed Solution #5 and AC4 verbatim — this is the command surface that makes the resolver real and observable to the user, and the sole gate that keeps model resolution off the review hot path.
- **Time-bound:** Implemented and passing within this sprint's execution window for Plan 19.7, immediately after Story 3's resolver lands.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-upgrade-resolves-advances-lock-slug-report.md) | Upgrade Re-Resolves Binding and Advances Lock with Before→After Slug Reporting | Unit |
| [04-02](../acceptance-criteria/04-02-resolution-isolated-to-upgrade-path.md) | Resolver and Models-Endpoint Calls Occur Only Inside `Upgrade()` — Never on the Review Hot Path | Integration |
| [04-03](../acceptance-criteria/04-03-dry-run-reports-without-writing.md) | `--all` Fan-Out and `--dry-run` Report the Full Before→After Slug Set Without Writing | Integration |

## Original Criteria Overview

1. `atcr personas upgrade <name>` re-resolves the named persona's family/channel binding, compares it to the current lock, and — only on a real change passing the version-advance rule — writes the new resolved slug to the lock; the command prints an explicit `name: old-slug → new-slug` line (or an "unchanged" line) for the persona.
2. `atcr personas upgrade --all` performs the same re-resolve/compare/report/write cycle across every installed persona in one run, with one before→after (or unchanged) line per persona, and `--dry-run` shows the same report without writing any lock.
3. The resolver call and the OpenRouter models-endpoint request occur only inside this command's execution path — no other command, no persona load, and no review invocation ever triggers resolution or an endpoint call, verified by tests asserting zero endpoint calls outside `Upgrade()`.

## Technical Considerations

- **Implementation Notes:** Insert the resolver call in `internal/personas/upgrade.go:27` immediately before the existing `isNewer`/write logic, per the grounding in `documentation/existing-resolver-patterns.md`. Reuse `isNewer()` (`upgrade.go:89`) for the version-advance decision by first extracting the version suffix from each resolved model slug (e.g. `4.8` from `anthropic/claude-opus-4.8`) and normalizing it to a `v`-prefixed semver string (`v4.8`) so the existing semver comparison can classify the transition as newer, older, or unchanged. Extend the existing before→after version report in `cmd/atcr/personas.go` to also print the resolved slug, keeping the existing `--all`/`--dry-run` flag semantics unchanged. Writes continue to flow through `writePersonaUnit()` (`internal/personas/unit.go:95`) so install and upgrade stay consistent.
- **Integration Points:** `internal/personas/upgrade.go` (`Upgrade()`), `cmd/atcr/personas.go` (`upgrade` subcommand and its reporting), Story 2's lock schema on `PersonaIndexEntry` (`internal/personas/search.go:14`), Story 3's hybrid resolver function, and Story 6's major-bump fixture-repass gate (must run before this story's lock write when the semver major changes).
- **Data Requirements:** No new schema beyond Story 2's lock field — this story only adds a resolver call and reporting, it does not define the lock's shape. Report output must include, per persona: name, previous resolved slug, new resolved slug (or "unchanged"), and whether the lock was written or skipped (e.g., dry-run, or fixture-gate failure on a major bump).

### References

- [Existing Codebase Patterns to Reuse](../documentation/existing-resolver-patterns.md) — exact `Upgrade()` insertion point, `isNewer()` reuse, and the rule that the resolver must never touch the review path.
- [Semantic Version Comparison](../documentation/semver-version-comparison.md) — how `isNewer` normalizes and compares version strings, which this story extends by extracting version suffixes from resolved model slugs.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A future change accidentally calls the resolver or the models endpoint from a code path other than `Upgrade()` (e.g. persona load, review rendering), silently reintroducing the exact non-determinism 19.6's C3 rejected | High | Add a test asserting the resolver/catalog client is never constructed or invoked outside `Upgrade()`'s call chain; keep the resolver call physically isolated to the documented insertion point at `upgrade.go:27` and never import it from `internal/registry/persona.go` or `internal/fanout/review.go` |
| `--dry-run` reporting diverges from what a real run would actually write, misleading a user about the true before→after impact | Medium | Share one code path for "compute the before→after report" between dry-run and real-run modes, differing only in whether the final write executes |
| A major-version resolution result bypasses Story 6's fixture-repass gate, silently advancing the lock across a breaking prompt-compatibility boundary | High | Sequence the fixture-repass check strictly before the lock write in `Upgrade()`, and cover it with a test where a failing fixture on a major bump must leave the lock unchanged while still reporting the would-be new slug and the reason it was blocked |

---

**Created:** July 08, 2026 06:01:13PM
**Status:** Draft - Awaiting Acceptance Criteria
