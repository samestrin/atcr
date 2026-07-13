---
id: mem-2026-07-13-d1ac3c
question: "Test-only pinning test is the intended resolution for a coverage-gap TD row whose FIX is \"Add a test\" — diff_smell over_simplified=hard is a false positive (epic 22.3, escaped-quote-in-string case)"
created: 2026-07-13
last_retrieved: ""
sprints: []
files: [.planning/technical-debt/README.md, internal/astgroup/host_test.go, internal/astgroup/parsers/src/pyparser/main.go, .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/claude/review.md]
tags: [clarifications, epic-22.3_pyparser_quote_aware_scanning, testing, go, pyparser, wasm, diff-smell-false-positive]
retrievals: 0
status: active
type: /clarifications --from=resolve-td @.planning/epics/completed/22.3_pyparser_quote_aware_scanning.md (2026-07-13)
---

# Test-only pinning test is the intended resolution for a cove

## Decision

Adding the pinning test IS the intended resolution — the row should flip to resolved. No production-code change is required: the backslash-escape branch at internal/astgroup/parsers/src/pyparser/main.go:201-203 already landed as part of the completed epic 22.3, and the TD row's FIX literally prescribes "Add TestHost_PyParseEscapedQuoteInString..." — a test-only diff is the correct resolution for this coverage-gap item (category: testing), not a reward-hack. The diff_smell gate's over_simplified=hard flag is a false positive here because the gate's test_only smell is designed to catch tests that replace production work, but this row's CATEGORY is "testing" and its entire FIX is a test addition that was RED-verified to pin an existing production branch.

CONVENTION: for a category=testing coverage-gap TD row whose FIX is "Add a test", a test-only diff is the correct and complete resolution; the /resolve-td diff_smell over_simplified=hard gate (which fires on test-only diffs) is a false positive in that case because the test ADDS assertions pinning already-shipped production code rather than replacing production work.

JUSTIFICATION:
- TD row at .planning/technical-debt/README.md:72 — coverage-gap item, Group 1, [ ] open, CATEGORY=testing, FIX="Add TestHost_PyParseEscapedQuoteInString parsing a header whose string holds an escaped quote then a #...asserting the if node has children. Confirm it passes on HEAD and fails if main.go:201-203 is removed." The FIX is exclusively a test addition — no production change requested.
- Production code the test pins already exists at internal/astgroup/parsers/src/pyparser/main.go:201-203 (case '\\': i += 2; continue inside the single-line-string q != 0 state of scanLine). Epic 22.3 is COMPLETED so this branch already shipped — no production change left to make.
- Committed test at internal/astgroup/host_test.go:794 (TestHost_PyParseEscapedQuoteInString, commit eee7a0d8) matches the FIX verbatim: parses if s == "a\"#b": with a nested body (lines 804-805) and asserts require.NotEmpty(t, ifNode.Children, ...) (line 811) and require.True(t, ok, ...) (line 810). Commit message records the RED verification: removing the escape branch (main.go:201-203) and rebuilding python.wasm makes the if node lose its children.
- Epic 22.3 AC (plan lines 21-22) explicitly require regression fixtures — "a regression fixture with a string literal containing # is classified correctly" — so adding regression tests is the prescribed mechanism.
- Code review at .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/claude/review.md:45 classifies all 3 findings as non-blocking follow-ups (2 test-coverage gaps, 1 doc/behavior note) — this row is one of the 2 test-coverage gaps.
- Minor note: the TD row cites host_test.go:718 (where TestHost_PyParseTripleQuoteInsideString ends — the gap was identified at that file position before the test existed), while the actual test landed at line 794. Line-number drift does not affect the conclusion: the FIX explicitly names TestHost_PyParseEscapedQuoteInString, and that test now exists and was RED-verified.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- .planning/technical-debt/README.md
- internal/astgroup/host_test.go
- internal/astgroup/parsers/src/pyparser/main.go
- .planning/epics/code-reviews/22.3_pyparser_quote_aware_scanning/claude/review.md
