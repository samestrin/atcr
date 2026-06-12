## Plan Overview
**Plan Type:** feature
**Last Modified:** 2026-06-10
**Plan Goal:** Build v1 of atcr (Agent Team Code Review): a standalone Go tool that fans code changes to multiple LLM reviewer personas and deterministically reconciles their findings into deduplicated, confidence-scored deliverables. Includes CLI, MCP server, and Agent Skill.
**Target Users:** Developers running code reviews via CLI, CI pipeline integrators, MCP clients (IDEs/agents), Agent Skill users
**Framework/Technology:** Go 1.24+, cobra CLI, OpenAI-compatible LLM APIs, MCP protocol

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 6 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Generated - 24 ACs across 6 user stories (4 ACs/story on average; US-01 has 6, US-03 has 2)
- **Coverage:** All 24 ACs trace to one or more requirements in `original-requirements.md`; refinement pass applied 2026-06-10 to close 6 partial-coverage gaps (see `/refine-design` report).

## Feature Analysis Summary

atcr addresses the gap in existing LLM code review tools: none offer a local-first, BYO-keys panel of diverse models with cross-model agreement scoring. The tool consists of three components: (1) a Go binary providing CLI and MCP server over a shared engine, (2) six embedded reviewer personas (bruce, greta, kai, mira, dax, otto), and (3) an Agent Skill that contributes the host-model review and orchestrates the workflow.

The core innovation is the deterministic reconciler: clustering by file/line, text-similarity dedupe, confidence computation based on reviewer agreement, and ambiguous.json sidecar for clusters needing semantic adjudication. This replaces the prompt-based merge in the source system with mechanical, testable Go code.

Three payload modes (diff, blocks, files) accommodate different model capabilities—small MoE models perform better with expanded code blocks than unified diffs. Per-agent payload overrides and byte budgets with recorded truncation provide fine-grained control.

## Technical Planning Notes

- **Architecture:** Single Go binary with internal packages: gitrange, payload, registry, llmclient, fanout, stream, reconcile, report, mcp. CLI (cobra) and MCP server share one engine—no logic forks.
- **Configuration:** Two-tier: user-level ~/.config/atcr/registry.yaml (providers, agents), project-level .atcr/config.yaml (roster, defaults). Precedence: CLI flag > project config > registry > embedded default.
- **Persona/Agent decoupling:** `persona` is a first-class registry concept (named prompt: lens, personality, severity rubric) separate from `agent` (a provider+model binding). Agents reference a persona (`persona: bruce`); fallback agents reference the same persona — never duplicated prompt text — so a fallback is by construction the same lens on a different model. Persona resolution order: agent's `persona:` ref > `<agent>.md` in registry dir > `_base.md` > embedded default (`--task-message` CLI flag overrides all).
- **Range Resolution:** Decision tree: explicit --base/--head → --merge-commit SHA → auto (merge-base against detected default branch). Empty range is a hard error before any provider call.
- **Default Review ID:** `<YYYY-MM-DD>_<branch-slug>` produced by `atcr review` (e.g., `2026-06-10_refactor-reconciler`). `--id` flag overrides; the chosen id is also written to `.atcr/latest` so subsequent `atcr reconcile` / `atcr report` calls default to it without arguments. *(Proposed during requirements; confirm in task 1.)*
- **Review Manifest Schema:** `.atcr/reviews/<id>/manifest.json` records the run provenance: `base`/`head` resolved SHAs, `detection_mode` (`explicit` | `merge_commit` | `auto`), default and per-agent `payload_mode(s)`, `roster` (agent names with fallback chains), `started_at`/`completed_at` timestamps, and `partial` flag. Every downstream tool reads from this file, not from CLI flags — enables re-runs and post-hoc inspection.
- **LLM Client Contract:** OpenAI-compatible POST `${base_url}/chat/completions` with `Authorization: Bearer ${api_key_env}`. Retry policy: 429/500/502/503/504 plus transport-level errors (connection reset, EOF, DNS failure), ~500ms initial delay, 1.5× backoff; context cancellation/deadline returns immediately. Other 4xx fail immediately. *(Transport-error retry added 2026-06-11 via /resolve-td — see sprint-plan.md TD Resolution Clarifications.)* API keys resolved from environment variables at invoke time, not load time. Per-agent `temperature` and `timeout_secs`. The retry budget must not exhaust the per-agent or global timeout.
- **Fan-out:** Parallel lane (default) + serial lane (rate-limited providers). Fallback agents with fallback_used/fallback_from tracking. Partial-success: error only if all agents fail.
- **Reconciler:** Go pipeline: discover → normalize → cluster (FILE, LINE±3) → dedupe (token-set similarity) → merge (REVIEWERS joined, SEVERITY max, PROBLEM/FIX longest, CATEGORY modal) → confidence (HIGH=2+ reviewers, MEDIUM=single) → emit findings.txt/json, report.md, ambiguous.json, summary.json.
- **Findings Format:** Versioned (atcr-findings/v1), pipe-delimited, 8 columns (per-source) or 9 columns (reconciled). Extraction by strict severity-prefix regex.
- **Exit Codes:** --fail-on <severity> returns nonzero when any finding at/above threshold survives reconciliation (CI gate).
- **Filesystem Discipline:** No writes outside the review directory and config locations. Stdout reserved for command output; in `atcr serve` mode, stdio transport owns stdout and all human/log output goes to stderr.

