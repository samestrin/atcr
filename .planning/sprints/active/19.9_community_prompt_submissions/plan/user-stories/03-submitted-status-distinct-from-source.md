# User Story 3: `submitted` Status Distinct from `Source`/Provenance

**Plan:** [19.9: Community Prompt Submissions (Intake & Curation)](../plan.md)

## User Story

**As a** maintainer curating incoming persona submissions
**I want** a submitted prompt's fixture-passing-but-unvetted state tracked as its own `submitted` status, separate from its `built-in`/`community`/`project` `Source` provenance
**So that** I can tell "where a persona came from" and "whether it has been vetted" apart, and graduate a submission into the vetted library by flipping only its vetting state — never mutating or overloading provenance

## Story Context

- **Background:** `PersonaMeta.Source` (`internal/personas/list.go:19,22`) already carries three de facto values (`"built-in"`, `"community"`, `"project"` — the last set by `listProject` at line 113) despite its comment implying a closed two-value enum. Story 2's `submit` flow (Theme 2) produces a fork+PR of a fixture-passing persona; once that persona lands, it needs a status marker recording that it passed the local fixture gate but has not yet been maintainer-reviewed. The epic's `original-requirements.md` Refinements section records this exact terminology collision as a resolved user-confirm item: an earlier draft conflated the new unvetted tier with the CLI's existing "community-contributed" language, and the resolution was to introduce `submitted` as an orthogonal status axis rather than a fourth `Source` value.
- **Assumptions:** Attribution/provenance metadata (submitter identity, source PR/commit reference, submission timestamp) travels alongside the `submitted` marker rather than inside `Source`. The marker is local/on-disk (or PR-embedded) status bookkeeping, not a new hosted service — consistent with AC3's GitHub-PR-native constraint. No existing call site (`List`, `ListTiers`, `listCommunity`, `listProject`) currently reads or writes anything named `submitted`, so this story is purely additive.
- **Constraints:** Must not add a fourth string value to `Source`'s existing `"built-in"|"community"|"project"` set, and must not change the meaning or output of any existing `Source` value for current callers/tests. Any new on-disk marker file must be written through an existing atomic-write helper (`internal/personas/unit.go:writeFileAtomic`, line 180, 0600 perms, symlink-refusing, sibling-temp-then-rename; or `internal/atomicfs.WriteFileAtomic`, line 24, 0644) — never a bare `os.WriteFile`. The marker must live outside the vetted `personas/community/` tree so graduation stays an explicit, human-reviewed promotion rather than something the automated `submit` path performs implicitly.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (fixture gate must pass before a `submitted` marker is ever written), Story 2 (fork+PR flow that produces the submission the marker describes) |

## Success Criteria (SMART Format)

- **Specific:** A `submitted` status concept exists as a distinct field/marker (not a `Source` value) that records fixture-passing-but-unvetted state plus attribution metadata for a submission, and `Source` remains unchanged for every existing and new persona.
- **Measurable:** New unit tests assert `Source` stays exactly `"built-in"`, `"community"`, or `"project"` after a submission is created (no fourth value observed anywhere in `internal/personas`), and a separate test asserts the `submitted` marker/status is readable and carries at least submitter attribution + a passing-fixture indicator.
- **Achievable:** Implemented as an additive marker (e.g., a sidecar status file or frontmatter key written via an existing atomic-write helper) plus, if needed, a non-breaking additive field on a struct used only by the submit/status path — no changes to `PersonaMeta.Source`'s type, values, or the signatures of `List`, `ListTiers`, `listCommunity`, or `listProject`.
- **Relevant:** Directly resolves AC2 and the epic's recorded terminology-collision finding, which blocks graduation semantics (Theme 4) from being built on a corrupted provenance field.
- **Time-bound:** Deliverable within this plan's sprint, landing before or alongside Story 4 (maintainer graduation), which depends on a clean `submitted` marker to flip during promotion.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-submitted-status-is-not-a-source-value.md) | `submitted` Status Is Not a Fourth `Source` Value | Unit |
| [03-02](../acceptance-criteria/03-02-submitted-marker-attribution-and-atomic-persistence.md) | `submitted` Marker Carries Attribution and Persists Only via Atomic Write | Unit |
| [03-03](../acceptance-criteria/03-03-marker-stored-outside-community-tree-and-list-extension-point.md) | Marker Lives Outside `personas/community/` and `List` Extension Point Leaves Existing Output Unchanged | Unit/Integration |

## Original Criteria Overview

1. A `submitted` status/marker concept is introduced as a field or sidecar artifact distinct from `PersonaMeta.Source`; no existing `Source` value, call site, or test is modified in meaning or behavior.
2. The `submitted` marker records attribution/provenance metadata (at minimum: submitter identity and confirmation the local fixture gate passed) and is persisted only via an existing atomic-write helper (`writeFileAtomic` or `atomicfs.WriteFileAtomic`), never a bare file write, and lives outside `personas/community/`.
3. `List` (or a clearly scoped extension point built for future use) can, in principle, surface `submitted` status without altering its existing `Source`-based output for `built-in`/`community`/`project` rows; existing `personas list` output and tests are unaffected.

## Technical Considerations

- **Implementation Notes:** Add the `submitted` concept as either (a) a new field on a submission-specific struct (not `PersonaMeta`) populated only along the `submit` code path, or (b) a sidecar marker file/frontmatter key read on demand — whichever keeps `internal/personas/list.go`'s existing structs and functions untouched in shape. Do not add a `Submitted bool` or `Status string` field to `PersonaMeta` itself unless it can be proven additive and zero-value-safe for every existing caller; prefer a separate type to avoid any risk of the two axes being conflated later.
- **Integration Points:** `internal/personas/list.go` (`PersonaMeta`, `List`, `ListTiers`, `listCommunity`, `listProject` — read-only integration point, extend without modifying existing behavior); `internal/personas/unit.go:writeFileAtomic` (line 180) and/or `internal/atomicfs/atomic.go:WriteFileAtomic` (line 24) for persistence; the Story 2 `submit` command, which is the sole producer of a `submitted` marker.
- **Data Requirements:** Attribution metadata to capture: submitter identity/handle, source persona name/version, submission timestamp, and a fixture-pass confirmation flag. Storage location must sit outside `personas/community/` (e.g., a per-submission marker under a status-tracking path) so graduation remains a distinct, explicit maintainer step (Story 4) rather than an automatic side effect of this story's write.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Implementer folds `submitted` into `Source` as a fourth string value (the exact mistake the epic's Refinements section already flagged and resolved) | High | Add an explicit unit test asserting `Source` never takes any value outside `"built-in"|"community"|"project"`; code review checklist references this story's Constraints section and the epic's terminology-collision resolution directly |
| A bare `os.WriteFile` is used for convenience instead of the existing atomic-write helpers, reintroducing TOCTOU/partial-write risk this codebase has already closed elsewhere | Medium | Reuse `writeFileAtomic`/`atomicfs.WriteFileAtomic` exclusively; add a test that the marker write refuses a pre-existing symlink at the destination, mirroring existing `writeFileAtomic` test coverage |
| Marker placed inside or copied into `personas/community/`, causing graduation to become an implicit automated action instead of an explicit maintainer review | Medium | Keep the marker's storage path outside `personas/community/` by construction (path constant lives with the submit/status code, not the vetted-library path); add a test asserting the marker path never resolves under `personas/community/` |

---

**Created:** July 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
