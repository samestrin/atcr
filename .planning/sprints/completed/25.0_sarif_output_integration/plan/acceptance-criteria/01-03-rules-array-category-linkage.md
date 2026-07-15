# Acceptance Criteria: SARIF Rules Array (Category Linkage, Structural)

**Related User Story:** [01: SARIF Formatter Core](../user-stories/01-sarif-formatter-core.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package/function (`internal/report/sarif.go`) | `sarifRule` struct + rule-collection logic inside `renderSarif`; feeds `runs[0].tool.driver.rules[]` |
| Test Framework | `go test` (table-driven) | New tests in `internal/report/sarif_test.go` alongside AC 01-02's document-structure tests |
| Key Dependencies | stdlib only (`encoding/json`) | Deterministic iteration requires an explicit first-seen-order slice, not a bare Go map (map iteration is unordered) |

### Related Files (from codebase-discovery.json)

- [`internal/report/sarif.go`](../../../../../internal/report/sarif.go) — modify: add `sarifRule` struct (`id`, `shortDescription.text`, `fullDescription.text`) and a helper that iterates `findings` once, collecting distinct `reconcile.JSONFinding.Category` values in first-seen order into `runs[0].tool.driver.rules[]`; each `reconcile.JSONFinding.Category` maps 1:1 to one `sarifRule.id` (per plan's refinement: "one rule per distinct `reconcile.JSONFinding.Category`").
- [`internal/report/sarif_test.go`](../../../../../internal/report/sarif_test.go) — modify/create: unit tests asserting rule count matches distinct-category count, rule `id`/`shortDescription.text`/`fullDescription.text` content, first-seen ordering, and empty-findings behavior (`rules[]` present as `[]`, not `null`).
- [`internal/reconcile/emit.go`](../../../../../internal/reconcile/emit.go) — reference only (no modification): source of the `Category string` field ([`internal/reconcile/emit.go:68`](../../../../../internal/reconcile/emit.go)) this AC's rule-collection logic reads.
- [`internal/report/testdata/report.sarif.json`](../../../../../internal/report/testdata/report.sarif.json) — create/update: golden fixture should exercise at least two distinct categories so `rules[]` and `results[].ruleId` linkage is visible end-to-end.

### Technical References

- [GitHub Code Scanning SARIF Integration Constraints](../documentation/github-code-scanning-integration.md)
- [SARIF 2.1.0 Schema Reference](../documentation/sarif-schema-reference.md)

## Happy Path Scenarios
**Scenario 1: one rule per distinct category, first-seen order**
- **Given** findings with categories `["security", "style", "security"]` (in that input order)
- **When** `renderSarif` builds `runs[0].tool.driver.rules[]`
- **Then** the array has exactly 2 entries, ordered `["security", "style"]` (first-seen order, not alphabetical or count-sorted), and each entry's `id` equals its category string

**Scenario 2: rule description content is category-generic, not finding-specific (resolved design decision)**
- **Given** a finding with `Category: "security"`
- **When** its rule entry is built
- **Then** `id == "security"`, `shortDescription.text == "security"` (the category string itself), and `fullDescription.text` is a fixed, synthesized sentence templated on the category (e.g. `"ATCR findings categorized as 'security'."`) — `shortDescription.text`/`fullDescription.text` MUST NOT be derived from any individual finding's `Problem` or `Fix` text, since a single rule entry is shared across every result carrying that `ruleId` and per-finding detail already lives in `result.message.text`; richer descriptions/help URIs beyond this synthesized sentence are explicitly out of scope per the story's Constraints section

**Scenario 3: results reference their rule via ruleId (structural placeholder)**
- **Given** a finding with `Category: "security"` and its corresponding `sarifRule{id: "security"}`
- **When** the finding's `results[]` entry is built
- **Then** `results[i].ruleId == "security"`, matching the `id` of the corresponding entry in `rules[]` (referential integrity between `results[].ruleId` and `rules[].id`), even though this story does not implement severity→`level` mapping (Story 2) on that same result object

## Edge Cases
**Edge Case 1: empty findings produce an empty (not null) rules array**
- **Given** `findings = nil` or `[]reconcile.JSONFinding{}`
- **When** `renderSarif` is called
- **Then** `runs[0].tool.driver.rules` serializes as `[]`, never `null` — same nil-guard discipline as `results[]` (AC 01-02 Edge Case 1)

**Edge Case 2: empty Category string still produces a rule entry**
- **Given** a finding with `Category: ""` (empty string — a plausible malformed or legacy record)
- **When** `renderSarif` collects distinct categories
- **Then** the empty string is treated as one distinct category value like any other (one rule entry with `id: ""`) — no panic, no silent drop of the finding's `results[]` entry or its `ruleId` linkage; the behavior is documented as-is since category normalization/defaulting is not in this story's scope

**Edge Case 3: all findings share the same category**
- **Given** 5 findings all with `Category: "security"`
- **When** `renderSarif` builds `rules[]`
- **Then** exactly 1 rule entry is produced (not 5 duplicates), and all 5 `results[]` entries carry `ruleId == "security"`

## Error Conditions
**Error Scenario 1: no error path specific to rule collection**
- **Given** rule collection operates purely over in-memory string values already present on validated `JSONFinding` records
- **When** `renderSarif` runs
- **Then** there is no reachable error condition unique to this AC beyond the write/marshal errors already covered by AC 01-02 — rule collection cannot itself fail (no I/O, no parsing); this AC's "error" surface is fully covered by AC 01-02's Error Scenarios

## Performance Requirements
- **Response Time:** Rule collection is a single O(n) pass over `findings` using a `map[string]bool` (or equivalent) seen-set alongside an ordered slice to preserve first-seen order — no O(n²) category-deduplication scan.
- **Throughput:** N/A (single-process call); memory overhead is O(k) for k distinct categories, negligible relative to the findings slice itself.

## Security Considerations
- **Authentication/Authorization:** N/A — same trust boundary as AC 01-02 (local rendering over already-produced findings).
- **Input Validation:** `Category` is attacker-influenced (LLM reviewer output) free text; it flows into `sarifRule.id`/`shortDescription.text`/`fullDescription.text` via standard `encoding/json` marshaling (safe JSON-string escaping) — no raw concatenation, no code execution, no path usage. A category value containing unusual characters (unicode, punctuation) must not break JSON structure or rule/result `ruleId` matching (exact string equality is used, not sanitized/normalized comparison, so `"Security"` and `"security"` remain distinct rules — documented behavior, not a defect, since normalization is out of this story's scope).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A findings slice with repeated and distinct categories in a deliberately non-alphabetical input order (to prove first-seen ordering, not sorted ordering); an empty-Category-string case; a single-category-repeated case.
**Mock/Stub Requirements:** None — pure in-memory struct transformation, no I/O.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/report/...`)
- [x] No linting errors
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `rules[]` contains exactly one entry per distinct `Category`, in first-seen order
- [x] Each rule entry has `id == shortDescription.text == Category`, and `fullDescription.text` is a synthesized, category-generic sentence (never sourced from a finding's `Problem`/`Fix`)
- [x] Each `results[].ruleId` matches the `id` of its finding's category rule entry
- [x] `rules[]` serializes as `[]` (never `null`) for empty/nil findings

**Manual Review:**
- [x] Code reviewed and approved
