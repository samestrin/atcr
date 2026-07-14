# User Story 4: SARIF CI Integration Documentation

**Plan:** [25.0: SARIF Output Integration](../plan.md)

## User Story

**As a** DevOps/security engineer wiring ATCR into a GitHub or GitLab pipeline
**I want** a copy-pasteable CI example showing how to pipe `atcr review && atcr reconcile && atcr report --format=sarif` into GitHub Advanced Security's Code Scanning upload and into GitLab CI's native SAST report widget
**So that** ATCR findings reach the centralized, org-wide security surfaces (GitHub's Security tab, GitLab's SAST widget) without me having to reverse-engineer the flag or guess at the correct SARIF upload wiring

## Story Context

- **Background:** `docs/ci-integration.md` already documents the `atcr review && atcr reconcile` two-step pattern and includes a "Maintained PR Action" subsection that pairs a fenced YAML snippet with a link to a deeper doc (`docs/github-action.md`) — that is the structural template this story's new subsection must mirror. `docs/github-action.md` documents the already-shipped, out-of-scope `atcr github` direct-API PR check + inline "Files Changed" comment flow (`cmd/atcr/github.go`). This story adds documentation only, covering the separate SARIF-based path to GitHub Advanced Security's Code Scanning "Security" tab and GitLab CI's native SAST report widget — surfaces `atcr github` does not reach.
- **Assumptions:** Stories 1-3 have already landed `atcr report --format=sarif` as a working, correctly severity-mapped, correctly line/region-anchored CLI flag by the time this story is authored — this story treats the flag as a given and documents its usage, it does not implement or test the formatter. `docs/ci-integration.md` is the correct home for the new subsection (per plan.md's explicit file target), consistent with where the existing "Maintained PR Action" example lives.
- **Constraints:** No code changes, no new CLI wiring, no new runtime dependency — this is a pure documentation deliverable. Must explicitly and clearly distinguish the new SARIF-upload path from `atcr github`'s already-shipped direct-API flow so the two are never conflated by a reader skimming the doc. Must not duplicate or contradict `docs/github-action.md` content — link to it instead of restating it. GitHub's SARIF constraints (size/count ceilings, PR-check line-matching behavior) belong to GitHub's *native* SARIF ingestion, not to `atcr github`, and must be attributed correctly if mentioned.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (SARIF Formatter Core — `--format=sarif` flag must exist); benefits from Stories 2-3 (severity mapping, line/region anchoring) being complete so the documented example reflects final, correct behavior |

## Success Criteria (SMART Format)

- **Specific:** `docs/ci-integration.md` gains a new subsection (e.g. "SARIF Upload for Code Scanning") containing a fenced GitHub Actions YAML snippet piping `atcr review && atcr reconcile && atcr report --format=sarif > results.sarif` into `github/codeql-action/upload-sarif@v3`, and a second fenced `.gitlab-ci.yml` snippet showing the equivalent GitLab CI job producing `results.sarif` and wiring it as a SAST report artifact.
- **Measurable:** Both YAML snippets are syntactically valid (parseable as YAML) and reviewable in a single PR diff to `docs/ci-integration.md`; the subsection contains at least one explicit sentence distinguishing this SARIF path from `atcr github`'s PR check/inline-comment flow with a link to `docs/github-action.md`.
- **Achievable:** Mirrors the existing "Maintained PR Action" subsection's structure (fenced snippet + doc link) already present in `docs/ci-integration.md` — no new documentation pattern to invent.
- **Relevant:** Directly satisfies the plan's Acceptance Criterion "A documentation example exists showing the CI integration (GitHub Code Scanning upload, and the GitLab CI SAST-widget equivalent)" and Theme 4 ("CI integration documentation"), making the SARIF format Stories 1-3 built actually discoverable and usable by the target audience.
- **Time-bound:** Deliverable in a single documentation-only pass once Story 1's `--format=sarif` flag exists; no sprint-phase dependency beyond that.

## Acceptance Criteria Overview

1. `docs/ci-integration.md` contains a new subsection with a fenced GitHub Actions YAML snippet demonstrating `atcr review && atcr reconcile && atcr report --format=sarif > results.sarif` piped into `github/codeql-action/upload-sarif@v3`.
2. The same subsection (or an adjacent one) contains a fenced GitLab CI YAML snippet showing a `.gitlab-ci.yml` job that produces `results.sarif` and publishes it via GitLab's native SAST report artifact mechanism.
3. The new content explicitly states that this SARIF-upload path is distinct from, and does not replace, `atcr github`'s already-shipped direct-API PR check + inline comment flow, with a link to `docs/github-action.md` for that separate flow.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/25.0_sarif_output_integration/`_

## Technical Considerations

- **Implementation Notes:** Add the new subsection to `docs/ci-integration.md` directly beneath (or alongside) the existing "Maintained PR Action" subsection, following its exact structure: a short lead-in sentence, a fenced YAML code block, and a link to further detail where relevant. Use the GitHub Actions snippet already validated in `documentation/github-code-scanning-integration.md`'s "Code Examples" section verbatim as the GitHub half. For the GitLab half, show a `.gitlab-ci.yml` job stage running the same `atcr review && atcr reconcile && atcr report --format=sarif > results.sarif` command and wiring `results.sarif` under `artifacts: reports: sast:` (GitLab's native SAST report ingestion path) per GitLab's SARIF-compatible report artifact support.
- **Integration Points:** `docs/ci-integration.md` (primary edit target, per plan.md's explicit file reference); cross-link to `docs/github-action.md` (read-only reference, no edits) to preserve the existing/new-path distinction; optionally reference `documentation/github-code-scanning-integration.md`'s Quick Reference table (size/count ceilings) as a brief caveat, correctly attributed to GitHub's native SARIF ingestion rather than to `atcr github`.
- **Data Requirements:** None — no schema, no new artifact format. The documentation describes an existing CLI invocation (`atcr report --format=sarif`) and two third-party CI ingestion mechanisms (GitHub `codeql-action/upload-sarif`, GitLab `artifacts:reports:sast`), neither of which ATCR calls directly.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| New SARIF subsection reads as a replacement for or duplicate of `atcr github`'s PR check/inline-comment flow, confusing readers about which integration to use | Medium | Include an explicit, unambiguous sentence stating the two paths are separate and serve different surfaces (Security tab/SAST widget vs. PR checks/comments), with a direct link to `docs/github-action.md`; place this near the top of the new subsection, not buried at the end. |
| GitLab CI SAST-widget wiring example is inaccurate or drifts from GitLab's actual native SARIF/SAST report ingestion mechanism, since ATCR has no existing GitLab integration to model it on | Medium | Base the `.gitlab-ci.yml` snippet strictly on GitLab's documented `artifacts:reports:sast` (or equivalent SARIF-compatible report) mechanism as grounded in the plan's technical-grounding notes; keep the snippet minimal (job, script, artifact wiring) rather than speculating on advanced GitLab features not verified against GitLab's docs. |
| Documentation is written before Stories 2-3 finalize severity mapping / line-anchoring, causing the example to reference behavior that later changes | Low | This story only demonstrates the CLI invocation and CI wiring, not the internal SARIF field mapping — the example remains valid regardless of Story 2/3 implementation details, since the command line (`atcr report --format=sarif`) does not change. |

---

**Created:** July 14, 2026 04:11:53PM
**Status:** Draft - Awaiting Acceptance Criteria
