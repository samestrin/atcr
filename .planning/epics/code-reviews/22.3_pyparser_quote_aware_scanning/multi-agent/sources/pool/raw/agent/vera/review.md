We need to review changed files. The diff includes many files but the only changed lines are in internal/astgroup/parsers/src/pyparser/main.go and host_test.go. The epic 22.3 changes. We need to find API contract changes that could break callers.

The public API: pyparser is internal to astgroup, but the host and parser functions may be used by other packages. Let's inspect the changes.

In main.go, previously there was significantLines, leadingIndent, bracketDelta, scanTripleQuotes, stripComment. Now they added scanLine, updated scanTripleQuotes to use scanLine, updated stripComment to use scanLine. Did any exported functions change? The functions are not exported (lowercase). But the parser code might be referenced by tests or other packages. The change is internal.

But the main change is that scanLine returns commentAt; but it's not exported. However, the change might affect the semantics of stripComment and scanTripleQuotes. That could break callers that rely on old behavior of stripComment or scanTripleQuotes. But since they are internal, maybe no callers. But tests rely on old behavior; they are updated accordingly.

We need to check if any public API changed: The parser is used via p.Parse, which is exported from Parser. But that's unchanged.

But the change to stripComment might affect how comment removal works. But that's internal.

Potential contract change: The comment removal now ignores # inside string or triple-quoted string. This is a behavioral change that callers might rely on (e.g., structural hash). But that's intended.

But the epic was to fix that. So no break.

Check other changed files: host_test.go added tests, no API changes.

Thus no breaking changes.

But we must examine if any exported functions or types changed. In main.go, no exported functions changed. The only exported function is main, which is not used.

The parse function is exported? parse is not exported. The Parser type is defined elsewhere.

Thus likely no breaking changes.

But the diff added a new function scanLine but not exported. No signature changes.

Thus I think no findings.

But we need to consider the change to scanTripleQuotes signature: previously it returned string; now still returns string. No change.

However, the semantics of stripComment changed: earlier it returned text[:i] where i is index of '#'. Now it uses scanLine, which may return a different commentAt. But that's intended. No breaking.

Thus no findings.

But the prompt says: "Review only the changed regions. The payload shows you the change in context, but a finding whose FILE:LINE falls outside the changed lines will be discarded before it reaches the report when grounding is active (git-range reviews) — it is not enough for the code to merely be visible in the surrounding context. If you must flag a genuine pre-existing issue in unchanged code, give it CATEGORY out-of-scope so the reconciler annotates rather than discards it."

We need to check if any changed lines introduce a breaking change. But the change is internal and not breaking external API.

Thus no findings.

Double-check: Are there any exported types in internal/astgroup/parsers/src/pyparser/main.go? No.

Thus output nothing.

But we must ensure there is no change that breaks other packages that import internal/astgroup/parsers/src/pyparser. That is internal, so not used.

Thus final answer: nothing.