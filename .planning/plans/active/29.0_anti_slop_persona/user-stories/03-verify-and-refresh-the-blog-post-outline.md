# User Story 3: Verify and Refresh the Blog Post Outline

**Plan:** [29.0: Anti-Slop Persona (Simon) & Content Marketing](../plan.md)

## User Story

**As a** content marketer publishing ATCR's top-of-funnel "Slopfix" narrative
**I want** the existing `.planning/product/content/blog/slopfix-ai-code-bloat.md` outline reviewed against the shipped `simon` persona and corrected wherever it drifted
**So that** a reader who follows the post's call to action runs a command that actually exists, instead of hitting an invalid CLI error on their first try

## Story Context

- **Background:** The blog outline already exists — authored under Sprint 19.6 (commit `15b938a4`), predating this epic — and already covers the Slopfix $10k/week hook, the `simon` persona pitch, a before/after code-example section, and a call to action. This story is a targeted review/refresh pass, not new authorship. Codebase discovery confirmed one concrete defect: section 5's CTA at approximately line 38 reads `atcr review --persona simon`, which is invalid — `cmd/atcr/review.go` has no `--persona` flag. The real, verified flow is `atcr personas install simon` (production use, mirroring the documented pattern `atcr personas install delia` in `docs/personas-install.md`) and the zero-setup, no-LLM fixture demo `atcr personas test simon` (mirroring `atcr personas test delia`).
- **Assumptions:** Stories 1 and 2 are complete before this story starts — the final category word (planned as `bloat` per `plan.md` and Story 1's Technical Considerations) and the persona's actual `## Focus` framing are locked in `personas/community/simon.yaml`/`simon.md`, and the fixture/roster/index registration from Story 2 is green under `go test ./personas/...`. This story reads those shipped artifacts as its source of truth rather than re-deriving them.
- **Constraints:** Only `.planning/product/content/blog/slopfix-ai-code-bloat.md` is modified — no new blog files, no changes to `personas/community/` or test files. Edits are corrective and minimal: fix the invalid CTA command and reconcile any drifted category word or framing language; do not rewrite sections that are already accurate, and do not expand the outline's scope beyond what Stories 1–2 shipped.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | User Story 1 (Author the `simon` Persona Unit), User Story 2 (Fixture Authoring & Test-Gate Integration) |

## Success Criteria (SMART Format)

- **Specific:** `.planning/product/content/blog/slopfix-ai-code-bloat.md` section 5 ("The Call to Action") no longer contains the invalid `atcr review --persona simon` invocation and instead cites a real, verifiable command for both production install and zero-setup demo.
- **Measurable:** A single grep for `--persona simon` (or `review --persona`) across the outline file returns zero matches after the edit; a grep for `atcr personas install simon` and/or `atcr personas test simon` returns at least one match; the category word used anywhere in the outline's framing (e.g. section 1's pain-point list, section 3's persona pitch) matches the exact word shipped in `simon.md`'s `## Focus` section.
- **Achievable:** The fix is a scoped edit to one existing Markdown file — no new research, no new narrative structure, bounded to the sections identified by codebase discovery.
- **Relevant:** Closes the loop between the shipped persona (Stories 1–2) and the marketing asset that pitches it — an invalid CTA in a top-of-funnel post directly undermines the epic's growth objective by breaking the reader's first hands-on interaction with ATCR.
- **Time-bound:** Completed as the final story of this plan, after Stories 1 and 2 land, within the same sprint.

## Acceptance Criteria Overview

1. The invalid `atcr review --persona simon` CTA is replaced with the verified `atcr personas install simon` (and/or `atcr personas test simon` for the no-setup demo path), matching the command surface in `cmd/atcr/personas.go` and the documented usage pattern in `docs/personas-install.md`.
2. Every category-word reference in the outline (e.g. "bloat"-flavored language in sections 1, 3, and 4) is checked against `simon.md`'s shipped `## Focus` section and updated if it drifted from the word Story 1 actually committed.
3. All other sections of the outline that already accurately reflect the shipped persona (the Slopfix hook, the cost narrative, the before/after code-example framing) are left unchanged — the diff is confined to the corrected CTA and any confirmed word-level drift, with no wholesale rewrite.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/29.0_anti_slop_persona/`_

## Technical Considerations

- **Implementation Notes:** Open `.planning/product/content/blog/slopfix-ai-code-bloat.md` and diff its claims against the final state of `personas/community/simon.yaml`/`simon.md` (Story 1) and the registered fixture/roster entries (Story 2). Replace the CTA line (approximately line 38, "Install ATCR today and run `atcr review --persona simon` on your next PR.") with wording that cites `atcr personas install simon` for adopting the persona and, optionally, `atcr personas test simon` as the zero-setup, no-API-key way to see it flag the fixture's planted slop before committing to a full CI run. Re-read sections 1, 3, and 4 for any category-word or persona-behavior phrasing that no longer matches the shipped `## Focus` section verbatim.
- **Integration Points:** None beyond the single Markdown file — this story does not touch `personas/community/`, `cmd/atcr/`, or any test file. It is a read-only verification pass against those artifacts followed by a text-only correction.
- **Data Requirements:** None. No new data, schema, or registry entry is introduced; the only output is the corrected prose in the existing outline file.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Story 3 starts before Stories 1–2 have locked the final category word or CLI surface, causing the "fix" to reference a value that later changes again | Medium | Sequence this story strictly after Stories 1 and 2 are marked complete and their persona/fixture artifacts are green under `go test ./personas/...`, per the plan's declared dependency order. |
| Scope creep into a full outline rewrite, duplicating the authorship already done under Sprint 19.6 | Medium | Constrain the diff to the CTA command fix and any confirmed word-level drift only; treat the existing hook, cost-narrative, and code-example sections as already correct unless a specific discrepancy is found. |
| The chosen replacement command (`atcr personas install simon`) reads correctly today but drifts if the `personas` CLI surface changes again later, reintroducing an invalid CTA | Low | Cite the command in the same form already used and verified in `docs/personas-install.md`'s worked examples (`atcr personas install delia`, `atcr personas test delia`), so the outline stays consistent with the project's own canonical documentation rather than an independently invented invocation. |

---

**Created:** July 16, 2026 09:15:34PM
**Status:** Draft - Awaiting Acceptance Criteria
