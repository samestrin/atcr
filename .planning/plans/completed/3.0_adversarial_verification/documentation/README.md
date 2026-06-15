# Plan Documentation References

**Created:** June 14, 2026 08:36:08AM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### Critical

1. **[verification-pipeline.md](verification-pipeline.md)** — Core verification mechanics: skeptic selection with different-model rule, verdict parsing (confirmed/refuted/unverifiable), confidence v2 tier model, re-emit path, and gate semantics. This is the architectural backbone of Epic 3.0.

2. **[cli-mcp-integration.md](cli-mcp-integration.md)** — The `atcr verify` CLI subcommand, `--verify` chaining flag on `atcr review`, and the `atcr_verify` MCP tool. Covers Cobra command structure, MCP tool registration, and handler patterns.

### Important

3. **[llm-tool-loop.md](llm-tool-loop.md)** — Skeptic invocation via the Epic 2.0 tool loop (`invokeToolLoop`), per-finding prompt construction, budget controls, and partial-success semantics.

### Reference

4. **[testing-fixtures.md](testing-fixtures.md)** — Testify assertion patterns, golden file testing, fixture corpus with planted true/false findings, verdict parsing tests, and end-to-end verification tests.

### Source Index

5. **[source.md](source.md)** — Lists the global specifications and package docs this plan's documentation is grounded against.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - `.planning/specifications/packages/cobra.md` (v1.10.2)
  - `.planning/specifications/packages/mcp-sdk.md` (v1.6.1)
  - `.planning/specifications/packages/openai.md`
  - `.planning/specifications/packages/go.md`
  - `.planning/specifications/packages/standard-library.md`
  - `.planning/specifications/packages/testify.md` (v1.11.1)
- **Codebase Discovery:** [codebase-discovery.json](../codebase-discovery.json)
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
