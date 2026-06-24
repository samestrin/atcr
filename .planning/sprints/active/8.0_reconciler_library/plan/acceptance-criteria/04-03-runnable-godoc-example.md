# Acceptance Criteria: Runnable godoc Example (example_test.go)

**Related User Story:** [04: OSS Adoption Documentation and Apache 2.0 License](../user-stories/04-oss-adoption-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go test file — godoc `Example` function | `reconcile/example_test.go`, compiled and run by `go test` |
| Test Framework | `go test` | `Example` functions run as tests; `// Output:` blocks are diff-asserted |
| Key Dependencies | `reconcile` package (lifted as-is in Story 2) | `Reconcile`, `Source`, `Finding`, `Options`, `Result` |
| Dependency Policy | stdlib-only in example body; testify allowed in `*_test.go` but the example stays dependency-light so `go doc` renders cleanly | no network, no external fixtures |

### Related Files (from codebase-discovery.json)
- `reconcile/example_test.go` - create: a godoc `Example` function (standard `func Example()` or `func ExampleReconcile()`) that constructs two `Source` values with overlapping `Finding` literals, calls `Reconcile(sources, opts)`, and asserts on the merged `Result` via an `// Output:` block or plain `if`/`fmt` checks.
- `reconcile/reconcile.go` (lifted in Story 2) - read: the `Reconcile` entry point and `sortMerged` total order (severity desc, then file, then line) that makes the example's expected output stable (`internal/reconcile/reconcile.go:64`).
- `reconcile/types.go` / `reconcile/finding.go` (lifted in Story 2) - read: real `Source{Name string; Findings []Finding}` and `Finding` wire fields (Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence).
- `reconcile/README.md` - create: the README quickstart (AC 04-02) mirrors this example.

## Happy Path Scenarios
**Scenario 1: Example constructs two reviewers and reconciles them**
- **Given** two `Source` values, each with `Finding` literals that overlap on `FILE` and `LINE` (within ±3)
- **When** `Reconcile(sources, Options{})` is called inside `func ExampleReconcile()`
- **Then** the returned `Result` contains merged findings with the agreeing reviewers joined and a `HIGH` (2-reviewer) confidence

**Scenario 2: Example demonstrates a disagreement annotation**
- **Given** the two sources disagree on severity for the same cluster (e.g., one `high`, one `low`)
- **When** the example prints the merged `Result`
- **Then** the output shows the `<lo> vs <hi>` disagreement annotation on that finding, so an adopter sees conflict preservation

**Scenario 3: Example runs green under `go test`**
- **Given** `reconcile/example_test.go` defines an `Example` function with an `// Output:` block
- **When** `go test ./reconcile/...` runs
- **Then** the example compiles, executes, and its `// Output:` block matches the actual printed output exactly (deterministic via `sortMerged`)

**Scenario 4: Example renders in `go doc`**
- **Given** the `Example` function is named per godoc convention
- **When** `go doc github.com/samestrin/atcr/reconcile` is run
- **Then** the example appears in the package documentation output

## Edge Cases
**Edge Case 1: Example relies on map iteration or unsorted input**
- **Given** the example builds `Source` findings in an arbitrary order
- **When** the `// Output:` block is matched
- **Then** the expected output is asserted on the `sortMerged` total order (severity desc, then file, then line), not insertion order; the example documents that determinism is a feature

**Edge Case 2: Example uses testify assertions**
- **Given** testify is allowed in `*_test.go`
- **When** the example is authored
- **Then** the example stays dependency-light (plain `if`/`fmt` + `// Output:`) so `go doc` renders cleanly and the example does not pull testify into the rendered godoc

**Edge Case 3: Example inputs collide with a fixture-driven test**
- **Given** the relocated pure-logic tests (e.g., `emit_test.go`'s `TestReconcile_TwoReviewersAgreeHighConfidence`) participate in the same `go test ./reconcile/...` run
- **When** the example's `Source`/`Finding` literals are chosen
- **Then** the example uses self-contained inline literals that do not depend on shared testdata fixtures, avoiding collisions

## Error Conditions
**Error Scenario 1: Example does not compile against the lifted types**
- Error message: `reconcile/example_test.go:xx:yy: unknown field 'Foo' in struct literal of type reconcile.Finding` (or similar)
- HTTP status / error code: N/A — `go test ./reconcile/...` fails to compile; CI blocks merge

**Error Scenario 2: `// Output:` block drifts from actual output**
- Error message: `got:\n<actual>\nwant:\n<expected>\nFAIL    ExampleReconcile`
- HTTP status / error code: N/A — `go test` fails the example; the fix is to re-sync the `// Output:` block with the deterministic `sortMerged` output

**Error Scenario 3: Example documents the deferred clean API**
- Error message: "example references deferred symbol"
- HTTP status / error code: N/A — the example must call `Reconcile(sources, opts) Result` (value return), not `(*Result, error)` or `ReconciledFinding`; a grep check fails the build otherwise

## Performance Requirements
- **Response Time:** The example runs in < 100ms (handful of findings; no I/O).
- **Throughput:** N/A — single example function.

## Security Considerations
- **Authentication/Authorization:** N/A — test code only.
- **Input Validation:** The example's `Finding` literals are hardcoded inline; no external/untrusted input is read. The example must not demonstrate deserializing untrusted input (that is AC 03-01's concern).
- **Network:** The example must not require network access; `go test ./reconcile/...` runs offline (verified by CI in a sandboxed, network-disabled job if available).

## Test Implementation Guidance
**Test Type:** UNIT (godoc example, compiled + run by `go test`)
**Test Data Requirements:** Two `Source` literals with overlapping findings (one agreement cluster producing `HIGH` confidence, one disagreement cluster producing the `<lo> vs <hi>` annotation). Inline literals only — no shared testdata files.
**Mock/Stub Requirements:** None — the example calls the real `Reconcile` from the lifted package. Author as `func ExampleReconcile()` so godoc attaches it to the `Reconcile` symbol; include an `// Output:` block so `go test` asserts the printed result. Document the determinism assumption (sortMerged total order) in a comment so a future maintainer does not "fix" the expected output.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./reconcile/...` runs the `Example` green)
- [x] No linting errors (`go vet`, `golangci-lint`)
- [x] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [x] `reconcile/example_test.go` defines a godoc `Example` function (standard `func Example()` or `func ExampleReconcile()`)
- [x] The example constructs two `Source` values, calls `Reconcile(sources, opts)`, and asserts on a merged `Result`
- [x] The example output demonstrates a merge with `HIGH` confidence and a `<lo> vs <hi>` disagreement annotation
- [x] The example compiles and runs under `go test ./reconcile/...` with no external dependencies or network access
- [x] The example renders in `go doc github.com/samestrin/atcr/reconcile` output
- [x] The example uses the lifted-as-is signature (value `Result` return, no error)

**Manual Review:**
- [x] Code reviewed and approved
- [x] Example output eyeballed against `sortMerged` ordering and confirmed stable across runs
