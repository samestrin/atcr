# User Story 4: AXI Stderr Isolation and Escape-Sequence Guarantee

**Plan:** [31.0: AXI Agent eXperience Interface Compliance](../plan.md)

## User Story

**As an** autonomous coding agent operator piping `atcr review --axi` into a file or downstream parser (e.g. `atcr review --axi > findings.json`)
**I want** stdout to contain nothing but the AXI payload — no human progress lines, no summary text, and no raw ANSI/OSC terminal escape sequences — while all diagnostic/progress output goes to stderr
**So that** I can trust `findings.json` to be a clean, directly-parseable artifact without a sanitization pass of my own

## Story Context

- **Background:** `atcr` already has a codebase-wide stderr-only diagnostics invariant: `internal/log` wraps `log/slog`, the root logger is constructed once in `setupLogger()` (`cmd/atcr/main.go:329`) writing to `cmd.ErrOrStderr()`, and subcommands retrieve it via `log.FromContext` (documented in `docs/logging.md`). This story does not rebuild that invariant — it closes a specific, verified gap: `atcr review`'s live human-readable progress/summary output writes directly to `cmd.OutOrStdout()` at multiple call sites in `cmd/atcr/review.go` (lines 433, 436, 551, 573, 591, 602) and `cmd/atcr/resume.go` (lines 153, 170, 188, 195, 259), including two separate callers of the shared `writeReviewSummary` function (`cmd/atcr/review_summary.go:80`). This is a distinct code path from `internal/report`'s findings renderer that `atcr report` uses (addressed in Story 1), so gating only the report renderer would leave `atcr review --axi`'s primary stdout polluted with human-oriented text, defeating the epic's "findings.json must be perfectly clean" requirement. Separately, `atcr` has a known counterexample proving it can emit raw terminal escape sequences on some paths: `osc8()` (`cmd/atcr/quickstart.go:456`, called from line 194) wraps URLs in OSC-8 hyperlink escapes. `--axi` mode must guarantee, not merely assume, that no such sequence ever reaches stdout.
- **Assumptions:** Story 1's `--axi`/AXI-mode flag and its command-context propagation mechanism (threaded through `PersistentPreRunE`, the same injection point used for the logger/telemetry client) already exist and are available to gate on in `cmd/atcr/review.go` and `cmd/atcr/resume.go`. The `osc8()` path (`cmd/atcr/quickstart.go`) is invoked from `atcr quickstart`, not `atcr review`/`atcr report`, but the guarantee this story delivers must be structural/testable enough to cover any current or future stdout write under `--axi`, not just the two files named above.
- **Constraints:** Must not alter stderr-side logging behavior (`internal/log`, `setupLogger()`) — this story only changes what is gated to stdout under `--axi`. Must not break existing non-`--axi` behavior of `cmd/atcr/review.go` or `cmd/atcr/resume.go` (human progress/summary text must remain unchanged when `--axi` is not passed). Must reuse the existing `sanitizeDisplay` idiom (`cmd/atcr/models.go:288-296`, strips control characters via `strings.Map` + `unicode.IsControl`, already pinned by `TestDriftLine_StripsControlChars` and `TestRenderPersonaSearch_StripsControlChars`) rather than inventing a parallel sanitization mechanism.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (requires the `--axi` flag and its command-context propagation mechanism to exist) |

## Success Criteria (SMART Format)

- **Specific:** Under `--axi`, every `cmd.OutOrStdout()` write in `cmd/atcr/review.go` and `cmd/atcr/resume.go` (including both callers of `writeReviewSummary`) is gated so that only the AXI payload (or nothing, if the payload is emitted elsewhere in the flow) reaches stdout, and a pinning test proves no ANSI/OSC escape byte sequence can appear in `--axi` stdout output.
- **Measurable:** `atcr review --axi > findings.json` on a piped/non-TTY invocation produces a `findings.json` containing zero occurrences of human progress/summary strings (e.g. "resuming review", "agents succeeded", "reconciled") and zero ANSI/OSC escape bytes (`\x1b`), verified by an automated test asserting on captured `cmd.OutOrStdout()` content across both the fresh-review and resume code paths.
- **Achievable:** Reuses the existing gating pattern already established for the logger/telemetry client injection in `PersistentPreRunE` and the existing `sanitizeDisplay` control-character-stripping idiom — no new sanitization mechanism needs to be designed.
- **Relevant:** Directly satisfies the epic's explicit acceptance criterion "Stderr is strictly used for progress bars/logs; stdout contains only the final payload," and is the specific gap the plan's refined understanding identified as unaddressed by the pre-existing stderr-diagnostics invariant.
- **Time-bound:** Deliverable within a single sprint cycle as Story 4 of 5; depends only on Story 1's flag/propagation plumbing, not on Stories 2 or 3.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-review-resume-stdout-gating.md) | Gate Human-Oriented Stdout Writes in review.go and resume.go Under AXI Mode | Unit/Integration |
| [04-02](../acceptance-criteria/04-02-escape-sequence-pinning-test.md) | Pinning Test Guarantees No ANSI/OSC Escape Sequences Reach `--axi` Stdout | Unit |
| [04-03](../acceptance-criteria/04-03-non-axi-regression-protection.md) | Non-AXI `review`/`resume` Behavior Remains Unchanged | Unit/Integration |

