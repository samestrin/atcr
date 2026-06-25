# Plan 9.0: Persona Ecosystem

## Plan Overview
**Plan Type:** feature
**Last Modified:** 2026-06-24
**Plan Goal:** Expand ATCR's reviewer panel beyond the 6 generalist built-in personas by shipping 3 domain-specific bonus personas (`sentinel`/security, `tracer`/performance, `idiomatic`/Go idioms), adding an `atcr personas` CLI for community repo integration, wiring per-persona corroboration scores into `atcr personas list --scores`, and extending skeptic selection with language-aware routing. Personas become the primary lever for vertical market adoption.
**Target Users:** Go developers, security engineers, performance engineers, framework-specific teams (Django, React), platform leads managing team review configuration.
**Framework/Technology:** Go 1.22+, Cobra CLI, yaml.v3, embed.FS, net/http (stdlib)

## Objectives

1. Ship 3 domain-specific bonus personas (`sentinel`, `tracer`, `idiomatic`) bundled with the ATCR binary, each backed by a CI-tested fixture that verifies the persona produces its expected finding category.
2. Add an `atcr personas` CLI with `install`, `remove`, `list`, `search`, `test`, and `upgrade` subcommands, fetching from a configurable community-repo URL and writing installed personas to `~/.config/atcr/personas/`.
3. Implement domain bundle installation (`atcr personas install bundle/<name>`) with initial `bundle/django` and `bundle/go-production` manifests.
4. Extend `SelectEligibleSkeptics` with language-aware routing using a new `AgentConfig.Language` field, preferring language-matched skeptics and silently falling back to the general pool when no match exists.
5. Wire per-persona corroboration scores into `atcr personas list --scores` using existing scorecard data, making persona ROI visible and data-driven.
6. Provide in-repo documentation (`docs/personas-install.md` and `docs/personas-authoring.md`) so teams can install and author personas without reading source code.

> **Scope note:** Community-repo scaffolding (T3), seeding 8–10 external personas (T4), and the community-repo contribution guide/CI workflow (community half of T7) are explicitly descoped per the 2026-06-23 clarifications. This plan covers only in-repo work: T1, T2, T5, T6, T8, and the in-repo docs portion of T7.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 6 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Generated — 26 criteria covering all 6 user stories

## Feature Analysis Summary
The plan is pre-divided into two sprints by the epic clarifications. **Sprint A** (registry + verify internals): T8 (`Language []string` field on `AgentConfig` + `SelectEligibleSkeptics` two-partition reorder) lands first because `AgentConfig` is imported by nearly every internal package; the schema change must be verified-green before T1 builds on it. T1 (bonus personas `sentinel`/`tracer`/`idiomatic` + test fixtures + CI step) follows immediately within the same sprint. **Sprint B** (surface layer): T2 (`atcr personas` CLI — 6 cobra sub-subcommands, greenfield `internal/personas` package), T5 (domain bundle YAML manifest + `install bundle/` resolution + `django` and `go-production` bundles), T6 (per-persona corroboration wiring — `--scores` flag reading from scorecard JSONL), and T7-in-repo (installation guide + persona authoring template).

The `SelectEligibleSkeptics` change is a contained two-partition reorder on the already-sorted `names` slice in `select.go`. Language-matched skeptics move to the front; the existing `n`-cap naturally selects them first. The corroboration score carrier is a caller-supplied `map[string]float64` added as a 4th parameter (nil-safe: a nil map means "no score data" and the matched partition sorts alphabetically). Only one production caller exists (`internal/verify/pipeline.go:162`), so the signature change is contained. The `atcr personas` CLI fetches from a configurable URL (hardcoded default: community repo raw endpoint), tested via `httptest.NewServer` — no live external calls in CI.

