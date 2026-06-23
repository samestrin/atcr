# Production-Readiness Review

**Date:** 2026-06-23
**Scope:** Full `atcr` codebase after Epic 7, before Epic 8 and remaining active epics.
**Axis:** component / subsystem (not chronological epics)
**Depth:** production hardening audit
**Dogfooding:** yes — run `atcr` against its own codebase
**Goals:** (a) works as expected (functional) · (b) production ready (structural)

## Context

- Go CLI, ~70k LOC, 156 source files, 191 test files, 89.6% baseline coverage, single `ci.yml`.
- Pipeline: git payload → parallel reviewer fan-out → tool-using reviewers → adversarial verification / debate → reconciliation → scorecard/report → GitHub Action surface.
- CI gates: `gofmt -l`, `golangci-lint v2.6.2`, `go test -race -coverprofile`.
- Build: `go build -o bin/atcr ./cmd/atcr`.

## Phase 0 — Baseline floor

Run and record before any deep review:

- `go build ./...`, `go vet ./...`
- `gofmt -l .` (must be empty), `golangci-lint run`
- `go test -race -coverprofile=coverage.out ./...` — capture per-package coverage, any `t.Skip`, race warnings, flaky reruns.

**Gate:** anything red here is P0 and blocks deeper work. 89.6% is the floor, not the verdict (coverage % ≠ test quality).

## Phase 1 — Functional verification (track a)

- Build `bin/atcr`; run `atcr doctor` (and `--json`).
- Real review run (epic 1.7 pattern): `atcr review` + `atcr reconcile` on a sample diff → valid scorecard/report.
- Exercise `internal/integration`.
- **Dogfood:** run atcr against its own recent diff/codebase; capture findings as input to Phase 2.
- GitHub Action smoke test per `docs/github-action.md`.

## Phase 2 — Structural audit, per subsystem cluster (track b)

Risk-ordered; each cluster gets the same checklist.

| # | Cluster | Packages |
|---|---------|----------|
| 1 | Core pipeline | `payload`, `fanout`, `reconcile`, `report`, `scorecard`, `stream` |
| 2 | Reasoning | `verify`, `debate`, `tools` |
| 3 | Providers/IO | `llmclient`, `mcp`, `registry`, `ghaction` |
| 4 | Resilience/infra | `circuitbreaker`, `cache`, `metrics`, `log`, `errors`, `atomicfs`, `atomicwrite`, `validation`, `gitrange`, `doctor` |
| 5 | Entry | `cmd/atcr` |

**Per-cluster checklist:**

- **Concurrency** — data races, goroutine leaks, bounded fan-out, context cancellation honored
- **Resources** — HTTP bodies/files closed, bounded buffers, no unbounded memory
- **Errors** — wrapped/propagated, none swallowed, correct exit codes (0/1/2)
- **Security** — secret redaction (4.9), input/config validation (4.2/4.3), markdown & Actions injection, path traversal (5.0/5.4)
- **Resilience** — timeouts, retries, circuit breaker, rate-limit backoff, graceful shutdown (4.1/4.5/4.6)
- **Observability** — structured logging, metrics, telemetry
- **API & test quality** — public surface, dead code, assertion strength (not just coverage)
- **AC conformance** — cross-check the cluster against its epics' acceptance criteria

## Phase 3 — Synthesis

- Consolidate into severity-ranked findings (P0/P1/P2).
- Route findings into `.planning/technical-debt/README.md` via the existing TD flow (`group_td`), plus a per-cluster production-readiness verdict.

## Success criteria (loop until verified)

1. Builds, vets, formats, lints clean; all tests pass under `-race`; zero unexplained `Skip`/flake.
2. End-to-end run + Action smoke test produce valid output; dogfood findings triaged.
3. Every cluster has a written verdict and findings logged.
4. Zero open P0; every P1 has an owner/plan.

## Execution

Multi-agent workflow: one auditor per subsystem cluster in parallel → adversarial verification of each finding → synthesis.
