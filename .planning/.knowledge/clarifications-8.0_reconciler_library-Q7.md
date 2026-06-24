---
id: mem-2026-06-23-68514f
question: "Is auth.go:42 (\"token never expires here | guard it\") a real TD finding or a synthetic test fixture that should be removed from the README?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [internal/reconcile/emit_test.go, internal/reconcile/golden_corpus_test.go, reconcile/reconcile_test.go, internal/reconcile/testdata/golden/findings.json, .planning/technical-debt/README.md]
tags: [clarifications, sprint-8.0_reconciler_library, process, synthetic-td, test-fixture, false-positive, reviewer-hallucination]
retrievals: 0
status: active
type: clarifications
---

# Is auth.go:42 ("token never expires here | guard it") a real

## Decision

Synthetic test fixture — confirmed, should be removed. No production auth.go exists anywhere in the repository. Every occurrence of "auth.go:42 — token never expires here" is inside test fixture helpers: internal/reconcile/emit_test.go:23, internal/reconcile/golden_corpus_test.go:29, reconcile/reconcile_test.go:13. The byte-identical golden corpus baselines (internal/reconcile/testdata/golden/findings.json:25, findings.txt:3) contain it as a fixed synthetic input to Reconcile(), not a production observation. A prior code-review artifact (.planning/epics/code-reviews/2.2_code_review_fanout_hardening/reconciled/td-stream-merged.txt:26) explicitly flagged the identical finding as a reviewer hallucination. ATCR is a code-review tool, not an auth service — there is no token-expiry code to patch. Same pattern and disposition as the previously removed db.go:100 synthetic entry. The row should be deleted from .planning/technical-debt/README.md (group 3, line ~54) with no code change needed.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit_test.go
- internal/reconcile/golden_corpus_test.go
- reconcile/reconcile_test.go
- internal/reconcile/testdata/golden/findings.json
- .planning/technical-debt/README.md
