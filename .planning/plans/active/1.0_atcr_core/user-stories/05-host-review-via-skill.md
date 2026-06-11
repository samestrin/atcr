# User Story 5: Host Review via Skill

**Plan:** [1.0: atcr Core - Review Engine, Reconciler, and Skill](../plan.md)

## User Story

**As a** developer using an AI agent (e.g., Claude Code)
**I want** the agent to contribute its own review as a +1 reviewer
**So that** I get 2+ sources for confidence scoring even with a single API key

## Story Context

- **Background:** The atcr Skill runs the host-model review (the AI agent's own review) and writes findings in the same v1 format as other reviewers. This ensures that even a user with a single API key gets 2+ sources (host + pool agents), enabling the confidence signal (HIGH = 2+ reviewers agree).
- **Assumptions:** User has Claude Code or similar AI agent with skill support. Agent has access to git and atcr binary. Agent can read the payload and generate findings in v1 format.
- **Constraints:** Skill must write findings to sources/host/{review.md, findings.txt} in v1 format. Skill must orchestrate the full loop: range → review → host review → reconcile → report. No sprint knowledge — input is git range/branch/PR.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | CLI Review Workflow (US-01), Findings Format |

## Success Criteria (SMART Format)

- **Specific:** Skill runs host-model review, writes sources/host/findings.txt in v1 format, orchestrates full loop (range → review → reconcile → report)
- **Measurable:** sources/host/findings.txt contains ≥1 finding in valid v1 format; reconciled report includes host as a reviewer
- **Achievable:** Skill instructions in SKILL.md guide the agent; findings format is documented in docs/findings-format.md
- **Relevant:** Enables confidence signal with minimal setup (single API key + host review)
- **Time-bound:** Implemented in task 12 (Skill)

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-skill-structure-and-installation.md) | Skill Structure and Installation | Unit + Integration |
| [05-02](../acceptance-criteria/05-02-host-review-findings-generation.md) | Host Review Findings Generation | Unit + Integration |
| [05-03](../acceptance-criteria/05-03-orchestration-loop.md) | Orchestration Loop | Integration |
| [05-04](../acceptance-criteria/05-04-adversarial-review-and-adjudication.md) | Adversarial Review and Ambiguity Adjudication | Unit + Integration |

## Original Criteria Overview

1. Skill installed in .claude/skills/atcr/ (or equivalent)
2. Skill runs host-model review: reads payload, generates findings in v1 format
3. Host findings written to sources/host/{review.md, findings.txt}
4. Host findings use same format as pool agents (8 columns: SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER)
5. REVIEWER column set to "host" for host findings
6. Skill orchestrates: `atcr range` → `atcr review` (background, polled) → host review → `atcr reconcile` → present report.md
7. Skill optionally adjudicates ambiguous.json clusters and re-invokes merge
8. Skill accepts git range/branch/PR as input (no sprint knowledge)
9. Skill outputs review directory path
10. Host review is adversarial (finds problems, not praise)

## Technical Considerations

- **Implementation Notes:** 
  - Skill file: skill/SKILL.md — instructions for the AI agent
  - Host review: agent reads payload (from .atcr/reviews/<id>/payload/), generates findings
  - Findings format: pipe-delimited, 8 columns, version header (# atcr-findings/v1)
  - Orchestration: skill calls atcr binary via shell commands or MCP tools
  - Background polling: `atcr review` can run in background; skill polls `atcr status` or waits for completion
  - Ambiguity adjudication: skill reads ambiguous.json, uses LLM to judge clusters, re-invokes reconcile

- **Integration Points:** 
  - atcr binary: range, review, reconcile, report, status commands
  - Filesystem: review directory, sources/host/, payload/
  - LLM: host model generates findings (the agent itself)

- **Data Requirements:** 
  - sources/host/findings.txt: v1 format, 8 columns
  - sources/host/review.md: human-readable review (optional, for context)
  - ambiguous.json: clusters needing semantic adjudication (optional input to skill)

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Host review format doesn't match v1 spec | High | Skill instructions include example row; validate format before writing |
| Host review is too verbose or not adversarial | Medium | Skill prompt includes adversarial personality clause (find problems, not praise) |
| Orchestration loop hangs (review never completes) | Medium | Timeout in skill; poll atcr status with max retries |
| Ambiguous cluster adjudication introduces bias | Low | Adjudication is optional; conservative default (unmerged) if skill doesn't adjudicate |
| Skill not installed correctly | Low | Document installation in README; provide install script |

---

**Created:** June 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
