# Acceptance Criteria: Structured `--model`/`--provider` Filtering (No Free-Text Fallback)

**Related User Story:** [03: Model-Aware Search and Discovery via `--model`/`--provider`](../user-stories/03-model-aware-search-and-discovery.md)
**Design References:** [cli-search-flags.md](../documentation/cli-search-flags.md), [persona-yaml-schema.md](../documentation/persona-yaml-schema.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package function (`internal/personas/search.go`) | Extends `Search()` or adds a sibling function/options struct |
| Test Framework | Go `testing` package, table-driven tests | `httptest.NewServer` for mock `index.json` responses |
| Key Dependencies | stdlib `strings`; existing `PersonaIndexEntry`/`FetchIndex` from Story 2 | No new third-party dependency |

### Related Files (from codebase-discovery.json)
- `internal/personas/search.go` (`PersonaIndexEntry`, `Search`) — modify: add structured `Provider`/`Model` matching (e.g., via a `SearchOptions` struct or additional parameters), combinable with the existing keyword/description substring path.
- `cmd/atcr/personas.go` (line ~218 `newPersonasSearchCmd`) — modify: read `--model`/`--provider` flag values and thread them into the extended `Search` call.
- `internal/personas/search_test.go` — create: table-driven tests asserting structured-field-only matching and the false-positive-rejection case (a model string appearing only in `Description` does not satisfy `--model`).
- `docs/personas-install.md` — modify: document `--model`/`--provider` filters.

**Field-semantics note (per LOCKED decision Q3):**
- `--model`/`--provider` match the **structured** `Model`/`Provider` fields only — never free text (`Name`/`Description`).
- `--provider` matches the **routing-endpoint key** (a key in the registry Providers map, e.g. `openrouter`, `synthetic`) — it is NOT the vendor/brand. Users **discover by `--model`**: the vendor/brand token (e.g. `deepseek`, `anthropic`) lives in the `Model` string, so "I have DeepSeek → `--model deepseek`" is the intended discovery path.
- Consequence for the library: every library persona's `model` value MUST contain the recognizable vendor/brand token so a `--model <vendor>` substring query matches it (e.g. a DeepSeek-bound persona must have `model` like `deepseek-chat`/`deepseek-coder`, not an opaque alias). This is an authoring constraint on the in-repo index, verified by the AC7 gate in AC 02-02.


## Happy Path Scenarios
**Scenario 1: Filter by `--model` matches structured Model field**
- **Given** a mock `index.json` containing a persona entry with `Model: "deepseek-chat"` and `Description: "General-purpose reviewer"`
- **When** `atcr personas search --model deepseek` is run
- **Then** the persona is returned because its structured `Model` field contains "deepseek" (case-insensitive substring), and the CLI exits 0

**Scenario 2: Filter by `--provider` matches structured Provider field**
- **Given** a mock `index.json` containing a persona entry with `Provider: "openai"`
- **When** `atcr personas search --provider openai` is run
- **Then** the persona is returned because its structured `Provider` field matches, independent of any keyword argument

**Scenario 3: `--model` and `--provider` combine as AND filters**
- **Given** a mock index with one persona having `Provider: "deepseek", Model: "deepseek-coder"` and another having `Provider: "openai", Model: "deepseek-coder"` (same model string, different provider)
- **When** `atcr personas search --model deepseek-coder --provider deepseek` is run
- **Then** only the first persona (matching both fields) is returned

## Edge Cases
**Edge Case 1: Model substring matches a longer model name (documented substring semantics)**
- **Given** a persona with `Model: "gpt-4o"` and `--model gpt-4` is supplied
- **When** the search runs
- **Then** the persona IS returned (substring match is deliberate, not exact-match), and this behavior is documented in a code comment on the matching function per the story's Risk table (near-miss model strings)

**Edge Case 2: Empty index / no structured matches**
- **Given** a mock index with no entries whose `Provider`/`Model` match the supplied flag(s)
- **When** `atcr personas search --model nonexistent-model` is run
- **Then** an empty result slice is returned (not an error) — consistent with existing `Search()` no-error-on-zero-results behavior

**Edge Case 3: Case-insensitive matching**
- **Given** a persona with `Model: "DeepSeek-Chat"`
- **When** `atcr personas search --model deepseek` is run (lowercase)
- **Then** the persona is returned

**Edge Case 4: `--provider` is the routing-endpoint key, not the vendor (per Q3)**
- **Given** a persona with `Provider: "openrouter"` (routing endpoint) and `Model: "deepseek-chat"` (vendor token in the model string)
- **When** `atcr personas search --provider deepseek` is run
- **Then** the persona is NOT returned by `--provider deepseek` because `deepseek` is the vendor (it lives in `Model`), and `--provider` only matches the routing-endpoint key `openrouter`; the correct discovery query is `--model deepseek`. This confirms vendor discovery is a `--model` concern, not a `--provider` one.

## Error Conditions
**Error Scenario 1: Free-text Description match must NOT satisfy `--model`**
- **Given** a persona with `Model: "gpt-4"` and `Description: "Tuned for deepseek workflows"` (mentions "deepseek" only in free text)
- **When** `atcr personas search --model deepseek` is run
- **Then** the persona is NOT returned — asserting the structured-field-only contract explicitly named in the story's Measurable criterion
- HTTP status / error code: N/A (filter-only, not an error path; command still exits 0 with an empty/"No personas found" result)

## Performance Requirements
- **Response Time:** Filtering is an in-memory linear scan over the already-fetched index (no additional network calls); negligible overhead versus existing keyword search for index sizes in the hundreds of entries.
- **Throughput:** N/A (single-user CLI invocation, not a service).

## Security Considerations
- **Authentication/Authorization:** N/A — read-only filtering against a public community index already fetched via `FetchIndex`; no new trust boundary introduced.
- **Input Validation:** `--model`/`--provider` flag values are trimmed and lowercased before comparison; no shell/path interpretation of flag values (they are never passed to exec or filesystem paths).

## Test Implementation Guidance
**Test Type:** UNIT (search.go filter logic) + INTEGRATION (personas.go RunE wiring against `httptest.NewServer` + `ATCR_PERSONAS_URL` override)
**Test Data Requirements:** A mock `index.json` fixture with at least 3 entries covering: (a) matching Model, (b) matching Provider only, (c) a persona whose Description mentions a model string that must NOT be matched by `--model`
**Mock/Stub Requirements:** `httptest.NewServer` serving the mock `index.json`; `personasClient`/`ATCR_PERSONAS_URL` override per existing E2E test pattern in this codebase

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `--model` and `--provider` flags filter using only `PersonaIndexEntry.Provider`/`Model` fields
- [x] A model string appearing only in `Description` does not satisfy `--model`
- [x] `--model` and `--provider` combine as AND filters when both supplied
- [x] Matching is case-insensitive and substring-tolerant, documented in code comments

**Manual Review:**
- [ ] Code reviewed and approved
