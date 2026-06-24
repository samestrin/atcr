# Sprint Design: Persona Ecosystem

**Created:** June 24, 2026
**Plan:** [Plan 9.0: Persona Ecosystem](./)
**Plan Type:** Feature ✨
**Status:** Design Complete

---

## Original User Request

> Expand ATCR's reviewer panel beyond the 6 generalist built-in personas by (a) shipping a curated set of domain-specific bonus personas with the binary, and (b) establishing a community-contributed persona repo with a quality bar, CLI integration, and fixture-based CI testing. Personas become the primary lever for vertical market adoption.

**Referenced Resources:**

- [Epic 9.0: Persona Ecosystem](original-requirements.md)
  - **Summary:** Full epic spec for the persona ecosystem, including all tasks, clarifications, and the re-scope decision (T3/T4/community-T7 descoped to a separate work item).
  - **Key Points:** Two-sprint structure decided (A: T8+T1, B: T2+T5+T6+T7-in-repo); `AgentConfig.Language` canonical form decided (no leading dot, lowercased); `SelectEligibleSkeptics` 4th parameter shape decided (`map[string]float64`, nil-safe).

**CRITICAL:** All sprint implementation must deliver on this original request.

---

## Configuration

**Sprint Name:** Persona Ecosystem
**Complexity:** 10/12 (VERY COMPLEX)
**Timeline:** 17 days across two sprints
**Phases:** 6
**Pattern:** Foundation → Core Routing → Built-in Personas → CLI Surface → Bundles+Scores → Docs+Validation

---

## Memory Search Context

Pre-generated semantic search phrases for `/execute-sprint` to query project memory:

```
Go cobra CLI subcommand registration patterns
go embed filesystem persona registry pattern
httptest.NewServer injectable http client Go testing
slice partition reorder skeptic language routing Go
yaml.v3 optional field backward compatibility Go registry
```

---

## Complexity Breakdown

- **Architecture:** 2/3 — New `internal/personas` package (greenfield) + new 6-subcommand Cobra tree; all built on existing Cobra/yaml.v3/embed.FS patterns already in the codebase
- **Integration:** 3/3 — Five cross-package integrations: `internal/personas` ↔ `registry` ↔ `scorecard` ↔ `net/http` ↔ `cmd/atcr`; httptest.NewServer isolation required throughout
- **Story/Task & Test:** 3/3 — 6 user stories, 26 ACs, 18 unit + 11 integration + 2 manual tests; 5 high-complexity ACs (02-01, 02-05, 02-06, 03-02, 03-05)
- **Risk/Unknown:** 2/3 — Caller-count assumption for `SelectEligibleSkeptics` requires grep verification; `normalizeExt` consistency between load-time and match-time; corroboration key casing; configurable URL format behavior

**Time Formula:** VERY COMPLEX base (13+ days) + 6 phases × ~3 days/phase = 18 days, adjusted for pre-committed two-sprint structure → 17 working days
**Calculation:** Sprint A (~7 days: T8 phases 1-2 + T1 phase 3) + Sprint B (~10 days: T2 phase 4 + T5/T6 phase 5 + T7-in-repo phase 6)

---

## Recommended Flags

**Adversarial:** true
**Gated:** true
**Recommendation strength:** STRONG (complexity 10/12)
**Suggested command:** `/create-sprint @.planning/plans/active/9.0_persona_ecosystem/ --gated`

Thresholds: adversarial triggered by complexity >= 6/12 or phases >= 3 ✓; gated triggered by complexity >= 8/12, phases >= 5, or duration > 5 days ✓; strong gated at complexity >= 10/12 ✓

---

## Phase Structure

### Phase 1 — T8: AgentConfig Language Field (Sprint A, Days 1-2)
**Focus:** Schema change — `Language []string` on `AgentConfig`, validation, canonicalization  
**Items:** Story 03 / AC 03-01, AC 03-05 (partial)
**Key files:**
- `internal/registry/config.go` — add `Language []string \`yaml:"language,omitempty"\``, extend `validateAgent` (reject empty entries + control chars), extend `applyDefaults` (trim space, strip leading dot, lowercase)
- `internal/verify/select.go` — add `normalizeExt(ext string) string` helper (strips dot, lowercases); used by both load-time and match-time

