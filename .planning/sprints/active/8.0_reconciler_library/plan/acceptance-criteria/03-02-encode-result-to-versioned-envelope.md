# Acceptance Criteria: Encode Result to Versioned JSON Envelope (reconcile-json/v1)

**Related User Story:** [03: JSON Format Adapter (reconcile-json/v1)](../user-stories/03-json-format-adapter.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package — `encoding/json` adapter | stdlib-only in shipped code |
| Test Framework | `go test` + testify | co-located `*_test.go` + golden fixtures |
| Key Dependencies | `encoding/json`, `reconcile.Result`, `reconcile.Finding`, `reconcile.Summary`, `reconcile.AmbiguousCluster`, `reconcile.Verification`, `reconcile.Options` | all lifted as-is in Story 2 |
| Schema Family | `reconcile-json/v1` | versioned independently of `atcr-findings/v1` |

### Related Files (from codebase-discovery.json)
- `reconcile/adapter/json/adapter.go` - create: encode entrypoint; marshals `reconcile.Result` into the output envelope `{version, reconciled_at, findings[], summary, ambiguous[]}`, stamping `"version":"reconcile-json/v1"` and `reconciled_at` from `Options.ReconciledAt` (or `time.Now().UTC()`).
- `reconcile/adapter/json/adapter_test.go` - create: golden-file assertions on encoded output; field-order assertions; version + RFC3339 timestamp assertions.
- `reconcile/adapter/json/testdata/encode_golden.json` - create: golden fixture capturing the canonical encoded shape.
- `reconcile/result.go` (lifted in Story 2) - read: source `Result`, `Summary`, `AmbiguousCluster` struct definitions whose JSON tags drive field names.
- `docs/findings-format.md` - read: conceptual reference for the round-tripped wire format; the adapter's `reconcile-json/v1` schema is its own independently-versioned contract.

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — the `Verification` sub-object rendered inside encoded findings follows the verdict/notes/skeptic shape defined here.

## Happy Path Scenarios
**Scenario 1: A populated Result encodes to the canonical envelope**
- **Given** a `reconcile.Result` with one finding (with `Reviewers`, `Confidence`, `Disagreement`, and a populated `*Verification`), a non-empty `Summary`, and one `AmbiguousCluster`
- **When** the encode function is called
- **Then** the output JSON is `{"version":"reconcile-json/v1","reconciled_at":<RFC3339>,"findings":[{severity,file,line,problem,fix,category,est_minutes,evidence,reviewers:[],confidence,disagreement,verification:{verdict,skeptic,notes}}],"summary":{...},"ambiguous":[...]}` with field names sourced from the library `Finding` JSON struct tags

**Scenario 2: reconciled_at comes from Options.ReconciledAt when set**
- **Given** a `reconcile.Options{ReconciledAt: T}` where `T` is a fixed timestamp
- **When** the encode function is called with those options
- **Then** the output `reconciled_at` equals `T.Format(time.RFC3339)` exactly, enabling deterministic tests

**Scenario 3: reconciled_at falls back to time.Now().UTC()**
- **Given** a `reconcile.Options` with a zero `ReconciledAt`
- **When** encoded
- **Then** `reconciled_at` is a valid RFC3339 string representing the current UTC time (assert format, not exact value)

## Edge Cases
**Edge Case 1: Empty findings array**
- **Given** a `Result` with `Findings: []Finding{}` (or nil per lift contract)
- **When** encoded
- **Then** `"findings":[]` is present — the top-level `findings` array is NOT `omitempty`; `omitempty` applies only to the per-finding `disagreement`/`verification` sub-fields. This guarantees byte-stability per AC 03-03

**Edge Case 2: Finding with nil Verification**
- **Given** a `Finding` whose `*Verification` is `nil`
- **When** encoded
- **Then** the `verification` key is absent from that finding's object (see AC 03-03 for the byte-stability guarantee)

**Edge Case 3: Finding with no disagreement**
- **Given** a `Finding` whose `Disagreement` is the zero value
- **When** encoded
- **Then** the `disagreement` key is absent from that finding's object

**Edge Case 4: Empty ambiguous list**
- **Given** a `Result` with `Ambiguous: nil` or `[]AmbiguousCluster{}`
- **When** encoded
- **Then** `"ambiguous":[]` is present — the top-level `ambiguous` array is NOT `omitempty`, matching the `findings` array policy in Edge Case 1

## Error Conditions
**Error Scenario 1: Marshal failure on non-serializable Finding**
- Error message: `json: error calling MarshalJSON for type reconcile.Finding: ...`
- HTTP status / error code: N/A (library call returns `error`); the adapter surfaces the underlying `json.Marshal` error unchanged

**Error Scenario 2: Zero-value Result**
- Encode takes `reconcile.Result` by value (the lifted `Reconcile` returns `Result`, not `*Result`), so there is no nil-pointer case. A zero-value `Result{}` encodes to the canonical empty envelope — `{"version":"reconcile-json/v1","reconciled_at":<RFC3339>,"findings":[],"summary":{...zero...},"ambiguous":[]}` — and never returns an error

## Performance Requirements
- **Response Time:** Encoding 1,000 findings completes in < 50ms on commodity hardware (stdlib `encoding/json` baseline).
- **Throughput:** Single allocation for the output buffer; no per-field reflection beyond `encoding/json`'s native cost. Field order is fixed by struct declaration order, so no sorting pass.

## Security Considerations
- **Authentication/Authorization:** N/A — no auth surface in the library.
- **Input Validation:** The adapter does NOT accept path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`); the library `Finding` type does not carry them, so leakage is structurally impossible (asserted in AC 03-04).
- **Output Integrity:** `reconciled_at` is the only timestamp and is UTC-normalized; no producer-supplied timestamp leaks into the output unless routed through `Options.ReconciledAt`.

## Test Implementation Guidance
**Test Type:** UNIT (with golden fixtures)
**Test Data Requirements:** A canonical `Result` fixture covering: finding with full `Verification` + `Disagreement`; finding with neither; empty `Findings`; empty `Ambiguous`; fixed `Options.ReconciledAt`. Golden file at `reconcile/adapter/json/testdata/encode_golden.json`.
**Mock/Stub Requirements:** None — encode is a pure function over `reconcile.Result` returning `[]byte`; uses the real lifted types from Story 2.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./reconcile/adapter/json/...`)
- [x] No linting errors (`go vet`, `golangci-lint`)
- [x] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [x] Output envelope carries `"version":"reconcile-json/v1"`
- [x] `reconciled_at` is RFC3339 and sourced from `Options.ReconciledAt` when set
- [x] `findings[]` field names match the library `Finding` JSON struct tags exactly
- [x] `summary` and `ambiguous[]` are present in the output
- [x] Golden fixture matches byte-for-byte (after `reconciled_at` substitution)
- [x] Zero-value `Result{}` encodes to the canonical empty envelope (`findings:[]`, `ambiguous:[]`), never an error

**Manual Review:**
- [x] Code reviewed and approved
- [x] Package doc states the output envelope shape and the `Options.ReconciledAt` precedence rule
