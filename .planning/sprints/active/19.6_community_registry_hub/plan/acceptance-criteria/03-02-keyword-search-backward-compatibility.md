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
- `internal/personas/search.go` (`Search`) — modify: preserve the existing keyword-only code path (`strings.Contains` on `Name`/`Description`, case-insensitive) when no `--model`/`--provider` flags are supplied, AND additionally match the bare positional keyword against the **structured** `Provider`/`Model` fields so a `search <vendor>` query finds a persona bound to that vendor even when the vendor token appears only in structured data. This reconciles the AC2 headline "I have DeepSeek → find the DeepSeek persona": the positional keyword becomes Name OR Description OR Provider OR Model. This is structured field matching, NOT the free-text leak AC2 forbids (AC2 forbids a model string buried in a free-text Description satisfying the *`--model` flag*; matching a bare positional keyword against a structured `Model` field is the opposite and is allowed).
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

**Scenario 3: Bare positional keyword matches a structured Model even when Name/Description do not (reconciles AC2 "I have DeepSeek")**
- **Given** a mock index containing a persona with `Name: "code-reviewer"`, `Description: "General-purpose reviewer"` (neither contains "deepseek") and structured `Provider: "openrouter"`, `Model: "deepseek-chat"`
- **When** `atcr personas search deepseek` is run (bare positional keyword, no `--model`/`--provider` flags)
- **Then** the persona IS returned because the positional keyword matches its structured `Model` field (`deepseek-chat` contains "deepseek", case-insensitive), satisfying the AC2 headline "I have DeepSeek → find the DeepSeek persona" from structured data
- **And** this is explicitly **structured matching**, NOT the free-text leak AC2 forbids: the model token was NOT required to be stuffed into `Name`/`Description`, and the `--model` flag's structured-only contract (AC 03-01) is unaffected

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
**Error Scenario 1: Regression — Name/Description keyword semantics must not change**
- **Given** index entries that carry NO structured `Provider`/`Model` (the pre-extension four-field shape)
- **When** the extended `Search()`/filter function runs with only a keyword and no `--model`/`--provider`
- **Then** results are byte-identical in set membership to the pre-extension implementation — any deviation on these entries is a regression. (The additive structured Provider/Model matching in Scenario 3 only ever *adds* matches for entries that DO carry structured fields; it never drops or alters a Name/Description match, so back-compat for legacy entries is preserved.)
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
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `atcr personas search <keyword>` with no new flags returns identical results to pre-story behavior for entries with no structured `Provider`/`Model`
- [x] Bare positional keyword ALSO matches structured `Provider`/`Model` so `search deepseek` finds a deepseek-bound persona from structured data (reconciles AC2), without requiring the token in `Name`/`Description`
- [x] Existing empty-keyword `usageError` guard still fires when keyword is blank and no `--model`/`--provider` are set
- [x] Keyword filter combines with `--model`/`--provider` as an additional AND condition, not a replacement

**Manual Review:**
- [ ] Code reviewed and approved
