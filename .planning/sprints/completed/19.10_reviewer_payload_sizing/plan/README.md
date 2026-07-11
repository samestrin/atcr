## Overview
Plan 19.10 gives atcr's multi-agent reviewer per-model token awareness. Today a single global byte budget is shipped identically to a heterogeneous 32k→144k-token roster, the 8192-token output cap is never reserved against input sizing, overflow hard-fails agents instead of degrading gracefully, and the 600s timeout is too tight for slow local backends under oversized payloads — confirmed by a 19.6 run where a 101-file/6,429-insertion diff returned 1 finding from 11 reviewers (5 ok, 3 timeout, 3 failed). This plan sizes each reviewer's payload to its own model's window (reserving output tokens), chunks overflow to fit using the window-aware Epic 14.3 chunker rather than shedding files, and ships `on_overflow` as a real configurable degradation policy with fallback provenance always recorded.

## Workflow Status
- [x] **Plan Created**
- [x] **Tasks** - `/create-tasks @.planning/plans/active/19.10_reviewer_payload_sizing/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/19.10_reviewer_payload_sizing/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/19.10_reviewer_payload_sizing/`
- [ ] **Execute Sprint** - `/execute-sprint`

## Timeline & Milestones
| Phase | Estimate | Deliverables |
|-------|----------|--------------|
| Context-window resolver + effective budget | ~1 day | F1 (`ContextWindowTokens`), F2 (output-reserved per-agent input budget) — closes the confirmed `dax` boundary overflow |
| Window-aware chunking + overflow policy | ~1.5 days | F3 (window-aware `chunkDiff` wiring), F4 (`on_overflow` policy: `chunk`/`truncate` implemented, `fallback`/`fail` recognized) |
| Fallback provenance + timeout scaling | ~1 day | F5 (summary.json substitution recording + reconcile de-weighting), F6 (load-scaled timeout) |
| Cache-key correctness + diagnosability | ~1 day | F7 (cache key folds effective budget/chunk plan), F8 (per-agent summary.json fields) |
| Sprint-plan limit + live audit | ~0.5–1.5 days | F9 (`max_sprint_plan_bytes` config), AC-Live scripted harness re-running the 19.6 range |
| **Total** | **4–6 days** | Full implementation, `go test ./...` passing, zero `ContextWindowExceededError` on live audit |

## Resource Requirements
- **Personnel**: 1 backend developer
- **Tools**: Go 1.25+, existing `internal/payload`/`internal/fanout`/`internal/registry`/`internal/cache`/`internal/reconcile` packages
- **External Dependencies**: None — no new third-party package; conservative static byte→token ratio, not a live tokenizer
- **Testing**: `go test`, testify assertions, plus a scripted (non-`go test`) live audit against `orchestrator.lan`

## Expected Outcomes
1. **No context-window failures**: every reviewer's payload fits its own model's window with the output budget reserved; the `24577 + 8192 > 32768` overflow class cannot recur.
2. **Graceful degradation, never silent**: overflow chunks by default (zero content loss), with `truncate`/`fallback`/`fail` as explicit, config-driven alternatives — every action recorded in `summary.json`.
3. **Provenance-preserving fallback**: any model-swap substitution is recorded so `reconcile`'s distinct-reviewer CONFIDENCE is never silently inflated.
4. **No stale cache hits**: a per-agent-sized payload is never served a stale full-payload cache entry.
5. **Timeout resilience**: previously-timed-out agents (`greta`, `vera`, `brad`) complete on large-but-valid multi-chunk payloads.
6. **Configurable sprint-plan context**: operators can raise the 16 KB sprint-plan cap to leverage larger context windows.

