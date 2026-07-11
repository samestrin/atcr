# Sprint Design: Community Prompt Submissions (Intake & Curation)

**Created:** July 10, 2026 12:01:49PM
**Plan:** [Community Prompt Submissions (Intake & Curation)](.planning/plans/active/19.9_community_prompt_submissions/)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> Establish a **GitHub-native** intake and curation flow so users contribute refined, battle-tested reviewer prompts back to the canonical library — turning "I improved my local copy" into "I opened a PR" in one command — **without** building a marketplace, website, or hosted registry (explicitly ruled out in Epic 19.6). Proposed Solution: (1) `atcr personas submit <name>` runs the fixture gate locally (reusing `personas.TestPersona`/`TemplateFixtureRunner`) and opens a fork+PR to `samestrin/atcr` via `gh` — fixture-gated, human-reviewed, no new hosting. (2) A two-tier curation model via a `submitted` status, orthogonal to the existing `Source` (`built-in`|`community`) provenance field: a submission lands `submitted` (fixture-passing but unvetted) until a maintainer graduates it into the vetted `personas/community/` library.

**Referenced Resources:**

- [GitHub Fork + PR Integration via go-gh](documentation/gh-fork-pr-integration.md)
  - **Summary**: Documents how `personas submit` shells out to the `gh` CLI (via `github.com/cli/go-gh/v2`) to fork `samestrin/atcr`, push a branch, and open a PR under the invoking user's own GitHub identity.
  - **Key Points**: `gh.ExecContext`/`gh.Path()` inherit the user's existing `gh auth login` session (no atcr-managed tokens); `internal/ghaction.Client` is architecturally the wrong fit (fixed-bot-token, single-repo REST client for Epic 17.0's --auto-fix flow).
- [Cobra Subcommand & Injectable-Seam Conventions](documentation/cobra-subcommand-patterns.md)
  - **Summary**: Describes the existing `newPersonas<Verb>Cmd()` registration pattern in `cmd/atcr/personas.go` and the package-var injectable-seam convention (`personasDir`, `personasClient`, `personasFixtureRunner`).
  - **Key Points**: `submit` becomes a seventh subcommand added the same way as `install`/`list`/`search`/`remove`/`test`/`upgrade`; any new external dependency (the `gh` wrapper) must be an injectable seam, not a bare inline call.
- [Local Fixture-Gate Reuse (TestPersona)](documentation/fixture-gate-reuse.md)
  - **Summary**: Documents the existing `TestPersona`/`TemplateFixtureRunner` fixture gate (`internal/personas/test.go`) and the name/path validation helpers `submit` must reuse before any GitHub call.
  - **Key Points**: Zero-network, zero-LLM local render check; `submit` must call this exact gate rather than reimplementing fixture logic, and must validate the name/path first via `validatePersonaName`/`personaPath`.
- [Status/Provenance Separation and Atomic Persistence](documentation/status-provenance-and-atomic-writes.md)
  - **Summary**: Explains why the new `submitted` status must stay orthogonal to the existing `Source` field, and which atomic-write helpers to reuse for any status marker.
  - **Key Points**: `Source` already carries three de facto values (`built-in`/`community`/`project`); `submitted` must never become a fourth. Any marker write must use `writeFileAtomic` or `atomicfs.WriteFileAtomic`, never a bare `os.WriteFile`.
- [Personas Install & Authoring Doc Updates (AC4)](documentation/personas-docs-updates.md)
  - **Summary**: Specifies the required edits to `docs/personas-install.md` and `docs/personas-authoring.md` for the new `submit` subcommand and the `submitted` → graduated curation model.
  - **Key Points**: `docs/personas-install.md` heading changes from "six" to "seven subcommands"; `docs/personas-authoring.md` gains a cross-reference from its existing contribution checklist plus a new two-tier-model section.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Community Prompt Submissions
