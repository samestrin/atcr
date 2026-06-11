# gopkg.in/yaml.v3

**Version:** v3.0.1 (May 27, 2022 — long-term stable)
**Registry:** [pkg.go.dev/gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)
**Official Docs:** [github.com/go-yaml/yaml](https://github.com/go-yaml/yaml)
**Tier:** Important
**Last Updated:** June 10, 2026

---

## Overview

YAML encoding/decoding for Go, based on a pure-Go port of libyaml. Developed within Canonical for Juju; the de-facto standard YAML library in the Go ecosystem (34k+ importers). Licenses: Apache-2.0, MIT.

## Quick Start

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

## Key APIs

- `Marshal(in interface{}) ([]byte, error)` / `Unmarshal(in []byte, out interface{}) error` — whole-document conversion. Type mismatches return `*yaml.TypeError` with partial unmarshaling (other fields still populate).
- `NewDecoder(r io.Reader)` / `NewEncoder(w io.Writer)` — stream-based processing. The encoder requires `Close()` to flush; `SetIndent(n)` controls indentation.
- `Decoder.KnownFields(true)` — strict mode: unknown YAML keys become errors. **Use this for atcr config and registry parsing** to catch typos (`serial_agnets:`) at load time instead of silently ignoring them.
- `Node` — low-level document representation (Kind, Tag, Value, Content, Line, Column, comments). Only needed for surgical YAML edits; atcr v1 does whole-file marshal/unmarshal and does not need it.

## Struct Tags

Format: `` `yaml:"key,flag1,flag2"` ``

| Flag | Purpose |
|------|---------|
| `omitempty` | Exclude zero/empty values; honors `IsZero()` |
| `flow` | Compact flow style |
| `inline` | Embed struct/map fields into parent |
| `-` | Ignore field |

Only exported (uppercase) fields are marshaled. Custom types can implement `MarshalYAML()` / `UnmarshalYAML()`.

## Caveats

- YAML 1.1 booleans (`yes/no`, `on/off`) decode as bools into typed bool fields — relevant when users hand-edit `registry.yaml`.
- Multi-document `Unmarshal` is not supported (use `Decoder` for multi-doc streams).
- v3.0.1 has been stable since 2022; no active feature development, which is acceptable for config parsing.

## Integration Notes (atcr)

- Parses `~/.config/atcr/registry.yaml` (providers + agents) and `.atcr/config.yaml` (roster, payload mode, timeouts, fail-on) — Epic 1.0 task 2.
- Always decode with `KnownFields(true)` via `NewDecoder` so malformed configs fail fast, matching the load-time fallback-chain validation philosophy.
- `goccy/go-yaml` (used in the base repo for AST manipulation) is intentionally NOT needed here.

---
**Source:** Extracted from pkg.go.dev on June 10, 2026.
