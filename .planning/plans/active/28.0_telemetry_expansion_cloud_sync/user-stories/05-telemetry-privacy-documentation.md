# User Story 5: Telemetry Privacy Documentation

**Plan:** [28.0: Telemetry Expansion & Cloud Sync](../plan.md)

## User Story

**As a** privacy-conscious `atcr` user or engineering manager evaluating the tool for a team
**I want** a dedicated `docs/telemetry.md` document (linked from `docs/README.md`) plus an updated `docs/scorecard.md` Privacy Model section that plainly state what the anonymous usage ping and `--sync-cloud` push collect, how Persona IDs are hashed, and exactly how to opt out
**So that** I can make an informed adoption decision, verify the tool's privacy claims against its actual behavior, and confidently disable telemetry for my team without guesswork

## Story Context

- **Background:** `docs/scorecard.md`'s existing "Privacy Model" section (around line 277) documents the Epic 10.0 `--export` allowlist/`scrubField` privacy guarantee for the local scorecard store, but it predates this plan's background telemetry ping, Persona ID hashing, and `--sync-cloud` push — none of which are described anywhere in `docs/`. `docs/README.md` is the canonical, single-source-of-truth documentation index consumed by the website build; every doc in `docs/*.md` must be linked from it. `cmd/atcr/docs_audit_test.go` enforces two relevant invariants: `TestDocsClaimedFlagsAreReal` (~line 587) fails the build if prose documents a `` `--flag` `` that doesn't exist on the compiled command tree, and `TestDocsIndexCoversEveryDoc` (~line 468) fails the build if any `docs/*.md` file exists but isn't linked from `docs/README.md`. Stories 1-4 introduce `--sync-cloud`, `ATCR_TELEMETRY`, `ATCR_API_KEY`, and `atcr config set telemetry false` — all of which need matching documentation or CI breaks the moment those stories land.
- **Assumptions:** Stories 1-4 (telemetry client, opt-out, Persona ID hashing, `--sync-cloud`) are implemented (or their exact flag/env-var/exit-code contracts are known) by the time this story's documentation is finalized, since the docs must describe real, working behavior rather than aspirational behavior. `docs/scorecard.md`'s existing Privacy Model section structure (allowlist table, "Preserved" / "Stripped / never exported" lists) is the established house style for privacy documentation in this repo and should be mirrored, not reinvented.
- **Constraints:** Every flag/env var named using the `` `--x` `` or documented-flag idiom must be real and match `canonicalFlags()` in `cmd/atcr/docs_audit_test.go`, or `TestDocsClaimedFlagsAreReal` fails. The new `docs/telemetry.md` file must be added to `docs/README.md`'s index (a new "Benchmarking & observability" entry alongside `scorecard.md`/`metrics.md` is the natural placement) or `TestDocsIndexCoversEveryDoc` fails. Must not contradict or weaken the existing Epic 10.0 privacy guarantees described in `docs/scorecard.md`'s Privacy Model — the new content must clearly separate the pre-existing local-store/`--export` allowlist boundary from the new background-telemetry and `--sync-cloud` data paths so a reader cannot conflate them.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (telemetry client), Story 2 (opt-out), Story 3 (Persona ID hashing), Story 4 (`--sync-cloud`) — this story documents their finalized, real behavior |

## Success Criteria (SMART Format)

