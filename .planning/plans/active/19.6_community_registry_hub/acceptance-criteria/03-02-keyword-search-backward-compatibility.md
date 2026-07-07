# Acceptance Criteria: Positional Keyword Search Remains Unchanged (Backward Compatibility)

**Related User Story:** [03: Model-Aware Search and Discovery via `--model`/`--provider`](../user-stories/03-model-aware-search-and-discovery.md)
**Design References:** [cli-search-flags.md](../documentation/cli-search-flags.md), [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package function (`internal/personas/search.go`) + Cobra command (`cmd/atcr/personas.go`) | Existing keyword/description substring path must be preserved as an independent filter |
| Test Framework | Go `testing` package, table-driven tests | Regression tests against current `Search()` behavior |
| Key Dependencies | stdlib `strings`; existing `PersonaIndexEntry` | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `internal/personas/search.go` (`Search`) — modify: preserve the existing keyword-only code path (`strings.Contains` on `Name`/`Description`, case-insensitive) when no `--model`/`--provider` flags are supplied.
- `cmd/atcr/personas.go` (line ~218 `newPersonasSearchCmd`) — modify: continue accepting the positional `<keyword>` argument and preserve the existing empty-keyword guard.
- `internal/personas/search_test.go` — create: regression tests asserting `atcr personas search <keyword>` output is identical pre/post this story for keyword-only calls.
- `docs/personas-install.md` — modify: document that existing keyword search behavior is unchanged.


## Happy Path Scenarios
**Scenario 1: Existing keyword search behaves identically with no new flags**
- **Given** a mock index with personas matching various names/descriptions
- **When** `atcr personas search code-reviewer` is run (no `--model`/`--provider` flags)
- **Then** the results are exactly the set of entries whose `Name` or `Description` contains "code-reviewer" (case-insensitive), matching pre-story behavior precisely

**Scenario 2: Keyword search combined with a flag narrows results, not replaces them**
- **Given** a mock index with personas matching keyword "review" across multiple providers
- **When** `atcr personas search review --provider openai` is run
- **Then** only personas matching BOTH the keyword substring on Name/Description AND the structured `Provider` field are returned (keyword path is not disabled by the presence of a new flag)

## Edge Cases
**Edge Case 1: Keyword with no matches returns the existing "no personas found" message**
- **Given** a mock index with no entries containing the keyword
- **When** `atcr personas search zzz-no-match` is run
- **Then** stdout prints `No personas found matching "zzz-no-match"` exactly as today, and the command exits 0

**Edge Case 2: Whitespace-only keyword still triggers the existing empty-keyword guard**
- **Given** a keyword argument of `"   "` (whitespace only)
- **When** `atcr personas search "   "` is run with no `--model`/`--provider` flags
- **Then** the existing `usageError(fmt.Errorf("keyword cannot be empty"))` guard fires unchanged (personas.go:225-227 behavior preserved)

## Error Conditions
**Error Scenario 1: Regression — keyword substring semantics must not change**
- **Given** the pre-story test suite's existing keyword assertions (if any) or documented behavior of `Search()`
- **When** the extended `Search()`/filter function runs with only a keyword and no `--model`/`--provider`
- **Then** results are byte-identical in set membership to the pre-extension implementation — any deviation is a regression
- HTTP status / error code: N/A (CLI usage error path unchanged: non-zero exit via `usageError` wrapping)

## Performance Requirements
- **Response Time:** No performance regression versus current keyword-only search; still a single linear scan over the fetched index.
- **Throughput:** N/A (single-user CLI invocation).

## Security Considerations
- **Authentication/Authorization:** N/A — no change to trust boundary; keyword remains a free-text local filter over already-fetched public index data.
- **Input Validation:** Existing `strings.TrimSpace` + empty-check on keyword is preserved; no new validation weakened by the extension.

## Test Implementation Guidance
**Test Type:** UNIT (search.go regression) + INTEGRATION (personas.go RunE, keyword-only invocation)
**Test Data Requirements:** Reuse the same mock `index.json` fixture as AC 03-01 to assert keyword-only results are unaffected by the presence of unrelated `Provider`/`Model` fields on entries
**Mock/Stub Requirements:** `httptest.NewServer` + `ATCR_PERSONAS_URL` override, matching the existing E2E test pattern

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `atcr personas search <keyword>` with no new flags returns identical results to pre-story behavior
- [ ] Existing empty-keyword `usageError` guard still fires when keyword is blank and no `--model`/`--provider` are set
- [ ] Keyword filter combines with `--model`/`--provider` as an additional AND condition, not a replacement

**Manual Review:**
- [ ] Code reviewed and approved
