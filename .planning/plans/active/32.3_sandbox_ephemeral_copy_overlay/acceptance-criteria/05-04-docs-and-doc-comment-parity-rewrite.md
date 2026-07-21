# Acceptance Criteria: docs/auto-fix.md and autofix_exec.go Doc-Comment Parity Rewrite

**Related User Story:** [05: Regression Proof and Documentation Parity](../user-stories/05-regression-proof-and-docs-parity.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/auto-fix.md`) and a Go doc comment (`internal/verify/autofix_exec.go`) | Prose-only change, no executable code or logic touched |
| Test Framework | N/A — verified by manual review and `grep`, not by `go test` | This AC is documentation, not code |
| Key Dependencies | None | Plain text edits |

## Related Files
- `docs/auto-fix.md` - modify: three passages rewritten together — the "mount mode is still read-only" claim (line 47, inside the "What runs in the sandbox" section spanning lines 38-53), the standalone "Limitation (read-only `/work`)" paragraph (lines 55-60), and the EROFS "effectively Go-only" blockquote (lines 62-71) — plus a new note on the `/bin/sh` + `cp -a` image requirement added to the sandbox-requirements guidance, per plan.md's Refinement Decisions (2026-07-21)
- `internal/verify/autofix_exec.go` - modify: `ResolveAutoFixSandbox`'s doc comment (lines 47-55), which verbatim-duplicates the "Read-only /work limitation (effectively Go-only today)" claim, rewritten in the same pass so it no longer disagrees with `docs/auto-fix.md`
- `docs/execution.md` - reference only (not modified): lines 51-62 ("What the sandbox guarantees") and lines 86-90 describe `--exec`'s read-only guarantee, which must remain textually accurate and unchanged by this story since `--exec`'s two call sites in `internal/tools/exec_tools.go` never set `Writable: true`

## Happy Path Scenarios
**Scenario 1: docs/auto-fix.md's "still read-only" claim is corrected**
- **Given** `docs/auto-fix.md:47` currently states unconditionally "The **mount mode is still read-only**"
- **When** the rewrite lands
- **Then** the passage instead states that `/work` is read-only by default and writable only when the internal `Writable: true` opt-in is set by the caller (`RunSandboxedValidation`, not an operator-facing config option), matching the actual behavior Stories 2-4 ship

**Scenario 2: The "Limitation (read-only /work)" paragraph and EROFS blockquote are replaced**
- **Given** `docs/auto-fix.md:55-60` (the limitation paragraph) and `:62-71` (the EROFS "effectively Go-only" blockquote) both describe a permanent limitation that no longer exists
- **When** the rewrite lands
- **Then** both are replaced with a description of the ephemeral `/src:ro` + `/work` tmpfs copy mechanism (the `cp -a` setup step), stating plainly that non-Go `validate_command`s (`npm run build`, `cargo build`, code generation, etc.) are now supported because they write into the ephemeral `/work` copy rather than a read-only mount

**Scenario 3: New image-requirement note is added**
- **Given** Command-mode `Writable:true` wraps execution in `/bin/sh -c '... && exec "$@"'` (per plan.md's Implementation Strategy)
- **When** the rewrite lands
- **Then** `docs/auto-fix.md`'s sandbox-requirements guidance gains a note that the operator's validation image must provide `/bin/sh` and `cp -a` (true for `alpine`/`golang`-family images, false for `distroless`/`scratch`), per plan.md's Refinement Decisions (2026-07-21)

**Scenario 4: autofix_exec.go's doc comment matches the new docs wording**
- **Given** `internal/verify/autofix_exec.go:47-55`'s `ResolveAutoFixSandbox` doc comment currently duplicates the stale "effectively Go-only today" limitation verbatim
- **When** the rewrite lands
- **Then** the comment is rewritten in the same commit/pass as the `docs/auto-fix.md` edits, using matching wording for the shared claim (the opt-in writable overlay, the `/bin/sh`/`cp -a` requirement) so the two descriptions cannot drift apart in meaning again

## Edge Cases
**Edge Case 1: docs/execution.md is untouched and remains accurate**
- **Given** `docs/execution.md:51-62` and `:86-90` describe `--exec`'s read-only guarantee (mounted read-only at `/work`, "the run cannot mutate your working tree")
- **When** this story's docs rewrite lands
- **Then** `docs/execution.md` receives zero edits, and its claims remain textually true because `--exec`'s two call sites never set `Writable: true` — this is the documentation-level proof paired with AC 05-03's test-level proof

**Edge Case 2: The `internal/sandbox` package doc's hard MUST is unaffected**
- **Given** the `internal/sandbox` package doc states a hard MUST that a snapshot mount gives "a read-only view of the snapshot ... the run cannot mutate the work tree"
- **When** this story's docs rewrite lands
- **Then** that package doc is not touched by this story (it is out of the named scope: only `docs/auto-fix.md` and `autofix_exec.go`'s doc comment are edited), and its claim remains accurate because it describes `Writable:false` behavior, the default and the only mode `--exec` ever uses

## Error Conditions
**Error Scenario 1: Stale phrasing surviving the edit is a review-blocking defect**
- If either file still contains the literal phrase "effectively Go-only" or an unconditional "still read-only" claim after the edit, that is a defect this AC's Definition of Done must catch before sign-off.
- Error message: not applicable — this is prose, not a runtime error; the "error condition" here is a documentation defect (stale/contradictory claim), not an application-level failure.
- HTTP status / error code: not applicable (documentation-only change, no HTTP surface).

**Error Scenario 2: The two files disagree after independent hand-editing**
- If `docs/auto-fix.md` and `internal/verify/autofix_exec.go`'s doc comment are edited in separate passes and end up describing the mechanism differently (e.g. one mentions the `/bin/sh`/`cp -a` requirement and the other omits it), that is the exact risk named in the story's Potential Risks table.
- Error message: not applicable — caught by the `grep`-based manual review described below, not by a compiler or test failure.
- HTTP status / error code: not applicable.

## Performance Requirements
- **Response Time:** Not applicable — documentation-only change with no runtime code path.
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** Not applicable — documentation-only change.
- **Input Validation:** Not applicable — no new input surface; the rewrite must not imply the writable overlay is operator-configurable (it is not — `Writable: true` is `RunSandboxedValidation`'s internal, non-configurable choice per plan.md's Refinement Decisions), avoiding introducing a misleading claim about a config knob that does not exist.

## Test Implementation Guidance
**Test Type:** DOCUMENTATION (manual/review-based verification, not an automated test)
**Test Data Requirements:** N/A — verification is `grep -n "read-only\|Go-only" docs/auto-fix.md internal/verify/autofix_exec.go` after the edit (per the story's Potential Risks mitigation) to confirm no stale phrasing survives, followed by a side-by-side diff review of the two files' wording for the shared claim.
**Mock/Stub Requirements:** N/A — no test harness involved; verification is `git diff` scope-checking (only the three named `docs/auto-fix.md` passages plus the one `autofix_exec.go` doc comment are touched) and manual reading for coherence.

## Definition of Done
**Auto-Verified:**
- [ ] Build succeeds (`go build ./...`) — the doc-comment edit must not break Go syntax
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] `git diff` on `docs/auto-fix.md` touches only the three identified passages (lines ~47, ~55-60, ~62-71) plus the new image-requirement note; `git diff` on `internal/verify/autofix_exec.go` touches only the `ResolveAutoFixSandbox` doc comment (lines 47-55), no executable code

**Story-Specific:**
- [ ] `grep -n "effectively Go-only" docs/auto-fix.md internal/verify/autofix_exec.go` returns no matches
- [ ] `grep -n "still read-only" docs/auto-fix.md` returns no unconditional match (a correctly-scoped "read-only by default, writable when Writable:true" phrasing is acceptable)
- [ ] `docs/auto-fix.md` states non-Go `validate_command`s (npm, cargo, etc.) are now supported via the ephemeral `/src:ro` + `/work` tmpfs copy
- [ ] `docs/auto-fix.md` documents the `/bin/sh` + `cp -a` image requirement for Command-mode `Writable:true`
- [ ] `internal/verify/autofix_exec.go`'s doc comment no longer disagrees with `docs/auto-fix.md`'s description of the same mechanism
- [ ] `docs/execution.md` and the `internal/sandbox` package doc receive zero edits and remain textually accurate for `--exec`'s `Writable:false`-only usage

**Manual Review:**
- [ ] Code reviewed and approved
