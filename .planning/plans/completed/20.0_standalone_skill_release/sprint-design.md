# Sprint Design: Standalone ATCR Skill Distribution

**Created:** July 11, 2026 02:50:34PM
**Plan:** [20.0: Standalone ATCR Skill Distribution](plan.md)
**Plan Type:** ✨ Feature
**Status:** Design Complete

---

## Original User Request

> We want to release `atcr` as a public engine alongside its standalone skill, allowing anyone to run multi-agent code reviews on any repository without needing the private `.planning/` sprint workflow, while continuing to seamlessly support the internal, private skill workflows. Per the 2026-07-05 addendum override, both the public OSS release and (as a later manual follow-up) the private `claude-prompts` skills adopt a single dispatcher skill under a unified `/atcr <command>` UX, rather than a fragmented set of single-capability skills.

**Referenced Resources:**

- [atcr Agent Skill — Installation & Usage](../../../../docs/skill-usage.md)
  - **Summary**: Documents installing `skill/SKILL.md` into an agent's skills directory and its review→reconcile→report orchestration flow, stating that all artifacts land under `.atcr/reviews/<id>/`.
  - **Key Points**: Skill is a single Markdown file with no executable code; install is a file copy (`.claude/skills/atcr/SKILL.md`); already cross-links `code-review-backend.md` for backend integrators — this citation must stay accurate once the dispatcher rewrite lands.
