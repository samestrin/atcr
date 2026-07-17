---
id: mem-2026-07-16-c8f087
question: "TD-007 HMAC pepper for HashPersonaID stays deferred until real cloud-sync endpoint ships"
created: 2026-07-16
last_retrieved: ""
sprints: []
files: [internal/scorecard/telemetry.go, cmd/atcr/flags.go, cmd/atcr/cloudsync.go]
tags: [clarifications, epic-28.0_telemetry_expansion_cloud_sync, architecture]
retrievals: 0
status: active
type: clarifications
---

# TD-007 HMAC pepper for HashPersonaID stays deferred until re

## Decision

Keep TD-007's HMAC hardening of HashPersonaID deferred — do not provision an application secret now. The --sync-cloud destination (defaultCloudEndpoint) is a compiled-in placeholder explicitly "not operational" until ATCR_API_KEY issuance is live, so there is no real backend to define secret ownership, rotation, or distribution for an HMAC pepper. Provisioning now would only churn the AC-pinned unsalted-SHA-256 digests without a corresponding consumer.

Justification:
- TD-007 comment states the deferral is intentional, tied to endpoint activation (internal/scorecard/telemetry.go:23-25).
- defaultCloudEndpoint is documented as a placeholder, "not operational until ATCR_API_KEY issuance is live" (cmd/atcr/flags.go:36-46).
- ATCR_API_KEY (cmd/atcr/cloudsync.go:42-46) is a bearer auth token, not designed as a shared application pepper for hashing — reusing it would conflate two different secret roles.
- No config.yaml secret field or build-time ldflags secret pattern exists elsewhere in the repo; ldflags only injects the non-secret version string (cmd/atcr/version.go:11).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/scorecard/telemetry.go
- cmd/atcr/flags.go
- cmd/atcr/cloudsync.go
