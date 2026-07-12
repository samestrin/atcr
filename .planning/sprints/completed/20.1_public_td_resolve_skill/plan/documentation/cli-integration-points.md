# CLI Integration Points (reconcile persistence hook, atcr debt family, justification fields)

**Priority: Important**

## Overview

Epic 20.1 does not need to invent new CLI surface area from scratch — it needs to graft onto three things that already exist in `atcr`. First, `atcr reconcile` already has a precedent for post-processing the reconciled `Result` via an opt-out flag: scorecard emission runs after `internal/reconcile.RunReconcile` returns, guarded by a `--no-scorecard` flag, and a `--no-local-debt` (or similarly named) flag can mirror that same shape to hook local-store persistence into the same place in `cmd/atcr/reconcile.go`.

Second, `atcr debt` is already a live Cobra command family (`list`, `add`, `dashboard`) backed by the Epic-12.1 sharded store under `.planning/technical-debt/items/*.yaml` and the `.planning/technical-debt/README.md` table, via `internal/tdmigrate` and `internal/debt`. Rather than stand up a second, confusingly-named top-level `atcr` command for the public/standalone store, the default path is to extend this existing family with a `resolve` subcommand registered in `cmd/atcr/debt.go`'s `newDebtCmd()`.

Third, the fields this epic's skill route needs to consume — `Justification` and `SourceReport` on reconciled findings — are already live in main, not a future dependency. They are stamped during the normal reconcile gate path and serialized on the JSON finding record, so AC3 (consuming justification/back-reference fields) requires no new plumbing, only reading what `atcr reconcile` already produces.

## Key Concepts

**Scorecard-flag precedent for a persistence hook.** `atcr reconcile` already has a `--no-scorecard` flag (`cmd/atcr/reconcile.go:43`) that opts out of a post-reconcile side effect. A new local-TD-store persistence hook should follow the same pattern — run after `internal/reconcile.RunReconcile` returns (`cmd/atcr/reconcile.go:90-99`) and after the scorecard emit block (`cmd/atcr/reconcile.go:110-111`), with its own `--no-local-debt`-style opt-out.
> Source: [codebase-discovery.json: related_files "cmd/atcr/reconcile.go"]
> Source: [codebase-discovery.json: integration_points "cmd/atcr/reconcile.go:runReconcile"]

**`atcr debt` as the extension point, not a new command family.** `atcr debt` already registers `list`/`add`/`dashboard` subcommands off a single parent Cobra command in `cmd/atcr/debt.go`'s `newDebtCmd()`, backed by `internal/debt`'s `insertRow`/`aggregate` helpers and `internal/tdmigrate`'s `ParseREADME`/`Item` parsing of the sharded store. It is currently entirely `.planning/`-scoped (private pipeline only). A public `resolve` verb should be added to this same parent command (`atcr debt resolve`) if the resolve flow is exposed as a CLI subcommand, consistent with `skill/SKILL.md` line 79 already documenting `atcr debt` in its routing table — extending the subcommand surface is additive and does not require changing that top-level routing row.
> Source: [codebase-discovery.json: existing_patterns[2] "Sharded technical-debt store + CLI"]
> Source: [codebase-discovery.json: integration_points "cmd/atcr/debt.go:newDebtCmd"]
> Source: [codebase-discovery.json: reusable_components "atcr debt CLI subcommand family"]

**Justification/SourceReport are already live, not a pending dependency.** `internal/reconcile/emit.go:154`'s `JSONFinding` type already carries `Justification` (`json:"justification,omitempty"`) and `SourceReport` (a back-reference to the review.md narrative section). These are stamped by `internal/reconcile/justification.go:72`'s `stampJustifications` during the normal `atcr reconcile` gate path invoked from `internal/reconcile/gate.go:255`. This is an ATCR-internal extension of the public `reconcile/finding.go` library type, not part of the library's core `Finding` struct — meaning AC3 can rely on `reconciled/findings.json` today without waiting on or re-verifying a separate epic landing.
> Source: [codebase-discovery.json: existing_patterns[3] "justification/back-reference already live on reconciled findings"]
> Source: [codebase-discovery.json: reusable_components "stampJustifications narrative extractor"]

