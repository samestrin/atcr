# Verification Pipeline Architecture [CRITICAL]

## Overview

The verification pipeline is the core of Epic 3.0. It runs **after** `atcr reconcile`, consuming deduped findings from `reconciled/findings.json` and running skeptic agents against each finding. Skeptics are registry agents with `role: skeptic`, selected under a **different-model rule** (a skeptic cannot share a model with any reviewer credited on the finding). Each skeptic receives a per-finding prompt ("try to disprove this finding") and runs the Epic 2.0 tool loop to check the actual code.

Verdicts (`confirmed` | `refuted` | `unverifiable`) feed **confidence v2**: VERIFIED (confirmed by skeptic) sits above HIGH; refuted findings demote to LOW but are retained (never deleted) with skeptic reasoning visible in a collapsed section. Cost controls include a `min_severity` floor (default MEDIUM), per-finding budgets, and skip-already-verified unless `--fresh`.

> Source: [original-requirements.md:Proposed Solution]
> Source: [plan.md:Technical Planning Notes]
> Source: [codebase-discovery.json:architecture_notes]

---

## Key Concepts

### Skeptic Selection & Different-Model Rule

- Skeptics are registry agents with `role: skeptic`. Use the constant `RoleSkeptic` from `internal/registry/config.go:37` (not the string literal).
- **Different-model rule:** Enforced at **selection time per finding**, not at load. A skeptic sharing a model with any reviewer credited on the finding is ineligible for that finding.
- If no eligible skeptic exists (all skeptics share models with credited reviewers, or no skeptics are registered), the finding gets `verdict='unverifiable'` with `reason='no_eligible_skeptic'`.
- The `Role` field on `AgentConfig` is already validated by `roleValid()` at `registry/config.go:111`. Empty defaults to `reviewer` at activation time (`applyDefaults()` at line 262). Epic 3.0 **activates** the role by filtering agents where `Role == RoleSkeptic`.

> Source: [codebase-discovery.json:existing_patterns → Registry Role Validation + Constants]
> Source: [codebase-discovery.json:integration_points → registry/config.go:37]

### Verdict Envelope & Parsing

- Skeptic output must be a **strict parseable envelope** with verdict + reasoning.
- Verdicts: `confirmed` | `refuted` | `unverifiable` (could not be established either way — e.g., budget tripped, evidence outside the jail).
- `internal/verify/verdict.go` produces a `*reconcile.Verification` value. The struct already exists at `internal/reconcile/emit.go:36` with fields `Verdict string`, `Skeptic string`, `Notes string` (omitempty). It is embedded as `*Verification` on `JSONFinding` at line 59, tagged `omitempty`.
- **Malformed output fallback:** If the skeptic response cannot be parsed, the verdict is `unverifiable` with the raw text preserved in the `Notes` field. The finding is **never dropped**.

> Source: [original-requirements.md:Skeptic mechanics]
> Source: [codebase-discovery.json:existing_patterns → Verification Struct (Reserved)]
> Source: [codebase-discovery.json:integration_gaps → Verification struct population path]

### Confidence Model v2

| Tier | Meaning |
|------|--------|
| VERIFIED | Confirmed by skeptic(s) |
| HIGH | 2+ independent reviewers, not yet verified or unverifiable |
| MEDIUM | Single reviewer, not refuted |
| LOW | Refuted — demoted, retained for audit with skeptic reasoning |

- Unverified findings keep their v1 confidence (HIGH/MEDIUM/LOW).
- Refuted findings become LOW **regardless of prior confidence**.
- Refuted findings stay in `findings.json`/`report.md` under a collapsed "Refuted" section — deletion would hide skeptic errors from the human.

> Source: [original-requirements.md:Confidence model v2]
> Source: [codebase-discovery.json:architecture_notes]

### Vote Mechanics

- `verify.votes` config: default 1 skeptic per finding; `--thorough` mode uses 3 skeptics with **majority rule**.
- Disagreeing skeptics → `unverifiable` with all reasonings preserved.
- Votes are collected per finding, then a verdict is computed from the majority.

> Source: [original-requirements.md:Skeptic mechanics]
> Source: [plan.md:Technical Planning Notes]

### Cost Controls

- `verify.min_severity` (default MEDIUM): findings below the floor skip verification and keep their v1 confidence.
- Per-skeptic budgets reuse the 2.0 loop budgets; per-finding timeout.
- Findings already VERIFIED in a previous run (re-verify after reconcile re-run) are skipped unless `--fresh`.

> Source: [original-requirements.md:Cost controls]
> Source: [plan.md:Technical Planning Notes]

### Re-emit & Manifest Updates

- Verify writes `reconciled/verification.json`: per finding — skeptic(s), model(s), verdict, reasoning, budgets used, duration.
- Verify re-emits `reconciled/findings.json` with per-finding `verification` block populated.
- `manifest.json` stages gains `"verify"` after the verify stage runs. Write via `fanout.WriteManifest(reviewDir, &m)`, which delegates to `payload.WriteManifest` at `internal/payload/manifest.go:86` (NOT in `reconcile/emit.go`).
- `summary.json` gains `verdictCounts` (confirmed/refuted/unverifiable breakdowns).
- Transcript artifacts: `verify/raw/<skeptic>/transcript.jsonl` per skeptic invocation (same format as Epic 2.0 reviewer transcripts).

