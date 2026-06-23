# Acceptance Criteria: README Documents Behavior, Install & Quickstart

**Related User Story:** [04: OSS Adoption Documentation and Apache 2.0 License](../user-stories/04-oss-adoption-documentation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Hand-authored Markdown docs | `reconcile/README.md` behavior + install + quickstart sections |
| Test Framework | `go test` (quickstart snippet compiles via `example_test.go`) + grep | quickstart must mirror the runnable example |
| Key Dependencies | `reconcile` package, `go get` module path | `github.com/samestrin/atcr/reconcile` |
| Behavior Spec | Deterministic clustering, Jaccard dedupe, max-severity merge, confidence v2, ambiguity sidecar | thresholds are part of the documented contract |

### Related Files (from codebase-discovery.json)
- `reconcile/README.md` - create: behavior section (clustering at `FILE, LINE±3`; token-set Jaccard dedupe with 0.7/0.4 integer-cross-multiply thresholds; max-severity merge with `<lo> vs <hi>` disagreement annotation; confidence v2 `VERIFIED/HIGH/MEDIUM/LOW`; ambiguity sidecar), install snippet, and quickstart mirroring `example_test.go`.
- `reconcile/example_test.go` - create: the quickstart in the README must mirror this `Example` function so an adopter can copy-paste and run.
- `reconcile/go.mod` - read: module path consumed by the `go get` install snippet.
- `reconcile/LICENSE` - create: the README's license pointer targets this file (Apache 2.0) and the `LICENSE-COMMERCIAL.md` placeholder (Story 5).

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — confidence v2 `VERIFIED/HIGH/MEDIUM/LOW` ordering documented in the README behavior section.

## Happy Path Scenarios
**Scenario 1: README explains the deterministic clustering behavior**
- **Given** the reconciler clusters findings at `FILE, LINE±3` with a deterministic total order
- **When** an adopter reads the behavior section of `reconcile/README.md`
- **Then** the README states clustering keys on `FILE` and `LINE ± 3` and that output ordering is deterministic (severity desc, then file, then line) — framing determinism as a feature

**Scenario 2: README documents the Jaccard dedupe thresholds**
- **Given** the reconciler dedupes via token-set Jaccard similarity with 0.7 (merge) and 0.4 (ambiguous) integer-cross-multiply thresholds
- **When** the behavior section is rendered
- **Then** both thresholds (0.7 and 0.4) and their meaning (above 0.7 merges, 0.4–0.7 is the ambiguous gray zone written to the ambiguity sidecar) appear in the README

**Scenario 3: README documents disagreement-preserving merge and confidence**
- **Given** the reconciler merges by max-severity and annotates severity conflicts as `<lo> vs <hi>`, and scores confidence v2 as `VERIFIED/HIGH/MEDIUM/LOW`
- **When** the behavior section is rendered
- **Then** the README describes the `<lo> vs <hi>` disagreement annotation, the confidence tiers (`VERIFIED/HIGH/MEDIUM/LOW`), and the ambiguity sidecar for gray-zone clusters

**Scenario 4: Install snippet resolves via `go get`**
- **Given** the README contains an install snippet `go get github.com/samestrin/atcr/reconcile`
- **When** an adopter runs that command in a fresh module
- **Then** the module path matches `reconcile/go.mod`'s module directive and the `replace` in the root `go.mod` (Story 1) resolves it locally during development

**Scenario 5: Quickstart mirrors the runnable example**
- **Given** `reconcile/example_test.go` (AC 04-03) constructs two `Source` values and calls `Reconcile`
- **When** the README quickstart is compared against the example
- **Then** the quickstart uses the same `Source`/`Finding` shape and the same `Reconcile(sources, opts)` call, so copy-paste produces a compiling program

## Edge Cases
**Edge Case 1: README quotes a threshold that was tuned after Story 2**
- **Given** a Jaccard threshold changes value during the lift
- **When** the README is reviewed
- **Then** the README's stated thresholds match the constants in the lifted `reconcile` source (e.g., the integer-cross-multiply values), not a stale concept-doc number

**Edge Case 2: Ambiguity sidecar is omitted from the docs**
- **Given** the reconciler writes gray-zone clusters to an ambiguity sidecar
- **When** the README behavior section is read
- **Then** the ambiguity sidecar is mentioned (so an adopter is not surprised by its existence); omitting it is a failure of this AC

**Edge Case 3: Quickstart diverges from the example**
- **Given** the README quickstart and `example_test.go` are edited independently
- **When** a diff is run between the two code blocks
- **Then** they are identical in shape; drift is a failure (mitigation: quickstart is the canonical copy, example mirrors it)

## Error Conditions
**Error Scenario 1: Install snippet uses the wrong module path**
- Error message: `go get: module github.com/samestrin/atcr/reconcile: ... not found` (or resolves to the wrong repo)
- HTTP status / error code: N/A — CI verifies the README module path string equals `awk '/^module/ {print $2}' reconcile/go.mod`

**Error Scenario 2: Quickstart does not compile**
- Error message: `./quickstart.go: x: undefined: reconcile.Source` (or similar)
- HTTP status / error code: N/A — the quickstart must compile because it mirrors `example_test.go`, which is itself compiled by `go test ./reconcile/...` (AC 04-03)

## Performance Requirements
- **Response Time:** N/A — static docs.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public docs.
- **Input Validation:** The quickstart must not encourage passing untrusted input to `Reconcile` without the adopter sanitizing reviewer-supplied `Evidence`/`Problem` strings; the README should note that `Finding` fields are caller-supplied and not sanitized by the library.
- **License Pointer:** The README includes a one-line pointer to `LICENSE-COMMERCIAL.md` (Story 5) so adopters see the dual-license path; this AC verifies the pointer exists and is accurate, not the commercial file's contents.

## Test Implementation Guidance
**Test Type:** UNIT (docs verification script) + the quickstart rides on AC 04-03's compiled example
**Test Data Requirements:** The lifted `reconcile` source for threshold constants; `reconcile/go.mod` for the module path; `reconcile/example_test.go` for the quickstart mirror.
**Mock/Stub Requirements:** None. Implement a `make verify-readme` target that greps the README for the threshold strings (0.7, 0.4), the clustering description (`LINE±3` or `LINE ± 3`), the disagreement annotation (`<lo> vs <hi>`), the confidence tiers, the install command, and the commercial-license pointer, then diffs the quickstart code block against `example_test.go`'s `Example` body.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./reconcile/...` green; `verify-readme` target passes)
- [ ] No linting errors (`go vet`, markdown lint if configured)
- [ ] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [ ] README documents clustering at `FILE, LINE±3` and the deterministic total order
- [ ] README documents Jaccard dedupe thresholds 0.7 (merge) and 0.4 (ambiguous) and the ambiguity sidecar
- [ ] README documents max-severity merge with `<lo> vs <hi>` disagreement annotation and confidence v2 `VERIFIED/HIGH/MEDIUM/LOW`
- [ ] README install snippet module path matches `reconcile/go.mod`
- [ ] README quickstart mirrors `reconcile/example_test.go` and compiles
- [ ] README contains a one-line pointer to `LICENSE-COMMERCIAL.md`

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Behavior description eyeballed against the lifted `reconcile` source constants
