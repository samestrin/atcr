# Code Review Stream - 7.4_executor_agent_mode (Epic)

**Started:** June 22, 2026 05:26:25PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — agent_mode:false behaves identically to Epic 7.0 (no regression, no new snapshot I/O)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:132-152` (the `else` branch is the unchanged snippet path; tool loop entered only when `ex.AgentMode && cc != nil && disp != nil`), `internal/verify/executor_agent_test.go:142` (`TestGenerateFixes_AgentModeOff_SnippetPathUnchanged` — a cc that errors if touched proves the loop is never entered)
- **Notes:** Off-by-default gate; `cc` is threaded but ignored when AgentMode is false.

### Criterion: AC2 — agent_mode:true enables the tool loop; executor reads file:line before proposing a fix
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:341` (`invokeExecutor`), `internal/verify/executor.go` `buildExecutorAgentPrompt` ("Read the file at the cited location first"), `internal/verify/executor_agent_test.go:51` (`TestGenerateFixes_AgentMode_ToolLoopThenFix` asserts `disp.count() >= 1`), `:37` (`PopulatesFix`)
- **Notes:** Agent built with `Tools:true, SupportsFC:true`; tool loop dispatches before the JSON fix is parsed.

### Criterion: AC3 — max_tool_calls respected; executor emits fix from available context at cap
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:353-377` (StatusOK-with-tripped-budget flows into the parser; only non-OK is a failure), `internal/registry/config.go` `EffectiveMaxToolCalls` → agent `MaxTurns`, `internal/verify/executor_agent_test.go:81` (`MaxToolCallsCapEmitsPartialFix`)
- **Notes:** Deliberate divergence from invokeSkeptic (skeptic→unverifiable, executor→best-effort fix) is documented and tested.

### Criterion: AC4 — tool-loop timeout/error produces FixWarning; run continues, other findings emitted
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:362-374` (non-OK status → warn string, never an error), `internal/verify/executor.go:152-159` (warn folded into `f.FixWarning`, `return`), `internal/verify/executor_agent_test.go:66` (`ProviderErrorWarns`)
- **Notes:** invokeExecutor never returns an error; zero-result guard prevents panic.

### Criterion: AC5 — agent mode reuses the already-open dispatcher; no second buildDispatcher / git checkout
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/pipeline.go:257` (passes the in-scope `cc` + `disp` from the skeptic phase into `generateFixes`), `internal/verify/executor.go:347` (`fanout.NewEngine(cc, fanout.WithDispatcher(disp))` — borrows, no new build)
- **Notes:** No `buildDispatcher` or snapshot call inside invokeExecutor.

### Criterion: AC6 — dispatcher unavailable + agent_mode:true → fall back to snippet path with logged warning
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:138-145` (`executor_agent_mode_fallback` warning + snippet path when AgentMode but cc/disp nil), `internal/verify/executor_agent_test.go:114` (`FallsBackWhenDispatcherNil` asserts the log line), `:130` (`FallsBackWhenCCNil`)
- **Notes:** Both halves of the harness (cc and disp) covered.

### Criterion: AC7 — unit tests cover agent_mode=false, =true success, =true tool-loop failure, max_tool_calls cap
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor_agent_test.go` — false (`:142`), true success (`:37`,`:51`), tool-loop failure (`:66`,`:98`), cap (`:81`), plus direct `invokeExecutor`/`parseExecutorResponse`/`buildExecutorAgent` unit tests and an end-to-end `runVerify` integration test (`:238`)
- **Notes:** Coverage exceeds the AC minimum.

### Criterion: AC8 — registry validation rejects agent_mode:true when executor block is absent
- **Verdict:** VERIFIED ✅ (satisfied-by-construction)
- **Evidence:** `internal/registry/config.go` `validateExecutor` early `e == nil` return makes `agent_mode` inexpressible without an executor block; `internal/registry/executor_config_test.go:588` (`TestExecutor_AbsentBlockValid_AC8`), `:543` (`MaxToolCallsOutOfRangeRejected` — the meaningful bound guard 1..MaxExecutorToolCalls)
- **Notes:** Matches the recorded clarification; the unsatisfiable literal AC was replaced with the absent-block-valid + bound-check guards.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (full hostile review; epic had no embedded adversarial tasks)
**Files Reviewed:** 7
**Issues Found:** 9 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic — no sprint-design.md)

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 9

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 3
- Low: 5

### Key Conclusion
The implementation is **functionally correct on all 5 epic contracts** (AC1 byte-for-byte snippet-path parity, AC2 tool-loop reuse, AC3 cap-emits-best-effort, AC4 failure isolation, AC5 dispatcher reuse, AC6 fallback). The independent implementation review found **zero** correctness/security/concurrency defects — only one cosmetic doc overstatement. The remaining 8 findings are **test-quality gaps**: places where green tests under-prove the behavior (AC5 reuse not asserted, StatusTimeout branch unexercised, AC2 "reads first" weakly pinned, no concurrent agent-mode -race test, sentinel-injection guard unpinned, AC6 log assertions weak/half-covered, end-to-end test over-mocked). None block the epic; all are good follow-up hardening.
