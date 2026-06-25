# User Story 3: Language-Aware Skeptic Routing

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** Go developer using ATCR with a language-scoped persona installed
**I want** skeptic selection to automatically prefer skeptics whose `Language` field matches the file extension of the finding under review
**So that** my findings are verified by skeptics that understand Go idioms, giving me higher-quality corroboration than a generalist skeptic can provide

## Story Context

- **Background:** `SelectEligibleSkeptics` in `internal/verify/select.go:55` currently selects skeptics from an alphabetically sorted pool with an `n`-cap, with no awareness of the language of the file being reviewed. A Go-scoped persona such as `idiomatic` (shipped in T1) declares `language: [go]` in its registry entry. Without routing, that persona competes equally with generalist skeptics regardless of whether the finding is in a `.go` file, wasting its domain specificity. The routing change introduces a two-partition reorder — language-matched skeptics first, unmatched after — so the existing `n`-cap naturally favors matched skeptics. No behavioral change occurs when no persona declares `language`, preserving full backward compatibility.
- **Assumptions:** `AgentConfig` gains a new `Language []string` field in `internal/registry/config.go` (canonical form: without leading dot, lowercased, e.g. `["go", "ts"]`) before this routing logic is implemented, making T8 a prerequisite of T1 in Sprint A. The corroboration score tie-break map (`map[string]float64`) passed as a 4th parameter to `SelectEligibleSkeptics` is nil-safe; a nil map degrades to alphabetical ordering within the matched partition. Only one production caller exists (`internal/verify/pipeline.go:162`), so the signature change is fully contained.
- **Constraints:** Fallback to the general pool when no language-matched skeptic exists must be silent — no log output, no error, no behavioral difference from today's unscoped run. The `Language` field must be backward-compatible: existing `registry.yaml` files that omit the field continue to load and function without modification. The `n`-cap in `select.go:84-86` must not be altered; routing works by reordering the slice the cap acts on, not by changing the cap itself.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | `AgentConfig.Language []string` field must be merged and green before `SelectEligibleSkeptics` routing is implemented (intra-sprint ordering within Sprint A) |

## Success Criteria (SMART Format)

- **Specific:** When a finding's file extension matches one or more installed skeptics' `Language` field, those skeptics appear before unmatched skeptics in the selection slice passed to the `n`-cap, causing the cap to preferentially select them.
- **Measurable:** Unit tests in `internal/verify/select_test.go` cover three scenarios — language match (matched skeptic selected), no-match fallback (general pool used, no error), and multiple-match tie-break (corroboration score desc, then alphabetical) — and all pass under `go test ./internal/verify/...`.
- **Achievable:** The change is a contained two-partition reorder on a single already-sorted `[]string` in `select.go`; no new packages, no external dependencies, and one production caller to update.
- **Relevant:** Language-matched skeptic routing directly improves verification quality for Go (and future language-scoped) personas, which is a primary value driver for vertical market adoption in this plan.
- **Time-bound:** Completed and verified green within Sprint A, before T1 bonus personas land on top of the new `Language` field.

## Acceptance Criteria Overview

This story is complete when the following acceptance criteria are met:

- **03-01**: `AgentConfig` supports an optional `Language []string` field with validation and canonicalization.
- **03-02**: `SelectEligibleSkeptics` reorders eligible skeptics to prefer language-matched agents.
- **03-03**: The production caller in `internal/verify/pipeline.go` passes the new `scores` parameter.
- **03-04**: Fallback to the general skeptic pool is silent and automatic when no language match exists.
- **03-05**: Existing registry YAML files without a `language` key continue to load and behave identically.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-agentconfig-language-field.md) | AgentConfig Language Field | Unit |
| [03-02](../acceptance-criteria/03-02-select-eligible-skeptics-routing.md) | SelectEligibleSkeptics Language Routing | Unit |
| [03-03](../acceptance-criteria/03-03-pipeline-caller-update.md) | Pipeline Caller Signature Update | Integration |
| [03-04](../acceptance-criteria/03-04-silent-fallback-no-match.md) | Silent Fallback When No Language Match | Unit |
| [03-05](../acceptance-criteria/03-05-registry-yaml-backward-compatibility.md) | Registry YAML Backward Compatibility | Unit + Integration |

