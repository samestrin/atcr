# User Story 1: Skeptic Selection & Role Plumbing

**Plan:** [3.0: Adversarial Verification](../plan.md)

## User Story

**As a** platform operator configuring atcr for adversarial verification
**I want** skeptics registered in `registry.yaml` with `role: skeptic` to be discoverable, filterable by role, and subject to a different-model enforcement rule per finding
**So that** the verification pipeline has a trustworthy foundation — only eligible skeptics (different model from any credited reviewer) are selected, and the rest of Epic 3.0 can build on a clean role-based agent lookup API

## Story Context

- **Background:** The registry (`internal/registry/config.go`) already defines three reserved role constants — `RoleReviewer`, `RoleSkeptic`, `RoleJudge` — and validates them via `roleValid()` at load. The `AgentConfig.Role` field is parsed and validated but inert: no code path filters agents by role or treats skeptics differently from reviewers. The `Verification` struct is reserved at `internal/reconcile/emit.go:36` and the `JSONFinding` carries `Reviewers []string` (needed for the different-model rule) and `*Verification` (omitempty, populated on write). What is missing is the selection layer: a way to query agents by role and enforce per-finding eligibility.
- **Assumptions:**
  - The registry YAML already supports `role: skeptic` on agent entries (validated since 1.x load).
  - The Epic 2.0 tool loop (`internal/llmclient`) and transcript infrastructure are available for skeptic invocation in later stories; this story only needs selection, not invocation.
  - `JSONFinding.Reviewers` reliably lists the agent names credited on each finding (produced by the reconciler).
- **Constraints:**
  - No changes to the on-disk registry YAML schema — `role` is already a valid field.
  - Must not break existing 1.x/2.0 configs where `role` is absent (empty string is valid and currently means "no role assigned yet").
  - All new logic must be unit-tested with table-driven tests matching existing registry patterns.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None (foundational — all subsequent Epic 3.0 stories depend on this) |

## Success Criteria (SMART Format)

- **Specific:** A `AgentsByRole(role string) map[string]AgentConfig` method (or equivalent) is added to `Registry`, returning all agents matching a given role. A `SelectEligibleSkeptics(finding JSONFinding, n int) []AgentConfig` function filters skeptics for a finding by the different-model rule and returns up to `n` candidates. If no eligible skeptic exists, the function returns an empty slice and the caller produces `verdict="unverifiable", notes="no_eligible_skeptic"`.
- **Measurable:** (1) `AgentsByRole(RoleSkeptic)` returns only agents with `role: skeptic` from a mixed registry. (2) Different-model rule rejects every skeptic whose `Model` matches any reviewer's model on the finding. (3) `go test ./internal/registry/... ./internal/verify/...` passes with >= 95% coverage on new code paths. (4) `go vet` and existing CI checks remain clean.
- **Achievable:** The role constant and field already exist; this is additive plumbing (filter function + eligibility check), not a schema migration.
- **Relevant:** Without role-based filtering and the different-model rule, the skeptic stage cannot function — every subsequent story (skeptic invocation, verdict parsing, confidence v2, gate integration) depends on this API.
- **Time-bound:** Expected to complete within the first week of the 3-4 week epic.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-agentsbyrole-filtering.md) | AgentsByRole Filtering | Unit |
| [01-02](../acceptance-criteria/01-02-different-model-exclusion.md) | Different-Model Exclusion Rule | Unit |
| [01-03](../acceptance-criteria/01-03-empty-selection-unverifiable.md) | Empty Selection and Unverifiable Verdict Contract | Unit |
| [01-04](../acceptance-criteria/01-04-empty-role-backward-compat.md) | Empty-Role Backward Compatibility | Unit |
| [01-05](../acceptance-criteria/01-05-test-coverage-requirements.md) | Comprehensive Table-Driven Test Coverage | Unit |

## Original Criteria Overview

1. `Registry.AgentsByRole(role)` returns a filtered map of agents matching the given role constant; returns empty map for unknown roles.
2. `SelectEligibleSkeptics(finding, n)` enforces the different-model rule: a skeptic sharing `Model` with any entry in `finding.Reviewers` is excluded.
3. When no eligible skeptic exists (no skeptics registered, or all share models with reviewers), the result is an empty selection — callers map this to `verdict="unverifiable", notes="no_eligible_skeptic"`.
4. Empty-role agents (1.x configs with no `role` field) are treated as `RoleReviewer` by `AgentsByRole` — backward compatible with existing registries.
5. Table-driven unit tests cover: mixed-role registry filtering, different-model exclusion, no-eligible-skeptic edge case, empty-role defaulting, and n-selection with fewer candidates than requested.

## Technical Considerations

- **Implementation Notes:**
  - Add `AgentsByRole(role string) map[string]AgentConfig` to `internal/registry/config.go` (method on `*Registry`). Empty `Role` is normalized to `RoleReviewer` before comparison so 1.x agents without an explicit role are included when filtering for reviewers but never when filtering for skeptics.
  - Add `SelectEligibleSkeptics(finding reconcile.JSONFinding, n int) []AgentConfig` to a new file `internal/verify/select.go` (the `verify` package). This function: (1) calls `reg.AgentsByRole(registry.RoleSkeptic)`, (2) builds a set of reviewer models by resolving each reviewer name to its `AgentConfig.Model`, (3) excludes skeptics whose model is in that set, (4) returns up to `n` candidates (ordering: deterministic by agent name for reproducibility).
  - The `verify` package is new in Epic 3.0; this story creates it with the selection subpackage only. Later stories add invocation, verdict parsing, and pipeline orchestration.
  - Model comparison is exact string match on `AgentConfig.Model` — no aliasing or family matching. This is intentional: the rule prevents same-model correlation, not same-provider correlation.
  - A reviewer name that does not resolve to a registered agent is skipped silently (defensive — reconciler output may reference agents removed from the registry between runs).
- **Integration Points:**
  - `internal/registry/config.go` — `RoleSkeptic` constant, `AgentConfig` struct, `Registry` type.
  - `internal/reconcile/emit.go` — `JSONFinding.Reviewers` (input to eligibility check), `Verification` struct (output shape used by callers).
  - New `internal/verify/` package — created in this story; later stories add `invoke.go`, `verdict.go`, `pipeline.go`.
- **Data Requirements:**
  - No schema changes to `registry.yaml` or `findings.json`.
  - No new configuration keys — `verify.votes` and `verify.min_severity` are introduced in later stories.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Empty-role agents misclassified as skeptics | High — could select a reviewer-agent as skeptic, violating the independence guarantee | Empty Role defaults to `RoleReviewer` in `AgentsByRole`; skeptics require explicit `role: skeptic`. Test explicitly for this. |
| Reviewer name in `finding.Reviewers` not found in registry (agent removed between runs) | Medium — model lookup fails, different-model rule silently weakened | Skip unresolvable reviewer names; log a warning. Document this behavior. The skeptic still passes the rule if the removed reviewer's model is unknown. |
| All skeptics share a model with at least one reviewer on a finding | Medium — finding becomes `unverifiable` instead of verified | This is correct behavior per the plan (independence guarantee). The `no_eligible_skeptic` reason makes the cause visible. Operators can add skeptics with diverse models. |
| New `internal/verify/` package creates import cycle with `internal/reconcile` | Medium — build failure | The `verify` package imports `reconcile` (for `JSONFinding`), but `reconcile` must not import `verify`. Verify this with `go build ./...` after initial scaffolding. |

---

**Created:** June 14, 2026 09:06:20AM
**Status:** Ready for Implementation
