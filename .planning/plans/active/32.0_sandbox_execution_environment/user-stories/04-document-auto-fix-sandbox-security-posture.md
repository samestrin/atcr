# User Story 4: Document the Auto-Fix Sandbox Security Posture and `--no-sandbox` Risk

**Plan:** [32.0: Sandboxed Auto-Fix Validation](../plan.md)

## User Story

**As an** ATCR operator deciding whether and how to run `atcr --auto-fix` in a local environment or an unattended CI runner
**I want** documentation that explains the default sandboxed validation behavior, what `--no-sandbox` does, and the concrete risk of using it
**So that** I can make an informed decision about my validation isolation posture instead of discovering the tradeoff only after reading source code or hitting a CLI warning mid-run

## Story Context

- **Background:** `docs/execution.md` is the only existing doc that documents container-isolation guarantees (network isolation, read-only mount, resource caps, non-root, preflight checks) and the `sandbox:` config block, written for the unrelated `--exec` reviewer-reproduction feature (Epic 11.0). Stories 1-3 of this plan wire that same `internal/sandbox` isolation into the `--auto-fix` validation step (Epic 17.0) as the new default, and add a `--no-sandbox` opt-out flag with CLI warnings. Today, the `auto_fix:` config block (`internal/registry/autofix.go:AutoFixConfig` — `ApplyTarget`, `ValidateCommand`, `ValidateTimeout`) is entirely undocumented in `docs/`; only passing mentions of `--auto-fix` exist in `docs/ci-integration.md` and `docs/agentic-consumption.md`, and neither `docs/registry.md` nor any other doc covers the `sandbox:` or `auto_fix:` blocks together. The epic's Acceptance Criterion 2 explicitly requires the `--no-sandbox` warning to be "accompanied by strict security warnings in the CLI **and documentation**" — this story delivers the documentation half.
- **Assumptions:** Stories 1-3 land the actual sandbox-routing behavior, the `--no-sandbox` flag, and its CLI warning text; this story documents that behavior and may reference the CLI warning's wording for consistency but does not need Stories 1-3 to be code-complete before drafting begins (the flag name `--no-sandbox`, the default-on posture, and the security rationale are already fixed by the epic and plan.md, so the documentation content can be written and reviewed in parallel, then reconciled against the final CLI warning text before merge). The choice of whether to extend `docs/execution.md` with a new section or create a new `docs/auto-fix.md` cross-linking it is an open design decision left to this story.
- **Constraints:** Documentation-only story — no changes to `internal/sandbox`, `internal/verify`, `internal/autofix`, `internal/registry`, or `cmd/atcr` Go code. Content must be technically accurate against whatever Stories 1-3 actually ship (flag name, default behavior, warning trigger conditions); it must not invent capabilities (e.g., must not claim a config-level `auto_fix.no_sandbox` option exists unless Story 2/3 actually implements one — verify before publishing). Tone and structure should mirror `docs/execution.md`'s existing "Security posture" and sandbox-guarantees sections rather than introducing a new documentation style.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Stories 1-3 (needs their final `--no-sandbox` flag name, default-on behavior, and CLI warning text to confirm documentation accuracy before merge; drafting can proceed in parallel and be reconciled at the end). |

## Success Criteria (SMART Format)

