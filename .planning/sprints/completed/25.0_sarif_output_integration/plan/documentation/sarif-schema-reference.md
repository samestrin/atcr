# SARIF 2.1.0 Schema Reference

`[CRITICAL]`

## Overview

SARIF (Static Analysis Results Interchange Format) is a standardized, open JSON-based format defined by the OASIS consortium for representing the output of static analysis tools — linters, security scanners, and code quality tools — in a way that is interoperable across analysis platforms and consumer UIs (SonarSource, "SARIF, or Static Analysis Results Interchange Format, is a standardized, open JSON-based format designed to represent the output of static analysis tools such as linters, security scanners, and code quality tools."). A SARIF log file is a single JSON document, UTF-8 encoded, rooted in a `version`/`$schema`/`runs` object. Each entry in `runs` carries its own `tool.driver` (the analysis engine's identity and rule catalog) and its own `results` array (the actual findings). This structure is what `internal/report/sarif.go`'s `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error` must serialize ATCR's reconciled findings into.

The base OASIS spec (Source A) is deliberately permissive: most result-level fields, including `ruleId` and the entire `region` object, are optional. This matters for ATCR because `reconcile.JSONFinding` (per codebase-discovery.json, `internal/reconcile/emit.go`) allows `Line` to be `0` or negative for file-level findings, and the existing `internal/ghaction/render.go` convention (`location()` helper) already omits the line reference entirely when `Line<=0`. A naive implementation that follows only the base spec would carry that same omission pattern straight into SARIF output.

The problem is that the two audiences for this SARIF file do not agree on what "valid" means. GitHub Code Scanning (Source B) is the primary consumer for this feature (feeding the Security tab), and it imposes stricter display requirements than the base spec: it requires at least one `location` per result, and within that location's `physicalLocation.region`, all four of `startLine`, `startColumn`, `endLine`, and `endColumn` are required for the result to actually render. A SARIF document that is spec-valid per Source A can therefore still fail to display correctly on GitHub. This divergence is the single most important design fact for whoever implements `renderSarif` — see the Key Concepts and Quick Reference sections below, and the open question flagged at the end of this document.

## Key Concepts

### Top-level version and $schema

- `"version"` must be the literal string `"2.1.0"`.
- `"$schema"` should reference the SARIF 2.1.0 schema document. The base spec points to `https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json`; GitHub's own docs use `https://json.schemastore.org/sarif-2.1.0.json`.
- The spec is explicit about the serialization and encoding: "A SARIF log file SHALL contain a serialization of the SARIF object model into the JSON format," and "A SARIF log file SHALL be encoded in UTF-8."
> Source: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html

GitHub additionally treats `$schema`, `version`, and a non-empty `runs[]` array as required top-level elements for a file to be accepted at all (not merely recommended).
> Source: https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support

### runs / tool.driver / rules structure

Each element of `runs` bundles one analysis execution's tool identity and its results:

```json
{
  "tool": {
    "driver": {
      "name": "ToolName",
      "informationUri": "https://...",
      "rules": [...]
    }
  },
  "results": [...]
}
```

- `tool.driver.name` (string) is the tool's human-readable identifier — this is the name GitHub displays in the Security tab. For ATCR output this should be the literal string `"atcr"`.
- `tool.driver.informationUri` is optional per the base spec — a URI with product information. For ATCR output this can point to the project repository or documentation.
- `tool.driver.rules` is an array of `reportingDescriptor` objects (rule metadata).
> Source: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html

GitHub requires `tool.driver.name` and `tool.driver.rules[]` to be present, and further requires each rule object to carry `id`, `shortDescription.text`, and `fullDescription.text`. Every `results[].ruleId` must match a rule `id` declared in `tool.driver.rules`, and that `ruleId` must stay consistent across analysis runs — GitHub uses it (together with `partialFingerprints`) for alert deduplication.
> Source: https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support

The SonarSource overview frames this the same way at a higher level: a SARIF file contains "version info, runs (analysis execution + tool metadata + scanned files), results (issues with severity/location/remediation), rules (checks implemented), artifacts (scanned files info)."
> Source: https://www.sonarsource.com/resources/library/sarif/

### Results array — base spec vs. GitHub's stricter requirements (the critical divergence)

Per the base OASIS spec, a result object should minimally include:

| Field | Type | Required per base spec? | Notes |
|-------|------|--------------------------|-------|
| `ruleId` | string | No | Recommended for traceability, but optional |
| `level` | string | No | Defaults to `"warning"` if omitted |
| `message` | object | Yes | Must contain a `"text"` property |
| `locations` | array | No | Array of location objects |

Valid `level` values per the base spec: `"error"`, `"warning"`, `"note"`, `"none"`. The spec states level "SHALL be a string in the format specified" for the enumeration.
> Source: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html

GitHub Code Scanning imposes a stricter, display-oriented subset of requirements on top of this:

- `message.text` is required (the alert description).
- `locations[]` must contain at least one entry — required for display.
- `partialFingerprints` is used for deduplication; if omitted, GitHub's upload action will attempt to auto-populate it, but consistent filepaths across runs are essential since differing paths create duplicate alerts.
- `ruleId` must be present and must match a declared rule `id`.
- Valid `level` values GitHub supports are `"note"`, `"warning"`, `"error"` — GitHub does not use `"none"` the same way for display purposes.
- GitHub orders results by precision: "code scanning orders results by precision on GitHub so that the results with the highest level, and highest precision are shown first."
> Source: https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support

### Locations / region — the core divergence

The base spec treats the `region` object as entirely optional: "a result can omit region details for file-level findings without pinpointing a specific line." A minimal location example from the spec:

```json
"locations": [
  {
    "physicalLocation": {
      "artifactLocation": {
        "uri": "path/to/file.js"
      },
      "region": {
        "startLine": 42
      }
    }
  }
]
```
> Source: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html

GitHub Code Scanning contradicts this permissiveness for display purposes. Its required `physicalLocation` fields are:

- `artifactLocation.uri` — relative file path from the repo root.
- `region.startLine` — required for display.
- `region.startColumn` — required.
- `region.endLine` — required.
- `region.endColumn` — required.
> Source: https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support

This is the exact tension `renderSarif` must resolve. Per codebase-discovery.json, `internal/ghaction/render.go`'s `location(f reconcile.JSONFinding) string` helper already special-cases `Line<=0` by omitting the line reference entirely ("if f.Line <= 0 { return f.File }") — the existing ATCR convention for a file-level finding with no specific line. That convention is spec-valid under Source A but is **not** sufficient for a result to display correctly in GitHub's Security tab under Source B, since `region.startLine/startColumn/endLine/endColumn` are all required there. Whether `renderSarif` should default `startColumn`/`endColumn` to `1` for such findings, synthesize an `endLine` equal to `startLine`, or accept degraded (non-)display for file-level findings is an open design decision — flagged here for the acceptance-criteria stage, not resolved in this document.
> Source: codebase-discovery.json (internal/ghaction/render.go location() convention)

### Severity mapping

`reconcile.JSONFinding.Severity` (codebase-discovery.json, `internal/reconcile/emit.go`) should map to SARIF's `result.level` via the codebase's existing canonical severity rubric — `reconcile.NormalizeSeverity()` and `reconcile.SeverityRank` in `reconcile/severity.go` / `reconcile/merge.go` — rather than introducing a new local CRITICAL/HIGH/MEDIUM/LOW-to-SARIF-level mapping. This avoids duplicating the severity rubric a second time in the codebase.
> Source: codebase-discovery.json (`reconcile/severity.go`, `reconcile/merge.go`)

### Size, count, and structural limits (GitHub-side)

GitHub enforces the following limits on ingested SARIF files: max file size 10 MB (gzip-compressed); max 25,000 results per run (only the top 5,000 are displayed, ranked by severity); max 25,000 rules per run; max 1,000 locations per result (only 100 displayed); max 10,000 thread flow locations per result (only 1,000 displayed); max 20 runs per file.
> Source: https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support

### PR display constraint (scoping note, not a blocker)

GitHub notes: "Results appear in PR checks only when all identified lines exist in the diff and were added or edited (not deleted)." This plan targets the Security tab / branch-wide scan primarily, not the PR-check flow that `atcr github` already covers — so this constraint is a scoping note rather than a blocker for `renderSarif`.
> Source: https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support

## Code Examples

Top-level SARIF structure (from the base spec summary):

```json
{
  "version": "2.1.0",
  "$schema": "...",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "atcr",
          "informationUri": "https://github.com/samestrin/atcr",
          "rules": [...]
        }
      },
      "results": [...]
    }
  ]
}
```

Minimal location/region example (from the base spec summary):

```json
"locations": [
  {
    "physicalLocation": {
      "artifactLocation": {
        "uri": "path/to/file.js"
      },
      "region": {
        "startLine": 42
      }
    }
  }
]
```

## Quick Reference

| Field | Base SARIF spec (Source A) | GitHub Code Scanning (Source B) | Notes |
|-------|------------------------------|----------------------------------|-------|
| `version` | Must be `"2.1.0"` | Required | — |
| `$schema` | Should reference SARIF 2.1.0 schema | Required | Source A and B cite different canonical URLs |
| `runs[]` | Present | Required, at least one run | Max 20 runs per file (GitHub) |
| `tool.driver.name` | Present | Required, displayed on GitHub | — |
| `tool.driver.rules[]` | Array of rule metadata | Required; each rule needs `id`, `shortDescription.text`, `fullDescription.text` | Max 25,000 rules per run (GitHub) |
| `results[].ruleId` | Optional but recommended | Required; must match a declared rule `id`; must stay consistent across runs | Used for GitHub deduplication |
| `results[].level` | Optional, default `"warning"`; values: error/warning/note/none | Supported values: note/warning/error (no `"none"`) | GitHub orders results by level + precision |
| `results[].message.text` | Required | Required (alert description) | — |
| `results[].locations[]` | Optional | Required, at least one location | Max 1,000 locations/result, 100 displayed (GitHub) |
| `results[].locations[].physicalLocation.artifactLocation.uri` | Present when location given | Required; relative path from repo root | Path consistency across runs is essential for dedup |
| `results[].locations[].physicalLocation.region` | Entirely optional (file-level findings may omit it) | `startLine`, `startColumn`, `endLine`, `endColumn` all required for display | **Core divergence** — drives the ATCR `Line<=0` design question |
| `results[].partialFingerprints` | Not part of base minimal example | Used for dedup; auto-populated by GitHub's upload action if missing | — |
| Results per run | Not limited by base spec | Max 25,000 (only top 5,000 displayed) | — |
| File size | Not limited by base spec | Max 10 MB (gzip-compressed) | — |

## Related Documentation

- [GitHub Code Scanning Integration](./github-code-scanning-integration.md)
- [Schema Validation with JSON Schema (Go)](./schema-validation-with-jsonschema-go.md)
- [JSON Encoding Conventions](./json-encoding-conventions.md)
