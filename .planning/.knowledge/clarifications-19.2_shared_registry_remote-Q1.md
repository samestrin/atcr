---
id: mem-2026-07-05-f90d67
question: "Remote registry.yaml secret handling — strict-decode already fails closed on literal api_key"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [internal/registry/config.go, internal/registry/decode.go, internal/registry/overlay.go, internal/registry/config_test.go]
tags: [clarifications, epic-19.2_shared_registry_remote, implementation, registry, secrets]
retrievals: 0
status: active
type: clarifications
---

# Remote registry.yaml secret handling — strict-decode alrea

## Decision

A registry.yaml Provider entry has no `api_key` field (only `api_key_env`, a name — the value is resolved from local env at invoke time, never at load, never from the file). Registry YAML decoding uses KnownFields(true) strict decoding, so any unrecognized field like a literal `api_key: sk-...` is already a hard load error today, not something that needs a special pre-decode scan-and-warn step. When a plan/AC says a literal secret in a config file should be "ignored with a warning," check whether strict-decode already makes that field an unreachable hard-error path before building new detection logic for it.

Justification:
- internal/registry/config.go:50-53 — Provider struct defines only APIKeyEnv (yaml:"api_key_env") and BaseURL; no api_key field exists to populate even if present in the file.
- internal/registry/decode.go:87-88 — decodeStrictYAML builds the YAML decoder with dec.KnownFields(true), so any unrecognized key causes a hard decode error rather than being silently dropped.
- internal/registry/overlay.go (parseRegistryFile, LoadProjectRegistry) both route through decodeStrictYAML and surface decode failures as load errors.
- internal/registry/config_test.go (TestRegistryLoad_APIKeyNotReadAtLoad) confirms only api_key_env (a name) travels through the registry; the value is never read from the file itself.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/config.go
- internal/registry/decode.go
- internal/registry/overlay.go
- internal/registry/config_test.go
