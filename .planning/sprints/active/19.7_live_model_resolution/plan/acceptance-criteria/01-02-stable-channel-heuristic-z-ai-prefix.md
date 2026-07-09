# Acceptance Criteria: `@stable` Channel Heuristic Defined Against Live Schema, `z-ai/` Prefix Pinned

**Related User Story:** [01: Catalog Routability Spike & Stable-Channel Heuristic](../user-stories/01-catalog-routability-spike-stable-channel-heuristic.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Design spike / written heuristic definition (no resolver code lands here) | Consumed as design input by Theme 2 (family/channel binding) and Theme 3 (hybrid resolver) in later stories |
| Test Framework | MANUAL — heuristic derived by inspecting the live `/api/v1/models` response; no automated test in this story | A future story's resolver/heuristic implementation (e.g. `internal/personas/catalog.go`, not yet created) will carry its own Go unit tests against a checked-in snapshot fixture |
| Key Dependencies | `OPENROUTER_API_KEY`, `net/http` re-list of `/api/v1/models` (reusing `internal/personas/client.go:99`'s `fetch()` pattern), `personas/community/index.json` for vendor-prefix cross-check | |

### Related Files (from codebase-discovery.json)
- `.planning/plans/active/19.7_live_model_resolution/documentation/openrouter-catalog-api.md` — modify: record the finalized `@stable` exclusion heuristic (specific preview/beta/exp substrings actually observed in live `id`/`canonical_slug` values, plus the `expiration_date` non-null handling rule) and the confirmed `z-ai/` vendor-prefix finding for glenna.
- `personas/community/index.json:1` — reference only: ground truth for `delia → deepseek/deepseek-v4-pro`, `quinn → qwen/qwen3-coder-plus`, `glenna → z-ai/glm-5.2` used to cross-check the vendor-prefix finding.
- `internal/personas/catalog.go` — create (in later stories Theme 2/3; NOT created by this AC): the future catalog-client/resolver file that will consume this heuristic — noted here so the heuristic's field names and exclusion list are written in a form directly usable when that file is authored.
- `internal/personas/client.go:99` — reference only: reuse the `fetch()` retry/backoff/timeout/size-cap pattern if the live `/api/v1/models` re-list is scripted.
- `.planning/epics/active/19.7_live_model_resolution.md` — reference: the epic's explicit "GLM vendor prefix is `z-ai/`, not `glm`" finding that this AC formally re-confirms against the live catalog before resolver code depends on it.

## Happy Path Scenarios
**Scenario 1: `@stable` heuristic derived from observed live catalog values**
- **Given** the maintainer has re-listed `/api/v1/models` (344+ models) with a valid `OPENROUTER_API_KEY`
- **When** the `id` and `canonical_slug` values are scanned for preview/beta/exp-style substrings actually present in the response (not a hypothetical list)
- **Then** the specific substrings observed (e.g. patterns such as `-preview`, `-beta`, `-exp`, or whatever tokens are genuinely present) are written into `documentation/openrouter-catalog-api.md` as the `@stable` exclusion list, alongside the rule that any model with a non-null `expiration_date` is also excluded from `@stable` regardless of its `id`/`canonical_slug` text

**Scenario 2: `z-ai/` vendor prefix confirmed and pinned for glenna**
- **Given** `personas/community/index.json` lists `glenna → z-ai/glm-5.2`
- **When** the live `/api/v1/models` catalog is checked for entries under the `z-ai/` prefix
- **Then** the finding explicitly records that glenna's resolver key is `z-ai/` (not `glm/`), cross-referenced against the index.json entry, so the future `created`-timestamp newest-in-vendor-prefix resolver for glenna is written against the correct namespace from the start

## Edge Cases
**Edge Case 1: No live model currently carries a preview/beta/exp substring**
- **Given** the live catalog scan finds zero models with any of the guessed substrings at spike time
- **When** the heuristic is written down
- **Then** the heuristic still names the substrings to watch for (as a forward-looking exclusion rule) but explicitly notes that zero models are excluded by this path today, so the `expiration_date` rule is not silently treated as the only signal by a future reader

**Edge Case 2: `delia`/`quinn` vendor prefixes (`deepseek/`, `qwen/`) also require cross-checking**
- **Given** the story's scope names all three alias-less personas (delia/quinn/glenna), not just glenna
- **When** the vendor-prefix finding is recorded
- **Then** `deepseek/` and `qwen/` are also confirmed present in the live catalog and cross-referenced against `index.json`'s `delia → deepseek/deepseek-v4-pro` and `quinn → qwen/qwen3-coder-plus` entries, even though the epic's specific historical mistake (AC7-adjacent) was the `glm/`-vs-`z-ai/` confusion

**Edge Case 3: A model's `id` contains a substring that looks like a preview marker but is part of a version number**
- **Given** the live catalog may contain slugs where a candidate substring appears as part of an unrelated token (e.g. a version segment rather than a stability marker)
- **When** the exclusion list is finalized
- **Then** the recorded heuristic notes any such false-positive risk observed during the spike so a later resolver implementation does not over-exclude a stable model on a substring collision

## Error Conditions
**Error Scenario 1: Catalog re-list fails (auth or transport error)**
- Error message: `401 Unauthorized` or transport error from the `/api/v1/models` re-list call
- HTTP status / error code: `401` / transient `5xx` — retried per `internal/personas/client.go:78`'s retriable-status convention before treating it as a blocking precondition failure

**Error Scenario 2: `z-ai/` prefix absent from the live catalog at scan time**
- Error message: recorded as an explicit discrepancy — "no `z-ai/`-prefixed models found in the live catalog as of [date]" — rather than silently falling back to assuming `glm/`
- HTTP status / error code: N/A (data-consistency finding, not an HTTP error); this would block Theme 3's glenna resolver design and must be flagged, not silently worked around

## Performance Requirements
- **Response Time:** N/A — one-time manual catalog re-list; no latency SLA (a reasonable reference bound is the existing `fetchTimeout` of 30s in `internal/personas/client.go:58` if scripted)
- **Throughput:** One catalog listing call is sufficient to derive the heuristic; no repeated polling required

## Security Considerations
- **Authentication/Authorization:** `OPENROUTER_API_KEY` read from environment only, never committed or embedded in the recorded finding
- **Input Validation:** N/A for this AC — reading a vendor-provided catalog response, not accepting external input; the future resolver code that consumes this heuristic (out of scope here) will need its own validation when it parses `id`/`canonical_slug`/`expiration_date`

## Test Implementation Guidance
**Test Type:** MANUAL
**Test Data Requirements:** The live `/api/v1/models` response (344+ models as observed at spike time); `personas/community/index.json`'s existing 10 persona entries for cross-check
**Mock/Stub Requirements:** None for this AC's manual derivation; a later story's resolver implementation will require a checked-in catalog snapshot fixture (already tracked as `.planning/plans/active/19.7_live_model_resolution/documentation/catalog-snapshot-fixture.md`) so its own tests run with zero live network in CI

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (no new automated tests introduced by this AC)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] The `@stable` exclusion list names the specific preview/beta/exp substrings actually observed in the live catalog's `id`/`canonical_slug` values, not a hypothetical/generic list
- [x] The `expiration_date` non-null handling rule is written down explicitly as part of the `@stable` heuristic
- [x] The `z-ai/` vendor prefix (not `glm/`) is confirmed present in the live catalog and recorded as glenna's pinned resolver key, cross-referenced against `personas/community/index.json`
- [x] `deepseek/` and `qwen/` prefixes for delia/quinn are also cross-checked against the live catalog and `index.json`

**Manual Review:**
- [ ] Code reviewed and approved
