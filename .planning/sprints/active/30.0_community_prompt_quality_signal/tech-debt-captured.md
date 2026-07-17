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

## TD-005 — `atcr init` template does not surface the quality_signal opt-in key (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-17
**File:** internal/registry/project.go:DefaultProjectConfigYAML
**Issue:** The `atcr init` config template renders a documented commented telemetry stanza but emits no quality_signal line or comment, so an operator who runs `atcr init` sees the telemetry knob but cannot discover quality_signal exists from the generated config — discoverability parity with the sibling opt-out key is broken.
**Why accepted:** Discoverability only; the key is fully functional and documented via `atcr config set --help` (shipped this phase) and docs/telemetry.md (Phase 6). Adding an init-template stanza is outside every Phase 2 task's file scope and would be speculative to fold in mid-phase.
**Fix in:** Post-sprint or a docs-phase follow-up — add a parallel commented `# quality_signal: false` stanza to DefaultProjectConfigYAML and extend the template's documentation test to assert it.

## TD-006 — quality-signal gate reads config cwd-relative while config-set writes repo-root (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-17
**File:** cmd/atcr/qualitysignal.go:qualitySignalGate
**Issue:** `qualitySignalGate()` reads `LoadQualitySignalSetting(".")` (cwd-relative) while `runConfigSet` persists via `repoRoot()`. A user who runs `atcr config set quality_signal true` from a subdirectory writes to `<root>/.atcr/config.yaml`, but a later `atcr review` from that same subdir has the gate read `<subdir>/.atcr/config.yaml`, miss the opt-in, and resolve disabled.
**Why accepted:** This is a faithful mirror of the pre-existing `telemetryGate` cwd-relative asymmetry (not a regression introduced here), and for the OPT-IN signal it fails to the safe/OFF direction — a missed opt-in never transmits. Structural independence from telemetry (the phase's core contract) is preserved.
**Fix in:** Post-sprint consistency pass — resolve the gate's config root via repo-root discovery (matching config-set and the roster loader), applied symmetrically to `telemetryGate` so both gates and their write paths agree on config location.

## TD-008 — `--preview` silently overrides action flags with no diagnostic (LOW)
**Origin:** Phase 3, task 3.5 gate review, 2026-07-17
**File:** cmd/atcr/review.go:runReview
**Issue:** The `--preview` short-circuit sits at the top of `runReview`, above the `--resume`/`--force` mutual-exclusion check and the `--auto-fix`/positional-arg handling. A user who combines `--preview` with `--resume`, `--force`, `--auto-fix`, or a positional review arg gets the preview payload and those flags are silently ignored with no stderr diagnostic. (Numbered TD-008, not TD-007, because TD-007 is the established `HashPersonaID` unsalted-hash caveat referenced throughout this sprint's docs.)
**Why accepted:** This is intended preview-precedence — `--preview` is a deliberate, side-effect-free inspection override that must never run a review, so honoring it first is correct. The only gap is discoverability (no "flag ignored" notice); no functional or privacy impact. Emitting warnings would be a UX-polish scope addition beyond Story 3's ACs.
**Fix in:** Post-sprint UX pass — either warn to stderr when `--preview` is combined with an action flag, or document the silent precedence in the `--preview` flag help text.
