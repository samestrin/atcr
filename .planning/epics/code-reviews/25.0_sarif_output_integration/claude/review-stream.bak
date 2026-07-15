# Code Review Stream - 25.0_sarif_output_integration (Epic)

**Started:** July 14, 2026 08:40:01PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests if enabled]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: `atcr report --format=sarif` produces valid SARIF JSON
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/report.go:25`, `internal/report/render.go:37,46,63-64`, `internal/report/sarif.go:101-131`
- **Notes:** `--format` flag lists `sarif`; `ValidFormat`/`Formats` include `FormatSarif`; `Render` routes `FormatSarif` → `renderSarif`, which emits a SARIF 2.1.0 log (json.MarshalIndent, nil-slice guards, trailing newline). Output shape validated against `internal/report/testdata/sarif-schema-2.1.0.json` and golden `report.sarif.json` in `sarif_test.go`.

### Criterion: SARIF output correctly maps ATCR severities to SARIF levels
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/report/sarif.go:173-194`
- **Notes:** `sarifLevel` derives branches from the canonical `reclib.SeverityRank` rubric (via `NormalizeSeverity`) — CRITICAL/HIGH → error, MEDIUM → warning, LOW → note; unrecognized non-empty token → warning + stderr diagnostic. Never emits "none"/empty. Single severity-comparison site (avoids TD-0052 desync).

### Criterion: File paths and line numbers correctly anchor to the git diff
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/report/sarif.go:210-221`
- **Notes:** `sarifLocation` sets artifactLocation.uri = f.File verbatim (repo-root-relative by the report layer) and startLine/endLine = f.Line. File-level findings (Line <= 0) synthesize a full 1,1,1,1 region (GitHub Code Scanning requires all four region fields). endColumn=2 for line>0 (SARIF exclusive-endColumn).

### Criterion: Documentation example shows CI integration (GitHub Code Scanning + GitLab CI SAST)
- **Verdict:** VERIFIED ✅
- **Evidence:** `docs/ci-integration.md:47-98`
- **Notes:** "SARIF Upload for Code Scanning" section documents GitHub (`github/codeql-action/upload-sarif@v3` with `security-events: write`) and GitLab (`artifacts:reports:sast`). Both pipe `atcr review && atcr reconcile && atcr report --format=sarif > results.sarif`, consistent with the existing two-step pattern.

## Adversarial Analysis (Discovery Mode — Epic)

**Mode:** Verification + Discovery (no epic-level risk profile)
**Files Reviewed:** 4 (internal/report/sarif.go, internal/report/render.go, cmd/atcr/report.go, internal/mcp/tools.go)
**Issues Found:** 5 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic mode)

### Verification Notes
- **DROPPED (false-positive):** Both agents flagged the file-level `1,1,1,1` "zero-length region" as an inconsistency. Verified against `acceptance-criteria/03-02-file-level-fallback-anchoring.md` — the `1,1,1,1` region is the explicitly specified, tested, and documented behavior (GitHub Code Scanning requires all four region fields non-zero to display; synthesizing over omission is an accepted trade-off). Implemented to spec — not a defect.
- **DROPPED (inaccurate):** Agent 2's claim that `internal/mcp/doc.go` hardcodes/omits `sarif` — verified false; doc.go does not enumerate formats.

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 1
- Low: 4

All 5 surviving items are non-blocking hardening/maintainability follow-ups (empty/absolute uri guard, empty-Category ruleId, MCP enum drift, MCP-path diagnostic visibility, sarifDiag latent race). None affect the 4 acceptance criteria, all of which are VERIFIED.
