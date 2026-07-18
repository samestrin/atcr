# TOON Format Reference (Token Optimized Object Notation)

**Priority:** Reference

## Overview

The epic proposes the axi payload as "a TOON (Token Optimized Object Notation) or compact JSON formatter" (original-requirements.md: Proposed Solution), and plan.md commits to a "token-dense format implementation" inside `internal/report`. The "or compact JSON" hedge is narrowed by the epic's own reference source: [axi.md](https://axi.md) mandates TOON by name as its Principle 1 — "Use TOON (Token-Optimized Object Notation) format instead of JSON or tab-separated tables," claiming ~40% token savings over equivalent JSON (see [axi-design-principles.md](axi-design-principles.md)). A non-TOON encoding is therefore a deviation the sprint design must explicitly justify, not a coequal option. TOON itself is a real, specification-driven format — not an epic invention: [toonformat.dev](https://toonformat.dev/) advertises "spec-driven implementations in TypeScript, Python, Go, Rust, .NET, and other languages," and the [syntax cheatsheet](https://toonformat.dev/reference/syntax-cheatsheet.html) provides the JSON→TOON mapping excerpted below. (The advertised **Go** implementation should be evaluated during `/design-sprint` against plan.md's current "hand-rolled formatters over third-party dependencies" stance — that decision is surfaced here, not made here.)

Two properties make TOON specifically relevant to atcr rather than generically interesting. First, its **tabular array** encoding (`key[N]{field1,field2}:` followed by delimiter-separated rows) is designed for exactly the findings shape — a uniform list of records with fixed columns — which is the axi renderer's primary payload. Second, TOON supports **alternative delimiters declared in the array header**, including **pipe** (`key[N|]{field|name}:`), so a TOON-encoded findings list can look structurally very close to the existing pipe-delimited `atcr-findings/v1` grammar (see [agentic-format-precedents.md](agentic-format-precedents.md)) — the strongest available answer to plan.md's risk of "fragmenting the machine-format surface."

The tension to resolve in design is escaping: TOON neutralizes problem characters with **selective quoting** (quote only strings that need it) and a fixed set of five escape sequences, whereas `atcr-findings/v1` uses **lossy field neutralization** (`|`→`/`, CR/LF→space, never quoted). Findings `PROBLEM`/`FIX`/`EVIDENCE` text routinely contains commas, colons, pipes, and (rarely) newlines, so the choice between TOON quoting and v1-style lossy escaping materially changes what a consuming agent sees. This document records the normative rules; the choice itself belongs to the sprint design.

## Key Concepts

### Objects and nesting

Plain JSON objects map to YAML-like `key: value` lines; nested objects indent one level.

