---
id: mem-2026-06-29-09b1ab
question: "braceparser CI Go version policy: bump all jobs to 1.26 or add a separate braceparser-module job?"
created: 2026-06-29
last_retrieved: ""
sprints: []
files: [.github/workflows/ci.yml, internal/astgroup/parsers/src/braceparser/go.mod, go.mod, internal/astgroup/parsers/build.sh, internal/astgroup/embed_test.go]
tags: [clarifications, epic-13.4_brace_language_parsers, ci-cd, braceparser, go-version, wasm]
retrievals: 0
status: active
type: clarifications skill — epic 13.4_brace_language_parsers Q6 second-run (2026-06-29)
---

# braceparser CI Go version policy: bump all jobs to 1.26 or a

## Decision

Add a dedicated braceparser-module CI job provisioning Go 1.26 — do NOT bump all jobs. The root module (go.mod: go 1.25.0) and reconcile module have no reason to move. The pattern already exists: the reconcile-module job in .github/workflows/ci.yml uses a separate setup-go step with its own version. The new job should use go-version: '1.26' and working-directory: ./internal/astgroup/parsers/src/braceparser, running go test ./... only. Do NOT include a wasm rebuild: wasm files are vendored/committed by design (build.sh:4-7 explicitly states this is not a CI step), and SHA256SUMS integrity is already validated by TestEmbeddedParsersMatchManifest in the root go test ./... run.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .github/workflows/ci.yml
- internal/astgroup/parsers/src/braceparser/go.mod
- go.mod
- internal/astgroup/parsers/build.sh
- internal/astgroup/embed_test.go