## Risk Summary
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Byte→token ratio too optimistic → residual overflow | Medium | High | Conservative ~3.5 B/tok ratio + safety margin; `on_overflow` net catches the tail |
| Chunking a 32k model on a slow backend re-triggers timeouts | Medium | High | F6 timeout scaling designed *with* chunking, not after |
| Cache serves stale full-payload for a per-agent-sized request | Medium | High | F7 cache-key folds effective budget/chunk plan; explicit regression test |
| Fallback silently corrupts reconcile CONFIDENCE | Low | Medium | F5 records every substitution; fallback is opt-in, not default |
| Scope creep into `internal/registry` config schema → larger sprint | Medium | Medium | Hold changes to payload+fanout; read resolved values; escalate explicitly if a schema field is unavoidable |

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Tasks](tasks/)
- [Sprint Design](sprint-design.md)

## Documentation References

### Critical (read before coding)
- **[CRITICAL]** [Context-Window Resolver](documentation/context-window-resolver.md) — static per-model context-window table (F1)
- **[CRITICAL]** [Per-Agent Budget & Chunking](documentation/per-agent-budget-and-chunking.md) — output-reserved budget and window-aware chunk plan (F2/F3)
- **[CRITICAL]** [on_overflow Policy](documentation/on-overflow-policy.md) — degradation ladder and config surface (F4)

### Important (review during development)
- **[IMPORTANT]** [Cache-Key Correctness](documentation/cache-key-correctness.md) — folding budget/chunk plan into the diff-cache key (F7)
- **[IMPORTANT]** [Diagnosability Fields](documentation/diagnosability-fields.md) — per-agent `summary.json` fields (F8)
- **[IMPORTANT]** [Fallback Provenance](documentation/fallback-provenance.md) — fallback model substitution recording (F5)
- **[IMPORTANT]** [Timeout Scaling](documentation/timeout-scaling.md) — load-scaled timeout for chunked payloads (F6)
- **[IMPORTANT]** [Config YAML Parsing](documentation/config-yaml-parsing.md) — YAML patterns for `max_sprint_plan_bytes` and `on_overflow` (F9/F4)

See [documentation/README.md](documentation/README.md) for the full index.

## Acceptance Criteria
- [ ] AC1: Each reviewer's payload is sized to its own model window (input tokens ≤ W − O); unit test covers a 32k-window and a 144k-window model
- [ ] AC2: Regression test asserts the input budget reserves the output cap (the `dax` 24577+8192>32768 arithmetic cannot recur)
- [ ] AC3: With `on_overflow: chunk`, an over-window payload delivers whole across multiple appropriately-sized chunks with zero files dropped
- [ ] AC4: `on_overflow` is a recognized config key; `chunk`/`truncate` implemented and tested; `fallback`/`fail` recognized and error clearly if prerequisites unmet
- [ ] AC5: A fallback-served slot's substitution is recorded in `summary.json`; `reconcile` does not double-count it as a distinct reviewer
- [ ] AC6: `greta`/`vera`/`brad` complete on a large-but-valid multi-chunk payload without hitting the timeout wall
- [ ] AC7: The fan-out cache key incorporates the per-agent effective budget/chunk plan
- [ ] AC8: `summary.json` records per-agent effective budget, resolved model window, reserved output tokens, chunk count, and degradation action
- [ ] AC-Live (scripted, env-coupled): re-running the exact 19.6 range against the real roster on `orchestrator.lan` produces zero `ContextWindowExceededError`; `dax`/`otto`/`greta`/`vera`/`brad` all complete `status=ok`; findings come from multiple agents
- [ ] AC9: `go test ./...` passes
- [ ] AC10: The sprint-plan byte limit is configurable via `max_sprint_plan_bytes`, verified by test

## Related Epics
- **Epic 19.6 (Community Registry Hub)**: the confirmed 101-file/6,429-insertion run this plan fixes.
- **Epic 14.3 (Context-Aware Diff Chunking)**: supplies the `chunkDiff`/`mergeChunkResults` primitives this plan makes window-aware.
- **Epic 5.2 (Diff Caching & Incremental Reviews)**: supplies the `internal/cache` key this plan extends for correctness (F7).
- **Epic 12.2 (Sprint Plan Scoping)**: owns the `MaxSprintPlanBytes`/`ScopeConstraint` mechanism this plan makes configurable (F9).
- **Epic 19.7 (Live Model Resolution)**: soft, non-blocking future source of live context-window values for F1's static table.
