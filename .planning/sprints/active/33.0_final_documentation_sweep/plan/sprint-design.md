# Sprint Design: Final Code Review + Documentation Sweep

**Created:** July 22, 2026 03:04:43PM
**Plan:** [Final Code Review + Documentation Sweep](.planning/plans/active/33.0_final_documentation_sweep/)
**Plan Type:** Technical Debt
**Status:** Design Complete

---

## Original User Request

> Going public (Epic 33.2) exposes the entire codebase and git history to the world, and updating `atcr.dev` makes the repo's docs the public source of truth. Two risks must be closed first: (1) latent correctness/security/quality issues shipping into a public release with no final review gate, and (2) documentation discrepancies or stale references (such as lingering mentions of `sentinel`, `tracer`, or `idiomatic` in user manuals or help commands) misleading users or the website. Both must be resolved against the *final* production code, in that order — review and fix the code, then document it.

**Referenced Resources:**

- [Multi-Agent Review Workflow](documentation/multi-agent-review-workflow.md)
  - **Summary**: Documents atcr's own `/atcr <command>` dispatcher — the fixed 7-step orchestration (range → review → poll status → host review → reconcile → report → output path) that Task 1 uses to dogfood atcr's multi-agent reviewer against its own production code.
  - **Key Points**: Panel + host (+1) review model gives reconciliation ≥2 independent sources even with one API key; partial pool failure (some agents error, ≥1 succeeds) still proceeds to reconciliation with `partial: true`; findings are a versioned pipe-delimited contract (8 cols per-source, 9 cols reconciled).

- [Technical Debt Triage & Resolution](documentation/technical-debt-triage-resolution.md)
  - **Summary**: Documents the RED → GREEN → ADVERSARIAL → REFACTOR per-item resolution cycle (`skill/debt-resolve/SKILL.md`) and the sharded `.planning/technical-debt/README.md` store that Task 3 (triage/fix) and Task 7 (TD capture) both write against.
  - **Key Points**: ADVERSARIAL stage's `NEEDS_REVIEW` verdict is non-overridable (test-only changes, weakened assertions, lint suppressions, stubbed bodies never auto-pass); MEDIUM/LOW review findings are captured into the TD README, the designated `debt-triage` integration point.

