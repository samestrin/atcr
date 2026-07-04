# Symbol-anchored Problem cells (`(symbolName)` prefix)

When atcr's reconciler emits a finding, it prepends a **stable symbol anchor**
to the finding's `problem` text: the name of the nearest enclosing named code
block (a function, method, class, or type), wrapped in parentheses. The
`problem` field then reads, for example:

```
(classifyHeader) Off-by-one when the header row is absent
```

This page documents that contract so external consumers — most importantly a
`resolve-td`-style skill that fixes captured technical debt — can rely on it.
It is a superset of the `problem` column defined in
[findings-format.md](findings-format.md); the anchor is content *inside* that
column, not a new column, so every legacy parser of the flat
[technical-debt](technical-debt.md) table keeps working unchanged.

## Why

During a technical-debt resolution session, editing one finding's code shifts
the line numbers of every finding below it in the same file. A downstream item
that cited `file.go:210` now points at the wrong line. A resolver that greps for
a **stable identifier** — the enclosing block's name — relocates the code
regardless of drift, where a line number alone cannot. The anchor gives the
resolver that identifier deterministically, instead of asking a model to guess a
relocation key from prose.

## Format contract

- **Placement.** The anchor occupies position 0 of the `problem` cell: a literal
  `(`, the symbol name, a literal `)`, and a single trailing space, immediately
  followed by the original problem text.
- **Symbol.** The name is the nearest **named** block enclosing the finding's
  line, resolved by walking the AST covering chain from the deepest covering
  block up toward the file root and taking the first block with a name. Unnamed
  control-flow blocks (`if` / `for` / `while` / `switch` / …) are skipped, so a
  finding inside an `if` inside `classifyHeader` anchors to `classifyHeader`, not
  the anonymous `if`.
- **Graceful degradation (no anchor).** The prefix is **omitted entirely** — the
  `problem` cell is byte-identical to the un-anchored text — when any of the
  following holds:
  - the finding's language has no AST parser (only brace-based and Python
    languages are supported; see [findings-format.md](findings-format.md) and
    the parser registry);
  - the finding is file-level (no line, or line ≤ 0) or its file is
    absent/unparseable;
  - no **named** block encloses the line (e.g. a finding between top-level
    declarations);
  - AST grouping is disabled (`ATCR_DISABLE_AST_GROUPING`).
- **Table safety.** A resolved name is used only if it is safe for the
  pipe-delimited Markdown table and the `(symbol)` parse. Names containing `|`,
  `(`, `)`, or any whitespace/control character are treated as unresolvable and
  the anchor is omitted. A bare identifier always qualifies; exotic names such as
  C++ `operator()` do not, and that finding is simply left un-anchored.
- **Not a grouping key.** The anchor is a human- and grep-facing display value.
  Finding clustering uses a separate structural key, so duplicate symbol names
  within a file are harmless for reporting; a resolver that finds an ambiguous
  name should fall back to proximity around the cited line.

## Consuming the anchor

A resolver extracts the anchor by matching a leading parenthesized group at the
very start of the `problem` cell:

```
^\(([^)]+)\)\s+
```

- **Capture group 1** is the `RELOCATE_KEY`: grep the cited file for it to find
  the (possibly drifted) block, then apply the fix within that block.
- **No match** means the finding was not anchored (see graceful degradation
  above); fall back to the cited line number / keyword relocation as before.
- The remainder of the cell (after the matched prefix) is the original problem
  text.

Because the anchor is optional and always leading, a consumer that does not
understand it can ignore the pattern and read the whole cell as the problem
description with no loss beyond the relocation hint.

## Provenance

The anchor is stamped in the atcr reconciler (`internal/reconcile`) after merge
and path validation and before findings are emitted, using the AST parsers in
`internal/astgroup`. atcr does **not** write the flat `technical-debt` table
itself; the anchor travels in the `problem` field into whatever store the caller
builds downstream (see [code-review-backend.md](code-review-backend.md) for the
backend-integration contract).

Within atcr itself the stamp lands once on the shared `JSONFinding.Problem`
field before Emit, so every in-repo consumer that reads that field renders the
`(symbolName)` anchor verbatim — not only the `technical-debt` table but also
the GitHub Action PR comment (`internal/ghaction/comments.go`), the check-run
summary table (`internal/ghaction/render.go`), and the human-readable report
(`internal/report/render.go`). This is intentional: confining the anchor to a
TD-only surface would require a schema change the emit-layer injection point
(epic 18.1 Q2) deliberately avoids.
