# Acceptance Criteria: Existing Personas Preserved; `--force` Semantics and Source Labeling Intact

**Related User Story:** [01: Community-Canonical Fetch-and-Pin Distribution](../user-stories/01-community-canonical-fetch-and-pin-distribution.md)
**Design References:** [fetch-and-distribution.md](../documentation/fetch-and-distribution.md)


## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go file-existence guard + CLI listing | `cmd/atcr/init.go`, `internal/personas/list.go` |
| Test Framework | Go `testing` | byte-for-byte file comparison before/after rerun |
| Key Dependencies | `os.Lstat`/`os.OpenFile(O_EXCL)` (existing `runInit` write helper), `commpersonas.List` | no new dependency; reuses existing never-overwrite machinery |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/init.go` (`runInit`, lines 76-78 force/anyExist gate, lines 96-118 O_EXCL write guard) — modify: preserve the per-file existence check so existing `.atcr/personas/<name>.{md,yaml}` files are never overwritten unless `--force` is passed.
- `internal/personas/list.go` (`PersonaMeta`, source labeling, lines 38-47, 117-172) — reference: labels rows `Source: "built-in"` vs `Source: "community"` and surfaces pinned versions for fetch-installed personas.
- `internal/personas/install.go` — reference: atomic install helper used for missing personas while existing files are skipped.
- `cmd/atcr/init_test.go` — modify: add a byte-for-byte equality test that pre-seeds `.atcr/personas/<name>.md` with hand-edited content and reruns `atcr init --force`.
- `cmd/atcr/personas_test.go` — modify: assert `atcr personas list` distinguishes `built-in` vs `community` rows with pinned versions after fetch-and-pin.
- `docs/personas-install.md` — modify: document `--force` semantics and source labeling.


## Happy Path Scenarios
**Scenario 1: Rerun with pre-existing hand-edited persona leaves it untouched**
- **Given** a workspace with a hand-edited `.atcr/personas/security.md` (modified from any built-in or community default)
- **When** `atcr init --force` runs against a mock registry
- **Then** `security.md` is byte-for-byte unchanged after the run, while any missing roster personas are installed fresh from the fetch-and-pin path alongside it

**Scenario 2: `atcr personas list` distinguishes sources after fetch-and-pin install**
- **Given** a workspace with personas installed via fetch-and-pin (community, pinned versions) alongside any remaining embedded built-ins
- **When** `atcr personas list` runs
- **Then** the output table shows `Source: community` with the fetched pin version for fetched personas, and `Source: built-in` with `Version: built-in` for any that remain built-in, matching the existing `PersonaMeta`/`renderPersonaList` format

## Edge Cases
**Edge Case 1: `--force` without any pre-existing files**
- **Given** an empty workspace
- **When** `atcr init --force` runs
- **Then** behavior is identical to `atcr init` without `--force` (nothing to overwrite), and all roster personas are installed via fetch-and-pin

**Edge Case 2: Missing community personas install alongside existing hand-edited ones**
- **Given** a workspace with a hand-edited `.atcr/personas/security.md` and no other persona files present
- **When** `atcr init` (no `--force`) runs against a mock registry advertising `security` and `performance` personas
- **Then** `performance` is installed fresh from the registry while `security.md` is left untouched (not overwritten, not duplicated), consistent with the per-file existence check

## Error Conditions
**Error Scenario 1: `atcr init` without `--force` on a workspace with existing config (unchanged pre-story contract)**
- Error message: `"config already exists at .atcr/config.yaml — use --force to overwrite"` (existing `runInit` line 77, unchanged by this story)
- HTTP status / error code: N/A (local CLI error); process exits non-zero via `usageError`

## Performance Requirements
- **Response Time:** Per-file existence checks (`os.Lstat`) add negligible overhead (microseconds) versus the network fetch cost already budgeted in AC 01-02.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** File-existence checks must resolve symlinks safely (reusing the existing `O_EXCL`/no-follow-through-symlink write discipline already present in `runInit`'s `write` closure) so a pre-planted symlink at a persona path cannot be used to redirect a fetch-and-pin write outside `.atcr/personas/`.

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** A pre-seeded `.atcr/personas/` directory with one hand-edited `.md` file; a mock registry index listing that persona plus at least one other.
**Mock/Stub Requirements:** `httptest.NewServer` mock registry; file-content comparison (before/after byte equality) rather than mocking the filesystem.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Existing on-disk `.md`/`.yaml` personas are never overwritten by the fetch-and-pin path, with or without `--force`
- [ ] Missing community personas install alongside pre-existing hand-edited ones (no clobber, no duplication)
- [ ] `atcr personas list` distinguishes `built-in` vs `community` sources with pinned versions shown for fetch-installed personas
- [ ] Byte-for-byte equality test passes for a pre-seeded hand-edited persona across an `init --force` rerun

**Manual Review:**
- [ ] Code reviewed and approved
