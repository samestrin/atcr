## Overview

Plan 1.0 implements v1 of atcr (Agent Team Code Review): a standalone Go tool that fans code changes out to a panel of heterogeneous LLM reviewer personas, then deterministically reconciles their findings into a single deduplicated, confidence-scored deliverable. The tool consists of a single Go binary (CLI + MCP server over one engine) and a companion Agent Skill that contributes the host-model review and orchestrates the flow.

This is a feature plan with 6 user stories covering CLI workflow, agent configuration, CI integration, MCP integration, host review via Skill, and payload mode selection. Complexity level: Complex.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/1.0_atcr_core/`
- [ ] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/1.0_atcr_core/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/1.0_atcr_core/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/1.0_atcr_core/`

## Timeline & Milestones

| Milestone | Target | Status |
|-----------|--------|--------|
| Plan created | 2026-06-10 | ✅ Complete |
| User stories generated | TBD | Pending |
| Acceptance criteria generated | TBD | Pending |
| Sprint designed | TBD | Pending |
| Sprint created | TBD | Pending |
| Implementation complete | TBD | Pending |
| Code review complete | TBD | Pending |
| Sprint complete | TBD | Pending |

## Resource Requirements

**Development:**
- Go 1.24+ environment
- golangci-lint for code quality
- Access to OpenAI-compatible LLM APIs for testing (or mocks)

**External Dependencies:**
- spf13/cobra (CLI framework)
- gopkg.in/yaml.v3 (YAML parsing)
- modelcontextprotocol/go-sdk (MCP server)
- github.com/stretchr/testify (testing)

**Reference Implementations (conceptual only):**
- llm-tools: internal/support/{commands,multireview,gitrange}, pkg/llmapi (~1,900 LOC core)
- claude-prompts: .claude/skills/{execute-code-review,reconcile-code-review}, persona templates

## Expected Outcomes

**Functional Deliverables:**
1. Go binary with CLI commands: atcr review, atcr reconcile, atcr report, atcr range, atcr init, atcr serve
2. MCP server exposing engine as tools (atcr_review, atcr_reconcile, atcr_report, atcr_range, atcr_status)
3. Agent Skill with host review and orchestration loop
4. Six embedded reviewer personas (bruce, greta, kai, mira, dax, otto)
5. Deterministic reconciler with clustering, dedupe, confidence scoring, ambiguous.json sidecar
6. Findings format v1 (pipe-delimited, versioned, machine-parseable)

**Quality Deliverables:**
- Unit test coverage ≥70% (range resolution, payload builders, stream parsing, fan-out concurrency, reconciler clustering/dedupe/confidence)
- Integration tests for end-to-end review workflow
- go vet and golangci-lint clean
- CI pipeline green (lint, test, build)

**Documentation Deliverables:**
- README.md rewritten around actual architecture (panel + reconcile)
- docs/findings-format.md (versioned spec)
- docs/registry.md (configuration guide)
- docs/payload-modes.md (diff vs blocks vs files guidance)
- CI gate examples

## Risk Summary

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Text-similarity dedupe under/over-merges | Medium | High | Conservative threshold + ambiguous.json sidecar; Skill adjudication path; tune with fixture corpora |
| blocks payload builder edge cases | Medium | Medium | Fallback to context diff; explicit tests per case (no braces, generated files, renames, binary) |
| Token cost surprises for blocks/files | Medium | Medium | Byte budgets with recorded truncation; docs steer large ranges to diff mode |
| Format spec churn after publication | Low | High | Version header (atcr-findings/v1) from day one; additive-only evolution policy |

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

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Package Recommendations](package-recommendations.md)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
