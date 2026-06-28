---
id: mem-2026-06-28-6eda99
question: "For astgroup wasm plugins (goparser/pyparser), when source is empty or unparseable, should the plugin error or return a bare root node?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [internal/astgroup/parsers/src/goparser/main.go, internal/astgroup/parsers/src/pyparser/main.go, internal/astgroup/host.go, internal/astgroup/host_test.go, internal/astgroup/grouper.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, plugin-contract, goparser, pyparser, empty-input, bare-root-node]
retrievals: 0
status: active
type: clarifications
---

# For astgroup wasm plugins (goparser/pyparser), when source i

## Decision

All plugins should return a bare root node (Python's current contract). Reasoning:

1. The "both error" option is blocked: internal/astgroup/host_test.go:99-108 (TestHost_ParseEmptySource) already pins Python's bare-root contract; invalidating that test requires changing Python, which is not feasible — pyparser is a best-effort heuristic (pyparser/main.go:6-8) with no concept of "unparseable Python."

2. The bare-root-node contract has cleaner semantics: a root node with no children means "parseable but nothing to group" (GroupKey="" via no-covering-block fallback); an error response means "plugin-level fatal failure." host.go:310-315 currently conflates these.

3. Both Go (current: error) and Python (current: bare root) produce GroupKey="" for empty input, but via different mechanisms — Go via pf.ok=false (grouper.go:167-169), Python via no-covering-block fallback (grouper.go:180-183). This intermediate-state divergence should be eliminated.

Fix for Go: special-case len(src)==0 in goparser/main.go before calling parser.ParseFile and emit node{Kind:"file"} (the root kind per main.go:122-123). Rebuild go.wasm via internal/astgroup/parsers/build.sh.

Host test: add a parallel test for Go in internal/astgroup/host_test.go asserting root.Kind=="file" and err==nil for nil/empty input (mirroring TestHost_ParseEmptySource at host_test.go:99-108).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/parsers/src/goparser/main.go
- internal/astgroup/parsers/src/pyparser/main.go
- internal/astgroup/host.go
- internal/astgroup/host_test.go
- internal/astgroup/grouper.go
