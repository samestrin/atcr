# Acceptance Criteria: Consumer Package Import Flip (All 9 Packages)

**Related User Story:** [01: Preserve ATCR as the Reference Implementation with Zero Behavioral Change](../user-stories/01-reference-implementation-preservation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package imports (compile-driven find-and-repoint) | No greenfield design — mechanical import flip |
| Test Framework | go test + testify | Existing corpus proves the flip is behavior-neutral |
| Key Dependencies | `github.com/samestrin/atcr/reconcile` | All consumers import library public types/severity helpers |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/reconcile.go` - modify: import library types (was `internal/reconcile`); `runReconcile` (`cmd/atcr/reconcile.go:35`) constructs `[]Source` via adapter
- `cmd/atcr/review.go` - modify: `runReview` (`cmd/atcr/review.go:89`) imports library types via adapter
- `cmd/atcr/resume.go` - modify: `runResume` (`cmd/atcr/resume.go:45`) imports library types via adapter
- `cmd/atcr/github.go` - modify: imports library `JSONFinding`/`IsFailing` semantics
- `cmd/atcr/report.go` - modify: imports library types via adapter
- `cmd/atcr/verify.go` - modify: imports library types via adapter
- `internal/debate/envelope.go`, `internal/debate/select.go`, `internal/debate/emit.go`, `internal/debate/debate.go` - modify: import library `Verification`/`Verdict` constants; `applyRulings` (`internal/debate/emit.go:107`) and `runDebate` (`internal/debate/debate.go:85`) mutate `*Verification`
- `internal/verify/votes.go`, `internal/verify/severity.go` - modify: `aggregateVerdicts` (`internal/verify/votes.go:25`) imports library `Verdict`; severity helpers imported from library
- `internal/report/disagree.go`, `internal/report/render.go` - modify: import library types; `render.go` severity helpers from library
- `internal/ghaction/render.go` - modify: `isRefuted`/`Conclusion` (`internal/ghaction/render.go:60`) key off library `JSONFinding`/`IsFailing`
- `internal/mcp/handlers.go` - modify: `handleReconcile` (`internal/mcp/handlers.go:278`) shares the adapter boundary + gate semantics
- `internal/fanout/metrics.go`, `internal/fanout/postprocess.go` - modify: severity helpers from library (`internal/fanout/metrics.go:107`, `internal/fanout/postprocess.go:19`)
- `internal/scorecard/reconcile.go` - modify: imports library types (added by 2026-06-23 audit)
- `internal/registry/config.go` - modify: severity helpers from library (added by 2026-06-23 audit)

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — verification contract consumed by `internal/debate`, `internal/verify`, and `internal/ghaction` after the import flip.

## Happy Path Scenarios
**Scenario 1: All 9 consumer packages import the library**
- **Given** the library public API surface exists in `github.com/samestrin/atcr/reconcile`
- **When** the 9 consumer packages (`cmd/atcr`, `internal/debate`, `internal/verify`, `internal/report`, `internal/ghaction`, `internal/mcp`, `internal/fanout`, `internal/scorecard`, `internal/registry`) are repointed to import library types/severity helpers
- **Then** `go build ./...` succeeds in the root module and no consumer references `internal/reconcile` for moved types or `internal/stream` for severity helpers

**Scenario 2: No consumer re-declares verdict/severity constants locally**
- **Given** the library exports `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` and `NormalizeSeverity`/`SeverityRank`
- **When** a grep scans all consumer packages for local `const` declarations of these names
- **Then** zero local re-declarations exist; every consumer imports the library's canonical definitions

**Scenario 3: CLI boundary sites route through the adapter**
- **Given** `cmd/atcr/reconcile.go:35`, `cmd/atcr/review.go:89`, and `cmd/atcr/resume.go:45` construct `[]Source` and call `Reconcile`
- **When** the import flip lands
- **Then** these sites call `Reconcile` via the `internal/reconcile/adapter` boundary (not directly into the library), preserving the ATCR-internal path-validation stamping

## Edge Cases
**Edge Case 1: internal/debate mutates Verification across the boundary**
- **Given** `internal/debate/emit.go:107` (`applyRulings`) and `internal/debate/debate.go:85` (`runDebate`) read/mutate `Merged.Verification`
- **When** `Verification` is now a library type
- **Then** the mutation still works because `Verification` is exported and consumers hold the same `*Verification` pointer (pointer-identity preserved by the adapter)

**Edge Case 2: internal/registry/config.go and internal/scorecard/reconcile.go (audit-added consumers)**
- **Given** the 2026-06-23 audit identified `internal/registry/config.go` and `internal/scorecard/reconcile.go` as severity-helper consumers not in the original epic list
- **When** the import flip lands
- **Then** these two packages also import the library's `NormalizeSeverity`/`SeverityRank` (the audit list is the source of truth, not the epic body)

**Edge Case 3: MCP handler shares the adapter boundary**
- **Given** `internal/mcp/handlers.go:278` (`handleReconcile`) shares the same boundary as the CLI
- **When** the flip lands
- **Then** the MCP handler routes through the adapter and gate semantics are unchanged

## Error Conditions
**Error Scenario 1: A consumer still imports internal/reconcile for a moved type**
- Error message: `cannot find package internal/reconcile.<MovedType>` or `imported and not used`
- HTTP status / error code: go build exit code 1

**Error Scenario 2: A consumer re-declares a verdict constant locally**
- Error message: grep guard fails: `VerdictConfirmed` found as a local `const` in a consumer package
- HTTP status / error code: CI guard script exit code 1

**Error Scenario 3: Stale internal/stream severity reference remains**
- Error message: grep guard fails: `internal/stream.NormalizeSeverity` or `internal/stream.SeverityRank` reference found after the flip
- HTTP status / error code: CI guard script exit code 1

## Performance Requirements
- **Response Time:** The import flip is compile-time only; no runtime overhead. Build time must not regress by more than 5%.
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** No consumer re-declares wire-format-critical constants (`Verdict*`, `SeverityRank`) locally — a local re-declaration could drift from the library's canonical values and break fixture byte-identity. The grep guard enforces single-source-of-truth.

## Test Implementation Guidance
**Test Type:** INTEGRATION (compile-driven; existing corpus proves behavior)
**Test Data Requirements:** The 9 consumer packages with their existing tests.
**Mock/Stub Requirements:** None. Verification is mechanical: `go build ./...` exit 0, plus a grep guard script asserting no `internal/reconcile` moved-type references and no `internal/stream` severity references remain, and no local `const Verdict*` re-declarations.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] All 9 consumer packages import `github.com/samestrin/atcr/reconcile` types/severity helpers
- [x] Zero local re-declarations of `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable`/`SeverityRank` in consumer packages (grep-verified)
- [x] Zero `internal/stream` severity references remain after the flip (grep-verified)
- [x] CLI and MCP handlers route `Reconcile` calls through `internal/reconcile/adapter`

**Manual Review:**
- [x] Code reviewed and approved
- [x] Confirm the 2026-06-23 audit consumer list (9 packages incl. scorecard + registry) is exhaustive
