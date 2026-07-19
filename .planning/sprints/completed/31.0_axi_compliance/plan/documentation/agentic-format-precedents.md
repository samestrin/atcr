# Existing Agent-Facing Format & Output-Safety Contracts

**Priority:** Important

## Overview

The AXI plan's own risk register warns that "inventing a new TOON schema duplicates the existing `atcr-findings/v1` agent-facing format precedent, fragmenting the 'machine format' surface," and its mitigation is to review that precedent during design before finalizing the new schema (plan.md: Risk Mitigation). That precedent is specified in `docs/findings-format.md`: atcr already ships a pipe-delimited, versioned, machine-parseable findings format that is the public contract between every findings producer (persona pool, host Skill, third-party tools) and the deterministic reconciler — and between atcr and any downstream consumer. Codebase discovery confirmed the canonical specification lives in `docs/findings-format.md` (not `docs/skill-usage.md`, an earlier miscitation corrected during discovery refinement), with skill-side copies in `skill/findings-format.md` and `skill/host-review.md` (codebase-discovery.json semantic_matches, architecture_notes).

Beyond the format itself, three more in-repo contracts bound how the new axi output mode must behave, and none of them require new invention: the **never-silent deterministic truncation** contract already used for reviewer payloads (whose `truncated` field name the AXI line cap should reuse), the **control-character sanitization** idiom that guarantees emitted text carries no escape sequences (and its OSC-8 counterexample proving interactive paths do emit them), and the **golden-file test gate** that pins every existing report format byte-for-byte (the gate the axi renderer must register with from day one). This document excerpts those four contracts so the design can align with them instead of rediscovering them.

## Key Concepts

### `atcr-findings/v1`: a versioned machine format with a hard header gate

Every findings file MUST begin with `# atcr-findings/v1` as its first non-blank line. The parser treats the header as a hard gate: a missing header is a fatal parse error (`missing version header`), and an unknown version (e.g. `# atcr-findings/v2`) is a *distinct* fatal error (`unknown findings version`), "so a consumer never silently parses incompatible data" (docs/findings-format.md:7-18).

Two shapes share one grammar: the **per-source** stream (8 columns: `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER`, written by each reviewer source) and the **reconciled** stream (9 columns, adding `CONFIDENCE` and widening `REVIEWER` to the comma-joined `REVIEWERS` set, written by `atcr reconcile`) (docs/findings-format.md:20-51).

Parsing is defensive by construction: extraction is by strict severity-prefix regex (`^(CRITICAL|HIGH|MEDIUM|LOW)\|`), so prose mentioning "HIGH risk" is never mistaken for a row; comment and blank lines are skipped; short rows are padded; and rows with more columns than expected "are recorded as skipped with their line number and reason — never silently misaligned" (docs/findings-format.md:67-73). Field escaping is lossy but structurally stable: literal `|` becomes `/`, and CR/LF inside a field becomes a single space, so the one-row-per-line invariant always holds (docs/findings-format.md:75-82).

> Source: [docs/findings-format.md](/Users/samestrin/Documents/GitHub/atcr/docs/findings-format.md): Version header, Per-source stream, Reconciled stream, Parsing rules, Field escaping

### Additive-only evolution policy — the model for any axi schema versioning

"The version header is in force from day one. **Evolution is additive-only within a major version:** new optional columns may be appended and new optional JSON fields may be added, but existing column positions, the severity enum, and the extraction regex never change under `v1`. Any breaking change increments the version (`atcr-findings/v2`), and the header gate guarantees old consumers reject it loudly rather than misparsing." (docs/findings-format.md:157-159). The JSON form already exercises this policy repeatedly — Epic 3.0's `verification` block, Epic 11.0's `evidence_exec`, Epic 6.1/6.2's merge markers, and Epic 18.2's `justification`/`source_report` all ride `v1` additively with `omitempty`, keeping non-participating records byte-identical to their pre-feature form (docs/findings-format.md:92-138).