## Documentation References

Organized documentation indexes for implementation reference:

**[CRITICAL] Core Architecture (read before coding):**
- [CLI Architecture & Commands](documentation/cli-architecture.md) — Cobra framework, command structure, flag validation, exit codes
- [MCP Server Implementation](documentation/mcp-server.md) — Stdio server, tool registration, typed args/results, stderr discipline
- [LLM Client & Fan-out Engine](documentation/llm-client-fanout.md) — OpenAI-compatible client, retry policy, parallel/serial lanes, timeouts

**[IMPORTANT] Supporting Systems (review during development):**
- [Configuration & Registry](documentation/configuration-management.md) — Two-tier YAML config, strict parsing, precedence rules
- [Testing Framework & Patterns](documentation/testing-patterns.md) — Testify assertions, httptest mocks, table-driven tests, fixtures

Full documentation index: [documentation/README.md](documentation/README.md)

---

## Implementation Strategy

The implementation follows the 13-task breakdown in dependency order:

1. **Scaffold:** Go module, cobra CLI skeleton, internal/ package boundaries, CI wiring (Go 1.24+)
2. **Config + init:** registry.yaml and .atcr/config.yaml loading, fallback-chain validation (dangling + cycle = error), atcr init, embedded defaults
3. **gitrange:** Resolution decision tree, default-branch detection, empty-range hard error, shallow detection, diff computation; atcr range command
4. **Payload engine:** diff, blocks (--function-context expansion), files builders; byte budgets + deterministic truncation with recording; payload template vars
5. **Personas:** Six embedded persona templates with prompt bodies ported from the battle-tested llm-tools registry prompts (adversarial personality clause, priority-ordered focus areas, inline output example row), updated for atcr: emit 7 columns with the engine appending REVIEWER, severity rubric defined directly as CRITICAL/HIGH/MEDIUM/LOW, per-payload-mode scope rules added. Add `persona` field to the registry schema (agents and fallbacks reference personas by name); resolution chain (flag > persona ref > agent.md > _base.md > embedded)
6. **LLM client:** OpenAI-compatible chat client, retry policy (429/5xx, ≈500ms initial, 1.5× backoff), per-agent temperature/timeout
7. **Fan-out engine:** Parallel/serial lanes, fallback invocation, global-timeout context handling, partial-success semantics, per-agent artifacts, merged pool findings, summary.json; atcr review command + review-directory/manifest/latest management
8. **Findings stream package:** v1 spec, extraction regex, escaping, padding, parser/writer shared by fan-out and reconcile
9. **Reconciler:** Discovery, normalization, clustering, deterministic text-similarity dedupe, merge rules, confidence, disagreement annotation, ambiguous.json sidecar, all four reconciled artifacts; atcr reconcile + --fail-on
10. **Report:** md/json/checklist renderers over findings.json; atcr report
11. **MCP server:** atcr serve with tool handlers (atcr_review, atcr_reconcile, atcr_report, atcr_range, atcr_status) over the engine
12. **Skill:** skill/SKILL.md with host review (writes v1 findings to sources/host/), orchestration loop, ambiguity adjudication path
13. **Docs:** README rewrite, docs/findings-format.md, docs/registry.md, docs/payload-modes.md (with diff-vs-blocks guidance), CI gate examples

