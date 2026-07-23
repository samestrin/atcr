# Your AI Findings Belong in the Security Tab, Not a Text File

**Status:** Draft
**Target:** Security Engineers, DevSecOps, AppSec Leads
**Publication Phase:** 3 — Enterprise Trust & Security
**Grounded in:** Epic 25.0 (SARIF 2.1.0 output for GitHub Code Scanning + GitLab SAST)

## The Hook

Your security team already has a place they look: GitHub Advanced Security's Code Scanning "Security" tab, or GitLab's native SAST report widget. That's where CodeQL findings, dependency alerts, and secret-scanning results surface, get triaged, and get tracked. If your AI code reviewer's output lives in a separate text file or a PR comment thread, it lives *outside* that workflow — which means, in practice, it doesn't get looked at with the same rigor.

The fix isn't a new dashboard. It's speaking the format the dashboards already read.

## The Technical Challenge

SARIF is a real standard with real teeth — GitHub and GitLab validate what you upload against the SARIF 2.1.0 schema, so "close enough" JSON gets rejected at the door. Every result needs proper severity levels the platform understands, and every finding needs to anchor to a location — including the awkward case of a *file-level* finding that has no single line to point at but still has to render in the Security tab instead of silently vanishing.

## The ATCR Solution

ATCR renders its reconciled findings as a first-class SARIF document (25.0):

- **`atcr report --format=sarif`** emits a schema-conformant SARIF 2.1.0 document — `$schema`, `runs[]`, `tool.driver`, `rules[]`, `results[]` — validated against the SARIF 2.1.0 schema, so it uploads cleanly to GitHub Code Scanning and shows up in GitLab CI's native SAST widget. These are centralized surfaces the existing `atcr github` PR-check and inline-comment integration deliberately doesn't reach.
- **Severity that maps to the platform's model:** CRITICAL/HIGH → `error`, MEDIUM → `warning`, LOW → `note`, reusing ATCR's canonical severity rubric — so an ATCR "HIGH" reads as an error in the Security tab, not an undifferentiated blob.
- **Nothing silently dropped:** findings anchor at the line level, and *file-level* findings get a synthesized fallback region so every result still displays in GitHub's Security tab instead of disappearing for lack of a line number.
- **Parity across surfaces:** the MCP `report` tool accepts `sarif` over the wire too, matching CLI/MCP output, and the format is documented for CI upload to both GitHub and GitLab.

Your reconciled, multi-agent, skeptic-verified findings land in the exact tool your security team already triages from — next to CodeQL, in the same queue.

## Call to Action

Add one CI step: `atcr report --format=sarif` and upload the result to GitHub Code Scanning or GitLab's SAST widget. Your AI review findings stop living in a text file nobody opens and start living in the Security tab your team already works. Meet your reviewers where they already are.

## Next Steps (drafting notes)

- [ ] Add the GitHub Actions upload snippet (`upload-sarif`).
- [ ] Screenshot ATCR findings rendered in the Code Scanning Security tab.
- [ ] Add the GitLab CI SAST-widget integration example.
