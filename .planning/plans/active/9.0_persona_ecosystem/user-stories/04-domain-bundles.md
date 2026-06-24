# User Story 4: Domain Bundle Installation

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** Django team lead configuring ATCR for my team's review workflow
**I want** to install all framework-relevant personas in a single command (`atcr personas install bundle/django`)
**So that** my team gets comprehensive ORM, typing, and security coverage immediately without researching and installing each persona individually

## Story Context

- **Background:** Teams adopting ATCR for a specific framework need multiple complementary personas to get meaningful coverage. A Django team needs `django-orm` (query pattern checks), `python-types` (type annotation enforcement), `security/owasp` (web security), and `security/secrets` (credential leakage). Installing these one-by-one requires knowing which personas exist, which are relevant, and running four separate commands — creating friction that blocks adoption. Domain bundles package these complementary personas under a single stack-focused name, letting teams reason at the level they already think in: "I'm running Django" rather than "I need personas X, Y, Z, and W."
- **Assumptions:** The `atcr personas` CLI (T2) is in place before bundle resolution is layered on top. YAML v3 and `embed.FS` are already established patterns in the codebase (used for the bonus personas in T1). Bundle manifests are versioned YAML files checked into the repository under `internal/personas/bundles/`. The resolver handles partial installs (some personas in a bundle already installed) gracefully by skipping already-present entries.
- **Constraints:** Only two initial bundles ship: `bundle/django` and `bundle/go-production`. The bundle resolver lives exclusively in `internal/personas/bundles.go` — no bundle logic bleeds into the CLI layer or `AgentConfig`. Bundle manifest format is intentionally minimal (name, description, personas list) to keep authoring friction low. The `install bundle/` prefix is the only bundle-aware path; `install <persona>` for individual personas is unchanged.
- **Documentation Reference:** See [YAML Bundle Manifests](../documentation/yaml-bundle-manifests.md) for manifest parsing rules and `AgentConfig.Language` integration.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 2 (atcr personas CLI — T2), User Story 1 (bonus personas — T1 provides some bundle members) |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas install bundle/django` installs exactly `django-orm`, `python-types`, `security/owasp`, and `security/secrets` into `~/.config/atcr/personas/` and exits 0; `atcr personas install bundle/go-production` installs its declared persona set and exits 0.
- **Measurable:** A unit test in `internal/personas/bundles_test.go` verifies the bundle resolver expands each named bundle to its exact persona list; an integration test confirms all four personas are present on disk after `install bundle/django` on a clean config directory.
- **Achievable:** Implementation requires one new Go file (`internal/personas/bundles.go`), two YAML manifest files, and wiring the `install` subcommand to detect the `bundle/` prefix — all within existing patterns already established by T2.
- **Relevant:** Reduces first-time setup from four commands (and knowledge of which four to run) to one command, directly lowering the adoption barrier for framework-focused teams and making ATCR immediately deployable as a stack-level tool.
- **Time-bound:** Delivered within Sprint B alongside T2 completion; both bundles are functional before Sprint B's cumulative adversarial review.

## Acceptance Criteria Overview

This story is complete when the following acceptance criteria are met:

- **04-01**: `atcr personas install bundle/django` installs all declared personas on a clean config directory.
- **04-02**: Partial bundle installs skip already-present personas and install the remainder.
- **04-03**: Installing an unknown bundle exits non-zero with a clear error and installs nothing.
- **04-04**: Bundle manifest YAML is validated at parse time with descriptive errors for missing fields.
- **04-05**: `internal/personas/bundles_test.go` covers expansion, errors, partial installs, and parse validation.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-clean-bundle-install.md) | Clean Bundle Installation | Integration |
| [04-02](../acceptance-criteria/04-02-partial-install-skip.md) | Partial Bundle Install Skip Behavior | Integration |
| [04-03](../acceptance-criteria/04-03-unknown-bundle-error.md) | Unknown Bundle Error Handling | Unit |
| [04-04](../acceptance-criteria/04-04-manifest-parse-validation.md) | Bundle Manifest Parse Validation | Unit |
| [04-05](../acceptance-criteria/04-05-bundle-test-coverage.md) | Bundle Test Coverage in bundles_test.go | Unit |

## Original Criteria Overview

1. Running `atcr personas install bundle/django` on a clean config directory installs all four declared personas (`django-orm`, `python-types`, `security/owasp`, `security/secrets`) with a success message listing each installed persona by name.
2. Running `atcr personas install bundle/django` when some bundle members are already installed skips already-present entries, installs the remainder, and reports the per-persona outcome (installed vs. already present) without error.
3. Running `atcr personas install bundle/unknown` exits non-zero with a clear error message (`unknown bundle: "unknown"`) and installs nothing.
4. The bundle manifest YAML format (`name`, `description`, `personas` list) is validated at parse time; a manifest missing required fields causes `bundles.go` to return a descriptive parse error rather than a nil-pointer panic.
5. `internal/personas/bundles_test.go` covers: successful expansion of both bundles, unknown bundle error, partial-install skip behavior, and manifest parse validation — all passing under `go test ./internal/personas/...`.

## Technical Considerations

- **Implementation Notes:** The `install` subcommand in `cmd/atcr/personas_install.go` detects the `bundle/` prefix with a simple `strings.HasPrefix(arg, "bundle/")` check, then delegates to `bundles.Resolve(name)` which returns `[]string` of individual persona names. The existing single-persona install loop then runs unchanged for each returned name. Bundle manifests are embedded via `go:embed bundles/*.yaml` in `internal/personas/bundles.go` and parsed with `yaml.v3` at call time (no global init). The resolver returns `([]string, error)` — unknown bundle yields a typed `ErrUnknownBundle` so callers can produce user-facing messages without string matching.
- **Integration Points:** Depends on `internal/personas` package (T2) for the single-persona install path that bundle resolution delegates to. The `atcr personas list` command should indicate when an installed persona came from a bundle (optional enhancement, not required for this story). No changes to `AgentConfig`, `select.go`, or any verify-pipeline code — bundle resolution is purely an install-time concern.
- **Data Requirements:** Two YAML bundle manifest files at `internal/personas/bundles/django.yaml` and `internal/personas/bundles/go-production.yaml`. Each manifest contains: `name` (string, matches directory key), `description` (string), `personas` (list of strings matching community-repo persona identifiers). The `go:embed` directive in `bundles.go` picks up both files automatically; adding a third bundle later requires only a new YAML file with no Go code changes.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Bundle persona names drift from actual published persona identifiers in the community repo | Medium | Pin persona identifiers in manifests to the exact names used by `internal/personas` fetch logic; add a CI check that validates all bundle members resolve against the known persona registry snapshot |
| Partial bundle install leaves config in an inconsistent state if a network error occurs mid-install | Medium | Install personas sequentially and report per-persona outcomes; a subsequent `install bundle/django` run skips already-installed entries and retries the failed ones — idempotency is the recovery path |
| `bundle/` prefix detection is too broad and collides with a future community persona scoped under a `bundle` namespace | Low | Reserve the `bundle/` prefix in the persona namespace spec (documented in `docs/personas-authoring.md`); community personas must not use `bundle` as a scope name |
| Manifest format ambiguity leads to inconsistent YAML across the two initial bundles | Low | Define the canonical struct in `bundles.go` first, generate both YAML files from that struct in tests, and validate both files parse cleanly in CI |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