## Original Criteria Overview

1. Every human-oriented `cmd.OutOrStdout()` write in `cmd/atcr/review.go` (fresh-review path) and `cmd/atcr/resume.go` (resume path), including both call sites of `writeReviewSummary`, is gated behind AXI mode so none of that text reaches stdout when `--axi` is active.
2. A pinning test (in the style of `TestDriftLine_StripsControlChars` / `TestRenderPersonaSearch_StripsControlChars`) proves that no ANSI/OSC escape sequence can appear in `--axi` stdout output, covering at minimum the `osc8()`-style escape pattern as a regression guard.
3. Non-`--axi` invocations of `atcr review` and `atcr resume` are unaffected — existing human progress/summary output and its current test coverage remain unchanged.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/31.0_axi_compliance/`_

## Technical Considerations

- **Implementation Notes:** Gate each `fmt.Fprintf`/`fmt.Fprintln` call to `cmd.OutOrStdout()` in `cmd/atcr/review.go` (lines 433, 436, 551, 573, 591, 602) and `cmd/atcr/resume.go` (lines 153, 170, 188, 195, 259) on the AXI-mode value from command context established in Story 1, mirroring the existing conditional pattern used elsewhere for mode-dependent output. `writeReviewSummary` (`cmd/atcr/review_summary.go:80`) is shared between both callers, so gating can be applied either at the call sites or by having the function itself become a no-op (or emit nothing additional) under AXI mode — pick whichever keeps the function's existing non-AXI contract and tests intact. For the escape-sequence guarantee, add a pinning test that asserts captured stdout under `--axi` contains no bytes matching an ANSI/OSC escape pattern (e.g. `\x1b\[` / `\x1b\]`), following the existing `sanitizeDisplay`-adjacent test precedent rather than introducing a new sanitization layer, since AXI's structured TOON/JSON payload (from Story 1) should not itself contain escape sequences by construction.
- **Integration Points:** `cmd/atcr/review.go` (fresh-review live progress/summary writes), `cmd/atcr/resume.go` (resume live progress/summary writes), `cmd/atcr/review_summary.go` (`writeReviewSummary`, shared by both callers), `cmd/atcr/models.go` (`sanitizeDisplay` precedent for control-character/escape stripping), Story 1's AXI-mode flag/command-context propagation mechanism.
- **Data Requirements:** None — this story changes what is written to stdout, not the underlying review/resume data model.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A stdout write in `cmd/atcr/review.go` or `cmd/atcr/resume.go` is missed during gating (e.g. a future write added after this story ships), silently reintroducing polluted stdout under `--axi` | High | Cover both files' call sites explicitly by line reference in this story's AC; add the escape-sequence/human-text pinning test so any future regression is caught by CI rather than discovered by a downstream agent consumer |
| The `writeReviewSummary` gating is applied inconsistently between its two callers (`cmd/atcr/review.go:436` and `cmd/atcr/resume.go:195`), leaving one path clean and the other polluted | Medium | Gate at the shared function level where practical, or add a test exercising both the fresh-review and resume invocation paths under `--axi` |
| Reusing `sanitizeDisplay` for the escape-sequence guarantee is insufficient if a future stdout write bypasses it entirely (e.g. a new raw `fmt.Fprint` call) | Medium | The primary guarantee should come from AXI stdout being restricted to the structurally-escape-free TOON/JSON payload (Story 1) rather than relying solely on post-hoc sanitization; the pinning test acts as a regression backstop, not the sole defense |

---

**Created:** July 18, 2026 09:03:31AM
**Status:** Draft - Awaiting Acceptance Criteria
