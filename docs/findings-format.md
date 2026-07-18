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
- **`CONFIDENCE`** is `HIGH` when 2+ distinct reviewers agree and `MEDIUM` for a single reviewer, with `LOW` reserved for untrusted sources. As a refinement, an isolated (single-reviewer) finding is promoted to `HIGH` when its model's per-run PageRank *authority* — earned by agreeing with other models elsewhere in the run — exceeds the uniform `1/N` baseline. Promotion is one-directional: authority never lowers a finding's confidence. When no cross-model agreement exists in a run the signal is inert and confidence is exactly the reviewer-count result.

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
| 9 | `CONFIDENCE` (reconciled only) | `HIGH` / `MEDIUM` / `LOW` | Reviewer-agreement signal, refined by per-run PageRank authority (an isolated finding from an above-`1/N`-authority model is promoted to `HIGH`; never demoted). |

## AXI (`--axi`) TOON encoding

`atcr report --format axi` (and the `--axi` flag on `atcr review`/`atcr resume`) re-encodes these same reconciled findings as a token-dense [TOON](https://toonformat.dev/) tabular array for agent consumption. It is a **re-encoding of this contract, not a competing schema** — the columns map to the reconciled 9-column stream field-for-field:

```
findings[N|]{severity|"file:line"|problem|fix|category|est_minutes|evidence|reviewers|confidence}:
  CRITICAL|"auth.go:42"|token never expires|check expiry|security|15|expiresAt unread|greta,host|HIGH
```

- **Delimiter:** the pipe (`[N|]{…}:`) is declared in the header so a row is structurally adjacent to the `SEVERITY|FILE:LINE|…` grammar above.
- **`N`:** the array header carries the true total finding count (independent of the emitted row count once the `--axi` line cap truncates — see [ci-integration.md](ci-integration.md)).
- **Escaping is faithful, not lossy:** unlike the per-source stream's `|`→`/` neutralization, AXI quotes any field containing the delimiter, a colon, a reserved token (`true`/`false`/`null`), a number-like value, or a control character, using only TOON's five escapes (`\\ \" \n \r \t`). Control/ANSI bytes have no TOON escape and are stripped, so `--axi` stdout is structurally free of escape sequences.
- **Additive signals:** a finding's optional severity `disagreement` annotation and its `verification` / `evidence_exec` JSON blocks (below) surface as additive `disagreement` / `verification.*` / `evidence_exec.*` columns when any finding in the payload carries them, so AXI is a superset — never a lossy subset — of the JSON form.
- **MCP:** `axi` is a CLI-only format and is intentionally excluded from the MCP `atcr_report` format enum.

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

When `verification` is present, readers must treat an absent or unrecognized `verdict` value (including `""`) as unverified rather than trusting it — the allowed enum is `confirmed | refuted | unverifiable`; any other value indicates a future format or a write error.

**Confidence v2.** When a finding is verified, its `confidence` is recomputed onto a four-tier axis, ordered `VERIFIED > HIGH > MEDIUM > LOW`: a `confirmed` verdict promotes the finding to `VERIFIED`, a `refuted` verdict demotes it to `LOW` (retained for audit, never deleted), and `unverifiable` leaves the v1 confidence unchanged. The v1 tiers (`HIGH`/`MEDIUM`/`LOW`, the reviewer-agreement signal) are unchanged for unverified findings. Full mechanics and gate semantics are in [verification.md](verification.md).

**Execution evidence (Epic 11.0).** A finding that was *reproduced by running code* in the sandbox carries an additive `evidence_exec` block — `{ "command": string, "exit_code": int, "output_excerpt": string }` — stamped **only** by the opt-in execution path (`atcr verify --exec` / `review --verify --exec`), never by `atcr reconcile`. It is `omitempty`, so a non-reproduced record stays byte-identical to pre-11.0 output. A reproduced finding is also marked `verification.verdict = "confirmed"` with `skeptic = "repro"`, so it is `VERIFIED` by the same confidence/gate rules above — execution does not add a new tier, it earns the existing confirmed verdict by demonstration. `atcr report` renders a "Reproduced" badge (the command, exit code, and a truncated output excerpt) when the block is present. The reproduction is only stamped `confirmed` when the failure reproduces **deterministically** (the command is run twice and must agree on a non-zero exit); a flaky or non-reproducing run is left `unverifiable` so flaky tests cannot poison the evidence. See [docs/execution.md](execution.md).

```json
  "evidence_exec": { "command": "go test ./calc", "exit_code": 1, "output_excerpt": "--- FAIL: TestAdd ... want 4 got 5" }
```

**Inline-merge markers (Epic 6.1 / 6.2).** A finding produced by the cross-examination stage's gray-zone "merge" ruling (`atcr debate`) carries two additive fields: `cluster_merged` (`true` on the survivor that unioned a gray-zone cluster's members) and `cluster_id` (the stable, content-addressed id of that source cluster, which lets the debate radar key merge-idempotency on cluster identity rather than `FILE:LINE` alone). Both are `omitempty` and are stamped **only** by the debate apply path — never by `atcr reconcile` — so a non-merged or non-debated record stays byte-identical to pre-6.x output. Per the additive-only evolution policy below, they ride `atcr-findings/v1` with no version bump; a strict consumer that rejects unknown JSON keys (`DisallowUnknownFields`-style) must tolerate them as it must any additive v1 field.

**Reconcile-time narrative (Epic 18.2).** Two additive fields carry the originating review's context past reconciliation, so a downstream technical-debt-resolution consumer inherits the reviewer's reasoning instead of re-deriving it from raw `review.md` files:

- `justification` — the narrative section extracted from the finding's originating source `review.md`, matched **best-effort** by `FILE:LINE`. It is distinct from `verification.notes`: `justification` is the reviewer's *original* explanation captured at reconcile time, whereas `verification.notes` is the *adversarial* stage's later skeptic/judge reasoning. Omitted when no `review.md` section references the finding's `FILE:LINE` (a match requires a line-level reference, so a bare "no issues" file mention never attaches a misleading narrative).
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
| `manifest.json` | `stages` (array) | `["review"]` — records the one stage that ran; `WriteManifest` normalizes nil to `["review"]` so the field is always present; readers MUST default an absent `stages` to `["review"]` for older manifests written before this field existed. **Active in 3.0:** `atcr verify` appends `"verify"` (idempotently) | later stages append `"debate"` (Epics 4.0–5.0) |
| per-agent `status.json` | `turns`, `tool_calls`, `tool_bytes` | absent — no tool loop ran | **Epic 2.0** — tool-using reviewer loop |

## Companion artifact: `disagreements.json`

`reconciled/disagreements.json` is a deterministic projection over the merged
findings and the gray-zone sidecar — the disagreement-radar handoff queue Epic
6.0 consumes. It is versioned independently (`schemaVersion`) and documented in
[disagreement-radar.md](disagreement-radar.md), along with the
`atcr report --disagreements` view and the `report.md` radar section.

## Evolution policy

The version header is in force from day one. **Evolution is additive-only within a major version:** new optional columns may be appended and new optional JSON fields may be added, but existing column positions, the severity enum, and the extraction regex never change under `v1`. Any breaking change increments the version (`atcr-findings/v2`), and the header gate guarantees old consumers reject it loudly rather than misparsing.
