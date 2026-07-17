---
id: mem-2026-07-16-30531a
question: "RED test harness pattern for review.go telemetry gate: reuse reconcile's TD-009 pattern, no new harness needed"
created: 2026-07-16
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, cmd/atcr/telemetry_gate_test.go, cmd/atcr/review_test.go, cmd/atcr/backend_contract_test.go]
tags: [clarifications, epic-28.0_telemetry_expansion_cloud_sync, testing]
retrievals: 0
status: active
type: clarifications
---

# RED test harness pattern for review.go telemetry gate: reuse

## Decision

To write a faithful RED test for cmd/atcr/review.go's post-gate deferred telemetry Send (mirroring reconcile.go's TD-009 pattern), no new test harness needs to be built. Compose existing pieces the same way TestReconcile_TelemetryStatus_ReflectsGateOutcome does: drive runReview directly (bypassing execCmd/root PersistentPreRunE) with an injected telemetry.Client plus telemetry.SetDoRequestForTest capture, a real git repo via initGitRepoWithChange, and an httptest LLM stub (mockFindingsServer / writeBackendContractConfig) so --fail-on has a real finding to gate on.

Justification:
- The reconcile TD-009 test pattern to mirror already exists at cmd/atcr/telemetry_gate_test.go:230-251, built on runReconcileGated (telemetry_gate_test.go:177-189); runReview has the identical signature `runReview(cmd *cobra.Command, _ []string) (err error)` (cmd/atcr/review.go:172).
- telemetry.SetDoRequestForTest (internal/telemetry/client.go:60-64) is documented as existing so tests in other packages can intercept sends across the package boundary without real networking.
- initGitRepoWithChange and writeReviewFixtureConfig (cmd/atcr/review_test.go:238-278), plus writeBackendContractConfig + mockFindingsServer (cmd/atcr/backend_contract_test.go:34-70), already provide a parameterized httptest base_url returning a real finding — exactly what's needed to make --fail-on trigger through fanout.ExecuteReview.
- Existing tests (cmd/atcr/review_test.go:646-705) already exercise full fanout end-to-end through a live git repo + registry config, proving this composition works without new plumbing.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- cmd/atcr/telemetry_gate_test.go
- cmd/atcr/review_test.go
- cmd/atcr/backend_contract_test.go
