# Acceptance Criteria: SelectEligibleSkeptics Language Routing

**Related User Story:** [03: Language-Aware Skeptic Routing](../user-stories/03-language-aware-skeptic-routing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Routing logic | Go package (`internal/verify`) | Two-partition reorder in `select.go` |
| Extension normalization | Go helper (`normalizeExt`) | Strips leading dot, lowercases |
| Tie-break | scores `map[string]float64` | Nil-safe; degrades to alphabetical |
| Test Framework | go test / testify | Table-driven unit tests |
| Key Dependencies | `path/filepath`, `sort`, standard `strings` | |

## Related Files
- `internal/verify/select.go:55` - modify: add 4th `scores map[string]float64` parameter to `SelectEligibleSkeptics`; insert two-partition reorder after `sort.Strings(names)` using `normalizeExt`
- `internal/verify/select.go` - modify: add `normalizeExt` private helper that strips a single leading dot and lowercases the result
- `internal/verify/select_test.go:44` - modify: add test cases for language-matched selection, no-match fallback, and multiple-match score tie-break

## Happy Path Scenarios

**Scenario 1: Language-matched skeptic selected first**
- **Given** a pool of skeptics `["alpha", "idiomatic", "zeta"]` where `idiomatic.Language = ["go"]` and `alpha`, `zeta` have no language
- **When** `SelectEligibleSkeptics` is called with `finding.File = "pkg/foo.go"`, `n=1`, and a non-nil scores map
- **Then** the returned slice contains `["idiomatic"]` — the language-matched skeptic is selected before alphabetically earlier generalists

**Scenario 2: Multiple language-matched skeptics ordered by score then alphabetically**
- **Given** skeptics `["alpha", "idiomatic", "sentinel"]` where both `idiomatic` and `sentinel` declare `Language = ["go"]` with scores `{"idiomatic": 0.9, "sentinel": 0.7}`
- **When** `SelectEligibleSkeptics` is called with `finding.File = "handler.go"`, `n=2`, and the scores map
- **Then** the returned slice is `["idiomatic", "sentinel"]` (higher score first within the matched partition)

**Scenario 3: Two matched skeptics with equal scores fall back to alphabetical**
- **Given** skeptics `["bravo", "alpha"]` both with `Language = ["go"]` and equal scores `0.8`
- **When** `SelectEligibleSkeptics` is called with `finding.File = "main.go"`, `n=2`
- **Then** the returned slice is `["alpha", "bravo"]` (alphabetical within matched partition after score tie)

## Edge Cases

**Edge Case 1: Nil scores map degrades to alphabetical within matched partition**
- **Given** matched skeptics `["zeta", "alpha"]` both with `Language = ["go"]` and `scores = nil`
- **When** `SelectEligibleSkeptics` is called with `finding.File = "util.go"`, `n=2`
- **Then** the returned slice is `["alpha", "zeta"]` (alphabetical; no panic)

**Edge Case 2: Extension with leading dot is normalized correctly**
- **Given** a skeptic with `Language = ["go"]`
- **When** `normalizeExt` is called on `".go"` (as returned by `filepath.Ext`)
- **Then** the result is `"go"` (dot stripped, already lowercase)

**Edge Case 3: Mixed-case extension normalized for matching**
- **Given** a finding file `"Handler.GO"` and a skeptic with `Language = ["go"]`
- **When** `normalizeExt(filepath.Ext("Handler.GO"))` runs
- **Then** the result is `"go"`, matching the skeptic's language entry

**Edge Case 4: n-cap unchanged — only reorder affects selection**
- **Given** a pool of 5 skeptics with 2 matched and `n=2`
- **When** `SelectEligibleSkeptics` is called
- **Then** exactly 2 skeptics are returned, both from the matched partition; the n-cap slice logic at lines 84–86 is not modified

## Error Conditions

**Error Scenario 1: Empty finding file path**
- **Given** `finding.File = ""`
- **When** `normalizeExt(filepath.Ext(""))` runs
- **Then** the result is `""` and no skeptic matches (all treated as unmatched); no error or log output

**Error Scenario 2: Finding file with no extension**
- **Given** `finding.File = "Makefile"`
- **When** `SelectEligibleSkeptics` runs
- **Then** no language match occurs; fallback pool order is used; no error

## Performance Requirements
- **Response Time:** Partition reorder must complete in O(n) time (single pass for matched, single pass for unmatched, one `append`). The sort within the matched partition is O(k log k) where k ≤ n (typically small, ≤10).
- **Throughput:** `SelectEligibleSkeptics` is called once per finding in the verify pipeline; overhead must be negligible (< 1µs for typical pool sizes ≤ 20 skeptics).
- **Allocation:** Pre-allocate `matched` and `unmatched` with `make([]string, 0, len(names))` to avoid growth copies.

## Security Considerations
- **Input Validation:** `normalizeExt` only strips a leading dot and lowercases; it does not execute or interpret the extension string. No injection risk.
- **Authentication/Authorization:** No auth impact; routing operates on already-loaded in-memory config.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven test cases with varying pools (matched/unmatched combos), `n` values (1, 2, all), score maps (nil, partial, full), and file extensions (`.go`, `.GO`, `.ts`, no extension, empty string).
**Mock/Stub Requirements:** Construct `AgentConfig` structs directly with `Language` set; no external dependencies to mock. Pass a stub `Finding` with only the `File` field populated.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `SelectEligibleSkeptics` signature updated to include 4th `scores map[string]float64` parameter
- [ ] Two-partition reorder implemented: matched skeptics precede unmatched in the slice before the n-cap is applied
- [ ] `normalizeExt` helper exists and is shared between `applyDefaults` and `SelectEligibleSkeptics`
- [ ] Unit tests cover language-match, no-match fallback, tie-break by score, tie-break by alphabet, and nil-scores cases

**Manual Review:**
- [ ] Code reviewed and approved
