# Code Review Stream - 13.5_pagerank-v2-observability (Epic)

**Started:** June 30, 2026 09:42:08AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — reconcile.Summary exposes AuthorityPromoted int with a stable JSON tag
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/reconcile.go:49`
- **Notes:** `AuthorityPromoted int \`json:"authority_promoted"\`` declared in the Summary struct with a doc comment marking it observability-only. JSON tag `authority_promoted` is stable and present in both golden fixtures (`internal/reconcile/testdata/golden/summary.json:12`, `reconcile/adapter/json/testdata/encode_golden.json`).

### Criterion: AC2 — Counter equals number of MEDIUM→HIGH flips by promoteByAuthority (unit-tested, asymmetric multi-reviewer fixture)
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/reconcile.go:111-120,151`; `reconcile/pagerank_confidence_test.go:185-203`
- **Notes:** Second pass compares `base.Confidence == ConfMedium && m.Confidence == ConfHigh` before/after `promoteByAuthority`, incrementing `authorityPromoted` only on an actual flip (robust to predicate evolution). Asserted by `TestReconcile_AuthorityPromotedSummaryCountsFlips` with an asymmetric alpha/beta/gamma fixture (counter == 1) plus an independent `countAuthorityFlips` oracle recount (lines 169-177). Adversarial reviewer confirmed the fixture's two 2-reviewer HIGH findings actively pin the "already-HIGH must not be counted" guard — dropping `base==ConfMedium` would make the counter read 3 and fail the test.

### Criterion: AC3 — All affected golden/snapshot fixtures updated; go test ./... green in reconcile
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/testdata/golden/summary.json:12`; `reconcile/adapter/json/testdata/encode_golden.json`; test run below
- **Notes:** Both serialized-Summary golden fixtures carry `"authority_promoted": 0` in struct order (before `partial`). `reconcile` module (separate go.mod) `go test ./...` PASSING; `reconcile/adapter/json` PASSING. Main module `go test ./...` also fully green.

### Criterion: AC4 — No behavioral change to confidence assignment — only the new observability field added
- **Verdict:** VERIFIED ✅
- **Evidence:** `reconcile/reconcile.go:113-120`; `reconcile/pagerank.go:200-212`; `reconcile/pagerank_confidence_test.go:205-237`
- **Notes:** `base := Merge(g)` is a distinct local; `promoteByAuthority` takes/returns `Merged` by value and mutates only the `Confidence` string, so `base.Confidence` is genuinely pre-promotion and no shared backing array is touched. The count path appends `m` unchanged and feeds the same slice to `sortMerged`. `TestReconcile_AuthorityPromotedZeroWhenNoPromotion` / `...ZeroWhenNoAgreement` pin no-spurious-counting; pre-existing confidence tests remain green.

---

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — epics have no sprint-design.md risk profile)
**Files Reviewed:** 2 (reconcile/reconcile.go, reconcile/pagerank_confidence_test.go)
**Issues Found:** 0 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 0

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 0

**Reviewer conclusion:** Tightly scoped ~10-line observability addition with a provably exact counter, real behavioral neutrality, golden-fixture consistency across both adapters, and tests that would fail under the most plausible wiring mistakes (always-0, count-all-HIGH, drop-the-base-guard). No genuine defects.