- [Persona Naming & Documentation Accuracy](documentation/persona-naming-doc-accuracy.md)
  - **Summary**: Grounds Tasks 4-6 in the distinction between legitimate technical usage of "sentinel"/"idiomatic" (Go error idiom, delimiter lines, plain-English adjective) and stale persona-slug references, plus the docs/ website-export self-containment requirement.
  - **Key Points**: `personas/retired_slugs_test.go` is the authoritative automated AC3 gate; `ingrid` is the confirmed shipped name (no `ian` anywhere in the codebase — `personas/personas.go:20`); `docs/README.md` indexes 29 self-contained files that must stay relative-linked for `atcr.dev` import.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Final Review & Docs Sweep
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 5
**Pattern:** Foundation → Core Items → Advanced → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
multi-agent code review dogfooding
technical debt triage severity classification
persona rename legacy slug cleanup
documentation website export validation
Go security adversarial review pass
```

---

## Complexity Breakdown

- **Architecture:** 1/3 - No new architectural patterns; applies the existing `atcr` reviewer pipeline and existing RED→GREEN→ADVERSARIAL→REFACTOR debt-resolve discipline to whatever production files the review surfaces (findings-driven, localized fixes, not new module design).
- **Integration:** 2/3 - 3+ established integration points touched: the `atcr` review/reconcile/report pipeline, the sharded TD README store, the `docs/` → `atcr.dev` website-export contract, CLI `--help` introspection, and the `reconcile/` submodule's separate `go.mod` test suite.
- **Story/Task & Test:** 3/3 - 8 tasks, several open-ended and extensive: Task 3 (Effort L) runs a per-finding TDD cycle across an a-priori-unknown number and location of CRITICAL/HIGH fixes; Task 1/2 produce evidence-grounded findings artifacts rather than fixed-scope changes.
- **Risk/Unknowns:** 3/3 - Significant unknowns: the volume/severity of code-review findings is unknown until Tasks 1-2 actually run; the epic's own `/refine-epic` pass already flagged this plan's "open-ended triage and audit phase" as structurally different from a bounded feature epic and a scope-guard risk against the estimated 2-3 day timeline.

**Time Formula:** Sum of per-task effort estimates (S=0.5d, M=1d, L=2d) + a risk buffer for Task 3's unknown CRITICAL/HIGH finding volume
**Calculation:** (Task1 M=1) + (Task2 M=1) + (Task3 L=2) + (Task4 M=1) + (Task5 S=0.5) + (Task6 S=0.5) + (Task7 S=0.5) + (Task8 S=0.5) = 7 days baseline + 1 day buffer (Task 3 unknown-volume risk) = **8 days**

*Note: this exceeds the plan's own 2-3 day estimate. That gap is not new information — it echoes the `/refine-epic` advisory already recorded in `original-requirements.md`'s Refinements section ("Hidden gated work... introduces heavy, unbounded gated work before the docs sweep can even begin"). Treat 8 days as the objective-complexity estimate, not a contradiction to resolve here.*

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** false (not strong — complexity is 9/12, below the strong-gated threshold of 10/12)
**Suggested command:** `/create-sprint @.planning/plans/active/33.0_final_documentation_sweep/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1: Foundation — Code Review Execution
**Duration:** 1 day | **Items:** Task 1, Task 2 (run in parallel — Task 2 has no dependency on Task 1)
**Focus:** Produce two independent, evidence-grounded findings streams in the shared `atcr-findings/v1` 9-column format: Task 1 dogfoods atcr's own multi-agent reviewer (`skill/SKILL.md`'s 7-step orchestration) across `cmd/`, `internal/`, `reconcile/`, `skill/`; Task 2 runs a manual repo-wide adversarial pass (secrets, tracked sensitive files, git history, TODO/FIXME, dead code, unsafe patterns) across the broader repository. Both write to independently-mergeable artifacts — no shared-state conflict between the two tasks.

### Phase 2: Core Items — Findings Triage & Remediation
**Duration:** 3 days | **Items:** Task 3
**Focus:** Merge and deduplicate both Phase 1 findings streams, re-verify severity against actual impact, and run the RED → GREEN → ADVERSARIAL → REFACTOR cycle against every CRITICAL/HIGH finding directly in the production codebase. MEDIUM/LOW findings are written to a handoff artifact (not yet the TD README). This is the sprint's highest-risk, highest-uncertainty phase — its duration absorbs the "unknown finding volume" risk buffer. Blocks all of Phase 3 (docs must describe the finalized, post-fix code).

