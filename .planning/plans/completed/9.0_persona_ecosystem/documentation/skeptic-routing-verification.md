# Skeptic Routing & Verification [CRITICAL]

## Overview

`SelectEligibleSkeptics` (`internal/verify/select.go:55`) is the primary extension point for language-aware skeptic routing. The function currently sorts candidate names alphabetically before applying the n-cap, which guarantees deterministic output. T8 extends this with a two-partition reorder that prefers language-matched skeptics — skeptics whose `AgentConfig.Language` entries overlap the normalized file extension of the finding under review — while preserving determinism within each partition.

The finding's file extension is sourced from `reconcile.JSONFinding.File` via `filepath.Ext(finding.File)`. A small helper, `normalizeExt`, strips the leading dot and lowercases the result before comparing against the skeptic's language scope entries. Within the matched partition, tie-breaking uses a caller-supplied `scores map[string]float64` (reviewer name to corroboration rate), with higher scores ranked first; when scores are equal or the map is nil, names sort alphabetically.

The architecture decision recorded on 2026-06-24 established that corroboration scores reach `SelectEligibleSkeptics` via a fourth parameter (`scores map[string]float64`), nil-safe so that callers without scorecard data fall through to alphabetical-only tie-breaking. The same map shape is built from `scorecard.Aggregate()` (`LeaderboardRow.CorroborationRate` keyed by reviewer name) and is shared with T6's `--scores` consumer, meaning the verify pipeline gains no new scorecard import.

## Key Concepts

### Two-Partition Reorder

After `sort.Strings(names)` at line 81 of `internal/verify/select.go`, the names slice is split into two partitions:

1. **Matched** — skeptics whose `Config.Language` entries overlap `normalizeExt(filepath.Ext(finding.File))`
2. **Unmatched** — all remaining skeptics

The n-cap is applied to `append(matched, unmatched...)`, so matched skeptics are always preferred when slots are limited.

> Source: [codebase-discovery.json / Pattern "Deterministic Skeptic Selection"]

Determinism within each partition is preserved: matched names sort by `scores[name]` descending, then by name ascending; unmatched names sort alphabetically. An absent key in the scores map evaluates to the zero value (`0.0`) and falls through to alphabetical ordering.

> Source: [codebase-discovery.json / Suggested approach]

### Signature Decision (2026-06-24)

The decided signature is:

```
SelectEligibleSkeptics(reg, finding, n, scores map[string]float64)
```

> **Codebase reality at plan time:** `internal/verify/select.go` currently implements the three-parameter form `SelectEligibleSkeptics(reg, finding, n)`. T8 will extend the signature by adding the fourth `scores map[string]float64` parameter and update the sole production caller at `internal/verify/pipeline.go:162`.

The fourth parameter is nil-safe. There is only one production caller: `internal/verify/pipeline.go:162`. Functional-option and separate-overload approaches were evaluated and rejected — the plain map avoids Option machinery for a single optional input and keeps the scoreless path explicit.

> Source: [codebase-discovery.json / Architecture note (DECIDED 2026-06-24)]

### Extension Helper: `normalizeExt`

```
normalizeExt(ext string) string = strings.ToLower(strings.TrimPrefix(ext, '.'))
```

This normalizes `filepath.Ext` output (e.g., `.Go` → `go`, `.TS` → `ts`) before comparing against language scope entries stored in `AgentConfig`.

> Source: [codebase-discovery.json / Suggested approach]

### Skeptic Struct and Language Field

The `Skeptic` struct (`internal/verify/select.go`) carries `Name`, `Config`, and `Provider`. `Config` is an `AgentConfig`, which will hold the `Language` field once T8 adds it. No new struct is required; the field extends the existing config shape.

> Source: [codebase-discovery.json / Reusable component "Skeptic struct"]

### Finding File Extension Source

The file extension used for language matching comes from `reconcile.JSONFinding.File`. The caller extracts it with `filepath.Ext(finding.File)` before passing the finding into `SelectEligibleSkeptics`.

> Source: [codebase-discovery.json / Integration gap: internal/reconcile/emit.go:JSONFinding.File]

## Code Examples