**TDD cycle:**
- RED: `TestAgentConfig_LanguageField_Validation`, `TestAgentConfig_LanguageField_Canonicalization`, `TestNormalizeExt_WithAndWithoutDot`
- GREEN: Implement field + validation + canonicalization + normalizeExt helper
- REFACTOR: Confirm `normalizeExt` is shared by both `applyDefaults` and the routing path (no duplication)

**Exit gate:** `go test ./internal/registry/... ./internal/verify/...` green; `go build ./...` clean

---

### Phase 2 — T8: SelectEligibleSkeptics Routing (Sprint A, Days 3-4)
**Focus:** Two-partition reorder in `select.go`; pipeline caller signature update  
**Items:** Story 03 / AC 03-02, AC 03-03, AC 03-04, AC 03-05 (remainder)

**Key files:**
- `internal/verify/select.go:55` — `SelectEligibleSkeptics(agents []AgentConfig, finding Finding, n int, scores map[string]float64) []string`; after `sort.Strings(names)`, partition into matched (finding file ext ∈ skeptic's Language) and unmatched; rebuild as `append(matched, unmatched...)`; within matched partition: sort by `scores[name]` descending then name ascending; nil map → alphabetical-only within matched partition
- `internal/verify/pipeline.go:162` — update sole production caller to pass scores map (nil acceptable until T6 wires it)
- `internal/verify/select_test.go` — `TestSelectEligibleSkeptics_LanguageMatch`, `_NoMatchFallback`, `_TieBreakByScore`, `_TieBreakAlphabeticalWhenNoScores`, `_NilScoresMap`, `_BackwardCompatNoLanguageField`

**Pre-implementation check:** `grep -r "SelectEligibleSkeptics" ./internal/` — confirm single caller; update any additional callers in same commit if found

**TDD cycle:**
- RED: All 5 routing test scenarios fail
- GREEN: Implement two-partition reorder + nil-safe score sort
- REFACTOR: Pre-allocate slices with `make([]string, 0, len(names))` to avoid growth copies

**Exit gate:** `go test ./internal/verify/...` green; no API leakage from verify into scorecard package (score map built by caller, not imported here)

---

### Phase 3 — T1: Bonus Built-In Personas (Sprint A, Days 5-7)
**Focus:** 3 persona .md files + 3 fixture files + updated registry + CI-passing tests  
**Items:** Story 01 / AC 01-01, AC 01-02, AC 01-03

**Key files:**
- `personas/personas_test.go` — rename `TestNames_ReturnsAllSix` → `TestNames_ReturnsAllNine`; update expected count from 6 to 9; add `TestGet_BonusPersonasNonEmpty`, `TestBonusPersonas_TemplateRenders`
- `personas/personas.go` — append `"sentinel"`, `"tracer"`, `"idiomatic"` to `names` slice (canonical position: after `"dax"`, before `"otto"`)
- `personas/sentinel.md` — security persona: OWASP Top 10, SQL/command injection, secrets leakage, insecure defaults; follows bruce.md template structure
- `personas/tracer.md` — performance persona: N+1 queries, memory leaks, allocation hot paths, escape analysis
- `personas/idiomatic.md` — Go idioms persona: error handling conventions, goroutine leaks, sync primitive misuse, stdlib misuse
- `personas/testdata/sentinel_fixture.patch` — synthetic SQL string concatenation (`query := "SELECT * FROM users WHERE id = " + userInput`)
- `personas/testdata/tracer_fixture.patch` — ORM call inside a `for` loop
- `personas/testdata/idiomatic_fixture.patch` — ignored error return (`val, _ := strconv.Atoi(s)`)
- `personas/personas_test.go` — `TestSentinelFixture`, `TestTracerFixture`, `TestIdiomaticFixture` using `testFixture` helper

**Fixture content rules:** Synthetic values only (e.g., `FAKE_API_KEY_00000000`); committed with mode 0644; no live network calls in rendering path

**TDD cycle:**
- RED: Rename test + update count (fails at 6, wants 9) — commit this as standalone RED commit
- GREEN: Add .md files + fixture files + names slice update (all in same commit to avoid CI window)
- REFACTOR: Review persona template quality; ensure all variable slots match bruce.md exactly

**Exit gate:** `go test ./personas/...` green including all 3 fixture tests; no outbound connections in test run

---

### Phase 4 — T2: atcr personas CLI (Sprint B, Days 8-11)
**Focus:** New `internal/personas` package + 6 Cobra sub-subcommands + atomic test update  
**Items:** Story 02 / AC 02-01, 02-02, 02-03, 02-04, 02-05, 02-06

**New files:**
- `internal/personas/client.go` — `RegistryBaseURL = "https://raw.githubusercontent.com/atcr/personas/main"`; injectable `http.Client` interface; env var `ATCR_PERSONAS_URL` override
- `internal/personas/paths.go` — `PersonasDir() string`; uses `os.UserConfigDir()`; overridable in tests
- `internal/personas/install.go` — `Install(client HTTPClient, baseURL, name, destDir string) error`; fetch → `validateAgent` → write; path traversal guard on `name` (`[a-zA-Z0-9_/-]+`, reject `..`)
- `internal/personas/list.go` — `List(personasDir string) ([]PersonaEntry, error)`; merges built-in (from `personas.Names()`) + community (from `os.ReadDir`); graceful on missing dir
- `internal/personas/search.go` — `Search(client, baseURL, keyword string) ([]PersonaEntry, error)`; fetches `index.json` from community repo
- `internal/personas/remove.go` — `Remove(name, personasDir string) error`
- `internal/personas/upgrade.go` — `Upgrade(client, baseURL, name, personasDir string, dryRun bool) error`; version comparison via `golang.org/x/mod/semver` (transitive dep)
- `internal/personas/bundles/` — deferred to Phase 5
- `cmd/atcr/personas.go` — `newPersonasCmd()` + 6 sub-subcommands: `install`, `list`, `search`, `remove`, `test`, `upgrade`
- `cmd/atcr/main.go` — `root.AddCommand(newPersonasCmd())` (in same commit as subcommand count test update)
- `cmd/atcr/main_test.go` — `TestRootCmd_HasExactlyFifteenSubcommands` (renamed from fourteen, count bumped to 15)
- `cmd/atcr/personas_test.go` — integration tests via `httptest.NewServer` for each subcommand

**Atomic commit rule:** `newPersonasCmd()` registration in `main.go` + `TestRootCmd` count bump to 15 must land in the same commit to avoid CI failure window.

**TDD cycle:**
- RED: All 6 subcommand tests + root count test fail
- GREEN: Implement package + register command
- REFACTOR: Consolidate HTTP client injection pattern; ensure all tests use temp dirs for PersonasDir

**Exit gate:** `go test ./cmd/atcr/... ./internal/personas/...` green; zero live network calls in CI; `go build ./...` clean

---

### Phase 5 — T5 Domain Bundles + T6 Corroboration Scores (Sprint B, Days 12-14)
**Focus:** Bundle resolver + YAML manifests + `--scores` flag wired to scorecard  
**Items:** Story 04 / AC 04-01–04-05; Story 05 / AC 05-01–05-04

**T5 files:**
- `internal/personas/bundles.go` — `Resolve(name string) ([]string, error)`; embedded `go:embed bundles/*.yaml`; typed `ErrUnknownBundle`; validates manifest at parse time (missing `name`/`personas` fields → error)
- `internal/personas/bundles/django.yaml` — members: `django-orm`, `python-types`, `security/owasp`, `security/secrets`
- `internal/personas/bundles/go-production.yaml` — members: TBD from plan context (idiomatic + sentinel + tracer coverage)
- `internal/personas/install.go` — detect `bundle/` prefix via `strings.HasPrefix`; delegate to `bundles.Resolve` then loop single-persona install
- `internal/personas/bundles_test.go` — `TestBundleResolve_Django`, `_GoProduction`, `_Unknown`, `_PartialInstallSkip`, `_ManifestParseMissingFields`

**T6 files:**
- `internal/personas/list.go` — extend `List()` or add `ListWithScores()` accepting `map[string]float64`; join on `strings.ToLower` of reviewer name; format rate as `"XX.X%"` or `"n/a"`; sort: numeric descending, then n/a alphabetically
- `cmd/atcr/personas.go` — wire `--scores` boolean flag to `list` subcommand; call `scorecard.Aggregate()` when flag set; pass resulting map to list logic
- `internal/personas/list_test.go` — `TestPersonasList_WithScores_HasRate`, `_NaForMissing`, `_SortOrder`, `_BaselineNoRegression`

**Score map key convention:** `strings.ToLower(reviewerName)` — same normalization on both sides of the join (persona list + scorecard aggregate).

**Exit gate:** `go test ./internal/personas/...` green for all bundle and score tests; `atcr personas install bundle/django` integration test passes against httptest server

---

### Phase 6 — T7-in-repo: Docs + Validation (Sprint B, Days 15-17)
**Focus:** Installation guide, authoring template, registry.md update, example YAML updates, cumulative adversarial review  
**Items:** Story 06 / AC 06-01, 06-02, 06-03

**New files:**
- `docs/personas-install.md` — covers all 6 `atcr personas` subcommands, bundle installation syntax, `~/.config/atcr/personas/` install path, `ATCR_PERSONAS_URL` env var override
- `docs/personas-authoring.md` — persona template (prompt section, severity rubric, output format, payload variable slots), canonical `language` format rules (`["go", "ts"]` — no leading dot, lowercased), fixture file requirements (`.patch`/`.diff` in `personas/testdata/`, synthetic values only), contribution checklist

**Modified files:**
- `docs/registry.md` — add `language` field reference entry: type (`[]string`), canonical form, nil semantics (no constraint), routing behavior (two-partition reorder, silent fallback)
- `examples/registry-without-executor.yaml` — add at least one agent definition with optional `language: ["go"]` example
- `examples/registry-with-executor.yaml` — same; must remain valid YAML after edit; run `go test ./...` to confirm

**Validation checks:**
- `TestRegistryExamples_Valid` — loads both example YAML files through `internal/registry` to confirm they parse cleanly after `language` field additions
- No reference to deprecated `docs/examples/registry.yaml` path in any new or modified file
- Authoring guide fixture field list cross-referenced against `TestPersonaFixture` test logic

**Cumulative adversarial review:**
- `go test ./...` clean (all packages)
- `golangci-lint run` clean
- `go vet ./...` clean
- Integration smoke: install → list → test fixture roundtrip via httptest
- `go build ./...` produces clean binary

---

## Work Decomposition

### Sprint A — Registry + Verify Internals (Phases 1-3)

| Phase | Story | AC(s) | Key Files | Test Type |
|-------|-------|-------|-----------|-----------|
| 1 | 03 | 03-01, 03-05 (partial) | `internal/registry/config.go`, `internal/verify/select.go` (normalizeExt only) | Unit |
| 2 | 03 | 03-02, 03-03, 03-04, 03-05 (remainder) | `internal/verify/select.go`, `internal/verify/pipeline.go:162` | Unit + Integration |
| 3 | 01 | 01-01, 01-02, 01-03 | `personas/*.md`, `personas/personas.go`, `personas/personas_test.go`, `personas/testdata/` | Unit + Integration |

### Sprint B — Surface Layer (Phases 4-6)

| Phase | Story | AC(s) | Key Files | Test Type |
|-------|-------|-------|-----------|-----------|
| 4 | 02 | 02-01–02-06 | `cmd/atcr/personas.go`, `internal/personas/*.go`, `cmd/atcr/main.go`, `cmd/atcr/main_test.go` | Unit + Integration |
| 5 | 04, 05 | 04-01–04-05, 05-01–05-04 | `internal/personas/bundles.go`, `bundles/*.yaml`, `internal/personas/list.go` | Unit + Integration |
| 6 | 06 | 06-01, 06-02, 06-03 | `docs/personas-install.md`, `docs/personas-authoring.md`, `docs/registry.md`, `examples/*.yaml` | Manual + Unit |

---

## Test Strategy

**PRIMARY_TEST_LOCATION:** `*_test.go` files in same package directory as source (Go convention)

**Test File Placement Examples:**
- `personas/personas_test.go` — bonus persona registry + fixture tests
- `internal/verify/select_test.go` — routing unit tests
- `internal/registry/config_test.go` — Language field validation/canonicalization
- `internal/personas/install_test.go`, `list_test.go`, `bundles_test.go` — new package tests
- `cmd/atcr/personas_test.go` — CLI integration tests via httptest.NewServer

**Unit Tests (18 ACs):** `go test` stdlib + `github.com/stretchr/testify/assert`; table-driven tests for all validation/canonicalization paths; in-process only

**Integration Tests (11 ACs):** `httptest.NewServer` for all HTTP fetch logic; `os.MkdirTemp` / `t.TempDir()` substituted for `PersonasDir()`; `//go:build integration` tag on filesystem-state-modifying tests

**Manual Review (2 ACs):** AC 06-01 (install guide walkthrough), AC 06-02 (authoring guide validation); no automated gate — verified by reading and following the docs without source-code lookups

**Test Environment Status:**
- Framework: Go test (stdlib) + testify/assert — present in codebase
- Execution: `go test ./...` — functional
- Coverage Tools: `go test -coverprofile=coverage.out ./...` — functional
- CI gate: zero live network calls allowed; all HTTP interaction uses httptest.NewServer

**Coverage baseline:** 80% (from config); new packages (`internal/personas/`) target ≥80%

---

## Architecture

**Primitives:**
- `AgentConfig` — core persona config; gains `Language []string \`yaml:"language,omitempty"\``
- `PersonaEntry` — list output row: name, source (built-in/community), version, corroborationRate
- `BundleManifest` — `{Name string; Description string; Personas []string}` parsed from embedded YAML
- `map[string]float64` — corroboration score carrier; nil = no data; keyed by lowercase reviewer name; shared between `SelectEligibleSkeptics` (4th param) and `atcr personas list --scores`

**Module Boundaries:**
- `personas` (public API): `Names() []string`, `Get(name string) (string, error)`, `Render(name string, payload Payload) (string, error)` — read-only, embedded, no I/O
- `internal/personas` (new): all lifecycle management; exposes `Install`, `List`, `Search`, `Remove`, `Upgrade`, `bundles.Resolve`; all HTTP behind injectable `HTTPClient` interface
- `internal/verify/select.go`: `SelectEligibleSkeptics(agents []AgentConfig, finding Finding, n int, scores map[string]float64) []string` — pure function; no I/O; decoupled from scorecard package
- `internal/registry/config.go`: `AgentConfig` (schema) + `applyDefaults` + `validateAgent` — single source of canonicalization for Language entries
- `internal/scorecard`: `Aggregate() []LeaderboardRow` — existing; consumed by `list --scores` via caller-built map

**External Dependencies:**
- `github.com/spf13/cobra` — already present; used for all CLI work
- `gopkg.in/yaml.v3` — already present; used for bundle manifest parsing
- `net/http` (stdlib) — sufficient for community repo fetch
- `golang.org/x/mod/semver` — transitive dep; used for version comparison in upgrade

**Replaceability:**
- `internal/personas` HTTP layer: swap `RegistryBaseURL` and `HTTPClient` impl without changing callers
- Bundle manifests: add new bundles by dropping a YAML file into `internal/personas/bundles/`; no Go changes
- Score carrier: `map[string]float64` is caller-supplied; scorecard implementation can change without touching verify

---

## Risk Analysis

**Purpose:** Pre-identified risks for verification during `/execute-code-review` adversarial phase.

### Security-Sensitive Areas

| Area | Scope | Attack Vectors | Defensive Measures |
|------|-------|----------------|-------------------|
| Persona install path construction | `internal/personas/install.go` | Path traversal via user-supplied persona name (e.g., `../../etc/passwd`) | Validate `name` matches `[a-zA-Z0-9_/-]+`; reject names containing `..` or absolute path segments before constructing destination path |
| Fixture file synthetic secrets | `personas/testdata/*.patch` | Commit scanning flags real credentials if fixtures use non-synthetic values | Use clearly fake values: `FAKE_API_KEY_00000000`, `FAKE_SECRET_XXXXXXXX`; document in authoring guide |
| YAML from community repo | `internal/personas/install.go` | Malicious YAML exploiting registry behavior | Run `validateAgent` against fetched YAML before any disk write; reject and return error without writing if validation fails |

### Performance-Critical Paths

| Path | Expected Load | Target | Strategy |
|------|---------------|--------|----------|
| `SelectEligibleSkeptics` two-partition reorder | Called once per finding in verify pipeline; O(n) where n = eligible skeptic count | < 1 ms | Pre-allocate `matched`/`unmatched` with `make([]string, 0, len(names))` to avoid growth copies; benchmark if verify pipeline shows regression |
| `scorecard.Aggregate()` JSONL scan | Called once per `atcr personas list --scores` invocation | < 500 ms on typical JSONL | Existing path already used by leaderboard display; no new I/O introduced; cached summary file is future upgrade if needed |

### Edge Case Categories

| Category | Scenarios | Expected Behavior |
|----------|-----------|-------------------|
| `normalizeExt` consistency | Dot-prefixed vs dotless input at load-time vs match-time | Both paths call the same `normalizeExt` helper; unit test covers `.go` → `go` and `go` → `go` idempotency |
| Nil scores map in routing | `SelectEligibleSkeptics` called with `nil` scores (before T6 lands, or when no scorecard data exists) | Nil map → zero value for all score lookups; matched partition sorts alphabetically; no panic, no error |
| Missing personas directory | `atcr personas list` on fresh install with no `~/.config/atcr/personas/` | `os.ReadDir` on missing dir returns error; `List()` treats this as empty set and returns only built-ins with no error |
| `go:embed` pickup scope | Unexpected `.md` files added to `personas/` during development | `Names()` exposes only entries in explicit `names` slice; `Get(name)` rejects names not in the slice regardless of what is embedded |
| Subcommand count test CI window | Registering `newPersonasCmd()` and bumping `TestRootCmd` count in separate commits | Atomic commit: register command + update test in same commit; RED phase uses temporary blank command |

### Defensive Measures Required

- **Input Validation:** Persona name in `install`/`remove`/`test`/`upgrade` validated against `[a-zA-Z0-9_/-]+` pattern; `..` and absolute paths rejected; YAML from community repo validated via `validateAgent` before write
- **Error Handling:** Typed errors (`ErrUnknownBundle`, `ErrPersonaNotFound`) so callers produce user-facing messages without string matching; `install` reports per-persona outcome on bundle installs for idempotent recovery
- **Logging/Audit:** No sensitive data logged; descriptive errors to stderr, success messages to stdout; `upgrade --dry-run` prints what would change without writing
- **Rate Limiting:** No rate limiting required (CLI tool, not server); single concurrent install
- **Graceful Degradation:** `list` returns built-ins only when personas directory is unreadable; `--scores` shows `n/a` for all personas with footer note when scorecard file is absent; language routing falls back to alphabetical pool silently when no language-matched skeptic exists

---

## Risks

**Technical:**
- `SelectEligibleSkeptics` single-caller assumption → `grep -r "SelectEligibleSkeptics" ./internal/` before implementing; update any additional callers in same commit
- `normalizeExt` used in both `applyDefaults` and routing → extract as shared package-level helper; covered by dedicated unit test with dot-prefixed and dotless inputs
- `go:embed bundles/*.yaml` picks up unexpected files → enumerate bundle names explicitly in `Resolve`; unknown names return `ErrUnknownBundle`
- Example YAML files become invalid after `language` field additions → run `go test ./...` after editing; `TestRegistryExamples_Valid` catches parse failures
- Corroboration join key casing drift → use `strings.ToLower` on both `PersonaEntry.Name` and `LeaderboardRow.ReviewerName` in the join; unit test covers mixed-case fixture

**TDD-Specific:**
- `TestNames_ReturnsAllSix` breaks CI before persona `.md` files exist if name-slice and test changes land in separate commits → rename test + update count + add `.md` files + update names slice all in the same GREEN commit (local RED verified first)
- `TestRootCmd_HasExactlyFifteenSubcommands` fails between command registration and test update → register `newPersonasCmd()` + update test count in same commit
- Fixture produces findings but in unexpected category → assert on category string (e.g., `"injection"`, `"n+1"`, `"error"`) rather than just non-empty output; use unambiguous canonical patterns in fixture diffs
- Persona template render test tied to payload struct shape → reuse existing `Payload` struct from `personas` package; do not create a parallel test-only struct

---

**Next:** `/create-sprint @.planning/plans/active/9.0_persona_ecosystem/ --gated`
