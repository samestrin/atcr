# Acceptance Criteria: Determinism via Checked-In Catalog Snapshot (No Live Network by Default)

**Related User Story:** [05: `atcr models check` Drift Report](../user-stories/05-atcr-models-check-drift-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Checked-in JSON fixture (`internal/personas/testdata/catalog_snapshot.json`) + snapshot-loading function in `cmd/atcr/models.go` | Default comparison source; no `HTTPClient`/network call in the default `check` path |
| Test Framework | Go `testing` + `testify`, including a repeated-run determinism assertion | Confirms byte-for-byte or field-for-field identical output across repeated invocations against the same fixture |
| Key Dependencies | `encoding/json` (stdlib) for snapshot parsing; no HTTP client dependency in this AC's default path | Snapshot format mirrors the catalog entry shape already used elsewhere in `internal/personas` (family/channel/slug/`expiration_date`) |

### Related Files (from codebase-discovery.json)
- `internal/personas/testdata/catalog_snapshot.json` — create: the checked-in deterministic catalog fixture used by `atcr models check`'s default comparison path, containing entries with `slug`, `family`, `channel`, and (where applicable) non-null `expiration_date` fields sufficient to exercise all three drift conditions in tests.
- `cmd/atcr/models.go` — create: a snapshot-loading function that reads `internal/personas/testdata/catalog_snapshot.json` (or an equivalent embedded/packaged path resolved relative to the binary/module) by default, with no `HTTPClient`/network call anywhere in `check`'s default `RunE` path.
- `cmd/atcr/models_test.go` — create: a determinism test that runs `atcr models check` (and `--json`) twice against an identical fixture/lock state and asserts byte-identical (or field-identical) output and exit code both times; a regression test asserting no outbound HTTP call occurs during the default `check` invocation.
- `documentation/catalog-snapshot-fixture.md` — reference: fixture discipline, zero-live-network CI requirement, and refresh command rationale.
- `cmd/atcr/main.go:202` (`newRootCmd` `AddCommand` list) — reference: the registration point for the `models` command family (modified in AC 05-01).

## Happy Path Scenarios
**Scenario 1: Repeated runs against identical state produce identical output**
- **Given** a fixed set of installed personas with fixed resolved locks and the checked-in `catalog_snapshot.json`
- **When** `atcr models check` is run twice in succession with no state change between runs
- **Then** both runs produce identical stdout content and identical exit codes — no ordering nondeterminism, no timestamp-dependent content, no network-dependent variance

**Scenario 2: Default invocation makes zero outbound network calls**
- **Given** `atcr models check` is invoked with no additional flags
- **When** the command executes
- **Then** no HTTP request is made to any OpenRouter (or other) endpoint — the comparison is sourced entirely from the local `catalog_snapshot.json` file

**Scenario 3: Snapshot fixture round-trips through the loader without loss**
- **Given** the checked-in `catalog_snapshot.json` containing representative entries for the newer-member, deprecation, and missing-slug scenarios
- **When** the snapshot-loading function parses it
- **Then** every entry's `slug`, `family`, `channel`, and `expiration_date` (where present) fields are available to the drift-comparison logic exactly as authored in the fixture

## Edge Cases
**Edge Case 1: CI environment with no network access still succeeds**
- **Given** a CI runner with outbound network access blocked or unavailable
- **When** `atcr models check` runs
- **Then** the command completes successfully (assuming valid local state), proving the default path has no hidden network dependency

**Edge Case 2: Catalog snapshot entry has a null/absent `expiration_date`**
- **Given** a catalog snapshot entry with no `expiration_date` key (or an explicit `null`)
- **When** compared against an installed persona's lock matching that slug
- **Then** no deprecation condition is reported for that persona — only a non-null `expiration_date` triggers the deprecation condition, consistent with the story's "deprecation (non-null `expiration_date`)" definition

## Error Conditions
**Error Scenario 1: Catalog snapshot file is missing from the expected path**
- Error message: `"failed to load catalog snapshot: %w"` (wrapping the underlying file-open error)
- HTTP status / error code: exit code `2` (per AC 05-03) — a missing snapshot is a command failure, not a "no conditions found" state

**Error Scenario 2: Catalog snapshot file contains malformed JSON**
- Error message: `"failed to parse catalog snapshot: %w"` (wrapping the underlying `encoding/json` error)
- HTTP status / error code: exit code `2`

## Performance Requirements
- **Response Time:** Snapshot load-and-parse completes in well under 50ms for a fixture of realistic size (dozens to low hundreds of catalog entries), since it is a single local file read with no network round trip.
- **Throughput:** Snapshot is loaded once per invocation and reused across all installed-persona comparisons, not re-read/re-parsed per persona.

## Security Considerations
- **Authentication/Authorization:** Not applicable — the default path requires no API key or credential since it never contacts OpenRouter.
- **Input Validation:** The snapshot JSON is parsed via standard `encoding/json` with a bounded struct shape; unexpected/extra fields are ignored rather than causing a decode failure, keeping the fixture forward-compatible with future catalog-field additions.

## Test Implementation Guidance
**Test Type:** UNIT (snapshot loader parsing + field extraction) + INTEGRATION (repeated-run determinism, no-network regression guard)
**Test Data Requirements:** The checked-in `catalog_snapshot.json` fixture itself, sized to cover at least one entry per drift condition (a family with a newer stable member, an entry with a non-null `expiration_date`, and installed-persona locks referencing a slug absent from the snapshot); a corrupted/missing-file variant for the error-condition tests (written to a temp path, not the real checked-in fixture).
**Mock/Stub Requirements:** An `http.RoundTripper` (or equivalent HTTP client seam) that fails the test immediately if invoked during the default `check` path, proving no live network call occurs.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `internal/personas/testdata/catalog_snapshot.json` exists, is checked into the repo, and covers all three drift-condition scenarios
- [ ] `atcr models check`'s default path makes zero outbound network calls, verified by a test that fails if any HTTP call is attempted
- [ ] Repeated runs against identical installed-persona/lock state produce identical stdout and exit code every time
- [ ] Only a non-null `expiration_date` triggers the deprecation condition; a null/absent value does not

**Manual Review:**
- [ ] Code reviewed and approved
