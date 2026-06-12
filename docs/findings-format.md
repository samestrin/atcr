# Findings Format — `atcr-findings/v1`

The findings stream is atcr's public contract: a pipe-delimited, machine-parseable, versioned text format. It is the integration surface between every producer (the persona pool, the host-model Skill, any third-party tool) and the deterministic reconciler — and between atcr and any downstream consumer.

Two shapes share one grammar: **per-source** (8 columns, written by each reviewer source) and **reconciled** (9 columns, written by `atcr reconcile`).

## Version header

Every findings file MUST begin with this exact line as its first non-blank line:

```
# atcr-findings/v1
```

The parser treats the header as a hard gate:

- **Missing header** → fatal parse error (`missing version header`).
- **Header with an unknown version** (e.g. `# atcr-findings/v2`) → fatal parse error (`unknown findings version`), distinct from "missing" so a consumer never silently parses incompatible data.

## Per-source stream (8 columns)

```
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER
```

Example:

```
# atcr-findings/v1
HIGH|internal/auth/token.go:42|JWT signature not verified before claims are read|Call jwt.Verify before decoding claims|security|20|token, _ := jwt.Parse(raw)|bruce
MEDIUM|internal/store/cache.go:88|Unbounded map grows without eviction|Add an LRU bound|performance|45|c.entries[k] = v // never deleted|greta
```

The trailing `REVIEWER` is a single source name. **Reviewer models do not emit it.** A persona model emits 7 columns (`SEVERITY` … `EVIDENCE`); the engine appends `REVIEWER` from the agent name afterward, so a model can never self-attribute a different reviewer. Any 8th-or-later field a model emits is folded back into `EVIDENCE` rather than landing in the `REVIEWER` slot.

## Reconciled stream (9 columns)

```
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWERS|CONFIDENCE
```

Example:

```
# atcr-findings/v1
HIGH|internal/auth/token.go:42|JWT signature not verified before claims are read|Call jwt.Verify before decoding claims|security|20|bruce: token, _ := jwt.Parse(raw)|bruce,greta|HIGH
MEDIUM|internal/store/cache.go:88|Unbounded map grows without eviction (disagreement: LOW vs MEDIUM)|Add an LRU bound|performance|45|c.entries[k] = v|otto|MEDIUM
```

- **`REVIEWERS`** is the comma-joined set of distinct sources that reported the merged finding. Commas inside a reviewer name are replaced with `/` before joining, so the column can never be forged into extra reviewers.
- **`CONFIDENCE`** is `HIGH` when 2+ distinct reviewers agree, `MEDIUM` for a single reviewer, and `LOW` is reserved for untrusted sources.

## Columns

| # | Column | Meaning | Notes |
|---|--------|---------|-------|
| 1 | `SEVERITY` | One of `CRITICAL`, `HIGH`, `MEDIUM`, `LOW` | Uppercase. The extraction anchor. |
| 2 | `FILE:LINE` | Path and 1-based line | Split on the **last** colon; a non-numeric or missing line parses as `0`, and a path containing a colon is preserved. |
| 3 | `PROBLEM` | What is wrong | On merge: the longest/most-detailed wins; severity disagreements are appended inline. |
| 4 | `FIX` | Suggested remediation | Longest wins on merge. |
| 5 | `CATEGORY` | Free-text tag (`security`, `performance`, `correctness`, …) | Modal value wins on merge. |
| 6 | `EST_MINUTES` | Integer effort estimate | Best-effort; non-numeric parses as `0`. Max wins on merge. |
| 7 | `EVIDENCE` | Supporting snippet or rationale | In reconciled rows, prefixed with the reviewer name. |
| 8 | `REVIEWER` (per-source) / `REVIEWERS` (reconciled) | Source attribution | Single name vs. comma-joined set. |
| 9 | `CONFIDENCE` (reconciled only) | `HIGH` / `MEDIUM` / `LOW` | Reviewer-agreement signal. |

## Parsing rules

- **Extraction is by strict severity-prefix regex:** `^(CRITICAL|HIGH|MEDIUM|LOW)\|`. A line is a finding only if it starts with a valid severity followed immediately by a pipe. Prose that merely mentions "this is HIGH risk" is never mistaken for a row.
- **Comment lines** (starting with `#`) and **blank lines** are skipped.
- **Short rows are padded** to the full column count with empty strings, so a reviewer that omits trailing fields still produces a valid finding.
- **A single trailing pipe** yields an empty final column; trailing empties beyond the expected count are trimmed as padding rather than treated as overflow.
- **Rows with more columns than expected** (an unescaped pipe leaked a field) are recorded as skipped with their line number and reason — never silently misaligned.

## Field escaping

Producers must neutralize characters that would break the one-finding-per-line, pipe-delimited grammar. atcr's writer does this automatically:

- A literal `|` inside any field is replaced with `/`.
- `CR`, `LF`, and `CRLF` inside any field are replaced with a single space, so an embedded newline can never split a finding across physical lines.

Escaping is lossy but structurally stable: the column count and one-row-per-line invariant always hold.

## Source discovery (reconcile inputs)

Any directory under a review's `sources/` that contains a `findings.txt` is a reconcile source — an open extension point: drop `sources/<tool>/findings.txt` from any producer and reconcile picks it up with zero config.

Discovery is **leaf-preference**: a directory's `findings.txt` is an input only when no subdirectory beneath it also contains one. Per-agent raw files (`sources/pool/raw/agent/<name>/findings.txt`) are the pool inputs; the merged `sources/pool/findings.txt` is written for downstream convenience but is **not** re-discovered, so reviewers are never double-counted. `reconciled/` is output, never an input.

## JSON form

`reconciled/findings.json` carries the same records in structured form, plus run metadata, for scripting. A reserved per-finding `verification` block (`{verdict, skeptic, notes}`) is reserved for the adversarial-verification stage (**Epic 3.0**): parsed if present, but never produced by any v1 code path and **absent from 1.x output**. Renderers and readers must tolerate both its absence and its presence — `atcr report` renders identically either way.

## Reserved fields in companion artifacts

The other v1 review artifacts carry reserved fields for the agentic stages on the same "parsed, not yet acted on" basis. Consumers must tolerate their presence and absence:

| Artifact | Field | 1.x value | Reserved for |
|----------|-------|-----------|--------------|
| `manifest.json` | `stages` (array) | `["review"]` — records the one stage that ran | **Epics 3.0–5.0** — later runs append `"verify"`, `"debate"` |
| per-agent `status.json` | `turns`, `tool_calls`, `tool_bytes` | absent — no tool loop ran | **Epic 2.0** — tool-using reviewer loop |

## Evolution policy

The version header is in force from day one. **Evolution is additive-only within a major version:** new optional columns may be appended and new optional JSON fields may be added, but existing column positions, the severity enum, and the extraction regex never change under `v1`. Any breaking change increments the version (`atcr-findings/v2`), and the header gate guarantees old consumers reject it loudly rather than misparsing.
