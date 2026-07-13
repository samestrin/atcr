---
id: mem-2026-07-13-b74fac
question: "Test-only pinning test is the intended resolution for a coverage-gap TD row whose FIX is \"Add a test\" — diff_smell over_simplified=hard is a false positive (epic 22.3, multi-line docstring case)"
created: 2026-07-13
last_retrieved: ""
sprints: []
files: [.planning/technical-debt/README.md, internal/astgroup/host_test.go, internal/astgroup/parsers/src/pyparser/main.go, .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/claude/review.md, .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/multi-agent/reconciled/report.md]
tags: [clarifications, epic-22.3_pyparser_quote_aware_scanning, testing, go, pyparser, wasm, diff-smell-false-positive]
retrievals: 0
status: active
type: /clarifications --from=resolve-td @.planning/epics/completed/22.3_pyparser_quote_aware_scanning.md (2026-07-13)
---

# Test-only pinning test is the intended resolution for a cove

## Decision

Adding the pinning test is the intended resolution — the row should flip to resolved. No production-code change is required: the production code this test pins (scanLine's triple-quote opening at internal/astgroup/parsers/src/pyparser/main.go:210-219 and the startInString skip path at main.go:84-88) already landed as part of the completed epic 22.3. This TD row is a coverage-gap item (category: testing) whose FIX literally says "Add a test...", and the committed TestHost_PyParseMultiLineDocstringSkipsContent matches that FIX 1:1, including the RED-verification step. The diff_smell gate's over_simplified=hard flag is a false positive for a coverage-gap row where a test-only diff is the prescribed resolution, not a reward-hack.

CONVENTION: for a category=testing coverage-gap TD row whose FIX is "Add a test", a test-only diff is the correct and complete resolution; the /resolve-td diff_smell over_simplified=hard gate (which fires on test-only diffs) is a false positive in that case because the test ADDS assertions pinning already-shipped production code rather than replacing production work.

JUSTIFICATION:
- TD row at .planning/technical-debt/README.md:71 — category=testing, [ ] open, FIX="Add a test parsing a func whose multi-line docstring body contains def fake(): and a # comment line, asserting collectFuncNames excludes 'fake' and includes the real following def. Verify it fails if delim-opening (main.go:210-219) is stubbed out."
- Committed test at internal/astgroup/host_test.go:756-783 matches the FIX exactly (docstring body has def fake(): at line 768 and # not real at 769; asserts fake excluded at 782, a/b included at 780-781).
- Production code already exists and is unchanged: scanLine triple-quote opening at internal/astgroup/parsers/src/pyparser/main.go:210-219, startInString skip at main.go:84-88, scanTripleQuotes delegating to scanLine at main.go:235-238.
- Epic 22.3 Acceptance Criteria explicitly require regression fixtures; the epic is COMPLETED (all AC checked [x]) so the production-code change already landed — this row is the leftover coverage gap surfaced by the post-completion review.
- Review artifacts confirm non-blocking test-coverage follow-ups: .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/claude/review.md:45, .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/multi-agent/reconciled/report.md:32.
- RED-verification (stubbing main.go:210-219 makes fake get collected, failing the test) confirms the test genuinely pins the production guard — not a no-op or weakened-assertion reward-hack. The diff ADDS assertions (require.Contains / require.NotContains) pinning existing production code.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/astgroup/host_test.go
- internal/astgroup/parsers/src/pyparser/main.go
- .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/claude/review.md
- .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/multi-agent/reconciled/report.md
