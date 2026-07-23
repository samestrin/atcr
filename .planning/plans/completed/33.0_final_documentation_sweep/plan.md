# Plan 33.0: Final Code Review + Documentation Sweep

## Metadata
- **Plan Type:** tech-debt
- **Created:** 2026-07-22
- **Last Modified:** 2026-07-22
- **Plan Goal:** Run a comprehensive code review (atcr's own multi-agent reviewer plus a human adversarial pass) over the production codebase and fix CRITICAL/HIGH findings, then audit all user-facing documentation against the finalized code — closing both risks before Epic 33.2 makes the repository and its history public.
- **Target Users:** Sam Estrin (maintainer, sole reviewer/approver); indirectly, future public users and contributors who will rely on `atcr.dev` and the repo's docs as source of truth once launched.
- **Framework/Technology:** Go (module `atcr`), MCP server framework; documentation in Markdown under `docs/` and `README.md`; internal tooling under `.planning/technical-debt/`.

## Objectives
1. **Production Code Review & Security Hardening:** Execute `atcr`'s multi-agent code reviewer and a manual adversarial security pass across `cmd/`, `internal/`, `reconcile/`, and `skill/` to catch latent bugs, security flaws, secrets, dead code, and TODO/FIXME debt before public release.
2. **Findings Triage & Technical Debt Routing:** Fix all CRITICAL and HIGH severity findings directly in the production codebase; capture all MEDIUM and LOW severity findings into `.planning/technical-debt/README.md`.
3. **Legacy Persona Cleanup:** Audit all codebase documentation, inline CLI help texts, and schemas to ensure legacy persona names (`sentinel`, `tracer`, `idiomatic`) are eliminated in favor of generalized multi-language personas (`sasha`, `penny`, `ingrid`/`ian`).
4. **Code-to-Docs Accuracy Sweep:** Audit `README.md`, `docs/`, inline command help screens, and schemas against finalized production code to ensure full coverage of features up to Epic 23.0.
5. **Website Export Compatibility:** Validate all markdown files in `docs/` for clean formatting, self-containment, and readiness for import into the `atcr.dev` website repository.

## Success Criteria
- [ ] AC1: Comprehensive code review (atcr multi-agent reviewer + adversarial pass) executed over `cmd/`, `internal/`, `reconcile/`, `skill/`; all CRITICAL/HIGH findings fixed and MEDIUM/LOW routed to `.planning/technical-debt/README.md`.
- [ ] AC2: Codebase and git history verified free of secrets, credentials, or embarrassing artifacts before public release.
- [ ] AC3: No legacy persona names (`sentinel`, `tracer`, `idiomatic`) remain in documentation or command help screens.
- [ ] AC4: All features up to Epic 23.0 accurately documented across `README.md`, `docs/`, inline help text, and schemas.
- [ ] AC5: All `docs/` markdown files validated as cleanly formatted and self-contained for import into `atcr.dev`.

## Planning Deliverables
### Tasks
- **Location:** [`tasks/`](tasks/*.md)
- **Status:** Generated
- **Estimated Count:** 8 tasks

## Tasks
1. [Task 01: Multi-Agent Code Review — Dogfood atcr Against Its Own Production Codebase](tasks/task-01-multi-agent-code-review.md)
2. [Task 02: Adversarial/Security Pass — Manual Review for Secrets, Dead Code, Unsafe Patterns, TODO/FIXME](tasks/task-02-adversarial-security-pass.md)
3. [Task 03: Findings Triage — Classify, Fix CRITICAL/HIGH, Route MEDIUM/LOW to Technical Debt](tasks/task-03-findings-triage.md)
4. [Task 04: Code-to-Docs Accuracy Audit](tasks/task-04-code-to-docs-audit.md)
5. [Task 05: Persona Reference Verification — Confirm sasha/penny/ingrid Consistency, No Legacy Slugs Remain](tasks/task-05-persona-reference-verification.md)
6. [Task 06: Website Compatibility Check — Validate `docs/` for Clean, Self-Contained `atcr.dev` Import](tasks/task-06-website-compatibility-check.md)
7. [Task 07: Technical Debt Capture — Shard MEDIUM/LOW Findings into `.planning/technical-debt/README.md`](tasks/task-07-technical-debt-capture.md)
8. [Task 08: Final Verification Pass — Re-run Automated Guards End-to-End (Plan Definition-of-Done Gate)](tasks/task-08-final-verification-pass.md)

## Feature Analysis Summary
This plan closes out the launch-readiness cluster's first gate: nothing in Epic 33.1 (launch content) or Epic 33.2 (go-public + atcr.dev) should proceed against unreviewed code or stale docs. The work splits into two ordered phases per the original request: (1) a comprehensive code review — dogfooding `atcr`'s own multi-agent reviewer against its own codebase, plus a manual adversarial pass focused on security, secrets, dead code, and TODO/FIXME debt — with CRITICAL/HIGH findings fixed before launch and MEDIUM/LOW routed to `.planning/technical-debt/README.md`; and (2) a documentation sweep confirming `README.md`, `docs/`, inline CLI help text, and schemas are accurate against the finalized code, free of legacy persona names (`sentinel`, `tracer`, `idiomatic`), and clean enough to serve as the `atcr.dev` website's source of truth. A preliminary scan (see `codebase-discovery.json`) found the persona rename already complete and automated-test-guarded (`personas/retired_slugs_test.go`), and no obvious TODO/FIXME debt or hardcoded secrets in non-test Go files — so the docs sweep is likely to dominate the effort, but the code review must still run to close AC1/AC2 with evidence, not assumption.

## Technical Planning Notes
- The retired-slug guard test (`personas/retired_slugs_test.go`) already enforces AC3 for the active persona set; work here should extend/rely on it rather than re-implement a manual grep, and should additionally hand-check prose docs (README.md, docs/, skill/SKILL.md) for mentions the test's persona-identifier-scoped regex cannot catch.
- Distinguish legitimate Go/technical usage of the words "sentinel" and "tracer" (sentinel error values, sentinel-delimited payload lines per `docs/payload-modes.md` and `docs/cross-examination.md`) from actual stale persona-slug references — false-positiving on the former would waste review time and risk incorrect edits to working code.
- Follow `.planning/specifications/coding-standards.md` (Go naming, error wrapping, `golangci-lint`/`go vet` gates) and `.planning/specifications/implementation-standards.md` (black-box modules, replaceable components) when fixing any CRITICAL/HIGH code-review findings.
- Route MEDIUM/LOW findings to `.planning/technical-debt/README.md` using the project's existing sharded TD format rather than fixing everything inline — this plan's scope is CRITICAL/HIGH only for code fixes.
- `docs/` currently holds 29 markdown files; verify each is self-contained and cleanly formatted (no repo-relative assumptions that break when imported into the separate `atcr.dev` repo) for AC5.

## Implementation Strategy
1. Run `atcr`'s multi-agent reviewer against its own production codebase (dogfooding), scoped to `cmd/`, `internal/`, `reconcile/`, `skill/`.
2. Run a manual adversarial pass emphasizing security, secrets/credentials, dead code, and TODO/FIXME debt (a shallow pre-scan found none of the latter in non-test files, but the full pass must confirm this rather than rely on the pre-scan).
3. Triage all findings by severity; fix CRITICAL/HIGH in the codebase; capture MEDIUM/LOW as technical debt via the existing TD workflow.
4. Once code is finalized, audit `README.md`, `docs/`, inline CLI help text, and schemas against it for accuracy.
5. Verify persona name references (`sasha`, `penny`, `ingrid`/`ian`) are used consistently and legacy names are absent from all documentation and command help screens.
6. Validate `docs/` markdown files are formatted cleanly and self-contained, ready for import into the `atcr.dev` website repository.

## Documentation References
- **[CRITICAL]** [documentation/multi-agent-review-workflow.md](documentation/multi-agent-review-workflow.md) — atcr's dispatcher orchestration (review/reconcile/verify/report) for dogfooding the multi-agent reviewer.
- **[CRITICAL]** [documentation/technical-debt-triage-resolution.md](documentation/technical-debt-triage-resolution.md) — the RED→GREEN→ADVERSARIAL→REFACTOR severity-triage cycle for the findings-triage and TD-capture tasks.
- **[IMPORTANT]** [documentation/persona-naming-doc-accuracy.md](documentation/persona-naming-doc-accuracy.md) — retired-slug guard test and legitimate-vs-stale "sentinel"/"idiomatic" usage grounding for the persona cleanup and code-to-docs audit tasks.

## Recommended Packages
No high-ROI packages identified — this plan is a review and documentation audit, not new functionality; it uses existing tooling (`golangci-lint`, `go vet`, `atcr`'s own reviewer) already present in the repo.

## Task Themes
| # | Theme | Summary |
|---|-------|---------|
| 1 | Multi-agent code review | Run atcr's own reviewer over `cmd/`, `internal/`, `reconcile/`, `skill/` |
| 2 | Adversarial/security pass | Manual review for secrets, dead code, unsafe patterns, TODO/FIXME |
| 3 | Findings triage | Classify all findings by severity; fix CRITICAL/HIGH, route MEDIUM/LOW to TD |
| 4 | Code-to-docs audit | Audit `README.md`, `docs/`, inline CLI help, schemas against finalized code |
| 5 | Persona reference verification | Confirm `sasha`/`penny`/`ingrid`/`ian` used consistently; no legacy names remain |
| 6 | Website compatibility check | Verify `docs/` markdown is clean and self-contained for `atcr.dev` import |
| 7 | Technical debt capture | Write MEDIUM/LOW findings into `.planning/technical-debt/README.md` |
| 8 | Final verification pass | Re-run automated guards (`retired_slugs_test.go`, lint/vet/tests) end-to-end |

## Risk Mitigation
- **Risk:** The "comprehensive code review" scope is open-ended and could balloon past the estimated 2-3 days. **Mitigation:** Scope the multi-agent review run to the components named in the original request (`cmd/`, `internal/`, `reconcile/`, `skill/`) and enforce the CRITICAL/HIGH-only fix bar; everything else is TD, not blocking.
- **Risk:** False positives when grepping for legacy persona names (e.g., "sentinel error", "idiomatic Go") could lead to wasted effort or incorrect edits. **Mitigation:** Use the existing identifier-scoped guard (`personas/retired_slugs_test.go`) as the authoritative check, and manually confirm any prose match is a genuine persona-name reference before editing.

## Next Steps
1. `/find-documentation @.planning/plans/active/33.0_final_documentation_sweep/`
2. `/create-documentation @.planning/plans/active/33.0_final_documentation_sweep/`
3. `/create-tasks @.planning/plans/active/33.0_final_documentation_sweep/`
4. `/design-sprint @.planning/plans/active/33.0_final_documentation_sweep/`
5. `/create-sprint @.planning/plans/active/33.0_final_documentation_sweep/`
