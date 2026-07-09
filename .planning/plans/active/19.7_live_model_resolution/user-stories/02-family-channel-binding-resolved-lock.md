# User Story 2: Family/Channel Binding & Resolved Lock

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr end user who has installed community personas
**I want** each persona to declare a logical model family/channel binding that resolves to a concrete slug recorded as a lock
**So that** my reviews run a deterministic, reproducible model every time, while the persona itself can still advance to newer models later without me hand-editing slugs

## Story Context

- **Background:** Epic 19.6 shipped a static `model` slug pinned directly on each persona (e.g., `claude-opus-4.8`). This story introduces the resolution layer's foundational data model: a logical family/channel binding (e.g., `anthropic/claude-opus@stable`) that atcr resolves to a concrete slug, persisted as a "lock." All other stories in this epic — the hybrid resolver, `personas upgrade` reporting, `models check`, and the major-bump gate — depend on this lock existing and being read at review time. Per `documentation/existing-resolver-patterns.md`, `internal/registry/persona.go:47`'s `ResolvePersona` prompt-resolution chain (project > registry/pinned-community > `_base` > embedded) must never be touched by this layer; the lock is strictly upstream of it and answers only "which model slug," never "which prompt."
- **Assumptions:** 19.6's existing pinned `model` value seeds the initial lock with zero migration required — no persona needs re-authoring for this story to land. The lock is read as a static value on the review path; no live endpoint call happens during a review, only during an explicit future `atcr personas upgrade` (Story 4/Theme 4, out of scope here).
- **Constraints:** New binding/lock fields on `PersonaIndexEntry` (`internal/personas/search.go:14`) must follow the existing `omitempty` + permissive-decode convention (no `KnownFields`/`DisallowUnknownFields`) so old-shape `index.json` payloads installed before this epic still decode without error. Any new lock field surfaced in `index.json` must either be exempted from 19.6's AC7 exact-match gate (`personas/community_test.go:TestCommunityIndex_Registration`, `internal/personas/search_test.go:TestCommunityIndex_ProviderModelMatchesYAML` / `TestVerifyCommunityIndex_FailsOnMismatch`) or kept in sync by the same generation path that writes `index.json`, since that gate currently enforces `index.json` provider/model/description == source YAML verbatim.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** Every installed persona unit has a family/channel binding field and a resolved-lock field on disk; `internal/fanout/review.go`'s `renderAgent` builds the `llmclient.Invocation` from the locked slug read via `internal/registry/config.go`'s `AgentConfig.Model`, with zero network/endpoint calls anywhere on the review code path.
- **Measurable:** 100% of personas installed or upgraded via `InstallUnit`/`Upgrade` (both routed through `internal/personas/unit.go:95`'s `writePersonaUnit`) persist the same binding+lock shape consistently; `go test ./...` covers install, upgrade, and review-path invocation with the new fields present.
- **Achievable:** 19.6's pinned `model` string becomes the initial lock value directly — no new resolution logic, no external API call, and no schema migration of existing installs is required for this story.
- **Relevant:** This is the schema/data-model foundation every other story in Epic 19.7 (hybrid resolver, upgrade reporting, `models check`, major-bump gate) reads from or writes to; without it, none of those stories have a lock to operate on.
- **Time-bound:** Deliverable within this sprint's first implementation phase, ahead of the resolver logic (Theme 3) that depends on the binding field existing.

## Acceptance Criteria Overview

1. `PersonaIndexEntry` (and the corresponding on-disk persona unit written by `writePersonaUnit`) gains a family/channel binding field and a resolved-lock field, both `omitempty` and permissively decoded, with 19.6's pinned `model` value seeding the initial lock for every existing persona with zero migration required.
2. The review path (`internal/fanout/review.go`'s `renderAgent`, via `internal/registry/config.go`'s `AgentConfig.Model`) reads only the locked slug — never the binding — and makes no endpoint/network call to resolve it; the model used in a review is fully deterministic given the on-disk lock.
3. `index.json` generation keeps any new lock-related field consistent with the source YAML so 19.6's AC7 exact-match gate (`TestCommunityIndex_ProviderModelMatchesYAML`, `TestVerifyCommunityIndex_FailsOnMismatch`) either passes unchanged or is deliberately and narrowly exempted for the new field, without weakening its existing provider/model/description checks.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.7_live_model_resolution/`_

## Technical Considerations

- **Implementation Notes:** Add the binding and lock fields as `omitempty`-tagged additions to `PersonaIndexEntry` (`internal/personas/search.go:14`), matching the exact permissive-decode convention 19.6 already established for `Provider`/`Model`. Seed the lock from the persona's existing `model` value at read/write time so no separate migration step or backfill command is needed. Route persistence of the lock through `internal/personas/unit.go:95`'s `writePersonaUnit`, the shared paired-write tail already used by both `InstallUnit` and `Upgrade`, so both code paths stay in sync by construction rather than by convention.
- **Integration Points:**
  - `internal/personas/search.go:14` (`PersonaIndexEntry`) — add binding/lock fields, `omitempty`, permissive decode.
  - `internal/registry/persona.go:47` (`ResolvePersona`) — must NOT be modified; this story's binding/lock layer sits strictly upstream and never touches prompt resolution.
  - `internal/registry/config.go` (`AgentConfig.Model`) — the field through which the locked slug reaches the wire.
  - `internal/fanout/review.go` (`renderAgent`) — builds the `llmclient.Invocation` from `AgentConfig.Model`; must read the lock as a static value with no resolver invocation on this path.
  - `internal/personas/unit.go:95` (`writePersonaUnit`) — shared paired-write tail for `InstallUnit` and `Upgrade`; the lock should flow through here for consistent persistence.
  - `personas/community_test.go` (`TestCommunityIndex_Registration`) and `internal/personas/search_test.go` (`TestCommunityIndex_ProviderModelMatchesYAML`, `TestVerifyCommunityIndex_FailsOnMismatch`) — 19.6's AC7 exact-match gate that any new `index.json` field must respect or be explicitly exempted from.
- **Data Requirements:** A family/channel binding value (e.g., `anthropic/claude-opus@stable`) and a resolved concrete slug (the lock) per persona, both persisted on-disk in the persona unit and reflected in `index.json` per the existing YAML-source-of-truth convention.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A new lock field added to `index.json` breaks 19.6's AC7 exact-match gate against source YAML, causing spurious CI failures on every existing community persona. | High | Keep the new field in sync via the same generation path that writes `index.json` (per `writePersonaUnit`), or scope the exact-match gate's assertions to explicitly exclude the new field rather than loosening its existing provider/model/description checks. |
| A future maintainer adds resolver logic directly into the review path (`renderAgent`) for convenience, reintroducing an endpoint call mid-review and breaking reproducibility. | High | Enforce via test coverage that the review path never invokes network code; keep the lock as a plain static field read, with resolution logic physically confined to `personas upgrade` (Theme 4) and never imported by `internal/fanout`. |
| Divergent persistence logic between `InstallUnit` and `Upgrade` causes the lock field to be written inconsistently (e.g., present after install but dropped after upgrade, or vice versa). | Medium | Route all lock persistence through the single shared `writePersonaUnit` tail (`internal/personas/unit.go:95`) rather than duplicating write logic in each caller. |
| Old-shape `index.json` payloads installed before this epic fail to decode once new fields are added, breaking existing installs. | Medium | Follow 19.6's established `omitempty` + permissive-decode (no `KnownFields`/`DisallowUnknownFields`) convention exactly, and add a regression test loading a pre-epic `index.json` fixture. |

---

**Created:** July 08, 2026 06:01:13PM
**Status:** Draft - Awaiting Acceptance Criteria
