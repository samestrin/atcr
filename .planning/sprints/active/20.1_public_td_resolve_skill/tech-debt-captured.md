# Tech Debt Captured — Sprint 20.1 (Public TD Resolve Skill)

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, tagged `SOURCE=execute-sprint`).

## TD-001 — Local-debt dedup freezes first-seen severity on escalation (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-07-12
**File:** cmd/atcr/reconcile.go:200
**Issue:** The reconcile persistence hook dedups by `history.FindingID(file,line,problem)`, which deliberately excludes severity (record.go:61-63). When an already-persisted finding is later re-reconciled at a HIGHER severity, the append-only store skips it as a duplicate, so the durable backlog freezes the first-seen severity and can under-prioritize an escalated item.
**Why accepted:** This is documented-as-intended (`StampID` / doc.go: stable id across re-settled severity keeps identity consistent with the rest of the codebase). No known consumer yet acts on backlog severity, and Story 3's `atcr debt resolve` selection can re-read live findings. Surfaced by the reviewer only as a request to confirm the design, not a defect.
**Fix in:** Future epic (post-20.1) — if escalation must surface, compare severity on a matched id and append an updated/annotated record when it rises; otherwise leave the documented first-seen-severity behavior and add an explicit note to the schema doc's "Identity and Deduplication" section.

## TD-002 — Local-debt persistence is CLI-only; MCP reconcile path skips it (MEDIUM)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-12
**File:** internal/mcp/handlers.go:344
**Issue:** The reconcile persistence hook (`persistLocalDebt`) is called only from the CLI `runReconcile` (cmd/atcr/reconcile.go:123). The MCP `atcr_reconcile` handler emits the scorecard through the shared `scorecard.EmitForReconcile` bridge but never persists to the local TD store, so reconciles driven via `atcr serve`/MCP accumulate zero `.atcr/debt/` records — an MCP-only user would see an empty backlog for Story 3's `atcr debt resolve`.
**Why accepted:** Out of Story 2's explicitly-scoped surface — AC 02-01 scopes the change to `runReconcile` (the CLI), and the epic's stated audience is standalone CLI/skill-dispatcher users (the skill routes to the CLI, not MCP). Adding MCP parity now would expand scope beyond the plan. The MCP path uses `Root:""`, so a shared bridge must first resolve a real repo root before `DefaultDir` would be safe (`DefaultDir("")` would write `.atcr/debt` under the server CWD).
**Fix in:** Follow-up epic — lift persistence into a shared bridge (like `scorecard.EmitForReconcile`) guarded on a resolved repo root, OR explicitly document the CLI-only scope in `internal/localdebt/doc.go` so a downstream dev is not misled by the "modeled on scorecard" parity language.

## TD-003 — `atcr debt` subcommands span two divergent backlogs (LOW)
**Origin:** Phase 2, task 2.8 gate review, 2026-07-12
**File:** cmd/atcr/debt.go:18
**Issue:** Within the one `atcr debt` command group, subcommands will target two different stores: `debt add`/`debt dashboard` read the private `.planning/technical-debt/`, while the new reconcile hook writes `.atcr/debt/` and Story 3's `debt resolve` will read `.atcr/debt/`. Sibling subcommands under one group operating on divergent backlogs is a UX surprise.
**Why accepted:** Intended by design per `internal/localdebt/doc.go` (public `.atcr/` audience vs private `.planning/` audience). Not a defect — a clarity gap.
**Fix in:** Story 3 / Story 5 (docs) — surface in `atcr debt --help` and `docs/skill-usage.md` which store each subcommand targets, so users are not surprised that `debt resolve` and `debt add` touch different backlogs. (Story 5 AC 05-03 already calls for public/private disambiguation — fold this in there.)