## Technical Planning Notes
- **AgentConfig.Language:** New `Language []string \`yaml:"language,omitempty"\`` field — nil = no constraint, backward-compatible. Follows the exact pattern of `Scope []string`. Do NOT reuse `Scope` — it means prompt-injection focus categories injected via `payload.ScopeFocus()`, a semantic collision. **Canonical form (decided 2026-06-24): without leading dot, lowercased** (e.g. `["go", "ts"]`). `validateAgent` rejects empty entries and control characters; `applyDefaults` canonicalizes at load by trimming space, stripping a single leading dot, and lowercasing — mirroring `MinSeverity`/`Role` canonicalization.
- **SelectEligibleSkeptics two-partition reorder:** After `sort.Strings(names)`, partition into language-matched (finding file extension ∈ skeptic's `Language`) and unmatched; rebuild `names` as `append(matched, unmatched...)`. Existing `n`-cap at `select.go:84-86` favors matched skeptics. Tie-break within matched: corroboration score desc (from the caller-supplied `map[string]float64`), then alphabetical (already held from sort); a nil or missing score falls back to alphabetical-only ordering. Matching normalizes both sides via `normalizeExt(filepath.Ext(finding.File))` that strips the leading dot and lowercases.
- **Bonus personas embed:** `personas/personas.go:names` slice grows from 6 to 9 (add `sentinel`, `tracer`, `idiomatic`). `go:embed *.md` picks up new `.md` files automatically. `TestNames_ReturnsAllSix` must be renamed and updated to 9 first (TDD RED before GREEN). Fixtures live in `personas/testdata/` as `.patch`/`.diff` files.
- **CLI subcommand count:** `cmd/atcr/main_test.go:TestRootCmd_HasExactlyFourteenSubcommands` must be updated to 15 atomically with adding `newPersonasCmd()` to avoid a CI failure window between the two changes.
- **`internal/personas` package:** New package for install/list/search/upgrade logic with a configurable `RegistryBaseURL` constant (default: `https://raw.githubusercontent.com/atcr/personas/main`). All HTTP fetch tests use `httptest.NewServer` — no live network calls in CI.
- **Registry documentation updates (T7-in-repo):** `docs/registry.md` must document the new `language` field and the language-aware skeptic routing behavior. `examples/registry-without-executor.yaml` and `examples/registry-with-executor.yaml` should add optional `language` examples for agent definitions. The deprecated `docs/examples/registry.yaml` path no longer exists and must not be referenced.

## Documentation References

### [CRITICAL] Must Read Before Coding
- [Bonus Built-In Personas](documentation/bonus-personas.md) — `sentinel`, `tracer`, `idiomatic` personas, embedded registration, and fixture expectations (T1)
- [Cobra CLI Patterns](documentation/cobra-cli-patterns.md) — Cobra subcommand architecture for `atcr personas` CLI and 6 sub-subcommands (T2)
- [YAML Bundle Manifests](documentation/yaml-bundle-manifests.md) — YAML v3 bundle manifest parsing and `AgentConfig.Language` field (T5/T8)
- [Skeptic Routing & Verification](documentation/skeptic-routing-verification.md) — `SelectEligibleSkeptics` extension for language-aware routing (T8)

### [IMPORTANT] Review During Development
- [HTTP & Standard Library Testing](documentation/http-stdlib-testing.md) — `net/http` + `httptest.NewServer` patterns for community repo fetch testing (T2)
- [Per-Persona Corroboration Scores](documentation/scorecard-corroboration.md) — scorecard aggregation and `atcr personas list --scores` wiring (T6)

## Implementation Strategy
Follow the pre-decided sprint order strictly: T8 (Language field + skeptic routing, verified-green by `go test ./...`) → T1 (bonus persona .md files + test fixtures + CI verification step) — both in Sprint A. Sprint B: T2 (atcr personas CLI scaffold + 6 subcommands) → T5 (bundle YAML + resolver + initial bundles) → T6 (corroboration --scores wiring) → T7-in-repo (docs). The corroboration score carrier (`map[string]float64` 4th parameter to `SelectEligibleSkeptics`, built by T6 from `scorecard.Aggregate()`) is already decided; T8 and T6 share the same map shape to avoid downstream caller churn. Each task runs a full RED→GREEN TDD cycle. Cumulative adversarial review after each sprint before PR.

## Recommended Packages
No high-ROI external packages to add — existing dependencies cover all implementation needs:
- `github.com/spf13/cobra` — already present, used for all CLI work
- `gopkg.in/yaml.v3` — already present, used for bundle manifest parsing
- `net/http` (stdlib) — sufficient for community repo fetch

## User Story Themes

### Theme 1 — Bonus Built-In Domain Personas (T1)
A security engineer, Go developer, or performance engineer gets domain-specific review coverage immediately, with no install step. `sentinel`, `tracer`, and `idiomatic` ship with the binary, each fixture-tested in CI.

### Theme 2 — Personas CLI: Discovery and Lifecycle (T2)
A developer installs a community persona (`atcr personas install security/owasp`), lists installed personas, searches by keyword, tests against its fixture, and upgrades when a new version is available.

### Theme 3 — Language-Aware Skeptic Routing (T8)
A Go developer's findings are verified by skeptics that understand Go idioms when a language-scoped persona is installed — skeptic selection routes to the best-matched persona automatically, with silent fallback to the general pool when no match exists.

### Theme 4 — Domain Bundles (T5)
A Django team lead installs `bundle/django` and gets `django-orm`, `python-types`, `security/owasp`, and `security/secrets` all in one command — no need to find and install each separately.

### Theme 5 — Corroboration Feedback (T6)
A platform lead runs `atcr personas list --scores` and sees which personas are finding the most corroborated issues on the team's actual codebase, making persona ROI visible and data-driven.

### Theme 6 — In-Repo Documentation (T7-in-repo)
A contributor follows the installation guide (`docs/personas-install.md`) to install their first community persona, and the authoring template (`docs/personas-authoring.md`) to contribute a new one.

## Planning Success Criteria
- `sentinel`, `tracer`, `idiomatic` ship with binary; each CI fixture passes (`atcr personas test <name>` returns pass)
- `atcr personas install/remove/list/search/test/upgrade` subcommands exist and function
- `atcr personas install bundle/<name>` installs all bundle members
- `atcr personas list --scores` shows corroboration rate from existing scorecard data
- `SelectEligibleSkeptics` prefers language-scoped skeptics; tests cover match, no-match fallback, multiple-match tie-break
- `AgentConfig.Language` is backward-compatible: existing `registry.yaml` files load unchanged
- All existing tests pass; `TestNames_ReturnsAllSix` updated to 9; `TestRootCmd_HasExactlyFourteenSubcommands` updated to 15

## Risk Mitigation
| Risk | Mitigation |
|------|------------|
| Corroboration score parameter shape — wrong choice causes T6/T8 caller churn | Decided 2026-06-24: caller-supplied `map[string]float64` 4th parameter to `SelectEligibleSkeptics`; nil-safe; shared shape with T6's `scorecard.Aggregate()` output; only one production caller to update |
| Subcommand count test CI failure window | Update `TestRootCmd` count and register `newPersonasCmd()` in the same commit |
| TestNames_ReturnsAllSix breaks before persona .md files exist | Write test update (RED) first, then create .md files (GREEN) |
| `go:embed` picks up unexpected .md files | Enumerate `names` slice explicitly; embed loads all *.md but only `Get(name)` for declared names is called in production |

## Next Steps
1. `/find-documentation @.planning/plans/active/9.0_persona_ecosystem/`
2. `/create-documentation @.planning/plans/active/9.0_persona_ecosystem/`
3. `/design-sprint @.planning/plans/active/9.0_persona_ecosystem/`
4. `/create-sprint @.planning/plans/active/9.0_persona_ecosystem/`
