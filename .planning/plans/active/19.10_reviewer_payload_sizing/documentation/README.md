# Plan Documentation References

**Created:** July 10, 2026 08:32:45PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### Critical (read before coding)

- [context-window-resolver.md](context-window-resolver.md) — Static per-model context-window table (F1) **[CRITICAL]**
- [per-agent-budget-and-chunking.md](per-agent-budget-and-chunking.md) — Output-reserved per-agent budget and window-aware chunk plan (F2/F3) **[CRITICAL]**
- [on-overflow-policy.md](on-overflow-policy.md) — `on_overflow` degradation ladder: `chunk`, `truncate`, `fallback`, `fail` (F4) **[CRITICAL]**

### Important (review during development)

- [cache-key-correctness.md](cache-key-correctness.md) — Folding effective budget / chunk plan into the diff-cache key (F7) **[IMPORTANT]**
- [diagnosability-fields.md](diagnosability-fields.md) — Per-agent `summary.json` fields for budget/window/chunks/action (F8) **[IMPORTANT]**
- [fallback-provenance.md](fallback-provenance.md) — Recording fallback model substitution and reconcile de-weighting (F5) **[IMPORTANT]**
- [timeout-scaling.md](timeout-scaling.md) — Load-scaled request timeout for chunked payloads (F6) **[IMPORTANT]**
- [config-yaml-parsing.md](config-yaml-parsing.md) — Config YAML parsing patterns for `max_sprint_plan_bytes` and `on_overflow` (F9/F4) **[IMPORTANT]**

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `.planning/specifications/packages/yaml-v3.md` (passed explicitly — `/find-documentation`'s semantic search scored nothing in `.planning/specifications/` above the configured relevance threshold for this plan, since the plan is internal Go wiring across `internal/payload`/`internal/fanout`/`internal/registry`/`internal/reconcile` with no dedicated architectural spec)
- **Plan & Requirements:** [`plan.md`](../plan.md) and [`original-requirements.md`](../original-requirements.md) — the authoritative intent and acceptance criteria for F1–F9 and AC-Live
- **Codebase Discovery:** `.planning/plans/active/19.10_reviewer_payload_sizing/codebase-discovery.json` — verified file paths, existing patterns, integration points, and reusable components
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