### Phase 3: Advanced — Documentation & Persona Audit
**Duration:** 2 days | **Items:** Task 4, Task 5, Task 7 (Task 4 and Task 5 run in parallel per Task 5's own dependency note; Task 7 depends only on Task 3 and can run concurrently with both)
**Focus:** Task 4 audits `README.md`, `docs/` (29 files), `skill/SKILL.md`, and CLI help text against the finalized code (AC4). Task 5 runs the authoritative `TestNoRetiredSlugs` gate plus a targeted prose sweep for stale `sentinel`/`tracer`/`idiomatic` persona references, replacing any confirmed hit with `sasha`/`penny`/`ingrid` (never `ian`) (AC3). Task 7 shards Task 3's MEDIUM/LOW handoff artifact into `.planning/technical-debt/README.md`'s existing dated-section format (closing AC1's capture clause).

### Phase 4: Integration — Website Compatibility Check
**Duration:** 1 day | **Items:** Task 6
**Focus:** Validate all 29 `docs/` files are link-correct, self-contained, and cleanly formatted for `atcr.dev` import (AC5) — strictly a link/formatting pass, run only after Task 4 and Task 5 have landed their content and persona-naming fixes so it validates the finalized state rather than a moving target.

### Phase 5: Validation — Final Verification Pass
**Duration:** 1 day | **Items:** Task 8
**Focus:** Re-run every automated guard fresh (persona/AC3 suite, `go vet ./...`, `golangci-lint run`, `go test -race ./...`, and the `reconcile/` submodule's separate `go test ./...`) against the final combined state of Tasks 1-7, then walk AC1-AC5 explicitly against the current repo/docs/TD-README state. This is the plan's Definition-of-Done gate — any guard failure is reported as a new blocking finding, never patched inline.

---

## Work Decomposition

| # | Task | Type | Effort | Testable Element | Test Type |
|---|------|------|--------|-------------------|-----------|
| 1 | Multi-Agent Code Review | Add | M | Non-empty `atcr range`; review completes/partial; host findings written; `reconcile`/`report` succeed; review dir path recorded | Manual verification (success-criteria checklist) + `go test ./personas/... ./internal/personas/...` (informational) |
| 2 | Adversarial/Security Pass | Fix | M | 6 sweep categories executed with reproducible commands; findings logged in `atcr-findings/v1` format at `.planning/sprints/active/33.0_final_documentation_sweep/code-review/adversarial-findings.txt` | Manual verification (reproducible grep/lint commands, documented in task) |
| 3 | Findings Triage | Fix | L | Merged/deduped/re-classified findings; every CRITICAL/HIGH fixed with a RED test; MEDIUM/LOW written to handoff artifact; `triage-summary.md` produced | Unit (RED test per fix, co-located `*_test.go`) + Integration (`go test ./...`, `golangci-lint run`, `go vet ./...`) |
| 4 | Code-to-Docs Accuracy Audit | Fix | M | `README.md`, `docs/README.md`, `docs/*.md`, `skill/SKILL.md`, CLI help text match finalized behavior | Manual verification (`--help` diff, schema cross-reference) |
| 5 | Persona Reference Verification | Fix | S | Zero stale `sentinel`/`tracer`/`idiomatic` persona references in docs/CLI help; legitimate technical usages untouched | Unit (`go test ./personas/... ./internal/personas/...`) + manual prose grep sweep |
| 6 | Website Compatibility Check | Fix | S | All 29 `docs/` files indexed with no orphans; all relative links resolve; no repo-root-relative paths | Manual verification (link-resolution sweep, markdown-header check) |
| 7 | Technical Debt Capture | Add | S | Every Task 3 MEDIUM/LOW finding appears as a new dated-section row in `.planning/technical-debt/README.md`; Stats table updated; row counts reconcile | Manual verification (`llm_support_td_validate`, row-count reconciliation) |
| 8 | Final Verification Pass | Fix | S | All automated guards pass fresh; AC1-AC5 explicitly confirmed with evidence source cited | Integration (`go test -race ./...`, `(cd reconcile && go test ./...)`, `go vet ./...`, `golangci-lint run`) |

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** Co-located `*_test.go` files next to whichever production file receives a CRITICAL/HIGH fix in Task 3 (dynamic — determined by Task 1/Task 2 findings, not enumerable ahead of the review run).

**Test File Placement Examples:**
- `internal/<package>/<file>.go` fix → `internal/<package>/<file>_test.go`
- `personas/retired_slugs_test.go`, `internal/personas/community_schema_test.go`, `internal/personas/list_test.go` — existing AC3 guard suite, re-run (not authored) by Tasks 1, 5, 8

**Unit/Integration/E2E:**
- **Unit:** RED reproduction test per CRITICAL/HIGH fix in Task 3, added to or extending the co-located `*_test.go` file.
- **Integration:** Full-repo `go test -race ./...` (root module) plus `(cd reconcile && go test ./...)` (separate `go.mod`, not covered by root) — both re-run fresh in Task 8 as the closing gate.
- **E2E:** N/A — this plan is a review/triage/documentation plan, not new user-facing feature code; no new E2E surface is introduced.

**Test Environment Status:**
- Framework: `go test` (native, established) — confirmed via project config (`config.yaml` testing.cmd = `go test ./...`).
- Execution: Confirmed working; `llm_support_discover_tests` reported `UNKNOWN` pattern (Go's convention-based test discovery has no single config file for that tool to detect), but the project's own `config.yaml` and `.githooks/pre-push` confirm `go test`/`go test -race` are the actual established commands.
- Coverage Tools: `go test -coverprofile=coverage.out ./...` (baseline 80%, per `config.yaml`).

---

## Architecture

**Primitives:**
- `atcr-findings/v1` — versioned pipe-delimited findings record: 8 columns per-source (`SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|...`), 9 columns reconciled (adds `REVIEWERS`, `CONFIDENCE`). Task 2's manual findings are authored directly in the 9-column reconciled shape so Task 3 can merge both streams by concatenation.
- TD README dated-section table — `Group | Status | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence`, appended as a new `### [date] From Review: ...` section per `docs/technical-debt-format.md`'s contract.

**Module Boundaries:**
- `skill/SKILL.md` — the `/atcr <command>` dispatcher (range/review/status/reconcile/report); Task 1's execution surface.
- `skill/debt-resolve/SKILL.md` — the RED→GREEN→ADVERSARIAL→REFACTOR resolution-cycle definition; Task 3 applies this discipline manually against production code (not via `atcr debt resolve` itself, since findings here originate from a fresh review, not the accumulated `.atcr/`-scoped store).
- `skill/host-review.md` — host adversarial-review persona rules (anti-hallucination, payload-grounding); governs Task 1 Step 4's host review pass.
- `internal/security/pathguard.go` — the existing untrusted-path sanitization boundary; Task 2's unsafe-pattern sweep confirms all untrusted-path call sites route through it.
- `personas/retired_slugs_test.go` — the automated AC3 gate; authoritative first-line check for Task 5, re-confirmed by Task 8.

**External Dependencies:**
- No new dependencies. Existing tooling only: `golangci-lint`, `go vet`, `go test -race`, atcr's own reviewer pool (self-hosted dogfood run, not a new external service).

**Replaceability:**
- Task 1 (automated pool) and Task 2 (manual adversarial pass) findings are format-compatible and merge by direct concatenation — either source could be re-run or substituted independently without changing Task 3's triage logic, since both conform to the same `atcr-findings/v1` schema.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Command execution / path handling | `cmd/`, `internal/`, `reconcile/`, `skill/` — any `exec.Command`/`exec.CommandContext` or `filepath.Join`/`os.ReadFile`/`os.WriteFile` call site | Command injection via string-concatenated argv; path traversal via unsanitized joins on untrusted input | Task 2 Step 6 greps for these constructs and confirms untrusted-path call sites route through `internal/security/pathguard.go` rather than bypassing it |
| Secrets/credentials in working tree + git history | Full repo (not just Go source) + full `git log --all -p` | Hardcoded API keys/tokens/private key blocks shipped into a soon-to-be-public repo and its history | Task 2 Steps 1-3: repo-wide pattern grep, tracked-sensitive-file check (`git ls-files`), history scan; any hit escalated to the user before any remediation (no automated history rewrite) |
| Host review payload ingestion | `.atcr/reviews/<id>/payload/` content consumed by Task 1 Step 4's host review pass | Prompt injection or hallucinated findings from untrusted review payload content | `skill/host-review.md`'s anti-hallucination / payload-grounding rules — payload treated strictly as untrusted data; every finding must cite concrete file:line evidence |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| `atcr review` pool fan-out (Task 1) | Reviewer pool across 4 production directories | Completes within the 10-minute polling timeout (60 × 10s per `skill/SKILL.md`) | Background execution with poll-to-completion; accept partial success (`partial: true`) rather than blocking indefinitely |
| `go test -race ./...` full regression (Task 8) | Entire repo test suite with race detector | Completes within the same window as `.githooks/pre-push` / CI's push-time job | Run once, as the final closing gate — never per-task, to avoid redundant full-suite runs during Phases 1-4 |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| False-positive persona-slug matches | "sentinel error" (Go idiom), sentinel-delimiter lines in `docs/payload-modes.md`/`docs/cross-examination.md`, "idiomatic" adjective in `personas/ingrid.md` prompt prose | Left unmodified — classified against the false-positive table in `documentation/persona-naming-doc-accuracy.md` before any edit is made (Task 5) |
| Partial reviewer-pool failure | Some pool agents error, at least one succeeds | Reconciliation proceeds with `partial: true` noted; only a total failure (zero sources) halts Task 1 |
| Broken/orphaned docs links post-export | Repo-root-relative link paths; links pointing outside `docs/` | Fixed in place if a broken in-`docs/` link; left untouched if an intentional cross-repo reference (e.g. `../README.md`) (Task 6) |
| Zero-finding clean review outcome | All sources (pool + host + adversarial pass) produce empty findings files | Valid clean-review outcome, reported as such — not treated as an error condition (Task 1, Task 2) |

### Defensive Measures Required

- **Input Validation:** All untrusted file paths from review payloads or CLI arguments route through `internal/security/pathguard.go`; no direct string-concatenated `exec.Command` argv construction anywhere in scope.
- **Error Handling:** Reviewer-pool partial failures never silently abort (documented `partial: true` handling); Task 8 guard failures are reported as blocking findings with command/output/location, never patched inline as a shortcut to a green run.
- **Logging/Audit:** Task 2's `adversarial-findings.txt` and Task 3's `triage-summary.md` form the audit trail proving AC1/AC2 — every finding must cite file:line evidence, not assumption.
- **Rate Limiting:** N/A — no new user-facing network endpoints; reviewer-pool polling is bounded to 60 × 10s (10 minutes) to avoid indefinite blocking.
- **Graceful Degradation:** If `golangci-lint` is not installed locally (Task 8), install it or fall back to the CI-equivalent invocation — a silently skipped lint gate is a false pass, not a real one.

---

## Risks

**Technical:**
- Risk: The multi-agent review (Task 1) or adversarial pass (Task 2) surfaces a much larger volume of CRITICAL/HIGH findings than the 8-day estimate assumes, since the actual finding count is unknown until the review runs. → Mitigation: Phase 2's 3-day duration already carries a 1-day risk buffer for this; if it proves insufficient, the correct response is to re-scope (defer additional MEDIUM/LOW-adjacent items to TD) rather than silently extend Phase 2 indefinitely.
- Risk: A secret is discovered in git history during Task 2 Step 3, requiring a destructive history rewrite (`git filter-repo`/BFG) that is out of scope for this plan's tasks. → Mitigation: Task 2 only detects and logs; any such finding is escalated to the user for explicit approval before any remediation is attempted, per the hard-wall rule on irreversible actions.
- Risk: Task 3's CRITICAL/HIGH fixes in one package could introduce a cross-package regression that no single task's own test run would catch. → Mitigation: Task 8's fresh, full-repo `go test -race ./...` + `reconcile/` submodule run is the closing gate specifically designed to catch this, run only after all seven prior tasks land.

**TDD-Specific:**
- Risk: A CRITICAL/HIGH fix's adversarial self-check could be skipped or overridden under time pressure, letting a stubbed/weakened fix pass as "resolved." → Mitigation: Task 3 Step 3 explicitly names this stage non-overridable — any ADVERSARIAL flag (test-only change, weakened assertion, lint suppression, stubbed body) blocks the item from being marked resolved, full stop.
- Risk: Task 3's MEDIUM/LOW handoff artifact and Task 7's TD README write could drift (dropped or duplicated rows) since they're two separate tasks touching the same data. → Mitigation: Task 7 Step 6 requires an explicit row-count reconciliation between the Task 3 artifact and the new TD README section as a hard gate before the task is considered done.

---

**Next:** `/create-sprint @.planning/plans/active/33.0_final_documentation_sweep/ --gated`