Each task includes TDD: write failing tests, implement to pass, refactor. Tests use testify for assertions and httptest for LLM client mocks.

## Recommended Packages

**Required (epic-specified):** spf13/cobra (CLI), gopkg.in/yaml.v3 (YAML), modelcontextprotocol/go-sdk (MCP)

**Recommended:** github.com/stretchr/testify (testing assertions/mocks)

**Stdlib (no external deps):** net/http (LLM API calls), log/slog (structured logging), os/exec (git operations)

## User Story Themes

### US-01: CLI Review Workflow
**Persona:** Developer  
**Journey:** Run `atcr review` on a feature branch → review directory created with per-agent artifacts → run `atcr reconcile` → view `reconciled/report.md`  
**Focus:** End-to-end CLI workflow with zero arguments

### US-02: Agent Configuration
**Persona:** Developer  
**Journey:** Run `atcr init` → edit .atcr/config.yaml to select agents → configure providers in ~/.config/atcr/registry.yaml → run review with custom roster  
**Focus:** Two-tier configuration with precedence rules

### US-03: CI Integration
**Persona:** CI Pipeline  
**Journey:** Run `atcr reconcile --fail-on HIGH` in CI → exit code 0 if no HIGH+ findings survive, nonzero otherwise → block PR merge on failure  
**Focus:** Exit-code semantics for PR gates

### US-04: MCP Integration
**Persona:** IDE/Agent (MCP Client)  
**Journey:** Connect to `atcr serve` via MCP stdio → call atcr_review, atcr_reconcile, atcr_report tools → receive structured results  
**Focus:** MCP server exposing engine as tools

### US-05: Host Review via Skill
**Persona:** Agent Skill User  
**Journey:** Invoke skill → skill runs host-model review → writes sources/host/findings.txt in v1 format → orchestrates atcr review → reconcile → present report  
**Focus:** Skill contributes +1 reviewer for confidence signal

### US-06: Payload Mode Selection
**Persona:** Developer  
**Journey:** Configure default payload mode in .atcr/config.yaml → override per-agent in registry → manifest.json records who saw what → small models get blocks, frontier models get diff  
**Focus:** Three payload modes (diff, blocks, files) with per-agent overrides

## Planning Success Criteria

- [ ] Architecture supports CLI, MCP, and Skill over a shared engine with no logic forks
- [ ] Range resolution handles all decision tree branches with hard errors for empty ranges
- [ ] Reconciler deterministically merges multi-source findings with confidence scoring
- [ ] Findings format is versioned (atcr-findings/v1) and machine-parseable
- [ ] CI integration via --fail-on exit codes works without glue code
- [ ] Test coverage ≥70% with unit tests for all critical paths (range, payload, stream, reconciler, fan-out, exit codes)
- [ ] `go vet ./...` clean across the whole module
- [ ] `golangci-lint run` clean (staticcheck + errcheck enabled per `.planning/specifications/coding-standards.md`)
- [ ] CI workflow green on every push to `main` and every PR (gates merge per `.planning/specifications/git-strategy.md`)

## Risk Mitigation

**Risk:** Deterministic text-similarity dedupe under/over-merges vs. prompt-based judgment  
**Mitigation:** Conservative threshold + ambiguous.json sidecar for clusters in the gray zone. Skill adjudication path for semantic review. Tune with fixture corpora from real multi-agent runs.

**Risk:** blocks payload builder edge cases (languages without braces, generated files, renames, binary files)  
**Mitigation:** Fall back to plain -U<n> context diff per file when --function-context fails. Explicit tests per case.

**Risk:** Token cost surprises for blocks/files on large ranges  
**Mitigation:** Byte budgets with recorded truncation (never silent). Documentation steers large ranges to diff mode. Per-agent payload overrides.

**Risk:** `files` payload mode surfaces out-of-change findings that pollute reconciliation  
**Mitigation:** Per-payload scope rules in persona prompts; reconcile annotates findings outside the changed-range files.

**Risk:** Findings-format spec churn after publication  
**Mitigation:** Version header (`atcr-findings/v1`) from day one; additive-only evolution policy documented in `docs/findings-format.md`.

## Out of Scope (v1)

Explicit non-goals for v1, deferred or dropped permanently per the requirements:

