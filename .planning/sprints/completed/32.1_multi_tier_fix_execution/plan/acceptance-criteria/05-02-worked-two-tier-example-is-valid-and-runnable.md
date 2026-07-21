# Acceptance Criteria: Worked Two-Tier Example Is Valid and Runnable

**Related User Story:** [05: Document the Multi-Tier Workflow](../user-stories/05-document-multi-tier-workflow.md)

## Acceptance Criteria
The worked two-tier example — a cheap-tier registry config and a frontier-tier registry config in `examples/` — loads with zero validation errors through atcr's real registry loader (dry-run, not visual inspection), demonstrates a meaningful ceiling contrast between tiers, and its comments describe exactly the two-independent-runs mechanism Story 3 ships.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | YAML example config | `examples/registry-with-executor.yaml`, extended or paired with a new sibling file (e.g. `examples/registry-with-executor-tier2.yaml`) |
| Test Framework | Go test exercising atcr's registry loader (`internal/registry`), run as a dry-run config load | Reuses the existing loader — no new test framework introduced |
| Key Dependencies | atcr's own `internal/registry` package (YAML unmarshal + `validateExecutor`) | No third-party YAML validator; the loader itself is the source of truth for "valid" |

### Related Files (from codebase-discovery.json)
- `examples/registry-with-executor.yaml` - modify: extend the existing single-executor example (currently lines 1-71) with a realistic cheap-tier `executor:` block (e.g. a local/cheap model, a low `max_estimated_minutes`), OR add a second commented section, per whichever shape Story 3's actual multi-tier mechanism (two independent registry runs against the same `findings.json`) requires.
- `examples/registry-with-executor-tier2.yaml` - create (if the chosen mechanism needs two separate files rather than one annotated file): a companion frontier-tier registry example (higher-capability model, no ceiling or a much higher `max_estimated_minutes`), reusing the same `providers:`/`agents:` shape as the tier-1 file so the pair is directly comparable.
- `docs/registry.md` - modify: the existing "runnable examples" cross-reference sentence (near the end of the Executor section, linking to `examples/registry-with-executor.yaml` and `examples/registry-without-executor.yaml`) is updated to also point at the new two-tier example(s).
- `.planning/plans/active/32.1_multi_tier_fix_execution/user-stories/03-run-second-tier-over-skipped-findings.md` - reference only: source of the actual multi-tier mechanism (interpretation (a): two independently-configured registry files run sequentially against the same `findings.json`) this worked example must match exactly.

## Happy Path Scenarios
**Scenario 1: Cheap-tier example loads cleanly through atcr's registry loader**
- **Given** a cheap-tier example registry with an `executor:` block setting a low-capability model/provider and a `max_estimated_minutes` ceiling (e.g. `15`)
- **When** the file is loaded via atcr's existing registry loader (the same load path `atcr verify` uses), in a dry-run/parse-only mode
- **Then** the load succeeds with zero validation errors, and the loaded `ExecutorConfig.EffectiveMaxEstimatedMinutes()` (or equivalent resolver) returns the configured ceiling value

**Scenario 2: Frontier-tier example loads cleanly and demonstrates no/high ceiling**
- **Given** a frontier-tier example registry with an `executor:` block setting a high-capability model/provider and either no `max_estimated_minutes` key or a materially higher value than the cheap-tier example
- **When** the file is loaded via the same registry loader
- **Then** the load succeeds with zero validation errors, and the loaded config's effective ceiling reflects "no ceiling" or the higher configured value, contrasting clearly with Scenario 1's cheap-tier config