- **Specific:** A new `docs/telemetry.md` file documents the telemetry event schema, what data is and is not collected, the `ATCR_TELEMETRY=0` and `atcr config set telemetry false` opt-out paths, Persona ID hashing for the leaderboard, and the `--sync-cloud`/`ATCR_API_KEY` cloud-sync push with its distinct auth-failure exit code; `docs/scorecard.md`'s Privacy Model section is updated to cross-reference it and clarify the boundary between the existing `--export` allowlist and the new telemetry/cloud-sync data paths.
- **Measurable:** `go test ./cmd/atcr/... -run TestDocs` passes, specifically `TestDocsIndexCoversEveryDoc` (new file linked in `docs/README.md`) and `TestDocsClaimedFlagsAreReal` (every documented flag is real); a manual read-through confirms no claim in `docs/telemetry.md` describes behavior that contradicts the actual implementation from Stories 1-4.
- **Achievable:** Pure documentation work following an established in-repo template (`docs/scorecard.md`'s Privacy Model structure) — no code changes required beyond what Stories 1-4 already deliver.
- **Relevant:** Directly satisfies epic AC3 ("`ATCR_TELEMETRY=0` strictly disables all background telemetry" — documented and discoverable) and the plan's Objective 5 ("Privacy Documentation"); unblocks CI for Stories 1-4 since `docs_audit_test.go` fails the moment their undocumented flags/env vars land.
- **Time-bound:** Deliverable within this sprint cycle as the final story, sequenced after Stories 1-4 so it documents finalized, real flag/env-var/exit-code contracts rather than speculative ones.

## Acceptance Criteria Overview

1. `docs/telemetry.md` exists, is linked from `docs/README.md`'s index, and documents the telemetry event schema, opt-out mechanisms (`ATCR_TELEMETRY=0`, `atcr config set telemetry false`), Persona ID hashing behavior, and the `--sync-cloud`/`ATCR_API_KEY` flow including its distinct auth-failure exit code.
2. `docs/scorecard.md`'s Privacy Model section is updated to describe the new hashed-persona telemetry/cloud-sync data path and clearly distinguish it from the existing `--export` allowlist/`scrubField` guarantee, with a cross-link to `docs/telemetry.md`.
3. `go test ./cmd/atcr/... -run TestDocs` (covering `TestDocsIndexCoversEveryDoc` and `TestDocsClaimedFlagsAreReal`) passes with the new/updated documentation in place.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/28.0_telemetry_expansion_cloud_sync/`_

## Technical Considerations

- **Implementation Notes:** Model `docs/telemetry.md` on `docs/scorecard.md`'s existing structure: an overview of what runs and when, a "Privacy Model" section with "Preserved" / "Stripped / never exported" style lists for the telemetry payload (mirroring the `{event, lang, lines, status}` schema from Story 1), an "Opt-Out" section documenting both `ATCR_TELEMETRY=0` and `atcr config set telemetry false` with example commands, a "Persona Leaderboard Data" section explaining the hashed Persona ID (from Story 3) and why it is a one-way hash rather than a raw identifier, and a "Cloud Sync (`--sync-cloud`)" section documenting the `ATCR_API_KEY` Bearer-token flow and the distinct exit code returned on a missing/invalid key (from Story 4). Use the `` `--x` `` flag idiom exactly as `docs_audit_test.go`'s regex expects (`` `--sync-cloud` `` flag) so `TestDocsClaimedFlagsAreReal` recognizes it, and confirm `ATCR_TELEMETRY`/`ATCR_API_KEY` are referenced in the env-var idiom the test suite checks (mirror `ATCR_DISABLE_AST_GROUPING`'s existing documented-env-var pattern for style consistency, per the plan's Technical Planning Notes).
- **Integration Points:** `docs/README.md` — add a new link (Benchmarking & observability section, alongside `scorecard.md` and `metrics.md`, is the natural placement per that section's existing scope). `docs/scorecard.md`'s Privacy Model section (~line 277) — update in place, do not replace, preserving all existing Epic 10.0 allowlist/scrubbing content. `cmd/atcr/docs_audit_test.go` — no code change needed here, but its `canonicalFlags()`/`auditedMarkdown()` machinery is the acceptance gate this story must satisfy.
- **Data Requirements:** No schema/data changes — this is documentation of schemas and behavior delivered by Stories 1-4 (telemetry payload shape, hashed Persona ID field, `--sync-cloud` payload contents, auth-failure exit code value).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Documentation is written before Stories 1-4's flag names, env vars, or exit codes are finalized, producing prose that describes behavior that doesn't match the implementation | High | Sequence this story last in the plan's execution order (as reflected in its Dependencies); verify each documented flag/env var/exit code against the actual implemented code in Stories 1-4 before finalizing, not against the plan's proposed names |
| A new `docs/telemetry.md` is added but not linked from `docs/README.md`, or a documented flag doesn't exist in the compiled command tree, silently breaking CI | Medium | Run `go test ./cmd/atcr/... -run TestDocs` locally as part of this story's Definition of Done — `TestDocsIndexCoversEveryDoc` and `TestDocsClaimedFlagsAreReal` are existing, deterministic gates that catch both failure modes directly |
| The new telemetry/cloud-sync documentation is worded in a way that implies it weakens or supersedes the existing Epic 10.0 `--export` privacy guarantee in `docs/scorecard.md`, causing user confusion or a false privacy regression perception | Medium | Explicitly frame the Privacy Model update as describing a separate, additive data path (background ping / `--sync-cloud`) distinct from the local-store `--export` allowlist boundary, and cross-link rather than merge the two explanations |

---

**Created:** July 15, 2026
**Status:** Draft - Awaiting Acceptance Criteria
