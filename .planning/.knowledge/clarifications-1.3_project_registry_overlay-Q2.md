---
id: mem-2026-06-11-80ea96
question: "Which trust mitigation should be implemented for project-defined providers in the atcr registry overlay?"
created: 2026-06-11
last_retrieved: ""
sprints: []
files: [internal/reconcile/ambiguous.go, cmd/atcr/main.go, cmd/atcr/doctor.go, internal/registry/config.go, internal/llmclient/client.go, internal/registry/gate.go]
tags: [clarifications, epic-1.3_project_registry_overlay, architecture, security, trust]
retrievals: 0
status: active
type: clarifications
---

# Which trust mitigation should be implemented for project-def

## Decision

Option (d): explicit trust gate (a) combined with a loud first-use banner (c). Implement `atcr trust` as the hard gate — a missing trust entry is a usageError that blocks execution. The banner confirms active project providers to the contributor after the gate passes and is a UX aid, not the security control.

Justification:
- internal/reconcile/ambiguous.go:153-167 — HashBytes() and AmbiguousHash() produce sha256:<hex> digests already used in production. The same pattern supports hashing (base_url, api_key_env) tuples for the trust store — no new crypto dependency.
- cmd/atcr/main.go:100-104 and cmd/atcr/doctor.go — atcr trust fits the existing cobra pattern: thin cmd/atcr/trust.go delegating to internal/registry/trust.go, matching the doctor command structure added in Epic 1.2.
- internal/registry/config.go:114-134 (LoadRegistry) — the trust check inserts naturally in the overlay-merge step: after merging project providers, check each against the user-dir allow file before any invocation reaches llmclient.Complete. Missing entry returns usageError pointing at atcr trust.
- internal/llmclient/client.go:117 — Complete constructs the endpoint as strings.TrimRight(inv.BaseURL, "/") + "/chat/completions" and POSTs the key there. Option (b) does NOT pin base_url — a project could name OPENAI_API_KEY (a "trusted" env name) while directing requests to an attacker host. Only (a) or (d) satisfies the success criterion.
- internal/registry/gate.go:23-45 (ResolveGateThreshold) — demonstrates the pattern for reading optional user-dir files with graceful missing-file handling. The trust store (~/.config/atcr/trusted_providers.yaml) follows the same pattern.
- Implementation: SHA-256 over base_url + "\x00" + api_key_env (reuses HashBytes), one atcr trust interactive confirmation flow, one trust-file read at overlay-load time. No external dependencies, no interactive prompt during review runs after initial trust is granted.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/ambiguous.go
- cmd/atcr/main.go
- cmd/atcr/doctor.go
- internal/registry/config.go
- internal/llmclient/client.go
- internal/registry/gate.go