- [Using atcr as a code-review backend (`--output-dir`)](../../../../docs/code-review-backend.md)
  - **Summary**: Documents the `--output-dir` + `atcr reconcile` contract that private-skill consumers (`execute-code-review`, `reconcile-code-review`) depend on, including the exact output tree and behavioral notes (partial runs, count divergence, temporary review-scope limitation).
  - **Key Points**: Names the four files a backend caller verifies (`sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, `reconciled/summary.json`) — this is the exact assertion surface for Story 2's new repo-local test.

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Standalone Skill Release
**Complexity:** 8/12 (COMPLEX)
**Timeline:** 8 days
**Phases:** 5
**Pattern:** Foundation → Independent Verification → Documentation & Migration → Integration → Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Claude Code skill dispatcher pattern
Cobra CLI command routing table test
backward compatibility contract test Go
shell install script go install wrapper
progressive disclosure secondary markdown files
```

---

## Complexity Breakdown

- **Architecture:** 2/3 - Converts `skill/SKILL.md` from a linear orchestration script into a router + on-demand secondary-file model — new for this repo, though grounded in Claude Code's externally-documented three-level Agent Skill format, not invented from scratch.
- **Integration:** 2/3 - 3+ integration surfaces must stay in lockstep: the dispatcher's command table vs. the live Cobra tree (`cmd/atcr/main.go:185-208`), three documentation files (`docs/skill-usage.md`, `docs/code-review-backend.md`, `README.md`), and the net-new `install.sh` vs. the existing `go install` path.
- **Story/Task & Test:** 3/3 - 5 user stories, 17 acceptance criteria spanning unit, integration, E2E, and manual verification — extensive by the rubric.
- **Risk/Unknowns:** 1/3 - Two prior `/refine-epic` passes already resolved scope ambiguity (packaging descoped to Epic 21.0, self-test satisfied by `atcr doctor`, cross-repo writes explicitly out of scope); remaining risk is regression during the dispatcher rewrite, which every story's own Potential Risks table already names a mitigation for.

**Time Formula:** Sum of per-story effort (L=3d, S=1d each) + 1 day integration/validation buffer
**Calculation:** Story1(L)=3d + Story2(S)=1d + Story3(S)=1d + Story4(S)=1d + Story5(S)=1d + buffer=1d = **8 days**

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** standard (not strong)
**Suggested command:** `/create-sprint @.planning/plans/active/20.0_standalone_skill_release/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days; strong gated at complexity >= 10/12.

This sprint crosses the gated threshold three ways at once (complexity = 8, phases = 5, duration = 8 days > 5), so `--gated` is recommended over the plain `--adversarial` fallback.

---

## Phase Structure

1. **Foundation: Dispatcher Rewrite** (~3 days) — Story 1 (Dispatcher Skill Rewrite). Highest-risk item, sequenced first since Stories 4 and 5 depend on its output (the dispatcher template and final command surface).
2. **Independent Verification Work** (~2 days) — Story 2 (Backend Contract Backward-Compat Test) and Story 3 (Install Script), run in either order — neither depends on Story 1 or on each other.
3. **Documentation & Migration Follow-Through** (~1 day) — Story 4 (Documentation Accuracy Pass, depends on Stories 1 & 3) and Story 5 (External Migration Descope Note, depends on Story 1).
4. **Integration & Cross-Verification** (~1 day) — End-to-end trace of the full review→reconcile→report flow through the new dispatcher; cross-check every routed command name against `cmd/atcr/main.go:185-208`; run the full `go test ./...` suite plus `golangci-lint`/`go vet`.
5. **Validation** (~1 day) — Definition-of-Done check: line-budget/frontmatter constraints, coverage baseline (80%), no external-repo writes, all 17 ACs closed.

---

## Work Decomposition

### Story 1: Dispatcher Skill Rewrite (Effort: L, Priority: High, Deps: None)

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| 01-01 | Dispatcher command routing table matches every entry in `newRootCmd` (`cmd/atcr/main.go:185-208`), zero invented/drifted names | Unit |
| 01-02 | Existing review→reconcile→report orchestration remains reachable as one routable command path | Unit |
| 01-03 | Host Review Instructions, Ambiguity Adjudication, Findings Format Reference moved byte-for-byte verbatim into `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md` | Unit |
| 01-04 | Frontmatter (`name` ≤64 chars, lowercase/numbers/hyphens, no reserved words; `description` ≤1024 chars) and SKILL.md body ≤~500 lines | Unit |
| 01-05 | `docs/skill-usage.md` still accurately describes install/usage/`.atcr/reviews/<id>/` output post-rewrite | Manual |

RED-GREEN-REFACTOR notes: RED = extend `skill/skill_test.go` with assertions for the new routing table, secondary-file existence, and line/frontmatter budgets (all should fail against the current linear-script content). GREEN = perform the minimal rewrite/relocation to pass. REFACTOR = tighten per-command routing entries to one line each without losing routing accuracy.

### Story 2: Backend Contract Backward-Compatibility Test (Effort: S, Priority: High, Deps: None)

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| 02-01 | `atcr review --output-dir` + `atcr reconcile` produce the exact documented tree (`manifest.json`, `payload/`, `sources/pool/findings.txt`, `sources/pool/summary.json`, `reconciled/findings.txt`, `reconciled/findings.json`, `reconciled/report.md`, `reconciled/summary.json`) | Integration |
| 02-02 | All three id-or-path resolution branches (bare id → `.atcr/reviews/<id>/`, explicit path, omitted → `.atcr/latest`) resolve correctly as table-driven subtests | Unit/Integration |
| 02-03 | Provider interaction mocked via `httptest.NewServer` (zero real network calls); git fixtures built via `os/exec`, never shell | Integration |

RED-GREEN-REFACTOR notes: this story adds a regression lock over already-correct, already-unit-tested engine behavior — RED is the new `cmd/atcr/backend_contract_test.go` failing to compile/pass against a fresh fixture, GREEN is wiring the fixture + assertions correctly, REFACTOR is extracting shared fixture helpers if `review_test.go`/`reconcile_test.go` patterns don't already cover it.

### Story 3: Install Script (Effort: S, Priority: High, Deps: None)

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| 03-01 | `install.sh` wraps `go install github.com/samestrin/atcr/cmd/atcr@latest`; succeeds when Go toolchain present | Integration |
| 03-02 | Exits non-zero with clear stderr when `go` missing or install fails; warns (non-fatal) when `$(go env GOPATH)/bin` absent from `$PATH` | Integration |
| 03-03 | README.md Quickstart documents `install.sh` as a companion to (not replacement of) the existing manual `go install` instruction | E2E |

Implementation note: no shell-test framework exists in this repo today (`examples/ci-gate.sh` has no companion test). `/create-sprint` should decide between (a) a small Go test in `cmd/atcr/` that shells out to `install.sh` with a temp `GOPATH`/`PATH` via `os/exec`, matching the repo's existing subprocess-testing convention, or (b) documented manual verification steps for the AC03-01/03-02 Integration-typed ACs — either is consistent with the "thin wrapper, ~40 lines" scope constraint.

### Story 4: Documentation Accuracy Pass (Effort: S, Priority: Medium, Deps: Story 1, Story 3)

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| 04-01 | `docs/skill-usage.md` command examples/paths verified against post-rewrite `skill/SKILL.md` | Manual |
| 04-02 | `docs/code-review-backend.md` "Output tree" / "Behavioral notes for callers" verified against current `--output-dir` behavior and Story 2's test assertions | Manual |
| 04-03 | `README.md` command table + Documentation section verified and cross-linked to `install.sh`, no functional change to Quickstart | Manual |

This is a diff-style verification pass, not new authoring — only lines demonstrably wrong get touched.

### Story 5: External Migration Descope Note (Effort: S, Priority: Low, Deps: Story 1)

| AC | Testable Element | Test Type |
|----|-------------------|-----------|
| 05-01 | `docs/external-migration.md` exists, states the workspace write-access boundary, cites Epic 12.0 | Manual |
| 05-02 | Numbered checklist (replace fragmented skills, copy/adapt `skill/SKILL.md` template, preserve `.planning/` hooks, validate against `docs/code-review-backend.md`) present and discoverable via cross-link | Manual |
| 05-03 | No writes/staged commits made to `~/Documents/GitHub/claude-prompts/` | Manual |

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** `cmd/atcr/` (Go stdlib `testing` + `testify`, table-driven, `t.Run` subtests), plus `skill/skill_test.go` for embedded SKILL.md content assertions.

**Test File Placement Examples:**
- `cmd/atcr/backend_contract_test.go` (new — Story 2, AC02-01/02/03)
- `skill/skill_test.go` (updated — Story 1, AC01-01/02/03/04)
- Story 3's install-script ACs: no existing shell-test convention in the repo; either a small `os/exec`-based Go test or documented manual verification (decide at `/create-sprint`).

**Unit/Integration/E2E:**
- Unit (4 ACs): dispatcher routing table, secondary-file verbatim split, frontmatter/line-budget — all in Story 1, asserting against the embedded `SkillMD` string.
- Integration (5 ACs): backend contract output-tree + id-or-path resolution (Story 2, hermetic via `httptest.NewServer` + `os/exec` git fixtures); install-script core flow + prereq/PATH checks (Story 3).
- E2E (1 AC): README Quickstart walkthrough for `install.sh` (Story 3).
- Manual (7 ACs): all of Story 4's documentation accuracy pass, all of Story 5's external-migration note, and Story 1's `docs/skill-usage.md` consistency check — no automated assertion, human-reviewed against ground-truth code/docs.

**Test Environment Status:**
- Framework: Go stdlib `testing` + `github.com/stretchr/testify` (assert/require) — already in `go.mod`, ready to use.
- Execution: `llm_support_discover_tests` returned `pattern: UNKNOWN` (its heuristics don't recognize Go's convention), but codebase-discovery.json and direct inspection confirm the established convention: co-located `*_test.go` files per Cobra subcommand (`cmd/atcr/review_test.go`, `cmd/atcr/reconcile_test.go`), in-process execution via `newRootCmd()`/`ExecuteContext`, with a `//go:build integration` tag reserved for any real-subprocess cases.
- Coverage Tools: `go test -coverprofile=coverage.out ./...` already configured (project config), baseline 80%.

---

## Architecture

**Primitives:** Cobra command registry (`newRootCmd`, `cmd/atcr/main.go:185-208`); SKILL.md frontmatter + Level 2/Level 3 markdown split; the documented `--output-dir` tree contract (`manifest.json`, `payload/`, `sources/pool/`, `reconciled/`); `install.sh` shell script.

**Module Boundaries:** `skill/SKILL.md` only ever invokes the `atcr` binary as a subprocess — it must never reach into engine internals directly, preserving the existing "Orchestration Steps" convention. `install.sh` only wraps `go install`; it must not duplicate `atcr doctor`'s self-test role. The new contract test treats `internal/fanout/reviewdir.go` as a read-only dependency (not modified) and asserts only the externally-documented contract surface, not `ReviewsRoot`'s internals (already unit-tested elsewhere).

**External Dependencies:** Go 1.24+ toolchain (`go install`); no new third-party packages — `testing` and `testify` are already present in `go.mod`.

**Replaceability:** Secondary skill markdown files (`skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md`) load on demand and can evolve independently of the dispatcher's routing table in `skill/SKILL.md`, per Claude Code's three-level progressive-disclosure model.

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|---------------------|
| `install.sh` distribution | Repo root shell script, likely distributed via README `curl \| bash` pattern | Supply-chain tampering of the script itself; script silently executing more than the documented `go install` step | Keep the script minimal and auditable (~40 lines per story constraint); no dynamic remote-code fetch beyond `go install`, which is itself checksum-verified by the Go module proxy |
| Dispatcher command routing | `skill/SKILL.md` routing table mapping user intent → `atcr` CLI commands | Routing table drifting to accept invented/unregistered command names, effectively widening the CLI surface an agent can invoke | Cross-check every routed command name against `cmd/atcr/main.go:185-208` (ground truth, not the stale `cobra.md` snapshot) in AC01-01's unit test |
| Backend contract test isolation | `cmd/atcr/backend_contract_test.go` | Test accidentally making real network calls or writing outside its temp fixture, leaking state across CI runs | `httptest.NewServer` for all provider interaction; `t.TempDir()` for all fixture output; `os/exec` (never shell string interpolation) for git fixture setup |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|----------------|--------|----------|
| SKILL.md Level 2 body loading | Loaded into every triggered agent session | Stay under ~500 lines to control context/token cost | Move Host Review, Ambiguity Adjudication, and Findings Format content to Level 3 secondary files loaded only when that code path executes |
| Backend contract test execution | Runs on every CI invocation of `go test ./cmd/atcr/...` | Fast enough not to slow the default test loop | Prefer in-process execution via `newRootCmd()`/`ExecuteContext` (matching `reconcile_test.go`'s `execCmd` pattern) over a real subprocess; gate any real-binary subprocess variant behind `//go:build integration` |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|---------------------|
| Dispatcher command resolution | Unregistered/typo'd command name; ambiguous natural-language intent | Dispatcher routes only to exact registered Cobra command names; ambiguous intent is clarified with the user, never guessed into an invented command |
| `install.sh` environment variance | Go toolchain absent; `go install` network/proxy failure; `$(go env GOPATH)/bin` already on `$PATH`; already-installed `atcr` binary (upgrade case) | Non-zero exit + clear stderr for missing Go or install failure; non-fatal PATH warning; silent success on re-run/upgrade (idempotent `go install`) |
| Id-or-path resolution (backend contract) | Bare review id, explicit path argument, omitted argument | Resolves to `.atcr/reviews/<id>/`, the given path, or `.atcr/latest` respectively, exactly as `docs/code-review-backend.md` documents |
| Documentation drift | Command renamed/removed by the dispatcher rewrite; artifact path changed; install instructions now duplicated/contradictory | Story 4's verification pass catches and corrects drift before sprint completion; no doc restructuring beyond closing confirmed inaccuracies |

### Defensive Measures Required

- **Input Validation:** Dispatcher routing table only accepts command names present in `newRootCmd`'s registration; no free-form command construction.
- **Error Handling:** `install.sh` distinguishes fatal failures (missing Go, failed `go install` — non-zero exit, stderr message) from non-fatal warnings (PATH misconfiguration — warn, continue, exit 0).
- **Logging/Audit:** Contract test favors `assert` over `require` for the bulk of output-tree checks so multiple missing files surface in a single run rather than stopping at the first failure.
- **Rate Limiting:** Not applicable — no new network-facing service introduced by this sprint.
- **Graceful Degradation:** `install.sh` succeeds even when `$(go env GOPATH)/bin` is absent from `$PATH`, degrading to a warning plus a suggested fix rather than blocking installation.

---

## Risks

**Technical:**
- Dispatcher rewrite regresses existing review→reconcile→report orchestration relied on by current adopters → Preserve all orchestration steps and Host Review/Ambiguity Adjudication instructions verbatim in secondary files; the rewrite changes only the entry surface.
- Command-routing table drifts from the actual Cobra command tree → Treat `cmd/atcr/main.go:185-208` as sole source of truth; verify via AC01-01's unit test, not the stale `cobra.md` doc snapshot.
- `install.sh` scope creep into release-automation territory (checksum verification, version pinning, OS/arch detection) → Constraint carried from two prior epic decisions (Epic 21.0 owns that work); AC review rejects any addition beyond the thin `go install` wrapper.

**TDD-Specific:**
- Story 1's RED phase requires asserting against content that doesn't exist yet (new secondary files, new routing table) — write failing assertions in `skill/skill_test.go` first, confirm they fail for the right reason (missing file/content), then perform the minimal relocation/rewrite to turn them green.
- Story 2 risks becoming a rewrite of already-passing unit tests rather than a new regression lock — scope assertions strictly to the externally-documented contract surface (`docs/code-review-backend.md`), not to `internal/fanout/reviewdir.go` internals already covered elsewhere.
- Story 3's install-script ACs have no pre-existing shell-test harness to anchor RED against — `/create-sprint` must decide the concrete test mechanism (Go `os/exec` harness vs. documented manual verification) before TDD sequencing can begin for that story.

---

**Next:** `/create-sprint @.planning/plans/active/20.0_standalone_skill_release/ --gated`