> Source: [docs/findings-format.md](/Users/samestrin/Documents/GitHub/atcr/docs/findings-format.md): Evolution policy, JSON form

### Never-silent deterministic truncation, and the `truncated` flag name

Reviewer payloads already operate under a byte budget — `payload_byte_budget`, default **524288 bytes (512 KiB)**, resolved with the usual precedence (CLI `--byte-budget` > project config > registry > embedded default). When a payload exceeds its budget, atcr truncates **deterministically** rather than letting a provider silently clip: whole files are dropped **largest-first** by size rank (ties broken by path), a budget of `0` means unlimited, a negative budget is rejected at validation, and "every drop is **recorded in the agent's `status.json`** — what was dropped and why is never silent" (docs/payload-modes.md:38-44).

The status.json field names are fixed in code: `Truncated bool \`json:"truncated"\`` and `FilesDropped []string \`json:"files_dropped"\`` (internal/fanout/status.go:292-293). Per codebase-discovery.json (existing_patterns: "Never-silent deterministic truncation"), the AXI 500-line cap should adopt the same contract: a deterministic truncation point, an explicit `truncated` flag in the emitted payload reusing this exact field name, and the cap overridable via `ATCR_AXI_MAX_LINES` — never a silently clipped stream.

> Source: [docs/payload-modes.md](/Users/samestrin/Documents/GitHub/atcr/docs/payload-modes.md): Byte budgets and truncation; internal/fanout/status.go:292-293; codebase-discovery.json

### Control-character sanitization idiom — and its OSC-8 counterexample

The in-repo idiom for guaranteeing emitted text carries no escape/control sequences is `sanitizeDisplay` (cmd/atcr/models.go:288-296), which strips control characters — explicitly including the U+2028/U+2029 line/paragraph separators — from any value bound for a human-readable line, via `strings.Map` + `unicode.IsControl`. The behavior is pinned by dedicated precedent tests `TestDriftLine_StripsControlChars` (cmd/atcr/models_test.go:180) and `TestRenderPersonaSearch_StripsControlChars` (cmd/atcr/personas_test.go:478). The counterexample matters just as much: `osc8` (cmd/atcr/quickstart.go:456-459) wraps URLs in OSC-8 terminal hyperlinks, proving atcr *does* emit ANSI/OSC escape sequences in interactive paths — so axi mode needs "a guarantee, not a hope: either render from a format that cannot contain escapes, or pass final output through a sanitize step following this precedent — with a StripsControlChars-style test pinning it" (codebase-discovery.json existing_patterns).

> Source: codebase-discovery.json existing_patterns ("Control-character sanitization for terminal output"); cmd/atcr/models.go:288-296; cmd/atcr/quickstart.go:456-459

### Golden-file test gate: every format is pinned byte-for-byte

Every report format's full output is pinned byte-for-byte to a checked-in fixture under `internal/report/testdata/` via the `goldenCases` table (internal/report/render_test.go:63-77 — currently `md`/`json`/`checklist`/`sarif`) and `TestRender_GoldenFiles` (render_test.go:80). Fixtures regenerate with `go test ./internal/report -update` (the `-update` flag is defined at render_test.go:18-21; a missing fixture fails with "run: go test ./internal/report -update"). Sprint 25.0 shipped SARIF through exactly this gate, and the axi renderer must register its own `goldenCases` entry plus committed fixture "so token-dense output drift fails loudly in CI" (codebase-discovery.json test_patterns, architecture_notes). The MCP side is pinned too: `internal/mcp/tools_test.go:108-130` derives the expected schema enum from `report.FormatList()` and the expected description from `report.Formats()` and asserts both match, citing Sprint 25.0's AC 04-04 — so a format added to the enum without considering MCP surfaces in a test failure, not silently.

> Source: internal/report/render_test.go:18-21, 62-80; internal/mcp/tools_test.go:108-130; codebase-discovery.json