## Original Criteria Overview

1. `AgentConfig` has a `Language []string \`yaml:"language,omitempty"\`` field; `validateAgent` rejects entries containing empty strings or control characters; `applyDefaults` canonicalizes entries by trimming whitespace, stripping a single leading dot, and lowercasing — matching the existing `MinSeverity`/`Role` canonicalization pattern.
2. `SelectEligibleSkeptics` accepts a 4th `scores map[string]float64` parameter; after `sort.Strings(names)`, it partitions names into language-matched and unmatched using `normalizeExt(filepath.Ext(finding.File))`, rebuilds names as `append(matched, unmatched...)`, and applies the existing `n`-cap — tie-breaking within the matched partition by score descending then alphabetical; a nil or absent score entry falls back to alphabetical only.
3. The single production caller at `internal/verify/pipeline.go:162` is updated to pass the corroboration score map (or nil when unavailable); `go build ./...` is clean after the update.
4. Fallback is silent: when no skeptic has a matching `Language` entry (or all skeptics have `Language: []`), `SelectEligibleSkeptics` behaves identically to its pre-routing behavior with no log output and no error.
5. Existing `registry.yaml` files that omit the `language` key load without error and produce identical skeptic selection behavior to the pre-routing baseline.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** The two-partition reorder operates on the `names []string` slice after `sort.Strings(names)` at `select.go:81`. A `normalizeExt` helper strips the leading dot and lowercases the result of `filepath.Ext(finding.File)` — the same normalization applied to each entry in `skeptic.Language` at load time via `applyDefaults`. The matched partition is built in a single pass over `names`; a second pass collects unmatched; `names = append(matched, unmatched...)` reconstructs the slice. The existing `n`-cap slice expression at lines 84–86 remains untouched.
- **Integration Points:** `internal/verify/select.go` (core routing logic), `internal/registry/config.go` (new `Language` field, `validateAgent`, `applyDefaults`), `internal/verify/pipeline.go:162` (sole production caller — signature update only), `personas/testdata/` (fixtures for `idiomatic`/`sentinel`/`tracer` that will exercise the `language` field added here).
- **Design Reference:** This story extends the adversarial verification interface; see [Adversarial Verification Interface](../../../../specifications/design-concepts/adversarial-verification-interface.md) for CLI/MCP semantics and report rendering conventions.
- **Data Requirements:** `AgentConfig.Language` is `[]string` with YAML tag `language,omitempty`; nil and empty slice are semantically equivalent (no language constraint). The caller-supplied `scores map[string]float64` maps persona name to corroboration rate (0.0–1.0); nil map is safe and degrades to alphabetical tie-breaking within the matched partition.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `SelectEligibleSkeptics` signature change breaks callers outside `pipeline.go` that were added after the plan was written | Medium | Run `grep -r "SelectEligibleSkeptics" ./internal` before implementing to confirm the single-caller assumption; update any additional callers in the same commit |
| `normalizeExt` normalization mismatch between load-time (`applyDefaults`) and match-time (`SelectEligibleSkeptics`) produces false no-matches | High | Extract `normalizeExt` into a shared internal helper used by both paths; covered by a dedicated unit test with dot-prefixed and dotless inputs |
| Partition reorder introduces an allocation-per-call regression in hot paths | Low | Pre-allocate `matched` and `unmatched` slices with `make([]string, 0, len(names))` to avoid growth copies; benchmark with `go test -bench` if the verify pipeline shows measurable regression |
| Backward-compatibility break: existing registry files with no `language` key silently select the wrong skeptic set | Medium | Add a regression test loading a fixture registry without `language` fields and asserting that `SelectEligibleSkeptics` output matches the pre-routing alphabetical baseline |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
