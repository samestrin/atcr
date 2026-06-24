# User Story 4: Domain Bundles

**Plan:** [9.0: Persona Ecosystem](../plan.md)

## User Story

**As a** Django team lead
**I want** to install a named domain bundle (`atcr personas install bundle/django`) and receive all relevant domain personas in a single command
**So that** my team gets a curated, cohesive review panel without having to discover, evaluate, and install each persona individually

## Story Context

- **Background:** Framework-specific teams need multiple complementary personas to get meaningful review coverage — a Django project benefits from ORM query analysis, Python type checking, OWASP security scanning, and secrets detection simultaneously. Installing each persona individually requires knowing they exist, finding the right name, and running multiple commands. Bundle manifests solve the discovery and installation friction by grouping curated persona sets under a single named handle. Two initial bundles ship: `bundle/django` (4 personas) and `bundle/go-production` (Go-specific set). Manifests are YAML files parsed in strict mode (`KnownFields(true)`) to catch typos at load time rather than producing silent partial installs.
- **Assumptions:** The `atcr personas install` command from Story 2 (Theme 2 — Personas CLI) is implemented and functional before this story's work begins. The `internal/personas` package exists. Individual personas referenced in a bundle manifest are fetchable from the configured registry URL. The `gopkg.in/yaml.v3` dependency is already present in the module.
- **Constraints:** Bundle manifests must use `gopkg.in/yaml.v3` with `KnownFields(true)` strict mode — no unknown keys are permitted. The `bundle/` prefix in the install argument is the routing discriminator; the resolver lives in `internal/personas/bundles.go`. No external dependency additions are allowed. The manifest format must remain hand-editable and minimal. Backward compatibility with existing `registry.yaml` files must not be broken.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 2 (atcr personas CLI — install subcommand), Story 3 (Language-Aware Skeptic Routing — AgentConfig.Language field) |

## Success Criteria (SMART Format)

- **Specific:** `atcr personas install bundle/django` installs exactly 4 personas (`framework/django-orm`, `language/python-types`, `security/owasp`, `security/secrets`) in a single command; `atcr personas install bundle/go-production` installs the go-production set; an unknown bundle name returns a clear error within one command invocation.
- **Measurable:** `internal/personas/bundles_test.go` passes with 100% coverage of round-trip YAML parsing, `bundle/` prefix routing, unknown-key rejection (`KnownFields(true)`), and unknown-bundle-name error path; `go test ./internal/personas/...` exits 0.
- **Achievable:** Implementation scope is contained to `internal/personas/bundles.go`, `internal/personas/bundles_test.go`, and two manifest files (`bundles/django.yaml`, `bundles/go-production.yaml`); the install dispatch in the existing `install` subcommand requires only a `bundle/` prefix check before delegating to the bundle resolver.
- **Relevant:** Reduces multi-step persona setup for framework teams to a single command, directly lowering the adoption barrier for vertical markets and making ATCR immediately useful for domain-specific workloads without manual curation.
- **Time-bound:** Delivered within Sprint B (after Sprint A's T8 and T1 land and are verified green); implementation completes before T6 (corroboration scores) begins.

## Acceptance Criteria Overview

1. `atcr personas install bundle/django` installs all four bundle members (`framework/django-orm`, `language/python-types`, `security/owasp`, `security/secrets`) and reports each installed persona by name.
2. Bundle manifest YAML is decoded in strict mode (`KnownFields(true)`); a manifest with an unrecognized key (e.g., `personnas:`) fails with a descriptive parse error rather than silently ignoring the field.
3. Requesting an unknown bundle name (e.g., `atcr personas install bundle/nonexistent`) returns a non-zero exit code and a clear human-readable error message identifying the bundle as not found.
4. `bundles/django.yaml` and `bundles/go-production.yaml` exist in the repository with valid manifests; both parse without error under `KnownFields(true)`.
5. All existing tests continue to pass; `internal/personas/bundles_test.go` covers round-trip parse, `bundle/` prefix routing, strict-mode rejection, and unknown-bundle error.

_Detailed AC: `/create-acceptance-criteria @/Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/9.0_persona_ecosystem/`_

## Technical Considerations

- **Implementation Notes:** The `bundle/` prefix in the install argument is the routing discriminator. The install command's dispatch checks for this prefix first and calls `bundles.Resolve(name)` in `internal/personas/bundles.go`. `Resolve` reads the manifest from the embedded or fetched `bundles/<name>.yaml`, decodes it with `yaml.NewDecoder` + `KnownFields(true)`, and returns the list of persona references. The install command then calls the single-persona install path for each member in order. The `BundleManifest` struct has four fields: `Name string`, `Description string`, `Personas []string`, all tagged with `yaml:"...,omitempty"` where optional. Strict-mode rejection of unknown keys (`personnas:`, `serial_agnets:`) surfaces typos at load time.
- **Integration Points:** `internal/personas/install.go` (existing install dispatch from Story 2) — add `strings.HasPrefix(name, "bundle/")` branch before the single-persona fetch path. `internal/personas/bundles.go` — new file, owns `BundleManifest` struct and `Resolve(name string) ([]string, error)`. `bundles/django.yaml` and `bundles/go-production.yaml` — new manifest files embedded or fetched from the registry. `internal/registry/config.go` — no changes needed for T5 itself; `AgentConfig.Language` is a T8 dependency already resolved by Sprint A.
- **Data Requirements:** `BundleManifest` struct: `Name string \`yaml:"name"\``, `Description string \`yaml:"description,omitempty"\``, `Personas []string \`yaml:"personas"\``. `bundles/django.yaml` manifest declares 4 personas; `bundles/go-production.yaml` declares the go-production set. All persona references in manifests use slash-namespaced identifiers matching the community registry format.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Partial bundle install on mid-list fetch failure leaves the user with an incomplete panel | Medium | Collect all errors before writing any state; report which personas failed with actionable messages; do not treat a partial install as success |
| Strict-mode YAML rejection (`KnownFields(true)`) breaks existing bundle manifests if the format evolves | Low | Treat `BundleManifest` as a stable, minimal schema; all optional fields use `omitempty`; schema changes require a version field or migration |
| `bundle/` prefix routing collision with a legitimate persona namespace named `bundle` | Low | Reserve `bundle/` as a routing prefix in the CLI dispatch; document that community persona namespaces must not begin with `bundle/` |
| Story 2 install subcommand not complete before T5 begins | Medium | T5 has an explicit dependency on Story 2; sprint ordering enforces this; T5 does not start until Story 2's install path is verified green |

---

**Created:** June 24, 2026
**Status:** Draft - Awaiting Acceptance Criteria
