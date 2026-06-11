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

**Executed:** Started 2026-06-10, completed 2026-06-11 (Phases 1–5 complete)
**Runtime:** _TBD_
**Status:** Ready for Review

### Progress
- **Phases:** 5/5 (Foundation + Core Systems + Engines + Integration + Validation & Docs complete)
- **Work Items:** 173/173 tasks (Phase 1: 30, Phase 2: 48, Phase 3: 42, Phase 4: 34, Phase 5: 7 incl. DoD 5.7) + 12 ACs

### Quality
- **Tests:** all passing (10 test packages + skill, race-clean)
- **Coverage:** 82.4% total (≥70% baseline)
- **Lint:** Clean (golangci-lint 0 issues, go vet clean, gofmt)

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

### Phase 4 Completion Notes (2026-06-11)
- MCP server (internal/mcp): `atcr serve` over StdioTransport (modelcontextprotocol/go-sdk v1.6.1); 5 generic typed tools (atcr_review/reconcile/report/range/status) with schema inference + report format-enum schema; fail-fast registration (duplicate-name guard + panic-recover); thin handlers over the same engine packages as the CLI; stderr discipline (slog→stderr, stdout owned by protocol); InMemoryTransport tests + no-stdout-leak assertion
- Non-blocking review: split fanout.RunReview into PrepareReview (scaffold + manifest + latest) + ExecuteReview (fan-out); atcr_review returns {review_id, review_path, status:"running", agent_count} immediately and runs fan-out in a tracked background goroutine (recover-guarded), drained (bounded 5s) on shutdown via mcp.Serve
- `atcr status` CLI command + fanout.ReadReviewStatus (manifest roster + pool summary.json → in_progress/completed/failed); 7th subcommand (added by AC 05-03 for the Skill polling loop)
- Skill (skill/SKILL.md + go:embed structure test): host (+1) adversarial review writing v1 8-col findings (REVIEWER=host), orchestration loop (range→review→status-poll→host→reconcile→report), untrusted-input clause, ambiguity-adjudication instructions; docs/skill-usage.md install guide
- Adjudication (internal/reconcile/ambiguous.go): content-addressed 128-bit cluster ids, adjudication.json decisions (merge/distinct/skipped) applied on reconcile re-invocation, validated against the preserved ambiguous.original.json baseline (idempotent re-runs), unknown-id + malformed rejection, conservative unmerged default
- Adversarial reviews: 2 holistic subagent reviews (MCP unit, skill+adjudication). Fixed inline — 1 CRITICAL (background-goroutine panic recover), 4 HIGH (shutdown drain, empty-range guard on review, adjudication idempotency, manifest-reader dedup) + cheap MEDIUM/LOW (AtOrAbove unknown-threshold guard, prompt-injection clause, 128-bit ids); deferred TD-023 (status read race), TD-024 (adjudication baseline binding)
- New dependency: github.com/modelcontextprotocol/go-sdk v1.6.1 (go directive bumped 1.24→1.25); go vet/lint clean, coverage 82.4%

### Phase 5 Completion Notes (2026-06-11)
- README rewrite: panel+reconcile architecture, quickstart (`atcr init` → `atcr review && atcr reconcile`), full 7-command table (incl. `status`), key-flag list, payload-mode guidance (diff = most compact/token-friendly, blocks = default), CI section with YAML-valid GitHub Actions snippet referencing examples/ci-gate.sh, exit-code table (0/1/2)
- docs finalized in place (grounded against source, not memory): findings-format.md (atcr-findings/v1 — 8-col/9-col layouts with examples, severity enum, `^(CRITICAL|HIGH|MEDIUM|LOW)\|` extraction regex, pipe→/ + CR/LF→space escaping, header gate, short-row padding/overflow rules, leaf-preference source discovery, additive-only evolution policy); registry.md (provider/agent/project schemas, 4-tier precedence, 6-level persona resolution chain, fallback dangling+cycle validation, reserved-key table); payload-modes.md (mode trade-offs, per-agent override examples, byte-budget truncation with 0=unlimited, files-mode markers `>>> CHANGED LINES`/`[deleted file:]`/`[binary file changed:]`, out-of-scope CATEGORY rule); ci-integration.md exit-code table corrected from TBD to 0/1/2
- examples/ci-gate.sh: thin one-shot wrapper (`atcr review --fail-on`), executable, `bash -n` clean, shellcheck 0 issues (shellcheck installed via Homebrew)
- DoD: all 7 commands + every documented flag cross-checked against `atcr --help`; all README doc links resolve; tests pass, coverage 82.4%, go vet clean, golangci-lint 0 issues
