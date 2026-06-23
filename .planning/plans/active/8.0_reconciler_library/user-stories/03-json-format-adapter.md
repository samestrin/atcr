# User Story 3: JSON Format Adapter (reconcile-json/v1)

**Plan:** [8.0: Reconciler Library Module Extraction](../plan.md)

## User Story

**As a** external tool author who emits findings as a JSON stream
**I want** a `reconcile-json/v1` adapter that decodes my finding stream into `[]reconcile.Source` and encodes a `reconcile.Result` back into a JSON finding stream
**So that** I can embed the reconciler's deterministic clustering, dedupe, and confidence scoring without importing the ATCR binary or coupling to ATCR's `atcr-findings/v1` wire format

## Story Context

- **Background:** The reconciler library extraction (plan 8.0) lifts ATCR's deterministic reconciler into a standalone, stdlib-only Go module at `github.com/samestrin/atcr/reconcile`. AC#4 requires one format adapter that proves embeddability without committing to a full format matrix — SARIF and other adapters can follow later based on demand. The adapter defines a new schema family `reconcile-json/v1`, versioned INDEPENDENTLY of `atcr-findings/v1`, so the external embedding contract is decoupled from ATCR's internal wire format. The adapter implementation does not exist yet — it is AC#4 work to be created at `reconcile/adapter/json/adapter.go`. The schema was resolved on 2026-06-23 in the codebase-discovery `integration_gaps` record and is the source of truth for input/output shapes.
- **Assumptions:**
  - The library public types (`Source`, `Finding`, `Merged`, `Result`, `Summary`, `Options`, `Verification`) are already lifted into `reconcile/` with JSON struct tags on the `Finding` type (Stories 1 and 2 land first).
  - The library is strictly stdlib-only in non-test files; the adapter uses only `encoding/json` — no third-party schema library.
  - The `reconcile-json/v1` schema is the contract between the library and its embedders; field names are the library `Finding`'s JSON struct tags.
  - Producers may emit unknown extra fields; the adapter tolerates them (ignore-by-default, the same convention ATCR's provider client uses).
- **Constraints:**
  - stdlib-only in shipped code (`encoding/json`); testify is allowed only in `*_test.go`.
  - Path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) are ATCR-internal and MUST NOT appear in the external schema.
  - Optional fields (`disagreement`, `verification`) use `omitempty` so their absence is byte-stable, preserving the deterministic total-order output.
  - Evolution within `reconcile-json/v1` is additive-only, mirroring the `atcr-findings` policy — no breaking changes inside the versioned family.
  - The adapter must round-trip the finding-stream semantics documented in `docs/findings-format.md` (findings.txt stream plus reconciled/findings.json, ambiguous.json, disagreements.json sidecars) through the independent `reconcile-json/v1` schema; it does not emit the ATCR-internal `atcr-findings/v1` wire format.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (module scaffold + `reconcile/go.mod`); Story 2 (public API lift — `Source`, `Finding`, `Result`, `Summary`, `Verification` with JSON struct tags must exist before the adapter can map to them) |

## Success Criteria (SMART Format)

- **Specific:** The adapter at `reconcile/adapter/json/adapter.go` exposes decode (external JSON stream → `[]reconcile.Source`) and encode (`reconcile.Result` → external JSON stream) functions implementing the `reconcile-json/v1` schema exactly as resolved in the codebase-discovery `integration_gaps` record.
- **Measurable:** A round-trip test decodes a fixture input (single object AND an array of source objects) into `[]Source`, runs `Reconcile`, encodes the `Result`, and asserts: (a) the output carries `"version":"reconcile-json/v1"` and an RFC3339 `reconciled_at`; (b) `findings[]` carry `reviewers[]`, `confidence`, and optional `disagreement`/`verification` with `omitempty` byte-stability (absent fields produce no keys); (c) `summary` and `ambiguous[]` are present; (d) zero path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) leak into the output; (e) the same input yields byte-identical output across runs.
- **Achievable:** Built entirely on `encoding/json` struct tags against library types that Story 2 already lifts as-is; no new algorithmic work, only schema mapping and envelope handling.
- **Relevant:** Satisfies AC#4 and is the single piece of evidence that the library is embeddable for tools that emit/consume JSON finding streams without importing the ATCR binary.
- **Time-bound:** Lands within the plan 8.0 sprint, after Stories 1 and 2, before the README/godoc example (Story 4) which references the adapter.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-decode-single-and-array-sources.md) | Decode Single and Array Source Objects (reconcile-json/v1) | Unit |
| [03-02](../acceptance-criteria/03-02-encode-result-to-versioned-envelope.md) | Encode Result to Versioned JSON Envelope (reconcile-json/v1) | Unit |
| [03-03](../acceptance-criteria/03-03-byte-stability-and-omitempty.md) | Byte-Stability and omitempty on Optional Fields | Unit |
| [03-04](../acceptance-criteria/03-04-path-validation-isolation-and-schema-independence.md) | Path-Validation Isolation and Schema Independence | Unit |

## Original Criteria Overview

