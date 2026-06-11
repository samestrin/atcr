# Sprint 1.0: atcr Core - Review Engine, Reconciler, and Skill

## Plan Metadata
**Created:** 2026-06-10
**Status:** Sprint Active
**Plan Number:** 1.0
**Plan Type:** feature
**Feature Category:** Developer Tools
**Priority:** High
**Assigned Team:** Solo Developer
**Epic/Initiative:** 1.0_atcr_core
**Dependencies:** None
**Stakeholders:** Sam Estrin

## Estimated User Story Count
**Complexity Level:** Complex
**Estimated Stories:** 6

---

## Workflow Status

**Document Status**: [x] Plan Created [x] User Stories Added [x] Acceptance Criteria Added [ ] Ready for Sprint

## Sprint Tracking

**Sprint Reference:** .planning/sprints/active/1.0_atcr_core/
**Sprint Number:** 1.0
**Sprint Created:** June 10, 2026
**Sprint Status:** Active
**Adversarial Review:** ENABLED 🎯
**Inline-Fix Severities:** CRITICAL/HIGH
**Execution Mode:** Gated 🚧

---

**Single Source of Truth:** This metadata.md file will be copied to the sprint folder and become the active tracking document during sprint execution.

---

## Execution Metrics

_Populated by `/execute-sprint` upon completion_

**Executed:** Started 2026-06-10 (Phases 1–3 complete)
**Runtime:** _TBD_
**Status:** In Progress — gated at Phase 3 boundary

### Progress
- **Phases:** 3/5 (Foundation + Core Systems + Engines complete)
- **Work Items:** 122/173 tasks (Phase 1: 30, Phase 2: 48, Phase 3: 42, includes DoD 3.41 + gate 3.42)

### Quality
- **Tests:** all passing (9 test packages, race-clean)
- **Coverage:** 84.4% total (≥70% baseline); reconcile 92%, report 94%, fanout 83%
- **Lint:** Clean (golangci-lint, go vet, gofmt)

### Changes
- **Files Changed (Phase 2):** internal/gitrange (resolver), internal/payload (builder/diff/resolve/budget/manifest/template/scope), internal/stream (parser/writer), internal/llmclient (client), internal/fanout (status), internal/registry (persona + payload-mode validation), cmd/atcr/range.go
- **Files Changed (Phase 3):** internal/fanout (engine/outcome/artifacts/reviewdir/review), internal/reconcile (discover/cluster/dedupe/merge/reconcile/emit/gate), internal/report (render), internal/stream (ParseModelOutput), internal/gitrange (CurrentBranch), cmd/atcr (review/reconcile/report/anchor)
- **Commits:** 10 Phase 3 implementation commits on feature/1.0_atcr_core (30 total)

### Phase 1 Completion Notes (2026-06-10)
- Scaffold: cobra CLI, 6 subcommands, centralized exit codes (0/1/2)
- Config: registry + project loaders (strict YAML), 4-tier precedence, fallback-chain DFS validation
- `atcr init`: embedded defaults → editable .atcr/ workspace (6 personas + _base)
- Adversarial reviews: 7 subagent reviews run; 4 HIGH findings fixed inline; 3 LOW deferred to tech-debt-captured.md (TD-001..003)

### Phase 2 Completion Notes (2026-06-10)
- gitrange: decision tree (explicit/merge_commit/auto), default-branch detection, empty-range + shallow-clone hard errors; `atcr range` prints Resolution JSON
- payload engine: diff/blocks/files builders (function-context + per-file -U10 fallback, binary/deleted/renamed markers); payload-mode enum validation at all config tiers; byte-budget deterministic truncation (smallest-first); manifest + per-agent status records; typed PayloadContext renderer (missingkey=error) + per-mode scope rules
- stream: atcr-findings/v1 parser/writer (severity-prefix regex, pipe/newline escaping, short-row padding, 8-col per-source / 9-col reconciled)
- llmclient: OpenAI-compatible chat client, 429/5xx retry (3 attempts, 500ms×1.5), invoke-time key resolution, no-redirect-follow
- registry: six-level persona resolution chain with path-traversal/dotfile/symlink/reserved-name guards
- Adversarial reviews: 8 unit reviews run; fixed inline — 1 CRITICAL (findings-stream newline split), 4 HIGH (quoted paths, comma-forged reviewers, trailing-pipe drop, dead retry path) + the duplicate-path budget HIGH and CLI enum HIGH; deferred TD-007..015
- DoD: tests pass, vet/lint clean, coverage 83.3%, `atcr range` smoke test verified (auto → JSON exit 0; empty range → exit 1)

### Phase 3 Completion Notes (2026-06-10/11)
- fan-out engine: parallel + serial lane scheduler (WaitGroup, ctx-classified timeouts), fallback chains, partial-success Outcome (sentinel errors), per-agent artifacts (review.md/findings.txt/status.json) + merged pool + summary.json, review-dir scaffold/id-derivation/latest pointer/manifest, full `atcr review` orchestration (LoadReviewConfig→buildSlots→engine→WritePool→manifest) with per-agent timeouts
- reconciler (net-new core): leaf-preference source discovery, single-linkage ±3 clustering, integer-threshold Jaccard dedupe + ambiguous sidecar, merge rules (max severity + disagreement, longest problem/fix, modal category, max est, reviewer-prefixed evidence), confidence (2+ reviewers = HIGH), 5-artifact emit (findings.txt 9-col / findings.json / report.md / summary.json / ambiguous.json), `atcr reconcile --fail-on` + one-shot `atcr review --fail-on` with centralized exit codes
- report: md/json/checklist renderers with HTML-escape + newline-flatten + rune-safe truncation + backtick-safe code spans; `atcr report --format/--output` with enum validation (TD-003 resolved)
- stream: ParseModelOutput (7-col model output, engine-set REVIEWER — TD-016 resolved)
- Adversarial reviews: 10 unit reviews run; fixed inline — 1 CRITICAL (backtick code-span injection), HIGHs (error misclassification, traversal-id, symlink read, markdown newline injection, exit-code consistency, REVIEWER forge); deferred TD-017..021
- DoD: tests race-clean, vet/lint clean, coverage 84.4%, end-to-end smoke verified (2 sources at lines 42/43 → 1 merged CRITICAL/HIGH-confidence; reconcile→report md/checklist; --fail-on exit 0/1/2)
