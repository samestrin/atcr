# Findings Format — `atcr-findings/v1`

The findings stream is atcr's public contract: a pipe-delimited, machine-parseable, versioned text format. It is the integration surface between every producer (the persona pool, the host-model Skill, any third-party tool) and the deterministic reconciler — and between atcr and any downstream consumer.

Two shapes share one grammar: **per-source** (8 columns, written by each reviewer source) and **reconciled** (9 columns, written by `atcr reconcile`).

A second, lossless per-source format, `# atcr-findings/v2` (`findings.toon`), is written beside every per-source `findings.txt`. It is described in [v2 lossless stream](#v2-lossless-stream-findingstoon) at the end of this document; everything before that section is the v1 contract.

## Version header

Every findings file MUST begin with this exact line as its first non-blank line:

```
# atcr-findings/v1
```

The parser treats the header as a hard gate:

- **Missing header** → fatal parse error (`missing version header`).
- **Header with an unknown version** (e.g. `# atcr-findings/v3`) → fatal parse error (`unknown findings version`), distinct from "missing" so a consumer never silently parses incompatible data. atcr's per-source reader also accepts `# atcr-findings/v2` (see [v2 lossless stream](#v2-lossless-stream-findingstoon)); its reconciled reader accepts only v1.

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
- **`CONFIDENCE`** is `HIGH` when 2+ distinct reviewers agree and `MEDIUM` for a single reviewer, with `LOW` reserved for untrusted sources. As a refinement, an isolated (single-reviewer) finding is promoted to `HIGH` when its model's per-run PageRank *authority* — earned by agreeing with other models elsewhere in the run — exceeds the uniform `1/N` baseline. Promotion is one-directional: authority never lowers a finding's confidence. When no cross-model agreement exists in a run the signal is inert and confidence is exactly the reviewer-count result.

## Columns

| # | Column | Meaning | Notes |
|---|--------|---------|-------|
| 1 | `SEVERITY` | One of `CRITICAL`, `HIGH`, `MEDIUM`, `LOW` | Uppercase. The extraction anchor. |
| 2 | `FILE:LINE` | Path and 1-based line | Split on the **last** colon; a non-numeric or missing line parses as `0`, and a path containing a colon is preserved. |
| 3 | `PROBLEM` | What is wrong | On merge: the longest/most-detailed wins; severity disagreements are appended inline. |
| 4 | `FIX` | Suggested remediation | Longest wins on merge. |
| 5 | `CATEGORY` | One lowercase word from the **closed reviewer vocabulary** (`security`, `performance`, `correctness`, `maintainability`, …) | The vocabulary is rendered into every reviewer prompt through `{{.ScopeRule}}`; its authority is `reconcile.Categories()`, never a persona file. **Not enforced at parse time** — a word outside the vocabulary is accepted as written, so drift shows up as a lower score rather than an error (the benchmark surfaces it as `out_of_vocabulary_rate`). Modal value wins on merge. The full member list is in [CATEGORY vocabulary](#category-vocabulary). |
| 6 | `EST_MINUTES` | Integer effort estimate | Best-effort; non-numeric parses as `0`. Max wins on merge. Also consumed as a routing input by the executor's complexity ceiling (`max_estimated_minutes`) — see [registry.md](registry.md#executor-fix-generation-active-in-70). |
| 7 | `EVIDENCE` | Supporting snippet or rationale | In reconciled rows, prefixed with the reviewer name. |
| 8 | `REVIEWER` (per-source) / `REVIEWERS` (reconciled) | Source attribution | Single name vs. comma-joined set. |
| 9 | `CONFIDENCE` (reconciled only) | `HIGH` / `MEDIUM` / `LOW` | Reviewer-agreement signal, refined by per-run PageRank authority (an isolated finding from an above-`1/N`-authority model is promoted to `HIGH`; never demoted). |

## CATEGORY vocabulary

`CATEGORY` takes one lowercase word from this closed set. The set is rendered into
every reviewer prompt through `{{.ScopeRule}}`, and its authority is
`reconcile.Categories()` (`reconcile/category.go`; `out-of-scope` is declared in
`reconcile/merge.go`). The table below is
hand-maintained in that constant's **offer order** — the order the prompt uses — and
pinned to it by `internal/reconcile/findings_format_taxonomy_test.go`. That guard
reads `reconcile/category.go` **from source in this tree**, so it resolves the same
vocabulary whether the build uses a local `go.work` or `GOWORK=off` (what CI sets):
adding, removing, or reordering a member without updating the table fails the suite
immediately, in the same pull request that made the change.

`reconcile` is a separate published module, so the vocabulary atcr actually renders
into prompts is the one in the `go.mod` pin, which can briefly lag this tree. That
gap is reported rather than enforced — the release-lag guard in the same file skips
with `release reconcile and bump the pin` — because failing on it would only move
the release deadlock one step, not remove it.

The last two rows are **routing values**, not defect classes. `out-of-scope` is a
reserved control token: its findings are annotated rather than promoted — kept in the
artifacts, counted in summaries, listed in their own report section, and excluded from
a severity gate. `other` is the escape hatch that makes the set closed rather than
lossy: a real finding that genuinely fits no member above. Neither says anything about
what is wrong with the code.

**What the guard checks in this table:** the `Category` column and its order, and the
partition the `Group` column describes — categories declared in one comment-marked
block of `reconcile/category.go` must share a `Group` cell, and separate blocks must
not. The `Group` wording and the whole **`What it labels` column are hand-maintained
prose, not machine-checked**: pinning either would mean exporting the glosses from a
module external tools embed. Treat them as a reader's aid that can lag
`category.go`'s comments, and fix them by hand when the constant's wording changes.

| Category | Group | What it labels |
|----------|-------|----------------|
| `correctness` | Defect class | The code produces a wrong result: off-by-one, inverted condition, unreachable branch. |
| `logic` | Defect class | Accepted equivalent spelling of `correctness`; a member because a shipped persona's worked example emits it. |
| `security` | Defect class | Injection, auth bypass, traversal — every vulnerability class except credential exposure. |
| `secret` | Defect class | An exposed credential specifically: hardcoded key, secret in a log, weak secret handling. |
| `performance` | Defect class | Hot-path cost: N+1 calls, needless allocation, accidental O(n^2). |
| `concurrency` | Defect class | Synchronization and lifecycle misuse: lock discipline, channel/WaitGroup hazards, goroutines with no exit path. |
| `race` | Defect class | A specific unsynchronized access to specific shared state, including check-then-act (TOCTOU). |
| `error-handling` | Defect class | Swallowed or ignored errors, missing timeouts, unbounded retries, partial-failure states. |
| `state` | Defect class | Stale caches, mutation of shared data, ordering assumptions. |
| `invariant` | Defect class | A property the code assumes but never establishes or enforces. |
| `type` | Defect class | Type-safety gaps: over-broad any/dynamic typing, unchecked assertions, lossy conversion. |
| `api-contract` | Contract and interface | The published interface itself is wrong: signature, error type, or ownership semantics callers must code around. |
| `contract` | Contract and interface | This change violates an existing contract — the function does not honour its own name, docs, or signature. |
| `validation` | Contract and interface | A validation rule that is missing, too weak, or applied on the wrong side of the boundary. |
| `input-validation` | Contract and interface | Untrusted input reaching logic unchecked — the trust-boundary subset of `validation`. |
| `resource-leak` | Resource and dependency | An acquired resource with no release path: unclosed handle, connection churn, unbounded cache. |
| `leak` | Resource and dependency | A leak of something other than a held resource: memory retained by a live reference, a leaked secret or internal detail. |
| `dependency` | Resource and dependency | A dependency that is unnecessary, unpinned, misused, or points the wrong way. |
| `configuration` | Resource and dependency | Dangerous defaults, unvalidated config, undocumented environment dependence. |
| `coupling` | Structure and design | Hidden dependencies, layer violations, config reach-through, two sources of truth. |
| `complexity` | Structure and design | This code is harder to follow than the problem requires. |
| `bloat` | Structure and design | Code that should not exist at all: dead branches, unused abstraction, speculative generality. |
| `duplication` | Structure and design | Parallel implementations that will drift apart. |
| `extensibility` | Structure and design | A hardcoded assumption the known roadmap already contradicts. |
| `maintainability` | Structure and design | Structural readability cost not captured above: functions doing three jobs, misleading structure, comments that lie. |
| `naming` | Structure and design | An identifier that promises something the code does not do, or one concept spelled three ways. |
| `style` | Structure and design | Formatting and idiom only — no behavioural or structural claim. |
| `observability` | Cross-cutting | If this breaks in production nobody will know: no log, metric, or error context. |
| `testing` | Cross-cutting | Absent, vacuous, or non-isolated tests. |
| `docs` | Cross-cutting | Documentation or comments made wrong by this change. |
| `out-of-scope` | **Routing value** | Outside the reviewed change — a pre-existing issue in files mode, out-of-range in diff/blocks mode. Annotated, never promoted. |
| `other` | **Routing value** | A real finding that genuinely fits no member above. Keeps the set closed rather than lossy. |

## AXI TOON encoding (`atcr report --format axi`)

`atcr report --format axi` re-encodes these same reconciled findings as a token-dense, specification-compliant [TOON](https://toonformat.dev/) tabular array for agent consumption, encoded by [`go-axi` v0.3.1](https://github.com/samestrin/go-axi). It is a **re-encoding of this contract, not a competing schema** — the columns map to the reconciled 9-column stream field-for-field:

```
findings[2]{severity,"file:line",problem,fix,category,est_minutes,evidence,reviewers,confidence}:
  CRITICAL,"auth.go:42",token never expires,check expiry,security,15,expiresAt unread,"greta,host",HIGH
  LOW,"util.go:7",unused var,"",style,0,"",otto,MEDIUM
```

- **Delimiter:** the standard TOON comma. Any off-the-shelf TOON decoder reads the payload with no custom delimiter option.
- **`N`:** the array header declares the rows actually present. The paginated CLI payload adds a `total` line (the true, pre-truncation count) and a `truncated` line after the array; see [Agentic Consumption → Pagination and truncation](agentic-consumption.md#pagination-and-truncation).
- **Escaping is faithful, not lossy:** unlike the per-source stream's `|`→`/` neutralization, the encoder quotes any field containing the delimiter, a colon, a reserved token (`true`/`false`/`null`), a number-like value, or a control character, using only TOON's five escapes (`\\ \" \n \r \t`). Control/ANSI bytes and invalid UTF-8 have no TOON escape and are stripped, so the payload is structurally free of escape sequences. Code containing `|`, `||`, quotes, or newlines survives verbatim for fields of at most 500 runes.
- **Additive signals:** a finding's optional severity `disagreement` annotation and its `verification` / `evidence_exec` JSON blocks (below) surface as additive `disagreement` / `verification.*` / `evidence_exec.*` columns when any finding in the payload carries them, so the axi payload is a superset — never a lossy subset — of the JSON form.
- **Column names are lower-case here and upper-case in the pipe stream — normalise before keying on them.** The TOON header declares `severity`, `file:line`, `est_minutes`; the **reconciled** nine-column grammar above declares `SEVERITY`, `FILE:LINE`, `EST_MINUTES`. Those two name the same nine fields in the same order and only the casing differs, so a consumer that reads both of atcr's own output formats must case-normalise its key lookup. One that does not gets rows which decode perfectly and then key to nothing, with no parse error to point at. The comparison is to the **reconciled** stream specifically, because two grammars appear above and only that one matches: the **per-source** stream has eight columns ending in `REVIEWER` — a single source name, with no `CONFIDENCE` column at all — so mapping the axi payload onto it differs by more than casing. `REVIEWER` is not `reviewers`, and the ninth field a consumer goes looking for is simply not there.
- **Legacy pipe fallback (deprecated):** the pre-migration pipe-delimited encoding (`findings[N|]{a|b}:`, header `N` = true total, no `total` line) is still available through `--format pipe`, `--legacy-pipe` on `review --axi` and `atcr --axi`, or `ATCR_LEGACY_PIPE=1` for every AXI surface. Each legacy route writes this notice to stderr: `warning: pipe-delimited AXI output is deprecated and will be removed in a future release; migrate to standard TOON.` It will be removed in a future release.

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

Discovery is **leaf-preference**: a directory's `findings.txt` is an input only when no subdirectory beneath it also contains one. A directory that also holds a `findings.toon` is read through that file instead, and a directory that holds only a `findings.toon` (the skill-driven host source) is a source too; see [Which file atcr reads](#which-file-atcr-reads). Per-agent raw files (`sources/pool/raw/agent/<name>/findings.txt`) are the pool inputs; the merged `sources/pool/findings.txt` is written for downstream convenience but is **not** re-discovered, so reviewers are never double-counted. `reconciled/` is output, never an input.

A source's **`review.md`** — the human-readable narrative each reviewer (every pool agent and the host) writes alongside its `findings.txt` in the same leaf directory — is, as of Epic 18.2, also read at reconcile time. `atcr reconcile` correlates each finding to the `review.md` section that references its `FILE:LINE` (best-effort) and carries that narrative forward as the `justification` / `source_report` JSON fields (see [JSON form](#json-form) below). It is an **optional** input: a source with no `review.md` simply contributes no narrative, and `review.md` never itself yields findings — only `findings.txt` does.

## JSON form

`reconciled/findings.json` carries the same records in structured form, plus run metadata, for scripting. Each finding may carry a per-finding `verification` block, **produced by the adversarial-verification stage (`atcr verify`, Epic 3.0)** and **absent from 1.x output** and from any review that has not been verified. Renderers and readers must tolerate both its absence and its presence: `atcr report` renders a finding with no block identically to pre-Epic-3.0 output, and renders the Skeptic section / v2 confidence only when the block is present.

```json
{
  "severity": "HIGH",
  "file": "internal/auth/token.go",
  "line": 42,
  "problem": "JWT signature not verified before claims are read",
  "confidence": "VERIFIED",
  "verification": { "verdict": "confirmed", "skeptic": "otto", "notes": "read token.go:42 — jwt.Parse called without Verify" }
}
```

The block fields are:

| Field | Meaning |
|-------|---------|
| `verdict` | `confirmed`, `refuted`, or `unverifiable`. |
| `skeptic` | The agent name that produced the verdict (the **judge** when the verdict came from the cross-examination stage). |
| `notes` | The skeptic's (or judge's) reasoning (omitted when empty). |
| `challenge_survived` | `true` when the finding survived a hostile cross-examination (`atcr debate`, Epic 6.0) — the judge ruled `uphold` or `split`. Omitted (absent) otherwise, so a non-debated finding's block is byte-identical to pre-6.0 output. See [cross-examination.md](cross-examination.md). |
| `truncated` | `true` when the skeptic reached this verdict from a SHORTENED read: its tool-output ceiling tripped, but because that ceiling was derived from the agent's context window rather than declared by an operator, the trip truncated the read instead of overruling the answer. The verdict STANDS — this is a caveat on how it was reached, never a separate tier and never a downgrade — and `report.md` renders it as `(answered from a truncated read)`. A truncated verdict is excluded from the reviewer's durable `survived_skeptic_rate` (neither numerator nor denominator), because a partial read is not evidence about the reviewer in either direction. **This field drives what a human sees; the score reads `trippedBudgets` in `reconciled/verification.json`.** The split is not an oversight: the scorecard is emitted during `atcr reconcile`, which rebuilds `findings.json` from `sources/` and strips every `verification` block on the way, so `truncated` no longer exists by the time the score is computed — `verification.json` is never rebuilt and is the only verdict record still standing. The two are kept in step from the other side: when a judge ruling clears `truncated`, `atcr debate` clears the matching `tool_budget_bytes` entry in `verification.json` in the same atomic write. **When every verdict in a run is truncated, `survived_skeptic_rate` is omitted rather than published as `0.0`** — there is nothing left to divide, and a `0.0` is indistinguishable from a reviewer whose findings were all refuted. Omitted (absent) otherwise, so an untruncated finding's block is byte-identical. See [verification.md](verification.md). |

When `verification` is present, readers must treat an absent or unrecognized `verdict` value (including `""`) as unverified rather than trusting it — the allowed enum is `confirmed | refuted | unverifiable`; any other value indicates a future format or a write error.

**Confidence v2.** When a finding is verified, its `confidence` is recomputed onto a four-tier axis, ordered `VERIFIED > HIGH > MEDIUM > LOW`: a `confirmed` verdict promotes the finding to `VERIFIED`, a `refuted` verdict demotes it to `LOW` (retained for audit, never deleted), and `unverifiable` leaves the v1 confidence unchanged. The v1 tiers (`HIGH`/`MEDIUM`/`LOW`, the reviewer-agreement signal) are unchanged for unverified findings. Full mechanics and gate semantics are in [verification.md](verification.md).

**Execution evidence (Epic 11.0).** A finding that was *reproduced by running code* in the sandbox carries an additive `evidence_exec` block — `{ "command": string, "exit_code": int, "output_excerpt": string }` — stamped **only** by the opt-in execution path (`atcr verify --exec` / `review --verify --exec`), never by `atcr reconcile`. It is `omitempty`, so a non-reproduced record stays byte-identical to pre-11.0 output. A reproduced finding is also marked `verification.verdict = "confirmed"` with `skeptic = "repro"`, so it is `VERIFIED` by the same confidence/gate rules above — execution does not add a new tier, it earns the existing confirmed verdict by demonstration. `atcr report` renders a "Reproduced" badge (the command, exit code, and a truncated output excerpt) when the block is present. The reproduction is only stamped `confirmed` when the failure reproduces **deterministically** (the command is run twice and must agree on a non-zero exit); a flaky or non-reproducing run is left `unverifiable` so flaky tests cannot poison the evidence. See [docs/execution.md](execution.md).

```json
  "evidence_exec": { "command": "go test ./calc", "exit_code": 1, "output_excerpt": "--- FAIL: TestAdd ... want 4 got 5" }
```

**Inline-merge markers (Epic 6.1 / 6.2).** A finding produced by the cross-examination stage's gray-zone "merge" ruling (`atcr debate`) carries two additive fields: `cluster_merged` (`true` on the survivor that unioned a gray-zone cluster's members) and `cluster_id` (the stable, content-addressed id of that source cluster, which lets the debate radar key merge-idempotency on cluster identity rather than `FILE:LINE` alone). Both are `omitempty` and are stamped **only** by the debate apply path — never by `atcr reconcile` — so a non-merged or non-debated record stays byte-identical to pre-6.x output. Per the additive-only evolution policy below, they ride `atcr-findings/v1` with no version bump; a strict consumer that rejects unknown JSON keys (`DisallowUnknownFields`-style) must tolerate them as it must any additive v1 field.

**Reconcile-time narrative (Epic 18.2).** Two additive fields carry the originating review's context past reconciliation, so a downstream technical-debt-resolution consumer inherits the reviewer's reasoning instead of re-deriving it from raw `review.md` files:

- `justification` — the narrative section extracted from the finding's originating source `review.md`, matched **best-effort** by `FILE:LINE`. Fenced blocks inside that section are **quoted examples, not the reviewer's prose**, so each **terminated** fence — its ``` markers included — is replaced by the single line `[quoted example elided]`; follow `source_report` to read the quote itself. An **unterminated** fence is the exception: its tail is rendered as ordinary prose rather than elided, and the excerpt always carries a ``` marker, because that marker is the only remaining sign a quote was opened at all. The marker is guaranteed to be **present**, not guaranteed to be **leading** — atcr prepends it whenever the extracted block begins inside the released tail (a quoted body beginning with a list item or a heading is a genuine block start, so the walk stops below the opener), and when the block starts at or above the opener the reviewer's own ``` is carried inline as ordinary excerpt text instead. So an excerpt drawn from an unterminated fence always **contains** a ```, and its absence does mean the excerpt is not released quote — but the converse does not hold: a bare ``` is not distinguishable from a reviewer who simply typed backticks, nor from the elision, which is why `source_report` is the authority. An excerpt that would consist of nothing but placeholders carries no reviewer content and is omitted entirely, exactly as an unmatched finding is. The literal is **not escaped** in retained prose, so a reviewer who writes it is passed through verbatim and is byte-indistinguishable from a real elision; a match is evidence, not proof — follow `source_report` for the authority. A placeholder that will not fit whole inside the excerpt's rune budget is dropped rather than cut in half, so a fragment of it never appears; the budget really is counted in runes, so non-ASCII prose buys the same excerpt length as ASCII. When the excerpt is truncated, the trailing `…` sits on its own line, so a cut landing on a placeholder can never fuse into it. It is distinct from `verification.notes`: `justification` is the reviewer's *original* explanation captured at reconcile time, whereas `verification.notes` is the *adversarial* stage's later skeptic/judge reasoning. Omitted when no `review.md` section references the finding's `FILE:LINE` (a match requires a line-level reference, so a bare "no issues" file mention never attaches a misleading narrative).
- `source_report` — the back-reference to that section: `{ "path": <review-dir-relative review.md path>, "line": <1-based anchor line>, "section": <nearest Markdown heading> }`, so a consumer can navigate to full detail without re-deriving the mapping. `path` is relative to the review directory (the same dir that holds `reconciled/findings.json`); `line` and `section` are omitted when absent.

```json
  "justification": "The handler calls jwt.Parse without jwt.Verify, so a forged token is accepted.",
  "source_report": { "path": "sources/host/review.md", "line": 42, "section": "Findings" }
```

Both are stamped **only** by `atcr reconcile` (never by the verify or debate paths), are `omitempty` — so a finding with no matched narrative stays byte-identical to pre-18.2 output — and ride `atcr-findings/v1` with no version bump per the additive-only policy below. The pair lands **only** in `reconciled/findings.json`; neither is ever written into the technical-debt README table's `Problem` cell, whose column structure Epic 18.1 freezes.

## Reserved fields in companion artifacts

The other v1 review artifacts carry reserved fields for the agentic stages on the same "parsed, not yet acted on" basis. Consumers must tolerate their presence and absence:

| Artifact | Field | 1.x value | Reserved for |
|----------|-------|-----------|--------------|
| `manifest.json` | `stages` (array) | `["review"]` — records the one stage that ran; `WriteManifest` normalizes nil to `["review"]` so the field is always present; readers MUST default an absent `stages` to `["review"]` for older manifests written before this field existed. **Active in 3.0:** `atcr verify` appends `"verify"` (idempotently). **Active in 6.0:** `atcr debate` appends `"debate"` (idempotently) | — |
| per-agent `status.json` | `turns`, `tool_calls`, `tool_bytes` | **Active:** present as possibly-zero integers for a `tools: true` agent (the tool-using reviewer loop shipped); absent for a single-shot agent | — |

## Companion artifact: `disagreements.json`

`reconciled/disagreements.json` is a deterministic projection over the merged
findings and the gray-zone sidecar — the disagreement-radar handoff queue Epic
6.0 consumes. It is versioned independently (`schemaVersion`) and documented in
[disagreement-radar.md](disagreement-radar.md), along with the
`atcr report --disagreements` view and the `report.md` radar section.

## Evolution policy

The version header is in force from day one. **Evolution is additive-only within a major version:** new optional columns may be appended and new optional JSON fields may be added, but existing column positions, the severity enum, and the extraction regex never change under `v1`. Any breaking change increments the version (`atcr-findings/v2`), and the header gate guarantees old consumers reject it loudly rather than misparsing.

## v2 lossless stream (`findings.toon`)

v1 cannot carry some text. Its writer replaces `|` with `/` and line breaks with a space, so a bitwise OR, a regex alternation, a shell pipeline, or a multiline diff is changed on the way to disk. v2 changes nothing: every `|`, quote, backslash, comma, colon, `\n`, and `\r\n` survives, in fields of any length.

v2 is a per-source format only. There is no reconciled v2 shape: `reconciled/findings.txt` stays v1, and atcr's reconciled reader rejects a v2 header.

### File layout

The first non-blank line is the header `# atcr-findings/v2`. The rest of the file is one [go-axi](https://github.com/samestrin/go-axi) v0.3.1 document holding the findings, in one of two encodings:

- a TOON table named `findings` (what atcr writes), or
- the go-axi JSON envelope `{"axi_format":"json","axi_notice":"...","data":{"findings":[...]}}` (what the skill-driven host reviewer writes).

Each finding has the same 8 fields as a v1 per-source row, in lower case: `severity`, `file_line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`, `reviewer`. `file_line` is `FILE:LINE`; a reader splits it at the last colon. `est_minutes` is an integer.

### TOON table

atcr writes the table with `goaxi.EncodeOrJSON`. This example is the exact output for two findings:

```
# atcr-findings/v2
findings[2]{severity,file_line,problem,fix,category,est_minutes,evidence,reviewer}:
  HIGH,"internal/fs/open.go:42",os.O_CREATE | os.O_WRONLY drops O_TRUNC,Use os.O_WRONLY | os.O_CREATE | os.O_TRUNC,correctness,10,"-f, _ := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o644)\n+f, _ := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)",bruce
  LOW,"cmd/main.go:7","log says \"done\" before the write","",style,5,"",bruce
```

With no findings the body is `findings[0]:`.

- **Delimiter:** the standard TOON comma. `|` is ordinary text.
- **Quoting:** the encoder quotes a field when TOON requires it, for example one that holds the delimiter, a colon, a quote, or a line break, and an empty field. Inside quotes it uses only TOON's five escapes: `\\ \" \n \r \t`.
- **`N`:** the header declares the exact row count. A reader rejects a table whose row count differs from `N`, so a file cut on a row boundary is an error, not a short read.
- **Columns:** a reader needs all 8 columns, in any order. It ignores any other column (see [v2 evolution](#v2-evolution)).

### JSON envelope

`goaxi.EncodeOrJSON` writes the envelope instead of the table only when TOON would lose data. Size never triggers it. The choice is made once for the whole document, so one field TOON cannot carry turns every row into the envelope. atcr's own findings are all text and integers, which TOON always carries, so in practice the envelope comes from the host reviewer, which writes it directly because plain JSON is easier to produce correctly than TOON indentation.

```
# atcr-findings/v2
{"axi_format":"json","axi_notice":"","data":{"findings":[{"severity":"HIGH","file_line":"scripts/release.sh:12","problem":"The pipeline returns the exit status of tee, not of the build","fix":"set -o pipefail\ngo build ./... | tee build.log","category":"correctness","est_minutes":10,"evidence":"go build ./... | tee build.log","reviewer":"host"}]}}
```

- **Routing:** a reader picks the envelope when the body, after leading whitespace, starts with the literal `{"axi_format`. It never routes on a bare `{` or `[`, because several TOON shapes start with `[`.
- **Required:** `axi_format` must be `"json"`, and the findings must be at `data.findings` (an empty array is a clean review). `axi_notice` is optional.
- **Keys:** every finding object must carry all 8 keys, spelled exactly in lower case. A missing or misspelled key is an error, never an empty field. Any other key, at any level, is ignored (see [v2 evolution](#v2-evolution)).
- **Nothing after it:** a second envelope, prose, or a code fence after the envelope is an error.

### What survives

v2 round-trips any text byte for byte, with one exception. `EncodeOrJSON` runs go-axi's sanitizer on both encodings, which strips control bytes other than tab, LF, and CR, ANSI escape sequences, `U+2028`/`U+2029`, and invalid UTF-8. v2 has no length cap. The 500-rune cap in [AXI TOON encoding](#axi-toon-encoding-atcr-report---format-axi) belongs to `atcr report --format axi` only.

**Not the same as `--format axi`.** Both use TOON through go-axi, but they are separate surfaces. The v2 stream (`internal/stream`) is a per-source findings file on disk with a `file_line` column and a single `reviewer`. The `--format axi` report is a rendering of the reconciled findings for agents, with `file:line`, `reviewers`, and `confidence` columns. Do not parse one with rules written for the other.

### Dual write

atcr writes every per-source findings file twice, side by side: `findings.toon` (v2) and `findings.txt` (v1). This covers each `sources/pool/raw/agent/<agent>/` directory and the merged `sources/pool/` file, including a pool rebuilt by `atcr review --resume`. `findings.txt` is byte-identical to what atcr wrote before v2 existed. Both files are encoded before either is written, and `findings.toon` is written first, so a failed write never leaves a stale `findings.toon` beside a fresh `findings.txt`.

The skill-driven host reviewer (atcr v0.4.0 or later) writes only `sources/host/findings.toon`, as the JSON envelope. It writes no `findings.txt`.

### Which file atcr reads

Every atcr reader picks a directory's file with one rule: `findings.toon` when it is a regular file, else `findings.txt`. A `findings.toon` that is a symlink, FIFO, device, or directory counts as absent. The readers that follow this rule are reconcile source discovery, the pool rebuild in `atcr review --resume`, `atcr history`, the audit capture, and `atcr benchmark` (both the case run and the repo-state reader). `atcr history`, the audit capture, and `atcr benchmark` read `sources/pool/findings.toon` first and fall back to `findings.txt` only when no `findings.toon` exists.

The choice is final. When atcr picks `findings.toon` and it does not parse, the reader reports an error; it never retries `findings.txt`, because that would hide a v2 writer bug behind lossy data. A file named `findings.toon` must carry the v2 header; a v1 header there is an error.

A `findings.txt`-only directory reads exactly as before, so review directories written by an older atcr, or by a third-party tool, still work.

### What reviewer models emit

Reviewer models do not write v2 files. Each persona prompt asks the model for one fenced `json` code block holding a JSON array of finding objects with 7 keys: `severity`, `file_line`, `problem`, `fix`, `category`, `est_minutes`, `evidence`. There is no `reviewer` key: the engine sets the reviewer from the agent name, and a model-supplied `reviewer` key is ignored. A clean review is the line `NO FINDINGS`. atcr parses the reply (`stream.ParseModelOutput`) and writes the result as both files above. [Persona authoring → Output Format](personas-authoring.md) has the prompt text.

The parser also accepts what models commonly send instead:

- several fenced `json` blocks, as a chunked review produces; their findings are combined;
- a block cut off mid-object; every complete object before the cut is kept;
- a JSON array with no fence, a `{"findings":[...]}` wrapper, or a single finding object;
- `file` and `line` keys in place of `file_line`;
- `NO FINDINGS` with a trailing `.`, `:`, or `!`, inside a code fence, or an empty array `[]` or `{"findings":[]}`, all read as a clean review;
- legacy 7-column pipe rows, from a custom persona that still uses the v1 contract.

An object with an unknown severity or no location is dropped. A reply that yields no findings and is not a clean review is recorded as `unparseable_response` in the agent's `status.json`.

### v2 evolution

v2 follows the same rule as v1: evolution is additive-only within a major version. A newer atcr may add a column or key. A reader must require the 8 fields above and ignore any other one, which is what atcr's reader does, so an older atcr still reads a newer file. Renaming or removing one of the 8 fields, or changing its meaning, needs a new version (`atcr-findings/v3`).

### v1 deprecation policy

v1 is still written and is not deprecated for removal yet. atcr writes `findings.txt` beside every `findings.toon`, byte-identical to its pre-v2 output, so an existing v1 consumer needs no change. A new consumer should read `findings.toon`. These consumers still read v1 only and are the next to migrate: `llm_support_td_dedupe`, the `/reconcile-code-review` skill, and `internal/report/legacy_pipe.go`. v1 stays until they have moved.
