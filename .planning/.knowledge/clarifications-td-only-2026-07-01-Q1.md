---
id: mem-2026-07-01-e5d5f4
question: "When diff_smell flags a resolve-td fix as test_only (hard), is a test-file-only change ever the correct resolution?"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: []
tags: []
retrievals: 0
status: active
type: project
---

# When diff_smell flags a resolve-td fix as test_only (hard), 

## Decision

Yes — a test-file-only change is correct-by-construction (a false positive on the diff_smell test_only gate) when the symbol being hardened is itself a test-only helper with no production callers. The test_only gate exists to catch reward-hacking (changing only tests to make a gate pass), but when the flagged code IS a _test.go helper, a test-file-only fix is the ONLY place the change can live, so the flag should be accepted rather than treated as a smell.

Worked example (epic 14.4): buildOneAgent is a test-only helper (internal/fanout/review_test.go:37) that forces proj.Agents=[]string{name} to coerce the production seam into emitting exactly one slot, then indexes slots[0].Primary. Adding a require len(slots)==1 guard before indexing converts a latent panic / silently-wrong first-chunk agent into a loud failure. This touches only a _test.go file and diff_smell flagged it test_only — but it is a legitimate test-scaffold hardening, not a masked production change. The production resolution seam (buildSlots at internal/fanout/review.go:780, renderAgent at :987) correctly emits one slot per chunk and is untouched; the "exactly 1 slot" constraint is a test assumption, not a production invariant.

Decision rule: before treating a test_only flag as a blocker, check whether the flagged symbol has any non-test caller. If none, the flag is a correct-by-construction false positive — accept the fix. If the invariant belongs in production code (a real caller depends on it), the test-only change is insufficient and the guard belongs in the production seam.</answer>
<tags>td-clarification, td-only, testing, diff_smell, reward-hacking, test-helper, adversarial-gate</tags>
<files>internal/fanout/review_test.go, internal/fanout/review.go</files>
<source>clarifications --from=resolve-td 2026-07-01</source>
</invoke>


## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
