# Plan Documentation References

**Created:** July 18, 2026 08:10:20AM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- [CLI Command & Output Control Patterns (Cobra)](cli-command-patterns.md) — **[CRITICAL]**
- [Exit-Code Contract & CLI/MCP Dual-Surface Precedent (Epic 3.0 `atcr verify`)](exit-code-cli-mcp-precedent.md) — **[CRITICAL]**
- [Existing Agent-Facing Format & Output-Safety Contracts](agentic-format-precedents.md) — **[IMPORTANT]**
- [MCP Tool Schema & Format-Enum Propagation](mcp-schema-format-propagation.md) — **[IMPORTANT]**
- [TOON Format Reference (Token Optimized Object Notation)](toon-format-reference.md) — **[REFERENCE]**

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - `.planning/specifications/packages/cobra.md`
  - `.planning/specifications/design-concepts/adversarial-verification-interface.md`
  - `.planning/specifications/packages/jsonschema-go.md`
  - `.planning/specifications/packages/mcp-go-sdk.md`
  - `.planning/specifications/packages/mcp-sdk.md`
  - `docs/findings-format.md`
  - `docs/payload-modes.md`
  - [TOON — toonformat.dev](https://toonformat.dev/)
  - [TOON Syntax Cheatsheet](https://toonformat.dev/reference/syntax-cheatsheet.html)
- **Local Plan References:**
  - [`source.md`](./source.md) — generated source-path index for this documentation set
- **Codebase Discovery:** `.planning/plans/active/31.0_axi_compliance/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
