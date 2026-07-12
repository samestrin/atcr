# Config YAML Parsing (gopkg.in/yaml.v3)

**Priority: Important**

## Overview

`gopkg.in/yaml.v3` (v3.0.1, May 27, 2022 — long-term stable) is the YAML encoding/decoding library for Go, based on a pure-Go port of libyaml. Developed within Canonical for Juju, it is the de-facto standard YAML library in the Go ecosystem with 34k+ importers, licensed under Apache-2.0 and MIT.

> Source: .planning/specifications/packages/yaml-v3.md:Overview

The library has been stable since 2022 with no active feature development, which the source notes is "acceptable for config parsing." atcr uses it to parse `~/.config/atcr/registry.yaml` (providers + agents) and `.atcr/config.yaml` (roster, payload mode, timeouts, fail-on).

> Source: .planning/specifications/packages/yaml-v3.md:Integration Notes (atcr)

For this plan, the reviewer-payload-sizing work adds two new config keys — `max_sprint_plan_bytes` (F9) and `on_overflow` (F4) — to the same YAML config surface that yaml.v3 already parses, so these keys inherit the library's struct-tag conventions and strict-decoding behavior described below.

## Key Concepts

- **Strict decoding via `KnownFields(true)`**: unknown YAML keys become errors instead of being silently ignored. The source explicitly calls out: "**Use this for atcr config and registry parsing** to catch typos (`serial_agnets:`) at load time instead of silently ignoring them."

> Source: .planning/specifications/packages/yaml-v3.md:Key APIs

- **Whole-document Marshal/Unmarshal**: `Marshal(in interface{}) ([]byte, error)` / `Unmarshal(in []byte, out interface{}) error` perform whole-document conversion; type mismatches return `*yaml.TypeError` with partial unmarshaling (other fields still populate). `NewDecoder(r io.Reader)` / `NewEncoder(w io.Writer)` support stream-based processing (encoder requires `Close()` to flush; `SetIndent(n)` controls indentation).

> Source: .planning/specifications/packages/yaml-v3.md:Key APIs

- **Node-level editing is not needed**: the low-level `Node` type (Kind, Tag, Value, Content, Line, Column, comments) is "only needed for surgical YAML edits; atcr v1 does whole-file marshal/unmarshal and does not need it." This confirms F9/F4 should be added as plain struct fields decoded via whole-file `Unmarshal`/`Decoder`, not via `Node` manipulation.

> Source: .planning/specifications/packages/yaml-v3.md:Key APIs

- **Struct tag format**: `` `yaml:"key,flag1,flag2"` ``, with flags `omitempty` (exclude zero/empty values, honors `IsZero()`), `flow` (compact flow style), `inline` (embed struct/map fields into parent), and `-` (ignore field). Only exported (uppercase) fields are marshaled; custom types can implement `MarshalYAML()` / `UnmarshalYAML()`.

> Source: .planning/specifications/packages/yaml-v3.md:Struct Tags

- **YAML 1.1 boolean caveat**: `yes/no`, `on/off` decode as bools into typed bool fields — relevant when users hand-edit `registry.yaml`. Multi-document `Unmarshal` is not supported (use `Decoder` for multi-doc streams).

> Source: .planning/specifications/packages/yaml-v3.md:Caveats

- **Codebase pointer + Effective\*() convention**: config fields that must distinguish "not set, inherit default" from "explicitly set to zero/default value" use a pointer (e.g. `AgentConfig.MaxContextLines *int`, `TimeoutSecs *int`) with an `Effective*()` method resolving the precedence chain, in `internal/registry/config.go`. This plan's new `max_sprint_plan_bytes` (F9) must follow this exact pointer + `Effective*()` resolver pattern already established for `PayloadByteBudget`/`TimeoutSecs`; `on_overflow` (F4) is instead a plain string policy key with a default, not a pointer.

> Source: codebase-discovery.json:Pointer-for-unset-vs-explicit-zero

- **Existing resolvers and validation**: `internal/registry/config.go` defines `DefaultTimeoutSecs`, `DefaultMaxContextLines`, `AgentConfig` (Model, MaxContextLines, TimeoutSecs), `EffectiveTimeoutSecs`/`EffectiveMaxContextLines` resolvers, and `Settings` validation — the pattern F9's resolver should extend.

> Source: codebase-discovery.json:internal/registry/config.go

- **Precedence chain integration**: `internal/registry/precedence.go` (`ResolveSettings`) is where `PayloadByteBudget` and other project-level settings get their embedded-tier defaults applied; `max_sprint_plan_bytes` and `on_overflow` must be threaded through this same precedence chain.

> Source: codebase-discovery.json:internal/registry/precedence.go

- **Scaffold generation**: `internal/registry/project.go`'s `DefaultProjectConfigYAML` renders the `.atcr/config.yaml` scaffold that `atcr init` writes, and needs updating to surface the new `max_sprint_plan_bytes` and `on_overflow` defaults/comments.

> Source: codebase-discovery.json:internal/registry/project.go

- **Files touched by this plan involving YAML config**: `internal/registry/config.go` (add `max_sprint_plan_bytes` to Settings/ProjectConfig/Registry + `Effective*()` resolver, add `on_overflow` policy key), `internal/registry/precedence.go` (thread both through `ResolveSettings`), `.atcr/config.yaml` (document/default both new keys), `internal/registry/project.go` (update `DefaultProjectConfigYAML` scaffold comment block).