> Source: [syntax-cheatsheet.html: Objects, Nested Objects](https://toonformat.dev/reference/syntax-cheatsheet.html)

### Arrays carry their length in the header — `key[N]:`

Every array header declares the element count: `tags[3]: foo,bar,baz` for primitive arrays, `key[N]{field1,field2,field3}:` for tabular arrays of uniform objects. Non-uniform (mixed) arrays fall back to hyphen-list items, and arrays of arrays use `- [N]:` item lines. A root-level array is just `[N]: ...`. For the axi payload this means a findings list is self-describing: the consumer knows the row count before reading rows, which composes cleanly with the plan's `truncated` flag (a capped payload can state the true `N` and mark truncation explicitly).

> Source: [syntax-cheatsheet.html: Primitive Arrays, Tabular Arrays, Mixed/Non-Uniform Arrays, Arrays of Arrays, Root Arrays, Array Headers](https://toonformat.dev/reference/syntax-cheatsheet.html)

### Tabular arrays are the findings case

Uniform object arrays encode as a column header plus one row per record — structurally the same idea as the reconciled `atcr-findings/v1` stream (fixed column set, one finding per line). This is the canonical TOON form an axi findings payload would use.

> Source: [syntax-cheatsheet.html: Tabular Arrays](https://toonformat.dev/reference/syntax-cheatsheet.html)

### Alternative delimiters, including pipe

The delimiter symbol is declared inside the brackets/braces of the header: `items[2|]{id|name}:` selects pipe-delimited rows and a pipe-separated field list (tab is the other alternative). With the pipe delimiter, a TOON tabular findings array is visually and structurally adjacent to the existing `SEVERITY|FILE:LINE|...` grammar — the convergence noted in the overview.

> Source: [syntax-cheatsheet.html: Array Headers → Alternative Delimiters](https://toonformat.dev/reference/syntax-cheatsheet.html)

### Quoting rules (must-quote conditions)

Strings **must** be quoted if they: are empty (`""`); have leading/trailing whitespace; equal `true`/`false`/`null` (case-sensitive); look like numbers (`"42"`, `"-3.14"`, `"1e-6"`, `"05"`); contain special characters (`:`, `"`, `\`, `[`, `]`, `{`, `}`, newline, tab, carriage return); contain the active delimiter (comma by default, or tab/pipe if declared); or equal/start with `-`. Otherwise strings may be unquoted — "Unicode and emoji are safe."

Only five escape sequences are valid inside quoted strings: `\\`, `\"`, `\n`, `\r`, `\t`. All other escapes (`\x`, `\u`) are invalid — so arbitrary control characters cannot be smuggled through a TOON payload, which aligns with the axi no-ANSI guarantee (see [agentic-format-precedents.md](agentic-format-precedents.md): Control-character sanitization idiom).

> Source: [syntax-cheatsheet.html: Quoting Special Cases, Quoting Rules Summary, Escape Sequences](https://toonformat.dev/reference/syntax-cheatsheet.html)

### Empty containers and type conversions

An empty object encodes as empty output; an empty array as `key[0]:`. Type conversions: finite numbers render as canonical decimal (no exponent, no trailing zeros); `NaN`/`±Infinity` become `null`; safe-range BigInts become numbers; Dates become quoted ISO strings; `undefined`/functions/symbols become `null`. (The cheatsheet's conversion table is TypeScript-oriented; a Go encoder would define equivalent mappings — a design-phase decision.)

> Source: [syntax-cheatsheet.html: Empty Containers, Type Conversions](https://toonformat.dev/reference/syntax-cheatsheet.html)

### Key folding (optional)

With `keyFolding: 'safe'`, chains of single-key nested objects collapse into dotted paths (`data.metadata.items[2]: a,b`). Optional and off by default; relevant only if the axi payload nests metadata deeply (e.g., run metadata around the findings list).

> Source: [syntax-cheatsheet.html: Key Folding (Optional)](https://toonformat.dev/reference/syntax-cheatsheet.html)

### Beyond the cheatsheet

The cheatsheet is explicitly a quick reference: "For rigorous, normative syntax rules and edge cases, see the Specification" — linked from the same page. If the design commits to TOON (rather than compact JSON), the full specification on [toonformat.dev](https://toonformat.dev/) becomes the normative source and should be re-fetched at that point.

## Code Examples

The following are verbatim from the source document.

### Tabular arrays (the findings shape)

> Source: [syntax-cheatsheet.html: Tabular Arrays](https://toonformat.dev/reference/syntax-cheatsheet.html)

JSON:

```json
{
  "items": [
    { "id": 1, "qty": 5 },
    { "id": 2, "qty": 3 }
  ]
}
```

TOON:

```
items[2]{id,qty}:
  1,5
  2,3
```

### Pipe-delimited tabular header

> Source: [syntax-cheatsheet.html: Array Headers → Alternative Delimiters](https://toonformat.dev/reference/syntax-cheatsheet.html)

```
items[2|]{id|name}:
  1|Alice
  2|Bob
```

### Strings that must be quoted

> Source: [syntax-cheatsheet.html: Quoting Special Cases](https://toonformat.dev/reference/syntax-cheatsheet.html)

```json
{
  "version": "123",
  "enabled": "true"
}
```

```
version: "123"
enabled: "true"
```

### Escape sequences

> Source: [syntax-cheatsheet.html: Escape Sequences](https://toonformat.dev/reference/syntax-cheatsheet.html)

| Character | Escape |
| --- | --- |
| Backslash (`\`) | `\\` |
| Double quote (`"`) | `\"` |
| Newline | `\n` |
| Carriage return | `\r` |
| Tab | `\t` |

All other escapes (e.g., `\x`, `\u`) are invalid.

## Quick Reference

| TOON feature | Cheatsheet section | Relevance to the axi payload |
|---|---|---|
| Tabular arrays `key[N]{f1,f2}:` | Tabular Arrays | Canonical encoding for the findings list — uniform records, fixed columns |
| Declared length `N` in every array header | Array Headers | Self-describing row count; composes with the `truncated` flag for capped payloads |
| Pipe/tab alternative delimiters | Array Headers → Alternative Delimiters | Pipe-delimited TOON converges with the `atcr-findings/v1` pipe grammar — the anti-fragmentation option |
| Must-quote conditions | Quoting Rules Summary | Findings text with commas/colons/pipes gets quoted, not munged — contrast with v1's lossy `|`→`/` neutralization (design decision) |
| Five valid escapes only (`\\ \" \n \r \t`) | Escape Sequences | No `\x`/`\u` escapes → control sequences can't ride the payload; supports the no-ANSI guarantee |
| Empty containers (`key[0]:`, empty output) | Empty Containers | Zero-findings reviews still emit a well-formed payload |
| Key folding (optional) | Key Folding | Only if run metadata nests deeply; off by default |
| Type conversions | Type Conversions | TS-oriented table; Go-encoder equivalents are a design-phase decision |
| Multi-language implementations (incl. Go) | [toonformat.dev](https://toonformat.dev/) | Evaluate the Go implementation vs. plan.md's hand-rolled-formatter stance during `/design-sprint` |

## Related Documentation

- Plan: [../plan.md](../plan.md)
- [TOON — toonformat.dev](https://toonformat.dev/)
- [TOON Syntax Cheatsheet](https://toonformat.dev/reference/syntax-cheatsheet.html) (links the normative Specification)
- [Existing Agent-Facing Format & Output-Safety Contracts](agentic-format-precedents.md) — the `atcr-findings/v1` precedent any TOON schema must be reconciled with
