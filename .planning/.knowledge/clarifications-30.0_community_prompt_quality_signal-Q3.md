---
id: mem-2026-07-17-affef1
question: "resolve-td over-simplification gate: hard/test_only verdict on a testing-category TD item"
created: 2026-07-17
last_retrieved: ""
sprints: [30.0_community_prompt_quality_signal]
files: [internal/telemetry/quality_signal_test.go, .planning/plans/completed/20.1_public_td_resolve_skill/acceptance-criteria/03-04-red-green-adversarial-refactor-cycle.md]
tags: [td-clarification, resolve-td, over-simplification-gate, testing-category, adversarial-verification]
retrievals: 0
status: active
type: td-clarification
---

# resolve-td over-simplification gate: hard/test_only verdict 

## Decision

The `llm_support_diff_smell` gate's `hard` verdict (`test_only`/`weakened_assertion`) is deliberately non-overridable by the resolve-td skill's own self-review — by design (see .planning/plans/completed/20.1_public_td_resolve_skill/acceptance-criteria/03-04-red-green-adversarial-refactor-cycle.md, Scenario 3 + Error Scenario 2), so the row stays [ ] and the committed GREEN fix waits for human inspection rather than auto-flipping to resolved. This is correct behavior, not a bug — the gate is a blunt, model-independent heuristic that cannot distinguish a genuine reward-hack (weakened/test-only fix hiding a real bug) from a TD item whose Category is literally "testing", where a compliant fix is *necessarily* test-only (you cannot fix "the test doesn't catch X" without touching the test).

When a human (via /clarifications) reviews such a flagged item, check for adversarial proof that the new test closes the gap: does the OLD test still pass while the NEW test fails on the exact input the PROBLEM describes? If yes, the fix is verified, not a reward-hack, and should be accepted (flip to [x]) even though the gate flagged it — the gate did its job by forcing this human checkpoint, and the checkpoint cleared it.

Concrete precedent: internal/telemetry/quality_signal_test.go — TD item's category was "testing", FIX offered two alternative shapes (reflect over struct tags for omitempty/ignored fields, OR assert exact marshaled bytes of a fully-populated fixture). The reflection approach was implemented and mutation-tested (injecting an omitempty field kept the OLD len==4 test green but failed the NEW reflection test). Accepted as-is; the marshaled-bytes alternative was judged redundant — the pre-existing sibling test already asserts exact key names via map comparison, and the marshaled-bytes approach is structurally weaker anyway (it only catches fields the fixture happens to populate, not future field additions the fixture author doesn't yet know about) whereas reflecting over the Go type's tags catches every field regardless of fixture content.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/telemetry/quality_signal_test.go
- .planning/plans/completed/20.1_public_td_resolve_skill/acceptance-criteria/03-04-red-green-adversarial-refactor-cycle.md
