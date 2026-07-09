# User Story 5: `atcr models check` Drift Report

**Plan:** [19.7: Live Model Resolution, Lockfile & Drift Detection](../plan.md)

## User Story

**As an** atcr end user (or the mechanical monitoring agent introduced in Epic 19.8)
**I want** a deterministic `atcr models check [--json]` command that reports whether any installed persona's locked model slug has drifted from the live catalog — newer family member available, deprecation via `expiration_date`, or the slug going missing entirely
**So that** I can see at a glance (or programmatically) which personas need attention, with human-readable output for interactive use, `--json` for machine consumption, and exit codes I can script against, without ever risking a review silently running on a stale or dead model

## Story Context

- **Background:** Story 2 establishes the family/channel binding and resolved lock persisted per persona; Story 4 (out of scope references aside) and the resolver work populate that lock via `personas upgrade`. This story adds the read-only observability surface on top: a new `atcr models` command family whose first subcommand, `check`, enumerates installed personas, reads each one's currently resolved lock, and compares it against the OpenRouter catalog (by default the checked-in deterministic snapshot at `internal/personas/testdata/catalog_snapshot.json`, per `documentation/models-check-command.md`) to report drift, deprecation, or missing-slug conditions. `atcr models check` is explicitly named in the Proposed Solution and AC5 as both a user-facing diagnostic and "the seam Epic 19.8's mechanical agent wraps" — this story ships only the deterministic CLI primitive; it does not build any LLM agent, prompt-drafting logic, or monitoring loop, which remain entirely out of scope and are reserved for Epic 19.8.
- **Assumptions:** The lock format and per-persona storage from Story 2 already exist and are readable by the time this story is implemented. Installed-persona enumeration reuses the existing tier-walking precedent in `internal/personas/list.go:53` (`ListTiers`), which already walks project > community > built-in tiers and deduplicates by name. The command never runs during a review and never mutates any lock — it is diagnostic-only, read-only.
- **Constraints:** `models` is registered as a new top-level Cobra command family in `cmd/atcr/main.go:202` alongside `personas`, `doctor`, `debt`, etc., with `check` as its first subcommand, following the exact conventions demonstrated in `cmd/atcr/personas.go` (`cmd.OutOrStdout()`/`cmd.ErrOrStderr()`, flag parsing, table rendering, optional machine-readable output). Determinism is non-negotiable: by default the command must compare against the checked-in catalog snapshot rather than a live network call, so the same lock state always produces the same report and the same exit code on every run, in CI and locally alike.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 2 (Family/Channel Binding & Resolved Lock) |

## Success Criteria (SMART Format)

- **Specific:** `atcr models check` enumerates every installed persona's resolved lock, compares each against the catalog snapshot, and reports each of the three drift conditions (newer family member available, deprecation via non-null `expiration_date`, missing slug) in both a human-readable table/text format and a `--json` machine-readable format, exiting with code 0 (no conditions found), 1 (one or more conditions found), or 2 (usage error or command failure).
- **Measurable:** `go test ./...` covers all three drift conditions individually and in combination, both output modes (default and `--json`), and all three exit codes (0/1/2), with the catalog snapshot fixture driving deterministic, reproducible assertions across repeated runs.
- **Achievable:** Reuses `ListTiers`' existing enumeration/dedup logic and `cmd/atcr/personas.go`'s established Cobra command conventions; the comparison logic is a straightforward per-persona lock-vs-snapshot diff with no new resolution or network code required.
- **Relevant:** Directly satisfies the Proposed Solution's item #6 and AC5 verbatim, delivering the deterministic drift-report primitive that is both a standalone user command and the exact seam Epic 19.8's mechanical agent will wrap — without building any part of that agent here.
- **Time-bound:** Deliverable within this sprint once Story 2's lock/binding fields exist to read from; sequenced after the data-model foundation and independent of the resolver/upgrade stories' internals beyond needing a populated lock to compare.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [05-01](../acceptance-criteria/05-01-command-registration-human-readable-drift-report.md) | Command Registration and Human-Readable Drift Report | Integration |
| [05-02](../acceptance-criteria/05-02-json-machine-readable-output.md) | `--json` Machine-Readable Output Shape | Integration |
| [05-03](../acceptance-criteria/05-03-exit-code-contract.md) | Exit-Code Contract (0 / 1 / 2) | Integration |
| [05-04](../acceptance-criteria/05-04-deterministic-catalog-snapshot-default.md) | Determinism via Checked-In Catalog Snapshot | Unit |

