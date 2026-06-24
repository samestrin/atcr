# User Story 1: Bonus Built-In Domain Personas

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** security engineer, performance engineer, or Go developer using ATCR
**I want** domain-specific reviewer personas (`sentinel`, `tracer`, `idiomatic`) to ship with the binary and be active immediately
**So that** I receive targeted, expert-level code review coverage in my domain without any install, configuration, or persona-authoring step

## Story Context

- **Background:** ATCR ships with six generalist built-in personas. Domain specialists currently get only generalist commentary on security vulnerabilities, performance regressions, or Go idiom violations — there is no persona that speaks their domain language. This forces users to either accept shallow coverage or write and maintain their own persona files. Shipping three curated domain personas with the binary removes that friction entirely and makes ATCR immediately useful to vertical market adopters.
- **Assumptions:** The existing `go:embed *.md` directive in `personas/personas.go` will pick up new `.md` files automatically once they are added to the `personas/` directory. The `AgentConfig.Language` field (T8) will exist before T1 ships, allowing bonus personas to optionally declare a language constraint — but this is optional; T1 personas are valid without it. Fixture-based CI testing requires only a `.patch`/`.diff` file and a Go test; it does not require the T2 `atcr personas test` CLI to be present.
- **Constraints:** Each bonus persona must follow the exact markdown template structure used by `personas/bruce.md` (system prompt, severity rubric, output format, payload variable slots). The `personas/personas.go:names` slice must be updated manually because `go:embed` loads all `.md` files but only names listed in `names` are exposed via `Names()` and `Get()`. The existing test `TestNames_ReturnsAllSix` must be renamed and updated to expect 9 before the persona files are created (TDD RED phase). CI must not require live network access for fixture verification.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | T8 (`AgentConfig.Language` field) must land first so bonus personas can optionally declare `language:` at creation time |

## Success Criteria (SMART Format)

- **Specific:** Three new personas — `sentinel` (security), `tracer` (performance), `idiomatic` (Go idioms) — are embedded in the ATCR binary, registered in `personas/personas.go:names`, and each produces at least one finding in its declared category when run against its corresponding test fixture.
- **Measurable:** `personas.Names()` returns exactly 9 names; `personas.Get("sentinel")`, `personas.Get("tracer")`, and `personas.Get("idiomatic")` each return a non-empty prompt string; CI fixture tests for all three personas pass in `go test ./personas/...` with zero live network calls.
- **Achievable:** The implementation adds three `.md` files, three `.patch` fixture files, and updates one Go source file and one test file — no new packages or external dependencies required.
- **Relevant:** Domain-specific personas are the primary lever for vertical market adoption identified in the Plan 9.0 goal; shipping them with the binary means zero-friction access for the target user segments (security, performance, Go).
- **Time-bound:** Delivered within Sprint A alongside T8, before Sprint B begins.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-names-registry-returns-nine.md) | Names Registry Returns Nine | Unit |
| [01-02](../acceptance-criteria/01-02-bonus-persona-prompt-content.md) | Bonus Persona Prompt Content | Unit |
| [01-03](../acceptance-criteria/01-03-fixture-ci-tests-no-network.md) | Fixture-Based CI Tests (No Network) | Integration |

## Original Criteria Overview

1. `personas.Names()` returns a slice of exactly 9 strings including `sentinel`, `tracer`, and `idiomatic` in the canonical order (`bruce`, `greta`, `kai`, `mira`, `dax`, `sentinel`, `tracer`, `idiomatic`, `otto`).
2. Each bonus persona's prompt content covers its declared domain: `sentinel` addresses OWASP Top 10 / injection / secrets leakage; `tracer` addresses N+1 queries / memory leaks / allocation hot paths; `idiomatic` addresses Go error handling / goroutine leaks / stdlib misuse.
3. A CI-passing test in `personas/personas_test.go` verifies each persona produces at least one finding in its expected category when applied to its fixture file in `personas/testdata/` — no live network calls, no external services.

## Technical Considerations

- **Implementation Notes:** Follow strict TDD order — rename `TestNames_ReturnsAllSix` to `TestNames_ReturnsAllNine` and update the expected count to 9 first (RED), then add `sentinel.md`, `tracer.md`, `idiomatic.md` to `personas/` and append the three names to the `names` slice in `personas/personas.go` (GREEN). Canonical position in the slice: immediately before `otto` (index 5–7). Each persona `.md` file must use the same `text/template` structure as `personas/bruce.md` with a system prompt section, severity rubric, output format block, and payload variable slots (`{{.Diff}}`, `{{.ScopeFocus}}`, etc.).
- **Integration Points:** `personas/personas.go` — `names` slice update and `go:embed *.md` pickup; `personas/personas_test.go` — test rename and count update; `personas/testdata/` — three new fixture files (`sentinel_fixture.patch`, `tracer_fixture.patch`, `idiomatic_fixture.patch`); downstream: once T2 CLI lands, `atcr personas test <name>` will delegate to the same fixture path already exercised by the Go test.
- **Data Requirements:** Each fixture file must be a minimal valid `.patch` or `.diff` that contains exactly the code pattern the persona is expected to flag (e.g., `sentinel_fixture.patch` contains a SQL string concatenation or hardcoded API key; `tracer_fixture.patch` contains an ORM call inside a loop; `idiomatic_fixture.patch` contains an ignored `error` return). Fixtures are committed to the repo and loaded via `os.ReadFile` in tests — no generation at test time.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `TestNames_ReturnsAllSix` breaks CI before persona `.md` files are created if the name-slice and test changes land in separate commits | Medium | Rename test and update count in the same commit that adds the `.md` files (single atomic GREEN commit after RED phase is verified locally) |
| `go:embed *.md` picks up unintended `.md` files placed in `personas/` during development | Low | `Names()` and `Get()` only expose names declared in the explicit `names` slice — unlisted files are embedded but unreachable through the public API |
| Fixture does not trigger the expected finding category (persona produces findings but in a different category) | Medium | Author each fixture to contain an unambiguous, canonical example of the target pattern; assert on finding category string in the test rather than just non-empty output |
| T8 (`AgentConfig.Language`) not yet merged when T1 authoring begins, requiring a second pass to add `language:` fields | Low | Bonus personas are valid without `language:` — add the field in the same PR as T8 if both land in Sprint A, or omit it initially; it is an optional field |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
