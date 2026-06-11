# Sprint Design: atcr Core - Review Engine, Reconciler, and Skill

**Created:** June 10, 2026
**Plan:** [1.0_atcr_core](.planning/plans/active/1.0_atcr_core/)
**Plan Type:** Feature (✨)
**Status:** Design Complete

---

## Original User Request

> Build v1 of atcr (Agent Team Code Review): a standalone, shareable tool that fans a code change out to a panel of heterogeneous LLM reviewer personas, then deterministically reconciles their findings into a single deduplicated, confidence-scored deliverable. Composed of a single Go binary (CLI + MCP server over one engine) and a companion Agent Skill that contributes the host-model review and orchestrates the flow.

**Referenced Resources:**

- [Epic Plan 1.0](.planning/epics/active/1.0_atcr_core.md)
  - **Summary**: Full epic requirements covering architecture, commands, filesystem layout, findings format, payload modes, configuration, reconciler pipeline, Skill, and MCP server.
  - **Key Points**: Fresh rewrite informed by llm-tools/claude-prompts; deterministic Go reconciler is the core value-add; six embedded personas (bruce, greta, kai, mira, dax, otto); three payload modes (diff, blocks, files); versioned findings format (atcr-findings/v1).

- [CLI Architecture](.planning/plans/active/1.0_atcr_core/documentation/cli-architecture.md)
  - **Summary**: Cobra-based CLI with six subcommands, context propagation, flag validation, and centralized exit codes.
  - **Key Points**: RunE lifecycle hooks; ExecuteContext for global timeout; MarkFlagsMutuallyExclusive for declarative validation; stdout reserved for output, stderr for logs.

- [Reconciler & Findings Stream](.planning/plans/active/1.0_atcr_core/documentation/reconciler.md)
  - **Summary**: Deterministic Go pipeline replacing prompt-based merge: discover → normalize → cluster → dedupe → merge → confidence → emit.
  - **Key Points**: Pipe-delimited v1 format (8-col per-source, 9-col reconciled); Jaccard token-set similarity ≥0.7 for merge, 0.4-0.7 gray zone → ambiguous.json; conservative default (unmerged) for ambiguous clusters.

- [LLM Client & Fan-out Engine](.planning/plans/active/1.0_atcr_core/documentation/llm-client-fanout.md)
  - **Summary**: OpenAI-compatible HTTP client with retry logic, parallel/serial fan-out lanes, context-based timeout hierarchy.
  - **Key Points**: net/http only (no provider SDKs); retry 429/5xx with ~500ms/1.5x backoff; WaitGroup always drains on cancel; partial-success semantics (error only if ALL agents fail).

- [Payload Engine](.planning/plans/active/1.0_atcr_core/documentation/payload-engine.md)
  - **Summary**: Three payload modes (diff, blocks, files) with per-agent overrides, byte budgets, deterministic truncation.
  - **Key Points**: `blocks` is default (small models read code better than diffs); `diff` is most token-friendly; truncation recorded in status.json, never silent; function-context fallback for edge cases.

- [MCP Server](.planning/plans/active/1.0_atcr_core/documentation/mcp-server.md)
  - **Summary**: Stdio MCP server exposing engine as five tools via generic mcp.AddTool with typed schemas.
  - **Key Points**: stdout owned by protocol; handlers are thin wrappers over shared engine; InMemoryTransport for testing; context propagation from client.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** atcr-core-review
**Complexity:** 9/12 (VERY COMPLEX)
**Timeline:** 11 days
**Phases:** 5
**Pattern:** Foundation → Core → Advanced → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Go multi-agent fan-out concurrency patterns
deterministic deduplication clustering Jaccard
MCP server Go SDK tool registration
OpenAI compatible retry backoff HTTP client
Go pipe-delimited stream parsing regex
cobra CLI context propagation exit codes
payload builder function-context git diff
```

---

## Complexity Breakdown

- **Architecture:** 3/3 - Novel reconciler design (cluster/dedupe/confidence/ambiguity), three-mode payload engine, dual CLI+MCP interface over shared engine, two-tier config with precedence resolution. No existing patterns to reuse; all built from scratch.
- **Integration:** 2/3 - Git operations via os/exec, OpenAI-compatible HTTP client, MCP go-sdk stdio transport, filesystem management for review directories. Four distinct external integration points, each well-defined but new implementation.
- **Story/Task & Test:** 2/3 - 6 stories, 24 ACs (14 unit + 10 integration). 8 ACs marked high complexity. Substantial test surface including httptest provider mocks, golden file comparisons, and integration tests.
- **Risk/Unknowns:** 2/3 - Deterministic text-similarity dedupe accuracy vs prompt-based judgment, blocks payload edge cases (binary files, no-brace languages, renames), token cost surprises, files mode out-of-scope findings.

**Time Formula:** Base 8 days (foundation+core+advanced) + 1 day per integration phase + 2 days validation buffer
**Calculation:** 8 + 1 (integration) + 2 (validation/docs) = 11 days

---

## Recommended Flags

**Adversarial:** Yes (complexity 9/12 ≥ 6, phases 5 ≥ 3)
**Gated:** Yes (complexity 9/12 ≥ 8, phases 5 ≥ 5)
**Recommendation strength:** Standard gated (complexity 9/12 < 10)
**Suggested command:** `/create-sprint @.planning/plans/active/1.0_atcr_core/ --gated`

---

## Phase Structure

### Phase 1: Foundation (Days 1-2)

**Focus:** Go module scaffold, cobra CLI skeleton, internal package boundaries, two-tier config loading with validation.

| Item | Source | Testable Elements | Test Type |
|------|--------|-------------------|-----------|
| Scaffold Go module + cobra CLI | US-01 | `go build` succeeds, `atcr --help` shows subcommands | Integration |
| Internal package boundaries | US-01 | Package imports compile, no circular deps | Unit |
| Registry config loading (yaml.v3 strict) | US-02 | Parse valid/invalid YAML, required field validation | Unit |
| Project config loading (.atcr/config.yaml) | US-02 | Parse project config, embedded defaults | Unit |
| Precedence resolution (CLI > project > registry > embedded) | US-02 | Each override level tested independently | Unit |
| Fallback chain validation (dangling + cycle detection) | US-02 | DFS cycle detection, dangling ref detection | Unit |
| `atcr init` command | US-02 | Creates .atcr/ dir, writes config + 6 persona files | Integration |

### Phase 2: Core Systems (Days 3-5)

**Focus:** Git range resolution, three-mode payload engine, findings stream parser, OpenAI-compatible LLM client.

| Item | Source | Testable Elements | Test Type |
|------|--------|-------------------|-----------|
| Range resolution decision tree | US-01 | Explicit base/head, merge-commit, auto-detect branches | Unit |
| Default branch detection | US-01 | origin/HEAD → origin/main → origin/master → local fallback | Unit |
| Empty range hard error | US-01 | 0 commits → error before any provider call | Unit |
| Shallow clone detection | US-01 | Detect shallow, suggest `git fetch --unshallow` | Unit |
| `atcr range` command | US-01 | Prints resolution JSON to stdout | Unit |
| Diff payload builder | US-06 | `git diff base..head` output verbatim | Unit |
| Blocks payload builder | US-06 | `--function-context` expansion, fallback to -U10 | Unit |
| Files payload builder | US-06 | Head version with changed-region sentinels | Unit |
| Byte budget truncation | US-06 | Deterministic drop largest-first, record in status.json | Unit |
| Per-agent payload override | US-06 | Registry `payload:` field overrides project default | Unit |
| Findings stream parser (v1 format) | US-01 | Strict severity-prefix regex, pipe escaping, short-row padding | Unit |
| OpenAI-compatible HTTP client | US-01 | POST to /chat/completions, auth header, JSON decode | Unit |
| Retry policy (429/5xx, ~500ms/1.5x backoff) | US-01 | httptest mock with retryable/non-retryable responses | Unit |

### Phase 3: Engines (Days 6-8)

**Focus:** Fan-out concurrency engine, reconciler pipeline, report rendering.

| Item | Source | Testable Elements | Test Type |
|------|--------|-------------------|-----------|
| Parallel lane (WaitGroup, context cancellation) | US-01 | Concurrent agent execution, drain on cancel | Unit |
| Serial lane (rate-limited agents) | US-01 | Sequential execution, ctx.Err() check before each | Unit |
| Fallback chain invocation | US-01 | Primary fails → fallback tried, fallback_used/fallback_from recorded | Unit |
| Partial-success semantics | US-01 | ≥1 success → exit 0 + partial:true; all fail → exit nonzero | Unit |
| Per-agent status.json | US-01 | ok/failed/timeout written for every agent | Unit |
| `atcr review` command + review directory | US-01 | Creates .atcr/reviews/<id>/, manifest.json, .atcr/latest pointer | Integration |
| Manifest.json schema | US-01 | base/head SHAs, detection_mode, payload_modes, roster, timestamps | Unit |
| Review ID generation (<YYYY-MM-DD>_<branch-slug>) | US-01 | Deterministic ID from date + branch slug | Unit |
| Source discovery (sources/* with findings.txt) | US-01 | Find all sources, exclude reconciled/ | Unit |
| Normalization (parse + pad + skip comments) | US-01 | Tolerate short rows, skip blanks/# lines | Unit |
| Clustering (FILE, LINE±3) | US-01 | Same file + nearby lines → same cluster | Unit |
| Deduplication (Jaccard token-set ≥0.7) | US-01 | Identical → merge, gray zone → ambiguous.json, distinct → separate | Unit |
| Merge rules (per-field) | US-01 | REVIEWERS joined, SEVERITY max, PROBLEM longest, CATEGORY modal | Unit |
| Confidence computation | US-01 | HIGH=2+ reviewers, MEDIUM=single, LOW=untrusted | Unit |
| Disagreement annotation | US-01 | severity mismatch → `disagreement: lo vs hi` preserved | Unit |
| ambiguous.json sidecar | US-01 | Gray-zone clusters written, default unmerged | Unit |
| `atcr reconcile` + --fail-on | US-03 | Exit nonzero when findings ≥ threshold survive | Unit |
| Report renderers (md/json/checklist) | US-01 | Three formats from same findings.json, golden file comparison | Unit |
| `atcr report` command | US-01 | --format flag selects renderer, output to stdout | Unit |

### Phase 4: Integration (Days 9-10)

**Focus:** MCP server, Skill definition, end-to-end orchestration validation.

| Item | Source | Testable Elements | Test Type |
|------|--------|-------------------|-----------|
| `atcr serve` stdio server startup | US-04 | StdioTransport initializes, stdin/stdout protocol | Integration |
| Tool registration (5 tools, typed schemas) | US-04 | mcp.AddTool with generic args/result structs | Unit |
| atcr_review handler | US-04 | Thin wrapper over engine.Review, same behavior as CLI | Integration |
| atcr_reconcile handler | US-04 | Thin wrapper over engine.Reconcile, --fail-on support | Integration |
| atcr_report, atcr_range, atcr_status handlers | US-04 | Thin wrappers, structured results | Integration |
| Stdout/stderr discipline | US-04 | Protocol on stdout, logs on stderr | Unit |
| Skill SKILL.md structure | US-05 | Installable to .claude/skills/atcr/, valid markdown | Integration |
| Host review findings generation | US-05 | Writes sources/host/findings.txt in v1 format | Unit |
| Orchestration loop (range → review → host → reconcile → report) | US-05 | Sequential steps, poll for completion | Integration |
| Adversarial review + ambiguity adjudication | US-05 | Host reviews adversarially, reads ambiguous.json, re-invokes reconcile | Integration |

### Phase 5: Validation & Docs (Day 11)

**Focus:** Documentation, CI examples, lint/vet clean, final validation.

| Item | Source | Testable Elements | Test Type |
|------|--------|-------------------|-----------|
| README rewrite | US-01 | Architecture overview, quickstart, payload mode guidance | Integration |
| docs/findings-format.md | US-01 | Versioned spec with examples | Integration |
| docs/registry.md | US-02 | Provider/agent/persona configuration reference | Integration |
| docs/payload-modes.md | US-06 | diff-vs-blocks token guidance, per-agent override examples | Integration |
| examples/ci-gate.sh | US-03 | Working CI script with --fail-on | Integration |
| `go vet ./...` clean | Quality | Zero warnings | Unit |
| `golangci-lint run` clean | Quality | Zero warnings | Unit |
| Coverage ≥70% | Quality | go test -coverprofile meets threshold | Unit |

---

## Work Decomposition

### US-01: CLI Review Workflow (6 ACs, High Complexity)

**Testable Elements:**
1. **End-to-End Review** (01-01): `atcr review` → directory created → per-agent artifacts written → `atcr reconcile` → `reconciled/report.md` exists. Integration test with httptest mock provider.
2. **Git Range Resolution** (01-02): Decision tree branches (explicit/merge-commit/auto), empty range error, shallow clone detection. Table-driven unit tests with exec.Command mocks.
3. **Review Directory Structure** (01-03): `.atcr/reviews/<id>/` layout, manifest.json fields, `.atcr/latest` pointer. Unit test verifying directory creation and file contents.
4. **Fan-out Agent Execution** (01-04): Parallel lane concurrency (WaitGroup drain on cancel), serial lane ctx check, fallback chains, partial-success semantics. Unit tests with httptest provider + atomic concurrency counters.
5. **Reconciliation Pipeline** (01-05): Discover → normalize → cluster → dedupe → merge → confidence → emit. Table-driven unit tests with fixture findings files.
6. **Report Rendering** (01-06): md/json/checklist formats from same data. Golden file comparison tests.

### US-02: Agent Configuration (4 ACs, Medium Complexity)

**Testable Elements:**
1. **Init Command** (02-01): Creates .atcr/ directory, writes config.yaml + 6 persona .md files. Integration test on temp directory.
2. **Provider/Agent Registry** (02-02): Parse registry.yaml with strict mode, validate required fields (provider, model). Unit tests with valid/invalid YAML fixtures.
3. **Precedence and Validation** (02-03): CLI > project > registry > embedded chain; cycle detection (DFS); dangling ref detection. Unit tests per precedence level + graph validation.
4. **Persona Resolution** (02-04): Resolution chain (--task-message > persona ref > agent.md > _base.md > embedded). Unit tests with template rendering and fallback.

### US-03: CI Integration (2 ACs, Medium Complexity)

**Testable Elements:**
1. **Fail-on Severity Threshold** (03-01): `atcr reconcile --fail-on HIGH` exits nonzero when HIGH+ findings survive. Unit tests with fixture reconciled findings at each severity level.
2. **CI One-Shot Mode** (03-02): `atcr review --fail-on <severity>` runs full pipeline and exits with threshold. Integration test.

### US-04: MCP Integration (4 ACs, Medium Complexity)

**Testable Elements:**
1. **Stdio Server** (04-01): `atcr serve` starts StdioTransport, responds to MCP protocol. Integration test with InMemoryTransport.
2. **Tool Registration** (04-02): 5 tools registered with typed schemas via mcp.AddTool. Unit test verifying schema inference.
3. **Review/Reconcile Handlers** (04-03): Handlers call engine methods, return typed results. Integration test with InMemoryTransport.
4. **Report/Range/Status Handlers** (04-04): Thin wrappers over engine, structured JSON results. Integration test.

### US-05: Host Review via Skill (4 ACs, High Complexity)

**Testable Elements:**
1. **Skill Structure** (05-01): SKILL.md with frontmatter, installable to .claude/skills/atcr/. Integration test verifying file structure.
2. **Host Review Findings** (05-02): Agent writes sources/host/findings.txt in v1 format (8-col, # atcr-findings/v1 header). Unit test verifying format compliance.
3. **Orchestration Loop** (05-03): range → review (background, polled) → host review → reconcile → report. Integration test verifying sequence.
4. **Adversarial Review + Adjudication** (05-04): Host reviews adversarially, reads ambiguous.json, writes adjudication decisions, re-invokes reconcile. Integration test.

### US-06: Payload Mode Selection (4 ACs, Medium Complexity)

**Testable Elements:**
1. **Payload Builders** (06-01): Three builders (diff, blocks, files) produce correct output. Unit tests with git repo fixtures.
2. **Payload Mode Configuration** (06-02): Project default + per-agent override in registry. Unit tests for config loading.
3. **Byte Budget and Truncation** (06-03): Deterministic truncation (drop largest-first), record in status.json + manifest.json. Unit tests.
4. **Payload Templates and Documentation** (06-04): Template vars (Payload, PayloadMode, FileCount, etc.), scope rules per mode. Unit tests for rendering.

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** `internal/<package>/` alongside source files (Go convention)

**Test File Placement Examples:**
- `internal/gitrange/resolver_test.go` — range resolution table-driven tests
- `internal/payload/builder_test.go` — payload builder tests with git repo fixtures
- `internal/stream/parser_test.go` — findings stream parse/escape/pad tests
- `internal/fanout/engine_test.go` — fan-out concurrency tests with httptest mocks
- `internal/reconcile/merger_test.go` — reconciler pipeline tests with fixture files
- `internal/report/renderer_test.go` — golden file comparison tests
- `internal/mcp/server_test.go` — InMemoryTransport integration tests
- `cmd/atcr/main_test.go` — end-to-end CLI tests

**Unit/Integration/E2E:**
- **Unit (14 ACs):** Table-driven tests using testify/assert, covering range resolution, payload builders, stream parsing, fan-out concurrency, reconciler pipeline, exit codes, config loading. httptest.NewServer for LLM client mocks.
- **Integration (10 ACs):** End-to-end workflow tests: `atcr review` → `atcr reconcile` → `atcr report` pipeline, MCP tool handlers via InMemoryTransport, `atcr init` file creation, Skill installation validation.
- **E2E (0 ACs):** Orchestration covered by integration tests (no external service dependencies).

**Test Environment Status:**
- Framework: `go test` + `github.com/stretchr/testify` (assert, require)
- Execution: `go test ./...`
- Coverage Tools: `go test -coverprofile=coverage.out ./...`, target ≥70%

---

## Architecture

**Primitives:**
- `Finding` — core record with Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence, Reviewer(s), Confidence
- `AgentConfig` — provider ref, model, temperature, timeout, rate_limited, fallback chain, persona ref, payload override
- `ReviewManifest` — base/head SHAs, detection_mode, payload_modes, roster, timestamps, partial flag
- `PayloadMode` — enum: diff | blocks | files
- `ClusterKey` — (file, line/3) bucket for grouping findings

**Module Boundaries:**
- `internal/gitrange` — range resolution (decision tree, default branch detection, diff computation). Pure functions + exec.Command. No config awareness.
- `internal/payload` — payload builders (diff, blocks, files) + byte budget + truncation. Consumes resolved range. No LLM awareness.
- `internal/registry` — config loading (registry.yaml, .atcr/config.yaml), precedence resolution, fallback chain validation, persona resolution. No execution awareness.
- `internal/llmclient` — OpenAI-compatible HTTP client with retry. Takes context + request, returns response. No fan-out awareness.
- `internal/fanout` — parallel/serial lanes, fallback invocation, partial-success semantics. Consumes registry configs + llmclient + payload. No reconciler awareness.
- `internal/stream` — findings v1 format: parser, writer, extraction regex, escaping. Shared by fan-out (write) and reconcile (read). No execution awareness.
- `internal/reconcile` — discover → normalize → cluster → dedupe → merge → confidence → emit. Consumes stream findings. No fan-out awareness.
- `internal/report` — renderers (md, json, checklist) over findings.json. Pure transformation. No execution awareness.
- `internal/mcp` — stdio server, tool registration, handler wrappers. Thin layer over engine. No unique logic.
- `cmd/atcr` — cobra CLI, flag validation, exit code mapping. Thin layer over engine. No unique logic.

**External Dependencies:**
- `github.com/spf13/cobra` — CLI framework (wrap in cmd/atcr only)
- `gopkg.in/yaml.v3` — YAML parsing (wrap in internal/registry only)
- `github.com/modelcontextprotocol/go-sdk` — MCP server (wrap in internal/mcp only)
- `github.com/stretchr/testify` — test assertions (test-only dependency)
- `net/http` — OpenAI-compatible API calls (wrap in internal/llmclient only)
- `os/exec` — git operations (wrap in internal/gitrange and internal/payload only)

**Replaceability:**
Each internal package is a black box replaceable via its interface. The engine struct (composed in cmd/atcr and internal/mcp) is the composition root — swap any package by implementing the same interface. LLM client is the primary extension point: any OpenAI-compatible endpoint works. Persona prompts are data files, not code — replaceable without recompilation.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| API Key Handling | Env var resolution at invoke time | Key leakage via error messages, logs, or status files | Keys resolved from env vars only; never logged; error messages redact key values; status.json contains no secrets |
| Path Traversal | Agent name sanitization, file paths | Malicious agent names or paths escaping review directory | `filepath.Base` sanitization on agent names; all writes confined to `.atcr/reviews/<id>/`; no symlinks followed outside review dir |
| YAML Strict Parsing | registry.yaml and .atcr/config.yaml | Malformed YAML with unexpected fields causing config injection | `yaml.v3` with `KnownFields(true)` strict mode; unknown fields produce load-time errors |
| Findings Injection | LLM-generated findings.txt content | Malformed rows with pipe characters breaking column count, severity spoofing | Strict severity-prefix regex extraction (`^(CRITICAL\|HIGH\|MEDIUM\|LOW)\|`); literal `|` within fields replaced with `/`; short rows padded, never trusted |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Fan-out Engine | 6+ concurrent LLM API calls | All agents complete within global timeout (default 600s) | Parallel lane via WaitGroup; per-agent derived context timeouts; serial lane for rate-limited agents; retry backoff must not exhaust agent timeout |
| Reconciler Clustering | 100-500 findings across 6 sources | Sub-second clustering and deduplication | Map-based bucket key (file, line/3); O(n) clustering; Jaccard computed only within clusters, not pairwise across all findings |
| Payload Construction (files mode) | Large changed file sets (50+ files) | Payload built within 5 seconds | Byte budget with deterministic truncation; sort by size rank, drop largest first; never silently drop content |
| MCP Protocol | Concurrent tool calls from IDE/agent | No blocking; context cancellation honored | Handler functions receive and honor ctx; fan-out operations propagate client timeout; stdout reserved for protocol |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| Empty/Invalid Git Ranges | 0 commits, base==head, non-existent refs, shallow clones | Hard error before any provider call; guidance text for shallow clone remediation |
| Binary and Generated Files | Binary files in blocks/files mode, *.pb.go, *_generated.go | Binary: "binary file changed" marker; generated: plain -U10 context diff; no function-context expansion |
| Language-Specific Edge Cases | Python/Ruby (no braces), Haskell, renamed files | Function-context fallback to -U10; rename detection follows new path |
| Malformed LLM Output | Missing columns, wrong severity text, prose with severity words, extra pipes | Strict regex extraction ignores prose; short rows padded; extra pipes replaced with `/` |
| Concurrent Timeout | Global timeout expires while agents running | WaitGroup always drains; in-flight agents get ctx.Done(); status.json = "timeout" for each; partial:true if ≥1 succeeded |
| Provider Failures | 429 rate limit, 5xx server error, network timeout, non-retryable 4xx | Retry 429/5xx with ~500ms/1.5x backoff; 4xx fails immediately; fallback agent tried if configured; all-fail → nonzero exit |
| Ambiguous Clusters | Findings at same location with similarity 0.4-0.7 | Written to ambiguous.json; default unmerged in output; Skill adjudication path re-invokes reconcile |

### Defensive Measures Required

- **Input Validation:** Strict YAML parsing (`KnownFields(true)`); severity-prefix regex for findings extraction; agent name sanitization via `filepath.Base`; flag mutual exclusion via cobra validators.
- **Error Handling:** Wrap all errors with `fmt.Errorf("context: %w", err)`; never log API keys; error messages reference env var names not values; per-agent errors recorded in status.json, not propagated unless all-fail.
- **Logging/Audit:** Structured logging via `log/slog` to stderr only; stdout reserved for command output (or MCP protocol in serve mode); every truncation recorded in status.json; every fallback recorded with fallback_used/fallback_from.
- **Rate Limiting:** Per-agent timeout derived from global timeout; serial lane for rate_limited agents checks ctx before each invocation; retry backoff tuned (500ms/1.5x) to not exhaust agent timeout.
- **Graceful Degradation:** Partial-success semantics (error only if ALL agents fail); ambiguous.json for conservative unmerged defaults; `files` mode scope rules prevent out-of-change findings from polluting reconciliation.

---

## Risks

**Technical:**
- Deterministic text-similarity dedupe under/over-merges vs. prompt-based judgment → Conservative Jaccard threshold (≥0.7) + ambiguous.json sidecar for gray zone (0.4-0.7); Skill adjudication path; tune with fixture corpora from real multi-agent runs.
- `blocks` payload builder edge cases (languages without braces, generated files, renames, binary files) → Fallback to plain `-U<n>` context diff per file when `--function-context` fails; explicit tests per edge case.
- `files` mode produces out-of-change findings that pollute reconciliation → Per-payload scope rules in persona prompts; reconcile annotates findings outside changed ranges as `out-of-scope`.
- Token cost surprises for `blocks`/`files` on large ranges → Byte budgets with recorded truncation (never silent); documentation steers large ranges to `diff` mode; per-agent payload overrides.
- MCP SDK API surface changes → Pin to v1.6.1; wrap all SDK calls in internal/mcp; interface boundary allows SDK swap.

**TDD-Specific:**
- Git operations via os/exec are hard to unit test → Create temporary git repos in test fixtures; use exec.CommandContext with test-specific repos; mock at the function boundary for higher-level tests.
- LLM client retry/concurrency needs httptest mocks → Use net/http/httptest.NewServer with controlled response sequences; atomic counters for concurrency verification.
- Reconciler clustering/dedupe needs diverse fixture data → Build fixture corpus with known cluster/dedupe outcomes; table-driven tests covering: exact duplicates, near-duplicates, distinct findings at same location, severity disagreements, short rows, empty sources.
- MCP server testing without spawning processes → Use InMemoryTransport from go-sdk; test tool handlers programmatically.
- Golden file tests for report rendering → Generate expected output fixtures from known inputs; use testify golden file comparison pattern.

---

**Next:** `/create-sprint @.planning/plans/active/1.0_atcr_core/ --gated`