**Complexity:** 9/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 5
**Pattern:** Foundation → Core Items → Advanced → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Cobra CLI injectable seam pattern
gh CLI fork PR Go integration
atomic file write Go TOCTOU
status field orthogonal to provenance
GitHub bot token vs user OAuth flow
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - Follows the existing cobra/injectable-seam conventions throughout, but introduces two genuinely new concepts to the codebase: the first `gh`-CLI shell-out integration point, and a new status/marker concept (`submitted`) that must stay structurally separate from the existing `Source` field.
- **Integration:** 2/3 - Three-plus integration points: the existing `TestPersona`/`TemplateFixtureRunner` fixture gate, the new `gh`/`go-gh` fork+PR mechanism, the atomic-write persistence layer for the submitted marker, and (optionally) the `personas list` extension point.
- **Story/Task & Test:** 3/3 - 5 user stories, 15 acceptance criteria, including one AC (02-02, fork/branch/push/PR-create sequencing) explicitly flagged High complexity in test-planning-matrix.md.
- **Risk/Unknowns:** 2/3 - First-of-its-kind external CLI integration (`gh`) with real preconditions (PATH, auth) and a real external system (GitHub); risks are well-identified and mitigated in plan.md, but the integration itself is new to this codebase.

**Time Formula:** `TOTAL_DAYS = Σ(phase_estimate)`, phase estimates weighted by story effort (S≈1–1.5d, M≈2–2.5d) and given a dedicated Validation phase for a COMPLEX-tier sprint.
**Calculation:** Phase 1 (Story 1, S) 1.5d + Phase 2 (Story 2, M) 2.5d + Phase 3 (Story 3, M) 2d + Phase 4 (Stories 4+5, S+S, docs-only) 1d + Phase 5 (Validation) 1d = **8 days**

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** false (not "strong" — complexity is 9/12, below the strong-gated threshold of 10/12)
**Suggested command:** `/create-sprint @.planning/plans/active/19.9_community_prompt_submissions/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

---

## Phase Structure

### Phase 1 — Foundation: Local Fixture-Gate Reuse & Submission Blocking
**Duration:** 1.5 days
**Items:** User Story 1 (all of Theme 1)
**Focus:** Stand up `newPersonasSubmitCmd()` in `cmd/atcr/personas.go` with only the local safety gate wired in — name validation, path resolution, `TestPersona`/`TemplateFixtureRunner` fixture check, non-zero-exit-on-failure stderr messaging. No `gh`/network code at all in this phase; the RunE's success path ends at a stubbed continuation point Phase 2 fills in. This phase is the safety precondition every later phase depends on.

### Phase 2 — Core Items: Fork + PR Automation via `gh`
**Duration:** 2.5 days
**Items:** User Story 2 (all of Theme 2)
**Focus:** Add `github.com/cli/go-gh/v2` as a dependency; build the `gh` precondition check (PATH + `gh auth status`); build the injectable `personasGitHub`-style seam wrapping fork/branch-push/PR-create; wire the seam's production implementation to `gh.ExecContext`; copy the resolved persona unit into the fork's working tree; wire the Phase 1 stub to call into this seam on a passing fixture gate; print the PR URL to stdout on success.

### Phase 3 — Core Items: `submitted` Status Distinct from `Source`
**Duration:** 2 days
**Items:** User Story 3 (all of Theme 3)
**Focus:** Introduce the `submitted` status/marker as an additive concept (separate struct or sidecar marker file) that never touches `PersonaMeta.Source`'s existing three values or the signatures of `List`/`ListTiers`/`listCommunity`/`listProject`. Persist attribution metadata (submitter identity, fixture-pass confirmation, timestamp) via `writeFileAtomic`/`atomicfs.WriteFileAtomic` at a path outside `personas/community/`. Wire the marker write into the Phase 2 submit flow so it fires only after a successful PR creation.

### Phase 4 — Integration: Documentation (Graduation Process + Submit Flow & Two-Tier Model)
**Duration:** 1 day
**Items:** User Story 4 (Theme 4) + User Story 5 (Theme 5)
**Focus:** Both stories are documentation-only and land together once Phases 1–3 have shipped real behavior to document accurately. Add the maintainer graduation checklist to `docs/personas-authoring.md` (persona placement, `index.json` entry, marker clearing, `Source` untouched, PR-native process). Update `docs/personas-install.md`'s heading to "seven subcommands" and add the `### atcr personas submit <name>` section. Cross-reference the contribution checklist and add the `submitted` → graduated two-tier-model section.

