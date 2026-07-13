# atcr Reconciled Review

## Summary

- Total findings: 10
- Sources: pool
- Clusters collapsed: 2
- Severity disagreements: 2
- Consensus filtered: 4 (uncorroborated singletons routed to the ambiguous sidecar)
- Out-of-scope findings: 3 (annotated, excluded from the gate)
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 4 | 0 |
| HIGH | 2 | 1 | 0 |
| MEDIUM | 0 | 0 | 0 |
| LOW | 0 | 0 | 0 |

## Disagreements

Top 11 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/payload/rangebuilder.go:31` (CRITICAL) · score 4
- Reviewers: archer (independence 1)
- Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;

### 2. solo_finding — `internal/payload/rangebuilder.go:31` (CRITICAL) · score 4
- Reviewers: archer (independence 1)
- Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;

### 3. solo_finding — `internal/payload/rangebuilder.go:31` (CRITICAL) · score 4
- Reviewers: archer (independence 1)
- Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;

### 4. solo_finding — `internal/payload/rangebuilder.go:31` (CRITICAL) · score 4
- Reviewers: archer (independence 1)
- Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;

### 5. solo_finding — `internal/fanout/resume.go:260` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: No test verifies that PrepareResume grounding step reuses the RangeBuilder and avoids extra git subprocesses (acceptance criteria)

### 6. severity_split — `internal/payload/rangebuilder_test.go:1` (HIGH) · score 2
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: archer, dax (independence 2)
- Problem: No test covers BuildEntries or BuildChangedLines error propagation from git failures (e.g., non-repo directory)

### 7. severity_split — `internal/payload/rangebuilder_test.go:1` (HIGH) · score 2
- Severity disagreement: MEDIUM vs HIGH
- Reviewers: archer, dax (independence 2)
- Problem: No test exercises BuildEntries with ModeDiff or ModeBlocks; only ModeFiles is tested

### 8. gray_zone — `internal/payload/builder.go:102` (MEDIUM) · score 2
- Reviewers: otto (independence 1)
- Problem: &#96;buildEntriesValidated&#96; is a &#34;leaky&#34; internal method
- Detail: similarity 0.00
- Positions:
  - otto — MEDIUM: &#96;buildEntriesValidated&#96; is a &#34;leaky&#34; internal method

### 9. gray_zone — `internal/payload/diff.go:474` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: Modified rangeChunks error path (via zeroCtxChunks) not tested
- Detail: similarity 0.00
- Positions:
  - dax — LOW: Modified rangeChunks error path (via zeroCtxChunks) not tested

### 10. gray_zone — `internal/payload/rangebuilder.go:24` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: &#96;RangeBuilder&#96; is not safe for concurrent use but doesn&#39;t enforce it
- Detail: similarity 0.00
- Positions:
  - otto — LOW: &#96;RangeBuilder&#96; is not safe for concurrent use but doesn&#39;t enforce it

### 11. gray_zone — `internal/payload/rangebuilder_test.go:36` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: Test assertion relies on execCount field accuracy; test could pass vacuously if counter misses zero-context diff
- Detail: similarity 0.00
- Positions:
  - dax — LOW: Test assertion relies on execCount field accuracy; test could pass vacuously if counter misses zero-context diff

## Findings

### CRITICAL

- `internal/payload/rangebuilder.go:31` — confidence MEDIUM, reviewers: archer
  - Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;
  - Fix: Change &#96;b.base&#96; to &#96;base&#96;
  - Evidence: base: b.base,
- `internal/payload/rangebuilder.go:31` — confidence MEDIUM, reviewers: archer
  - Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;
  - Fix: Change &#96;b.base&#96; to &#96;base&#96;
  - Evidence: base: b.base,
- `internal/payload/rangebuilder.go:31` — confidence MEDIUM, reviewers: archer
  - Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;
  - Fix: Change &#96;b.base&#96; to &#96;base&#96;
  - Evidence: base: b.base,
- `internal/payload/rangebuilder.go:31` — confidence MEDIUM, reviewers: archer
  - Problem: (NewRangeBuilder) Undefined variable &#96;b&#96; used for struct field initialization; should be &#96;base&#96;
  - Fix: Change &#96;b.base&#96; to &#96;base&#96;
  - Evidence: base: b.base,

### HIGH

- `internal/fanout/resume.go:260` — confidence MEDIUM, reviewers: dax
  - Problem: No test verifies that PrepareResume grounding step reuses the RangeBuilder and avoids extra git subprocesses (acceptance criteria)
  - Fix: Add a subprocess-count assertion in the resume integration test to confirm the reduction
  - Evidence: changed, groundingDisabledReason := computeGroundingData(ctx, req, rb) at line 260; no corresponding assertion in the diff
- `internal/payload/rangebuilder_test.go:1` — confidence HIGH, reviewers: archer, dax
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: No test covers BuildEntries or BuildChangedLines error propagation from git failures (e.g., non-repo directory)
  - Fix: Add a test with a non-git directory to verify errors surface from underlying git operations
  - Evidence: [dax] TestRangeBuilder_InvalidRefError only tests validation errors; post-validation git errors untested / [archer] rb := NewRangeBuilder(context.Background(), &#34;deadbeefdeadbeef&#34;, head)
- `internal/payload/rangebuilder_test.go:1` — confidence HIGH, reviewers: archer, dax
  - Severity disagreement: MEDIUM vs HIGH
  - Problem: No test exercises BuildEntries with ModeDiff or ModeBlocks; only ModeFiles is tested
  - Fix: Add test cases for ModeDiff and ModeBlocks to ensure RangeBuilder wiring works for all valid modes
  - Evidence: [dax] TestRangeBuilder_GroundingReusesPayloadGitProcesses uses ModeFiles exclusively; other modes not covered

## Out-of-Scope Findings

Pre-existing issues outside the reviewed change — annotated for the record, excluded from the severity gate.

### HIGH

- `.planning/epics/superseded/23.0_human_persona_renaming.md:51` — confidence MEDIUM, reviewers: archer
  - Problem: Unresolved git merge conflict markers present in committed file
  - Fix: Remove &#96;&lt;&lt;&lt;&lt;&lt;&lt;&lt; HEAD&#96;, &#96;========&#96;, and &#96;&gt;&gt;&gt;&gt;&gt;&gt;&gt;&#96; blocks
  - Evidence: &lt;&lt;&lt;&lt;&lt;&lt;&lt; HEAD:.planning/epics/completed/23.0_human_persona_renaming.md...
- `.planning/epics/superseded/23.0_human_persona_renaming.md:51` — confidence MEDIUM, reviewers: archer
  - Problem: Unresolved git merge conflict markers present in committed file
  - Fix: Remove &#96;&lt;&lt;&lt;&lt;&lt;&lt;&lt; HEAD&#96;, &#96;========&#96;, and &#96;&gt;&gt;&gt;&gt;&gt;&gt;&gt;&#96; blocks
  - Evidence: &lt;&lt;&lt;&lt;&lt;&lt;&lt; HEAD:.planning/epics/completed/23.0_human_persona_renaming.md...
- `.planning/epics/superseded/23.0_human_persona_renaming.md:51` — confidence MEDIUM, reviewers: archer
  - Problem: Unresolved git merge conflict markers remain in the file
  - Fix: Remove &#96;&lt;&lt;&lt;&lt;&lt;&lt;&lt;&#96;, &#96;========&#96;, and &#96;&gt;&gt;&gt;&gt;&gt;&gt;&gt;&#96; blocks
  - Evidence: &lt;&lt;&lt;&lt;&lt;&lt;&lt; HEAD:.planning/epics/completed/23.0_human_persona_renaming.md...