## Original Criteria Overview

1. `atcr models` is registered as a new top-level command family (`cmd/atcr/main.go:202`) with `check` as its first subcommand, following `cmd/atcr/personas.go` conventions for output streams and flag handling.
2. `atcr models check` enumerates all installed personas (reusing `internal/personas/list.go:53`'s tier-walking/dedup precedent), reads each persona's resolved lock, and compares it against the default catalog snapshot (`internal/personas/testdata/catalog_snapshot.json`) to report newer-family-member drift, `expiration_date` deprecation, and missing-slug conditions.
3. The command supports both a human-readable default output and a `--json` machine-readable output mode carrying the same drift data, and exits 0 when no conditions are found, 1 when one or more conditions are found, and 2 on usage error or command failure — this exit-code/output contract is exactly what Epic 19.8's mechanical agent is expected to consume, though building that agent is explicitly out of scope for this story.

## Technical Considerations

- **Implementation Notes:** Register the `models` command family in `cmd/atcr/main.go:202` and implement `check` as a sibling pattern to `cmd/atcr/personas.go`, using `cmd.OutOrStdout()`/`cmd.ErrOrStderr()` for output and a `--json` flag gating machine-readable output versus a rendered table for interactive use. Enumerate installed personas via the same walk/dedup approach as `ListTiers` (`internal/personas/list.go:53`), pull each persona's resolved lock (from Story 2), and diff it against entries in the catalog snapshot to classify each persona into zero or more of the three conditions. Default to the checked-in `internal/personas/testdata/catalog_snapshot.json` for determinism; do not call OpenRouter live in the default path.
- **Integration Points:**
  - `cmd/atcr/main.go:202` — register the new `models` top-level command family.
  - `cmd/atcr/personas.go` — sibling command conventions to mirror (output streams, flags, table/JSON rendering).
  - `internal/personas/list.go:53` (`ListTiers`) — reusable precedent for enumerating installed personas across project/community/built-in tiers with dedup by name.
  - Story 2's binding/lock fields on the persona unit — the data this command reads to know each persona's currently resolved slug.
  - `internal/personas/testdata/catalog_snapshot.json` — the default deterministic comparison source for the catalog.
- **Data Requirements:** Read-only access to each installed persona's resolved lock and the catalog snapshot's slug/family/`expiration_date` metadata; no new persisted state is introduced by this command, and no lock is ever modified by `models check`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Comparing against a live OpenRouter catalog by default makes the report non-reproducible across runs, breaking CI and confusing users chasing a moving target. | High | Default to the checked-in `catalog_snapshot.json` for all comparisons; treat any live-catalog mode as an explicit, separate opt-in outside this story's default behavior. |
| `--json` and human-readable outputs drift out of sync over time (e.g., a new condition added to one format but not the other), breaking Epic 19.8's mechanical agent which depends on the JSON contract. | Medium | Derive both output formats from a single internal drift-report data structure so adding or changing a condition updates both renderers simultaneously; cover both formats in the same test cases. |
| Exit-code contract (0/1/2) is loosely implemented (e.g., usage errors and drift-found conditions both return 1), making the command unreliable as a scripting/agent primitive. | High | Explicitly test all three exit codes in isolation and in combination, distinguishing "conditions found" (1) from "command/usage failure" (2) at the Cobra command boundary. |
| Persona enumeration diverges from `ListTiers`' existing tier precedence/dedup behavior, causing `models check` to report on a different persona set than `personas list` does. | Medium | Reuse `internal/personas/list.go:53`'s enumeration logic directly rather than reimplementing tier-walking/dedup independently. |

---

**Created:** July 08, 2026 06:01:13PM
**Status:** Draft - Awaiting Acceptance Criteria
