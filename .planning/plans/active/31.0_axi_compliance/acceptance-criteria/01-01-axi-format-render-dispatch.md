# Acceptance Criteria: `FormatAXI` Render Dispatch for `atcr report`

**Related User Story:** [01: `--axi` Token-Dense Output Mode for `atcr review` and `atcr report`](../user-stories/01-axi-token-dense-output-mode.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI command + view-layer renderer (`internal/report` package) | Extends the existing format-enum dispatch pattern used by `FormatSarif`/`FormatChecklist` |
| Test Framework | `go test` with `testify/assert` + `testify/require`; golden-file byte comparison | Mirrors `TestRender_GoldenFiles` in `internal/report/render_test.go` |
| Key Dependencies | Go standard library (`bytes`, `strings`, `unicode/utf8`); no third-party TOON library unless `/design-sprint` explicitly adopts one | Plan.md's default stance is hand-rolled formatters over third-party dependencies (see `documentation/toon-format-reference.md`) |

## Related Files
- `internal/report/render.go` - modify: add `FormatAXI = "axi"` to the format-enum block (line ~23-28), add a `case FormatAXI:` branch in `Render()`'s switch (line ~57-74) dispatching to a new `renderAXI` function, and add `FormatAXI` to `ValidFormat` and `FormatList`.
- `cmd/atcr/report.go` - modify: accept `--format axi` (or `--format=axi`) through the existing `report.ValidFormat(format)` check (line 35) with no new flag-parsing branch required, since format validation and dispatch are already format-agnostic.
- `internal/report/render_test.go` - modify: add `{"axi", FormatAXI, "report.axi", nil}` to the `goldenCases` table (line ~63-73) so `TestRender_GoldenFiles` (line ~80) exercises and byte-pins the new format.
- `internal/report/testdata/` - create: `report.axi` (or `.toon`/`.json` per final schema decision) golden fixture generated via `go test ./internal/report -update`.

## Happy Path Scenarios
**Scenario 1: `atcr report --format axi` renders a token-dense payload**
- **Given** a review directory with a populated `reconciled/findings.json` containing 2+ findings across multiple severities
- **When** the user runs `atcr report --format axi <review-dir>`
- **Then** stdout contains a single TOON (or compact-JSON) payload representing all findings, exits 0, and contains zero `\x1b[` ANSI escape sequences and zero Markdown table (`|---|`) or heading (`#`/`##`) syntax

**Scenario 2: `atcr report --format axi --output file.toon` writes to a file**
- **Given** the same populated review directory
- **When** the user runs `atcr report --format axi --output findings.toon <review-dir>`
- **Then** the file is written with the same byte content that would have gone to stdout, and the CLI exits 0 with no stdout report body (consistent with the existing `--output` behavior in `runReport`, `cmd/atcr/report.go` lines 118-126)

## Edge Cases
**Edge Case 1: Zero findings**
- **Given** a review directory whose reconciled findings list is empty
- **When** `atcr report --format axi` runs
- **Then** the renderer emits a well-formed empty-array TOON/JSON payload (e.g. `findings[0]:` per the TOON spec's empty-container rule, or `{"findings":[]}` for JSON) rather than an error or a human-oriented "No findings." sentence, and exits 0

**Edge Case 2: Findings with pipe, comma, colon, or newline characters in `PROBLEM`/`FIX`/`EVIDENCE`**
- **Given** a finding whose `Problem` field contains a literal pipe (`|`), comma, colon, or embedded newline
- **When** rendered with `--format axi`
- **Then** the field is quoted/escaped per the TOON must-quote rules (`documentation/toon-format-reference.md`: quote on delimiter-char, colon, newline) rather than losslessly mangled (no silent `|`→`/` substitution as `atcr-findings/v1` does), so the original text is round-trippable from the axi payload

**Edge Case 3: Unicode file paths and finding text**
- **Given** a finding whose `File` path or `Problem` text contains multi-byte UTF-8 characters
- **When** rendered with `--format axi`
- **Then** the payload preserves the characters byte-for-byte (no mojibake, no truncation of the path), consistent with the markdown renderer's existing unicode-path guarantee (`internal/report/render.go` `codeSpan`, line ~474-485)

## Error Conditions
**Error Scenario 1: Malformed `reconciled/findings.json`**
- **Given** a review directory with a corrupt or truncated `findings.json`
- **When** `atcr report --format axi` runs
- **Then** the command exits 1 (present-but-malformed data path), consistent with the existing `readReconciledFindings` error classification in `cmd/atcr/report.go` (lines 66-72)
- Error message: `"failed to parse findings: <wrapped error>"`

**Error Scenario 2: Missing reconciled data**
- **Given** a review directory that has never been reconciled
- **When** `atcr report --format axi` runs
- **Then** the command exits 2 (usage error)
- Error message: `"no reconciled data found: run 'atcr reconcile' first"`

## Performance Requirements
- **Response Time:** Rendering 1,000 findings to an axi payload completes in under 200ms on commodity hardware (no network I/O, pure in-memory formatting — same performance class as existing `renderJSON`/`renderMarkdown`).
- **Throughput:** No streaming requirement for this AC; the full findings slice is rendered into an in-memory buffer, matching every existing `Render()` format.

## Security Considerations
- **Authentication/Authorization:** None — `atcr report` is a local, offline command operating on on-disk artifacts; no new attack surface introduced by the format.
- **Input Validation:** Free-text fields (`Problem`, `Fix`, `Evidence`) must be quoted/escaped per TOON's five valid escape sequences (`\\`, `\"`, `\n`, `\r`, `\t`) so a reviewer-controlled string cannot break the payload's row/column structure or smuggle raw control characters (`\x`/`\u` escapes are explicitly invalid per the TOON spec, `documentation/toon-format-reference.md` line 43) — this also enforces the story's "zero ANSI escape sequences" requirement, since a raw `\x1b` byte cannot be represented as a valid TOON escape and must be quote-escaped or rejected.

## Test Implementation Guidance
**Test Type:** UNIT (golden-file) + UNIT (behavioral edge cases)
**Test Data Requirements:** Reuse `sample()` (`internal/report/render_test.go` line 23-31) for the golden case; add a dedicated fixture with pipe/comma/colon/newline/unicode content for the escaping edge-case tests (mirroring the pattern of `sampleWithFixWarning()`, line ~105+).
**Mock/Stub Requirements:** None — pure function over an in-memory `[]reconcile.JSONFinding` slice; no filesystem or network mocking needed for the renderer unit tests. A CLI-level integration test may additionally invoke `atcr report --format axi` against a fixture review directory and assert `grep -P '\x1b\['` finds no matches, per the story's Measurable success criterion.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/report/...`)
- [ ] No linting errors (`golangci-lint run` or project equivalent)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `FormatAXI` added to the format enum and `Render()` dispatch without altering any existing golden file's byte content
- [ ] New `report.axi` golden fixture checked in and covered by `TestRender_GoldenFiles`
- [ ] Zero-findings and escaping edge cases each have a dedicated unit test
- [ ] `atcr report --format axi` output contains no `\x1b[` sequences and no Markdown table/heading syntax, verified by an automated check

**Manual Review:**
- [ ] Code reviewed and approved