- **Openclaw / remote-container backend** — dropped permanently; v1 interacts with providers via direct HTTPS only.
- **Sprint-pipeline coupling** — no `SPRINT_PATH`, DoD / work-item verification, test/coverage/lint gates, pre-seeded TD captures, or merge-commit file scraping. The `--merge-commit` flag (base = SHA^, head = SHA) is the lone carry-over.
- **Backlog management** — no TD README, no global numbering, no persistence. Downstream consumers own this; atcr emits findings only.
- **SARIF output** — candidate v2 deliverable; not emitted by `atcr report` in v1.
- **GitHub PR-comment posting** — candidate v2 deliverable; CI gates use exit codes only in v1.
- **Web UI, hosted service, telemetry** — none in v1; atcr is a local-first, BYO-keys tool.

## Extended Scope (refinement annotation)

Items in this plan beyond the literal requirements text, retained because they are load-bearing for the architecture or explicit design clarifications captured in the requirements:

- **Default review-id scheme** (`<YYYY-MM-DD>_<branch-slug>`) — was marked "Proposed; confirm at execution" in the requirements; the plan codifies it so the `atcr review` command and the `.atcr/latest` pointer have a deterministic shape. Sprint task 1 may override.
- **Manifest schema** — requirements mention `manifest.json` but did not enumerate its fields. The plan specifies `base`/`head` SHAs, `detection_mode`, default and per-agent `payload_mode(s)`, `roster`, `started_at`/`completed_at`, `partial` flag — making every downstream tool data-driven rather than CLI-flag-driven.
- **Filesystem discipline** (`stdout` reserved; `atcr serve` writes only to stderr) — not enumerated in requirements but necessary for stdio transport to coexist with human-readable output.
- **golangci-lint + go vet gating** in success criteria — requirements list "go vet and golangci-lint clean; CI green" as a quality bar; the plan spells this out as an explicit success criterion.
- **Six named personas** (bruce, greta, kai, mira, dax, otto) — requirements name them; the plan assigns each a domain (generalist/correctness, algorithmic, architecture, production, testing, style) so user-story authors can route review concerns to a specific persona.

## Clarifications

- **Q: Should atcr's shipped personas pull from the existing `~/.config/llm-tools/agents/registry.yaml` to make agents more robust, or use the plan's persona domain assignments?** A: Both, split by layer. The shipped six personas (bruce, greta, kai, mira, dax, otto) keep the plan's domain assignments, but their prompt bodies are ported from the battle-tested registry prompts — the adversarial personality clause ("find problems the author would prefer you didn't", no-flattery rules), priority-ordered focus areas, and the inline output example row. The registry's wiring (providers, model bindings, fallback pairs, local endpoints) does NOT ship as defaults — it is environment-specific and stays in the user's personal `~/.config/atcr/registry.yaml`. (2026-06-10)
- **Q: How do fallback agents get their prompts?** A: Persona and agent are decoupled in the registry schema. A `persona` is a named prompt (lens, personality, severity rubric); an `agent` is a provider+model binding that references one (`persona: bruce`). Fallback agents reference the same persona as their primary — never duplicated prompt text. This replaces the copy-paste pattern in the source registry (e.g., `bruce-backup` carrying a full copy of bruce's prompt). (2026-06-10)
- **Q: What must change when porting the prompt bodies?** A: Three corrections: (1) personas emit 7-column findings and the engine appends REVIEWER — models no longer self-attribute; (2) the severity rubric is defined directly in terms of CRITICAL/HIGH/MEDIUM/LOW rather than blocking/significant/minor with implicit translation; (3) per-payload-mode scope rules are added (new requirement — the source prompts assume a diff). (2026-06-10)
- **Q: Does the extended personal cast (bob2, archer, ronin, brad, bob, vera, backups) ship with atcr?** A: No. They remain in the user's personal registry; atcr supports them as soon as it is pointed at the user's providers, but the shipped defaults are the six personas only. (2026-06-10)

## Next Steps
1. `/find-documentation @.planning/plans/active/1.0_atcr_core/`
2. `/create-documentation @.planning/plans/active/1.0_atcr_core/`
3. `/create-user-stories @.planning/plans/active/1.0_atcr_core/`
4. `/create-acceptance-criteria @.planning/plans/active/1.0_atcr_core/`
5. `/design-sprint @.planning/plans/active/1.0_atcr_core/`
6. `/create-sprint @.planning/plans/active/1.0_atcr_core/`
