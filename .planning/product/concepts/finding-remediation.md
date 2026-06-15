# Concept: Finding Remediation (Auto-Fix)

**Status:** Conceptual
**Created:** 2026-06-14
**Priority:** Medium (hard but valuable)

## Problem

ATCR finds bugs. That's valuable. But detection is only half the problem — the other half is remediation. A team gets 20 HIGH findings and now has to:
- Understand each finding
- Figure out the right fix
- Write the fix
- Test the fix

For 30-50% of findings, the fix is obvious and mechanical ("add error check", "close file handle", "fix null check"). Teams waste hours on busywork.

Meanwhile, Copilot, Cursor, and other IDE tools are moving toward auto-fix. They can suggest fixes for individual findings, but they lack the multi-model consensus that ATCR provides.

## Solution

**Auto-fix findings.** After ATCR detects a finding, generate a fix, validate it, and present it alongside the finding.

### How it works

1. **Detection** — ATCR finds a bug (existing pipeline).
2. **Fix generation** — A model (possibly a different one) reads the finding + surrounding code and generates a fix.
3. **Validation** — The fix is validated:
   - Does it compile/build?
   - Does it pass existing tests?
   - Does it address the original finding? (re-run ATCR on the fixed code)
4. **Presentation** — The finding now includes a `fix` block with the suggested change:

```json
{
  "severity": "HIGH",
  "file": "auth.go",
  "line": 42,
  "problem": "Missing error check after database call",
  "fix": {
    "description": "Add error check and return early on failure",
    "diff": "@@ -40,6 +40,9 @@\n result, err := db.Query(...)\n+if err != nil {\n+  return fmt.Errorf(\"query failed: %w\", err)\n+}\n",
    "validated": true,
    "validation_details": {
      "compiles": true,
      "tests_pass": true,
      "finding_resolved": true
    }
  }
}
```

### Why this works

- You already have 2.0 tool-using reviewers (they can read the code).
- You already have the finding format (add a `fix` block).
- You already have the validation pipeline (re-run ATCR).
- Teams will pay for auto-fix; detection alone is a commodity.

### Pricing

- **Premium tier** — auto-fix is a paid add-on ($X/month or $Y per fix).
- **Freemium** — detection is free, auto-fix is paid.

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Fix generation | Model generates a fix for each finding | 2 weeks |
| Fix validation | Automated validation (compiles, tests pass, finding resolved) | 1 week |
| Fix presentation | Finding format includes `fix` block | 3 days |
| CLI command | `atcr fix --apply` to apply fixes | 3 days |
| Confidence scoring | Only offer fixes with high confidence | 2 days |

## Revenue Model

**Premium tier:**
- Freemium: detection is free, auto-fix is paid
- $49/mo for 100 auto-fixes, $199/mo for 1000 auto-fixes
- Enterprise: custom pricing

**Why this works:**
- Detection is a commodity; remediation is the value.
- Teams will pay for auto-fix; it saves hours of busywork.
- It differentiates from Copilot/Cursor (they lack multi-model consensus).

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Fix generation | 2 weeks | Model generates a fix for each finding |
| Fix validation | 1 week | Automated validation (compiles, tests pass, finding resolved) |
| Fix presentation | 3 days | Finding format includes `fix` block |
| CLI command | 3 days | `atcr fix --apply` to apply fixes |
| Confidence scoring | 2 days | Only offer fixes with high confidence |
| **Total** | **4-6 weeks** | |

## Moat / Differentiation

- **You have the infrastructure.** 2.0 tool-using reviewers, finding format, validation pipeline.
- **Multi-model consensus.** Copilot/Cursor lack this; their fixes may introduce new bugs.
- **Validation is key.** A fix that doesn't compile or breaks tests is worse than no fix.
- **Premium tier.** Detection is a commodity; remediation is the value.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Auto-fix is hard | High | High | Start with simple findings (missing error checks); expand gradually |
| Fix introduces new bugs | High | High | Strict validation; only offer fixes with high confidence |
| Competing with Copilot/Cursor | Medium | Medium | Differentiate on multi-model consensus + validation |
| Teams don't trust auto-fix | Medium | Medium | Start with "suggested fixes" (manual apply); move to auto-apply later |
| High API costs (fix generation) | Medium | Medium | Use cheaper models for fix generation; only generate fixes for HIGH+ findings |

## Open Questions

- **Which findings get auto-fixes?** All findings, or only HIGH+?
- **Which model generates the fix?** Same model that found it, or a different one?
- **How do you validate?** Compile + tests + re-run ATCR? Or just compile + tests?
- **Do you offer auto-apply?** `atcr fix --apply` or just `atcr fix --suggest`?
- **How do you price it?** Per-fix, subscription, or enterprise?

## Why This Is a Medium-Term Bet

1. **It's hard.** Auto-fix is a hard problem; many fixes will be wrong.
2. **It's valuable.** If you can crack it, teams will pay.
3. **It differentiates.** Copilot/Cursor lack multi-model consensus + validation.
4. **It's a premium tier.** Detection is a commodity; remediation is the value.

**Time to first dollar:** 3-6 months (if you can get auto-fix working).

## Relationship to Other Concepts

- **Epic 2.0 (tool-using reviewers)** — the foundation; fix generation uses the same tool harness.
- **Epic 5.0 (executing reviewers)** — the validation pipeline; re-running ATCR on fixed code.
- **Review-as-a-Service API** — auto-fix could be a premium tier.
- **Team Edition** — enterprises want auto-fix; it's a premium feature.

## References

- Copilot, Cursor — competitors (IDE tools with auto-fix, but no multi-model consensus)
- SWE-bench — the benchmark for auto-fix (different problem, similar idea)
- ATCR Epic 2.0, 5.0 — the foundation