### Verification Artifact Schema

`reconciled/verification.json` is the structured record of the verify stage. It is separate from `findings.json` so the per-finding verdict envelope stays compact and re-runnable without rewriting the full finding text.

```json
{
  "verifiedAt": "2026-06-14T12:00:00Z",
  "minSeverity": "MEDIUM",
  "fresh": false,
  "thorough": false,
  "findings": [
    {
      "file": "internal/auth/token.go",
      "line": 42,
      "problem": "JWT signature not verified before claims are read",
      "verdict": "confirmed",
      "skeptic": "skeptic-claude",
      "model": "claude-sonnet-4-6",
      "reasoning": "The code calls jwt.Parse without the key function; the claims are read before verification.",
      "durationMs": 3400,
      "trippedBudgets": []
    }
  ],
  "verdictCounts": {
    "confirmed": 12,
    "refuted": 3,
    "unverifiable": 1
  }
}
```

- `verdict` is one of `confirmed`, `refuted`, or `unverifiable`.
- `trippedBudgets` lists any loop budgets that halted the skeptic (e.g., `["max_turns"]`), producing an `unverifiable` verdict.
- `verdictCounts` mirrors the breakdown written to `summary.json`.

> Source: [original-requirements.md:Artifacts]
> Source: [codebase-discovery.json:integration_points → payload/manifest.go:86]
> Source: [codebase-discovery.json:architecture_notes]

---

## Code Examples

### Verification Struct (Reserved in Codebase)

```go
// From internal/reconcile/emit.go:36
type Verification struct {
    Verdict string `json:"verdict"`              // confirmed|refuted|unverifiable
    Skeptic string `json:"skeptic"`              // skeptic agent name
    Notes   string `json:"notes,omitempty"`      // reasoning or raw text if malformed
}

// From internal/reconcile/emit.go:59
type JSONFinding struct {
    // ... other fields ...
    Verification *Verification `json:"verification,omitempty"`
}
```

> Source: [codebase-discovery.json:existing_patterns → Verification Struct (Reserved)]

### Skeptic Selection (Different-Model Rule)

```go
// Filter skeptics by role using the constant
var eligible []registry.Agent
for name, a := range reg.Agents {
    if a.Role == registry.RoleSkeptic {
        // Apply different-model rule at selection time
        if !sharesModelWithReviewers(a, finding.Reviewers) {
            eligible = append(eligible, a)
        }
    }
}

if len(eligible) == 0 {
    // No eligible skeptic → unverifiable
    return &reconcile.Verification{
        Verdict: "unverifiable",
        Notes:   "no_eligible_skeptic",
    }
}
```

> Source: [codebase-discovery.json:reusable_components → Registry Agent Filter]

### Confidence v2 Recomputation

```go
func confidenceV2(v1Confidence string, verdict string) string {
    switch verdict {
    case "confirmed":
        return "VERIFIED"
    case "refuted":
        return "LOW"
    case "unverifiable":
        return v1Confidence // keep v1 confidence
    default:
        return v1Confidence // no verdict yet
    }
}
```

> Source: [original-requirements.md:Confidence model v2]

### Manifest Stage Update

```go
// Load existing manifest.json from the review directory.
data, err := os.ReadFile(filepath.Join(reviewDir, "manifest.json"))
if err != nil {
    return err
}
var m payload.Manifest
if err := json.Unmarshal(data, &m); err != nil {
    return err
}

// Append 'verify' to stages.
m.Stages = append(m.Stages, "verify")

// Write atomically via the fanout wrapper (delegates to payload.WriteManifest).
if err := fanout.WriteManifest(reviewDir, &m); err != nil {
    return err
}
```

> Source: [codebase-discovery.json:integration_points → payload/manifest.go:86]
> Source: [internal/fanout/reviewdir.go:WriteManifest]

---

## Quick Reference

| Concept | Location | Notes |
|---------|----------|-------|
| RoleSkeptic constant | `internal/registry/config.go:37` | Use constant, not string literal |
| Verification struct | `internal/reconcile/emit.go:36` | Already reserved, populate-on-write |
| JSONFinding.Verification | `internal/reconcile/emit.go:59` | `*Verification`, omitempty |
| ReadReconciledFindings | `internal/reconcile/emit.go:145` | Loads `reconciled/findings.json` |
| WriteManifest | `internal/payload/manifest.go:86` | NOT in reconcile/emit.go |
| CountAtOrAbove | `internal/reconcile/gate.go:57` | Gate counter for `--fail-on`; operates on `[]reconcile.Merged` |
| failingFindings | `internal/mcp/handlers.go:339` | MCP-layer gate helper; returns `[]reconcile.JSONFinding` |

---

## Related Documentation

- [CLI & MCP Integration](cli-mcp-integration.md) — `atcr verify` subcommand, `atcr_verify` MCP tool
- [LLM Integration & Tool Loop](llm-tool-loop.md) — skeptic invocation via `invokeToolLoop`
- [Testing & Fixtures](testing-fixtures.md) — verdict parsing tests, fixture corpus
