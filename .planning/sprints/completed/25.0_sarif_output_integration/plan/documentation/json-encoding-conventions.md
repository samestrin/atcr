# encoding/json Conventions for renderSarif

`[REFERENCE]`

## Overview

atcr keeps its dependency tree small by design: everything beyond the direct third-party dependencies declared in `go.mod` is intentionally standard library (Epic 1.0 constraint: keep the dependency tree small). SARIF output is no exception — `renderSarif` builds its output tree from hand-written structs and marshals them with the standard `encoding/json` package rather than pulling in a SARIF-specific serialization library.

`encoding/json` already has an established role in atcr as the JSON layer behind the OpenAI-compatible provider client (struct-tagged request/response types, `json.Marshal`/`json.Unmarshal`). Within `internal/report`, that same package underpins the existing `renderJSON` function, which is the direct precedent for `renderSarif`: both take `(w io.Writer, findings []reconcile.JSONFinding) error`, both live in the same package, and both are format renderers dispatched from `Render`. `renderSarif` should reuse `renderJSON`'s marshaling conventions rather than inventing new ones, so the two renderers stay consistent for anyone reading the package.

The sections below extract the conventions that apply, cited from the standard-library spec and from `renderJSON` itself, and call out where they do or do not carry over to SARIF output.

## Key Concepts

**Struct-tagged marshaling.** atcr's established `encoding/json` pattern is struct-tagged types passed through `json.Marshal`/`json.Unmarshal`.

> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client] — "JSON: struct-tagged request/response types with `json.Marshal`/`json.Unmarshal`. Decode into a minimal envelope (choices → message → content); ignore unknown fields by default, which tolerates provider-specific extras."

`renderSarif`'s SARIF struct tree should follow the same tagging discipline (`json:"fieldName"`, `json:"fieldName,omitempty"` where SARIF's schema allows an optional field to be absent). The "ignore unknown fields by default" half of this convention is a `json.Unmarshal`-decode concern from the provider client; `renderSarif` only marshals (encodes) SARIF output, it does not parse SARIF back in, so that half does not apply here — it is noted only for completeness.

**nil-slice guard, indent, trailing newline, and error propagation.** The concrete conventions `renderSarif` should mirror all come from the sibling `renderJSON` function it sits beside in the same file.

> Source: [internal/report/render.go:renderJSON]

```go
// renderJSON re-emits the findings as indented JSON, never truncated — the
// machine contract for downstream tooling.
func renderJSON(w io.Writer, findings []reconcile.JSONFinding) error {
	if findings == nil {
		findings = []reconcile.JSONFinding{}
	}
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
```

Four conventions are visible in this precedent:

1. **nil-slice guard** — a nil `findings` slice is replaced with an empty slice literal before marshaling, so the JSON output is `[]` rather than `null`. `renderSarif` should apply the same guard to any slice-typed SARIF field built from `findings` (e.g., a `results` array), so an empty finding set still emits a valid empty JSON array rather than `null`.
2. **`json.MarshalIndent` with two-space indent** — output is human-readable, not minified. `renderSarif` should marshal its SARIF struct tree the same way.
3. **trailing newline** — the marshaled bytes have `'\n'` appended before the `Write` call, so the emitted file/stream ends with a newline like the other renderers in the package.
4. **error propagation** — both the `Marshal` error and the `Write` error are returned to the caller, never swallowed or logged-and-ignored.

## Code Examples

```go
// renderJSON re-emits the findings as indented JSON, never truncated — the
// machine contract for downstream tooling.
func renderJSON(w io.Writer, findings []reconcile.JSONFinding) error {
	if findings == nil {
		findings = []reconcile.JSONFinding{}
	}
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
```

## Quick Reference

| Convention | renderJSON precedent | Applies to renderSarif? |
|---|---|---|
| Struct-tagged types | `reconcile.JSONFinding` carries `json:"..."` tags | Yes — SARIF struct tree needs equivalent tags matching the SARIF schema field names |
| Marshal function | `json.MarshalIndent(findings, "", "  ")` | Yes — marshal the SARIF root struct the same way |
| nil-slice guard | `if findings == nil { findings = []reconcile.JSONFinding{} }` | Yes — apply to any nil slice-typed SARIF field (e.g., `results`) so it emits `[]` not `null` |
| Trailing newline | `w.Write(append(data, '\n'))` | Yes — keep output consistent with the other renderers in the package |
| Error propagation | Both `Marshal` and `Write` errors returned, not swallowed | Yes — same contract |
| Ignore unknown fields on decode | Provider client decodes into a minimal envelope, ignoring unknown fields | No — `renderSarif` only encodes SARIF, it does not decode/parse it |

## Related Documentation

- [./sarif-schema-reference.md](./sarif-schema-reference.md)
- [./github-code-scanning-integration.md](./github-code-scanning-integration.md)
- [./schema-validation-with-jsonschema-go.md](./schema-validation-with-jsonschema-go.md)
