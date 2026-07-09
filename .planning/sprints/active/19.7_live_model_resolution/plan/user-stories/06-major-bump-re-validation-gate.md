# User Story 6: Major-Bump Re-Validation Gate

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr maintainer running `atcr personas upgrade` when a persona's bound model family crosses a major version boundary (e.g. `4.x` → `5.x`)
**I want** the upgrade to require the persona's existing fixture to still pass and to surface an explicit "prompt tuned for the prior major — verify" flag before the lock advances, while a same-major minor advance (e.g. `4.8` → `4.9`) continues to auto-lock without friction
**So that** a major vendor bump can never silently carry a persona's lock forward across a boundary where the prompt's tuning may no longer hold, while routine minor advances stay effortless

## Story Context

- **Background:** Story 4 wires `atcr personas upgrade` to re-resolve each persona's family/channel binding and, on a real change, advance the lock. This story inserts a safety gate into that exact write path, specifically for major-version transitions. Per `documentation/semver-version-comparison.md`, atcr already depends on `golang.org/x/mod/semver` for persona upgrade detection — `internal/personas/upgrade.go`'s `isNewer(local, remote string) bool` is the sole existing call site, using `semver.IsValid` and `semver.Compare` to decide whether `remote` is newer than `local`. `isNewer` answers "is this newer," not "is this a major jump" — this story does not reimplement version parsing, it adds one classification on top of the same package: `semver.Major(v)` extracts just the major-version prefix (e.g. `"v4"` from `"v4.9.2"`), so comparing `semver.Major(local)` against `semver.Major(remote)` gives the trigger condition, `semver.Major(remote) != semver.Major(local)`, that distinguishes a major jump from a minor advance.
- **Assumptions:** Story 4's upgrade flow already computes both the current locked version and the newly resolved version before deciding whether to write the lock; this story's classification slots in at that comparison point, upstream of the write. Every persona already ships a committed `.patch` fixture exercised by `TemplateFixtureRunner` (per `documentation/existing-resolver-patterns.md`), and that fixture-gate is a hard requirement independent of the resolver — this story reuses `TemplateFixtureRunner` unchanged rather than building any new fixture mechanism.
- **Constraints:** A minor advance (major prefix unchanged) must auto-lock exactly as Story 4 already does — this gate must add zero friction to the common case. A major jump (major prefix changed) must not advance the lock unless the persona's existing fixture still passes under `TemplateFixtureRunner`; if the fixture fails, the lock must not advance and the command must report why. Critically, a passing fixture on a major bump proves only that the template still RENDERS under the new model context — it does NOT prove the prompt is still well-tuned for the new major version's behavior, capabilities, or quirks. That tuning judgment cannot be automated: this story's sole automatable output is the fixture pass/fail result plus a mandatory human-facing "prompt tuned for the prior major — verify" flag, surfaced whenever a major jump occurs (regardless of fixture outcome). No prompt-quality inference, model-behavior comparison, or automatic re-tuning is in scope — that judgment call, and any future tooling to assist it, is reserved for Epic 19.8.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | User Story 3 (Hybrid Resolver), User Story 4 (Reproducible Upgrade) |

## Success Criteria (SMART Format)

- **Specific:** During `atcr personas upgrade`, when a resolved slug's version crosses a major boundary relative to the current lock (`semver.Major(remote) != semver.Major(local)`), the upgrade gates the lock write on `TemplateFixtureRunner` re-passing the persona's existing fixture and always emits a "prompt tuned for the prior major — verify" flag in the report; when the crossing is a minor advance only (`semver.Major(remote) == semver.Major(local)`), the lock auto-advances exactly as Story 4 already does, with no fixture re-run and no flag.
- **Measurable:** Tests cover all four combinations — (major bump, fixture passes), (major bump, fixture fails), (minor bump, no gate triggered), and (same major, no version change) — asserting the lock is written only when the gate is satisfied, and the "verify" flag appears in the report if and only if the transition is classified major.
- **Achievable:** Adds one `semver.Major` comparison and one conditional call to the already-existing `TemplateFixtureRunner`, with no new fixture format, no new resolver logic, and no changes to `isNewer`'s existing signature or minor-path behavior.
- **Relevant:** Directly satisfies Proposed Solution item #7 and AC6 verbatim: "A major-version model jump gates on the persona fixture re-passing and surfaces a re-tune flag; a minor jump auto-locks."
- **Time-bound:** Implemented and passing within this sprint's execution window for Plan 19.7, sequenced after Story 4's upgrade flow exists to gate.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [06-01](../acceptance-criteria/06-01-major-jump-fixture-gate-and-verify-flag.md) | Major-Version Jump Gates the Lock on Fixture Re-Pass and Always Surfaces a Verify Flag | Unit |
| [06-02](../acceptance-criteria/06-02-minor-jump-auto-lock-regression-guard.md) | Minor-Version Jump Continues to Auto-Lock With Zero Added Friction (Regression Guard) | Unit |

