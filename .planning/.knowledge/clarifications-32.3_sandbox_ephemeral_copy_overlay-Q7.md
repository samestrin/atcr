---
id: mem-2026-07-21-21b689
question: "Can a daemon-free fake-shim unit test assert a real kernel error like EROFS?"
created: 2026-07-21
last_retrieved: ""
sprints: [32.3_sandbox_ephemeral_copy_overlay]
files: [internal/sandbox/docker_test.go]
tags: [clarifications, sprint-32.3_sandbox_ephemeral_copy_overlay, testing, docker, test-strategy]
retrievals: 0
status: active
type: clarifications
---

# Can a daemon-free fake-shim unit test assert a real kernel e

## Decision

No. A fake-shim test (a shell script standing in for the `docker` CLI, with no real container or daemon) can only assert what the code under test CONSTRUCTS — e.g. that the correct mount flags (`:ro`, `--tmpfs`) appear in the invocation argv. It cannot assert what the Docker daemon or kernel would actually ENFORCE at runtime (e.g. that a write against a `:ro`-mounted path really fails with `EROFS`), because there is no real bind mount or kernel-level mount semantics in a fake-shim test — that requires a real Docker daemon and a genuinely gated integration-test tier (e.g. behind a `//go:build integration` tag), which many daemon-free test suites don't have. When reviewing a test-quality clarification that asks for a stronger runtime-behavior assertion (EROFS, permission denial, etc.) inside an existing daemon-free unit test, the correct answer is usually: the existing argv-construction check is already the correct and sufficient daemon-free proxy; a true runtime-behavior proof requires a new, additive integration-test tier, not a rewrite of the existing unit test.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/sandbox/docker_test.go