1. Decode accepts both a single source object `{"version":"reconcile-json/v1","source":"<name>","findings":[...]}` and an array of such objects, mapping each to one `reconcile.Source` whose `Findings` are `[]reconcile.Finding`; unknown fields are ignored by default.
2. Encode produces `{"version":"reconcile-json/v1","reconciled_at":<RFC3339>,"findings":[{severity,file,line,problem,fix,category,est_minutes,evidence,reviewers:[],confidence,disagreement?,verification?{verdict,skeptic,notes}}],"summary":{...},"ambiguous":[...]}` from a `reconcile.Result`, with field names sourced from the library `Finding`'s JSON struct tags.
3. `disagreement` and `verification` are `omitempty` so their absence is byte-stable; `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` never appear in the external schema; the schema evolves additively-only within `reconcile-json/v1`, independent of `atcr-findings/v1`.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`_

## Technical Considerations

- **Implementation Notes:** The adapter lives at `reconcile/adapter/json/adapter.go` (per `files_to_create`). It is a thin `encoding/json` layer over the library's lifted types — no reconciler logic. Decode uses `json.RawMessage` sniffing (first non-space byte `[` vs `{`) to accept either a single source object or an array, then unmarshals each into an envelope `{version, source, findings[]}` whose `findings` map directly to `[]reconcile.Finding` via the library `Finding`'s JSON struct tags. Encode marshals `reconcile.Result` into the versioned output envelope, stamping `"version":"reconcile-json/v1"` and `reconciled_at` from `Options.ReconciledAt` (or `time.Now().UTC()`). The `Finding` type already carries `Disagreement` and `*Verification`; `omitempty` on both keeps absent fields out of the bytes. The library is synchronous and stateless — the adapter inherits that (no `context`, no goroutines).
- **Integration Points:**
  - Depends on Story 2's lifted `Finding` JSON struct tags — field names in the external schema ARE those tags (`severity`, `file`, `line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewer`/`reviewers`).
  - `Options.ReconciledAt` feeds the output `reconciled_at` timestamp, keeping the same timestamp source the lift-as-is `Reconcile` already uses.
  - The ATCR boundary adapter (`internal/reconcile/adapter/adapter.go`, ATCR-internal) is a separate concern: it converts `stream.Finding` ↔ `reconcile.Finding` and stamps path-validation fields. The external JSON adapter never touches path-validation fields — it reads/writes the pure library `Finding`.
  - `docs/findings-format.md` documents the `atcr-findings/v1` wire format the adapter must round-trip conceptually (findings.txt stream + reconciled/findings.json + ambiguous.json + disagreements.json sidecars), but `reconcile-json/v1` is its own independently-versioned schema, not a copy of `atcr-findings/v1`.
- **Data Requirements:** Schema family `reconcile-json/v1`, versioned INDEPENDENTLY of `atcr-findings/v1`. INPUT fields per finding: `severity`, `file`, `line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewer`. OUTPUT fields per finding: the same core fields plus `reviewers[]`, `confidence`, optional `disagreement`, optional `verification` (`verdict`, `skeptic`, `notes`). OUTPUT top-level: `version`, `reconciled_at` (RFC3339), `findings[]`, `summary{...}`, `ambiguous[]`. Excluded: `PathValid`, `PathWarning`, `PathSuggestion`, `ClusterMerged`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Path-validation fields (`PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`) leak into the external schema, coupling embedders to ATCR internals | High | The library `Finding` (Story 2) does NOT carry path-validation fields — they live only on ATCR's `JSONFinding` wrapper at the boundary adapter. The external adapter maps the pure library `Finding`, so leakage is structurally impossible; a round-trip test asserts none of the four field names appear in encoded output. |
| `omitempty` missing or field ordering changes break byte-stability, undermining the deterministic-output guarantee that makes the reconciler a credible reference implementation | High | Apply `omitempty` explicitly to `disagreement` and `verification` per the resolved schema; add a byte-stability test that encodes the same `Result` twice and asserts identical bytes. Note `encoding/json` marshals struct fields in declaration order, so field order is fixed by the library `Finding` definition. |
| Single-object vs array input ambiguity causes decode failures for producers who emit one source | Medium | Sniff the first non-space byte of the input: `[` → unmarshal as `[]envelope`; otherwise unmarshal as a single `envelope` and wrap into a one-element slice. Test both paths. |
| Schema drift between `reconcile-json/v1` and `atcr-findings/v1` confuses embedders who assume compatibility | Medium | Document the independence in the adapter package doc and README; the `version` field is `reconcile-json/v1`, never `atcr-findings/v1`. Additive-only evolution within v1 mirrors the `atcr-findings` policy but the version strings and field sets are distinct. |
| Unknown producer-specific fields break decode for tools that emit extras | Low | `encoding/json` ignores unknown fields by default (no `DisallowUnknownFields`); this matches ATCR's provider-client convention. Document this tolerance so producers know extras are safe. |
| Adapter depends on library types that Story 2 has not yet lifted, blocking parallel work | Medium | Sequence the adapter after Stories 1 and 2; the dependency is explicit in the Story Details table. If parallelized, stub the `Finding` struct tags first and refactor once Story 2 lands. |

---

**Created:** June 23, 2026 11:48:36AM
**Status:** Draft - Awaiting Acceptance Criteria
