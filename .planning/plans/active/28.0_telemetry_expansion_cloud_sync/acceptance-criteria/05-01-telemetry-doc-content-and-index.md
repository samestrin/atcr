# Acceptance Criteria: `docs/telemetry.md` Content Coverage and `docs/README.md` Index Link

**Related User Story:** [05: Telemetry Privacy Documentation](../user-stories/05-telemetry-privacy-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (GitHub Flavored Markdown) | New file, single-directory docs/ tree |
| Test Framework | `go test` (Go standard testing package) | `cmd/atcr/docs_audit_test.go` audits the file's prose against the live command tree |
| Key Dependencies | None (pure documentation, no new package) | Content must describe real Stories 1-4 behavior, not aspirational behavior |

## Related Files
- `docs/telemetry.md` - create: new document covering the telemetry event schema, opt-out mechanisms, Persona ID hashing, and `--sync-cloud`/`ATCR_API_KEY` cloud-sync flow with its distinct auth exit code
- `docs/README.md` - modify: add a new entry for `docs/telemetry.md` under the existing "Benchmarking & observability" section (alongside `scorecard.md` and `metrics.md`)
- `cmd/atcr/docs_audit_test.go` - reference only (no code change): `TestDocsIndexCoversEveryDoc` (~line 468) and `TestDocsClaimedFlagsAreReal` (~line 587) are the gates this AC's file additions must satisfy

## Happy Path Scenarios

**Scenario 1: `docs/telemetry.md` documents the anonymous usage ping schema**
- **Given** Story 1 has implemented `internal/telemetry`'s fire-and-forget `{event, lang, lines, status}` payload
- **When** a reader opens `docs/telemetry.md`
- **Then** the document states the exact event schema fields (`event`, `lang`, `lines`, `status`), that it fires on `review`/`reconcile` completion, that it is fail-open/non-blocking, and explicitly lists what is never collected (source code, file paths, file contents)

**Scenario 2: `docs/telemetry.md` documents both opt-out surfaces**
- **Given** Story 2 implements `ATCR_TELEMETRY=0` and `atcr config set telemetry false`
- **When** a reader looks for how to disable telemetry
- **Then** the document has a dedicated "Opt-Out" section documenting `ATCR_TELEMETRY` (accepted values and that it is read once at root-command construction; the enabled-state semantics, i.e. `0`/`false` disables, unset/`true` enables — the inverse of `ATCR_DISABLE_AST_GROUPING`) and `atcr config set telemetry false` (with an example command and a note that it persists to `.atcr/config.yaml`), and states that the two are OR'd (either disables, neither re-enables the other)

**Scenario 3: `docs/telemetry.md` documents Persona ID hashing for the leaderboard**
- **Given** Story 3 adds a deterministic, non-reversible `HashPersonaID` function
- **When** a reader reads the "Persona Leaderboard Data" section
- **Then** the document explains that the Persona ID transmitted for leaderboard ranking is a one-way SHA-256 hash (not the raw persona/reviewer string), that it is deterministic (same input always hashes the same way, enabling cross-run correlation) but non-reversible, and that this hashing path is kept separate from the existing `--export`/`PublicRecord` allowlist

**Scenario 4: `docs/telemetry.md` documents `--sync-cloud` and its auth failure exit code**
- **Given** Story 4 registers `--sync-cloud` on `review`/`reconcile` and introduces a dedicated `exitAuth` exit code
- **When** a reader reads the "Cloud Sync" section
- **Then** the document states that setting the `` `--sync-cloud` `` flag pushes the complete anonymized scorecard (including hashed Persona ID and time/credits saved metrics) to the configured cloud endpoint, authenticated via `ATCR_API_KEY` as a `Bearer` token, and that a missing or invalid key exits with the dedicated auth exit code (distinct from the generic usage-error exit code) rather than a silent failure

**Scenario 5: `docs/telemetry.md` is linked from `docs/README.md`**
- **Given** `docs/README.md`'s "Benchmarking & observability" section currently lists `benchmark.md`, `scorecard.md`, `metrics.md`, and `logging.md`
- **When** `docs/telemetry.md` is added to the repo
- **Then** `docs/README.md` gains a new bullet linking `telemetry.md` in that same section, using the same `[Title](telemetry.md) — one-line description.` bullet style as its neighbors

## Edge Cases

**Edge Case 1: Env var boolean-direction ambiguity**
- **Given** `ATCR_TELEMETRY`'s enabled-direction semantics are the inverse of the existing `ATCR_DISABLE_AST_GROUPING` precedent
- **When** the opt-out section is written
- **Then** the doc explicitly calls out the inverse direction (`ATCR_TELEMETRY=0`/`false` disables; unset or truthy enables) so a reader familiar with `ATCR_DISABLE_AST_GROUPING` does not assume the same polarity

**Edge Case 2: Flag idiom must match the audit regex exactly**
- **Given** `TestDocsClaimedFlagsAreReal` only recognizes the exact "`` `--x` `` flag" idiom (backtick-wrapped flag name immediately followed by the word "flag")
- **When** `--sync-cloud` is referenced in `docs/telemetry.md`
- **Then** at least one occurrence uses the literal phrasing `` `--sync-cloud` flag `` so the audit test's regex captures and validates it against the live command tree

## Error Conditions

**Error Scenario 1: File created but not linked from the index**
- **Given** `docs/telemetry.md` exists on disk without a corresponding link in `docs/README.md`
- **When** `go test ./cmd/atcr/... -run TestDocsIndexCoversEveryDoc` runs
- **Then** the test fails with an error naming `docs/telemetry.md` as unlinked — this AC is not satisfied until the link is added

**Error Scenario 2: Documented flag name does not match the compiled tree**
- **Given** the doc uses `` `--sync-cloud` flag `` idiom but Story 4 registered the flag under a different name (e.g. a typo or an unregistered flag)
- **When** `go test ./cmd/atcr/... -run TestDocsClaimedFlagsAreReal` runs
- **Then** the test fails, naming the undocumented/mismatched flag — this AC requires the flag name to be verified against the actual Story 4 implementation before finalizing prose

## Performance Requirements
- **Response Time:** Not applicable — static documentation, no runtime execution path.
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** The doc must not include a real or example `ATCR_API_KEY` value, real API tokens, or any live endpoint credential; only placeholder values (e.g. `ATCR_API_KEY=<your-key>`) are permitted in examples.
- **Input Validation:** N/A (documentation only); however, the doc's description of validation behavior (e.g. exit code on invalid key) must match Story 4's actual implemented behavior, not an aspirational description.

## Test Implementation Guidance
**Test Type:** UNIT (existing Go test suite; no new test code required for this AC — it is satisfied by the doc content and index link, verified by pre-existing tests)
**Test Data Requirements:** None — verification is `go test ./cmd/atcr/... -run TestDocs` against the finalized docs/ tree plus a manual read-through comparing doc claims to Stories 1-4's merged implementation (flag names, env var names, exit code value).
**Mock/Stub Requirements:** None.

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./cmd/atcr/... -run TestDocsIndexCoversEveryDoc` passes
- [ ] `go test ./cmd/atcr/... -run TestDocsClaimedFlagsAreReal` passes
- [ ] No linting errors (markdown lint / repo formatting conventions, if configured)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `docs/telemetry.md` documents the `{event, lang, lines, status}` schema and what is never collected
- [ ] `docs/telemetry.md` documents `ATCR_TELEMETRY` and `atcr config set telemetry` with correct value semantics and OR'd behavior
- [ ] `docs/telemetry.md` documents Persona ID hashing (deterministic, non-reversible, separate from `--export`)
- [ ] `docs/telemetry.md` documents `--sync-cloud`/`ATCR_API_KEY`/Bearer auth and the dedicated auth exit code
- [ ] `docs/README.md` links `telemetry.md` under "Benchmarking & observability"

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Each documented flag/env var/exit code cross-checked against the actual merged Stories 1-4 implementation, not the plan's proposed names