## Code Examples

The following are verbatim from the source documents.

### Version header and per-source stream

> Source: docs/findings-format.md:9-32

```
# atcr-findings/v1
HIGH|internal/auth/token.go:42|JWT signature not verified before claims are read|Call jwt.Verify before decoding claims|security|20|token, _ := jwt.Parse(raw)|bruce
MEDIUM|internal/store/cache.go:88|Unbounded map grows without eviction|Add an LRU bound|performance|45|c.entries[k] = v // never deleted|greta
```

### Reconciled stream (9 columns)

> Source: docs/findings-format.md:36-48

```
SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWERS|CONFIDENCE
```

### status.json truncation fields

> Source: internal/fanout/status.go:292-293

```go
Truncated     bool     `json:"truncated"`
FilesDropped  []string `json:"files_dropped"`
```

### Control-character stripping helper

> Source: cmd/atcr/models.go:288-296

```go
// sanitizeDisplay strips control characters (including U+2028/2029 line and
// paragraph separators) from a value bound for a human-readable line, mirroring
// the TD-008 control-char discipline. The --json path needs no equivalent — the
// standard-library encoder escapes control characters itself.
func sanitizeDisplay(s string) string {
```

## Quick Reference

| Contract | Source of truth | What the AXI design must do with it |
|---|---|---|
| `atcr-findings/v1` versioned wire format | `docs/findings-format.md` (skill-side copies: `skill/findings-format.md`, `skill/host-review.md`) | Review before finalizing the TOON/axi schema; do not fragment the machine-format surface with a redundant competing schema (plan.md Risk Mitigation) |
| Hard version gate (`missing`/`unknown` are distinct fatal errors) | `docs/findings-format.md`:7-18 | If the axi payload carries a schema version, reject loudly on mismatch rather than misparsing |
| Additive-only evolution within a major version | `docs/findings-format.md`:157-159 | Version the axi schema the same way: optional additions only within a version; breaking changes bump it |
| Defensive parsing posture (strict prefix anchor, skip-and-record overflow) | `docs/findings-format.md`:67-73 | Emit output that is trivially anchored (no prose rows), so consumers never need heuristics |
| Deterministic, never-silent truncation + `truncated`/`files_dropped` naming | `docs/payload-modes.md`:38-44; `internal/fanout/status.go`:292-293 | Reuse the `truncated` flag name and the deterministic-truncation-point philosophy for the 500-line cap + `ATCR_AXI_MAX_LINES` override |
| Control-char sanitization idiom (`sanitizeDisplay`) + OSC-8 counterexample | `cmd/atcr/models.go`:288-296; `cmd/atcr/quickstart.go`:456-459 | Guarantee no ANSI/OSC sequence reaches axi stdout, pinned by a StripsControlChars-style test |
| Golden-file byte-stability gate (`goldenCases`, `-update`) | `internal/report/render_test.go`:18-21, 63-80 | Register an axi `goldenCases` entry and commit the fixture from day one |
| MCP enum/description drift pin | `internal/mcp/tools_test.go`:108-130 | Expect CI failure if `FormatAXI` enters the enum without a deliberate MCP parity decision (see [mcp-schema-format-propagation.md](mcp-schema-format-propagation.md)) |

## Related Documentation

- Plan: [../plan.md](../plan.md)
- [docs/findings-format.md](/Users/samestrin/Documents/GitHub/atcr/docs/findings-format.md) — canonical `atcr-findings/v1` specification
- [docs/payload-modes.md](/Users/samestrin/Documents/GitHub/atcr/docs/payload-modes.md) — byte-budget and deterministic-truncation contract
- [MCP Tool Schema & Format-Enum Propagation](mcp-schema-format-propagation.md) — the MCP-side parity question for a new format constant
- [Exit-Code Contract & CLI/MCP Dual-Surface Precedent](exit-code-cli-mcp-precedent.md) — the exit-code contract axi output must layer onto
