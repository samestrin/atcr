# User Story 3: AXI Pagination and Truncation Guarantees

**Plan:** [31.0: AXI Agent eXperience Interface Compliance](../plan.md)

## User Story

**As an** autonomous coding agent operator running `atcr review --axi`/`atcr report --axi` against arbitrarily large PRs or diffs
**I want** AXI-mode stdout output deterministically capped at a documented line count (default 500), with a `truncated` flag and the true total count preserved, and the cap overridable via `ATCR_AXI_MAX_LINES`
**So that** my agent process can never have its context window blown by a massive findings list or diff, and can always tell — programmatically, without guessing — whether it is looking at the complete payload or a capped subset

## Story Context

- **Background:** `atcr` already has a "never-silent deterministic truncation" contract for reviewer-fanout payloads: a byte budget (default 512 KiB, `payload_byte_budget`, resolved CLI flag > project config > registry > embedded default) with deterministic largest-first-by-size drops (ties broken by path), recorded per-agent in `status.json` via `Truncated bool` (`json:"truncated"`) and `FilesDropped []string` (`json:"files_dropped"`) — see `internal/fanout/status.go:286-293` and `docs/payload-modes.md`. Story 3 extends this exact philosophy — deterministic, never-silent truncation with an explicit flag — to the new AXI stdout payload built in Story 1, but the axis being capped is **emitted line count**, not payload bytes, since the goal is bounding what an agent must read, not what atcr sends to a model. The TOON tabular-array format chosen in Story 1 (`documentation/toon-format-reference.md`) declares each array's true element count in its header (`key[N]{...}:`), which composes naturally with a `truncated` flag: a capped payload can state the real total `N` while marking that only a subset of rows was emitted.
- **Assumptions:** Story 1 has landed a `FormatAXI` renderer in `internal/report/render.go` (and the equivalent `atcr review` live-output path) producing the payload this story wraps/truncates; the 500-line default and `ATCR_AXI_MAX_LINES` override apply uniformly to whichever payload AXI mode emits (findings lists, diffs), not solely to findings; truncation is measured in total emitted lines across the payload, not per-section.
- **Constraints:** Must reuse the existing `truncated` field name/semantics from `internal/fanout/status.go` for consistency rather than inventing a differently-named flag. The env-var override must follow the existing `ATCR_*` parsing idiom already established in `cmd/atcr/main.go` (`logLevelFromEnv` at line 288, `telemetryEnabledFromEnv` at line 306): read once per run, parse `ATCR_AXI_MAX_LINES` as an integer, and on an unset/blank/invalid value fail open to the documented default (500) with exactly one `stderr` warning — never a fatal error or a hard crash. Truncation must be deterministic (a fixed cut point, e.g. by finding priority/order already established by the renderer), never arbitrary or random, so repeated runs against the same input produce byte-identical truncated output. Must not alter the existing `internal/fanout` byte-budget truncation contract — this is a new, separate line-based cap layered on top of AXI-mode output specifically.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (`--axi` token-dense output mode must exist before it can be paginated/truncated) |

## Success Criteria (SMART Format)

