## Plan Overview
**Plan Type:** feature
**Last Modified:** 2026-06-14
**Plan Goal:** Add an adversarial verification stage where skeptic agents (different models from the finders) attempt to refute each unique finding before it reaches the final report, producing verdicts that feed a second confidence axis and making the CI gate trustworthy enough to block merges.
**Target Users:** Developers running CI gates, project maintainers configuring skeptic rosters, developers reviewing verification output
**Framework/Technology:** Go 1.25+, Cobra CLI, MCP SDK, OpenAI-compatible LLM API

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated `/create-user-stories @.planning/plans/active/3.0_adversarial_verification/`
- **Estimated Count:** 6 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/3.0_adversarial_verification/`

## Feature Analysis Summary
Epic 3.0 adds a verification stage to the atcr pipeline. After `atcr reconcile` produces deduped findings with v1 confidence (HIGH/MEDIUM/LOW based on reviewer agreement), `atcr verify` runs skeptic agents against each finding. Skeptics are registry agents with `role: skeptic`, selected under a different-model rule (a skeptic cannot share a model with any reviewer credited on the finding). Each skeptic receives a per-finding prompt ("try to disprove this finding") and runs the Epic 2.0 tool loop to check the actual code. Verdicts (confirmed | refuted | unverifiable) feed confidence v2: VERIFIED (confirmed by skeptic) sits above HIGH; refuted findings demote to LOW but are retained (never deleted) with skeptic reasoning visible in a collapsed section. Cost controls include a min_severity floor (default MEDIUM), per-finding budgets, and skip-already-verified unless --fresh.

## Technical Planning Notes
- **New package:** `internal/verify/` — verify.go (orchestration), skeptic.go (prompt construction), verdict.go (parsing)
- **Reuse:** `invokeToolLoop` from `internal/fanout/loop.go` drives skeptic invocation unchanged; skeptics are just agents with a per-finding prompt scope
- **Registry activation:** `Role` field on `AgentConfig` (already validated as reviewer|skeptic|judge in 1.x) is now acted on; helpers filter skeptics vs reviewers
- **Different-model rule:** Enforced at selection time per finding, not at load — a skeptic sharing a model with any credited reviewer is ineligible for that finding
- **Confidence v2:** VERIFIED > HIGH > MEDIUM > LOW; unverified findings keep v1 confidence; refuted → LOW regardless of prior confidence
- **Gate semantics:** `--fail-on` skips refuted findings; `--require-verified` counts only VERIFIED findings (strictest gate)
- **Re-emit:** Verify writes `verification.json`, re-emits `findings.json` with per-finding `verification` block (already reserved in findings-format v1), updates `manifest.json` stages to include `"verify"`; `summary.json` gains `verdictCounts` (confirmed/refuted/unverifiable breakdowns)
- **Vote mechanics:** `verify.votes` config — default 1 skeptic per finding; `--thorough` mode uses 3 skeptics with majority rule; disagreeing skeptics produce `unverifiable` with all reasonings preserved
- **Transcript artifacts:** `verify/raw/<skeptic>/transcript.jsonl` per skeptic invocation (same format as Epic 2.0 reviewer transcripts)
- **Idempotency:** Verification is re-runnable over the same `reconciled/findings.json`; findings with an existing verdict are skipped unless `--fresh`
- **Skeptic failure handling:** Timeout or provider error on a skeptic invocation yields `unverifiable` for that finding — the run never fails nor drops a finding due to a single skeptic error
- **No new dependencies:** Existing stack (cobra, MCP SDK, testify, yaml.v3) covers all requirements

## Documentation References

Organized documentation indexes for implementation (see [documentation/README.md](documentation/README.md)):

### Critical (Must read before coding)
- **[verification-pipeline.md](documentation/verification-pipeline.md)** — Core verification mechanics: skeptic selection, verdict parsing, confidence v2, re-emit, gate semantics
- **[cli-mcp-integration.md](documentation/cli-mcp-integration.md)** — `atcr verify` subcommand, `--verify` flag, `atcr_verify` MCP tool

### Important (Review during development)
- **[llm-tool-loop.md](documentation/llm-tool-loop.md)** — Skeptic invocation via `invokeToolLoop`, prompt construction, budgets

### Reference (Consult as needed)
- **[testing-fixtures.md](documentation/testing-fixtures.md)** — Testify patterns, golden files, fixture corpus, verdict parsing tests

## Implementation Strategy
Build in dependency order (matches epic task breakdown):
1. **Skeptic selection + role plumbing** — activate Role field, add Skeptics()/Reviewers() helpers, apply default role at activation time
2. **Skeptic invocation** — build per-finding prompt, call invokeToolLoop, collect response
3. **Verdict parsing** — extract verdict (confirmed|refuted|unverifiable) + reasoning from skeptic response; malformed → unverifiable with raw text preserved
4. **Confidence v2 + re-emit** — apply v2 tiers, write verification.json, re-emit findings.json with verification block, update manifest stages
5. **CLI command + MCP tool** — `atcr verify` subcommand, `atcr_verify` MCP tool, `--verify` chaining flag on `atcr review`
6. **Gate semantics** — update `CountAtOrAbove` (`internal/reconcile/gate.go:57`) to skip refuted findings and support `--require-verified`; update `failingFindings` (`internal/mcp/handlers.go:339`) for the MCP path
7. **Report updates** — skeptic section, collapsed refuted section, v2 tier display
8. **Docs + fixtures** — verification.md, update registry.md and findings-format.md, build fixture corpus with planted true/false findings

## Recommended Packages
No high-ROI packages identified — existing stack covers all requirements.

## User Story Themes
| # | Theme | Persona | Journey | Est. Effort |
|---|-------|---------|---------|-------------|
| 1 | Skeptic Selection & Role Plumbing | Backend developer | Configure skeptics in registry.yaml, filter by role, enforce different-model rule per finding | M |
| 2 | Skeptic Invocation & Verdict Parsing | Backend developer | Build per-finding prompt, drive tool loop, parse verdict envelope (confirmed/refuted/unverifiable), handle malformed output | L |
| 3 | Confidence v2 & Re-emit | Backend developer | Recompute confidence tiers, write verification.json, re-emit findings.json with verification block, update manifest stages | M |
| 4 | CLI Command & MCP Tool | Developer | Run `atcr verify`, use `atcr_verify` MCP tool, chain review→reconcile→verify with `atcr review --verify` | M |
| 5 | Gate Semantics | DevOps / CI | `--fail-on` skips refuted findings; `--require-verified` counts only VERIFIED; fixture matrix tests | S |
| 6 | Report Updates & Documentation | Developer | Report shows skeptic section, collapsed refuted section, v2 tiers; docs updated; fixture corpus built | M |

## Planning Success Criteria
- [ ] `atcr verify` runs skeptics over deduped findings and produces verification.json plus re-emitted reconciled artifacts with v2 confidence tiers
- [ ] Different-model rule enforced: skeptic sharing model with credited reviewer is never selected; no eligible skeptic → unverifiable with reason `no_eligible_skeptic`
- [ ] Refuted findings demoted to LOW, retained with skeptic reasoning, excluded from `--fail-on` counts
- [ ] `--fail-on high --require-verified` passes/fails correctly across fixture matrices (confirmed/refuted/unverifiable × severities)
- [ ] `verify.min_severity` floor and vote majority both honored
- [ ] Skill chains review → reconcile → verify and presents the verified report
- [ ] Skeptic envelope parsing tested against malformed outputs (fallback: unverifiable with raw text preserved)
- [ ] End-to-end fixture: deliberately false finding refuted by scripted mock skeptic; true finding confirmed
- [ ] docs: verification.md (mechanics, confidence v2, gate semantics), registry.md (`role: skeptic`)

## Risk Mitigation
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Over-eager skeptics refute true findings (false negatives) | Medium | High | Conservative default prompt ("refute only with concrete evidence"); `--thorough` majority voting (3 skeptics); refuted findings retained and visible; fixture corpus tracks refutation accuracy |
| Cost doubling for large reviews | Medium | Medium | `verify.min_severity` floor (default MEDIUM); dedup-first placement (verify runs on unique findings, not duplicates); per-finding budgets; skip already-verified unless `--fresh` |
| Skeptic and finder share blind spots despite different models | Medium | Medium | Different-model rule is necessary-not-sufficient; document the limit; Epic 4.0 debate adds pressure |
| Malformed skeptic output breaks pipeline | Low | High | VerdictParser fallback: unverifiable with raw text preserved; never drops the finding; tested against malformed fixture |
| Verify stage breaks existing reconciler | Low | High | Verify is a SEPARATE stage; reconciler is unchanged; verify re-emits reconciled artifacts; tested end-to-end |

## Next Steps
1. `/find-documentation @.planning/plans/active/3.0_adversarial_verification/`
2. `/create-documentation @.planning/plans/active/3.0_adversarial_verification/`
3. `/create-user-stories @.planning/plans/active/3.0_adversarial_verification/`
4. `/create-acceptance-criteria @.planning/plans/active/3.0_adversarial_verification/`
5. `/design-sprint @.planning/plans/active/3.0_adversarial_verification/`
6. `/create-sprint @.planning/plans/active/3.0_adversarial_verification/`
