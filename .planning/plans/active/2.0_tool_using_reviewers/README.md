## Overview
Plan 2.0 transforms single-shot pool reviewers into bounded agents that explore the repository through read-only, path-jailed tools. The Go engine owns the entire tool harness — no external agent framework, no SSH, no containers. The payload becomes the starting point of a review, not the universe.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/2.0_tool_using_reviewers/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/2.0_tool_using_reviewers/`
- [ ] **Design Sprint** - `/design-sprint @.planning/plans/active/2.0_tool_using_reviewers/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/2.0_tool_using_reviewers/`

## Timeline & Milestones
| Milestone | Target | Dependencies |
|-----------|--------|--------------|
| Tool harness (defs, dispatcher, path jail) | Sprint 1, Week 1 | None |
| Snapshot manager (worktree fast path, git worktree add) | Sprint 1, Week 1 | None |
| Agent loop (multi-turn invoke, budgets, degrade) | Sprint 1, Week 2 | Tool harness, snapshot manager |
| Accounting + artifacts (transcript.jsonl, status counters) | Sprint 2, Week 1 | Agent loop |
| Persona updates (tool-enabled sections) | Sprint 2, Week 1 | Agent loop |
| Registry activation (flip reserved fields) | Sprint 2, Week 2 | Agent loop |
| Docs (registry.md, payload-modes.md) | Sprint 2, Week 2 | All above |
| Integration tests (httptest-scripted, fixture repo) | Sprint 2, Week 2 | All above |

## Resource Requirements
- **Backend engineer**: Go systems programming, concurrency, LLM API integration
- **Testing**: httptest mock providers, fixture repos, path jail escape vectors
- **Documentation**: registry.md, payload-modes.md updates
- **Estimated effort**: 3-4 weeks (per epic estimate)

## Expected Outcomes
- Reviewer agents can read files, grep for patterns, and list directories within the repo snapshot
- Multi-turn conversations with bounded budgets (max_turns, tool_budget_bytes, timeout_secs)
- Path jail prevents escape via absolute paths, `..`, symlinks, or `.git/` access
- Graceful degradation for non-tool-capable models
- Full transcript replay via transcript.jsonl
- No new third-party dependencies

## Risk Summary
| Risk | Impact | Mitigation |
|------|--------|------------|
| Small models thrash with tools | Medium | Loop hygiene, conservative defaults, per-agent opt-in |
| Provider function-calling variance | Medium | Lowest-common-denominator wire format, degrade path |
| Token cost explosion | High | Hard budgets, counters in status.json, docs set expectations |
| Worktree edge cases | Medium | Fast path only when clean, explicit jail tests |

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/) (pending)
- [Acceptance Criteria](acceptance-criteria/) (pending)
