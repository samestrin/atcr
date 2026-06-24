---
id: mem-2026-06-23-dbb271
question: "How should SelectEligibleSkeptics be extended for language-aware persona routing?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [internal/verify/select.go, internal/reconcile/emit.go, internal/scorecard/export.go]
tags: [clarifications, epic-9.0_persona_ecosystem, architecture, verify, skeptic-routing, language-aware]
retrievals: 0
status: active
type: clarifications
---

# How should SelectEligibleSkeptics be extended for language-a

## Decision

Two-partition sort on the eligible names slice: language-matching skeptic names first, non-matching follow. The existing n-cap at select.go:84-86 naturally favors language-matched skeptics. "General-purpose" in the epic is conceptual (= a skeptic with no Language scope declared) — no such field/constant exists in code. Tie-break: highest corroboration score (scorecard.RowData.CorroborationRate) then alphabetical by name (confirmed at 9.0_persona_ecosystem.md:178). File extension for language matching: normalizeExt(filepath.Ext(finding.File)) from JSONFinding.File (internal/reconcile/emit.go:64), where normalizeExt strips the leading dot + lowercases.

RESOLVED 2026-06-24 — corroboration scores reach the selector via a caller-supplied scores map[string]float64 4th parameter (reviewer name -> rate), nil-safe (nil = alphabetical-only tie-break). This is a deliberate one-param signature change; the sole production caller is internal/verify/pipeline.go:162. Chosen over a functional option (avoids Option machinery for one optional input) and a new overload (avoids a first-class scoreless path). The map is built by the caller from scorecard.Aggregate() (LeaderboardRow.CorroborationRate keyed by reviewer) — the same source T6's --scores uses — so verify gains no scorecard import.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/select.go
- internal/reconcile/emit.go
- internal/scorecard/export.go
