# User Story 1: Community-Canonical Fetch-and-Pin Distribution

**Plan:** [19.6: Community-Canonical Model-Indexed Personas](../plan.md)

## User Story

**As a** first-time atcr user running `atcr init` or `atcr quickstart`
**I want** my reviewer personas fetched and pinned from the canonical `samestrin/atcr` community repo instead of copied out of the compiled binary
**So that** I always get the current, community-maintained persona set (with a reproducible version pin), while still being able to work fully offline or keep my existing hand-edited personas untouched

## Story Context

- **Background:** `internal/personas/client.go:24` currently hardcodes `RegistryBaseURL` to `https://raw.githubusercontent.com/atcr/personas/main`, resolved through `BaseURL()` (env-override via `ATCR_PERSONAS_URL`, else the constant). Separately, `cmd/atcr/init.go` and `cmd/atcr/quickstart.go` do not use this fetch path at all today — they copy embedded built-in `.md` personas from `builtins.Names()`/`builtins.Get()` directly into `.atcr/personas/`, making the compiled binary the de facto canonical source. This story repoints the registry default to `samestrin/atcr` and switches `init`/`quickstart` onto the fetch-and-pin path so the community repo becomes canonical in practice, not just in theory.
- **Assumptions:** The `samestrin/atcr` repo will host a `personas/community/` directory (index.json + per-persona YAML) reachable via raw content URLs in the same shape the existing `FetchIndex`/`FetchPersonaYAML` functions already expect. The existing `HTTPClient` interface, `fetch()` helper, and `PersonaIndexEntry` unknown-field-tolerant decoding require no structural change for this story — only the default URL, and new `init`/`quickstart` call sites, change.
- **Constraints:** `runInit`'s existing refuse-to-overwrite-without-`--force` contract (`cmd/atcr/init.go:76-78`) must be preserved. No breaking change for users who already have hand-edited `.md` personas under `.atcr/personas/`. `atcr review` and other network-dependent commands are unaffected; only persona acquisition during onboarding changes. **Dual-directory note:** today `.atcr/personas/` holds editable `.md` prompt templates copied from embedded built-ins, while `~/.config/atcr/personas/` (via `internal/personas.PersonasDir`) holds installed community persona `.yaml` agent configs. Per **Clarification C2** (original-requirements.md), the two-directory/two-format split is being **converged**, not entrenched: this story does not itself redesign resolution, but must not deepen the split. The resolution model — custom prompts resolvable via a single `ResolvePersona` precedence chain — is locked by **C1/C2** and owned by [AC 01-06](../acceptance-criteria/01-06-custom-prompt-resolution-precedence.md).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `RegistryBaseURL` in `internal/personas/client.go` points at the `samestrin/atcr` repo's in-repo persona path by default; `atcr init` and `atcr quickstart` add a fetch-and-pin step that installs community personas from `commpersonas.BaseURL()` into `~/.config/atcr/personas/` (the existing community-agent-config directory), recording each fetched YAML's `version` field as the pinned version; the existing embedded-built-in `.md` template copy into `.atcr/personas/` is preserved so the project workspace keeps its prompt-resolution chain; an `--offline` flag on both commands skips the community fetch and falls back to today's embedded-built-in behavior; existing `.atcr/personas/` `.md` files are never overwritten by the new fetch path.
- **Measurable:** A default (non-overridden, non-`--offline`) `atcr init`/`atcr quickstart` run against a mock registry installs community personas under `~/.config/atcr/personas/` sourced from the repointed URL with a recorded version pin; `atcr init --offline` and `atcr quickstart --offline` install only the embedded built-in `.md` templates with zero network calls; rerunning `init`/`quickstart` against a workspace with pre-existing hand-edited `.md` personas leaves those `.atcr/personas/` files byte-for-byte unchanged.
- **Achievable:** The fetch, HTTP-injection, and unknown-field-tolerant decode machinery already exist in `internal/personas/client.go`; this story wires two new call sites (`init`, `quickstart`) onto that existing machinery plus a one-constant URL change — no new subsystem is required.
- **Relevant:** This is the foundational distribution mechanism the plan's model-indexed persona library (Theme 2) and discovery flow (AC6) build on — until the fetch source is canonical and pinned, model-aware search has no reliable persona set to search over.
- **Time-bound:** Deliverable within this sprint's first phase, ahead of the model-indexed catalog and search work that depends on it.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-registry-base-url-repoint.md) | Registry Base URL Repointed to samestrin/atcr | Unit |
| [01-02](../acceptance-criteria/01-02-init-quickstart-fetch-and-pin.md) | init/quickstart Fetch-and-Pin Community Personas | Integration |
| [01-03](../acceptance-criteria/01-03-offline-flag-fallback.md) | `--offline` Flag Preserves Embedded-Built-In Behavior | Unit |
| [01-04](../acceptance-criteria/01-04-fetch-failure-error-handling.md) | Fetch Failure Produces a Descriptive, Non-Zero-Exit Error | Integration |
| [01-05](../acceptance-criteria/01-05-preserve-existing-personas-and-source-labeling.md) | Existing Personas Preserved; `--force` Semantics and Source Labeling Intact | Integration |
| [01-06](../acceptance-criteria/01-06-custom-prompt-resolution-precedence.md) | Custom Community Prompt Resolves via a Single Precedence Chain | Unit + Integration |