> Source: codebase-discovery.json:files_to_modify

## Configurable Sprint-Plan Limit (F9)

F9 replaces the hardcoded `MaxSprintPlanBytes` constant (`16384` in `internal/payload/sprintplan.go:20`) with a configurable `max_sprint_plan_bytes` key in `.atcr/config.yaml`, defaulting to `65536` (64 KiB).

> Source: [original-requirements.md](../original-requirements.md):Requirements:F9, codebase-discovery.json:semantic_matches:MaxSprintPlanBytes

- **Pointer + `Effective*()` resolver.** `max_sprint_plan_bytes` must follow the same pointer + `Effective*()` resolver pattern as `PayloadByteBudget` and `TimeoutSecs`, because operators need to distinguish "not set, inherit default" from "explicitly set to zero/default value."

  > Source: codebase-discovery.json:existing_patterns:Pointer-for-unset-vs-explicit-zero

- **Thread through `ResolveSettings`.** The resolved value is applied in the precedence chain at `internal/registry/precedence.go` before `internal/payload/sprintplan.go` consumes it.

  > Source: codebase-discovery.json:files_to_modify:internal/registry/precedence.go

- **Parameterize `ReadSprintPlan` / `ScopeConstraint`.** `internal/payload/sprintplan.go` currently reads the package-level `MaxSprintPlanBytes` constant directly. F9 changes the call sites to pass the resolved config value as a parameter.

  > Source: codebase-discovery.json:files_to_modify:internal/payload/sprintplan.go

- **Default and documentation.** The new default (65536/64 KiB) and the `on_overflow` key must be documented in `.atcr/config.yaml` and in the `atcr init` scaffold produced by `internal/registry/project.go`.

  > Source: codebase-discovery.json:files_to_modify:.atcr/config.yaml, codebase-discovery.json:files_to_modify:internal/registry/project.go

## Code Examples

```bash
go get gopkg.in/yaml.v3
```

```go
import "gopkg.in/yaml.v3"

type Config struct {
    Agents       []string `yaml:"agents"`
    SerialAgents []string `yaml:"serial_agents,omitempty"`
    TimeoutSecs  int      `yaml:"timeout_seconds,omitempty"`
}

var cfg Config
if err := yaml.Unmarshal(data, &cfg); err != nil { ... }
out, err := yaml.Marshal(cfg)
```

> Source: .planning/specifications/packages/yaml-v3.md:Quick Start

## Quick Reference

| Item | Detail | Source |
|------|--------|--------|
| Version | v3.0.1 (May 27, 2022, long-term stable) | .planning/specifications/packages/yaml-v3.md:header |
| Strict mode | `Decoder.KnownFields(true)` — unknown keys error, catches typos like `serial_agnets:` | .planning/specifications/packages/yaml-v3.md:Key APIs |
| Whole-doc API | `Marshal`/`Unmarshal`; `*yaml.TypeError` on mismatch, partial unmarshal continues | .planning/specifications/packages/yaml-v3.md:Key APIs |
| Node-level editing | Not needed — atcr does whole-file marshal/unmarshal | .planning/specifications/packages/yaml-v3.md:Key APIs |
| Struct tag flags | `omitempty`, `flow`, `inline`, `-` | .planning/specifications/packages/yaml-v3.md:Struct Tags |
| Boolean caveat | YAML 1.1 `yes/no`/`on/off` decode as bool | .planning/specifications/packages/yaml-v3.md:Caveats |
| F9 pattern | `max_sprint_plan_bytes` as pointer field + `Effective*()` resolver, mirroring `PayloadByteBudget`/`TimeoutSecs` | codebase-discovery.json:Pointer-for-unset-vs-explicit-zero |
| F4 pattern | `on_overflow` as plain string policy key with a default (not a pointer) | codebase-discovery.json:Pointer-for-unset-vs-explicit-zero |
| Precedence wiring | Both keys threaded through `ResolveSettings` in internal/registry/precedence.go | codebase-discovery.json:internal/registry/precedence.go |
| Scaffold update | `DefaultProjectConfigYAML` in internal/registry/project.go needs new defaults/comments | codebase-discovery.json:internal/registry/project.go |
| F9 default | `max_sprint_plan_bytes` defaults to 65536 (64 KiB), replacing the 16384 constant | [original-requirements.md](../original-requirements.md):Requirements:F9 |
| F9 pattern | Pointer field + `EffectiveMaxSprintPlanBytes()` resolver, mirroring `PayloadByteBudget` | codebase-discovery.json:existing_patterns:Pointer-for-unset-vs-explicit-zero |

## Related Documentation

- `.planning/specifications/packages/yaml-v3.md` — full source specification for gopkg.in/yaml.v3
- `internal/registry/config.go` — `Settings`, `AgentConfig`, `EffectiveTimeoutSecs`, `EffectiveMaxContextLines`
- `internal/registry/precedence.go` — `ResolveSettings` precedence chain
- `internal/registry/project.go` — `DefaultProjectConfigYAML` scaffold generator
- `codebase-discovery.json` — plan-local codebase discovery findings
