# Per-Persona Corroboration Scores [IMPORTANT]

## Overview

Epic 3.3 added per-run scorecard tracking: every finding records which reviewers credited it and whether a skeptic later confirmed or refuted it. Sprint 9.0 surfaces this data in `atcr personas list --scores` so teams can see which personas are finding the most corroborated issues on their actual codebase. The same per-reviewer corroboration rate is reused as the tie-break input for language-aware skeptic routing (T8).

> Source: [original-requirements.md / Phase 3 — Per-persona corroboration tracking]

## Key Concepts

### Scorecard Storage

Scorecard records are stored as monthly JSONL files under the user config directory:

```
~/.config/atcr/scorecard/YYYY-MM.jsonl
```

The default directory is returned by `scorecard.DefaultDir()` (`internal/scorecard/paths.go:23`), which resolves `os.UserConfigDir()/atcr/scorecard`. Tests can override the directory by passing an explicit path.

> Source: [internal/scorecard/paths.go]

### Corroboration Rate

`scorecard.Aggregate()` (`internal/scorecard/aggregate.go:118`) groups records by `(reviewer, model)` and emits `LeaderboardRow.CorroborationRate`. For `atcr personas list --scores`, the rate is keyed by reviewer name and displayed alongside each installed persona. The same `map[string]float64` shape is passed into `SelectEligibleSkeptics` as the `scores` parameter for language-match tie-breaking.

> Source: [codebase-discovery.json / Pattern "Scorecard aggregation for corroboration rates"]

### `--scores` Flag Behavior

When `atcr personas list --scores` is invoked, the command:

1. Lists installed personas from `~/.config/atcr/personas/`.
2. Reads scorecard JSONL records via `scorecard.ReadAll` or `scorecard.Aggregate` from the configured scorecard directory.
3. Maps each persona/reviewer name to its `CorroborationRate`.
4. Prints the persona name, namespace, version, and corroboration rate (or "n/a" when no data exists).

The implementation lives in `internal/personas/list.go` and follows the existing `cmd/atcr/leaderboard.go` pattern for reading and aggregating scorecard records.

> Source: [codebase-discovery.json / Integration gap: internal/scorecard]

### Shared Map Shape with T8

The corroboration score carrier is intentionally a plain `map[string]float64` (reviewer name → rate) so that T6 and T8 share one shape:

- T6 builds the map from `scorecard.Aggregate()` and renders it in `atcr personas list --scores`.
- T8 receives the same map as the fourth parameter to `SelectEligibleSkeptics` for matched-partition tie-breaking.
- The `verify` package gains no new `scorecard` import; the caller (`internal/verify/pipeline.go:162`) builds the map and passes it in.

> Source: [original-requirements.md / Technical Approach / Score carrier]

## Code Examples

### Scorecard aggregation

```go
records, err := scorecard.ReadAll(dir, scorecard.ReadOptions{})
if err != nil { ... }
rows, err := scorecard.Aggregate(records)
```

> Source: [standard-library.md / testing + net/http/httptest — provider mocks]

### Scores map for SelectEligibleSkeptics

```go
scores := make(map[string]float64)
for _, row := range rows {
    scores[row.Reviewer] = row.CorroborationRate
}
sk := SelectEligibleSkeptics(reg, finding, votes, scores)
```

> Source: [skeptic-routing-verification.md / Signature Decision]

## Quick Reference

| Item | Detail |
|------|--------|
| Scorecard directory | `~/.config/atcr/scorecard/` |
| Default dir function | `scorecard.DefaultDir()` |
| Record format | Monthly JSONL (`YYYY-MM.jsonl`) |
| Aggregation function | `scorecard.Aggregate()` |
| Rate field | `LeaderboardRow.CorroborationRate` |
| CLI flag | `atcr personas list --scores` |
| Implementation file | `internal/personas/list.go` |
| Shared map shape | `map[string]float64` (reviewer → rate) |
| Consumer of same map | `SelectEligibleSkeptics` tie-break (T8) |
| Existing pattern | `cmd/atcr/leaderboard.go` |

## Related Documentation

- `internal/scorecard/scorecard.go` — `Record` schema
- `internal/scorecard/aggregate.go` — `Aggregate()` and `LeaderboardRow`
- `internal/scorecard/paths.go` — default scorecard directory
- `cmd/atcr/leaderboard.go` — existing CLI pattern for scorecard aggregation
- [Skeptic Routing & Verification](skeptic-routing-verification.md) — scores map used for language-match tie-break
