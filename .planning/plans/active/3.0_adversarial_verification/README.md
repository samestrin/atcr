## Overview
Plan 3.0 adds an adversarial verification stage to atcr. After `atcr reconcile` produces deduped findings with v1 confidence (HIGH/MEDIUM/LOW based on reviewer agreement), `atcr verify` runs skeptic agents — different models from the finders, with tool access — against each finding. Skeptics try to disprove the finding by reading the actual code via the Epic 2.0 tool loop. Verdicts (confirmed | refuted | unverifiable) feed confidence v2: VERIFIED sits above HIGH; refuted findings demote to LOW but are retained with skeptic reasoning visible in a collapsed section. Cost controls include a min_severity floor, per-finding budgets, and skip-already-verified. The result: a CI gate trustworthy enough to block merges.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/3.0_adversarial_verification/`
- [ ] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/3.0_adversarial_verification/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/3.0_adversarial_verification/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/3.0_adversarial_verification/`

## Timeline & Milestones
| Milestone | Target | Status |
|-----------|--------|--------|
| Plan created | 2026-06-14 | ✅ Done |
| User stories + AC | TBD | Pending |
| Sprint design | TBD | Pending |
| Sprint execution | TBD | Pending |
| Verification stage functional | TBD | Pending |
| Docs + fixtures complete | TBD | Pending |

Estimated duration: 3-4 weeks (per epic plan)

## Resource Requirements
- **Backend developer** — Go implementation of verify package, CLI command, MCP tool
- **Test fixtures** — Planted true/false findings, mock skeptics, malformed output cases
- **Documentation** — verification.md, registry.md update, findings-format.md update

## Expected Outcomes
- `atcr verify` command producing verification.json and re-emitted reconciled artifacts
- Confidence v2: VERIFIED > HIGH > MEDIUM > LOW
- CI gate that blocks merges only on verified findings (--fail-on high --require-verified)
- Refuted findings retained with skeptic reasoning (no silent deletion)
- Cost controls: min_severity floor, per-finding budgets, skip-already-verified
- End-to-end fixture corpus with planted true/false findings

## Risk Summary
| Risk | Impact | Mitigation |
|------|--------|------------|
| Over-eager skeptics refute true findings | High | Conservative prompt, --thorough majority voting, refuted retained and visible |
| Cost doubling for large reviews | Medium | min_severity floor, dedup-first, per-finding budgets, skip-already-verified |
| Skeptic/finder shared blind spots | Medium | Different-model rule (necessary-not-sufficient); document limit; Epic 4.0 debate |
| Malformed skeptic output | High | VerdictParser fallback: unverifiable with raw text preserved |

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [Documentation References](documentation/README.md)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)

## Documentation References

Organized documentation indexes for implementation (see [documentation/README.md](documentation/README.md)):

### Critical (Must read before coding)
- **[verification-pipeline.md](documentation/verification-pipeline.md)** — Core verification mechanics: skeptic selection, verdict parsing, confidence v2, re-emit, gate semantics
- **[cli-mcp-integration.md](documentation/cli-mcp-integration.md)** — `atcr verify` subcommand, `--verify` flag, `atcr_verify` MCP tool

### Important (Review during development)
- **[llm-tool-loop.md](documentation/llm-tool-loop.md)** — Skeptic invocation via `invokeToolLoop`, prompt construction, budgets

### Reference (Consult as needed)
- **[testing-fixtures.md](documentation/testing-fixtures.md)** — Testify patterns, golden files, fixture corpus, verdict parsing tests
- **[source.md](documentation/source.md)** — Source index for global specifications and package docs
