# User Story 1: Bonus Built-In Domain Personas

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** security engineer, Go developer, or performance engineer using ATCR
**I want** domain-specific review personas (`sentinel`, `tracer`, `idiomatic`) shipped with the binary
**So that** I get focused, expert-lens feedback on my code changes without installing or configuring anything beyond the standard ATCR binary

## Story Context

- **Background:** ATCR currently ships six generalist reviewer personas (`bruce`, `greta`, `kai`, `mira`, `dax`, `otto`) embedded in the binary via `go:embed`. Domain specialists — security engineers auditing injection vectors, performance engineers hunting N+1 queries, Go developers checking idiomatic conventions — receive the same generalist commentary as any other user. Three new domain personas (`sentinel` for security, `tracer` for performance, `idiomatic` for Go idioms) will expand the panel from 6 to 9, each embedded at compile time and backed by a CI-tested fixture that verifies the persona produces at least one expected finding category. Sprint A delivers T1 immediately after T8 (the `AgentConfig.Language` field) so these personas can optionally declare a `language:` scope from day one.
- **Assumptions:** The `go:embed *.md` directive in `personas/personas.go` automatically picks up new `.md` files; only entries listed in the `names` slice are exposed through `Names()` and `Get()`. The existing persona file format (Go `text/template` markdown, no YAML frontmatter) is the authoritative template; `personas/bruce.md` is the canonical reference. Fixture `.patch` files in `personas/testdata/` can be authored as minimal synthetic diffs that reliably trigger the expected category — they do not need to be real production diffs. The `atcr personas test <name>` CLI (T2, Sprint B) is not yet available in Sprint A; CI fixture verification runs via an internal Go test in `personas/personas_test.go`.
- **Constraints:** All three personas must be embedded in the binary — no download or install step at runtime. The `names` slice ordering is prescribed: `sentinel`, `tracer`, `idiomatic` slot between `dax` and `otto`. The existing test `TestNames_ReturnsAllSix` must be renamed `TestNames_ReturnsAllNine` and updated to assert count 9 before any persona `.md` file is created (TDD RED phase). The `go:embed` glob must not inadvertently expose non-persona `.md` files; the explicit `names` slice acts as the allow-list. Persona `.md` files follow the same `text/template` structure as `bruce.md` — no YAML frontmatter is permitted in `.md` persona files (the registry `AgentConfig` schema is the correct place for structured metadata).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | T8 (`AgentConfig.Language` field) must land and pass `go test ./...` before T1 begins |

## Success Criteria (SMART Format)

- **Specific:** Three new persona `.md` files (`sentinel.md`, `tracer.md`, `idiomatic.md`) are present in `personas/`, registered in the `names` slice, embedded in the binary, and each paired with a fixture in `personas/testdata/` that a corresponding Go test verifies produces its expected finding category.
- **Measurable:** `personas.Names()` returns exactly 9 names; `personas.Get("sentinel")`, `personas.Get("tracer")`, and `personas.Get("idiomatic")` all return non-empty content without error; three new test cases in `personas/personas_test.go` each pass `go test ./personas/...` against their respective fixtures; CI `go test ./...` exits 0.
- **Achievable:** The embed mechanism and persona file format are already established; this story extends an existing pattern with no new infrastructure. The three fixtures are small synthetic `.patch` files, each exercising a single well-defined finding category.
- **Relevant:** Domain specialists currently receive only generalist review commentary. Shipping focused personas for security, performance, and Go idioms directly in the binary removes all friction for the highest-value vertical market users and makes personas the primary adoption lever — no install, no configuration.
- **Time-bound:** Delivered within Sprint A (9.0-A), immediately after T8 is verified green, before Sprint B work begins.

## Acceptance Criteria Overview

1. `personas.Names()` returns `["bruce","greta","kai","mira","dax","sentinel","tracer","idiomatic","otto"]` in that exact order, and `len(Names()) == 9`.
2. Each of `sentinel`, `tracer`, and `idiomatic` has a corresponding `.md` file in `personas/` following the `bruce.md` template structure, and `personas.Get("<name>")` returns non-empty rendered content without error.
3. Each bonus persona has a fixture file in `personas/testdata/` (e.g., `sentinel_fixture.patch`) and a Go test in `personas/personas_test.go` that runs the persona against its fixture and asserts at least one finding in the expected category (SQL injection or hardcoded secret for `sentinel`; N+1 query or unbounded allocation for `tracer`; ignored error or goroutine leak for `idiomatic`).
4. `TestNames_ReturnsAllSix` is renamed `TestNames_ReturnsAllNine` and asserts count 9 — this rename is committed in the RED phase, before the `.md` files exist.
5. `go test ./...` passes with no new test failures introduced by this story.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** Follow strict TDD order — rename and update `TestNames_ReturnsAllNine` first (RED), then create the three `.md` files and register names in the slice (GREEN). The `names` slice in `personas/personas.go` is the only registration point; `go:embed *.md` handles file inclusion automatically. Each persona `.md` file must use the same `text/template` variable set as `bruce.md` (payload variables for diff content, repo context, output format instructions). Fixture `.patch` files in `personas/testdata/` should be minimal: 10–30 lines that reliably trigger the target category without relying on external services. The internal test for each fixture can use `personas.Get(name)` to retrieve the prompt and assert the fixture-based invocation path — the exact test harness pattern is already established for existing persona tests.
- **Integration Points:** `personas/personas.go` (`names` slice + `go:embed`); `personas/personas_test.go` (`TestNames_ReturnsAllNine` + three new fixture tests); `personas/testdata/` (three new `.patch` files); `AgentConfig.Language` field (T8) — bonus personas may optionally declare `language: [go]` for `idiomatic`, but this is not required for the story to be complete. The `atcr personas test <name>` CLI (T2) will exercise these same fixtures in Sprint B; the internal Go test in Sprint A is the interim verification path.
- **Data Requirements:** Three `.md` persona prompt files, each following the `personas/bruce.md` template structure. Three `.patch` fixture files, each a synthetic diff demonstrating the target vulnerability or pattern. No database, no network calls, no new package imports.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `go:embed` picks up unexpected `.md` files (e.g., README or scratch files in `personas/`) | Medium | The explicit `names` slice is the production allow-list — undeclared `.md` files are embedded but never returned by `Names()` or `Get()`. Keep `personas/` clean; add a test asserting `Names()` length equals 9 exactly. |
| `TestNames_ReturnsAllSix` renamed/updated before `.md` files exist causes CI red between commits | Medium | The RED commit (test update only) must not be pushed to CI as a standalone PR; T8 and T1 ride the same Sprint A PR so the GREEN state is what CI sees on merge. |
| Fixture `.patch` files too generic — persona produces no findings on fixture, causing flaky test | Low | Author fixtures as targeted, minimal diffs that contain an unambiguous trigger (e.g., a literal SQL concatenation for `sentinel`); assert `len(findings) >= 1` with the expected category as a substring match, not an exact count. |
| Persona `.md` prompt quality insufficient for domain coverage | Low | Each prompt is reviewed against the finding-category table in `documentation/bonus-personas.md` before the story is marked done; acceptance requires the fixture test to pass, which validates end-to-end prompt→finding routing. |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
