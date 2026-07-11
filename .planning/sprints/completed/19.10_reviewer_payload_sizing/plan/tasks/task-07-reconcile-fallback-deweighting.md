# Task 07: Reconcile Fallback-Aware Distinct-Reviewer De-Weighting

**Source:** Plan 19.10 – Debt Item #5 (reconcile-side de-weighting)
**Priority:** P2 | **Effort:** M | **Type:** Add

## Problem Statement
`internal/reconcile`'s distinct-reviewer CONFIDENCE/independence calculus counts
`len(JSONFinding.Reviewers)` as the number of independent model voices behind a
finding (`internal/reconcile/disagree.go:279,295,314,364` — `atLeastOne(len(f.Reviewers))`
/ `atLeastOne(len(reviewers))`). That count assumes each named reviewer used a
distinct model. litellm `context_window_fallbacks` breaks that assumption: when a
persona overflows its context window, it can be routed to a fallback model that
may be shared across multiple personas (e.g. one universal high-context net model
backing every persona). Today reconcile has no way to know a reviewer slot was
served by a fallback, so two personas silently routed to the same fallback model
are counted as 2 independent voices instead of 1 — inflating both the
`independence` score in `disagreements.json` and the confidence semantics readers
derive from `Reviewers`.

Task 06 (fanout side) records the substitution in `summary.json` /
`AgentStatus.FallbackUsed` / `AgentStatus.FallbackFrom`
(`internal/fanout/status.go:286-295`). This task consumes that provenance: it
threads a fallback-provenance signal through the reconcile wire types and makes
`BuildDisagreements` / the independence helpers treat a fallback-served slot as
non-distinct for the model it substituted, rather than as an additional distinct
model voice.

## Solution Overview
Add a fallback-provenance field to `stream.Finding` (the record reconcile
consumes) and stamp it per source at discovery time from that source's
`AgentStatus` (mirroring the existing `PathValid`/`PathWarning` stamping pattern
in `internal/reconcile/validate.go`, not by touching the extracted library's wire
type). Propagate the provenance onto `JSONFinding` as a `Reviewers`-keyed side
field via a new post-merge stamping step (parallel to `validateFindingPaths` in
`internal/reconcile/gate.go:239`), so `JSONFindings()`
(`internal/reconcile/emit.go:184`) and the dead-but-intended
`adapter.ToJSONFinding` boundary both carry it. Then change
`internal/reconcile/disagree.go`'s independence helpers to compute a
fallback-collapsed distinct-reviewer count instead of a raw `len(Reviewers)`
count: two reviewers that both fell back to the same actual model collapse to one
independent voice.

**Design constraint — do not touch the extracted library.** `internal/reconcile`'s
`Finding` and `Merged` types are *type aliases* to
`github.com/samestrin/atcr/reconcile` (`reclib.Finding` /
`reclib.Merged` — see `internal/reconcile/lib.go:26-36`, `reconcile/finding.go:18`),
a separate, stdlib-only public library package **outside** `internal/reconcile`
and outside this task's component scope. `PathValid`/`PathWarning`/`PathSuggestion`
already establish the precedent for this exact problem: they are NOT fields on
`reclib.Finding`/`reclib.Merged` either — they live only on ATCR-internal
`stream.Finding` and `JSONFinding`, and are stamped onto `JSONFinding` post-merge
(`internal/reconcile/validate.go`, `internal/reconcile/adapter/adapter.go`'s
`ToJSONFinding(f reconcile.Finding, paths stream.Finding)` — note the `paths`
side-channel parameter). Fallback provenance follows the identical pattern: it
never becomes a field on `reclib.Finding`/`reclib.Merged`.

## Technical Implementation
### Steps
1. **`internal/stream/parser.go`** — add a fallback-provenance field to `Finding`
   (line 46), analogous to the existing `PathValid`/`PathWarning` fields: e.g.
   `FallbackFrom string` (empty when the slot was served by its configured
   primary model; the substituted-from model/persona name when fallback served
   it). Document it as NOT a wire-format column (like `PathValid`) — it is
   stamped post-parse, not read from `findings.txt`.
2. **Stamp `stream.Finding.FallbackFrom` at discovery time** — in
   `internal/reconcile/discover.go`'s `Discover`/`leafFindingsFiles` path, for
   each source read the sibling `AgentStatus` (`status.json`, written by
   `internal/fanout/artifacts.go`'s `statusFor`, consumed via Task 06's
   provenance) and, when `AgentStatus.FallbackUsed` is true, stamp
   `FallbackFrom = AgentStatus.FallbackFrom` onto every `stream.Finding` parsed
   from that source — mirroring how `internal/reconcile/validate.go`'s
   `validateFindingPaths` stamps `PathValid`/`PathWarning` post-parse rather than
   reading them from the wire. Match by source name / reviewer name; a source
   with no status.json or no fallback simply leaves the field at its zero value
   (fail-closed — treated as an independent, non-fallback voice).
