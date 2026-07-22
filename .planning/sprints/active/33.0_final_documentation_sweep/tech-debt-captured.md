# Tech Debt Captured — Sprint 33.0 (during /execute-sprint)

Items deferred or observed during sprint execution. Read by `/execute-code-review` Phase 1 and pre-seeded into the adversarial TD stream (`SOURCE=execute-sprint`).

---

## TD-001 — Reconcile drops host-source reviewer attribution on cross-source cluster merge (LOW)
**Origin:** Phase 1, task 1.1 host-review pass, 2026-07-22
**File:** internal/reconcile/ (merge/cluster reviewer-union path)
**Issue:** In review `33.0-dogfood`, `sources/host/findings.txt` contributed 2 grounded findings (`summary.json` shows `per_source_counts.host: 2`, `sources_scanned: [host, pool]`), and both merged into existing pool clusters (total stayed 25, `clusters_collapsed: 4`). But zero reconciled rows carry `host` in the REVIEWERS column — e.g. `cmd/atcr/root.go:19` remained `brad,dax` though the host reported the same issue at `root.go:20` (within the ±3 cluster window). The host's independent corroboration is therefore not reflected in REVIEWERS/CONFIDENCE. NOT YET CONFIRMED as a bug vs. expected consensus-filter behavior (`consensus_filtered: 8`) — verify before fixing.
**Why accepted:** Phase 1 is review-only; the host contribution is preserved verbatim in `sources/host/findings.txt` + `review.md`, which is Task 3's canonical handoff, so triage is unaffected. Root-causing atcr's reconcile internals is out of scope for the review-production phase.
**Fix in:** Task 3 triage (verify) or a follow-up TD sprint — confirm whether a lower-severity host finding merging into a higher-severity pool cluster should union its reviewer into REVIEWERS (and thus lift CONFIDENCE), add a reconcile test covering cross-source reviewer-union, and fix if confirmed.

---

## TD-002 — Pool parser scrapes findings out of fenced example/fixture blocks in agent prose reviews (MEDIUM)
**Origin:** Phase 1, task 1.1.A adversarial review, 2026-07-22
**File:** internal/fanout/ (pool findings parser) + internal/reconcile/testdata/golden/findings.txt (the scraped fixture)
**Issue:** In review `33.0-dogfood`, the `archer` agent's prose review quoted a fenced example block mirroring `internal/reconcile/testdata/golden/findings.txt` (which lists example rows for db.go/auth.go/legacy.go/pay.go/util.go — none exist in the repo). The pool findings parser scraped `legacy.go:7/preexisting smell outside the diff` out of that fenced example block and emitted it as a real finding. Result: a hallucinated finding citing a nonexistent file was carried into reconciled output as a HIGH-severity row, inflating the reported HIGH count to 4 (astgroup logged `open legacy.go: no such file or directory`). Only partially mitigated by a `file not found` note; still counted and mis-severitied.
**Why accepted:** Phase 1 is review-only; the hallucination is grounded-out by the host review.md and this capture, so Task 3 triage will not act on `legacy.go`. Fixing the parser is out of scope for the review-production phase.
**Fix in:** Follow-up TD sprint (atcr engine) — make the pool parser ignore fenced code/example blocks inside an agent's prose `review.md`, and/or drop findings whose cited file does not exist in the head tree (demote `file not found` out of the counted/HIGH set). Add a parser test with a fenced example block.

## TD-003 — Reconcile does not collapse byte-identical / same-FILE:LINE duplicate findings (MEDIUM)
**Origin:** Phase 1, task 1.1.A adversarial review, 2026-07-22
**File:** internal/reconcile/ (cluster/dedupe path)
**Issue:** In review `33.0-dogfood`, the bogus `legacy.go:7` row appears TWICE, byte-identical, in `reconciled/findings.txt` (and twice in `report.md`). reconcile's cluster-by-FILE:LINE ±3 dedup did not collapse the identical pair, inflating `total_findings` to 25 and the report's HIGH count. A byte-identical duplicate at the same FILE:LINE should always collapse regardless of the out-of-scope/file-not-found path.
**Why accepted:** Phase 1 is review-only; the duplicate is cosmetic to triage (both rows are the same rejected hallucination). Root-causing reconcile clustering is out of scope for the review-production phase.
**Fix in:** Follow-up TD sprint (atcr engine) — collapse byte-identical / same-FILE:LINE findings during reconcile clustering before writing findings.txt, findings.json, and report.md. Add a reconcile test with a byte-identical duplicate pair. Likely related to [[TD-001]] (same cluster/merge path).