- **Specific:** AXI-mode stdout output (for both `atcr review --axi` and `atcr report --axi`) is capped at 500 emitted lines by default; when the underlying payload would exceed that cap, output is deterministically truncated at the line boundary, a `truncated: true` field is present in the emitted payload, and the payload's TOON array header (or compact-JSON equivalent) still reports the true total element count `N`.
- **Measurable:** A new automated test feeds a synthetic payload exceeding 500 lines through AXI-mode rendering and asserts: (1) emitted line count is <= the configured cap, (2) `truncated` is `true` and present, (3) the declared total count in the array header equals the untruncated element count, (4) setting `ATCR_AXI_MAX_LINES=50` in the test process changes the cap to 50, and (5) an invalid value (e.g. `ATCR_AXI_MAX_LINES=notanumber`) falls back to 500 with exactly one `stderr` warning line and a zero exit code (no fatal error). A payload under the cap has `truncated: false` (or the field's documented false-case representation) and no cap-related stderr output.
- **Achievable:** Follows the `internal/fanout/status.go` `Truncated`/`FilesDropped` precedent for the flag semantics and the `cmd/atcr/main.go` `logLevelFromEnv`/`telemetryEnabledFromEnv` precedent for the env-var parsing idiom — both patterns already exist and are directly reusable, not invented from scratch.
- **Relevant:** Directly satisfies the epic's pagination/truncation requirement and is the specific guarantee that prevents a large PR's findings/diff from silently overflowing a calling agent's context window — the epic's stated primary risk for "other autonomous agents" consuming `atcr` output.
- **Time-bound:** Implementable within a single sprint cycle once Story 1's AXI renderer exists; delivered as a wrapping truncation step applied uniformly to AXI-mode output rather than a per-command reimplementation.

## Acceptance Criteria Overview

1. AXI-mode output (both `atcr review --axi` and `atcr report --axi`) enforces a default 500-emitted-line cap, truncating deterministically (fixed, reproducible cut point) rather than arbitrarily when the payload exceeds the cap.
2. A `truncated` boolean field (reusing the exact field name/semantics from `internal/fanout/status.go`'s `Truncated bool` / `json:"truncated"`) is present in every AXI payload, `true` when capped and `false` otherwise, while the payload's declared total count (e.g. TOON's `key[N]{...}:` header) always reflects the true, uncapped total.
3. The cap is overridable via the `ATCR_AXI_MAX_LINES` environment variable, parsed once per run following the existing `ATCR_*` idiom in `cmd/atcr/main.go`: unset/blank/invalid values fail open to the default (500) with exactly one `stderr` warning, never a fatal error.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`_

## Technical Considerations

- **Implementation Notes:** Implement the cap as a wrapping writer or post-processing step applied uniformly to AXI-mode output (per plan.md's Implementation Strategy), rather than threading truncation logic into each renderer/command individually. Add an `axiMaxLinesFromEnv() int` function in `cmd/atcr/main.go` mirroring `logLevelFromEnv`/`telemetryEnabledFromEnv`'s structure (read `os.Getenv("ATCR_AXI_MAX_LINES")` once, `strconv.Atoi`, warn-and-default on parse failure). Truncation must pick a deterministic cut point — e.g. truncate the TOON tabular array's rows array at row index `cap`, preserving header and closing structure — so the same input always yields byte-identical truncated output. Reuse `truncated` as the JSON/TOON field name for direct conceptual continuity with `internal/fanout/status.go`.
- **Integration Points:** `internal/report/render.go` (`FormatAXI` renderer from Story 1 — the wrapping/truncation step consumes its output), `cmd/atcr/main.go` (new `axiMaxLinesFromEnv`, alongside `logLevelFromEnv`/`telemetryEnabledFromEnv`), `cmd/atcr/review.go`/`cmd/atcr/resume.go` (live AXI output path also needs the cap applied, per Story 1's scope of both code paths), `internal/fanout/status.go` (semantic/naming precedent for `truncated`, not a shared implementation — the byte-budget truncation there is a distinct mechanism).
- **Data Requirements:** No new data collection. The cap operates on the already-rendered AXI payload (line-counting and truncating text/structured output), and the TOON array header's `N` value is the pre-truncation element count already known to the renderer before any lines are dropped.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Truncation logic is implemented per-command (duplicated in `atcr review` and `atcr report` paths) rather than as a single shared wrapper, causing the two commands to diverge in cap behavior or the `truncated` flag's semantics | Medium | Implement one shared truncation step (wrapping writer or post-processor) consumed by both code paths, per plan.md's Implementation Strategy, rather than reimplementing per command |
| An invalid `ATCR_AXI_MAX_LINES` value is treated as a fatal/usage error instead of failing open, breaking the "never hard-error on a misconfigured env var" posture already established by `telemetryEnabledFromEnv` | Medium | Mirror `telemetryEnabledFromEnv`'s exact fail-open pattern: parse failure logs one `stderr` warning and falls back to the documented default (500), never a non-zero exit or fatal error |
| The line cap is applied to rendered text after the TOON header's `N` is already fixed at a stale value, so the "true total" claim in the array header silently drifts from what a naive truncation step would produce if it clipped the header along with the rows | Low | Compute the array header's `N` from the pre-truncation element count and apply truncation only to the row/body section, verified by a dedicated test asserting header `N` != emitted row count when `truncated: true` |

---

**Created:** July 18, 2026 09:03:31AM
**Status:** Draft - Awaiting Acceptance Criteria
