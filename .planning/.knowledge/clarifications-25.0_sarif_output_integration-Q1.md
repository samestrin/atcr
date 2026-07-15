---
id: mem-2026-07-14-c2b39a
question: "MCP stdio doesn't surface server stderr — scope diagnostic-only fixes narrowly"
created: 2026-07-14
last_retrieved: ""
sprints: []
files: [internal/report/sarif.go, internal/report/render.go, internal/mcp/handlers.go, internal/mcp/tools.go]
tags: [clarifications, epic-25.0_sarif_output_integration, implementation, sarif, mcp]
retrievals: 0
status: active
type: clarifications
---

# MCP stdio doesn't surface server stderr — scope diagnostic

## Decision

When a diagnostic-only correction (e.g. an unrecognized-value warning written to a diag io.Writer) is LOW severity and the alternative fix requires threading a new signal through a public render/report signature (Render() -> handleReport -> ReportResult + MCP tool schema), prefer the smaller local fix (e.g. per-render dedup) over the signature-changing plumbing. Rationale: MCP's stdio transport does not surface a server process's stderr to the client as part of the protocol response, so plumbing a warnings channel through ReportResult would be establishing new client-visible surface area rather than fixing an existing gap — disproportionate for a diagnostic-only edge case.

Evidence:
- internal/report/sarif.go:105-108 (diag is a threaded parameter, not a package global); internal/report/sarif.go:101-103 (renderSarif hardwires os.Stderr); internal/report/sarif.go:207-209 (sarifLevel emits one Fprintf per corrupt finding, no dedup)
- internal/report/render.go:57 (Render()'s public signature, called from cmd/atcr/report.go and internal/mcp/handlers.go:416)
- internal/mcp/tools.go:170-173 (ReportResult struct — the MCP-visible surface a warnings channel would need to extend)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/report/sarif.go
- internal/report/render.go
- internal/mcp/handlers.go
- internal/mcp/tools.go
