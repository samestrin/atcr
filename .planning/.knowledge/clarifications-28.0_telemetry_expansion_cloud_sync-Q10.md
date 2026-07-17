---
id: mem-2026-07-16-ecfbea
question: "Don't thread pre-parsed cfg.Project.Telemetry into telemetryGate — reconcile.go has no cfg to reuse"
created: 2026-07-16
last_retrieved: ""
sprints: []
files: [cmd/atcr/telemetry.go, cmd/atcr/review.go, cmd/atcr/reconcile.go]
tags: [clarifications, epic-28.0_telemetry_expansion_cloud_sync, implementation]
retrievals: 0
status: active
type: clarifications
---

# Don't thread pre-parsed cfg.Project.Telemetry into telemetry

## Decision

Do not thread an already-parsed cfg.Project.Telemetry parameter into telemetryGate() to remove a redundant config read. The "already-parsed" premise only holds for the review.go call site (cfg comes from fanout.LoadReviewConfig's strict parse) — reconcile.go never loads a registry.ProjectConfig at all, so threading there would require ADDING a brand-new config load rather than removing one, producing net-negative churn (2 call sites + 6 tests) for a LOW-perf gain. Keep telemetryGate() as the single, self-contained entry point calling LoadTelemetrySetting directly, which already preserves the fail-safe-to-disabled behavior naturally.

Justification:
- review.go's call site loads cfg via fanout.LoadReviewConfig(".", ...) at cmd/atcr/review.go:299, returning early on error; telemetryGate() runs at review.go:420 with cfg already available.
- reconcile.go has NO equivalent full config load before telemetryGate() at cmd/atcr/reconcile.go:186; its only registry call is resolveGateThreshold (reconcile.go:333-341) via the narrower registry.ResolveGateThreshold, which never exposes .Telemetry.
- telemetryGate() is called from exactly 2 production sites (review.go:420, reconcile.go:186), referenced in 6 assertions in cmd/atcr/telemetry_gate_test.go:127-156.
- internal/registry/telemetry_setting.go:27-46 (LoadTelemetrySetting) returns (nil,nil) when absent, a wrapped error only on read failure or non-boolean value; telemetryGate() (cmd/atcr/telemetry.go:61-64) converts any such error to false — the fail-safe the epic's opt-out acceptance criterion depends on.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/telemetry.go
- cmd/atcr/review.go
- cmd/atcr/reconcile.go
