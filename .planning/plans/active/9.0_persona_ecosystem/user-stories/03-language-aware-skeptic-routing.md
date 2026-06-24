# User Story 3: Language-Aware Skeptic Routing

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** Go developer using ATCR with a language-scoped persona installed
**I want** the verification step to automatically prefer skeptics that understand Go idioms
**So that** domain-specific findings are challenged by the most contextually relevant reviewers, improving verification accuracy without any manual configuration

## Story Context

- **Background:** `SelectEligibleSkeptics` (`internal/verify/select.go:55`) currently sorts all candidates alphabetically before applying the n-cap, producing a deterministic but language-blind selection. When a Go-specific persona such as `idiomatic` is installed alongside generalist personas, the skeptic pool contains language-matched candidates that are never preferentially selected. T8 extends the function with a two-partition reorder — language-matched skeptics move to the front so the existing n-cap naturally prefers them. A nil `Language` field on a skeptic config means no constraint, preserving backward compatibility for all existing registry configurations. Corroboration scores from the scorecard are passed as a `map[string]float64` fourth parameter for tie-breaking within the matched partition, reusing the same map shape that T6's `scorecard.Aggregate()` produces.
- **Assumptions:** `AgentConfig` in `internal/registry/config.go` does not yet have a `Language` field. The sole production caller of `SelectEligibleSkeptics` is `internal/verify/pipeline.go:162`. Existing registry YAML files that lack a `language:` key must parse cleanly (nil slice = no constraint). The `idiomatic` persona introduced by T1 declares `language: [go]` and serves as the first real consumer of this routing.
- **Constraints:** The change must not alter behavior when no language-matched skeptic exists — silent fallback to the existing general pool is required. Determinism within each partition must be preserved (scores desc, then alphabetical). No new external imports are permitted; the implementation uses only stdlib (`strings`, `filepath`, `sort`). The `Language` field canonical form is without a leading dot and lowercased (e.g., `["go", "ts"]`), normalized at load time by `applyDefaults`.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None — T8 is the first task in Sprint A; T1 depends on T8 |

## Success Criteria (SMART Format)

- **Specific:** `SelectEligibleSkeptics` returns language-matched skeptics first when the finding's file extension matches at least one skeptic's `Language` entries; when no match exists the output is identical to current alphabetical behavior.
- **Measurable:** Three new test cases in `internal/verify/select_test.go` pass: (1) language-match prioritization, (2) no-match fallback (output unchanged), (3) tie-break by corroboration score within the matched partition. All existing tests in `./...` continue to pass.
- **Achievable:** The change is a contained two-partition reorder after `sort.Strings(names)` at `select.go:81`, a nil-safe fourth parameter on one function, and one updated production caller — estimated at under 80 lines of production code and 60 lines of test code.
- **Relevant:** Language-aware routing is the prerequisite for the `idiomatic` persona (T1) to deliver meaningful skeptic selection for Go files, directly enabling the vertical-market positioning of the persona ecosystem.
- **Time-bound:** Implemented, passing `go test ./...`, and verified-green before any T1 work begins, within Sprint A.

## Acceptance Criteria Overview

1. `AgentConfig` gains a backward-compatible `Language []string` field with `yaml:"language,omitempty"` tag; `applyDefaults` normalizes entries (trim space, strip one leading dot, lowercase); `validateAgent` rejects empty entries and control characters.
2. `SelectEligibleSkeptics` accepts a nil-safe `scores map[string]float64` fourth parameter and applies the two-partition reorder: matched skeptics (`Config.Language` overlaps `normalizeExt(filepath.Ext(finding.File))`) sorted by score descending then name ascending; unmatched sorted alphabetically; `append(matched, unmatched...)` fed to the existing n-cap.
3. The sole production caller at `internal/verify/pipeline.go:162` is updated to the four-parameter signature; all tests pass with `go test ./...`; existing registry YAML files without `language:` load without error.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** Insert the two-partition reorder immediately after `sort.Strings(names)` at `select.go:81`. The `normalizeExt` helper (`strings.ToLower(strings.TrimPrefix(ext, "."))`) is a package-private function in `internal/verify`. The scores map key is the reviewer name as stored in the registry. An absent key evaluates to `0.0` (Go zero value for `float64`) and falls through to alphabetical ordering. The matched partition is stable: sorted by `-scores[name]` (descending float, negated for `sort.Slice`), then by name ascending for equal scores.
- **Integration Points:** `internal/registry/config.go:AgentConfig` (field addition, `applyDefaults`, `validateAgent`); `internal/verify/select.go` (function signature + partition logic + `normalizeExt`); `internal/verify/pipeline.go:162` (updated call site, passes nil until T6 wires the scorecard map). Test fixtures reuse `testSkeptic` from `internal/verify/invoke_test.go:22`.
- **Data Requirements:** `AgentConfig.Language []string` with `yaml:"language,omitempty"` — follows the exact pattern of `Scope []string` at `internal/registry/config.go:267`. The `normalizeExt` function is the only new data transformation; no schema migration is required for existing YAML files.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Sorting instability between matched and unmatched partitions breaks determinism in tests | Medium | Sort each partition explicitly before concatenating; cover with a determinism test that calls `SelectEligibleSkeptics` twice with the same input and asserts identical output |
| `applyDefaults` normalization strips a leading dot from a legitimate directory-separator character in a language tag | Low | Normalization strips only a single leading dot using `strings.TrimPrefix`; a language value of `".go"` becomes `"go"`, which is the intended canonical form; values with embedded dots (e.g., `".tar.gz"`) are not valid language identifiers and are caught by `validateAgent` |
| T1 (`idiomatic` persona) commits before T8 is merged, causing a CI failure window | Medium | Sprint A ordering strictly requires T8 to reach a green `go test ./...` baseline before any T1 persona `.md` file or fixture is added; enforced in the sprint plan task order |
| The fourth `scores` parameter shape diverges from T6's `scorecard.Aggregate()` output, requiring caller churn | Low | Shape is pre-decided (2026-06-24): `map[string]float64` keyed by reviewer name, matching `LeaderboardRow.CorroborationRate`; T6 passes the map directly with no conversion |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
