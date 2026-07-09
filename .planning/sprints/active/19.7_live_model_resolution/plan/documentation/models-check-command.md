# `atcr models check` Command Design

**Priority:** [IMPORTANT]

## Overview

`atcr models check` is the deterministic drift-report primitive at the center of AC5. It reads the resolved lock currently stored for each installed community persona, compares it against the OpenRouter catalog, and reports one of three conditions:

1. **Newer family member available** — a model newer than the locked slug exists for the persona's family/channel.
2. **Deprecation** — the locked slug carries a non-null `expiration_date`.
3. **Missing slug** — the locked slug no longer appears in the catalog.

The command is intentionally read-only and offline-friendly: in normal operation it works against the checked-in catalog snapshot fixture so it is deterministic and safe for CI and for Epic 19.8's downstream mechanical monitoring agent. A `--json` flag emits machine-readable output; without it, the command prints human-readable lines and uses meaningful exit codes.

## Key Concepts

- **Command family registration.** `atcr models` is a new top-level Cobra command family registered in `cmd/atcr/main.go:202` alongside `personas`, `doctor`, `debt`, etc. `check` is the first subcommand under it. Future subcommands (e.g. a refresh command for the catalog snapshot fixture) can live under the same family.
  > Source: [codebase-discovery.json > integration_points > cmd/atcr/main.go:202]

- **Sibling to the personas subcommand family.** `cmd/atcr/personas.go` already demonstrates the project's conventions: `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` for output, flag parsing via `cmd.Flags()`, table rendering for humans, and optional machine-readable output. `models check` follows the same patterns.
  > Source: [codebase-discovery.json > integration_points > cmd/atcr/personas.go]

- **Installed-persona enumeration.** `internal/personas/list.go:53` (`ListTiers`) walks project > community > built-in tiers and deduplicates by name. `models check` needs a similar enumeration, but over installed community personas and their locked model slugs rather than over persona files. The deduplication-by-name pattern is the reusable precedent.
  > Source: [codebase-discovery.json > semantic_matches > internal/personas/list.go:53]

- **No review-path invocation.** The command is a user-facing (and agent-facing) diagnostic only. It never runs during a review; reviews continue to consume the static lock so reproducibility is preserved.

- **Exit-code contract.** Meaningful exit codes let scripts and Epic 19.8's agent act on the result:
  - `0` — no drift, deprecation, or missing-slug conditions found.
  - `1` — one or more drift/deprecation/missing-slug conditions found.
  - `2` — usage error or command failure (e.g. cannot read installed personas).

- **Human-readable output.** One line per condition, e.g.:

  ```
  anthony: anthropic/claude-opus-4.8 → anthropic/claude-opus-5.0 (newer member)
  gene: google/gemini-pro-1.5 has expiration 2026-09-01 (deprecation)
  milo: openai/gpt-4o-missing no longer in catalog (missing)
  ```

- **JSON output shape (`--json`).** A stable array of condition objects. Example shape:

  ```json
  [
    {
      "persona": "anthony",
      "condition": "newer-member",
      "current_slug": "anthropic/claude-opus-4.8",
      "suggested_slug": "anthropic/claude-opus-5.0",
      "family": "anthropic/claude-opus",
      "channel": "stable"
    },
    {
      "persona": "gene",
      "condition": "deprecation",
      "current_slug": "google/gemini-pro-1.5",
      "expiration_date": "2026-09-01"
    },
    {
      "persona": "milo",
      "condition": "missing",
      "current_slug": "openai/gpt-4o-missing"
    }
  ]
  ```

  The exact field names are planning-level guidance; the sprint design will finalize them. The key requirement is that the output is machine-readable and stable enough for Epic 19.8's agent to consume without parsing free text.

- **Determinism comes from the catalog snapshot.** By default, `models check` uses the checked-in `internal/personas/testdata/catalog_snapshot.json` rather than calling OpenRouter live. This guarantees that running the command twice against the same lock produces the same report, which is essential both for reproducible CI gates and for agentic consumption.
  > Source: [catalog-snapshot-fixture.md]

## Code Examples

No literal code exists yet for `cmd/atcr/models.go`. The registration shape follows the existing personas command:

```go
// ILLUSTRATIVE ONLY — not existing code.
func newModelsCmd() *cobra.Command {
    cmd := &cobra.Command{Use: "models", Short: "Inspect model bindings and drift"}
    cmd.AddCommand(newModelsCheckCmd())
    return cmd
}

func newModelsCheckCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "check",
        Short: "Report model drift/deprecation/missing-slug for installed personas",
        RunE: func(cmd *cobra.Command, args []string) error {
            jsonOut, _ := cmd.Flags().GetBool("json")
            // ... enumerate, resolve, report
            return nil
        },
    }
    cmd.Flags().Bool("json", false, "emit machine-readable JSON")
    return cmd
}
```

## Quick Reference

| Item | Value |
|---|---|
| Command | `atcr models check [name]` (all installed personas if no name) |
| Flag | `--json` — machine-readable output |
| Conditions reported | `newer-member`, `deprecation`, `missing` |
| Exit codes | `0` = clean, `1` = drift found, `2` = error |
| Catalog source | Checked-in snapshot by default; deterministic |
| Consumers | Human users, CI gates, Epic 19.8 mechanical agent |
| Registration point | `cmd/atcr/main.go:202` (add `newModelsCmd()`) |

## Related Documentation

- [Catalog Snapshot Fixture Discipline](catalog-snapshot-fixture.md)
- [Existing Codebase Patterns to Reuse](existing-resolver-patterns.md)
- [OpenRouter Catalog & Completions API](openrouter-catalog-api.md)
- codebase-discovery.json (gap-004, integration_points)
- original-requirements.md (AC5)
