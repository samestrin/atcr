## [1.0.0] - 2026-06-11

### Added

- Go CLI binary with `review`, `reconcile`, `report`, `init`, `serve`, and `status` commands
- Fanout engine that dispatches review prompts to heterogeneous LLM reviewer personas
- Git range resolver supporting base/head refs, merge-commit SHA, and `.atcr/latest` pointer
- Reconciliation pipeline with dedup, cluster-merge, ambiguous detection, and confidence scoring
- MCP stdio server with tool schemas for review/reconcile/report/status integration
- Payload builders with diff extraction, persona templates, byte-budget truncation, and manifest generation
- Registry/config system with project overlay, persona resolution, precedence chain, and gate configuration
- Six embedded reviewer personas (bruce, greta, kai, mira, dax, otto) with shared base template
- Agent Skill definition for host-model review contribution and orchestration
- CI workflow with gofmt verification, golangci-lint, and race-enabled test suite
