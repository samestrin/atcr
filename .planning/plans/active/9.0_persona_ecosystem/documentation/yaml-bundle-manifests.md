# YAML Bundle Manifests ![CRITICAL](https://img.shields.io/badge/priority-CRITICAL-red)

## Overview

Bundle manifests are YAML files that group related personas into a named domain panel, enabling `atcr personas install bundle/<name>` to install a curated set of reviewers in one command. Each manifest lives under `bundles/` and declares a name, description, and an ordered list of persona references. The format is intentionally minimal so that hand-edited files remain readable and typos surface at load time rather than silently producing incomplete installs.

Parsing uses `gopkg.in/yaml.v3` — the de-facto standard YAML library in the Go ecosystem with 34k+ importers. The decoder is always constructed via `yaml.NewDecoder` with `KnownFields(true)` (strict mode) so unexpected keys such as `personnas:` or `serial_agnets:` are hard errors at load time, consistent with the load-time fallback-chain validation philosophy already in place for `registry.yaml` and `.atcr/config.yaml`.

The new `internal/personas/bundles.go` file owns manifest parsing and install-path resolution; `internal/personas/bundles_test.go` covers round-trip parsing and `bundle/` prefix routing. No changes to the public CLI surface are required beyond wiring the `bundle/<name>` path in the existing `install` command dispatch.

> Source: [codebase-discovery.json / File to create: internal/personas/bundles.go]

## Key Concepts

### Bundle Manifest Format

A bundle manifest is a plain YAML file with four top-level fields. The `personas` list contains slash-namespaced references matching entries already registered in `~/.config/atcr/registry.yaml`.

> Source: [codebase-discovery.json / Epic plan domain bundle YAML example]

### KnownFields Strict Mode

`Decoder.KnownFields(true)` turns unknown YAML keys into hard errors. This is the required decoding mode for all atcr config and registry parsing so that typos fail fast.

> Source: [yaml-v3.md / Key APIs — Decoder.KnownFields(true)]

### AgentConfig.Language Field (T8)

`Language []string` is a new optional field on `AgentConfig` in `internal/registry/config.go`. It follows the same pattern as the existing `Scope []string` field: nil slice means no constraint, backward-compatible with 1.x configs. Canonical form is without a leading dot, lowercased (e.g. `["go","ts"]`). `applyDefaults` normalizes each entry (trim space, strip one leading dot, lowercase). `validateAgent` rejects empty entries and control characters, mirroring the `Scope` guard. File-level matching uses `normalizeExt(ext) = strings.ToLower(strings.TrimPrefix(ext, "."))` applied to `filepath.Ext(finding.File)`.

> Source: [codebase-discovery.json / Architecture note (DECIDED 2026-06-24)]

> Source: [codebase-discovery.json / Integration gap: internal/registry/config.go:AgentConfig]

### Struct Tags and omitempty

All optional fields use the `yaml:"field,omitempty"` tag so that zero/empty values are omitted when marshaling, keeping round-tripped config files clean. Only exported (uppercase) fields are marshaled.

> Source: [yaml-v3.md / Struct Tags]

### Backward Compatibility

A 1.x config missing the `language:` key parses cleanly because the zero value of `[]string` is nil. This matches the existing `Scope []string` field pattern, which is the canonical model for all new optional `AgentConfig` fields.

> Source: [codebase-discovery.json / Pattern "Optional AgentConfig field"]

## Code Examples

### Bundle Manifest YAML

```yaml
# bundles/django.yaml
name: django
description: Django application review panel
personas:
  - framework/django-orm
  - language/python-types
  - security/owasp
  - security/secrets
```

> Source: [codebase-discovery.json / Epic plan domain bundle YAML example]

### Decoder Construction (strict mode)

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

> Source: [yaml-v3.md / Quick Start]

### AgentConfig Language Field Declaration

```go
Language []string `yaml:"language,omitempty"`
```

> Source: [codebase-discovery.json / Reusable component "AgentConfig optional field pattern"]

## Quick Reference

| Item | Detail |
|------|--------|
| Bundle manifest location | `bundles/<name>.yaml` |
| Install command | `atcr personas install bundle/<name>` |
| New Go file (parsing) | `internal/personas/bundles.go` |
| New Go file (tests) | `internal/personas/bundles_test.go` |
| YAML library | `gopkg.in/yaml.v3` v3.0.1 |
| Decoder mode | `KnownFields(true)` — strict, unknown keys are errors |
| AgentConfig new field | `Language []string \`yaml:"language,omitempty"\`` |
| Canonical form | No leading dot, lowercased (`go`, `ts`, not `.Go`) |
| Nil slice semantics | No language constraint — backward-compatible |
| Normalization site | `applyDefaults` (~line 699, `internal/registry/config.go`) |
| Validation site | `validateAgent` (~line 625, `internal/registry/config.go`) |
| AgentConfig field location | `internal/registry/config.go:267` |
| Matching function | `normalizeExt(ext) = strings.ToLower(strings.TrimPrefix(ext, "."))` |

## Related Documentation

- `gopkg.in/yaml.v3` — [pkg.go.dev/gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)
- go-yaml source and issue tracker — [github.com/go-yaml/yaml](https://github.com/go-yaml/yaml)
- `internal/registry/config.go` — AgentConfig definition, `validateAgent`, `applyDefaults`
- `internal/personas/bundles.go` — bundle manifest parsing and `bundle/` install resolution (T5)
- `internal/personas/bundles_test.go` — round-trip parsing and routing tests
- Sprint 9.0 plan — `.planning/plans/active/9.0_persona_ecosystem/`
