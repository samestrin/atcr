# Plan 29.0: Anti-Slop Persona (Simon) & Content Marketing

## Metadata
- **Plan Type:** feature
- **Last Modified:** 2026-07-17

## Plan Overview
**Plan Type:** feature
**Plan Goal:** Ship a new community-registry persona (`simon`) hyper-focused on detecting AI-generated code bloat — tautological comments, unnecessary abstractions, defensive-programming overkill, and dead/hallucinated code — fully wired into the project's persona-authoring test gate, and pair it with a marketing outline positioning ATCR as the free alternative to paid "slop cleanup" services.
**Target Users:** Persona ecosystem maintainers/contributors (author the persona); engineering team leads evaluating ATCR (consume the blog outline).
**Framework/Technology:** Go 1.25, `gopkg.in/yaml.v3`, `testify`, the existing `personas/community/` embed-based registry.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated `/create-user-stories @.planning/plans/active/29.0_anti_slop_persona/`
- **Estimated Count:** 3 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/29.0_anti_slop_persona/`

## Feature Analysis Summary
Engineering teams using AI coding assistants accumulate "slop" — tautological comments, unnecessary factories/interfaces for single structs, defensive-programming overkill, and dead code paths — that costs real money to clean up manually (the epic cites a team charging $10k/week for exactly this). ATCR already ships a model-indexed community persona registry (13 personas as of Sprint 19.6) with a mature, test-enforced authoring contract: a persona is a YAML + Markdown prompt + patch fixture triple, registered in a JSON index and a Go test roster. This plan adds a 14th persona, `simon`, whose prompt is scoped exclusively to AI-authorship bloat rather than general correctness/security/performance, and pairs it with the already-drafted marketing outline at `.planning/product/content/blog/slopfix-ai-code-bloat.md`.

Codebase discovery surfaced two corrections to the original epic text: (1) the fixture location for a **community** persona is `personas/community/testdata/`, not `personas/testdata/` (that path is built-in-only); (2) the epic's 3 stated tasks omit a mandatory 4th integration point — registering `simon` in the hand-maintained `communityPersonas` roster inside `personas/community_test.go`. Without that roster entry, `simon` is still runtime-resolvable (the embed-based accessors in `personas/community.go` list the directory directly) but is invisible to every fixture/differentiation/index-registration gate test, which would defeat the epic's own AC3 ("a passing fixture test proves the persona successfully identifies slop").

## Technical Planning Notes
- **Persona unit:** `personas/community/simon.yaml` + `personas/community/simon.md`, modeled directly on `personas/community/sonny.yaml` / `sonny.md`. Requires a concrete `provider`/`model` binding per the registry schema (even though slop detection isn't inherently model-specific) — pick a single model at task-decomposition time; VendorToken reuse across personas is permitted (e.g. `gene` and `milo` both use `gpt`).
- **Fixture:** `personas/community/testdata/simon_fixture.patch` — a synthetic unified-diff planting a known instance of AI-authored bloat (e.g. a pointless single-implementation interface plus a tautological comment), sized to trigger the persona without resembling legitimate business logic.
- **Category word:** must be a single lowercase word, distinct from the 13 already claimed (`coupling`, `logic`, `contract`, `validation`, `race`, `leak`, `complexity`, `type`, `dependency`, `observability`, `secret`, `duplication`, `invariant`) — e.g. `bloat`. That exact word must also appear in `simon.md`'s prompt text.
- **Test roster + index registration:** append `simon` to the `communityPersonas` slice in `personas/community_test.go` (Slug/VendorToken/Category) and add a matching entry to `personas/community/index.json` (provider/model must byte-match the YAML; non-empty `tasks`/`tags`).
- **Differentiation gate:** `simon`'s `## Role` + `## Focus` token-set must stay under the 0.85 Jaccard similarity threshold against all 13 existing personas — keep the language specific to AI-authorship artifacts, not generic code quality, to avoid drifting close to `dax` (test-coverage skeptic) or `sasha`/security-flavored personas.
- **Blog outline:** `.planning/product/content/blog/slopfix-ai-code-bloat.md` already exists (committed under Sprint 19.6, before this epic), fully covering the Slopfix hook, the `simon` pitch, a before/after code example, and a call to action. This plan treats it as a review/refresh item, not fresh authorship.
- **Dependencies:** Epic 19.6 (Community Registry Hub) is satisfied — the embed-based registry and its authoring test gates already exist (13 personas shipped); Epic 23.0 (Human Names for Personas) is honored by the human name `simon`, consistent with the existing roster (`sonny`, `milo`, `gene`, `orson`, …). No outstanding dependency work remains.

