# Plan Documentation References

**Created:** June 10, 2026
**Last Modified:** 2026-06-10
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### [CRITICAL] Core Architecture & Contracts

| File | Focus |
|------|-------|
| [reconciler.md](reconciler.md) | Deterministic reconciler pipeline (cluster, dedupe, merge, confidence, ambiguous sidecar) — the net-new core of v1 |
| [findings-format.md](findings-format.md) | `atcr-findings/v1` pipe-delimited format (per-source 8 cols, reconciled 10 cols) — the public contract with downstream tools |
| [cli-architecture.md](cli-architecture.md) | Cobra CLI framework, command structure, flag validation, exit codes, context propagation |
| [mcp-server.md](mcp-server.md) | MCP stdio server, generic `mcp.AddTool` with typed args/result, stderr discipline |
| [llm-client-fanout.md](llm-client-fanout.md) | OpenAI-compatible client, retry policy, parallel/serial lanes, timeout handling |
| [range-resolution.md](range-resolution.md) | Git range decision tree, default-branch detection, empty-range hard error, shallow-clone guard |
| [payload-engine.md](payload-engine.md) | Three payload modes (diff/blocks/files), byte budgets, deterministic truncation recording |

### [IMPORTANT] Supporting Systems

| File | Focus |
|------|-------|
| [configuration-management.md](configuration-management.md) | Two-tier YAML config, strict parsing, precedence rules, fallback-chain validation |
| [testing-patterns.md](testing-patterns.md) | Testify assertions/mocks, httptest provider mocks, table-driven tests, fixtures |

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - `.planning/specifications/packages/cobra.md` — CLI framework
  - `.planning/specifications/packages/mcp-go-sdk.md` — MCP SDK
  - `.planning/specifications/packages/yaml-v3.md` — YAML parsing
  - `.planning/specifications/packages/testify.md` — Testing framework
  - `.planning/specifications/packages/standard-library.md` — Go stdlib patterns
- **Codebase Discovery:** `.planning/plans/active/1.0_atcr_core/codebase-discovery.json`
- **Plan:** `.planning/plans/active/1.0_atcr_core/plan.md`
- **Requirements:** `.planning/plans/active/1.0_atcr_core/original-requirements.md`

---

## How to Use

1. Start with **Critical** documentation before coding — the reconciler and findings format define the public contract; CLI, MCP, fan-out, range resolution, and payload engine are the surrounding execution layer
2. Review **Important** docs during development — configuration and testing patterns
3. Consult source documents directly for API details and edge cases

---

**Navigation:** [← Back to Plan](../README.md)