**Where a persistence hook needs to sit relative to justification stamping.** The CLI hook in `cmd/atcr/reconcile.go` receives the `Result` after `stampJustifications` has already run inside the gate pipeline (`internal/reconcile/gate.go:255`), so a local-store persistence call placed after the scorecard emit block will automatically see enriched fields — no change to `gate.go` itself is required unless the hook is later moved deeper into the engine.
> Source: [codebase-discovery.json: integration_points "internal/reconcile/gate.go:255 (post stampJustifications)"]

**The resolver must consume the stable symbol anchor.** Epic 18.1 stamps a leading `(symbolName)` anchor into `JSONFinding.Problem` so a downstream resolver can relocate a finding after earlier fixes shift line numbers. `docs/technical-debt-format.md` defines the anchor contract and the `^\(([^)]+)\)\s+` extraction regex. The `/atcr debt resolve` skill should prefer the anchor over the raw `line` when locating the code block to edit, falling back to the cited line only when no anchor is present.
> Source: `docs/technical-debt-format.md`

**Resolved design decisions.** The three integration gaps recorded in the discovery snapshot have been closed during design sprint refinement:

1. **Reconcile-time persistence granularity:** write-time dedup by `history.FindingID(file, line, problem)`. The hook reads the full existing store (`ReadAll`) before appending and skips any finding whose `id` already exists. No severity threshold is applied; every reconciled finding that is not already present is persisted. See AC 02-03.

2. **Skill route store contract:** `/atcr debt resolve` is implemented as a CLI subcommand (`atcr debt resolve`) registered under the existing `atcr debt` Cobra family. The skill shells out to the CLI and never reads `.atcr/debt/*.jsonl` directly, preserving `skill/SKILL.md`'s CLI-invocation-only dispatcher contract. See AC 03-02.

3. **Idempotency on re-reconcile:** write-time dedup provides idempotency. Re-running `atcr reconcile` against the same review directory does not duplicate unchanged findings. A dedup-read failure fails open (append anyway) rather than silently dropping findings. See AC 02-03.

## Quick Reference

| Location | Type | Description |
|---|---|---|
| `cmd/atcr/reconcile.go:runReconcile` | hook | Where a new local-TD-store-append call would be added, mirroring how scorecard emission is wired via `internal/scorecard` with a `--no-scorecard` opt-out flag. Natural placement is after the `scorecard.EmitForReconcile` call (~line 111) so the store receives the same reconciled `Result` scorecard does. |
| `internal/reconcile/gate.go:255` (post `stampJustifications`) | hook | Point in the reconcile gate pipeline where justification/back-reference data is already attached to findings; a local-store persistence call should run after this point so persisted TD records carry the enriched fields. In practice the CLI hook already receives the `Result` post-stamping, so no change to `gate.go` is required unless the hook moves deeper into the engine. |
| `cmd/atcr/debt.go:newDebtCmd` | hook | The existing `atcr debt` command family registers `list`/`add`/`dashboard` subcommands. A `resolve` subcommand can be added here if the public resolve flow is exposed as `atcr debt resolve` (consistent with the `/atcr debt resolve` skill route), or it can live as a separate top-level command if design decides otherwise. Default path is to extend this family. |

## Related Documentation

- `cmd/atcr/reconcile.go` — reconcile command implementation; integration point for the local-store persistence hook (AC2)
- `cmd/atcr/debt.go` — existing `atcr debt` Cobra command family (`list`/`add`/`dashboard`); candidate home for a new `resolve` subcommand
- `internal/reconcile/justification.go` — `stampJustifications`, source of the `Justification`/`SourceReport` fields consumed by AC3
- `internal/reconcile/gate.go` — reconcile gate pipeline; shows where `stampJustifications` runs relative to where a persistence hook would need to read enriched findings
- `docs/technical-debt.md` — existing documentation for the `.planning/`-scoped `atcr debt` commands