## TD-004 — Local-debt resolution is terminal; a re-detected finding never re-opens (MEDIUM)
**Origin:** Phase 3, task 3.5 gate review, 2026-07-12
**File:** cmd/atcr/debt_resolve.go:95
**Issue:** `selectOpenDebt` folds an id closed if ANY record for it ever carried a terminal status ("resolved wins forever"), ignoring recency. Combined with the Phase 2 reconcile hook's ID-dedup (`persistLocalDebt` at cmd/atcr/reconcile.go:205 counts resolved records in `seen`), a finding that is marked resolved and then re-detected in a later `atcr reconcile` run is skipped on append AND folded closed on read — so it can never re-enter the open backlog even if the fix regressed. There is no re-open path.
**Why accepted:** Not a Story-3 defect in isolation — `selectOpenDebt` correctly reflects the append-only store's current semantics, and a terminal resolution is a defensible design for an append-only ledger. A real re-open path requires changing Phase-2 committed/adversarially-reviewed `persistLocalDebt` dedup (build `seen` only from non-terminal records) AND the fold (latest-record-per-id wins) together — a Phase-3-only fold change would not solve it (the reconcile hook never appends a newer open record after resolution, so "latest wins" would just re-pick the resolved record). This is a cross-phase design decision out of Story 3's scope, and adjacent to TD-001 (same append-only + dedup-by-id "frozen state" root).
**Fix in:** Follow-up epic (post-20.1) — decide whether local-store resolution is permanently terminal (document it) or re-openable on re-detection (then: `persistLocalDebt` builds `seen` from non-terminal records only, and `selectOpenDebt` folds by latest ts per id). Story 5 (Phase 4 docs) should state the current terminal-resolution behavior explicitly so a standalone user is not surprised that a regressed fix stays off the backlog.

## TD-005 — Local-debt persist diverges from the gate's inclusion contract (out-of-scope / refuted findings persisted as open debt) (MEDIUM)
**Origin:** Phase 5, task 5.1 cumulative adversarial review, 2026-07-12
**File:** cmd/atcr/reconcile.go:158
**Issue:** `persistLocalDebt` writes every `res.JSONFindings()` record unconditionally, but the reconcile gate in the same `runReconcile` (`gateFindings` → `reconcile.IsFailing`, gate.go:97-106) explicitly EXCLUDES two classes: `category == out of scope` and skeptic-`REFUTED` findings. The two consumers of one `res` disagree — findings reconciliation itself deems non-actionable land in the local store and are surfaced by `atcr debt resolve --list` as open, actionable debt. Out-of-scope is set at reconcile time (no verify stage required), so this is reachable on a plain review→reconcile flow, not just a verified one.
**Why accepted:** MEDIUM per the sprint's adversarial policy (inline-fix CRITICAL/HIGH only; MEDIUM/LOW defer). The fix touches Phase-2-committed, already-adversarially-reviewed `persistLocalDebt`, and "what counts as persistable debt" is a design decision (mirror the gate exactly, or intentionally keep a superset backlog) better made deliberately than as a validation-phase hotfix. Mitigated downstream: the resolve cycle's pre-fix still-exists / clear-fix evaluation and `NEEDS_REVIEW` fallback catch a non-reproducing item before it is "fixed."
**Fix in:** Follow-up epic (post-20.1) — filter findings through the shared exclusion before `Append` (skip `strings.ToLower(f.Category) == reconcile.CategoryOutOfScope` and `f.Verification.Verdict == REFUTED`), mirroring `IsFailing`, so the store's open set matches what the gate considers real. Adjacent to TD-006 (both are `persistLocalDebt` Record-construction gaps).

## TD-006 — Local-debt Record drops PathWarning and Verification verdict on persist (LOW)
**Origin:** Phase 5, task 5.1 cumulative adversarial review, 2026-07-12
**File:** cmd/atcr/reconcile.go:181
**Issue:** The `localdebt.Record` built in `persistLocalDebt` copies `File`/`Line`/`Problem`/etc. but drops the JSONFinding's `PathWarning`/`PathSuggestion` (Epic 5.0 hallucinated-path validation) and the `Verification` verdict. A finding whose cited path was flagged non-existent is persisted with the bogus `File` and no warning, and a refuted finding loses its refutation signal — so `atcr debt resolve` can present a hallucinated location as ground truth (mitigated only by the skill's grep-then-`NEEDS_REVIEW` fallback) and cannot distinguish a disproven finding.
**Why accepted:** LOW per the sprint's adversarial policy. The `localdebt.Record` schema (v1) has no field for path-warning or verdict, so a real fix is a schema addition (`schema_version` bump) — out of scope for a validation-phase change. The skill's symbol-anchor grep + `NEEDS_REVIEW`-on-not-found path already prevents acting on a hallucinated location.
**Fix in:** Follow-up epic (post-20.1) — either skip path-warned findings on persist, or add `path_warning`/verdict fields to the v2 Record so the resolve surface can flag or exclude them. Pairs with TD-005 (same `persistLocalDebt` inclusion/fidelity surface).
