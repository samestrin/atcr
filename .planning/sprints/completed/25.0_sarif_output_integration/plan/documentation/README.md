# Plan Documentation References

**Created:** July 14, 2026 03:59:11PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### Critical

- [SARIF 2.1.0 Schema Reference](sarif-schema-reference.md) — Core SARIF structure (version/$schema, runs, tool.driver, results) per the base OASIS spec, and the stricter fields GitHub Code Scanning requires for display; flags the direct conflict between the base spec's optional `region` and GitHub's required `region.startLine/startColumn/endLine/endColumn`.
- [GitHub Code Scanning SARIF Integration Constraints](github-code-scanning-integration.md) — GitHub's actual SARIF ingestion requirements: required fields, size/count limits, the `ruleId`/`rules[]` linkage requirement, deduplication via `partialFingerprints`, and how the Security-tab upload flow is distinct from the already-shipped `atcr github` PR-comment flow.

### Important

- [Schema-Validating SARIF Output with jsonschema-go](schema-validation-with-jsonschema-go.md) — How to use the already-vendored `google/jsonschema-go`'s `Schema.Resolve()` / `Resolved.Validate()` path to schema-check `renderSarif`'s output in `internal/report/sarif_test.go`, and the local SARIF schema fixture this will require.

### Reference

- [encoding/json Conventions for renderSarif](json-encoding-conventions.md) — atcr's stdlib-first JSON conventions and the `renderJSON` precedent in `internal/report/render.go` that `renderSarif` should mirror.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - `.planning/specifications/packages/jsonschema-go.md`
  - `.planning/specifications/packages/standard-library.md`
  - [OASIS SARIF v2.1.0 spec](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html)
  - [GitHub Code Scanning SARIF support](https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support)
  - [SonarSource SARIF overview](https://www.sonarsource.com/resources/library/sarif/)
- **Local Plan References:**
  - [`package-recommendations.md`](../package-recommendations.md) — documents the hand-rolled-struct default and the optional `owenrumney/go-sarif/v2` alternative
  - [`source.md`](./source.md) — generated source-path index for this documentation set
- **Codebase Discovery:** `.planning/plans/active/25.0_sarif_output_integration/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding — in particular, resolve the `region`-omission-vs-GitHub-requires-it conflict flagged in `sarif-schema-reference.md` before implementing `renderSarif`'s line-anchoring logic.
2. Review **Important** docs during development — the jsonschema-go validation path needs a vendored SARIF schema fixture that does not exist in the repo yet.
3. Consult **Reference** docs for specific questions.

---

**Navigation:** [← Back to Plan](../README.md)
