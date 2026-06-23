# Acceptance Criteria: README Documents Public API Surface (Lifted As-Is)

**Related User Story:** [04: OSS Adoption Documentation and Apache 2.0 License](../user-stories/04-oss-adoption-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Hand-authored Markdown docs | `reconcile/README.md` — module-root readme surfaced by pkg.go.dev |
| Test Framework | `go doc` + grep assertions in CI | cross-check README type names against the library package |
| Key Dependencies | `reconcile` package symbols from Story 2 | `Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `Verdict*` constants |
| API Contract | Lifted as-is — NO deferred clean API | no `(*Result, error)`, no `ReconciledFinding`, no `Options{LineTolerance, SimilarityThreshold}` |

### Related Files (from codebase-discovery.json)
- `reconcile/README.md` - create: documents the public API surface (entry point signature + every public type and the `Verdict*` constants), cross-checked against the package's `go doc` output.
- `reconcile/reconcile.go` (lifted in Story 2) - read: source of truth for the `Reconcile(sources []Source, opts Options) Result` signature and exported symbols; README must not drift from these names (`internal/reconcile/reconcile.go:64`).
- `reconcile/types.go` / `reconcile/finding.go` (lifted in Story 2) - read: source struct definitions for `Source`, `Finding`, `Merged`, `Options{ReconciledAt, Partial, Merges, Root}`, `Result`, `Summary`, `Verification`, `Verdict*` constants.
- `.planning/product/concepts/reconciler-library.md` - read: conceptual reference; confirms the deferred clean API is out of scope and must NOT appear in the README.

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — the `Verification` type and `Verdict*` constants documented in the README originate from this design concept.

## Happy Path Scenarios
**Scenario 1: README lists the entry point with the lifted-as-is signature**
- **Given** the `reconcile` package exports `func Reconcile(sources []Source, opts Options) Result`
- **When** an adopter reads `reconcile/README.md`
- **Then** the README shows the exact signature `Reconcile(sources []Source, opts Options) Result` (value return, no error) in a fenced Go block

**Scenario 2: README documents every lifted-as-is public type**
- **Given** Story 2 lifted `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, and the `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` constants into the `reconcile` package
- **When** the README's API section is rendered
- **Then** each of those type names appears with its field set matching the package's `go doc` output (e.g., `Options{ReconciledAt, Partial, Merges, Root}`, `Finding`'s wire fields Severity/File/Line/Problem/Fix/Category/EstMinutes/Evidence, `Verification{Verdict, Skeptic, Notes}`)

**Scenario 3: README renders under `go doc`**
- **Given** `reconcile/README.md` exists at the module root
- **When** `go doc github.com/samestrin/atcr/reconcile` is run
- **Then** the package doc (the `// Package reconcile ...` comment in `doc.go` or the package comment) renders alongside the README content pkg.go.dev surfaces

## Edge Cases
**Edge Case 1: README references a type whose name was renamed in Story 2**
- **Given** a public type was renamed during the lift (e.g., a field moved)
- **When** the README is cross-checked against `go doc -all github.com/samestrin/atcr/reconcile`
- **Then** the README is updated to the real name; stale names are a failure of this AC

**Edge Case 2: Adopter greps the README for an install path**
- **Given** the README documents the module path `github.com/samestrin/atcr/reconcile`
- **When** an adopter runs `go get github.com/samestrin/atcr/reconcile`
- **Then** the path in the README matches the `go.mod` module directive exactly (verified by `head -1 reconcile/go.mod`)

## Error Conditions
**Error Scenario 1: README documents the deferred clean API**
- Error message: "README references deferred symbol: ReconciledFinding / (*Result, error) / Options.LineTolerance"
- HTTP status / error code: N/A — CI grep check (`grep -E 'ReconciledFinding|\\*Result, error|LineTolerance|SimilarityThreshold' reconcile/README.md` returns no matches) fails the build

**Error Scenario 2: README type list diverges from the package exports**
- Error message: "README type set != `go doc` exported type set"
- HTTP status / error code: N/A — a CI script diffs the README's documented type names against `go doc -all` exported symbols and fails on mismatch

## Performance Requirements
- **Response Time:** N/A — static Markdown; `go doc github.com/samestrin/atcr/reconcile` renders in < 1s.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public docs.
- **Input Validation:** No code executes from the README; fenced Go blocks are illustrative, not compiled (the runnable code is AC 04-03's `example_test.go`). The README must not advise embedding insecure input handling.
- **License Pointer:** README includes a one-line pointer to `LICENSE-COMMERCIAL.md` (delivered by Story 5) so the dual-license intent is visible without duplicating Story 5 content; this AC only verifies the pointer exists, not the commercial file.

## Test Implementation Guidance
**Test Type:** UNIT (docs cross-check script)
**Test Data Requirements:** The exported symbol set from `go doc -all github.com/samestrin/atcr/reconcile`; the set of type names extracted from `reconcile/README.md`.
**Mock/Stub Requirements:** None — runs against the real lifted package and the README file. Implement as a small `make verify-readme` target or a `.githooks`-driven script: extract fenced Go identifiers from the README, diff against `go doc -all` output, and `grep -E` the deferred-API denylist.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./reconcile/...` still green; README cross-check script passes)
- [ ] No linting errors (`go vet`, markdown lint if configured)
- [ ] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [ ] README shows `Reconcile(sources []Source, opts Options) Result` exactly (value return, no error)
- [ ] README documents `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, and `VerdictConfirmed/Refuted/Unverifiable`
- [ ] README contains no reference to the deferred clean API (`ReconciledFinding`, `(*Result, error)`, `LineTolerance`, `SimilarityThreshold`)
- [ ] `reconcile/README.md` module path string matches `reconcile/go.mod` module directive

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] README type list eyeballed against `go doc -all github.com/samestrin/atcr/reconcile` output