## Original Criteria Overview

1. The upgrade flow classifies every resolved-version transition as major or minor using `semver.Major(local)` vs. `semver.Major(remote)`, reusing the existing `isNewer`/semver-compare machinery in `internal/personas/upgrade.go` rather than introducing new version-parsing logic.
2. On a classified major jump, the lock write is gated on the persona's existing `.patch` fixture re-passing under the unmodified `TemplateFixtureRunner`; a failing fixture blocks the lock advance and the command reports the block and the reason. On a classified minor jump, the lock auto-advances with no fixture re-run, matching Story 4's existing behavior unchanged.
3. Every major-jump transition — whether the fixture passes or fails — surfaces an explicit "prompt tuned for the prior major — verify" flag in the upgrade report, making clear to the human operator that a passing fixture proves only template rendering, not prompt tuning appropriateness, and that verifying the latter is a human judgment call out of this story's and this epic's automated scope.

## Technical Considerations

- **Implementation Notes:** Add a major/minor classification step in `internal/personas/upgrade.go`, immediately alongside the existing `isNewer` comparison, using `semver.Major(local)` and `semver.Major(remote)` on the version strings extracted from the resolved model slugs (e.g. `4.8` from `anthropic/claude-opus-4.8` is normalized to `v4.8` — the same `"v"`-prefixed form `isNewer` already constructs). When the classification is major, invoke the existing `TemplateFixtureRunner` against the persona's already-committed fixture before permitting the lock write in `writePersonaUnit()`; on minor, skip straight to the existing auto-lock path unchanged. The "verify" flag is a report-only annotation — it does not gate anything by itself beyond triggering the fixture check; the fixture result is the actual write-gate.
- **Integration Points:** `internal/personas/upgrade.go` (`isNewer`, the lock-write decision point Story 4 introduces), `golang.org/x/mod/semver` (`semver.Major`, already vendored), `TemplateFixtureRunner` (existing, unchanged — reused as-is per `documentation/existing-resolver-patterns.md`), Story 4's before→after reporting in `cmd/atcr/personas.go` (extended to carry the fixture-block reason and the "verify" flag), Story 3's hybrid resolver (supplies the candidate resolved slug this gate classifies).
- **Data Requirements:** No new schema — this story reads the same local/remote version strings Story 4's upgrade flow already computes (extracted from resolved model slugs and normalized for semver) and the same committed `.patch` fixture every persona already ships. The upgrade report gains two new pieces of per-persona output on a major jump: the fixture pass/fail result and the "prompt tuned for the prior major — verify" flag; no new persisted lock field is introduced.

### References

- [Existing Codebase Patterns to Reuse](../documentation/existing-resolver-patterns.md) — `isNewer()`/`TemplateFixtureRunner` reuse and the lock-write decision point.
- [Semantic Version Comparison](../documentation/semver-version-comparison.md) — `semver.Major` classification on the same normalized version strings `isNewer` uses.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A fixture passing on a major bump is misread (by a user or by future tooling) as proof the prompt is still well-tuned for the new major version, when it only proves the template renders | High | Always pair a passing fixture on a major jump with the mandatory "prompt tuned for the prior major — verify" flag in the report — the flag is unconditional on major jumps, not conditional on fixture failure, so a pass can never look like a silent all-clear |
| Major/minor classification drifts from `isNewer`'s existing newer/not-newer semantics (e.g. a pre-release or malformed version string is classified inconsistently between the two checks), causing the gate to fire on the wrong transitions | Medium | Reuse the exact same normalized version strings `isNewer` already constructs (via `semver.IsValid` validation) as the input to `semver.Major`, rather than re-deriving or re-parsing versions independently; cover invalid/malformed version strings with a test asserting the gate degrades safely (no lock write) rather than misclassifying |
| A future change accidentally treats a failing major-jump fixture as non-blocking, or skips the gate entirely for `--all`/batch upgrade runs, silently advancing a persona's lock across an unverified major boundary | High | Sequence the fixture-repass check strictly before the lock write for every persona examined, including in `--all` runs; add a test where a failing fixture on a major bump must leave that persona's lock unchanged while still reporting the would-be new slug, the fixture failure, and the reason the lock did not advance |

---

**Created:** July 08, 2026 06:01:13PM
**Status:** Draft - Awaiting Acceptance Criteria