**Scenario 3: The pair demonstrates the actual two-tier mechanism, not a speculative one**
- **Given** Story 3 has locked the multi-tier mechanism as two independently-configured registry runs against the same `findings.json` (interpretation (a) in Story 3's Story Context)
- **When** the worked example is authored
- **Then** the example's accompanying comments explicitly describe running atcr twice — once per config file — against the same `findings.json`, with tier 2 picking up only the findings tier 1 skipped, matching Story 3's shipped mechanism (updated to match if Story 3's implementation changes before this story is marked done)

## Edge Cases
**Edge Case 1: A single file cannot cleanly express two independent registries**
- **Given** a registry file has exactly one top-level `executor:` key and cannot represent two full registries (each needing its own `providers:`/`agents:`/`executor:`) in one document
- **When** the worked example is authored
- **Then** a second sibling file (`examples/registry-with-executor-tier2.yaml`) is added rather than forcing an invalid or misleading single-file structure, per the story's own contingency note ("or a companion example file, if a single file cannot cleanly show two independent registries")

**Edge Case 2: Example must not silently rely on unshipped fields**
- **Given** this story is sequenced after Stories 1 and 3, whose exact field names/mechanism might still be in flux at drafting time
- **When** the example is finalized
- **Then** it is validated against Story 1's actual shipped `ExecutorConfig` fields (not the plan's speculative field names) before this AC is marked done — a load-time failure due to a typo'd or renamed field is caught before merge, not left as a copy-paste trap for an operator

## Error Conditions
**Error Scenario 1: Malformed or schema-invalid YAML in the worked example**
- Error message: exact `validateExecutor`/YAML-unmarshal error text atcr's loader emits for the specific defect (e.g. "max_estimated_minutes: value out of range" or a YAML syntax error), surfaced by the dry-run load test
- HTTP status / error code: N/A (CLI/loader exit code; a non-zero exit from the dry-run load command counts as AC failure)

**Error Scenario 2: Cheap-tier and frontier-tier ceilings are not meaningfully distinct**
- Error message: "worked example's two tiers do not demonstrate a meaningful ceiling contrast (e.g. both set to the same value)" — a reviewer-facing review comment, not a runtime error
- HTTP status / error code: N/A (caught by manual review of the example's realism, not by the loader)

## Performance Requirements
- **Response Time:** The dry-run load of each example file must complete as fast as any other existing example load (sub-second) — no new performance surface is introduced.
- **Throughput:** N/A — a one-time example load, not a hot path.

## Security Considerations
- **Authentication/Authorization:** Example files must use placeholder env-var references only (e.g. `api_key_env: ANTHROPIC_API_KEY`), matching the existing example's convention — no literal API keys or secrets committed.
- **Input Validation:** The example's field values must fall within the documented valid ranges from AC 05-01 (e.g. a positive, in-range `max_estimated_minutes`) so it cannot be copy-pasted as a demonstration of an invalid config.

## Test Implementation Guidance
**Test Type:** INTEGRATION — a dry-run config load through atcr's real registry loader (reusing the existing `internal/registry` test harness pattern, e.g. `registry.Load(path)` or equivalent), not a hand-rolled YAML syntax check.
**Test Data Requirements:** The two example files themselves (`examples/registry-with-executor.yaml` cheap-tier variant, `examples/registry-with-executor-tier2.yaml` frontier-tier variant), plus a minimal fixture `findings.json` with a mix of `EST_MINUTES` values if the test also exercises the end-to-end skip/pickup behavior (optional — Story 3's own AC already covers the routing behavior; this AC's test scope is "the YAML loads," not "the routing is correct").
**Mock/Stub Requirements:** None required for a pure load/validate test; if extended to an end-to-end fix-generation smoke test, the provider clients would need stubbing (out of scope for this documentation-only story).

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `examples/registry-with-executor.yaml` (and/or a new `examples/registry-with-executor-tier2.yaml`) contains a complete, schema-valid cheap-tier and frontier-tier executor example
- [x] Both example files load with zero errors through atcr's actual registry loader (verified by a dry-run load, not visual inspection alone)
- [x] The example's accompanying comments describe the real two-independent-runs mechanism Story 3 ships, not a speculative or contradicted one
- [x] `docs/registry.md`'s runnable-examples cross-reference sentence is updated to mention the new two-tier example file(s)

**Manual Review:**
- [x] Code reviewed and approved