- **Specific:** A `docs/` page (either a new `--auto-fix` section appended to `docs/execution.md` or a new `docs/auto-fix.md` cross-linking it) explains: (1) `--auto-fix` validation is sandboxed by default using the same container isolation as `--exec` (network-isolated, read-only mount, resource-capped, non-root); (2) what `--no-sandbox` does and why it is dangerous (untrusted, potentially LLM-hallucinated or prompt-injected code runs with the `atcr` process's full host privileges); (3) that using `--no-sandbox` prints a warning on every run; (4) concrete guidance on when it might be acceptable (e.g., no Docker available) and what risk the operator is accepting; and documents the `auto_fix:` config block (`apply_target`, `validate_command`, `validate_timeout`) that is currently absent from all `docs/`.
- **Measurable:** All four required content elements above are present and verifiably correct against the final Story 1-3 implementation (flag name matches exactly, warning-on-every-run claim matches the actual CLI behavior, no invented config options); reviewed by re-reading the merged Stories 1-3 code/CLI help text before this story's docs PR is finalized.
- **Achievable:** Confined to writing/editing one or two Markdown files under `docs/`; no code changes, no new tooling, directly modeled on `docs/execution.md`'s existing structure and tone.
- **Relevant:** Directly satisfies the epic's Acceptance Criterion 2 requirement that `--no-sandbox` be "accompanied by strict security warnings in the CLI **and documentation**" — without this story, the documentation half of that AC is unmet even if the code ships correctly.
- **Time-bound:** A single small documentation change, completable within the same sprint cycle as Stories 1-3, with final accuracy reconciliation happening once those stories' flag/warning text is settled.

## Acceptance Criteria Overview

1. Documentation exists (extending `docs/execution.md` or a new `docs/auto-fix.md`) that states the `--auto-fix` validation step is sandboxed by default via the same container isolation guarantees documented for `--exec`, and documents the previously-undocumented `auto_fix:` config block.
2. Documentation explains `--no-sandbox`: what it bypasses, the specific risk (host-privileged execution of untrusted/potentially malicious LLM-generated code), that it prints a warning on every invocation, and concrete guidance on when its use might be acceptable.
3. Content is cross-linked with `docs/execution.md`'s existing sandbox-guarantees and security-posture sections rather than duplicating them, and is verified accurate against the actual flag name and behavior shipped by Stories 1-3 before merge.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/32.0_sandbox_execution_environment/`_

## Technical Considerations

- **Implementation Notes:** Two viable structural approaches, to be decided during `/design-sprint` or drafting: (a) append a new "Auto-fix validation sandboxing" section to `docs/execution.md`, reusing its existing "What the sandbox guarantees" content by reference rather than duplicating it; or (b) create `docs/auto-fix.md` documenting the full `auto_fix:` config surface (`apply_target`, `validate_command`, `validate_timeout`) plus the sandboxing/`--no-sandbox` posture, cross-linking back to `docs/execution.md` for the shared container-isolation guarantees. Given the `auto_fix:` block is entirely undocumented today and independent of `--exec`, option (b) likely gives operators a clearer single reference for `--auto-fix` as a whole, but either satisfies the AC as long as the required content is present and discoverable.
- **Integration Points:** Cross-link from `docs/ci-integration.md` and `docs/agentic-consumption.md` (which already mention `--auto-fix` in passing) to the new/extended doc, so operators following those entry points reach the security-posture content. Reference (do not restate verbatim) `docs/execution.md`'s existing "What the sandbox guarantees" and "Security posture" sections for the shared isolation model.
- **Data Requirements:** None — pure documentation content, no schema or persisted state involved.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Documentation is drafted before Stories 1-3 finalize the exact `--no-sandbox` flag name, default behavior, or CLI warning wording, and ships inaccurate or inconsistent claims (e.g., wrong flag name, wrong warning trigger condition). | Medium | Treat this story's content as a draft until Stories 1-3 merge; do a final accuracy pass against the actual CLI help text and warning strings immediately before this story's docs change is merged. |
| Duplicating `docs/execution.md`'s sandbox-guarantees content instead of cross-linking creates two sources of truth that drift out of sync as the sandbox implementation evolves. | Low | Cross-link to the existing "What the sandbox guarantees" section rather than re-describing the isolation mechanics; only describe what is specific to the auto-fix validation context (e.g., that the mutated working tree, not a read-only review snapshot, is what gets validated). |
| The `--no-sandbox` warning language in docs drifts from the CLI's actual warning text over time (e.g., a future change to the CLI wording isn't mirrored in docs), weakening the "strict warnings in the CLI and documentation" requirement. | Low | Keep the docs statement of intent (what the warning conveys) rather than quoting exact CLI string literals verbatim, so future CLI wording tweaks don't require a docs update to stay accurate. |

---

**Created:** July 19, 2026
**Status:** Draft - Awaiting Acceptance Criteria
