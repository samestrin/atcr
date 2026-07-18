# Tech Debt Captured — Sprint 31.0 AXI Compliance

Deferred findings surfaced during `/execute-sprint`. Read by `/execute-code-review`
(pre-seeded into the adversarial TD stream, tagged `SOURCE=execute-sprint`).

## TD-001 — `atcr report --format` help omits `axi` (MEDIUM)
**Origin:** Phase 1, task 1.8 GATE, 2026-07-18
**File:** cmd/atcr/report.go:21,25
**Issue:** Phase 1 added `axi` to `report.FormatList()`/`ValidFormat`, so `atcr report --format axi` works and the invalid-format error (report.go:36, via `report.Formats()`) now lists `axi`. But the command's `Short` (report.go:21) and `--format` flag help (report.go:25) are hardcoded to "md, json, checklist, or sarif" and were left stale — a self-contradicting CLI surface (error advertises axi, --help denies it, --format axi silently works).
**Why accepted:** The clean fix must also update the pinned `reportCmd.Short` assertion in cmd/atcr/main_test.go:342 (a guard owned by the unrelated quality-report story). report.go is modified in Phase 2 (task 2.2 registers the `--axi` flag there), which is the natural, non-cross-coupling place to reconcile the help text and the pinned test together.
**Fix in:** Sprint 31.0 Phase 2 (task 2.2 GREEN) — derive the `--format` help + `Short` from `report.Formats()` (mirroring the MCP `descReport`/`tools.go` dynamic pattern) so it can never drift again, and update main_test.go:342 accordingly.

## TD-002 — AXI additive-block columns mix bare and quoted-empty cell types (LOW)
**Origin:** Phase 1, task 1.8 GATE, 2026-07-18
**File:** internal/report/render.go:222,229
**Issue:** In a mixed payload the `evidence_exec.exit_code` and `verification.challenge_survived` columns emit a bare int/bool for findings that carry the block but a quoted empty string ("") for those that do not. A strictly-typed TOON tabular-array reader could reject or mis-type a column whose cells alternate between number/bool and string. The "faithful re-encoding / round-trippable" claim is asserted structurally (field-count width) but not against an actual TOON parser.
**Why accepted:** No TOON parser dependency is in-repo, and TOON tabular cells are loosely typed per-cell in the reference; the empty-vs-real distinction is deliberate (an absent exit_code must not read as 0). Round-trip validation against a real parser is better placed with Phase 4's escape/pinning work.
**Fix in:** Sprint 31.0 Phase 4 (AC 04-02) — if a strict-typed parser is adopted, add a mixed present/absent round-trip test decoding the payload; otherwise document the loose-typing assumption on the additive columns.

## TD-003 — AXI `reviewers` cell is ambiguous if a reviewer name contains a comma (LOW)
**Origin:** Phase 1, task 1.8 GATE, 2026-07-18
**File:** internal/report/render.go:210
**Issue:** `axiRow` joins reviewers with a comma and only quotes the joined value if it trips a must-quote rule; since the active delimiter is the pipe, a comma does not force quoting, so a reviewer name containing a comma would make the `reviewers` cell ambiguous to parse back into a set. This mirrors the pre-existing `joinReviewers` assumption (render.go:744) but matters more in a parse-intended machine payload.
**Why accepted:** Reviewer names in practice are persona/host identifiers without commas (the same assumption the human report already relies on). No known producer emits a comma-bearing reviewer name.
**Fix in:** Sprint 31.0 or later — if comma-bearing reviewer names become possible, quote-per-name or use a sub-delimiter that cannot appear in a name; enforce the no-comma invariant upstream at reconcile time.
