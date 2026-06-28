---
id: mem-2026-06-27-8a9bf0
question: "Should a CI job rebuild go.wasm/python.wasm in CI to verify they match committed binaries, or is a Go-side checksum test the right approach?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/astgroup/embed.go, internal/astgroup/parsers/build.sh, .github/workflows/ci.yml]
tags: [clarifications, epic-13.1_ast_plugin_architecture, CI, wasm, checksum, scope, embed]
retrievals: 0
status: active
type: clarifications
---

# Should a CI job rebuild go.wasm/python.wasm in CI to verify 

## Decision

A CI rebuild-and-diff job is out of scope for this project. The epic Clarifications explicitly exclude "a CI Wasm build toolchain," and internal/astgroup/parsers/build.sh:4-6 confirms the script is "NOT part of the normal `go build` / CI path." Running `GOOS=wasip1 GOARCH=wasm go build` in CI contradicts this constraint.

The correct gate is a Go-side embedded-vs-manifest checksum test (no Wasm toolchain required):
1. Developer runs build.sh locally, generates SHA256SUMS via `sha256sum parsers/go.wasm parsers/python.wasm > parsers/SHA256SUMS`, commits all three.
2. A Go test in internal/astgroup/ reads embedded bytes from parserFS, recomputes SHA256, compares against the committed SHA256SUMS file.
3. The existing ci.yml:47 `go test -v -race ./...` step catches any mismatch on every PR. No new .github/workflows/ file is needed.
4. The CI runner is self-hosted (gauntlet); installing a Wasm toolchain would be an infrastructure change the epic avoids.

No SHA256SUMS file currently exists under internal/astgroup/parsers/ — it must be created locally and committed with the .wasm binaries.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/astgroup/embed.go
- internal/astgroup/parsers/build.sh
- .github/workflows/ci.yml
