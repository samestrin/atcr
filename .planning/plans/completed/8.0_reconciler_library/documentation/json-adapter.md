# JSON Format Adapter (reconcile-json/v1) [CRITICAL]

## Overview

The JSON format adapter (`reconcile-json/v1`) converts an external finding stream into `[]reconcile.Source` for input and a `reconcile.Result` back into an external finding stream for output, satisfying AC#4. Its purpose is to prove the reconciler library is embeddable without committing to a full format matrix. The library is strictly stdlib-only, so the adapter is built on `encoding/json`, the standard package that implements JSON per RFC 7159.

The adapter defines a new schema family, `reconcile-json/v1`, versioned independently of `atcr-findings/v1`. Input is a JSON object (or an array of such objects) per source; output is a versioned, timestamped document carrying reconciled findings, a summary, and ambiguous clusters. The schema is the contract between the library and its embedders — field names are the library `Finding`'s JSON struct tags, mapped by `encoding/json` struct tags.

Path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) are ATCR-internal and explicitly excluded from the external schema. Optional fields (`disagreement`, `verification`) use `omitempty` so their absence is byte-stable, preserving the deterministic total-order output that makes the reconciler a credible reference implementation. Evolution within v1 is additive-only, mirroring the `atcr-findings` policy. The adapter implementation does not exist yet — it is AC#4 work to be done in `reconcile/adapter/json/adapter.go`.

## Key Concepts

**reconcile-json/v1 schema family, independently versioned.** The schema family `reconcile-json/v1` is versioned INDEPENDENTLY of `atcr-findings/v1`. This decouples the external embedding contract from ATCR's internal wire format.
> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

**Input contract (decode -> []reconcile.Source).** The adapter accepts either a single JSON object or an array of source objects, each `{version, source, findings[]}`, mapping each to one `reconcile.Source` whose `Findings` are `[]reconcile.Finding`.
> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

**Output contract (encode reconcile.Result).** The encoded result is a versioned document with `reconciled_at` (RFC3339), `findings[]` carrying `reviewers[]`, `confidence`, optional `disagreement`, and optional `verification` (`verdict`, `skeptic`, `notes`), plus a `summary` object and an `ambiguous[]` array.
> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

**Struct-tag field mapping via encoding/json.** `encoding/json` uses struct tags for field mapping; the external schema's field names are the library `Finding`'s JSON tags.
> Source: [.planning/specifications/packages/go.md:Common Patterns/Struct Tags for JSON]

**omitempty byte-stability.** `omitempty` is applied to `disagreement`/`verification` so absence is byte-stable, preserving deterministic output.
> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

**Exclusion of path-validation fields.** `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` are ATCR-internal and NOT part of the external schema.
> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

**Additive-only evolution within v1.** Evolution within v1 is additive-only, mirroring the `atcr-findings` policy — no breaking changes inside the versioned family.
> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

**stdlib-only constraint.** The library is stdlib-only; no external dependencies are required — the standard library covers JSON out of the box.
> Source: [.planning/specifications/packages/go.md:Integration Notes]

**Ignore unknown fields by default.** Decoding into a minimal envelope while ignoring unknown fields by default tolerates producer-specific extras — the same convention ATCR's provider client uses.
> Source: [.planning/specifications/packages/standard-library.md:net/http + encoding/json — provider client]

**Schema defined before implementation.** The external JSON adapter schema should be defined in `reconcile/adapter/json` before implementing the adapter; kept simple and versioned independently from `atcr-findings/v1`.
> Source: [codebase-discovery.json:architecture_recommendations]

**Round-trips the findings-format wire format.** The adapter must round-trip the wire format documented in `docs/findings-format.md` — the findings.txt stream plus reconciled/findings.json, ambiguous.json, and disagreements.json sidecars.
> Source: [codebase-discovery.json:semantic_matches/docs/findings-format.md]

