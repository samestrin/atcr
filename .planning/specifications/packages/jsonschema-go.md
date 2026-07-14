# github.com/google/jsonschema-go

**Version:** v0.4.3
**Registry:** [pkg.go.dev/github.com/google/jsonschema-go](https://pkg.go.dev/github.com/google/jsonschema-go)
**Tier:** Important
**Last Updated:** 2026-07-14

---

## Overview

`jsonschema-go` is a Go library for generating JSON Schema from Go struct types using reflection and struct tags. It is used by atcr's MCP server layer to derive input/output schemas for tool registration automatically from typed argument structs.

The package is bidirectional: it can also parse an *externally-authored* JSON Schema document and validate arbitrary JSON values against it — the reflection path above is only one direction. See [Validating JSON Against an External Schema](#validating-json-against-an-external-schema) below.

## Installation

```bash
go get github.com/google/jsonschema-go
```

## Core API

```go
import "github.com/google/jsonschema-go/jsonschema"

type ReviewArgs struct {
    ID   string `json:"id,omitempty"   jsonschema:"review id; defaults to a generated id"`
    Base string `json:"base,omitempty" jsonschema:"base git ref"`
    Head string `json:"head,omitempty" jsonschema:"head git ref"`
}

reflector := jsonschema.Reflector{}
schema, err := reflector.Reflect(ReviewArgs{})
```

- `Reflector.Reflect(v)` — produces a `*jsonschema.Schema` from the type of `v` using `json` and `jsonschema` struct tags.
- `jsonschema:"description text"` — single-field tag sets the property description.
- `json:"name,omitempty"` — controls property name and whether the field is required.

## Struct Tags

| Tag | Example | Effect |
|-----|---------|--------|
| `json:"name"` | `json:"id"` | Sets the JSON property name |
| `json:"name,omitempty"` | `json:"id,omitempty"` | Property is optional (not in `required`) |
| `jsonschema:"desc"` | `jsonschema:"review id"` | Sets the property `description` |

## Validating JSON Against an External Schema

Unlike the reflection path above (Go struct → schema), this direction goes JSON Schema document → validator, for checking that some arbitrary JSON output conforms to a schema atcr does not own (e.g. a third-party wire format spec).

```go
import (
    "encoding/json"
    "github.com/google/jsonschema-go/jsonschema"
)

// 1. Parse the external schema document. *jsonschema.Schema implements
// UnmarshalJSON, so a standard schema file loads with encoding/json.
var schema jsonschema.Schema
if err := json.Unmarshal(schemaBytes, &schema); err != nil {
    // handle error
}

// 2. Resolve references (required before Validate).
resolved, err := schema.Resolve(&jsonschema.ResolveOptions{
    BaseURI: "https://example.com/schemas/", // for relative $ref resolution
    Loader:  myLoader,                       // optional: fetches external $ref targets
})
if err != nil {
    // handle error
}

// 3. Validate a JSON value. The value must look like the result of
// unmarshaling JSON into `any` (map[string]any / []any / scalars), not a
// typed struct — unmarshal into `any` first if starting from raw bytes.
var data any
json.Unmarshal(jsonBytes, &data)
if err := resolved.Validate(data); err != nil {
    // data does not conform to schema
}
```

- `Schema.UnmarshalJSON` — loads a `*jsonschema.Schema` from raw JSON Schema bytes (Draft-07 and Draft 2020-12 supported); this is the entry point for validating against a spec atcr did not generate itself.
- `Schema.Resolve(opts *ResolveOptions) (*Resolved, error)` — resolves `$ref`s within the schema before it can validate; `Loader` supplies external references, `BaseURI` anchors relative refs.
- `Resolved.Validate(v any) error` — validates a decoded JSON value (`any`/`map[string]any`/`[]any`, not a raw `[]byte` and not necessarily a typed struct) against the resolved schema; returns a descriptive error on the first (or aggregated) violation.
- `Resolved.ApplyDefaults(v any) error` — optional: fills in schema-defined defaults on the instance.

## Integration Notes (atcr)

- Used in `internal/mcp/tools.go` to build the `atcr_report` input schema and to derive schemas for other MCP tool arguments automatically via the `mcp.AddTool` generic helper.
- The MCP SDK's `mcp.AddTool` generic form internally calls schema inference; `jsonschema-go` provides the underlying reflection engine.
- atcr uses `jsonschema.Reflector{}` with default settings — no `AllowAdditionalProperties` override is applied at the tool layer.
- The `Resolve`/`Validate` path (above) is not yet used anywhere in atcr as of this writing; it is documented here because Plan 25.0 (SARIF Output Integration) intends to use it in `internal/report/sarif_test.go` to schema-check `renderSarif`'s output against the official SARIF 2.1.0 schema, avoiding a second JSON-schema dependency.

---
**Source:** go.mod v0.4.3; usage confirmed in internal/mcp/tools.go. Validation API confirmed via pkg.go.dev/github.com/google/jsonschema-go/jsonschema (not yet exercised in-repo).
