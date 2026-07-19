# Acceptance Criteria: `--no-sandbox` Risk Is Documented and Cross-Linked, Verified Accurate Against Shipped CLI Behavior

**Related User Story:** [04: Document the Auto-Fix Sandbox Security Posture and `--no-sandbox` Risk](../user-stories/04-document-auto-fix-sandbox-security-posture.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/` tree) | Same target file(s) as AC 04-01 (`docs/auto-fix.md` or an added `docs/execution.md` section); this AC covers the `--no-sandbox` risk content and the cross-link/accuracy-reconciliation requirement specifically. |
| Test Framework | `cmd/atcr/docs_audit_test.go` (existing suite) + manual reconciliation against Stories 1-3's merged CLI code | No new automated test asserts the exact warning wording (the story's Potential Risks table deliberately avoids pinning docs to a verbatim CLI string, to prevent future wording drift from failing docs); accuracy of intent (flag name, default-on posture, warn-every-run behavior) is a manual gate. |
| Key Dependencies | Story 3's `--no-sandbox` flag and its CLI warning text (dependency; not yet merged as of this AC's authoring — see Story Details "Dependencies"), `docs/execution.md`'s existing "Security posture" section (cross-link target) | |

## Related Files
- `docs/auto-fix.md` (or the `docs/execution.md` section from AC 04-01) - modify: add the `--no-sandbox` risk/warning subsection.
- `docs/ci-integration.md` - modify: add a cross-link to the new `--no-sandbox` content next to the file's existing `--auto-fix` mention (`docs/ci-integration.md:30`, currently only about the `--axi --auto-fix` flag-combination usage error), per the story's Integration Points.
- `docs/agentic-consumption.md` - modify: add a cross-link next to the file's existing `--auto-fix` mentions (`docs/agentic-consumption.md:111-112`, currently only about the `--axi --auto-fix` incompatibility), per the story's Integration Points.
- `docs/execution.md` - reference only: the "Security posture" section (lines 123-130) is the tone/structure model to mirror, not duplicate, for the new `--no-sandbox` risk language.

### Related Files (from codebase-discovery.json)

- `docs/auto-fix.md` (or the `docs/execution.md` section from AC 04-01) — update: add the `--no-sandbox` risk/warning subsection (discovery `files_to_create`/`files_to_modify`, pending the structural choice made in AC 04-01).
- `docs/ci-integration.md:30` — update: cross-link the new `--no-sandbox` content beside the existing `--auto-fix` mention (story Integration Points).
- `docs/agentic-consumption.md:111-112` — update: cross-link the new content beside the existing `--auto-fix` mentions (story Integration Points).

## Happy Path Scenarios
**Scenario 1: Reader finds what `--no-sandbox` bypasses and why it is dangerous**
- **Given** a reader opens the `--no-sandbox` documentation section
- **When** they read it
- **Then** they find a plain statement that `--no-sandbox` disables the container-isolation validation path and runs the post-apply validation command directly on the host, with the specific risk named: untrusted, potentially LLM-hallucinated or prompt-injected code then executes with the full privileges of the `atcr` process (no network isolation, no read-only rootfs, no capability drop, no non-root confinement)

**Scenario 2: Reader finds the "warns on every run" claim, stated as intent rather than a quoted string**
- **Given** the same section
- **When** the reader looks for what happens when they pass `--no-sandbox`
- **Then** they find a statement that every `--no-sandbox` invocation prints a security warning (not just the first use), phrased as a description of intent ("a warning is printed on every run") rather than a verbatim quote of the CLI's exact wording — per the story's Potential Risks mitigation, so a future CLI wording tweak does not require a synchronized docs edit to stay accurate

**Scenario 3: Reader finds concrete guidance on acceptable use**
- **Given** the same section
- **When** the reader is deciding whether to use `--no-sandbox`
- **Then** they find concrete guidance naming at least one legitimate scenario (e.g., Docker is not installed/available in the environment) alongside an explicit statement of the risk being accepted by choosing it, so the guidance is actionable rather than a bare "don't do this"

**Scenario 4: Reader following a cross-link reaches the security content**
- **Given** a reader starts from `docs/ci-integration.md` or `docs/agentic-consumption.md` (both already mention `--auto-fix` in passing)
- **When** they follow the new cross-link added by this story
- **Then** they land on the `--no-sandbox` risk content without needing to already know it lives in `docs/auto-fix.md` (or the extended `docs/execution.md`)

## Edge Cases
**Edge Case 1: Docs drafted before Stories 1-3 finalize the flag name/behavior**
- **Given** this story's content may be drafted in parallel with Stories 1-3 (per Dependencies: "drafting can proceed in parallel and be reconciled at the end")
- **When** Stories 1-3 merge with their final `--no-sandbox` flag name, default-on posture, and warning trigger condition
- **Then** this doc's content is re-read against that merged code one final time before this story's docs PR merges (per Potential Risks row 1), and any mismatch (wrong flag name, wrong trigger condition — e.g. "warns once" vs. "warns every run") is corrected before merge, not after

**Edge Case 2: Doc must not duplicate `docs/execution.md`'s isolation-mechanics prose**
- **Given** the doc needs to explain what `--no-sandbox` removes
- **When** describing the isolation properties being bypassed
- **Then** the doc cross-links to `docs/execution.md`'s "What the sandbox guarantees" section rather than re-listing every guarantee (no network, read-only rootfs, cap-drop, non-root, resource caps) verbatim a second time, avoiding a second source of truth that can drift (per Potential Risks row 2)

**Edge Case 3: `TestDocsClaimedFlagsAreReal` gate on the `--no-sandbox` flag mention**
- **Given** the doc uses the exact idiom `` `--no-sandbox` flag `` (backtick-quoted name immediately followed by the word "flag")
- **When** `cmd/atcr/docs_audit_test.go`'s `TestDocsClaimedFlagsAreReal` runs
- **Then** it will fail CI unless `--no-sandbox` is already a real, registered CLI flag by the time this doc merges — which is exactly why this story's Dependencies require reconciling against Stories 1-3's merged flag before finalizing, not just before drafting

## Error Conditions
**Error Scenario 1: Doc claims a config-level `no_sandbox` option that does not exist**
- Condition: the doc states or implies `--no-sandbox` can be set via `.atcr/config.yaml`'s `auto_fix:` block (e.g. a fictional `auto_fix.no_sandbox: true`)
- This is explicitly out of scope per the story's Constraints ("must not invent capabilities... unless Story 2/3 actually implements one — verify before publishing") and per this plan's grounding rule; catchable only by manual review against the final `AutoFixConfig` struct and Story 3's flag implementation, since no config-schema-drift test exists for this field
- Error message: none automated — a reviewer must reject the PR description/diff on this basis if found

**Error Scenario 2: Doc references `--no-sandbox` before it exists in the compiled CLI**
- Condition: this AC's content merges to `docs/` while `--no-sandbox` is still unimplemented (Stories 1-3 incomplete)
- Error message: `"%s documents \`--%s\` as a flag but no such CLI flag exists"` (`TestDocsClaimedFlagsAreReal`, `cmd/atcr/docs_audit_test.go:594`)
- Exit status: `go test ./cmd/atcr/...` fails, blocking CI until either the flag lands or the doc's merge is sequenced after Story 3

## Performance Requirements
- **Response Time:** N/A (static content). CI-relevant: `go test ./cmd/atcr/...` must continue to pass in the same order-of-magnitude runtime as today since no new test code is added by this docs-only story.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** The doc itself is the control here — it exists specifically to prevent an operator from reaching for `--no-sandbox` without understanding that it removes host-privileged execution containment for untrusted, potentially adversarial (prompt-injected or hallucinated) LLM-generated code. Content must state the risk in concrete terms (full host privileges of the `atcr` process, no network/filesystem/capability containment) rather than vague hand-waving, and must not undersell it to make the flag seem more casual than it is.

## Test Implementation Guidance
**Test Type:** DOCS AUDIT (existing automated suite, no new test code) + MANUAL (accuracy reconciliation against Stories 1-3)
**Test Data Requirements:** None beyond live repo state (compiled Cobra tree via `cmd/atcr/docs_audit_test.go`'s `canonicalCommands`/`canonicalFlags`).
**Mock/Stub Requirements:** None. Verification sequence: (1) draft content early using the flag name/behavior fixed by the epic/plan (`--no-sandbox`, default-on sandboxing, warn-every-run intent); (2) once Stories 1-3 merge, re-read the actual CLI warning text and flag registration and reconcile any drift; (3) run `go test ./cmd/atcr/...` to confirm `TestDocsClaimedFlagsAreReal` and `TestDocsReferenceOnlyRealCommands` pass now that `--no-sandbox` is a real flag; (4) manually click through the new cross-links from `docs/ci-integration.md` and `docs/agentic-consumption.md` to confirm they resolve.

## Definition of Done

**Auto-Verified:**
- [ ] `go build ./...` succeeds
- [ ] `go test ./cmd/atcr/...` passes, including `TestDocsClaimedFlagsAreReal` (confirms `--no-sandbox` is a real flag by merge time) and `TestDocsReferenceOnlyRealCommands`
- [ ] No dangling markdown links from the new cross-links in `docs/ci-integration.md` / `docs/agentic-consumption.md`

**Story-Specific:**
- [ ] Doc states what `--no-sandbox` bypasses and the concrete host-privilege-execution risk
- [ ] Doc states a warning prints on every invocation (as intent, not a verbatim CLI string)
- [ ] Doc gives concrete guidance on when use might be acceptable (e.g., no Docker available) and what risk is being accepted
- [ ] `docs/ci-integration.md` and `docs/agentic-consumption.md` cross-link to the new content from their existing `--auto-fix` mentions
- [ ] Content reconciled against Stories 1-3's final merged `--no-sandbox` flag name, default behavior, and warning trigger condition immediately before this story's docs PR merges

**Manual Review:**
- [ ] Code reviewed and approved
