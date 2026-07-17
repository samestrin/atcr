---
id: mem-2026-07-16-3d81c4
question: "Telemetry client drain-before-exit deferred until defaultTelemetryEndpoint is real"
created: 2026-07-16
last_retrieved: ""
sprints: []
files: [cmd/atcr/main.go, internal/telemetry/client.go, cmd/atcr/telemetry.go]
tags: [clarifications, epic-28.0_telemetry_expansion_cloud_sync, architecture]
retrievals: 0
status: active
type: clarifications
---

# Telemetry client drain-before-exit deferred until defaultTel

## Decision

Accept dropped pings on process exit until the real telemetry endpoint lands — no code change needed to thread a drainable client handle through newRootCmd. defaultTelemetryEndpoint is currently "", making every Client.Send an unconditional no-op, so there is nothing in flight to lose today. Client.Wait() already exists for graceful-shutdown drain but its doc comment confirms production callers deliberately never call it. Revisit only once defaultTelemetryEndpoint points at a real backend and pings are actually in flight.

Justification:
- cmd/atcr/telemetry.go:19 sets defaultTelemetryEndpoint = ""; cmd/atcr/main.go:144-147 documents it as a no-op "in dev, CI, and production for now".
- internal/telemetry/client.go:100-113 (isHTTPS, Send) shows Send no-ops whenever the endpoint isn't a well-formed https:// URL — always true today.
- internal/telemetry/client.go:159-165 defines Wait(), documented as "intended for deterministic tests and graceful-shutdown drain; production callers fire-and-forget and never call it."
- cmd/atcr/main.go:143-145 documents the client as "constructed once here... deliberately not a package-level singleton" — changing newRootCmd's signature to return it would touch ~33 test call sites for no present benefit.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/main.go
- internal/telemetry/client.go
- cmd/atcr/telemetry.go