**Implementation pending (AC#4).** The file `reconcile/adapter/json/adapter.go` is listed in `files_to_create` as the JSON adapter: external finding stream -> `[]Source` and `Result` -> external finding stream (AC#4), based on `docs/findings-format.md`. No adapter Go code exists yet; do not assume Decode/Encode function bodies — describe the schema and contract only.
> Source: [codebase-discovery.json:files_to_create]

## Code Examples

### encoding/json struct tags (verbatim)

`encoding/json` uses struct tags for field mapping. This is the mechanism the adapter relies on to bind the external schema field names to the library `Finding` type:

```go
type User struct {
    Name  string `json:"name"`
    Email string `json:"email,omitempty"`
}
```
> Source: [.planning/specifications/packages/go.md:Common Patterns/Struct Tags for JSON]

### reconcile-json/v1 schema (verbatim)

The complete schema resolution, quoted verbatim from the codebase discovery record:

> Define schema family 'reconcile-json/v1', versioned INDEPENDENTLY of atcr-findings/v1. INPUT (decode -> []reconcile.Source): a JSON object per source {"version":"reconcile-json/v1","source":"<name>","findings":[{severity,file,line,problem,fix,category,est_minutes,evidence,reviewer}]}; the adapter accepts either a single object or an array of these objects, mapping each to one reconcile.Source whose Findings are []reconcile.Finding. OUTPUT (encode reconcile.Result): {"version":"reconcile-json/v1","reconciled_at":<RFC3339>,"findings":[{severity,file,line,problem,fix,category,est_minutes,evidence,reviewers:[],confidence,disagreement?,verification?{verdict,skeptic,notes}}],"summary":{...},"ambiguous":[...]}. Field names are the library Finding's JSON tags; path-validation fields (PathValid/PathWarning/PathSuggestion/ClusterMerged) are ATCR-internal and NOT part of the external schema. omitempty on disagreement/verification so absence is byte-stable. Additive-only evolution within v1, mirroring the atcr-findings policy.

> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

**Input shape** (verbatim substring of the resolution above — single source object; the adapter also accepts an array of these):

```json
{"version":"reconcile-json/v1","source":"<name>","findings":[{severity,file,line,problem,fix,category,est_minutes,evidence,reviewer}]}
```

**Output shape** (verbatim substring of the resolution above — encoded from `reconcile.Result`):

```json
{"version":"reconcile-json/v1","reconciled_at":<RFC3339>,"findings":[{severity,file,line,problem,fix,category,est_minutes,evidence,reviewers:[],confidence,disagreement?,verification?{verdict,skeptic,notes}}],"summary":{...},"ambiguous":[...]}
```

> Source: [codebase-discovery.json:integration_gaps/JSON adapter external schema]

## Quick Reference

| Aspect | Value |
|--------|-------|
| Schema family | `reconcile-json/v1` |
| Versioning | Independent of `atcr-findings/v1` |
| Input | Single object or array of `{version, source, findings[]}` -> `[]reconcile.Source` |
| Output | `{version, reconciled_at, findings[], summary, ambiguous[]}` from `reconcile.Result` |
| Implementation | `encoding/json` (stdlib-only, no external deps) |
| Field-name source | The library `Finding`'s JSON struct tags |
| Optional fields | `disagreement`, `verification` (`omitempty` for byte-stability) |
| Excluded fields | `PathValid`, `PathWarning`, `PathSuggestion`, `ClusterMerged` (ATCR-internal) |
| Evolution policy | Additive-only within v1 (mirrors `atcr-findings`) |
| Unknown-field handling | Ignore by default (tolerates producer-specific extras) |
| Target file | `reconcile/adapter/json/adapter.go` |
| Status | AC#4 — not yet implemented (to be created) |

## Related Documentation

- [.planning/specifications/packages/go.md](../../specifications/packages/go.md) — `encoding/json` struct tags and stdlib integration notes
- [.planning/specifications/packages/standard-library.md](../../specifications/packages/standard-library.md) — `net/http` + `encoding/json` provider-client conventions (ignore-unknown-fields, `json.Marshal`/`json.Unmarshal`)
- [codebase-discovery.json](../codebase-discovery.json) — `integration_gaps/JSON adapter external schema` (verbatim schema resolution), `files_to_create`, `architecture_recommendations`
- [docs/findings-format.md](../../../../../docs/findings-format.md) — `atcr-findings/v1` wire format the adapter must round-trip
- [plan.md](../plan.md) — 8.0 reconciler library plan, AC#4
