---
id: mem-2026-06-25-ffdb90
question: "Should a local operator CLI (all paths operator-supplied on command line) apply path-traversal restriction / validate against a base directory?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark.go, cmd/atcr/benchmark_run.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, scope, security, path-traversal, operator-cli]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Should a local operator CLI (all paths operator-supplied on 

## Decision

No — close as not-applicable. Path-traversal restriction is a web-service concern where a trusted server exposes a base directory to untrusted external input. For a local operator CLI (like atcr benchmark) where the operator supplies all paths directly on the command line, there is no untrusted input and no defined base directory. Restricting write paths would actively break legitimate use (e.g., --out /data/results/). Existing flags (--suite-path, --out, --checkpoint) follow the pattern of accepting any operator-chosen path with no base constraint. OWASP path-traversal applies to web services, not local CLIs where the user controls all inputs.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark.go
- cmd/atcr/benchmark_run.go