No verbatim Go code appears in the source documents. The following CLI and JSON examples are drawn directly from the adversarial verification interface specification.

### `atcr verify` invocation forms

```
atcr verify [id-or-path]
```

> Source: [adversarial-verification-interface.md / CLI Command: `atcr verify` / Form]

### `atcr review --verify` chaining

```
atcr review --verify [--fail-on <severity>] [--require-verified] ...
```

> Source: [adversarial-verification-interface.md / CLI Chaining: `atcr review --verify` / Form]

### MCP tool input schema

```json
{
  "id_or_path": "string (optional, review id only; paths are not accepted; defaults to .atcr/latest)",
  "fresh": "boolean (optional, default false)",
  "thorough": "boolean (optional, default false)",
  "min_severity": "string (optional, default 'MEDIUM')",
  "fail_on": "string (optional, CRITICAL|HIGH|MEDIUM|LOW)",
  "require_verified": "boolean (optional, default false)"
}
```

> Source: [adversarial-verification-interface.md / MCP Tool: `atcr_verify` / Input schema]

### MCP tool output schema

```json
{
  "review_id": "string",
  "verdict_counts": {
    "confirmed": 0,
    "refuted": 0,
    "unverifiable": 0
  },
  "findings_processed": 0,
  "duration_ms": 0,
  "gate_status": {
    "pass": true,
    "failing_count": 0
  }
}
```

> Source: [adversarial-verification-interface.md / MCP Tool: `atcr_verify` / Output schema]

### CLI output format

```
verified N finding(s): C confirmed, R refuted, U unverifiable
gate: pass
```

When `--fail-on` fires:

```
verified N finding(s): C confirmed, R refuted, U unverifiable
gate: fail — M finding(s) at/above <severity> survive
```

> Source: [adversarial-verification-interface.md / CLI Command: `atcr verify` / Output]

## Quick Reference

| Item | Detail |
|------|--------|
| Primary function | `SelectEligibleSkeptics` — `internal/verify/select.go:55` |
| T8 insertion point | After `sort.Strings(names)` at line 81 |
| Decided signature | `SelectEligibleSkeptics(reg, finding, n, scores map[string]float64)` |
| Scores parameter | 4th param, nil-safe; nil = alphabetical-only tie-break |
| Partition order | `append(matched, unmatched...)` — matched skeptics cap-favored |
| Tie-break (matched) | `scores[name]` desc, then name asc |
| Tie-break (unmatched) | Alphabetical |
| Extension source | `filepath.Ext(reconcile.JSONFinding.File)` |
| Normalization helper | `normalizeExt`: strip `.`, lowercase |
| Scores map shape | `map[string]float64` — reviewer name → corroboration rate |
| Production caller | `internal/verify/pipeline.go:162` |
| Test entry point | `internal/verify/select_test.go:TestSelectEligibleSkeptics` (line 44) |
| Test fixture helper | `internal/verify/invoke_test.go:testSkeptic` (line 22) |
| Language field home | `AgentConfig` (carried by `Skeptic.Config`) |
| Gate exit codes | `0` pass / `1` gate fail / `2` config error |
| `--fail-on` counts | Findings at/above severity whose verdict ≠ `refuted` |
| `--require-verified` | Combined with `--fail-on`: counts only `VERIFIED` confidence findings |
| Refuted rendering | Collapsed `<details>` section; omitted when count = 0 |
| Backward compat | Findings without `verification` block render as pre-Epic 3.0 |

## Related Documentation

- `internal/verify/select.go` — `SelectEligibleSkeptics` implementation; T8 two-partition reorder target
- `internal/verify/select_test.go` — extend `TestSelectEligibleSkeptics` with language-match, no-match fallback, and tie-break cases
- `internal/verify/invoke_test.go` — `testSkeptic` fixture factory; follow for language-aware skeptic test fixtures
- `internal/verify/pipeline.go:162` — sole production caller of `SelectEligibleSkeptics`; receives updated signature
- `internal/reconcile/emit.go` — defines `JSONFinding.File`; source of the finding's file path for extension extraction
- `adversarial-verification-interface.md` — full CLI/MCP interface specification for Epic 3.0 adversarial verification
