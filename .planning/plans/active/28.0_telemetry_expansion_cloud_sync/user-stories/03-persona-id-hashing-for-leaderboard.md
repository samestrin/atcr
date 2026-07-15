# User Story 3: Persona ID Hashing for the Persona Leaderboard

**Plan:** [28.0: Telemetry Expansion & Cloud Sync](../plan.md)

## User Story

**As an** `atcr.dev/dashboard` backend consumer aggregating the crowdsourced Persona Leaderboard
**I want** each scorecard record to carry a cryptographically hashed Persona ID in a dedicated telemetry/cloud-sync schema
**So that** I can aggregate which community personas (Epic 19.6) are empirically most effective across runs, without the existing Epic 10.0 public leaderboard export's privacy guarantees being weakened or bypassed to get there

## Story Context

- **Background:** `internal/scorecard/export.go` already owns the codebase's only anonymization boundary for the public leaderboard export (Epic 10.0): `PublicRecord` (line ~35) is a documented allowlist ("anything not here ... cannot leak"), `scrubField` (line ~321) is defense-in-depth PII stripping, and `AnonymizeRecord`/`ScrubPublicRecord` (line ~143) is the single ingestion-time scrub point. This is a settled, prior-clarified design (`docs/scorecard.md` Privacy Model, `clarifications-15.1_leaderboard-cost-na-rendering-Q3`), not something this story is allowed to touch. The epic's literal text asks to "conditionally bypass `scrubField`... replacing them with cryptographic hashes when telemetry is active" — that would be a runtime toggle weakening an existing privacy guarantee and is explicitly rejected by the plan's Technical Planning Notes and Risk Mitigation #1.
- **Assumptions:** The on-disk `Record` struct (`internal/scorecard/scorecard.go:52`) already carries the raw persona identity in its `Reviewer` field (mapped to `PublicRecord.Persona` today via `AnonymizeRecord`). A one-way, deterministic hash (SHA-256, per plan.md's Go-stdlib-only package list) of that raw value is sufficient to let the backend correlate repeated runs of the same persona without recovering the original string. No new external package is needed.
- **Constraints:** Must NOT modify the behavior, signature, or output of `scrubField`, `PublicRecord`, `AnonymizeRecord`, `ScrubPublicRecord`, or `runLeaderboardExport` (`cmd/atcr/leaderboard.go:156`) — the existing Epic 10.0 leaderboard `--export` path's allowlist and scrubbing behavior must be byte-for-byte unchanged, verified by a regression test. The new hashed-Persona-ID field must live on a separate, explicitly-named schema/function so the two paths cannot be confused or accidentally merged in a future refactor.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None (can be built independently of the telemetry-ping and `--sync-cloud` stories; the `--sync-cloud` payload story will consume this hashing function once it exists) |

## Success Criteria (SMART Format)

- **Specific:** A new, separately-named function (e.g. `HashPersonaID`) and an accompanying telemetry-scoped record/schema type in `internal/scorecard` produce a deterministic SHA-256 hash of a run's Persona ID, without adding a bypass flag to `scrubField` or altering `PublicRecord`'s allowlist.
- **Measurable:** Given the same raw Persona ID input, the hash output is identical across repeated calls and across process restarts (no per-run salt); given two different Persona IDs, the outputs differ; a unit test asserts both properties plus that the raw Persona ID string never appears in the hashed output.
- **Achievable:** Reuses the Go stdlib `crypto/sha256` package (already available, no new dependency) and follows the existing "scrub/hash once, at ingestion" shape established by `AnonymizeRecord`.
- **Relevant:** Directly satisfies epic AC "The exported scorecard schema includes Persona ID hashing for the Persona Leaderboard" and unblocks the backend's ability to rank personas per the epic's stated objective.
- **Time-bound:** Deliverable within the current sprint alongside the other four plan.md user story themes; no external dependency blocks start.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-hashed-persona-id-function.md) | Deterministic Hashed-Persona-ID Function | Unit |
| [03-02](../acceptance-criteria/03-02-telemetry-persona-schema.md) | Dedicated Telemetry Persona Schema Type | Unit |
| [03-03](../acceptance-criteria/03-03-leaderboard-export-regression.md) | Existing Leaderboard Export Path Byte-for-Byte Regression | Integration |
| [03-04](../acceptance-criteria/03-04-hash-property-unit-tests.md) | Hash Determinism, Uniqueness, and Non-Reversibility Unit Tests | Unit |

## Original Criteria Overview

1. A new hashing function/schema is added to `internal/scorecard` that produces a deterministic, non-reversible hash of a run's Persona ID, kept fully separate from `PublicRecord`/`scrubField`/`AnonymizeRecord`/`ScrubPublicRecord`.
2. A regression test asserts the existing Epic 10.0 `--export` (leaderboard) path's output is byte-for-byte unchanged after this story's changes land (same `PublicRecord` allowlist, same `scrubField` behavior, no new fields leaked onto that schema).
3. A unit test on the new hashing path asserts determinism (same input -> same hash), uniqueness (different input -> different hash), and non-reversibility (raw Persona ID string never appears in the hash output or any log/error message on that path).

## Technical Considerations

- **Implementation Notes:** Add the hashing function (e.g. `HashPersonaID(raw string) string`) alongside the existing anonymization functions in `internal/scorecard/export.go` (or a new `internal/scorecard/telemetry.go` file if keeping it out of `export.go` reduces the risk of future accidental coupling to `PublicRecord`), but give it a distinct name and doc comment explicitly stating it is NOT part of the `PublicRecord` allowlist path. Source the raw Persona ID from `Record.Reviewer` (`internal/scorecard/scorecard.go:52`), the same field `AnonymizeRecord` already reads — do not add a new field to `Record` for this.
- **Integration Points:** This hashing function is a pure, stateless building block consumed later by the `--sync-cloud` payload story (Story 4) and potentially by the usage-telemetry-ping story (Story 1) if persona ranking data ever rides the ping — this story only needs to deliver the hashing primitive and its own schema/output shape, not wire it into the CLI command flow.
- **Data Requirements:** Output is a hex-encoded SHA-256 digest (or equivalent fixed-length deterministic hash), string type, safe to log and transmit. No raw Persona ID, Reviewer, or Model value may appear alongside the hash in the new schema's exported fields.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Hashing path gets implemented as a flag on `scrubField`/`PublicRecord` instead of a separate schema, silently weakening the Epic 10.0 leaderboard export's documented privacy guarantee | High | Explicit constraint in this story and a regression test (AC #2) asserting the existing `--export` path's output is unchanged; code review checks no bypass flag was added to `scrubField` |
| A future refactor merges the new hashing schema back into `PublicRecord` because the two look similar | Medium | Distinct naming, a doc comment on the new function stating it is a separate boundary, and a comment cross-reference in `PublicRecord`'s doc noting the telemetry path is intentionally separate |
| Non-deterministic hashing (e.g. accidentally seeded with a per-run salt) breaks the backend's ability to correlate repeated runs of the same persona | Medium | Unit test explicitly asserts determinism across repeated calls with the same input |

---

**Created:** July 15, 2026
**Status:** Draft - Awaiting Acceptance Criteria
