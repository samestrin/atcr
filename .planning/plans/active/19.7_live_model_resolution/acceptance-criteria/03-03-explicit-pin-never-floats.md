# Acceptance Criteria: Explicit-Slug Pin Resolves Unchanged and Never Floats Regardless of Channel

**Related User Story:** [03: Hybrid Resolver (Alias / Created-Timestamp / Explicit-Pin)](../user-stories/03-hybrid-resolver-alias-created-timestamp-explicit-pin.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go resolver strategy-selection short-circuit: explicit pin check runs first, before any catalog lookup | Per story's Implementation Notes: "explicit pin short-circuits first; else check the 7-persona alias table; else fall back to the `created`-timestamp scan" |
| Test Framework | `testing` + `httptest.NewServer`, two-snapshot comparison test | Asserts identical output across two catalog snapshots where the vendor-prefix newest member differs |
| Key Dependencies | Story 2's binding schema (explicit-pin field on the persona binding) — assumed available on `PersonaIndexEntry` or the on-disk persona unit | No catalog network call occurs on this path at all |

## Related Files
- `internal/personas/catalog.go` - create: strategy-selection entry point that checks the explicit-pin field first and returns it unconditionally before evaluating alias or vendor-prefix strategies
- `internal/personas/catalog_test.go` - create: a test resolving the same pinned persona against two distinct catalog snapshots (`catalog_snapshot.json` and a second in-test fixture with a newer vendor-prefix member) and asserting byte-identical resolved slugs
- `internal/personas/testdata/catalog_snapshot.json` - create: baseline fixture; the second comparison snapshot for this AC's test may be constructed inline in the test file (a modified in-memory copy) rather than a second checked-in file
- `internal/personas/unit.go` - reference only (`unit.go:95`, `writePersonaUnit`): the resolved slug this AC covers is what eventually gets written into the persona's lock field — this AC does not modify the write path, only proves the resolver's output is stable

## Happy Path Scenarios
**Scenario 1: Pinned persona resolves to its pinned slug unchanged**
- **Given** a persona bound with an explicit-slug pin (e.g. `deepseek/deepseek-v4-pro`) regardless of what family/channel fields are also present
- **When** the resolver is invoked with this persona's binding and any catalog model list
- **Then** it returns the pinned slug exactly, performing no alias lookup and no `created`-timestamp scan

**Scenario 2: Pin is invariant across two catalog snapshots with a different "newest" vendor member**
- **Given** the same pinned persona resolved once against catalog snapshot A (where `deepseek/deepseek-v4-pro` is the newest DeepSeek entry) and once against catalog snapshot B (where a fictitious newer `deepseek/deepseek-v5` entry exists with a later `created` timestamp)
- **When** the resolver runs against each snapshot in turn
- **Then** both resolutions return the identical pinned slug `deepseek/deepseek-v4-pro` — proving the pin never floats onto the newer vendor entry even though the `created`-timestamp scan would have picked it for an unpinned persona in the same family

**Scenario 3: Pin overrides channel regardless of `@stable` vs `@latest`**
- **Given** a pinned persona whose binding also carries a channel value of `@latest`
- **When** the resolver runs
- **Then** the pinned slug is still returned unchanged — channel is irrelevant once an explicit pin is present, per the story's Constraints ("an explicit-slug pin is a first-class, always-available third path — it must never be re-resolved or floated regardless of which channel is otherwise in effect")

## Edge Cases
**Edge Case 1: Pin field present alongside a family that would otherwise route through the alias table**
- **Given** a persona with both an explicit pin and a family value that matches one of the 7 alias-covered vendors (e.g. a pin override on an Anthropic-family persona)
- **When** the resolver runs
- **Then** the pin takes precedence — the alias table is never consulted, confirming strategy-selection order (pin → alias → created-timestamp)

**Edge Case 2: Pin field present alongside a family that would otherwise route through the `created`-timestamp scan**
- **Given** glenna (or delia/quinn) bound with an explicit pin instead of the default family/channel binding
- **When** the resolver runs against a catalog snapshot where a newer `z-ai/` (or `deepseek/`/`qwen/`) entry exists
- **Then** the pinned slug is returned, not the newer vendor-prefix match — this is the specific case the story's second Potential Risk calls out as high-impact

**Edge Case 3: Pin is an empty string (misconfigured binding)**
- **Given** a persona binding where the pin field is present but is an empty/whitespace-only string
- **When** the resolver evaluates strategy selection
- **Then** an empty pin is treated as "no pin set" and control falls through to the alias/`created`-timestamp strategies (not silently returned as a valid empty slug) — this prevents a zero-value struct field from masquerading as an intentional pin

## Error Conditions
**Error Scenario 1: Pin field is present but not a syntactically plausible slug (no `/`)**
- Error message: `"invalid pin %q for persona %q: expected vendor/model slug"`
- HTTP status / error code: N/A (library validation error) — caught before the pin is ever written into a lock or used in a request, per Story 2's binding validation (this AC only asserts the resolver rejects it rather than passing it through blindly)

## Performance Requirements
- **Response Time:** Pin short-circuit is an O(1) field check with zero I/O; must be faster than either the alias or `created`-timestamp paths since it runs first in the selection switch
- **Throughput:** N/A — single-persona resolution, no batch requirement beyond the two-snapshot comparison test running in under 50ms total

## Security Considerations
- **Authentication/Authorization:** N/A — no network call occurs on the pin path
- **Input Validation:** The pin string is untrusted only in the sense that it originates from a community persona's YAML/index metadata; this resolver does not re-validate slug syntax beyond the basic plausibility check in Error Scenario 1 — deeper validation is Story 2's binding-schema responsibility, referenced here for completeness

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** One pinned persona binding per test case; two catalog snapshots (baseline fixture + one modified in-memory variant) differing only in which vendor-prefix entry has the newest `created` timestamp
**Mock/Stub Requirements:** `httptest.NewServer` for the baseline snapshot; the comparison snapshot can be served by a second, separate `httptest.NewServer` instance within the same test

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A pinned persona resolves to its exact pinned slug, unaffected by catalog contents
- [ ] The two-snapshot comparison test proves identical output when the vendor-prefix "newest" member differs between snapshots
- [ ] Pin precedence is proven against both the alias-table path and the `created`-timestamp path
- [ ] An empty/whitespace pin falls through to the next strategy rather than resolving to an empty slug

**Manual Review:**
- [ ] Code reviewed and approved
