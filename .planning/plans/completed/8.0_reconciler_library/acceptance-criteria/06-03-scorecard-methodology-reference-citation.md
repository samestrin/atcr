# Acceptance Criteria: Scorecard Methodology Cites Standalone Reference Implementation

**Related User Story:** [06: Independent Module CI and Leaderboard Reference Citation](../user-stories/06-independent-module-ci-leaderboard-citation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (additive note) | `docs/scorecard.md` — the scorecard/leaderboard methodology doc that feeds Epic 10.0 |
| Test Framework | manual review + grep/diff verification | the citation is a text reference, not executable code |
| Key Dependencies | module path `github.com/samestrin/atcr/reconcile` (must match `reconcile/go.mod` exactly) | `docs/leaderboard-methodology.md` does NOT exist; the citation lands in `docs/scorecard.md` |
| Sequencing | Citation lands in the implementation phase AFTER the module exists | references a real module path, not a planned one (Phase-0 resolution) |

### Related Files (from codebase-discovery.json)
- `docs/scorecard.md` - modify: add an additive note citing `github.com/samestrin/atcr/reconcile` as the standalone reference implementation backing every scorecard record; the note states it can be run and inspected independently.
- `reconcile/go.mod` - read: the module path cited in the doc must match the `module github.com/samestrin/atcr/reconcile` line in `go.mod` exactly so the citation resolves.
- `reconcile/README.md` - create: the module's own documentation of the public API the citation points consumers toward; the scorecard note links or points at the same import path.
- `docs/verification.md` - reference: existing cross-linked doc in the scorecard's `## Related` section; the new citation follows the same additive, cross-link style rather than restructuring the methodology.

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — the scorecard methodology references the deterministic reconciler that implements the verification contract defined here.

## Happy Path Scenarios
**Scenario 1: Scorecard doc cites the module as the reference implementation**
- **Given** `docs/scorecard.md` describes the scorecard/leaderboard methodology and feeds Epic 10.0
- **When** the additive note is added
- **Then** the doc states that the deterministic reconciler backing every scorecard record is the standalone reference implementation `github.com/samestrin/atcr/reconcile`, and that it can be run and inspected independently

**Scenario 2: Cited module path matches `go.mod` exactly**
- **Given** `reconcile/go.mod` declares `module github.com/samestrin/atcr/reconcile`
- **When** the citation in `docs/scorecard.md` is inspected
- **Then** the path string matches the `go.mod` module path exactly (byte-for-byte), so the reference resolves for anyone who imports or inspects the module

**Scenario 3: Citation is additive and does not restructure the methodology**
- **Given** the existing `docs/scorecard.md` structure (Record Schema, Storage, CLI Usage, Privacy Model, Schema versioning, Related)
- **When** the note is added
- **Then** it is a short additive note (not a restructure) — the existing methodology, schema, and privacy model sections are unchanged; the note makes the reconciler reference explicit rather than implicit

**Scenario 4: Citation lands after the module exists**
- **Given** the module scaffold at `./reconcile/` with its own `go.mod` is in place (Stories 1-3)
- **When** the `docs/scorecard.md` edit lands
- **Then** it references a real, importable module path — not a planned one — satisfying the Phase-0 resolution that the citation cannot land before the module exists

## Edge Cases
**Edge Case 1: Citation notes the `replace` directive development bridge**
- **Given** the module is consumed via a root `replace` directive during extraction and is not yet published to a separate repo
- **When** the citation is written
- **Then** it notes that `github.com/samestrin/atcr/reconcile` is the intended public import path and that the `replace` directive is the documented development-time bridge, with separate-repo publication following extraction

**Edge Case 2: Citation distinguishes the reconciler from ATCR's path-validation**
- **Given** the standalone module is stdlib-only and excludes ATCR's path-validation/I/O machinery
- **When** the note describes the reference implementation
- **Then** it identifies the deterministic reconciler (clustering, dedupe, merge, confidence) — not ATCR's full review pipeline — as the reference backing the scorecard metrics

**Edge Case 3: Citation integrates with the existing `## Related` cross-links**
- **Given** `docs/scorecard.md` already cross-links `docs/verification.md` and `docs/findings-format.md` in `## Related`
- **When** the citation is added
- **Then** it follows the same additive cross-link style (a note or `## Related` entry pointing at the module), keeping the doc's existing navigation pattern

**Edge Case 4: Epic 10.0 leaderboard consumes the cited reference**
- **Given** Epic 10.0 (Model-Eval Leaderboard) consumes `docs/scorecard.md`
- **When** the leaderboard references the methodology
- **Then** the citation makes the reconciler reference explicit so the leaderboard's credibility claim (deterministic, inspectable, reproducible) is supported by a documented module path

## Error Conditions
**Error Scenario 1: Cited path does not match `go.mod`**
- Error message: `docs/scorecard.md cites github.com/samestrin/atcr/Reconcile (wrong case/path) but go.mod declares github.com/samestrin/atcr/reconcile`
- HTTP status / error code: N/A — a grep check comparing the cited string against `reconcile/go.mod`'s module path fails the doc verification

**Error Scenario 2: Citation lands before the module exists**
- Error message: `docs/scorecard.md references github.com/samestrin/atcr/reconcile but ./reconcile/go.mod does not exist yet`
- HTTP status / error code: N/A — the Phase-0 resolution forbids citing a planned path; the edit must land after the module scaffold (Stories 1-3)

**Error Scenario 3: Citation restructures the methodology**
- Error message: the edit rewrites or reorders existing sections (Record Schema, Privacy Model, etc.) instead of adding a note
- HTTP status / error code: N/A — the edit must be additive; a diff showing restructured sections fails review

**Error Scenario 4: Citation points at the wrong doc (`leaderboard-methodology.md`)**
- Error message: the note is added to `docs/leaderboard-methodology.md` (which does not exist) instead of `docs/scorecard.md`
- HTTP status / error code: N/A — the citation must land in `docs/scorecard.md`, the actual methodology doc

## Performance Requirements
- **Response Time:** N/A — static documentation edit.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** N/A — public documentation.
- **Input Validation:** The cited module path must match `reconcile/go.mod`'s `module` line exactly so the reference resolves. The note must not embed credentials, internal hostnames, or private paths (the module path is public).
- **Reproducibility:** The citation is the credibility anchor for the leaderboard — it must point at a real, inspectable module so third parties can run and verify the reconciler independently. Note the `replace` directive development bridge so readers understand the path is the intended public import path.

## Test Implementation Guidance
**Test Type:** UNIT (doc verification script)
**Test Data Requirements:** The `module` line from `reconcile/go.mod` and the citation text in `docs/scorecard.md`.
**Mock/Stub Requirements:** None. Implement a verification step (make target or `.githooks` step) that: (1) greps `docs/scorecard.md` for `github.com/samestrin/atcr/reconcile`; (2) extracts the `module` path from `reconcile/go.mod`; (3) asserts the cited string matches the `go.mod` module path exactly; (4) asserts the module exists (`test -f reconcile/go.mod`) so the citation never references a planned path.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (doc verification script passes; `go test ./reconcile/...` still green)
- [ ] No linting errors
- [ ] Build succeeds (the doc edit does not affect the build)

**Story-Specific:**
- [ ] `docs/scorecard.md` contains an additive note citing `github.com/samestrin/atcr/reconcile` as the standalone reference implementation (AC#8)
- [ ] The cited path matches `reconcile/go.mod`'s `module` line exactly (byte-for-byte)
- [ ] The note states the reconciler can be run and inspected independently
- [ ] The citation lands after the module exists (references a real path, not a planned one)
- [ ] The edit is additive — existing methodology/schema/privacy sections are unchanged
- [ ] The note documents the `replace` directive development bridge and that separate-repo publication follows extraction

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Confirm the citation lives in `docs/scorecard.md` (not the non-existent `docs/leaderboard-methodology.md`)
