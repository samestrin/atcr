# Acceptance Criteria: Git Range Resolution

**Related User Story:** [01: CLI Review Workflow](../user-stories/01-cli-review-workflow.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Range Resolver | Go package | internal/gitrange/resolver.go |
| Git Interaction | os/exec | git rev-parse, symbolic-ref, merge-base, diff |
| Test Framework | testify | table-driven tests |

## Related Files
- `internal/gitrange/resolver.go` - create: range resolution decision tree and default branch detection
- `internal/gitrange/resolver_test.go` - create: unit tests for resolver logic
- `cmd/atcr/review.go` - modify: integrate resolver into review command
- `cmd/atcr/range.go` - create: `atcr range` pre-flight command that prints `Resolution` JSON without invoking any provider

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [Range Resolution](../documentation/range-resolution.md) — Authoritative spec for the decision tree, default-branch fallback chain, empty-range hard error, shallow-clone guard, and `Resolution` struct shape written to `manifest.json`.
- [CLI Architecture](../documentation/cli-architecture.md) — `MarkFlagsMutuallyExclusive` for `--base` / `--merge-commit`; `os/exec` `CommandContext` patterns; argv-only (no shell -c) for git invocations.

### Spec alignment notes

- The decision tree order is exact: explicit `--base`/`--head` → `--merge-commit SHA` (base = `SHA^`, head = `SHA`) → auto (`merge-base HEAD` against `origin/HEAD` → `origin/main` → `origin/master` → local `main` → local `master`).
- Empty range (0 commits OR `base == head`) is a **hard error before any provider call** — never a silent zero-findings pass. The error message must name the resolved SHAs.
- Shallow-clone detection via `git rev-parse --is-shallow-repository` produces a hard error with `git fetch --unshallow` guidance. The resolver does **not** auto-unshallow.
- `Resolution` struct: `Base`, `Head`, `DetectionMode` (`"explicit" | "merge_commit" | "auto"`), `DefaultBranch`, `CommitCount` (always ≥1), `Shallow`, `ResolvedAt`. The empty-range hard error is raised before any Resolution struct is emitted; every successful Resolution therefore has CommitCount ≥ 1.

## Happy Path Scenarios

**Scenario 1: Explicit range via flags**
- **Given** user provides `--base abc123 --head def456`
- **When** range resolution runs
- **Then** returned range is `{base: abc123, head: def456, mode: "explicit"}` with no git calls needed

**Scenario 2: Merge-commit detection**
- **Given** user provides `--merge-commit abc123` flag
- **When** range resolution runs
- **Then** resolver uses `git rev-parse abc123^` as base and `abc123` as head with mode "merge_commit"

**Scenario 3: Auto-detection via default branch**
- **Given** no range flags provided; user is on feature branch `feature/foo`
- **When** range resolution runs
- **Then** resolver detects default branch (e.g., origin/main), runs `git merge-base origin/main HEAD`, returns `{base: <merge-base>, head: HEAD, mode: "auto"}`

**Scenario 4: Default branch fallback chain**
- **Given** `origin/HEAD` does not exist; `origin/main` exists
- **When** resolver probes for default branch
- **Then** resolver falls back from origin/HEAD → origin/main and uses origin/main as the base reference

## Edge Cases

**Edge Case 1: base equals head (empty range)**
- **Given** merge-base resolves to same SHA as HEAD
- **When** range resolution completes
- **Then** resolver returns hard error: "empty range: base and head are the same commit (abc123)"

**Edge Case 2: Shallow clone detected**
- **Given** `git rev-parse --is-shallow-repository` returns true (shallow clone in use)
- **When** resolver detects shallow repo
- **Then** resolver returns a **hard error** (NOT a warning): "shallow clone detected: cannot review incomplete history; run `git fetch --unshallow` to enable full history access"
- **Note:** The resolver does **not** auto-unshallow. This prevents silent zero-findings CI gate clears on incomplete history.

**Edge Case 3: Detached HEAD state**
- **Given** user is in detached HEAD state
- **When** auto-detection runs
- **Then** resolver falls back to local branch chain; if no local branch found, returns error with guidance

## Error Conditions

**Error Scenario 1: All default branch probes fail**
- Error message: "could not detect default branch: tried origin/HEAD, origin/main, origin/master, local main, local master"
- Exit code: 1

**Error Scenario 2: Invalid SHA provided**
- Error message: "invalid git ref: 'xyz999' does not resolve to a commit"
- Exit code: 1

**Error Scenario 3: Not a git repository**
- Error message: "not a git repository (or any of the parent directories): .git"
- Exit code: 1

**Error Scenario 4: Shallow clone (hard error, not warning)**
- Error message: "shallow clone detected: cannot review incomplete history; run `git fetch --unshallow` to enable full history access"
- Exit code: 1
- Trigger: `git rev-parse --is-shallow-repository` returns true
- Note: Resolver must NOT auto-unshallow. Surfacing as a hard error (not a warning) prevents silent zero-findings CI gate clears on incomplete history, per `plan.md` Risk Mitigation and US-01 #14.

## Performance Requirements
- **Response Time:** Range resolution completes in <500ms (5 git calls max)
- **Throughput:** No concurrent git calls; sequential execution sufficient

## Security Considerations
- **Input Validation:** All user-provided SHAs/refs validated via `git rev-parse` before use
- **Injection Prevention:** No shell interpolation; all git args passed as separate exec arguments

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven tests covering each detection mode, fallback chain scenarios, and error cases
**Mock/Stub Requirements:** Mock `os/exec` calls to git using test helper that returns predefined outputs; use `exec.Command` interface for injection

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Decision tree resolves correctly for explicit, merge_commit, and auto modes
- [x] Default branch fallback chain: origin/HEAD → origin/main → origin/master → local main → local master
- [x] Empty range (base==head) produces hard error before any provider call
- [x] Shallow clone produces HARD ERROR (not warning) with `git fetch --unshallow` guidance; resolver does NOT auto-unshallow

**Manual Review:**
- [ ] Code reviewed and approved
