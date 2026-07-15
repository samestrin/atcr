# Schema-Validating SARIF Output with jsonschema-go

`[IMPORTANT]`

## Overview

Plan 25.0 adds `internal/report/sarif.go`'s `renderSarif` function, which must emit JSON that conforms exactly to the SARIF 2.1.0 specification. Hand-written assertions on individual fields cannot catch every way generated JSON can drift from that spec — a missing required property, a wrong enum value, or an incorrectly shaped nested object can all pass field-by-field checks while still failing a strict SARIF consumer (e.g. GitHub code scanning). Validating the test output against the official SARIF 2.1.0 JSON Schema in `internal/report/sarif_test.go` closes that gap deterministically: it either conforms or it does not.

`github.com/google/jsonschema-go` is already a dependency of this codebase (used in `internal/mcp/tools.go` to reflect Go structs into JSON Schema for MCP tool registration). That existing usage exercises only one direction of the package — Go struct → JSON Schema. The package is bidirectional, and the direction this plan needs is the other one: JSON Schema document → validator, for checking arbitrary JSON against a schema atcr does not own. Because that direction is already available in the same dependency, this plan can schema-validate `renderSarif`'s output without adding a second JSON-schema library to go.mod.

The validation flow has three steps: (1) parse the official SARIF 2.1.0 schema document with `Schema.UnmarshalJSON`, (2) resolve its internal `$ref`s with `Schema.Resolve`, producing a `*Resolved` validator, and (3) call `Resolved.Validate` on the decoded output of `renderSarif` to check conformance. Each step is covered in more detail below.

## Key Concepts

### `Schema.UnmarshalJSON`

Loads a `*jsonschema.Schema` from raw JSON Schema bytes (Draft-07 and Draft 2020-12 are both supported). This is the entry point for validating against a spec atcr did not generate itself — in this case, the official SARIF 2.1.0 schema document.

> Source: [.planning/specifications/packages/jsonschema-go.md:Validating JSON Against an External Schema]

### `Schema.Resolve(opts *ResolveOptions) (*Resolved, error)`

Resolves `$ref`s within the schema before it can validate anything. `BaseURI` anchors relative `$ref` resolution; `Loader` supplies external reference targets if the schema pulls in definitions from outside itself. This step is required — a `Schema` cannot validate directly; only the `*Resolved` value returned by `Resolve` can.

> Source: [.planning/specifications/packages/jsonschema-go.md:Validating JSON Against an External Schema]

### `Resolved.Validate(v any) error`

Validates a decoded JSON value against the resolved schema and returns a descriptive error on the first (or aggregated) violation.

**Important caveat:** `Validate` expects a value that looks like the result of unmarshaling JSON into `any` — `map[string]any`, `[]any`, and scalars — not a raw `[]byte` and not a typed Go struct. This means the `sarif_test.go` work must call `json.Unmarshal` on `renderSarif`'s own output into an `any` variable before passing it to `Validate`; passing the raw `[]byte` output or a typed `SarifLog`-style struct directly will not work.

> Source: [.planning/specifications/packages/jsonschema-go.md:Validating JSON Against an External Schema]

### Missing schema fixture — a task this plan must add

This repository does not currently have a copy of the official SARIF 2.1.0 JSON Schema document anywhere in the tree (confirmed against the package's Integration Notes: "The `Resolve`/`Validate` path ... is not yet used anywhere in atcr as of this writing"). The `sarif_test.go` work therefore needs to fetch or vendor that schema as a local fixture — for example `testdata/sarif-schema-2.1.0.json` — and load it via `os.ReadFile` (or equivalent) before calling `json.Unmarshal(schemaBytes, &schema)`. Without this fixture, step 1 of the validation flow has nothing to parse.

> Source: [.planning/specifications/packages/jsonschema-go.md:Integration Notes (atcr)]

## Code Examples

Reproduced verbatim from the source documentation's "Validating JSON Against an External Schema" section:

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

> Source: [.planning/specifications/packages/jsonschema-go.md:Validating JSON Against an External Schema]

## Quick Reference

| API | Signature | Purpose |
|-----|-----------|---------|
| `Schema.UnmarshalJSON` | `(s *Schema) UnmarshalJSON(data []byte) error` | Parses raw JSON Schema bytes (Draft-07 or Draft 2020-12) into a `*jsonschema.Schema` |
| `Schema.Resolve` | `(s *Schema) Resolve(opts *ResolveOptions) (*Resolved, error)` | Resolves `$ref`s within the schema, producing a validator; required before `Validate` |
| `Resolved.Validate` | `(r *Resolved) Validate(v any) error` | Validates a decoded JSON value (`any`/`map[string]any`/`[]any`) against the resolved schema |
| `Resolved.ApplyDefaults` | `(r *Resolved) ApplyDefaults(v any) error` | Optional: fills in schema-defined defaults on the instance |

## Related Documentation

- [SARIF Schema Reference](./sarif-schema-reference.md)
- [GitHub Code Scanning Integration](./github-code-scanning-integration.md)
- [JSON Encoding Conventions](./json-encoding-conventions.md)
- [Source: jsonschema-go package documentation](../../../../specifications/packages/jsonschema-go.md)
