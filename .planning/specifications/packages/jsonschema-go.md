# github.com/google/jsonschema-go

**Version:** v0.4.3
**Registry:** [pkg.go.dev/github.com/google/jsonschema-go](https://pkg.go.dev/github.com/google/jsonschema-go)
**Tier:** Important
**Last Updated:** 2026-06-14

---

## Overview

`jsonschema-go` is a Go library for generating JSON Schema from Go struct types using reflection and struct tags. It is used by atcr's MCP server layer to derive input/output schemas for tool registration automatically from typed argument structs.

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

## Integration Notes (atcr)

- Used in `internal/mcp/tools.go` to build the `atcr_report` input schema and to derive schemas for other MCP tool arguments automatically via the `mcp.AddTool` generic helper.
- The MCP SDK's `mcp.AddTool` generic form internally calls schema inference; `jsonschema-go` provides the underlying reflection engine.
- atcr uses `jsonschema.Reflector{}` with default settings — no `AllowAdditionalProperties` override is applied at the tool layer.

---
**Source:** go.mod v0.4.3; usage confirmed in internal/mcp/tools.go.
