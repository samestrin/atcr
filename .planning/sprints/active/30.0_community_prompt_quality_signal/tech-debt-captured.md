# Tech Debt Captured — Sprint 30.0 Community Prompt Quality Signal

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review`
Phase 1 and pre-seeded into the adversarial TD stream (SOURCE=execute-sprint).

---

## TD-001 — Schema v1→v2 bump makes new records invisible to pre-30.0 binaries (MEDIUM)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-17
**File:** internal/localdebt/record.go:19
**Issue:** Bumping SchemaVersion 1→2 for a purely additive omitempty Model field means a pre-30.0 binary (SchemaVersion==1) skips new v2 records as "unsupported schema_version 2" and drops them from the backlog on a downgrade or mixed-version usage — even though an old reader would structurally read the record and simply ignore the unknown model key. Prior additive fields (Justification, SourceReport, Status, ResolvedAt) were added omitempty with no version bump.
**Why accepted:** The 1→2 bump is an explicit, pinned sprint contract (AC 01-02, sprint-design, and TestRecord_SchemaVersionIsTwo assert it) — overriding it here would contradict the ratified plan. The downgrade-drops-records scenario is low-probability (requires downgrading atcr after opting into v2).
**Fix in:** Post-sprint / a future schema-policy pass — either document that v2 records are intentionally invisible to pre-30.0 binaries (accepted downgrade loss), or revisit whether additive omitempty fields warrant a version bump at all in localdebt's forward-incompat-skip model.

## TD-002 — Cross-model merged findings do not contribute to per-model signal (LOW)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-07-17
**File:** cmd/atcr/reconcile.go:resolveRecordModel
**Issue:** A finding flagged by two personas that ran on DIFFERENT models (a cross-model merged consensus finding) cannot be faithfully attributed by the single Record.Model field, so it is marked attribution-incomplete (Model="") and excluded from per-(persona, model) rows. Its dismissal/confirmation outcome therefore does not contribute to any per-model signal.
**Why accepted:** Record.Model is singular by design (schema v2). Excluding the ambiguous case is honest (it never mis-credits a model a persona did not run); same-model merges and single-reviewer findings — the common case — are attributed correctly. Every Story 1 AC is satisfied.
**Fix in:** Future enhancement — if per-model fidelity across mixed-model merges matters, attribution would need a per-(reviewer, model) shape (e.g. a persona->model map on Record) rather than a single Record.Model field.
**Resolved:** 2026-07-17 — The original MIS-ATTRIBUTION corruption (first-reviewer's model wrongly credited to every persona), which the Phase 1 gate (1.11) escalated to HIGH, is FIXED: resolveRecordModel now returns "" when reviewers span 2+ distinct models, so no wrong (persona, model) row is ever emitted. The residual above (cross-model consensus excluded, not attributed per-persona-model) remains a LOW future enhancement.

## TD-004 — Known+unrecorded-model merge credits a persona to a model it may not have run (MEDIUM)
**Origin:** Phase 1, task 1.11 gate re-review, 2026-07-17
**File:** cmd/atcr/reconcile.go:resolveRecordModel
**Issue:** When a merged finding has one reviewer with a recorded pool-summary model and another reviewer whose model is unrecorded (empty), resolveRecordModel skips the empty one and returns the sole known model; AggregateQualitySignal then credits that model to BOTH personas — including the one whose model is unknown. This is the same "guess a model a persona may not have run on" failure family the cross-model fix avoids, left open for the unrecorded-model case. Reachable only on a degraded pool summary with a missing model field, and identical to the pre-fix behavior (not a regression).
**Why accepted:** A correct fix requires knowing each persona's model at aggregation time — the single Record.Model field cannot express it, so this needs the same per-(reviewer, model) schema enhancement deferred in [[TD-002]]. Excluding whenever any reviewer is unresolved would over-exclude legitimate merges involving a no-model "host" reviewer. Uncommon path; all Story 1 ACs pass.
**Fix in:** Bundle with TD-002's per-persona-model enhancement — attribute each persona only to its own known model, dropping personas with an unresolved model rather than borrowing a sibling's.

## TD-003 — FoldRecords collapses records sharing an empty ID (LOW)
**Origin:** Phase 1, task 1.5.A adversarial review, 2026-07-17
**File:** internal/localdebt/store.go:231
**Issue:** FoldRecords (reused by foldTerminalByID) keys its fold on Record.ID; distinct records that both carry ID=="" would collapse into one group and silently lose all but one. Not reachable today because StampID always yields a non-empty content hash, but neither FoldRecords nor foldTerminalByID guards against a hand-written/legacy empty-ID record.
**Why accepted:** Unreachable via the normal write path (every persisted record is StampID'd to a non-empty hash); adding a guard now would be speculative hardening against an input the pipeline never produces.
**Fix in:** Post-sprint robustness pass — skip or warn on r.ID=="" before folding in FoldRecords, or document the always-stamped invariant on the fold contract.