### Phase 5 — Validation
**Duration:** 1 day
**Items:** Full-suite verification across all 5 stories / 15 ACs
**Focus:** `go test ./...` full pass, `go vet ./...`, `golangci-lint run`, `go fmt` check, coverage against the 80% baseline (`go test -coverprofile=coverage.out ./...`), documentation cross-check against actual shipped command output/error text (per Story 5's Technical Considerations), and adversarial risk-profile review against this document's Risk Analysis section.

---

## Work Decomposition

### Story 1 — Local Fixture-Gate Reuse and Submission Blocking (Phase 1)

| Testable Element | RED (failing test) | GREEN (minimal impl) | AC |
|---|---|---|---|
| Invalid name rejected | `submit badname..` exits non-zero, stderr message, zero fork/PR side effects | Validate via `commpersonas.ValidName` before any further work | [01-01](acceptance-criteria/01-01-invalid-persona-name-rejection.md) |
| Missing fixture blocks | `submit <name-with-no-fixture>` exits non-zero, stderr identifies missing fixture | Check `outcome.HasFixture` after `TestPersona` call | [01-02](acceptance-criteria/01-02-missing-fixture-blocks-submission.md) |
| Fixture pass/fail gates progression | Failing fixture (`Passed != Total`) blocks with pass/fail counts in stderr; passing fixture proceeds past the gate | Branch on `err != nil \|\| outcome.Passed != outcome.Total` | [01-03](acceptance-criteria/01-03-fixture-gate-pass-fail-evaluation.md) |

### Story 2 — Fork + PR Automation via `gh` (Phase 2)

| Testable Element | RED (failing test) | GREEN (minimal impl) | AC |
|---|---|---|---|
| `gh` precondition check | Missing `gh` on PATH or failed `gh auth status` halts before any fork/branch/commit call | Standalone precondition function using `gh.Path()` + `gh.ExecContext(ctx, "auth", "status")` | [02-01](acceptance-criteria/02-01-gh-precondition-check.md) |
| Fork/branch/push/PR-create sequence | Stubbed `gh` seam records fork → push → pr-create called exactly once each, in order; PR URL surfaced to stdout | Seam implementation sequences the three `gh` operations; "fork already exists" treated as non-fatal | [02-02](acceptance-criteria/02-02-fork-branch-push-and-pr-create.md) |
| Injectable `gh` seam | Tests stub the seam with zero real `gh` binary invocations or network calls | Package-level interface/var matching `personasClient`/`personasFixtureRunner` convention | [02-03](acceptance-criteria/02-03-injectable-gh-seam-for-testing.md) |

### Story 3 — `submitted` Status Distinct from `Source`/Provenance (Phase 3)

| Testable Element | RED (failing test) | GREEN (minimal impl) | AC |
|---|---|---|---|
| `submitted` not a `Source` value | After a submission, `Source` is still exactly `built-in`/`community`/`project` for every persona | New status lives on a separate struct/marker, `PersonaMeta` untouched | [03-01](acceptance-criteria/03-01-submitted-status-is-not-a-source-value.md) |
| Marker carries attribution, atomic persistence | Marker readable with submitter attribution + fixture-pass flag; write uses `writeFileAtomic`/`atomicfs.WriteFileAtomic`, refuses symlinked destination | Sidecar marker file (or additive field on a submission-only struct) written via existing atomic helper | [03-02](acceptance-criteria/03-02-submitted-marker-attribution-and-atomic-persistence.md) |
| Marker location + `List` extension point | Marker path never resolves under `personas/community/`; `personas list` output unchanged for existing rows | Marker path constant lives outside vetted tree; `List` extension point added without altering existing `Source`-based output | [03-03](acceptance-criteria/03-03-marker-stored-outside-community-tree-and-list-extension-point.md) |

### Story 4 — Maintainer Graduation into the Vetted Library (Phase 4, docs-only)

| Testable Element | Verification | AC |
|---|---|---|
| Documented persona placement + `index.json` entry | Doc review: checklist covers moving persona into `personas/community/` and adding a matching `PersonaIndexEntry` | [04-01](acceptance-criteria/04-01-documented-persona-placement-and-index-entry.md) |
| Marker clearing without touching `Source` | Doc review: procedure states `Source` never changes during graduation | [04-02](acceptance-criteria/04-02-submitted-marker-clearing-without-touching-source.md) |
| Manual PR-native process, checklist cross-reference | Doc review: procedure references existing PR-review-merge gate, no new tooling implied | [04-03](acceptance-criteria/04-03-manual-pr-native-process-with-checklist-cross-reference.md) |

### Story 5 — Documentation of the Submit Flow and Two-Tier Model (Phase 4, docs-only)

| Testable Element | Verification | AC |
|---|---|---|
| `submit` documented as seventh subcommand | Doc review: heading updated, new section matches existing per-command format, positioned between `test` and `upgrade` | [05-01](acceptance-criteria/05-01-submit-subcommand-documented-in-install-guide.md) |
| Contribution checklist cross-references `submit` | Doc review: checklist item references automated equivalent | [05-02](acceptance-criteria/05-02-contribution-checklist-cross-references-submit.md) |
| Two-tier model section | Doc review: new section explains `submitted` → graduated model in plain language, no terminology collision | [05-03](acceptance-criteria/05-03-submitted-to-graduated-two-tier-model-section.md) |

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** `cmd/atcr/personas_test.go` (CLI/cobra layer) and `internal/personas/*_test.go` (business-logic layer), following the existing `Test<Subject>_<Scenario>` naming convention (e.g. `TestPersonasTest_DefaultRunnerBuiltinFixture`).

**Test File Placement Examples:**
- `cmd/atcr/personas_test.go` (extend) or a new `cmd/atcr/personas_submit_test.go` — CLI-level RunE tests using the existing `executeSplit` stdout/stderr helper (personas_test.go:35)
- `internal/personas/submit.go` (new) + `internal/personas/submit_test.go` (new) — business logic: fixture-gate orchestration, name/path resolution, marker read/write, `gh` seam interface

**Unit/Integration/E2E:**
- **Unit (8 ACs):** 01-01, 01-02, 01-03, 02-01, 02-02, 02-03, 03-01, 03-02 — all via `go test`, stdlib `testing`, table-driven where scenarios repeat (matches existing coding-standards.md convention). `gh` interaction fully stubbed via the injectable seam — no real `gh` binary or network calls in any unit test.
- **Integration (1 AC):** 03-03 — exercises the full `personas list` CLI output path to confirm the `submitted` marker doesn't alter existing `Source`-based rows.
- **E2E (0 ACs):** None — the `gh` fork/PR flow is validated entirely through seam-stubbed unit tests per 02-03; no live GitHub calls in the test suite.
- **Manual (6 ACs):** 04-01, 04-02, 04-03, 05-01, 05-02, 05-03 — documentation-only, verified by doc review against actual shipped command behavior (Story 5's Technical Considerations require this cross-check before finalizing docs).

**Test Environment Status:**
- Framework: Go standard `testing` package (table-driven tests); `testify`/`assert`/`require` available per coding-standards.md but not used in existing `internal/personas`/`cmd/atcr` test files — match existing file's style when extending it.
- Execution: `go test ./...` (project test command per `.planning/.config/config.yaml`)
- Coverage Tools: `go test -coverprofile=coverage.out ./...` against an 80% coverage baseline

---

## Architecture

**Primitives:**
- `PersonaMeta` (`internal/personas/list.go:19`, existing, unmodified) — provenance (`Source`) stays exactly `built-in`/`community`/`project`.
- `FixtureOutcome` (`internal/personas/test.go`, existing) — the pass/fail/HasFixture result `submit`'s gate branches on.
- New: a submission-scoped marker/struct (name TBD, e.g. `SubmissionStatus`) carrying submitter identity, source persona name, submission timestamp, and a fixture-pass confirmation flag — deliberately **not** a field on `PersonaMeta`.

**Module Boundaries:**
- `cmd/atcr/personas.go` — CLI layer: `newPersonasSubmitCmd()`, argument validation, output formatting (`cmd.OutOrStdout()`/`cmd.ErrOrStderr()`), and the injectable seam *declarations* (`personasFixtureRunner` reused as-is; a new `personasGitHub`-style seam for fork/push/PR-create).
- `internal/personas/submit.go` (new) — business logic: name/path resolution, fixture-gate orchestration via existing `TestPersona`, submitted-marker read/write. Keeps the existing `internal/personas` vs `cmd/atcr` split the rest of the package tree already follows.

**External Dependencies:**
- `github.com/cli/go-gh/v2` — wraps the `gh` binary (`gh.ExecContext`, `gh.Path()`); the *only* new external dependency this plan introduces. Wrapped entirely behind an injectable interface so no test invokes it directly.

**Replaceability:** The `gh`-based fork/PR delivery mechanism is fully swappable behind its seam — a future alternate transport (e.g. a direct REST client) could replace it without touching `RunE`'s logic, as long as it satisfies the same fork/push/PR-create interface. The `submitted` marker's storage mechanism (sidecar file vs. struct field) is likewise isolated behind Story 3's read/write functions, so its persistence format can change without touching Story 1/2/4/5 call sites.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Persona name → path resolution | `internal/personas/paths.go` validation before any fork/PR work | Path traversal (`..`), absolute paths, symlink swap at the resolved unit path | Reuse `validatePersonaName`/`personaPath` verbatim (no reimplementation); mirror the validation-then-resolve order already used by `install`/`remove` |
| `gh` CLI argument construction | `gh.ExecContext` calls for fork/push/pr-create, including persona name and attribution text in branch names / PR title / PR body | Shell-metacharacter or argument injection if any user-controlled string (persona name, submitter identity) is interpolated unsafely; injected markdown/HTML in PR body | Use `go-gh`'s argument-array `ExecContext` calls exclusively (never a shell string built with `fmt.Sprintf` + `sh -c`); validate persona name before it reaches any `gh` argument |
| Submitted-marker persistence | `internal/personas/submit.go` marker write path | Symlink TOCTOU at the marker destination; partial/corrupt write on crash mid-write | Reuse `writeFileAtomic`/`atomicfs.WriteFileAtomic` exclusively — both already refuse symlinked destinations and use sibling-temp-then-rename |
| GitHub auth/credential handling | `gh auth status` precondition check | Accidental logging of a token or auth header in error output | Never log a token or auth header. **Ratified behavior:** AC 02-01 (DoD-checked, test-verified) supersedes this note's earlier "generic message only" wording — it surfaces `gh`'s credential-redacted `auth status` stderr as diagnostic detail. `gh` redacts the token before capture, so the residual (login/host/scope) is low-severity diagnostic info, not a secret. This stderr pass-through is the final behavior (TD clarification 2026-07-10, `submit.go:158`). |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| Local fixture-gate render (`TemplateFixtureRunner`) | Single persona per invocation, interactive CLI use | Sub-second, zero network/LLM calls (already true of the reused implementation) | No new work needed — call the existing zero-network gate as-is |
| `gh` fork/push/PR-create sequence | Single submission per invocation, network-bound against GitHub | Bounded by GitHub API latency; must not hang indefinitely on a stalled network call | A single derived `context.Context` (the 90s `personasSubmitTimeout`) is threaded through every downstream `gh.ExecContext`/git call so a stalled network can't hang the CLI. **One shared deadline across the fork→sync→push→PR sequence satisfies this requirement** — the mandate is that every call observes a bounded context, not that each step gets an independent per-operation budget; per-step budgets would be disproportionate for a LOW, single-invocation CLI path (TD clarification 2026-07-10, `personas.go:144`). |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| `gh` unavailable | `gh` not on PATH; `gh auth status` fails | Halt before any fork/branch/commit work, clear actionable stderr message |
| Fork already exists | User has previously forked `samestrin/atcr` | Non-fatal — proceed to branch/push against the existing fork rather than erroring |
| Invalid/unresolvable name | Name fails `validatePersonaName`; name doesn't resolve to any known persona | Non-zero exit, stderr error, zero fork/PR side effects |
| Missing/failing fixture | `HasFixture: false`; `Passed != Total` | Block submission, stderr reports which condition and (for failures) pass/fail counts |
| Marker already exists for this persona (resubmission) | User re-runs `submit` on a persona already carrying a `submitted` marker | Not yet specified by any AC — flag during implementation as a decision point (overwrite vs. version vs. reject); default to overwrite-with-updated-timestamp unless implementation reveals a reason otherwise |
| Symlinked marker destination | Pre-existing symlink at the marker path | `writeFileAtomic`/`atomicfs.WriteFileAtomic` refuses and returns a clear error |

### Defensive Measures Required

- **Input Validation:** `validatePersonaName`/`personaPath` before any resolution or `gh` call; all `gh` arguments passed as discrete array elements, never shell-concatenated strings.
- **Error Handling:** Every `RunE` failure path returns a non-nil error (no inline `os.Exit`); errors wrapped with `fmt.Errorf("...: %w", err)` per coding-standards.md; distinct, actionable messages for name-validation failure vs. fixture-gate failure vs. `gh`-precondition failure vs. fork/PR failure.
- **Logging/Audit:** The `submitted` marker's attribution fields (submitter identity, timestamp, fixture-pass confirmation) double as the audit trail for a submission; never log raw `gh auth`/token output.
- **Rate Limiting:** Not applicable — single-invocation, user-driven CLI command; GitHub's own API rate limits apply to `gh` itself and are outside this plan's control.
- **Graceful Degradation:** Do not write the `submitted` marker unless the PR was actually created successfully — avoid a state where a marker exists but no corresponding PR does; a fork/push/PR-create failure after a passing fixture gate must not leave partial, inconsistent local or remote state.

---

## Risks

**Technical:**
- `gh` CLI absent/unauthenticated on the user's machine → confusing mid-flow failure → mitigated by an upfront `gh.Path()` + `gh auth status` precondition check before any fork/branch/commit work (Phase 2).
- `submitted` implemented as a fourth `Source` value, breaking `List`/`ListTiers`/`listCommunity`/`listProject` → mitigated by an explicit unit test asserting `Source` never takes a value outside its existing three, plus code-review reference to this document's Risk Analysis (Phase 3).
- Hard-wired `gh.ExecContext` calls inline in `RunE`, making `submit` untestable in CI → mitigated by the injectable seam convention already established for `personasClient`/`personasFixtureRunner` (Phase 2).
- `gh repo fork` treated as fatal when a fork already exists, breaking repeat submissions → mitigated by explicitly handling "fork already exists" as a non-fatal, expected outcome (Phase 2).
- Bare `os.WriteFile` used for the submitted marker instead of the existing atomic helpers, reopening TOCTOU/partial-write risk → mitigated by reusing `writeFileAtomic`/`atomicfs.WriteFileAtomic` exclusively, with a symlink-refusal test (Phase 3).

**TDD-Specific:**
- AC 02-02 (fork/branch/push/PR-create sequencing) is flagged High complexity and risks becoming an integration-test-shaped unit test → mitigated by decomposing the `gh` seam into small independently-stubbable methods (Fork/PushBranch/CreatePR) so each step is RED-GREEN-REFACTOR'd separately.
- Docs-only stories (4, 5) have no automated test coverage → mitigated by treating doc-accuracy review as an explicit Phase 5 Validation step (cross-checking documented command output/error text against the actual Phase 1–3 implementation), not skipped as "no tests needed."
- Stories 3/4 have a hard sequencing dependency on Stories 1/2 (the marker is only ever written after a real PR exists) → mitigated by the phase ordering in this document (Phase 1 → 2 → 3 → 4), avoiding parallel implementation that could let Story 3 land ahead of a working submit flow to attach it to.

---

**Next:** `/create-sprint @.planning/plans/active/19.9_community_prompt_submissions/`
