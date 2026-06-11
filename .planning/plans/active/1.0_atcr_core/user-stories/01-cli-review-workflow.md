# User Story 1: CLI Review Workflow

**Plan:** [1.0: atcr Core - Review Engine, Reconciler, and Skill](../plan.md)

## User Story

**As a** developer performing code reviews
**I want** to run a multi-agent review with zero arguments on a feature branch
**So that** I get comprehensive, confidence-scored feedback without manual orchestration

## Story Context

- **Background:** Developers need a simple way to fan out code changes to multiple LLM reviewers and get a unified, deduplicated report. The CLI should handle range resolution, agent fan-out, and reconciliation automatically.
- **Assumptions:** User is on a feature branch with uncommitted or recently committed changes. Providers are configured in ~/.config/atcr/registry.yaml. Project has .atcr/config.yaml with roster.
- **Constraints:** Must work with zero arguments on a feature branch. Range resolution must detect default branch automatically.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Agent Configuration (US-02), Range Resolution, Fan-out Engine |

## Success Criteria (SMART Format)

- **Specific:** Developer can run `atcr review && atcr reconcile` on a feature branch with zero arguments and receive a reconciled report
- **Measurable:** End-to-end workflow completes successfully with ≥1 agent finding captured and reconciled
- **Achievable:** Uses existing Cobra CLI framework, gitrange package, fan-out engine, and reconciler
- **Relevant:** Core value proposition — panel review with deterministic reconciliation
- **Time-bound:** Implemented in tasks 1, 3, 7, 9 (scaffold, gitrange, fan-out, reconciler)

## Acceptance Criteria Overview

1. `atcr review` resolves git range using decision tree (explicit → merge-commit → auto)
2. Review directory created at .atcr/reviews/YYYY-MM-DD_branch-slug/ with manifest.json
3. Fan-out engine invokes configured agents in parallel/serial lanes
4. Per-agent artifacts written to sources/pool/raw/agent/{review.md, findings.txt, status.json}
5. Merged findings written to sources/pool/findings.txt
6. `atcr reconcile` discovers sources, clusters, dedupes, merges, computes confidence
7. Reconciled artifacts written: findings.txt, findings.json, report.md, summary.json
8. `atcr report` renders human-readable report from reconciled data
9. .atcr/latest pointer updated with review ID
10. Empty range produces hard error before any provider call

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/1.0_atcr_core/`_

## Technical Considerations

- **Implementation Notes:** 
  - Range resolution: internal/gitrange/resolver.go — decision tree with default-branch detection (origin/HEAD → origin/main → origin/master → local main → local master)
  - Fan-out: internal/fanout/engine.go — parallel lane (sync.WaitGroup) + serial lane (rate_limited agents), fallback chain, global timeout context
  - Reconciler: internal/reconcile/merger.go — discover → normalize → cluster (FILE, LINE±3) → dedupe (token-set similarity) → merge → confidence → emit
  - Report: internal/report/renderer.go — md, json, checklist formats
  - Review directory: .atcr/reviews/<id>/{manifest.json, payload/, sources/{pool,host}/, reconciled/}
  - Latest pointer: .atcr/latest contains most recent review ID

- **Integration Points:** 
  - Git (os/exec): git rev-parse, git symbolic-ref, git merge-base, git diff
  - OpenAI-compatible LLM APIs: POST ${base_url}/chat/completions
  - Filesystem: review directory structure, config files

- **Data Requirements:** 
  - manifest.json: base/head SHAs, detection_mode, payload_mode(s), roster, timestamps, partial flag
  - findings.txt: pipe-delimited, 8 columns per-source (SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER)
  - reconciled findings.txt: 9 columns (adds CONFIDENCE)

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Empty range (0 commits or base==head) silently passes | High | Hard error in gitrange resolver before any provider call |
| Agent failure causes entire review to fail | Medium | Partial-success semantics: error only if ALL agents fail; partial:true recorded |
| Default branch detection fails on unusual repo setups | Medium | Fallback chain: origin/HEAD → origin/main → origin/master → local main → local master; clear error if all fail |
| Shallow clone causes incomplete diff | Medium | Detect shallow repo, emit guidance to run git fetch --unshallow |
| Review directory already exists | Low | Use --id flag to override; or append timestamp to avoid collision |

---

**Created:** June 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
