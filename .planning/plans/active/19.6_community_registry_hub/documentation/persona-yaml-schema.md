# Persona YAML Schema & Struct Tags [IMPORTANT]

## Overview

Persona YAML files in atcr already follow a documented authoring contract: `provider` and `model` are required keys, validated strictly against the registry-agent schema, while `name`, `version`, and `description` are optional catalog-only keys the registry loader ignores (docs/personas-authoring.md, personas/_base.md). This plan extends that contract by promoting `provider`/`model` into structured, indexable metadata and adding `Tasks`/`Tags` fields so a user can discover a persona by the model they already have. Because YAML parsing in Go is field-driven, this is fundamentally a struct-tag exercise: each new key needs a corresponding `yaml:"..."` tag on the decode target, and the decode path needs to decide how strictly it treats unrecognized or missing keys.

`gopkg.in/yaml.v3` is the de-facto standard YAML library for Go (34k+ importers) and is the natural fit for this work, since it is already the library atcr's config parsing is built on (`.planning/specifications/packages/yaml-v3.md`). It offers whole-document `Marshal`/`Unmarshal` for simple cases and a `Decoder` with `KnownFields(true)` for strict validation — the latter is exactly the mechanism recommended for atcr's registry and config parsing "to catch typos... at load time instead of silently ignoring them." Extending `PersonaIndexEntry` (internal/personas/search.go) with `Provider`, `Model`, `Tasks`, and `Tags` fields is additive: encoding/json ignores unknown fields on decode by default, and the equivalent yaml.v3 behavior (absent `KnownFields(true)`) is the same — old index entries without these fields simply decode with zero values, and new fields decode cleanly into existing consumers without a breaking migration.

The struct-tag design also has to reconcile two decode paths that behave differently on strictness: the persona YAML loader (which must strictly validate `provider`/`model` per the authoring contract) and the `PersonaIndexEntry` JSON/YAML struct (which should stay permissive so index files can gain fields over time without breaking older readers). yaml.v3's per-decoder `KnownFields(true)` setting — rather than a global parser option — is what makes this split possible: the persona loader can opt into strict mode while the index reader stays in default, permissive mode.

## Key Concepts

- **Struct tag format `` `yaml:"key,flag1,flag2"` `` drives every field mapping.** Only exported (uppercase) Go fields are marshaled/unmarshaled, and the tag's first segment sets the YAML key name (e.g. `Provider string `yaml:"provider"``, `Model string `yaml:"model"``). This is the mechanism for adding `Provider`/`Model`/`Tasks`/`Tags` to `PersonaIndexEntry` alongside its existing `Name`/`Version`/`Description`/`Path` fields.
  > Source: [yaml-v3.md]

- **`omitempty` excludes zero/empty values and honors `IsZero()`.** Optional catalog-only keys such as `name`, `version`, `description` — and the new optional `Tags` field — should carry `omitempty` so index entries that don't set them don't serialize empty strings/slices into index.json.
  > Source: [yaml-v3.md]

- **`flow` produces compact flow style and `inline` embeds struct/map fields into the parent.** Relevant if `Tasks`/`Tags` are modeled as a nested struct rather than a flat list, or if the index format wants a compact single-line representation for a list-valued field.
  > Source: [yaml-v3.md]

- **`-` ignores a field entirely**, useful for any internal-only bookkeeping field on `PersonaIndexEntry` that must never round-trip through YAML/JSON.
  > Source: [yaml-v3.md]

- **`Decoder.KnownFields(true)` enables strict mode: unknown YAML keys become decode errors.** This is the mechanism that should back the required-field validation described in docs/personas-authoring.md — provider+model are REQUIRED and validated strictly against the registry agent schema. Applying `KnownFields(true)` on the persona-loading decode path catches typo'd keys (e.g. `providr:`) at load time rather than silently dropping them, matching atcr's existing load-time fallback-chain validation philosophy for config parsing.
  > Source: [yaml-v3.md]

- **Type mismatches return a `*yaml.TypeError` with partial unmarshaling** — other fields still populate even when one field's type is wrong. This matters for persona YAML parsing since a malformed `model` value should not prevent the rest of the fields (used for index construction) from being read.
  > Source: [yaml-v3.md]

- **Custom types can implement `MarshalYAML()`/`UnmarshalYAML()`.** This is the extension point if `Tasks` or `Tags` ever need custom encode/decode logic beyond a plain slice/string mapping.
  > Source: [yaml-v3.md]

- **PersonaIndexEntry's additive extension mirrors encoding/json's unknown-field tolerance.** Just as encoding/json ignores unknown fields on decode by default, adding `Provider`/`Model`/`Tasks`/`Tags` to `PersonaIndexEntry` (internal/personas/search.go) is additive as long as the index decode path does not enable strict/`KnownFields(true)` mode — keeping the persona-YAML loader strict (per the authoring contract) while keeping the index-entry decoder permissive is what allows both requirements to coexist.
  > Source: [yaml-v3.md], codebase pattern (internal/personas/search.go)

## Code Examples

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
> Source: [yaml-v3.md] — illustrates the `yaml:"key,omitempty"` struct-tag pattern to apply when adding `Provider`/`Model`/`Tasks`/`Tags` fields to `PersonaIndexEntry`.

## Quick Reference

| Flag | Purpose |
|------|---------|
| `omitempty` | Exclude zero/empty values; honors `IsZero()` |
| `flow` | Compact flow style |
| `inline` | Embed struct/map fields into parent |
| `-` | Ignore field |

> Source: [yaml-v3.md]

## Related Documentation

- [gopkg.in/yaml.v3 package documentation](../../../../specifications/packages/yaml-v3.md)
