# Plan 9.0: Persona Ecosystem

## Overview
Expand ATCR's reviewer panel beyond the 6 generalist built-in personas by shipping 3 domain-specific bonus personas with the binary, adding an `atcr personas` CLI for community repo integration, wiring per-persona corroboration scores into `atcr personas list --scores`, and extending skeptic selection with language-aware routing. Planned as two sprints: **Sprint A** (T8 schema + T1 bonus personas) and **Sprint B** (T2 CLI + T5 bundles + T6 scores + T7-in-repo docs).

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** — `/create-user-stories @.planning/plans/active/9.0_persona_ecosystem/`
- [x] **Acceptance Criteria** — `/create-acceptance-criteria @.planning/plans/active/9.0_persona_ecosystem/`
- [x] **Design Sprint** — `/design-sprint @.planning/plans/active/9.0_persona_ecosystem/`
- [ ] **Sprint Plan** — `/create-sprint @.planning/plans/active/9.0_persona_ecosystem/`

## Sprint Structure (Pre-Decided)

### Sprint A — Registry + Verify Internals
| Task | Description | Primary Files |
|------|-------------|---------------|
| T8 | `Language []string` on `AgentConfig`; `SelectEligibleSkeptics` two-partition reorder | `internal/registry/config.go`, `internal/verify/select.go` |
| T1 | `sentinel`, `tracer`, `idiomatic` personas + fixtures + CI verification | `personas/*.md`, `personas/personas.go`, `personas/testdata/` |

### Sprint B — Surface Layer
| Task | Description | Primary Files |
|------|-------------|---------------|
| T2 | `atcr personas` CLI (install/remove/list/search/test/upgrade) | `cmd/atcr/personas.go`, `internal/personas/` |
| T5 | Domain bundles (YAML manifest + resolver + django + go-production) | `internal/personas/bundles.go` |
| T6 | Per-persona corroboration wiring (`--scores` flag) | `internal/personas/list.go`, scorecard integration |
| T7-in-repo | Installation guide + persona authoring template | `docs/personas-install.md`, `docs/personas-authoring.md` |

## Timeline & Milestones
| Milestone | Description | Estimate |
|-----------|-------------|----------|
| Sprint A complete | T8 + T1 merged, CI green | ~1.5 weeks |
| Sprint B complete | T2 + T5 + T6 + T7-in-repo merged, CI green | ~2 weeks |

## Resource Requirements
- Go development environment with `go test ./...` passing
- Access to `internal/verify/`, `internal/registry/`, `personas/`, `cmd/atcr/`
- Scorecard JSONL readable from `~/.config/atcr/scorecard/` (or configured path) for T6

## Expected Outcomes
- 9 total built-in personas (6 generalists + 3 domain specialists), all CI-tested
- `atcr personas` CLI with 6 subcommands, configurable community repo URL, httptest.NewServer-based tests
- Language-aware skeptic routing in `SelectEligibleSkeptics` with match/fallback/tie-break tests
- Domain bundles (django, go-production) installable via `atcr personas install bundle/`
- `atcr personas list --scores` surfaces corroboration rates from scorecard data
- Installation guide and authoring template in `docs/`

## Risk Summary
- **Corroboration score parameter shape** — decided 2026-06-24: caller-supplied `map[string]float64` 4th parameter to `SelectEligibleSkeptics`; shared with T6's `scorecard.Aggregate()` output
- **Subcommand count test** — update TestRootCmd count and add personas command in same commit
- **Persona name test** — update TestNames count (6→9) before adding .md files (TDD RED first)

## Documentation References

### [CRITICAL] Must Read Before Coding
- [Bonus Built-In Personas](documentation/bonus-personas.md) — `sentinel`, `tracer`, `idiomatic` personas, embedded registration, and fixture expectations (T1)
- [Cobra CLI Patterns](documentation/cobra-cli-patterns.md) — Cobra subcommand architecture for `atcr personas` CLI (T2)
- [YAML Bundle Manifests](documentation/yaml-bundle-manifests.md) — Bundle manifest parsing and `AgentConfig.Language` field (T5/T8)
- [Skeptic Routing & Verification](documentation/skeptic-routing-verification.md) — `SelectEligibleSkeptics` language-aware routing (T8)

### [IMPORTANT] Review During Development
- [HTTP & Standard Library Testing](documentation/http-stdlib-testing.md) — `httptest.NewServer` patterns for fetch testing (T2)
- [Per-Persona Corroboration Scores](documentation/scorecard-corroboration.md) — scorecard aggregation and `atcr personas list --scores` wiring (T6)

## Plan Assets
- [Sprint Design](sprint-design.md)
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Documentation](documentation/README.md)
- [User Stories](user-stories/) _(pending)_
- [Acceptance Criteria](acceptance-criteria/) _(pending)_
