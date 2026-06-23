# Acceptance Criteria: Lifted-as-is Public API Surface

**Related User Story:** [02: Embeddable Public API Module Scaffold](../user-stories/02-public-api-embeddability.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Package Type | Go package (`package reconcile`) | All types and functions in package `reconcile` |
| API Surface | Exported Go symbols | `Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, verdict constants |
| Documentation | `go doc` | All exported symbols appear in `go doc github.com/samestrin/atcr/reconcile` |
| Type Stability | Lift-as-is mandate | Shapes identical to existing `internal/reconcile` package; no signature changes |

### Related Files (from codebase-discovery.json)
- `reconcile/reconcile.go` - create: `Reconcile(sources []Source, opts Options) Result` entry point + `Options`/`Result`/`Summary` types (`internal/reconcile/reconcile.go:64`)
- `reconcile/merge.go` - create: `Merged` struct (embeds `Finding` + `Disagreement` + `*Verification`) (`internal/reconcile/merge.go`)
- `reconcile/emit.go` - create: `Verification` struct (`internal/reconcile/emit.go:40`), `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` constants (`internal/reconcile/emit.go:61-63`), library `Finding` type
- `reconcile/discover.go` - create: `Source` type with `Name string` and `Findings []Finding` fields (`internal/reconcile/discover.go:25`)

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — source of truth for the `Verification` struct, verdict enum, and confidence v2 ordering exposed in the public API.

## Happy Path Scenarios
**Scenario 1: Reconcile function is exported and callable**
- **Given** the `reconcile` package is imported as `github.com/samestrin/atcr/reconcile`
- **When** an external caller invokes `reconcile.Reconcile(sources, opts)` with valid `[]Source` and `Options` arguments
- **Then** the function returns a `Result` containing `Findings []Merged`, `Ambiguous []AmbiguousCluster`, and `Summary`

**Scenario 2: All core types appear in go doc**
- **Given** the module is built successfully
- **When** `go doc github.com/samestrin/atcr/reconcile` is executed
- **Then** the output lists `Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `VerdictConfirmed`, `VerdictRefuted`, `VerdictUnverifiable` as exported symbols

**Scenario 3: Finding carries the 9 wire-format fields**
- **Given** the library `Finding` type is exported
- **When** a caller constructs a `Finding` value
- **Then** it exposes all 9 wire-format fields: `Severity`, `File`, `Line`, `Problem`, `Fix`, `Category`, `EstMinutes`, `Evidence`, `Reviewer`/`Reviewers`, `Confidence`, plus `Disagreement` and `*Verification`

**Scenario 4: Options struct matches lifted-as-is shape**
- **Given** the `Options` type is exported
- **When** a caller inspects its fields
- **Then** it contains `ReconciledAt time.Time`, `Partial bool`, `Merges map[string]int`, `Root string` — identical to the existing internal type

## Edge Cases
**Edge Case 1: Verdict constants are exported strings**
- **Given** the verdict constants are defined in `emit.go`
- **When** a caller compares `Verification.Verdict` against `reconcile.VerdictConfirmed`
- **Then** the comparison succeeds because the constants are exported `string` values

**Edge Case 2: Merged embeds Finding and is field-promoted**
- **Given** a `Merged` value is constructed
- **When** a caller accesses `merged.Severity` or `merged.File`
- **Then** the embedded `Finding` fields are promoted and accessible without qualifying through `.Finding`

**Edge Case 3: Result struct contains all three top-level fields**
- **Given** a `Result` is returned from `Reconcile`
- **When** the caller accesses its fields
- **Then** `Findings []Merged`, `Ambiguous []AmbiguousCluster`, and `Summary` are all present and populated

## Error Conditions
**Error Scenario 1: Deferred clean API is not present**
- Error condition: a caller tries to use `(*Result, error)` return signature or `ReconciledFinding` type
- Symptom: compile error — `ReconciledFinding` is not an exported symbol
- Expected behavior: the proposed clean API (`(*Result, error)`, `ReconciledFinding`, `Options{LineTolerance, SimilarityThreshold}`) is deferred to a follow-on epic and must NOT appear in this module

**Error Scenario 2: Unexported helper accessed externally**
- Error condition: a caller tries to access `sortMerged`, `mergeVerification`, `verdictRank`, or `DedupeCluster` directly
- Symptom: compile error — these are unexported implementation details
- Expected behavior: only the public API surface is importable; private helpers remain unexported

## Performance Requirements
- **API Call Latency:** `Reconcile` is synchronous and stateless — no goroutines spawned, no `context.Context` parameter; latency is bounded by input size (O(n log n) for sort + O(n^2) worst-case dedupe)
- **Memory:** No allocations beyond the result slice and dedupe working set; no caches retained between calls

## Security Considerations
- **Input Validation:** `Reconcile` trusts its callers to provide well-formed `Source`/`Finding` values; path validation stays in ATCR's adapter layer, not in the library
- **No File I/O:** The library performs no file reads or writes — all I/O is the caller's responsibility, reducing attack surface

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Existing test fixtures from `internal/reconcile/*_test.go` (disagree_test.go, cluster_merge_test.go, emit_test.go) moved to `reconcile/*_test.go`
**Mock/Stub Requirements:** None — pure function with no external dependencies
**Verification Commands:**
- `go doc github.com/samestrin/atcr/reconcile` — verify all symbols listed
- `go doc github.com/samestrin/atcr/reconcile.Reconcile` — verify signature `func Reconcile(sources []Source, opts Options) Result`
- `go doc github.com/samestrin/atcr/reconcile.VerdictConfirmed` — verify constant is exported

## Definition of Done
**Auto-Verified:**
- [ ] `go build ./reconcile/...` exits 0
- [ ] `go test ./reconcile/...` passes
- [ ] No linting errors from `golangci-lint run ./reconcile/...`

**Story-Specific:**
- [ ] `go doc github.com/samestrin/atcr/reconcile` lists `Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `VerdictConfirmed`, `VerdictRefuted`, `VerdictUnverifiable`
- [ ] `Reconcile` signature is exactly `func Reconcile(sources []Source, opts Options) Result` (not `(*Result, error)`)
- [ ] `Finding` type carries all 9 wire-format fields plus `Disagreement` and `*Verification`
- [ ] Deferred clean API symbols (`ReconciledFinding`, `Options{LineTolerance, SimilarityThreshold}`) are absent

**Manual Review:**
- [ ] Code reviewed and approved
