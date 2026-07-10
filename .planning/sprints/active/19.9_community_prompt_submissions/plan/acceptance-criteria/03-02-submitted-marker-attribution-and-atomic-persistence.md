# Acceptance Criteria: `submitted` Marker Carries Attribution and Persists Only via Atomic Write

**Related User Story:** [03: `submitted` Status Distinct from `Source`/Provenance](../user-stories/03-submitted-status-distinct-from-source.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct + atomic file writer | Marker struct (submitter identity, source persona name/version, timestamp, fixture-pass flag) serialized to a sidecar file |
| Serialization | YAML or JSON (match existing repo convention — `list.go` already uses `gopkg.in/yaml.v3` for persona metadata) | Pick one and keep it consistent with sibling marker/status files if any exist |
| Test Framework | Go `testing` package, `os`/`path/filepath` for symlink and tmpdir fixtures | Mirrors existing `writeFileAtomic` test patterns in `internal/personas`; testify is not used in this codebase |
| Key Dependencies | `internal/personas/unit.go:writeFileAtomic` (line 180) or `internal/atomicfs.WriteFileAtomic` (line 24) | No bare `os.WriteFile` anywhere in the new code path |

### Related Files (from codebase-discovery.json)
- `internal/personas/unit.go` (modify, or a new sibling file in the same package) — add the marker-writing function that calls `writeFileAtomic` (line 180) exclusively for persisting the `submitted` marker
- `internal/atomicfs/atomic.go` (reference only, no modification expected) — `WriteFileAtomic` (line 24) is an acceptable alternative persistence primitive if the marker lives outside the `personas` package's existing symlink-refusing writer
- `internal/personas/<new_file>_test.go` (create) — tests for marker content, atomic-write usage, and symlink refusal

## Design References
- [Status/Provenance Separation and Atomic Persistence](../documentation/status-provenance-and-atomic-writes.md) — the atomic-write helpers (`writeFileAtomic`, `atomicfs.WriteFileAtomic`) and the requirement to keep the marker outside `personas/community/`

## Happy Path Scenarios
**Scenario 1: A submission produces a marker with complete attribution metadata**
- **Given** the Story 2 submit flow has confirmed the local fixture gate passed for persona `foo`
- **When** the `submitted` marker is written for `foo`
- **Then** the persisted marker contains at minimum: submitter identity/handle, the persona name and version submitted, a submission timestamp, and a fixture-pass confirmation flag set to true

**Scenario 2: The marker is written through the existing atomic-write helper**
- **Given** a marker is about to be persisted for a fresh submission
- **When** the write occurs
- **Then** it goes through `writeFileAtomic` (sibling-temp-file-then-rename, 0600 perms) or `atomicfs.WriteFileAtomic` (0644) — never a direct `os.WriteFile` or `os.Create` call — verifiable by the write leaving no partially-written file visible to a concurrent reader mid-write

## Edge Cases
**Edge Case 1: Marker destination pre-exists as a symlink**
- **Given** an attacker or stale state has planted a symlink at the marker's destination path
- **When** the marker write is attempted
- **Then** the write is refused with an error (mirroring `writeFileAtomic`'s existing `Lstat`-based symlink guard at `internal/personas/unit.go:181`), and no data is written through the symlink

**Edge Case 2: Re-submission of the same persona overwrites the marker cleanly**
- **Given** a persona already has a `submitted` marker from a prior submission attempt
- **When** it is submitted again (e.g., after local edits)
- **Then** the marker is atomically replaced (sibling-temp-then-rename) with the new attribution metadata — no partial/mixed old-and-new content is ever readable

## Error Conditions
**Error Scenario 1: Marker write fails because the parent directory does not exist**
- Error message: `"failed to write persona temp file: <underlying os error>"` (or equivalent wording consistent with `writeFileAtomic`'s existing error wrapping at `internal/personas/unit.go:186`)
- HTTP status / error code: N/A (CLI process exits non-zero; no HTTP boundary)

**Error Scenario 2: Marker write attempted through a symlinked destination**
- Error message: `"refusing to write persona to symlink at <path>"`-style message (matching `writeFileAtomic`'s existing phrasing at `internal/personas/unit.go:182`) or an equivalent explicit refusal if using `atomicfs.WriteFileAtomic` plus a pre-check
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** Marker write completes as a single-file atomic operation (temp write + rename), no slower than existing `writeFileAtomic` calls for comparably sized persona files (sub-100ms on local disk for a small metadata file)
- **Throughput:** N/A — one marker per submission, not a bulk/batch operation

## Security Considerations
- **Authentication/Authorization:** Submitter identity in the marker is self-reported metadata (e.g., from `gh` CLI auth context or git config), not an authorization credential — must not be treated as a trust boundary by any downstream graduation logic
- **Input Validation:** Marker file permissions must match the writer used (0600 via `writeFileAtomic` or 0644 via `atomicfs.WriteFileAtomic`); symlink destinations must be refused per Edge Case 1; attribution string fields (submitter handle, persona name) must not be interpreted as paths without sanitization if used to construct the marker's own file path

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Temp directories for marker storage location (outside any `personas/community/` fixture path), sample attribution payloads (submitter handle, persona name/version, timestamp, fixture-pass flag)
**Mock/Stub Requirements:** None required — real filesystem operations against `t.TempDir()`; a pre-planted symlink fixture is needed for the symlink-refusal test (mirrors existing `writeFileAtomic` test setup in the package, if present, or follows the same pattern)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Marker struct/schema includes submitter identity, persona name/version, timestamp, and fixture-pass flag
- [ ] Marker persistence calls only `writeFileAtomic` or `atomicfs.WriteFileAtomic` — grep confirms no bare `os.WriteFile`/`os.Create` in the new code path
- [ ] A test confirms the marker write refuses a pre-existing symlink at the destination
- [ ] A test confirms re-submission atomically replaces a prior marker without partial-write exposure

**Manual Review:**
- [ ] Code reviewed and approved
