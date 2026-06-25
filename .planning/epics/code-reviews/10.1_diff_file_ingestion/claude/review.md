# Code Review Stream - 10.1_diff_file_ingestion (Epic)

**Started:** June 25, 2026 09:53:14AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: internal/payload entry turns diff text / diff-file path into same []FileEntry shape as BuildEntries, byte-identical
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/ingest.go:41` (BuildEntriesFromDiff), `internal/payload/ingest.go:71` (BuildEntriesFromDiffFile); parity tests `internal/payload/ingest_test.go:46,76,239`
- **Notes:** Returns []FileEntry (Path/Size/Body). Round-trip identity asserted via joinBodies==input for loose, git-format, and case-01.diff fixture. Per the epic's recorded clarification, parity is round-trip + structural (join==diff, entry count = file count, paths parsed), not byte-equality with git output.

### Criterion: New primitive enforces size cap and rejects path traversal when given a file path
- **Verdict:** VERIFIED ✅
- **Evidence:** Cap at `internal/payload/ingest.go:87-102` (Stat check + io.LimitReader TOCTOU defense, DefaultMaxDiffBytes=10MiB at :17); traversal guard `isSafeDiffPath` at `internal/payload/ingest.go:328-340`; tests `internal/payload/ingest_test.go:133-152`
- **Notes:** Rejects absolute paths and `..` traversal mirroring isSafeRelPath. Size cap enforced via both pre-read Stat and bounded read.

### Criterion: Exported internal/fanout function builds PreparedReview from ingested diff accepted by ExecuteReview unchanged (same modePayload/Slots wiring)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:349` (PrepareReviewFromDiff) shares `finalizePreparedReview` (:229) with PrepareReview; force-mode collapse via buildSlots/buildAgent forceMode param (`:716,725`); end-to-end acceptance test `internal/fanout/ingest_review_test.go:198,222`
- **Notes:** Builds a "diff"-only payloads map and forces every agent to mode "diff", so a blocks/files roster resolves cleanly through the strict payloads[mode] lookup. ExecuteReview accepts the prepared review with no signature/wiring change.

### Criterion: Test feeds case-01.diff through path and asserts entry parity with git-sourced equivalent
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/ingest_test.go:230` (TestBuildEntriesFromDiff_FixtureParity), `internal/fanout/ingest_review_test.go:198` (TestPrepareReviewFromDiff_FixtureEndToEnd)
- **Notes:** Reads internal/benchmark/testdata/suite-valid/case-01.diff, asserts single entry, path "pay.go", verbatim round-trip.

### Criterion: End-to-end test runs ExecuteReview over ingested diff with stub Completer; git-ref builders unmodified and still pass
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/ingest_review_test.go:222` (ExecuteReview with newFake() stub Completer, no network); git-ref builders unchanged — only additive change is buildSlots/buildAgent gaining a forceMode param (resume.go:287, PrepareReview:214 call with "")
- **Notes:** builder.go / BuildEntries / BuildDiff / buildPayloads bytewise unchanged (git diff confirms). Existing test edits are purely the `""` forceMode signature update. ExecuteReview semantics untouched.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (Full hostile review)
**Files Reviewed:** 2 (internal/payload/ingest.go, internal/fanout/review.go)
**Issues Found:** 5 (verified from TD_STREAM)
**Risk Profile:** Loaded (from epic Risks table)

### Risk Verification Summary
- ✅ Anticipated & Addressed: 3 (entry parity → round-trip tests; memory exhaustion → 10 MiB cap; path traversal → isSafeDiffPath)
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 5 (loose-parser robustness on `\ No newline` markers, over-counted hunk headers, double trailing newline, spaced ` b/` paths, allocation churn)

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 1
- Low: 3

All 5 findings empirically confirmed via probe tests against the real parser (probe removed after verification). Every finding is in the new loose-format structural walk (`looseSectionStarts`); the git-format path and the security guards (size cap, path traversal) are robust. None block the epic — all are edge-case hardening on top of a correct, well-tested core.
