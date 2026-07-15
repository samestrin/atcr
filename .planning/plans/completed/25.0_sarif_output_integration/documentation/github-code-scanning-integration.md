# GitHub Code Scanning SARIF Integration Constraints

`[CRITICAL]`

## Overview

GitHub's SARIF ingestion is stricter than the bare SARIF 2.1.0 spec. A file can be schema-valid SARIF and still render nothing useful on the Security tab: GitHub additionally requires each result to resolve to a rule definition, to carry a precise physical location, and to stay under a set of hard size and count ceilings. `renderSarif` (the formatter behind `atcr report --format=sarif`) has to satisfy GitHub's display requirements, not just the schema, or `atcr`'s findings will upload successfully but show up empty, truncated, or unlinked to a rule on the Security tab.

The most consequential of these constraints for this plan is the `rules[]` / `ruleId` linkage: every result's `ruleId` must match an `id` in `tool.driver.rules`, and each rule entry needs `shortDescription.text` and `fullDescription.text`. `reconcile.JSONFinding.Category` (`internal/reconcile/emit.go:68`) is the natural source field for this — `renderSarif` needs to build a stable `rules[]` array (one entry per distinct `Category` value) alongside the `results[]` array, not just emit bare results and hope GitHub infers a rule. This is a concrete design requirement this document surfaces because the plan's original Proposed Solution did not call out the linkage explicitly.

Finally, the Security-tab upload flow this plan targets is distinct from `atcr github` (`cmd/atcr/github.go`, documented in `docs/github-action.md`), which already posts PR checks and inline "Files Changed" comments directly via the GitHub API. That integration is separate, already shipped, and out of scope here. GitHub's SARIF-specific PR-annotation constraint (results only surface in PR checks for lines present in the diff) applies to GitHub's *native* SARIF-based PR annotations, not to `atcr github`'s own comment flow — the two paths should not be conflated in the CI doc example this plan adds.

## Key Concepts

### Required fields for display

A SARIF file must have `$schema` (pointing at the SARIF 2.1.0 schema), `version: "2.1.0"`, and at least one entry in `runs[]`. Each run needs `tool.driver` (with `name` and `rules[]`) and `results[]` (may be empty). Each result needs `message.text` and at least one entry in `locations[]`; each location's `physicalLocation` needs `artifactLocation.uri` (a repository-root-relative path) plus `region.startLine`, `region.startColumn`, `region.endLine`, and `region.endColumn` — all four region fields are required for the result to display. `partialFingerprints` is not strictly required to upload, but GitHub uses it for deduplication and will attempt to populate it if absent.

> Source: [https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support]

### rules[] / ruleId linkage maps to reconcile.JSONFinding.Category

Every result's `ruleId` must match a rule `id` defined in `tool.driver.rules`, and that `ruleId` must stay consistent across analysis runs so GitHub can deduplicate alerts correctly. Each rule requires `id`, `shortDescription.text`, and `fullDescription.text`. `renderSarif` should derive `ruleId` from `reconcile.JSONFinding.Category` (`internal/reconcile/emit.go:68`, also referenced at lines 212, 265, and 358 for the `Category` field's propagation through the reconcile pipeline) and emit one `tool.driver.rules[]` entry per distinct category value, rather than emitting `results[]` alone.

> Source: [https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support]; internal/reconcile/emit.go

### Size and count limits

| — see Quick Reference table below for exact figures — |

GitHub enforces hard ceilings on uploaded SARIF: a maximum file size (gzip-compressed), and maximums on results per run, rules per run, locations per result, thread-flow locations per result, and runs per file. Several of these are soft-truncated for display (e.g., only the top severity-ranked subset renders) rather than rejected outright, but the underlying counts must still stay under the upload ceiling or the upload itself fails.

> Source: [https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support]

### Severity ordering

Valid `level` values are `note`, `warning`, and `error`. GitHub orders results by precision and severity level when deciding what to surface first: "code scanning orders results by precision on GitHub so that the results with the highest level, and highest precision are shown first." `renderSarif` should map `atcr`'s internal severity scale onto these three levels rather than inventing additional levels.

> Source: [https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support]

### Deduplication via partialFingerprints

GitHub matches alerts across uploads using `partialFingerprints.primaryLocationLineHash`. Consistent file paths across runs are essential — if the same finding is reported with a different `artifactLocation.uri` between runs, GitHub treats it as a new alert instead of matching the existing one, producing duplicate alerts. The upload action generates fingerprints automatically when they are missing from the SARIF file, but the underlying `ruleId` and location path still need to be stable run-to-run for that automatic fingerprinting to dedupe correctly.

> Source: [https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support]

### PR checks vs. Security tab — and how this differs from atcr github

GitHub's SARIF-based PR check display has an additional constraint beyond upload validity: "Results appear in PR checks only when all identified lines exist in the diff and were added or edited (not deleted)." This plan targets the org-wide, branch/repo-aggregated Security tab (GitHub Advanced Security Code Scanning), not the PR-check flow — so this line-matching constraint is informational context here, not a requirement this plan must satisfy.

It is also a constraint on GitHub's own SARIF-based PR annotations specifically. `atcr github` (`cmd/atcr/github.go`, documented in `docs/github-action.md`) already posts PR checks and inline "Files Changed" comments directly through the GitHub API, independent of SARIF upload. The two integration paths are separate and should not be conflated: the SARIF pipeline documented here feeds the Security tab; `atcr github` feeds PR checks/comments directly.

> Source: [https://docs.github.com/en/code-security/reference/code-scanning/sarif-files/sarif-support]; docs/github-action.md; cmd/atcr/github.go

## Code Examples

CI step piping the intended `atcr` pipeline into GitHub's `upload-sarif` action, following the same fenced-YAML-plus-doc-link structure as the existing `docs/ci-integration.md` "Maintained PR Action" section:

```yaml
- uses: actions/checkout@v4
  with: { fetch-depth: 0 }

- name: Run atcr and emit SARIF
  run: atcr review && atcr reconcile && atcr report --format=sarif > results.sarif

- name: Upload SARIF to GitHub Code Scanning
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: results.sarif
```

## Quick Reference

| Constraint | Value | Source |
|---|---|---|
| Max SARIF file size | 10 MB (gzip-compressed) | GitHub SARIF support docs |
| Max results per run | 25,000 (only top 5,000 displayed by severity) | GitHub SARIF support docs |
| Max rules per run | 25,000 | GitHub SARIF support docs |
| Max locations per result | 1,000 (only 100 displayed) | GitHub SARIF support docs |
| Max thread flow locations per result | 10,000 (only 1,000 displayed) | GitHub SARIF support docs |
| Max runs per file | 20 | GitHub SARIF support docs |
| Required `level` values | `note`, `warning`, `error` | GitHub SARIF support docs |
| Required region fields per location | `startLine`, `startColumn`, `endLine`, `endColumn` | GitHub SARIF support docs |
| `ruleId` source in atcr | `reconcile.JSONFinding.Category` | internal/reconcile/emit.go:68 |
| PR-check line matching | Only diff-added/edited lines display in PR checks | GitHub SARIF support docs |

## Related Documentation

- [./sarif-schema-reference.md](./sarif-schema-reference.md)
- [./schema-validation-with-jsonschema-go.md](./schema-validation-with-jsonschema-go.md)
- [./json-encoding-conventions.md](./json-encoding-conventions.md)
