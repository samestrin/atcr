# Code Review Stream - 6.0_cross_examination (Epic)

**Started:** June 21, 2026 01:38:50PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: Severity disputes ≥2 tiers and ambiguous clusters route through debate; judge rulings replace severity-max and resolve merge/split.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/select.go:72-89` (SelectItems filters radar by trigger kind incl. severity_split/gray_zone), `internal/debate/emit.go:110-112` (split ruling overwrites `Severity` with judge's settled value, replacing severity-max), `internal/debate/protocol.go:236-247` + `internal/debate/envelope.go:120-124` (judge envelope carries `cluster_decision: merge|separate` for gray-zone)
- **Notes:** Triggers default-on via `ResolveConfig` (select.go:38-44). Gray-zone rulings recorded (not per-finding-applied) per the documented adjudication-path split (debate.go:133-143).

### Criterion: Three-distinct-models rule enforced across proposer/challenger/judge.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/cast.go:65-102` (CastRoles), `internal/debate/cast.go:77-83` (challenger via `pickDistinct(RoleSkeptic, exclude proposer model)`, judge via `pickDistinct(RoleJudge, exclude proposer+challenger models)`), `internal/debate/cast.go:133-156` (pickDistinct excludes models by set)
- **Notes:** Enforced in code, never config. Insufficient distinct models → unresolved (cast.go:87-89) unless `AllowSingleModel` opt-in (cast.go:91-101). SingleModel disclosed in report.

### Criterion: Bounded protocol — hard 3-turn cap, per-item budget, debate.max_items priority ordering with overflow recorded (never silent).
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/protocol.go:56-78` (RunDebate hard-codes exactly 3 turns: proposer→challenger→judge), `internal/debate/select.go:81-89` (MaxItems cap, Overflow slice populated), `internal/debate/select.go:97-116` (sortByPriority: severity-rank desc first), `internal/debate/emit.go:74-87,209-216` (Overflow written to debate.json), `internal/debate/protocol.go:139-160` (per-seat tool budgets forwarded from AgentConfig)
- **Notes:** `DefaultDebateMaxItems = 5` (registry/config.go:73); 0 = unlimited (select.go:82). Overflow recorded in artifact + report (contested.go:84-86).

### Criterion: Unattended CI run resolves ambiguous clusters without Skill involvement.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/debate.go:62-71` (pure Go `Debate` orchestrator), `cmd/atcr/debate.go:38-68` (`atcr debate` CLI), `internal/mcp/handlers.go:597-624` (`atcr_debate` MCP tool) — three entry points, none requiring host Skill adjudication
- **Notes:** Failure-isolated: harness unavailable / halted seat → unresolved item, never a failed run (debate.go:108-121, debateOne never returns error). Supersedes Skill path for unattended runs per epic Clarifications.

### Criterion: Transcripts replayable; rulings auditable in report.md.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/debate/transcript.go:25-105` (append-only JSONL: per-turn statements + RulingEvent, flush-per-event), `internal/debate/debate.go:216-235` (transcript opened per item, ruling recorded), `internal/report/contested.go:53-87` (report.md "Contested findings" section with per-ruling one-line rationale + overflow disclosure)
- **Notes:** Transcript path recorded in debate.json ItemResult.Transcript (emit.go:68). Best-effort transcript writes never fail the ruling (transcript.go:16-20).


## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (3 hostile reviewers, full mode)
**Files Reviewed:** 20 source files (internal/debate/*, reconcile/confidence, verify/confidence_v2, registry/config, report/contested+render, cmd/atcr/{debate,review,report}, mcp/{handlers,tools,server})
**Issues Found:** 16 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic — no sprint-design.md)

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 4
- Low: 12

### Confirmed-correct (hostile reviewers could not break)
- overturn -> verdict "refuted" never blocks the gate (reconcile/gate.go IsFailing excludes refuted)
- ConfidenceForVerdict: confirmed -> VERIFIED, refuted -> LOW (folded onto existing axis, no DEBATED tier)
- Report Contested section: all free text routed through esc/escTrunc (HTML-escape + newline-flatten + backtick-escape); file paths via codeSpan; no injection reachable
- Transcript JSONL: Statement written via json.Marshal — newline/quote injection impossible
- parseRuling: malformed/empty/out-of-enum judge output degrades to unresolved, never drops the item; brace matching is string/escape-aware
- max_items cap slicing (matched[:N] / matched[N:]) is off-by-one-correct; overflow recorded in debate.json + report
