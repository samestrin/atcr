# Tech Debt Captured — Sprint 25.0 SARIF Output Integration

Deferrals surfaced during `/execute-sprint`. Read by `/execute-code-review` Phase 1.

## TD-001 — SARIF empty artifactLocation.uri may break GitHub ingestion (MEDIUM)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-14
**File:** internal/report/sarif.go:181
**Issue:** A finding with an empty File emits artifactLocation.uri:"". GitHub Code Scanning can reject an entire SARIF upload when a result's uri is empty/invalid, so one file-less reconciled finding could break ingestion of the whole document.
**Why accepted:** AC 03-01 Edge Case 3 explicitly mandates File pass-through with no substitution/defaulting; changing it now would contradict the acceptance criteria. Normal findings always carry a File, so the golden and common path are unaffected.
**Fix in:** post-4.3 smoke test / future sprint — if GitHub rejects empty-uri results, add a placeholder-uri or location-omission branch and update AC 03-01 accordingly.

## TD-002 — SARIF empty ruleId / rule id "" renders blank in Code Scanning (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-14
**File:** internal/report/sarif.go:143
**Issue:** An empty Category yields a rule with id:"" and results with ruleId:"". An empty reportingDescriptor id renders as an unnamed rule in GitHub's Security tab and risks rejection by stricter validators.
**Why accepted:** AC 01-03 Edge Case 2 explicitly documents empty-Category pass-through as in-spec and out of this story's normalization scope. Reconciled findings normally carry a Category.
**Fix in:** future sprint — add category normalization/defaulting ("uncategorized") upstream or in renderSarif, coordinated with an AC 01-03 update.

## TD-003 — MCP report tool schema enum excludes sarif (transport clients cannot select it) (MEDIUM)
**Origin:** Phase 3, task 3.1/3.2.A discovery, 2026-07-14
**File:** internal/mcp/tools.go:97
**Issue:** The MCP `report` tool constrains `format` to a closed enum `md/json/checklist` (reportInputSchema, asserted in tools_test.go:69,111). An over-the-wire `sarif` request is therefore rejected by schema validation before handleReport runs, so real MCP-transport clients cannot obtain SARIF even though the in-process handler renders it correctly. CLI/MCP output parity (identical bytes) IS proven in-process, but MCP *access* to sarif is not wired.
**Why accepted:** AC 01-04 scopes MCP to "no code change expected" and its Scenario 3 tests handleReport in-process; extending the transport enum (plus updating tools_test.go's two ElementsMatch assertions and the ReportArgs jsonschema doc string) is beyond the sprint's defined scope. Sprint goal was output-parity coverage, which is met.
**Fix in:** follow-up decision — if MCP clients should be able to request SARIF, add `sarif` to reportInputSchema's enum + ReportArgs doc string and update tools_test.go:69,111. Otherwise document that SARIF is CLI-only by design.