3. **`internal/reconcile/emit.go`** — add a `Reviewers`-keyed provenance field to
   `JSONFinding` (line 62), e.g. `FallbackReviewers map[string]string
   \`json:"fallback_reviewers,omitempty"\`` (reviewer name -> the model/persona
   it substituted for). Update `JSONFindings()` (line 184) to populate it on the
   cached path (`RunReconcile`'s `r.jsonFindings`, gate.go) — add a
   `stampFallbackProvenance` step parallel to `validateFindingPaths`
   (`internal/reconcile/gate.go:239`) that, for each merged finding's
   `Reviewers`, looks up whether the originating per-source `stream.Finding` for
   that reviewer carried `FallbackFrom` and records it. The unstamped/derived
   branch of `JSONFindings()` (no cached `r.jsonFindings`) leaves the field empty,
   matching the existing path-validation fallback behavior for a `Result` built
   without I/O.
4. **`internal/reconcile/adapter/adapter.go`** — thread the field through the
   ATCR-internal boundary only, never through `reconcile.Finding` (the external
   library type): `ToFinding`/`FromFinding` are unchanged (they convert to/from
   the library type and must not carry ATCR-only fields, matching the existing
   `PathValid` precedent). `ToJSONFinding(f reconcile.Finding, paths
   stream.Finding)` gains a line stamping `FallbackReviewers` from `paths` the
   same way it already stamps `PathValid`/`PathWarning`/`PathSuggestion` from
   `paths`.
5. **`internal/reconcile/disagree.go`** — add a helper, e.g.
   `distinctReviewerCount(reviewers []string, fallback map[string]string) int`,
   that collapses reviewers sharing the same non-empty fallback target into a
   single voice (two reviewers with different, or empty, fallback targets each
   count individually). Replace the four `atLeastOne(len(f.Reviewers))` /
   `atLeastOne(len(reviewers))` call sites (`severitySplitItem` line 279,
   `soloItem` line 295, `verificationItem` line 314, `grayZoneItem` line 364)
   with `atLeastOne(distinctReviewerCount(f.Reviewers, f.FallbackReviewers))` (and
   the cluster-level equivalent in `grayZoneItem`, which builds its own
   `reviewers` slice from `c.Findings` — pass through the matching
   `FallbackReviewers` data from the cluster's member findings).
6. Update the `IndependenceModelReviewerCount` doc comment (line 23-29) to note
   the v1 proxy is now fallback-aware (distinct-reviewer count, collapsed by
   shared fallback target) rather than a raw name count — `disagreements.json`'s
   `independenceModel` value itself (`"distinct-reviewer-count"`) does not need
   to change, only its documented semantics.
7. Run `go build ./...` to confirm the extracted library package
   (`reconcile/finding.go`, `reconcile/merge.go`, etc.) was not touched and
   still compiles unchanged.

## Files to Create/Modify
- `internal/stream/parser.go` – modify (`Finding` struct, line 46 — add
  `FallbackFrom` field)
- `internal/reconcile/discover.go` – modify (`Discover`/leaf-file read path —
  stamp `FallbackFrom` from the source's `AgentStatus`)
- `internal/reconcile/emit.go` – modify (`JSONFinding`, line 62 — add
  `FallbackReviewers`; `JSONFindings()`, line 184)
- `internal/reconcile/gate.go` – modify (`RunReconcile`, ~line 239 — add
  `stampFallbackProvenance` alongside `validateFindingPaths`)
- `internal/reconcile/disagree.go` – modify (`IndependenceModelReviewerCount`
  doc, line 23-29; `severitySplitItem`/`soloItem`/`verificationItem`/
  `grayZoneItem` independence calc, lines 279/295/314/364)
- `internal/reconcile/adapter/adapter.go` – modify (`ToJSONFinding` — stamp
  `FallbackReviewers` from `paths`)

## Documentation Links
- [Fallback Provenance](../documentation/fallback-provenance.md)

## Related Files (from codebase-discovery.json)
- `internal/fanout/chunker.go` — existing `FallbackFrom` union pattern
  (`mergeResultGroup`, lines 233-268) this task's source-level stamping mirrors
- `internal/fanout/status.go` — `AgentStatus.FallbackUsed`/`FallbackFrom`
  (lines 286-295), the provenance this task reads
- `internal/reconcile/validate.go` — `validateFindingPaths`, the established
  post-merge stamping pattern this task's `stampFallbackProvenance` mirrors

## Success Criteria
- [x] `stream.Finding` carries a fallback-provenance field (`FallbackFrom`, `json:"-"`), stamped post-parse
      (not a wire-format column) from the source's `AgentStatus`
- [x] `JSONFinding` carries a `Reviewers`-keyed fallback-provenance field (`FallbackReviewers`),
      populated on the `RunReconcile` cached path and empty on the
      no-I/O-derived path (consistent with `PathValid`/`PathWarning`)
- [x] `internal/reconcile/adapter/adapter.go`'s `ToJSONFinding` stamps the new
      field from its `paths stream.Finding` parameter; `ToFinding`/`FromFinding`
      (which cross the external library boundary) are unchanged
- [x] A finding whose `Reviewers` includes two personas that both fell back to
      the same model is **not** double-counted as 2 distinct reviewers in
      `IndependenceModelReviewerCount`'s independence score — verified by test
- [x] A finding whose `Reviewers` includes two personas on their own configured
      (non-fallback) models continues to count as 2 distinct reviewers
      (no regression to the existing independence score)
- [x] `github.com/samestrin/atcr/reconcile` (the extracted library package) is
      not modified by this task — `reclib.Finding`/`reclib.Merged` gain no new
      field (verified: `git status` clean for `reconcile/`)
- [x] `disagreements.json`'s `independenceModel` value and schema version are
      unchanged (only the underlying count computation changes)

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `distinctReviewerCount`/independence helper: 2 reviewers, both fallback to the
  same model → count 1; 2 reviewers, fallback to different models → count 2;
  2 reviewers, one fallback + one primary → count 2; 0 reviewers → `atLeastOne`
  floors at 1 (existing behavior preserved)
- `severitySplitItem`/`soloItem`/`verificationItem`/`grayZoneItem`: independence
  score reflects the collapsed count, not `len(Reviewers)`, when
  `FallbackReviewers` is populated
- `JSONFindings()`: cached (`RunReconcile`) path carries `FallbackReviewers`;
  derived (no-I/O) path leaves it empty
- `adapter.ToJSONFinding`: `FallbackReviewers` stamped from `paths`, matching the
  existing `PathValid`/`PathWarning`/`PathSuggestion` stamping test pattern

**Integration Tests:**
- Fixture-based test per AC5: a synthetic `sources/` tree with two agent
  directories whose `status.json` both report `FallbackUsed: true,
  FallbackFrom: "same-model"` (or equivalent) for a shared finding location;
  assert `RunReconcile` → `disagreements.json`'s `independence` for that
  finding is 1, not 2, and that `summary.json`'s reviewer/agent inventory still
  lists both original persona names (fallback de-weights CONFIDENCE, it does
  not hide the substitution or collapse the original reviewer identity)

**Test Files:**
- `internal/reconcile/disagree_test.go`
- `internal/reconcile/emit_test.go`
- `internal/reconcile/adapter/adapter_test.go`
- `internal/reconcile/discover_test.go` (or `gate_test.go`, wherever the
  fixture-based AC5 integration test is added)

## Risk Mitigation
- **Silent CONFIDENCE inflation if provenance is dropped anywhere in the
  pipeline** — mitigated by stamping at the earliest point (discovery,
  mirroring `PathValid`) and propagating through a single post-merge stamping
  step (mirroring `validateFindingPaths`), with an explicit fixture test
  asserting the end-to-end `independence` value.
- **Accidental modification of the extracted library** (`reclib.Finding` /
  `reclib.Merged`) would silently widen this task's blast radius outside
  `internal/reconcile` and break the Epic 8.0 library-extraction boundary —
  mitigated by the explicit design constraint in Solution Overview and a
  `go build ./...` check that the `reconcile/` (non-`internal/`) package's
  files are untouched.
- **Fail-open on a missing/malformed `status.json`** — a source with no fallback
  data must default to "not a fallback" (each reviewer counts individually),
  never to "assume fallback" — mirrors the existing fail-closed default for
  `PathValid` on an unvalidated finding.

## Dependencies
- Task-06 (Fallback Provenance — Fanout) — supplies the `summary.json`/
  `AgentStatus.FallbackUsed`/`FallbackFrom` provenance this task reads via
  discovery-time stamping

## Definition of Done
- [x] All Success Criteria above are met
- [x] `go build ./...` passes (confirms `reconcile/` extracted library package
      is unmodified)
- [x] `go test ./...` passes
- [x] `go vet ./...` and project lint pass
- [x] Fixture-based AC5 integration test added and passing (`TestRunReconcile_FallbackDeWeightsIndependenceEndToEnd` + regression control `TestRunReconcile_NoFallbackKeepsFullIndependence`)
- [x] No change to `disagreements.json` schema version or `independenceModel`
      string value
- [x] Manual code review checklist above completed