## Original Criteria Overview

1. `RegistryBaseURL` (or equivalent default) resolves to the `samestrin/atcr` repo's in-repo persona path; `ATCR_PERSONAS_URL` override behavior is unchanged.
2. `atcr init` and `atcr quickstart`, run without `--offline`, fetch community personas via the existing HTTP client/injection pattern into `~/.config/atcr/personas/` and record each installed persona's fetched `version` as its pin; `atcr personas upgrade` continues to compare against and advance that pin. The existing `.md` template copy into `.atcr/personas/` is preserved.
3. `--offline` flag on both commands skips the community fetch entirely and reproduces today's embedded-built-in install behavior with no network access.
4. A fetch failure without `--offline` produces a descriptive, non-zero-exit error rather than a silent fallback.
5. Existing `--force` semantics and existing on-disk `.md` personas are preserved: no overwrite of hand-edited files; missing community personas install alongside them; `atcr personas list` distinguishes `built-in` vs `community` sources with pinned versions shown.

## Technical Considerations

- **Implementation Notes:** Change the `RegistryBaseURL` constant in `internal/personas/client.go:24` to the `samestrin/atcr` in-repo path; `BaseURL()`'s env-override-else-constant logic needs no change. Add `--offline` flags to `cmd/atcr/init.go` and `cmd/atcr/quickstart.go`; when absent, add a call into `internal/personas.Install` or `InstallBundle` targeting `commpersonas.BaseURL()` that writes fetched YAML agent configs to `~/.config/atcr/personas/`, capturing the fetched YAML's `version` field as the pin (consistent with how `internal/personas/list.go` and `internal/personas/upgrade.go` already read/compare that field). Preserve the existing `builtins.Names()`/`builtins.Get()` copy loop that writes `.md` templates into `.atcr/personas/` so review-time persona resolution continues to find built-in prompt templates. Model-specific personas' custom Markdown prompts are first-class and MUST resolve (**Clarification C1**): the prompt travels with the persona as one self-contained installable unit (inline in the YAML or a co-located file installed atomically), reachable via the single `ResolvePersona` precedence chain — see [AC 01-06](../acceptance-criteria/01-06-custom-prompt-resolution-precedence.md). `/design-sprint` sizes the reformat of built-ins into that unit, not *whether* custom prompts resolve.
- **Integration Points:** `internal/personas/client.go` (`RegistryBaseURL`, `BaseURL`, `HTTPClient`, `fetch`), `cmd/atcr/init.go:runInit`, `cmd/atcr/quickstart.go:runQuickstart`, `cmd/atcr/personas.go` (package-level `personasClient` swap point for tests), `internal/personas/list.go` and `internal/personas/upgrade.go` (version-pin read/compare, unchanged logic reused).
- **Data Requirements:** No new schema; reuses existing `PersonaIndexEntry` (unknown-field-tolerant JSON decode) and persona YAML `version` field as the pin source. No new on-disk pin file — the installed YAML's own `version` field remains the source of truth per the existing `list`/`upgrade` pattern.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `samestrin/atcr`'s in-repo persona path is not yet public/populated when this story ships | High | Gate on `--offline` fallback being correct and well-tested first; document that `atcr init --offline` is the safe path until the registry content lands; use a mock `httptest.NewServer` registry for all CI tests so shipping is not blocked on the live repo |
| Rerun of `init`/`quickstart --force` accidentally clobbers a user's hand-edited `.md` persona | High (data loss) | Explicit test: pre-seed a `.atcr/personas/` workspace with a modified `.md` file, run `init --force`, assert the file is byte-identical afterward; only install fetched personas that don't already exist on disk |
| Fetch failure (network down, repo unreachable) during onboarding blocks first-run UX with no guidance | Medium | Descriptive error message explicitly suggesting `--offline`; covered by an AC6-adjacent test simulating a fetch failure against the mock registry |
| Changing the default `RegistryBaseURL` silently changes behavior for existing users who rely on `ATCR_PERSONAS_URL` being unset (implicitly pointing at the old `atcr/personas` source) | Medium | `ATCR_PERSONAS_URL` override path is untouched by this story; only the fallback constant changes, and this is called out explicitly in onboarding docs (Theme 3) |
| Community personas that ship custom Markdown prompts are not reachable by `ResolvePersona` | Medium | **Resolved by Clarification C1/C2 + [AC 01-06](../acceptance-criteria/01-06-custom-prompt-resolution-precedence.md):** the prompt travels with the persona as one self-contained unit and resolves via a single precedence chain. The "constrain to references" fallback is rejected — custom prompts are first-class |

---

**Created:** July 07, 2026 11:22:46AM
**Status:** Draft - Awaiting Acceptance Criteria
