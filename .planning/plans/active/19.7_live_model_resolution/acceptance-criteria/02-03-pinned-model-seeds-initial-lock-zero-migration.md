# Acceptance Criteria: 19.6's Pinned Model Values Seed the Initial Lock with Zero Migration

**Related User Story:** [02: Family/Channel Binding & Resolved Lock](../user-stories/02-family-channel-binding-resolved-lock.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | In-repo fixture data (`index.json`, community persona YAML) + regression tests | `personas/community/index.json`, `personas/community/*.yaml` |
| Test Framework | Go `testing` — extends the existing AC7 gate tests in `internal/personas/search_test.go` and `personas/community_test.go` | No new test framework |
| Key Dependencies | `gopkg.in/yaml.v3`, `encoding/json` (already used by `verifyCommunityIndex`) | |

## Related Files
- `personas/community/index.json` - modify: for any entries that gain an authored `binding` value (e.g. `anthony`: `"binding": "anthropic/claude-opus@stable"`), the existing `model` value (e.g. `"anthropic/claude-opus-4.8"`) is left byte-for-byte unchanged — proving "zero migration": the pre-story `model` value already IS the initial lock, requiring no rewrite, backfill command, or data transform to become one.
- `internal/personas/search_test.go` (`verifyCommunityIndex`, line 30) - reference/modify: the function's exact-match loop checks only `Provider`/`Model` equality against the source YAML (plus the path-escape/read checks); it does not enumerate `Binding` at all. Add a small regression test proving this explicitly (see Edge Case 1) so the exemption is asserted, not merely assumed by omission.
- `personas/community_test.go` (`TestCommunityIndex_Registration`, line 330) - modify: this test additionally checks `Description` equality (line 356-367); extend it (or add a sibling assertion) so that when an entry carries a `binding` value it is compared for equality against the source YAML's `binding:` key — keeping the new field in sync via the same test that already guards `Provider`/`Model`/`Description`, without weakening any of those three existing checks.
- `personas/community/anthony.yaml` (and, at the maintainer's discretion, the other 9 community persona YAMLs) - modify (optional, illustrative): may add a `binding:` key mirroring the eventual family/channel target for that persona; this is authoring metadata for Theme 3's resolver, not a requirement of this AC — a persona may legitimately ship no `binding` at all and still have a valid lock (its `model` value).

## Happy Path Scenarios
**Scenario 1: Existing pinned `model` value is the initial lock with no data change**
- **Given** `personas/community/anthony.yaml`'s existing `model: anthropic/claude-opus-4.8` (authored in Epic 19.6, before this epic)
- **When** this story's schema extension (AC 02-01) lands
- **Then** `anthony.yaml`'s `model` value is unchanged (`git diff` shows zero modification to that line), and it is immediately usable as the resolved lock a review runs — no command, migration script, or manual edit is required to "convert" it into a lock

**Scenario 2: `index.json` generation keeps an authored `binding` in sync with its source YAML**
- **Given** a community persona YAML that declares both `model: anthropic/claude-opus-4.8` and `binding: anthropic/claude-opus@stable`
- **When** the corresponding `index.json` entry is authored/regenerated
- **Then** the entry's `binding` value equals the YAML's `binding:` value exactly, mirroring how `provider`/`model`/`description` are already kept in sync (per `TestCommunityIndex_Registration`)

**Scenario 3: AC7's exact-match gate passes unchanged with the new field present**
- **Given** the full `personas/community/index.json` and its 10 source YAMLs after this story's changes
- **When** `go test ./personas/... ./internal/personas/...` runs (specifically `TestCommunityIndex_Registration`, `TestCommunityIndex_ProviderModelMatchesYAML`)
- **Then** both tests pass with no changes to their `Provider`/`Model`/`Description` assertions — the new `Binding` field's presence or absence does not perturb the existing exact-match checks

## Edge Cases
**Edge Case 1: `verifyCommunityIndex` does not enumerate `Binding` — confirmed by test, not by omission**
- **Given** `internal/personas/search_test.go`'s `verifyCommunityIndex` (line 30-86), which checks `e.Provider`/`e.Model` against the parsed source YAML but has no equivalent check for `Binding`
- **When** an `index.json` entry's `binding` value is deliberately made to drift from (or simply differ in presence from) its source YAML's `binding:` key, with `Provider`/`Model`/`Description` left correct
- **Then** `TestCommunityIndex_ProviderModelMatchesYAML`/`TestVerifyCommunityIndex_FailsOnMismatch` still pass (report zero problems) for that entry — proving `Binding` is exempt from this specific gate by construction, exactly as the story's Constraints section requires ("either exempted... or kept in sync by the same generation path")

**Edge Case 2: An entry authors `binding` without a corresponding resolver yet (Theme 3 not yet built)**
- **Given** an `index.json`/YAML pair with a `binding` value set ahead of Theme 3's hybrid resolver landing
- **When** the persona is installed, reviewed, or upgraded using only this story's changes
- **Then** nothing reads or acts on `binding` yet (per AC 02-02) — it is inert authored metadata; the persona continues to review using its unchanged `model`/lock value with no error, warning, or behavior change

**Edge Case 3: A persona ships no `binding` at all**
- **Given** any of the other community personas that do not (yet) declare a `binding` key
- **When** the index/YAML pair is loaded, installed, or reviewed
- **Then** `Binding` decodes as `""` and the persona's `model` value continues to serve, unmodified, as its lock — a persona is never required to declare a `binding` for this story to be satisfied

## Error Conditions
**Error Scenario 1: A future generation script writes `binding` without keeping it in sync**
- Error message: `"%s: binding mismatch — index=%q yaml=%q"` (mirroring `verifyCommunityIndex`'s existing `"%s: provider mismatch — index=%q yaml=%q"` wording), IF the maintainer opts to add a dedicated sync check per Related Files above
- HTTP status / error code: N/A — this is a `go test` failure (`TestCommunityIndex_Registration` or a new sibling check), not an HTTP path

## Performance Requirements
- **Response Time:** No runtime performance impact — this AC is fixture data plus test-suite additions, not a hot-path code change.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** Not applicable — in-repo fixture/test changes only.
- **Input Validation:** Any authored `binding` value in `index.json`/community YAML is repo-owned, reviewed content (not untrusted runtime input), so no additional runtime validation is introduced by this AC; runtime validation of a *fetched* third-party persona's `binding` remains covered by the existing `ValidateCommunityPersonaYAML`/install-time guardrails (AC 02-01) and, later, Theme 3's resolver-input validation.

## Test Implementation Guidance
**Test Type:** UNIT (fixture-backed, in-repo)
**Test Data Requirements:** The real `personas/community/index.json` and its 10 source YAMLs (no synthetic fixture needed for the "zero migration" proof — it is validated against the actual pre-existing `model` values); the existing `testdata/badindex/` fixture (used by `TestVerifyCommunityIndex_FailsOnMismatch`) is the template for an analogous `binding`-drift fixture if Edge Case 1's exemption test is added there instead of inline.
**Mock/Stub Requirements:** None — pure file-read/parse/compare, matching the existing `verifyCommunityIndex`/`TestCommunityIndex_Registration` test style.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Every existing community persona's `model` value is confirmed unchanged (byte-for-byte) by this story — the "zero migration" claim is verifiable in the diff, not just asserted in prose
- [ ] `TestCommunityIndex_Registration` and `TestCommunityIndex_ProviderModelMatchesYAML`/`TestVerifyCommunityIndex_FailsOnMismatch` pass unmodified in their `Provider`/`Model`/`Description` assertions after this story's changes
- [ ] A test demonstrates `Binding` drift/absence does not trip the existing AC7 exact-match gate (explicit exemption, not accidental omission)
- [ ] If any `binding` values are authored in this story, they are proven to match their source YAML via a test (sync-by-construction, not by convention alone)

**Manual Review:**
- [ ] Code reviewed and approved
