---
id: mem-2026-07-17-cdcfc9
question: "registry config-setting duplication: where shared helpers live + when to bundle Load/Set extractions"
created: 2026-07-17
last_retrieved: ""
sprints: [30.0_community_prompt_quality_signal]
files: [internal/registry/telemetry_setting.go, internal/registry/quality_signal_setting.go]
tags: [td-clarification, resolve-td, scope-splitting, registry, refactor-convention]
retrievals: 0
status: active
type: td-clarification
---

# registry config-setting duplication: where shared helpers li

## Decision

In internal/registry, the shared config-persistence helpers (withConfigLock, configMapping, setMappingBool, syncDir, boolLiteral) all live in telemetry_setting.go even though quality_signal_setting.go depends on them — telemetry_setting.go is the established "home" for cross-setting registry helpers. When extracting a new shared helper for a duplicated Load/Set pair (e.g. loadConfigBool(root, key), setConfigBool(root, session, key, enabled)), add it to telemetry_setting.go too, and have every sibling setting file (quality_signal_setting.go, future ones) delegate to it.

When a TD review flags both the Load-path AND Set-path of the same duplicated pair (e.g. LoadQualitySignalSetting vs LoadTelemetrySetting, and SetQualitySignalSetting vs SetTelemetrySetting), bundle both extractions into one resolve-td pass rather than doing them separately — even though each alone already exceeds SAFE_SCOPE (multi-file). Rationale: they're the same refactor shape on the same file pair; doing only one leaves the pair in a self-inconsistent half-refactored state, and when the reviewer's stated risk is specifically "two copies of a security-sensitive path WILL drift" (as with the atomic Set-path write), leaving the higher-severity half unfixed defeats the point of the lower-severity Load-path fix.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/telemetry_setting.go
- internal/registry/quality_signal_setting.go
