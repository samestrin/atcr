# Decision: v1.0 naming review of the stable surface

- Date: 2026-10-28
- Status: accepted (the owner accepted R1, R2 and R3 on 2026-10-28)
- Epic: 84.0 (v1.0 Stabilization and Release), task T3

## Context

After 1.0.0, renaming a command, flag, config key, environment variable or
persona breaks users' configs and scripts. Before the freeze we reviewed every
entry in [`docs/stable-surface.md`](../stable-surface.md) (generated from the
code by `cli/stable_surface_test.go`) against two rules:

- **The CLI surface convention** (epic 35.22): a human default with an opt-in
  `--json` ([agentic-consumption.md](../agentic-consumption.md#the---json-convention)),
  reporting entry points kept where they are
  ([2026-10-22-reporting-entry-point-shape.md](2026-10-22-reporting-entry-point-shape.md)),
  and one name per meaning, with a superseded name kept as a hidden deprecated
  alias.
- **The persona naming rule** ([personas-authoring.md](../personas-authoring.md#persona-naming-human-first-names)):
  every persona is one human first name matching `^[a-z]+$`.

A rename that is accepted is applied by task T4, which keeps the old name
working as an alias that prints one deprecation warning per run.

## Proposed renames

| # | Surface | Current name | Proposed name | Why | Owner |
|---|---|---|---|---|---|
| R1 | flag | `atcr verify --fresh` | `atcr verify --reverify` | The same meaning has two names. `atcr review` already split its old `--fresh` into `--reverify` (re-verify findings that carry a verdict) and `--no-file-cache`, and marked `--fresh` deprecated (`cli/review.go`). `atcr verify --fresh` still means exactly what `review --reverify` means (`cli/verify.go`). | accepted |
| R2 | registry key | `executor.fix_timeout` | `executor.fix_timeout_secs` | It is an integer number of seconds (`internal/registry/config.go`, `TimeoutSecs`). Every other integer-seconds key ends in `_secs` (`timeout_secs`, `agents.<name>.timeout_secs`, `sandbox.timeout_secs`); the keys without the suffix (`auto_fix.validate_timeout`, `webhook.job_timeout`) take a duration string such as `30s`. Without the suffix a user cannot tell which form `fix_timeout` takes. | accepted |
| R3 | registry and project key | `cache_max_bytes` | `max_cache_bytes` | The other size caps in the same struct are `max_<thing>_bytes` (`max_claim_bytes`, `max_prefetch_bytes`, `max_sprint_plan_bytes`). `cache_max_bytes` is the only one in the reverse order. | accepted |

## Reviewed and not proposed

- **Personas.** All 26 names (10 built-in, 16 community) are single human
  first names and pass `personas/naming_test.go`. No change.
- **Commands.** No command is renamed. The reporting entry points stay as
  decided on 2026-10-22, and `verify diff` is already a deprecated alias of
  `diff-smell`.
- **`agents.<name>.tool_budget_bytes`.** Its word order differs from
  `payload_byte_budget` and `chunk_byte_budget`, but `tool_budget_bytes` is also
  the budget name atcr writes into `status.json` and `verification.json` when
  the budget trips (`internal/fanout/status.go`, `internal/debate/emit.go`,
  read back by `internal/scorecard/scorecard.go`). Renaming the key would split
  it from the output, and renaming the output would break stored reviews.
- **`debt dashboard --top` and `debt list` / `debt resolve --max`.** Different
  meanings on purpose: `--top` is a display cutoff where 0 hides the list, `--max`
  is a selection cap where 0 means no cap. Both help texts say so.
- **`--repo`.** On `review` and `github` it names the GitHub `owner/name`; on
  `reconcile`, `verify` and `diff-smell` it is the hidden deprecated alias of
  `--repo-root`. The clash ends when those aliases are removed, so no new name
  is needed.
- **Other flags.** `--timeout` on `doctor` and `review` is integer seconds, like
  `timeout_secs`. `benchmark export` and `benchmark fit --in` pair with
  `--output`. `skill export --dest` already has `--dir` as its alias.
  `debt sync-outcomes --td` is short but has one meaning and is documented.
- **`thinking`, `thinking_level`, `thinking_style`.** Three settings: on or
  off, how much, and which request field carries it.
- **Environment variables.** Every name starts with `ATCR_` and is
  upper-case. No rename. `ATCR_CATALOG_URL` and `ATCR_CATALOG_SNAPSHOT` are
  test seams, and `ATCR_TIER4_INDEX_MAX_FILES` and `ATCR_DISABLE_AST_GROUPING`
  are tuning switches; task T6 (stability promise) should say whether they are
  promised or marked experimental, rather than renaming them.

## Notes for T4 and T6

- `atcr review --fresh` says "deprecated" in its help but is not hidden, so the
  inventory lists it as a stable flag. T4 should hide it like the other aliases
  (no rename).
- The existing aliases are hidden and silent on purpose: a parse-time warning
  pollutes consumers that merge stdout and stderr (`cli/flags.go`,
  `addQualitySignalFlags`). The epic asks for one deprecation warning per run for
  the renames accepted here, so T4 must write it to stderr only, once, and never
  into `--json` or `--axi` stdout.
- `telemetry_notice_shown` is state that atcr writes, not a setting a user
  sets. T6 may leave it out of the promise.

## Decision

The owner answered the board card "atcr v1.0 renames (epic 84.0 T3)" on
2026-10-28 (Paperclip interaction `9bfc4c59`, on issue EST-3046):

| # | Rename | Owner |
|---|---|---|
| R1 | `atcr verify --fresh` → `atcr verify --reverify` | accept |
| R2 | `executor.fix_timeout` → `executor.fix_timeout_secs` | accept |
| R3 | `cache_max_bytes` → `max_cache_bytes` | accept |

Task T4 applies all three. Each old name keeps working as an alias that prints
one deprecation warning per run, and the inventory is regenerated.
