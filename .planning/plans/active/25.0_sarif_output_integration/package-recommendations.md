# Package Recommendations

**Generated:** 2026-07-14
**Project type:** Go (module `github.com/samestrin/atcr`)

## Recommendation

### owenrumney/go-sarif/v2 (optional)

- **Category:** SARIF document builder
- **Handles:** SARIF 2.1.0 JSON schema construction (runs, tool.driver, rules, results, physicalLocation/region) via typed Go structs and a fluent builder, instead of hand-rolling the schema struct tree.
- **Install:** `go get github.com/owenrumney/go-sarif/v2`
- **Integration point:** `internal/report/sarif.go` (new file), called only from `renderSarif`, so the dependency is fully wrapped behind the existing `report.Render` seam per this repo's "wrap external dependencies" standard.
- **Reason:** Used by several established Go security-tooling projects (e.g. tfsec, checkov's Go integrations) to emit SARIF; reduces the risk of a hand-rolled schema drifting from the SARIF 2.1.0 spec (wrong `level` enum, missing required `$schema`/`version` fields, malformed `physicalLocation`) that AC1/AC2 of this epic depend on getting right.
- **Scores:** maturity 7/10, complexity_saved 6/10, integration_risk 3/10 (single-purpose, no transitive surface beyond schema types).

### Trade-off: hand-rolled JSON (no new dependency)

Every existing format in `internal/report/render.go` (`renderJSON`, `renderMarkdown`, `renderChecklist`) is implemented via stdlib `encoding/json` and hand-built struct trees — SARIF's schema is larger but no more structurally complex than what `renderJSON` already does for `[]reconcile.JSONFinding`. A hand-rolled `internal/report/sarifdoc.go` struct tree (mirroring the official SARIF 2.1.0 schema for just the subset ATCR needs: `run.tool.driver.{name,rules}` + `run.results[].{ruleId,level,message,locations}`) keeps the format list free of new dependencies, consistent with every other format in the same file.

**This repo already depends on `github.com/google/jsonschema-go` (go.mod)** — usable in `internal/report/sarif_test.go` to validate `renderSarif`'s output against the official SARIF 2.1.0 JSON schema without adding a new dependency for validation.

**Recommendation:** default to the hand-rolled struct-tree approach (zero new runtime dependency, consistent with the file's existing pattern) unless the SARIF subset needed grows beyond `results[]` + basic locations (e.g. if a future epic needs `codeFlows`, `relatedLocations`, or multi-tool `runs[]` — at that point `go-sarif/v2`'s complexity-saved argument gets stronger).

## Summary

No high-ROI package is required to complete this epic's scope (a single `results[]`-only SARIF 2.1.0 document). `owenrumney/go-sarif/v2` is documented above as an available option if schema surface grows.