## Documentation References
- [persona-yaml-and-prompt-authoring.md](documentation/persona-yaml-and-prompt-authoring.md) [CRITICAL] — `simon.yaml`/`simon.md` authoring contract: yaml.v3 strict-schema decoding and the text/template prompt-rendering pattern every community persona follows.
- [test-gate-and-fixture-verification.md](documentation/test-gate-and-fixture-verification.md) [CRITICAL] — how the `personas/community_test.go` roster and embedded-set gates use testify `assert`/`require` to verify simon's fixture, differentiation, category uniqueness, and index registration.

Full index: [documentation/README.md](documentation/README.md)

## Implementation Strategy
Author the persona triple by extending the existing `sonny`-style pattern rather than inventing new structure; wire it into the two registration points (`community_test.go` roster, `index.json`) in the same pass so the fixture-gate suite exercises it immediately; verify locally with `go test ./personas/...` before considering the persona complete; then review the existing blog outline against the final persona details (exact command syntax, category word) and refresh only what has drifted.

## Recommended Packages
No high-ROI packages identified — this is pure content authoring (YAML/Markdown/JSON + a small Go test-roster append) using only existing dependencies (`gopkg.in/yaml.v3`, `github.com/stretchr/testify`).

## User Story Themes

### Theme 1 — Author the `simon` Persona Unit
Write `personas/community/simon.yaml` and `personas/community/simon.md` per the authoring contract in `docs/personas-authoring.md`: required agent-binding fields, mandatory prompt sections (`## Role`, `## Focus`, `## Scope`, `## Severity Rubric`, `## Output Format` with the exact 7-column contract), all required template tokens, exactly one vendor-guidance citation, and a Focus section hyper-focused on tautological/apologetic AI comments, unnecessary design patterns applied to simple logic, defensive-programming overkill, and dead/hallucinated code paths.

### Theme 2 — Fixture Authoring & Test-Gate Integration
Write `personas/community/testdata/simon_fixture.patch` (synthetic diff planting known AI-slop) and register `simon` in both `personas/community_test.go`'s `communityPersonas` roster and `personas/community/index.json`, so the full existing fixture/differentiation/distinct-category/index-registration test suite covers the new persona and passes (`go test ./personas/...` green).

**Extended Scope:** the roster + `index.json` registration goes beyond the epic's three stated tasks. Codebase discovery identified it as a mandatory integration point — without it `simon` is runtime-resolvable but invisible to every fixture/differentiation/index gate test, which would defeat the epic's own fixture-test acceptance criterion.

### Theme 3 — Blog Post Outline Verification & Refresh
Review the existing `.planning/product/content/blog/slopfix-ai-code-bloat.md` against the final `simon` persona (exact category word, confirmed CLI invocation, any framing changes from Themes 1–2) and refresh only what has drifted — no new outline authoring required.

## Planning Success Criteria
- `simon.yaml` + `simon.md` exist in `personas/community/` and pass `TestCommunityPersonas_PromptStructure`, `TestCommunityPersonas_SlugConsistency`, and `TestCommunityPersonas_Differentiation`.
- `simon_fixture.patch` exists at `personas/community/testdata/simon_fixture.patch` and `TestCommunityPersonas_FixtureAndPromptCategory` passes for `simon`.
- `simon` is registered in the `communityPersonas` Go roster and `personas/community/index.json`, and `TestCommunityIndex_Registration` / `TestCommunityPersonas_DistinctCategories` / `TestCommunityPersonas_DistinctTaskScoping` all pass with `simon` included.
- `go test ./personas/...` is fully green with `simon` present.
- `.planning/product/content/blog/slopfix-ai-code-bloat.md` accurately reflects the shipped `simon` persona.

## Risk Mitigation
- **Category or Jaccard collision with an existing persona:** mitigated by choosing a distinct category word (e.g. `bloat`) up front and drafting Focus bullets specific to AI-authorship artifacts, verified against both gates before marking the work done.
- **Silent omission of the `communityPersonas` roster registration** (the highest-risk gap identified in codebase discovery — the persona would be runtime-functional but untested): mitigated by making roster + index registration an explicit story (Theme 2), not an implicit sub-step of "fixture testing."
- **Redundant blog authorship:** mitigated by scoping Theme 3 as review/refresh of the already-existing outline rather than new writing.

## Next Steps
1. `/find-documentation @.planning/plans/active/29.0_anti_slop_persona/`
2. `/create-documentation @.planning/plans/active/29.0_anti_slop_persona/`
3. `/create-user-stories @.planning/plans/active/29.0_anti_slop_persona/`
4. `/create-acceptance-criteria @.planning/plans/active/29.0_anti_slop_persona/`
5. `/design-sprint @.planning/plans/active/29.0_anti_slop_persona/`
6. `/create-sprint @.planning/plans/active/29.0_anti_slop_persona/`
