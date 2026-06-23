---
id: mem-2026-06-22-f37865
question: "How should AC5 (real-PR integration test) be satisfied in Epic 7.3?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [.github/workflows/ci.yml, internal/integration/logging_test.go, internal/verify/verify_e2e_test.go]
tags: [clarifications, epic-7.3_github_action_pr_integration, testing, integration-test, github-action]
retrievals: 0
status: active
type: clarifications
---

# How should AC5 (real-PR integration test) be satisfied in Ep

## Decision

Satisfy AC5 with: (1) a committed example workflow (`on: pull_request`), (2) Go tests against a `net/http/httptest`-backed fake GitHub API server, and (3) a documented manual smoke-test procedure. The project's established integration/e2e test pattern uses `net/http/httptest` — never live network calls (see `internal/integration/logging_test.go`, `internal/verify/verify_e2e_test.go`). The CI runner only has `contents: read` permission (`.github/workflows/ci.yml:9`), making live PR posting structurally impossible without new infrastructure. AC5's "demonstrates the end-to-end flow" language supports a mocked demonstration.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .github/workflows/ci.yml
- internal/integration/logging_test.go
- internal/verify/verify_e2e_test.go
