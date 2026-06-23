# Acceptance Criteria: Severity Canonical Ownership Migration

**Related User Story:** [02: Embeddable Public API Module Scaffold](../user-stories/02-public-api-embeddability.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Canonical Owner | Library package (`reconcile/severity.go`) | `NormalizeSeverity`/`SeverityRank` move here from `internal/stream/severity.go` |
| Elimination Target | `internal/reconcile/merge.go:30` | Redundant init-copy of severity mapping must be removed |
| Backward Compatibility | ATCR re-exports or imports from library | `internal/stream` sources severity from library or ATCR imports directly |
| Verification | `go test` fixture diff | Byte-identical severity normalization output |

### Related Files (from codebase-discovery.json)
- `reconcile/severity.go` - create: `NormalizeSeverity`/`SeverityRank` moved from `internal/stream/severity.go:33` as canonical owner
- `internal/stream/severity.go` - modify: either re-export from library or delegate to `reconcile.NormalizeSeverity`/`reconcile.SeverityRank`
- `reconcile/merge.go` - create: merge logic that previously had redundant init-copy at `internal/reconcile/merge.go:30` now imports canonical `NormalizeSeverity`/`SeverityRank` from same package
- `internal/reconcile/merge.go` - modify: remove redundant severity init-copy at line 30

## Happy Path Scenarios
**Scenario 1: Library owns NormalizeSeverity as canonical**
- **Given** `NormalizeSeverity` and `SeverityRank` previously lived in `internal/stream/severity.go`
- **When** the migration is performed
- **Then** `reconcile/severity.go` is the canonical owner of `NormalizeSeverity` and `SeverityRank`; the library's merge logic imports them from the same package

**Scenario 2: Redundant init-copy is eliminated**
- **Given** `internal/reconcile/merge.go:30` previously had a redundant init-copy of the severity mapping
- **When** the library owns severity canonically
- **Then** the init-copy is removed; `merge.go` calls `NormalizeSeverity`/`SeverityRank` directly from the library package

**Scenario 3: ATCR severity behavior is unchanged**
- **Given** ATCR's existing test corpus uses severity normalization
- **When** `go test ./...` is run after migration
- **Then** all severity-related tests pass with byte-identical output — no behavioral change

## Edge Cases
**Edge Case 1: internal/stream delegates to library**
- **Given** `internal/stream` previously exported `NormalizeSeverity`/`SeverityRank`
- **When** other ATCR packages import them from `internal/stream`
- **Then** `internal/stream` either re-exports from the library (thin wrapper) or those call sites are updated to import from `github.com/samestrin/atcr/reconcile` directly

**Edge Case 2: SeverityRank ordering preserved**
- **Given** `SeverityRank` assigns integer ranks to severity levels (critical > high > medium > low)
- **When** the function is moved to the library
- **Then** the ranking values and ordering are identical to the original implementation

**Edge Case 3: NormalizeSeverity handles unknown severities**
- **Given** `NormalizeSeverity` receives an unrecognized severity string
- **When** it is called
- **Then** it returns the same default/fallback value as the original implementation (no behavioral change)

## Error Conditions
**Error Scenario 1: Dual-ownership drift**
- Error condition: both `internal/stream/severity.go` and `reconcile/severity.go` define `NormalizeSeverity` independently
- Symptom: the two copies diverge over time, producing different normalization results
- Fix: `internal/stream` must source from the library (import or re-export); only one definition exists

**Error Scenario 2: init-copy not removed**
- Error condition: `internal/reconcile/merge.go:30` still has the redundant init-copy after migration
- Symptom: code compiles but the init-copy shadows or duplicates the library's canonical version
- Fix: delete the init-copy; use the library's `NormalizeSeverity`/`SeverityRank` directly

**Error Scenario 3: Circular import**
- Error condition: `internal/stream` imports `github.com/samestrin/atcr/reconcile` while the library imports `internal/stream`
- Symptom: `go build` fails with `import cycle not allowed`
- Fix: the library must NOT import `internal/stream`; if `internal/stream` needs the library, the dependency flows one way (stream -> reconcile, never reverse)

## Performance Requirements
- **Normalization Speed:** `NormalizeSeverity` is a string-to-string lookup — O(1) via map or switch; no performance regression from the move
- **No Runtime Cost:** Moving the function between packages has zero runtime overhead — it is a compile-time relocation

## Security Considerations
- **Input Handling:** `NormalizeSeverity` must handle arbitrary string input without panicking (case-insensitive, trim whitespace) — same behavior as original
- **No File Access:** Severity functions perform no file I/O; they are pure string transformations

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven test cases with known severity strings (critical, high, medium, low, unknown, empty, mixed-case) and expected normalized outputs
**Mock/Stub Requirements:** None — pure functions
**Verification Commands:**
- `go test ./reconcile/... -run Severity` — library severity tests pass
- `go test ./internal/stream/... -run Severity` — ATCR severity tests (if re-exporting) pass
- `go test ./...` — full corpus green (no behavioral change)
- `grep -n 'NormalizeSeverity\|SeverityRank' internal/reconcile/merge.go` — confirm init-copy removed (should show import usage, not local definition)

## Definition of Done
**Auto-Verified:**
- [ ] `go build ./reconcile/...` exits 0
- [ ] `go test ./reconcile/...` passes
- [ ] `go test ./...` passes (full ATCR corpus — no behavioral change)
- [ ] No linting errors

**Story-Specific:**
- [ ] `reconcile/severity.go` contains `NormalizeSeverity` and `SeverityRank` as canonical definitions
- [ ] `internal/reconcile/merge.go` no longer has a redundant init-copy at line 30
- [ ] No circular import between `internal/stream` and `reconcile`
- [ ] Severity normalization output is byte-identical to pre-migration (verified by existing fixtures)

**Manual Review:**
- [ ] Code reviewed and approved
