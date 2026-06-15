# Tech Debt Captured — Sprint 3.3 Per-Run Scorecard

## TD-001 — Rate-table model-key normalization (MEDIUM)
**Origin:** Phase 1, task 1.2.A adversarial review, 2026-06-15
**File:** internal/llmclient/rates.go:19
**Issue:** `modelRates` keys are bare model ids (e.g. `claude-sonnet-4-6`), but real-world model ids in use carry suffixes or provider prefixes (`claude-opus-4-8[1m]`, `anthropic/claude-...`, `us.anthropic.claude-...`). Any such variant misses the map and silently yields $0 cost, so the scorecard under-reports cost for a renamed or prefixed model with no signal.
**Why accepted:** Graceful degradation (unknown model → $0) is by design and the known-model AC passes with exact ids. Normalization is an accuracy enhancement, not a correctness blocker for the schema/emitter work; the cost column is `omitempty`.
**Fix in:** Phase 2 (scorecard emitter) or a follow-up — normalize the model key before lookup (strip `[...]` suffix and known provider prefixes), or emit a one-time debug signal when a non-empty model misses the table so silent zero-cost is observable.

## TD-002 — Per-turn vs cumulative usage accumulation (tool-loop path) (MEDIUM)
**Origin:** Phase 1, task 1.5 gate review, 2026-06-15
**File:** internal/llmclient/chat.go:85-93 (ChatResponse.Usage); internal/fanout/loop.go:116,273
**Issue:** `ChatResponse.Usage` is per-turn. A tool-capable agent calls `Chat()` N times in the fanout loop (plus a final-answer call), but the loop currently discards `resp.Usage` and no field on fanout `Result` accumulates it. A Phase 2 emitter reading a single `resp.Usage` would undercount multi-turn agents' `tokens_in`/`tokens_out`/`cost_usd`.
**Why accepted:** Phase 1 scope is `internal/llmclient` only; accumulating per-agent usage requires editing the fanout loop/`Result`, which is Phase 2 integration work. Phase 1 correctly exposes per-turn usage from both the single-shot (`CompleteWithUsage`) and tool-loop (`Chat`) paths.
**Fix in:** Phase 2 — add a usage accumulator on the fanout `Result` (sum `resp.Usage` after each `Chat()` in the loop and the final-answer call) before the scorecard emitter reads per-reviewer totals. This is a hard prerequisite for accurate multi-turn token/cost columns.

## TD-003 — Rate table has no override path / staleness auditability (LOW-MEDIUM)
**Origin:** Phase 1, task 1.5 gate review, 2026-06-15
**File:** internal/llmclient/rates.go:17-27
**Issue:** The rate table is hardcoded with no config/env override. Rates are "approximate as of 2026-06" and silently drift; a stale rate yields a confidently-wrong `cost_usd` (worse than the $0 an unknown model produces). No "rates last updated" signal and the model id is not surfaced to distinguish "unknown model → $0" from "free".
**Why accepted:** Acceptable for Phase 1 provided the scorecard docs (Phase 5) describe cost as approximate. Not a correctness blocker for the schema/emitter.
**Fix in:** Phase 5 docs note cost is approximate; durability follow-up — expose the table via config (YAML key) and/or centralize a "rates last updated" constant; ensure the scorecard records the model id so a $0 cost is auditable.

## TD-004 — Scorecard append atomicity assumes POSIX O_APPEND semantics (LOW)
**Origin:** Phase 2, task 2.2.A adversarial review, 2026-06-15
**File:** internal/scorecard/store.go:23
**Issue:** Concurrent same-file appends rely on a single `write()` to an `O_APPEND` regular file being atomic (contiguous, end-of-file seek + write). This holds on Linux and macOS regardless of record size, and the comment + concurrency test were corrected in 2.3 to assert un-torn lines via a per-record sentinel. The remaining gap is purely portability: a future platform whose filesystem/runtime does not provide atomic per-`write()` `O_APPEND` for regular files (e.g. some network filesystems, or a non-POSIX target) could interleave or tear lines under truly concurrent multi-process reconcile runs.
**Why accepted:** atcr targets Linux/macOS where the guarantee holds; the scorecard store is local. Two concurrent `atcr reconcile` runs against the same month file is already an uncommon case, and torn lines degrade gracefully (ReadRecords skips malformed lines).
**Fix in:** A follow-up if a non-POSIX or networked-FS target is added — gate concurrent writes with an OS advisory file lock (flock) around the append, or document the single-writer assumption for networked config dirs.

## TD-005 — MCP atcr_reconcile does not emit a scorecard (CLI/MCP parity) (MEDIUM)
**Origin:** Phase 2, task 2.5 gate review, 2026-06-15
**File:** internal/mcp/handlers.go (atcr_reconcile handler)
**Issue:** The CLI `atcr reconcile` emits a per-run scorecard (cmd/atcr/reconcile.go:emitScorecard), but the MCP `atcr_reconcile` handler runs `RunReconcile` without emitting one. Any reconcile driven through the MCP server (the agentic Skill path) produces zero scorecard records, so the local store — and the future leaderboard data set — silently omits all MCP-driven runs. The two reconcile entry points diverge.
**Why accepted:** Phase 2's approved scope was the CLI reconcile path plus the fan-out usage wiring; internal/mcp was explicitly not in scope (and the original epic ACs are all phrased around `atcr reconcile`, the CLI). Whether MCP-driven reconciles SHOULD emit local scorecards is a genuine product decision (the MCP path serves the agentic Skill — local records there may be desirable or noise), not a clear bug.
**Fix in:** A follow-up once the product decision is made — if yes, extract `emitScorecard(reviewDir, res)` into a shared helper invoked from both the CLI command and the MCP handler after `RunReconcile` succeeds (mirrors the shared gate-threshold resolver pattern so the two layers cannot fork); if no, document scorecard emission as intentionally CLI-only.
**Resolved:** 2026-06-15 — decision: MCP-driven reconciles SHOULD emit (the store is the monitoring foundation, not CLI-only). Extracted `scorecard.EmitForReconcile(reviewDir, res)` as the single shared bridge; both `cmd/atcr/reconcile.go` and `internal/mcp/handlers.go:handleReconcile` now call it after `RunReconcile` succeeds, so the two entry points cannot diverge. Added unit tests for the bridge and HOME isolation to the affected MCP test.

## TD-006 — No e2e assertion that tool-loop usage reaches status.json (LOW)
**Origin:** Phase 2, task 2.5 gate review, 2026-06-15
**File:** internal/fanout/loop.go:128,284
**Issue:** Tool-loop token accumulation (`addUsage` after every `Chat()` turn plus the final-answer call) is unit-tested (`TestResult_AddUsageAccumulates`), and persistence of usage through `status.json` is covered for the single-shot path (`TestReadPoolSummary_RoundTrip`, `TestStatusFor_PersistsUsageWhenPresent`). But no single end-to-end test drives a tool-enabled agent through `invokeToolLoop` and asserts its summed multi-turn usage lands in the persisted `status.json`. `statusFor` is path-agnostic (it persists whatever tokens the Result carries), so the gap is harmless today.
**Why accepted:** The wiring is verified by code inspection and the constituent links are each unit-tested; the existing tool-loop suite still passes. A full tool-loop e2e fixture (fake ChatCompleter returning per-turn usage + dispatcher) is more setup than the LOW severity warrants for Phase 2.
**Fix in:** Extend `internal/fanout/engine_e2e_test.go` with a tool-enabled agent whose fake ChatCompleter returns non-zero per-turn usage; assert the persisted `status.json` records the summed `tokens_in`/`tokens_out` and the model.
