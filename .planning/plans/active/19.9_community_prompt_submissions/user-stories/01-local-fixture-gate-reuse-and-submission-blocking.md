# User Story 1: Local Fixture-Gate Reuse and Submission Blocking

**Plan:** [19.9: Community Prompt Submissions (Intake & Curation)](../plan.md)

## User Story

**As a** atcr user who has tuned a persona locally
**I want** `atcr personas submit <name>` to reject a persona whose fixture fails or is missing before any GitHub interaction is attempted
**So that** I get a fast, clear, local failure signal instead of an unvetted or broken prompt reaching a fork/PR on the canonical repository

## Story Context

- **Background:** The intake pipeline already trusts a fixture-passing check as the local quality gate — `personas test` runs it today via `commpersonas.TestPersona(name, personasFixtureRunner)` (internal/personas/test.go:195), backed by the production `TemplateFixtureRunner` (internal/personas/test.go:33) which renders a persona's template against its committed patch fixture with zero network and zero LLM calls. `submit` must not introduce a second gate; it must call this exact existing gate, matching the manual "fixture presence/pass" step already documented in the contribution checklist (docs/personas-authoring.md:162) but enforcing it automatically.
- **Assumptions:** The persona named on the command line already exists locally (built-in, on-disk installed, or embedded community-library) and is resolvable via the same path helpers install/search/remove already use. No `gh`/GitHub interaction of any kind (fork, branch, commit, `gh pr create`) is in scope for this story — that is Theme 2's concern. A persona with `HasFixture: false` (no fixture defined at all) is treated as a blocking condition for submission, consistent with the checklist's "fixture presence" requirement, even though `personas test` itself treats "no fixture" as a non-failing informational case.
- **Constraints:** Must reuse `commpersonas.TestPersona`/`TemplateFixtureRunner` verbatim rather than reimplementing fixture logic; must validate `<name>` via `validatePersonaName` (internal/personas/paths.go:42) and resolve it via `personaPath` (internal/personas/paths.go:72) before the fixture gate runs, mirroring the validation order already used by `install`/`remove`; the failure path must produce output on stderr (never stdout) with a non-zero exit, and must not touch the network or invoke any `gh` binary.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas submit <name>` validates the persona name, resolves its path, runs the existing fixture gate (`TestPersona` + `TemplateFixtureRunner`), and returns a non-zero exit with a clear stderr error — without any fork, branch, commit, or `gh` invocation — whenever the name is invalid, the persona cannot be resolved, no fixture is defined, or the fixture fails.
- **Measurable:** New unit tests (using the `executeSplit` helper pattern at cmd/atcr/personas_test.go:35) assert stderr contains an actionable message and stdout/side effects show zero fork/PR activity for: an invalid name, a name with no fixture, and a name with a failing fixture.
- **Achievable:** The fixture gate, name validation, and path resolution are all pre-existing, already-tested functions; this story only wires them into a new subcommand's early-exit path, matching the pattern six existing `newPersonas<Verb>Cmd()` subcommands already follow.
- **Relevant:** Directly implements AC1's blocking requirement and is the safety precondition every later theme (fork/PR automation, status/provenance, docs) depends on — no submission-blocking work here means no later fork/PR call can be trusted to run only on vetted-locally-passing personas.
- **Time-bound:** Completed within this sprint's first implementation phase, before any `gh`-integration code (Theme 2) is started.

## Acceptance Criteria Overview

1. `atcr personas submit <name>` with an invalid persona name (fails `validatePersonaName`) exits non-zero with a clear stderr error and performs no fork/PR work.
2. `atcr personas submit <name>` on a persona with no fixture defined (`HasFixture: false`) exits non-zero with a clear stderr error identifying the missing fixture, and performs no fork/PR work.
3. `atcr personas submit <name>` on a persona whose fixture fails (`Passed != Total`) exits non-zero with a clear stderr error reporting the pass/fail counts, and performs no fork/PR work.
4. `atcr personas submit <name>` on a persona whose fixture fully passes proceeds past the local gate (subsequent fork/PR behavior is out of scope for this story and covered by Theme 2).

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/19.9_community_prompt_submissions/`_

## Technical Considerations

- **Implementation Notes:** Add `newPersonasSubmitCmd()` in cmd/atcr/personas.go alongside the other `newPersonas<Verb>Cmd()` functions, and register it via `cmd.AddCommand(...)` in `newPersonasCmd()` (personas.go:106-113). `RunE` should: (1) call `validatePersonaName`/resolve via `personaPath` — matching the validation order used by `install`/`remove` — (2) call `commpersonas.TestPersona(name, personasFixtureRunner)`, (3) on `err != nil`, `!outcome.HasFixture`, or `outcome.Passed != outcome.Total`, write a clear message to `cmd.ErrOrStderr()` and return a non-nil error (no `os.Exit` inline, consistent with existing subcommands), and (4) only on a full pass, proceed to a stubbed/no-op continuation point that Theme 2 will fill in with the fork/PR call.
- **Integration Points:** Reuses `personasFixtureRunner` (the existing package var at personas.go:88) as the injectable seam — no new fixture-runner seam is needed for this story. Follows the same `personasDir`/`personasClient`/`personasFixtureRunner` injectable-seam convention cited in the plan's technical notes so tests can stub dependencies without filesystem or network access.
- **Data Requirements:** None — this story reads existing persona files and the existing `FixtureOutcome` struct; it introduces no new persisted fields or schema (the `submitted` status field is Theme 3's concern).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Treating "no fixture" (`HasFixture: false`) as a hard block diverges from `personas test`'s softer "No fixture defined" informational message, potentially confusing users familiar with `test`'s behavior | Medium | Give `submit`'s no-fixture error distinct, explicit wording (e.g. "cannot submit %q: no fixture defined — add a fixture before submitting") so it reads as a submission-specific policy, not a `test` regression |
| A future contributor adds fork/PR logic before this story's fixture gate, allowing a bypass path | High | Structure `RunE` so the gate check is the first statement after name/path resolution and returns immediately on any non-passing outcome, with a unit test asserting zero `gh`-related side effects occur on failure |
| Reusing `personaPath`/`validatePersonaName` incorrectly (e.g. skipping validation before resolving) reopens a path-traversal class of bug already closed for install/remove | Low | Mirror the exact validation-then-resolve order already used by `newPersonasRemoveCmd()` (personas.go:279-296) and add a table-driven test covering the same traversal cases those commands already guard against |

---

**Created:** July 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria
