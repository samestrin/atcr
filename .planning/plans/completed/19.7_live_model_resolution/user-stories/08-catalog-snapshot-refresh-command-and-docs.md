# User Story 8: Catalog Snapshot Fixture, Refresh Command & Documentation

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr maintainer and CI pipeline
**I want** a checked-in OpenRouter catalog snapshot fixture, an on-demand refresh command to regenerate it, and documentation explaining the family/channel/lock model
**So that** all resolver and `models check` tests run deterministically with zero live network in CI, the fixture can be kept current as the real catalog drifts, and users understand why model resolution only advances on an explicit `atcr personas upgrade`

## Story Context

- **Background:** Stories 3 and 5 depend on reading OpenRouter catalog data, but the epic's reproducibility posture (Epic 19.6's C3 guardrail and Objective 11) forbids live network calls in CI. A checked-in JSON snapshot of `/api/v1/models` at `internal/personas/testdata/catalog_snapshot.json` backs every resolver/catalog test. Because the live catalog's model list and schema drift over time, the epic also ships a refresh command that regenerates the snapshot on demand. Separately, AC8 requires end-user documentation of the new family/channel/lock model and the reproducible-vs-upgrade behavior. This story owns those three deliverables: the fixture, the refresh command, and the documentation updates.
- **Assumptions:** Story 2's lock field and Story 3's resolver are implemented or in progress, so the fixture contents are known to cover every resolver branch. Story 5's `atcr models check` command is the natural home for a `refresh` subcommand under the same `models` family.
- **Constraints:** The fixture must be committed to version control; CI must never call OpenRouter live. The refresh command may touch the network (it is explicitly opt-in and maintainer-initiated), but its output is committed so tests remain offline. Documentation updates must not duplicate the detailed design docs in `documentation/`; they should link to them and explain user-facing behavior.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 2 (Family/Channel Binding & Resolved Lock), User Story 3 (Hybrid Resolver), User Story 5 (`atcr models check`) |

## Success Criteria (SMART Format)

- **Specific:** A checked-in catalog snapshot fixture exists at `internal/personas/testdata/catalog_snapshot.json` with sufficient entries to exercise alias passthrough, `created`-timestamp newest-in-vendor-prefix selection (including `z-ai/`), `@stable` preview/expiring exclusion, deprecation detection, missing-slug handling, and all 10 existing pinned slugs; an `atcr models refresh` command regenerates the fixture from a live `/api/v1/models` call; and `docs/personas-authoring.md` plus `docs/personas-install.md` document the family/channel/lock model and reproducible-vs-upgrade behavior.
- **Measurable:** `go test ./...` passes with every resolver/catalog test using the fixture and zero live network; running `atcr models refresh` updates the snapshot file and a test asserts the fixture round-trips through the catalog client; documentation contains explicit sections on family/channel syntax, the resolved lock, the `upgrade` command as the only path that advances the lock, and the `models check` command.
- **Achievable:** Reuses the catalog client from Stories 1/3, the `models` command family from Story 5, and the existing documentation files referenced in `codebase-discovery.json`; no new external dependency or schema change is required.
- **Relevant:** Directly satisfies AC8 and Objective 11: zero-live-network CI, deterministic tests, a maintainable snapshot, and user-facing docs for the new resolution model.
- **Time-bound:** Deliverable within this sprint as the final integration story, after Stories 2, 3, and 5 provide the schema and commands the fixture and docs support.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [08-01](../acceptance-criteria/08-01-checked-in-catalog-snapshot-coverage.md) | Checked-In Catalog Snapshot Covers Every Resolver Branch | Unit |
| [08-02](../acceptance-criteria/08-02-models-refresh-command-regenerates-snapshot.md) | `atcr models refresh` Regenerates the Snapshot from Live `/api/v1/models` | Integration |
| [08-03](../acceptance-criteria/08-03-docs-family-channel-lock-and-reproducibility.md) | Docs Document Family/Channel/Lock Model and Reproducible-vs-Upgrade Behavior | Documentation |

## Original Criteria Overview

1. A checked-in catalog snapshot fixture at `internal/personas/testdata/catalog_snapshot.json` contains enough of the `/api/v1/models` response to exercise all resolver and `models check` branches, including at least one `~`-prefixed `-latest` alias per alias-covered vendor, multiple models under the `deepseek/`, `qwen/`, and `z-ai/` prefixes, a preview/beta/exp-tokened model, a model with a non-null `expiration_date`, and all 10 of Epic 19.6's pinned slugs.
2. An on-demand refresh command (`atcr models refresh`) fetches the live `/api/v1/models` endpoint and rewrites the checked-in fixture, so maintainers can keep CI data current without editing the file by hand.
3. `docs/personas-authoring.md` and `docs/personas-install.md` are updated with sections explaining the family/channel binding syntax, the resolved lock, the fact that reviews run the lock deterministically, and that `atcr personas upgrade` is the only command that advances the lock.

## Technical Considerations

- **Implementation Notes:** The fixture is a JSON file committed under `internal/personas/testdata/` and loaded by tests with `os.ReadFile` + an `httptest.NewServer` that serves it back to the catalog client. The refresh command reuses the catalog client from Story 3 with a live `BaseURL` of `https://openrouter.ai/api/v1`, writes the fetched array to the fixture path, and records the fetch date in a top-level comment or adjacent metadata. Documentation updates should be additive sections in the existing docs rather than new files, linking back to the plan's `documentation/` folder for implementation details.
- **Integration Points:**
  - `internal/personas/catalog.go` (Story 3) — catalog client the refresh command reuses.
  - `cmd/atcr/models.go` (Story 5) — the `models` command family where `refresh` is registered alongside `check`.
  - `internal/personas/testdata/catalog_snapshot.json` — the fixture path used by resolver and `models check` tests.
  - `docs/personas-authoring.md` — documents the index-entry schema and persona contribution checklist; add the family/channel/lock model here.
  - `docs/personas-install.md` — documents install/upgrade behavior; add the before→after lock report and reproducibility note here.
- **Data Requirements:** The fixture is a snapshot of OpenRouter's `/api/v1/models` JSON response. No new persisted application state is introduced.

### References

- [Catalog Snapshot Fixture Discipline](../documentation/catalog-snapshot-fixture.md) — fixture contents, refresh command rationale, and zero-live-network CI discipline.
- [OpenRouter Catalog & Completions API](../documentation/openrouter-catalog-api.md) — the endpoint and schema the snapshot captures.
- [Existing Codebase Patterns to Reuse](../documentation/existing-resolver-patterns.md) — `HTTPClient` + `httptest` testing discipline reused by the fixture-driven tests.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| The fixture is missing a model shape needed by a future resolver branch, causing tests to pass while the implementation has an untested edge case | Medium | Require the fixture to cover every branch declared in Story 3's ACs before this story is considered done; treat fixture coverage as part of the AC8 gate. |
| The refresh command is run in CI by mistake, reintroducing live-network dependence | Medium | Document clearly that `atcr models refresh` is maintainer-initiated only; do not invoke it from tests or CI workflows; add a test that asserts the default `models check` path uses the fixture, not a live call. |
| Documentation updates drift out of sync with the actual schema or command behavior | Low | Link docs to the plan's `documentation/` source-of-truth files rather than duplicating implementation details, and list the docs sections as AC8 deliverables. |

---

**Created:** July 09, 2026 12:50:49AM
**Status:** Draft - Awaiting Acceptance Criteria
