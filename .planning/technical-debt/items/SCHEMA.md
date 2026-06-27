# Technical-Debt Shard Schema (`items/`)

This directory holds technical-debt storage **sharded by source** (Epic 12.1).
Each `*.yaml` file is one provenance unit — all the items captured from a single
`### [date] From <Sprint|Review>: <label>` section of
[`../README.md`](../README.md).

> **Additive, not yet canonical.** The README Markdown table remains the
> authoritative machine-read source for all existing TD tooling. These shards are
> generated alongside it and are not yet read by any tool. See the README's
> "Sharded Storage Format" section.

## File naming

```
items/<YYYY-MM-DD>_<sanitized-label>.yaml
```

- `<YYYY-MM-DD>` is the section date; `<sanitized-label>` is the section label
  lower-cased with non-`[a-z0-9._-]` runs collapsed to `-`.
- The original label is preserved verbatim in the `label:` field; the filename is
  only an addressing scheme. Same-date/same-label collisions get a `-2`, `-3`…
  suffix.
- The leading date prefix means a label can never produce a path-traversal
  filename.

## Shard document

A shard is a single YAML document: section metadata plus an `items` list.

| Field         | Type   | Required | Notes |
|---------------|--------|----------|-------|
| `date`        | string | yes      | `YYYY-MM-DD`. |
| `source_type` | enum   | yes      | `Sprint` or `Review`. |
| `label`       | string | yes      | Original section label, verbatim. |
| `items`       | list   | yes      | ≥ 1 item (see below). |

## Item

| Field         | Type     | Required | Notes |
|---------------|----------|----------|-------|
| `group`       | string   | yes      | Positional group label from the table (`U` or a number). A presentation facet, **not** a storage key. |
| `status`      | enum     | yes      | `open` ↔ `[ ]`, `deferred` ↔ `[/]`, `resolved` ↔ `[x]`. |
| `severity`    | enum     | yes      | `CRITICAL` \| `HIGH` \| `MEDIUM` \| `LOW`. |
| `file`        | string   | yes      | `file:line`, a range, or free text. |
| `problem`     | string   | yes      | Long-form; multi-line allowed (block scalar). |
| `fix`         | string   | yes      | Long-form; multi-line allowed (block scalar). |
| `category`    | string   | yes      | Free-form label (`correctness`, `security`, …) — intentionally not an enum. |
| `est_minutes` | int      | yes      | Best-guess effort, ≥ 0. |
| `source`      | string   | yes      | Capture origin (`code-review`, `execute-epic-*`, …). |
| `reviewers`   | string[] | no       | Reconciled sections only (`omitempty`). |
| `confidence`  | string   | no       | Reconciled sections only (`omitempty`). |
| `notes`       | string   | no       | Optional long-form, for hand-editing (`omitempty`). |

## YAML-safety guarantees

The format must never become a silent failure source:

1. **Generated, not hand-authored.** Shards are produced by `yaml.v3` `Marshal`,
   which emits valid, correctly-quoted YAML by construction. Values that *look*
   like other YAML types (`no`/`yes`/`on`/`off`, `1.10`, `007`, `null`, leading
   `-`, embedded `:`) are quoted so they round-trip as strings.
2. **Strict-load validation gate.** `td-migrate validate` decodes every shard with
   `KnownFields(true)` plus schema checks. An unknown field, a tab in
   indentation, a bad enum, or a missing required field is a **hard error**
   (non-zero exit) — never ignored.
3. **Adversarial fixture corpus.** `internal/tdmigrate/fixtures_test.go` round-trips
   the known YAML footguns above to lock the safety in place.

## Editing by hand

Hand-edits are allowed but must keep the file valid: run
`go run ./cmd/td-migrate validate` afterward. Prefer block scalars (`|`) for
multi-line `problem` / `fix` / `notes`. Re-running `td-migrate migrate`
regenerates every shard from the README table and will overwrite hand-edits, so
edit the README table (still authoritative) when both are intended to agree.
