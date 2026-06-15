# Acceptance Criteria: Manifest Stage & Summary Verdict Updates

**Related User Story:** [03: Confidence v2 & Re-emit](../user-stories/03-confidence-v2-re-emit.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Functions | Go functions in `internal/verify/emit_manifest.go` and `internal/verify/emit_summary.go` | JSON artifact updates |
| Test Framework | go test + testify | Table-driven with temp dir |
| Key Dependencies | `internal/payload` (Manifest, WriteManifest), `encoding/json` | Uses existing manifest writer |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/payload/manifest.go:16` - reference: `Manifest` struct
- `internal/payload/manifest.go:32` - reference: `Stages []string` field
- `internal/payload/manifest.go:86` - reference: `WriteManifest` (atomic write)

- `internal/verify/emit_manifest.go` - create: `UpdateManifestStage(reviewDir string) error`
- `internal/verify/emit_summary.go` - create: `UpdateSummaryVerdicts(reviewDir string, counts VerdictCounts) error`, summary struct with `verdictCounts` field
- `internal/verify/emit_manifest_test.go` - create: idempotency and atomic write tests
- `internal/verify/emit_summary_test.go` - create: verdict count update tests
- `internal/payload/manifest.go` - modify: none (reference for `Manifest`, `WriteManifest`, `atomicWriteFile`)

## Happy Path Scenarios

**Scenario 1: UpdateManifestStage adds "verify" to stages**
- **Given** a `manifest.json` with `stages: ["review"]`
- **When** `UpdateManifestStage(reviewDir)` is called
- **Then** `manifest.json` is updated to `stages: ["review", "verify"]`, written atomically

**Scenario 2: UpdateManifestStage is idempotent**
- **Given** a `manifest.json` with `stages: ["review", "verify"]`
- **When** `UpdateManifestStage(reviewDir)` is called
- **Then** `manifest.json` is unchanged (no duplicate "verify" entry), returns nil

**Scenario 3: UpdateSummaryVerdicts writes verdict counts**
- **Given** a `summary.json` with existing fields and `VerdictCounts{Confirmed: 5, Refuted: 2, Unverifiable: 1}`
- **When** `UpdateSummaryVerdicts(reviewDir, counts)` is called
- **Then** `summary.json` is updated with `"verdictCounts": {"confirmed": 5, "refuted": 2, "unverifiable": 1}`, written atomically

## Edge Cases

**Edge Case 1: Manifest has no stages field (1.x manifest)**
- **Given** a `manifest.json` without a `stages` field (parsed as nil/empty slice)
- **When** `UpdateManifestStage` is called
- **Then** `stages` is set to `["verify"]` (WriteManifest defaults nil stages to `["review"]`, then "verify" is appended — result depends on implementation: either `["review", "verify"]` via WriteManifest default, or `["verify"]` if stages was explicitly empty)

**Edge Case 2: Summary.json has no verdictCounts field (1.x summary)**
- **Given** a `summary.json` without `verdictCounts`
- **When** `UpdateSummaryVerdicts` is called
- **Then** `verdictCounts` field is added; existing fields remain intact

**Edge Case 3: Summary verdict counts all zero**
- **Given** `VerdictCounts{Confirmed: 0, Refuted: 0, Unverifiable: 0}`
- **When** `UpdateSummaryVerdicts` is called
- **Then** `verdictCounts` field is written with all zeros (or omitted via `omitempty` if all zero — implementation decision; story says `omitempty`)

## Error Conditions

**Error Scenario 1: Manifest.json does not exist**
- Error message: wrapped `os.ErrNotExist`
- Behavior: Returns error

**Error Scenario 2: Summary.json does not exist**
- Error message: wrapped `os.ErrNotExist`
- Behavior: Returns error

**Error Scenario 3: Malformed manifest.json**
- Error message: wrapped JSON unmarshal error
- Behavior: Returns error, existing file untouched

## Performance Requirements
- **Response Time:** < 50ms per update
- **Throughput:** Single update per pipeline run

## Security Considerations
- **Input Validation:** VerdictCounts values are non-negative integers (enforced by Go type system).
- **Atomic write:** Both functions use temp file + `os.Rename` pattern.
- **Schema compatibility:** `verdictCounts` uses `omitempty` so 1.x summaries without the field remain parseable by old consumers.

## Test Implementation Guidance
**Test Type:** UNIT (with filesystem I/O via `t.TempDir()`)
**Test Data Requirements:** Sample `manifest.json` and `summary.json` files. VerdictCounts structs.
**Mock/Stub Requirements:** None — uses real filesystem and existing `payload.WriteManifest`.

```go
func TestUpdateManifestStage_Idempotent(t *testing.T) {
    dir := t.TempDir()
    m := payload.Manifest{
        Base: "abc123", Head: "def456",
        Stages: []string{"review", "verify"},
    }
    require.NoError(t, payload.WriteManifest(filepath.Join(dir, "manifest.json"), &m))

    require.NoError(t, UpdateManifestStage(dir))

    data, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
    var got payload.Manifest
    require.NoError(t, json.Unmarshal(data, &got))
    assert.Equal(t, []string{"review", "verify"}, got.Stages)
}

func TestUpdateSummaryVerdicts(t *testing.T) {
    dir := t.TempDir()
    // Write initial summary
    summary := map[string]any{"total_findings": 10}
    data, _ := json.MarshalIndent(summary, "", "  ")
    os.WriteFile(filepath.Join(dir, "summary.json"), data, 0o644)

    counts := VerdictCounts{Confirmed: 5, Refuted: 2, Unverifiable: 1}
    require.NoError(t, UpdateSummaryVerdicts(dir, counts))

    updated, _ := os.ReadFile(filepath.Join(dir, "summary.json"))
    var got map[string]any
    json.Unmarshal(updated, &got)
    vc := got["verdictCounts"].(map[string]any)
    assert.Equal(t, float64(5), vc["confirmed"])
    assert.Equal(t, float64(2), vc["refuted"])
}
```

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/verify/...`)
- [x] No linting errors (`go vet ./internal/verify/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `UpdateManifestStage` appends "verify" to stages (idempotent — no duplicates)
- [x] `UpdateManifestStage` uses `payload.WriteManifest` for atomic write
- [x] `UpdateSummaryVerdicts` adds/updates `verdictCounts` field with `omitempty`
- [x] Both functions handle missing files with appropriate errors
- [x] Schema backward compatibility: 1.x manifests/summaries remain parseable

**Manual Review:**
- [x] Code reviewed and approved
