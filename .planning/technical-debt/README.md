# Technical Debt Tracking

This file is a staging area for small technical debt items discovered during development. Items are triaged and moved to individual sprint documents in `sprints/` as they are prioritized.

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 3 | 0 | 0 |
| MEDIUM | 6 | 13 | 0 |
| LOW | 43 | 9 | 16 |
| SEVERITY | 1 | 0 | 0 |


**Last Modified:** 2026-06-16 | **Open Items:** 53 | **Deferred Items:** 22 | **Resolved Items:** 16 | **Total Items:** 91

## Directory Structure

```
technical-debt/
├── README.md                    # This file (staging area)
├── CLAUDE.md                    # AI assistant guidelines
└── sprints/
    ├── active/                  # Currently being addressed
    ├── pending/                 # Prioritized, not yet started
    └── completed/               # Resolved items
```

## How to Use

1. **Small items**: Add to this README under "Staging Area" below
2. **Larger items**: Create a new document in `sprints/pending/`
3. **During sprint planning**: Move items from pending to active
4. **After resolution**: Move items from active to completed


### [2026-06-16] From Sprint: 3.5_severity-rank-consolidation

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [x] | LOW | internal/boundaries_test.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 2 | [x] | LOW | internal/boundaries_test.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 2 | [x] | LOW | internal/boundaries_test.go:38 | Unnecessary blank line after verify entry | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 2 | [x] | LOW | internal/boundaries_test.go:45 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 3 | [x] | LOW | internal/fanout/postprocess.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 3 | [x] | LOW | internal/fanout/postprocess.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 3 | [ ] | LOW | internal/fanout/postprocess.go:25 | Unnecessary blank line after function declaration | Remove stream.NormalizeSeverity call | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 3 | [ ] | LOW | internal/fanout/postprocess.go:25 | Comparison lacks braces for single-line if | Remove stream.NormalizeSeverity call | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 3 | [ ] | MEDIUM | internal/fanout/postprocess.go:25 | Redundant normalization adds overhead (disagreement: LOW vs MEDIUM) | Remove stream.NormalizeSeverity call | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 3 | [ ] | LOW | internal/fanout/postprocess.go:36 | Comparison lacks braces for single-line if | Remove stream.NormalizeSeverity call | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 3 | [ ] | MEDIUM | internal/fanout/postprocess.go:36 | Redundant normalization adds overhead (disagreement: LOW vs MEDIUM) | Remove stream.NormalizeSeverity call | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 3 | [x] | LOW | internal/fanout/postprocess.go:62 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/disagree.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/disagree.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | MEDIUM | internal/reconcile/disagree.go:282 | Epic 3.5 normalized severity lookups only at the scoped boundary sites (merge.go:104 grayZoneItem disagree.go:338/360) but left disagree.go sibling finding-level lookups raw: spreadFromDisagreement (248-249) severitySplitItem (282) soloItem (298) verificationItem (322) sortDisagreements tiebreak (463) - severity is never normalized at parse time (parser.go:193 stores f[0] verbatim). On mixed-case input a real HIGH solo scores rank 0 and sorts dead-last and the radar-JSON ordering diverges from the normalized report-view ordering. Latent desync on the exact non-canonical-casing surface the epic set out to eliminate. | Wrap each raw SeverityRank lookup in disagree.go in stream.NormalizeSeverity: lines 248-249 (replace strings.TrimSpace with stream.NormalizeSeverity which trims and upper-cases) 282 298 322 and 463. Add a radar test feeding a lowercase-severity JSONFinding and asserting a non-zero solo score plus an ordering that matches render.go. Either normalize all reconcile lookup sites or none - do not leave the file half-migrated. | correctness | 30 | code-review | claude | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/disagree.go:336 | Unnecessary blank line after variable declaration | Assign stream.NormalizeSeverity(f.Severity) to maxSev for consistency with merge.go | maintainancy | 5 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/disagree.go:336 | Comparison lacks braces for single-line if | Assign stream.NormalizeSeverity(f.Severity) to maxSev for consistency with merge.go | maintainancy | 5 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | MEDIUM | internal/reconcile/disagree.go:336 | maxSev stores raw f.Severity instead of normalized form | Assign stream.NormalizeSeverity(f.Severity) to maxSev for consistency with merge.go | maintainancy | 5 | code-review | greta | MEDIUM |
| 4 | [ ] | HIGH | internal/reconcile/disagree.go:336 | Double normalization wastes cycles (disagreement: LOW vs HIGH) | Assign stream.NormalizeSeverity(f.Severity) to maxSev for consistency with merge.go | maintainancy | 5 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/disagree.go:355 | Unnecessary blank line after variable declaration | Add braces for consistency | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | HIGH | internal/reconcile/disagree.go:360 | Double normalization wastes cycles (disagreement: MEDIUM vs HIGH) | Use stream.NormalizeSeverity(maxSev) consistently | performance | 10 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | MEDIUM | internal/reconcile/disagree.go:360 | Potential rank lookup failure for maxSev | Use stream.NormalizeSeverity(maxSev) consistently | performance | 10 | code-review | otto | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/disagree.go:365 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/merge.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/merge.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | HIGH | internal/reconcile/merge.go:103 | Returning normalized severity breaks caller expectations (disagreement: LOW vs HIGH) | Remove the per-finding NormalizeSeverity call; normalize only at the true boundary (merge.go:104 raw reviewer stream.Finding) | maintainancy | 30 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | MEDIUM | internal/reconcile/merge.go:103 | NormalizeSeverity called on every finding in mergeSeverity loop but findings already arrive canonical from upstream | Remove the per-finding NormalizeSeverity call; normalize only at the true boundary (merge.go:104 raw reviewer stream.Finding) | maintainancy | 30 | code-review | bruce | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/merge.go:103 | Unnecessary blank line and comparison lacks braces for single-line if | Remove the per-finding NormalizeSeverity call; normalize only at the true boundary (merge.go:104 raw reviewer stream.Finding) | maintainancy | 30 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/merge.go:124 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/reconcile.go:108 | sortMerged (reconcile.go:108) and AtOrAbove (gate.go:45,49) do raw SeverityRank lookups - correct today only because mergeSeverity canonicalizes Merged.Severity upstream (merge.go:116) making merge.go normalization silently load-bearing for two other files. Every other consumer normalizes at the lookup site; reconcile sort/gate are lone outliers trusting upstream canonicalization - an invisible coupling a future simplification of mergeSeverity would break. | Normalize at the lookup site in sortMerged (reconcile.go:108) and AtOrAbove (gate.go:49 and threshold at :45) using SeverityRank[stream.NormalizeSeverity(...)] so each consumer is self-defending. Verify the gate test with a lowercase merged severity. This makes mergeSeverity normalization a convenience rather than a hidden correctness dependency. | maintainability | 30 | code-review | claude | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_cons |  |  |  | 0 | code-review | bob2-backup | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:11 | Comparison lacks braces for single-line if | Add trailing comma after last element | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:18 | Test name too long | Shorten to TestMergeNormalizesMixedCase | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:23 | Comparison lacks braces for single-line if | Add trailing comma after last element | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:30 | Test name too long | Shorten to TestGrayZoneNormalizesMixedCase | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:34 | Comparison lacks braces for single-line if | Add trailing comma after last element | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:42 | Test name too long | Shorten to TestMergeNoDisagreementOnCase | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [ ] | LOW | internal/reconcile/severity_consolidation_test.go:58 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render.go:122 | severityRankOf now calls stream.NormalizeSeverity (ToUpper+TrimSpace allocating a new string) invoked inside sort.SliceStable comparator on both operands per comparison O(n log n) times. The pre-3.5 version did a bare reconcile.SeverityRank[s] lookup with no allocation so the consolidation introduced a minor per-compare allocation regression in the render hot path. | Decorate-sort: precompute rank := severityRankOf(f.Severity) once per finding into a parallel slice before sorting then compare cached ints in the closure. Verify existing render tests still pass; optional micro-benchmark on a few-hundred-finding slice. Low priority - only matters at high finding counts. | performance | 15 | code-review | claude | MEDIUM |
| 5 | [ ] | LOW | internal/report/render.go:204 | writeSummaryGrid buckets findings with counts[f.Severity] using raw severity against a map keyed by canonical reconcile.SevCritical/SevHigh/... constants. A mixed-case "high" from hand-edited findings.json misses its bucket and falls into OTHER row even though severityRankOf ranks it correctly as HIGH. The summary grid and findings list disagree on what counts as a known severity - the consolidation touched severityRankOf for exactly this casing reason but left its sibling counter un-normalized. | Normalize the bucket key: c ok := counts[canonicalize(f.Severity)] - a canonicalize helper already exists at render.go:411 and matches NormalizeSeverity. Add a render test with a lowercase severity asserting it lands in its canonical bucket not OTHER. | correctness | 15 | code-review | claude | MEDIUM |
| 5 | [ ] | LOW | internal/report/render.go:415 | Unnecessary blank line after function declaration | Add braces for consistency | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render.go:424 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render_test.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render_test.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render_test.go:286 | Comparison lacks braces for single-line if | Add braces for consistency | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render_test.go:290 | Comparison lacks braces for single-line if | Add test for mixed-case severity input | maintainancy | 5 | code-review | Reviewer | MEDIUM |
| 5 | [ ] | LOW | internal/report/render_test.go:302 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 6 | [x] | LOW | internal/stream/severity.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 6 | [ ] | LOW | internal/stream/severity.go:5 | Unnecessary blank line after package declaration | Add comment about read-only after init | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 6 | [x] | LOW | internal/stream/severity.go:14 | Map initialization lacks trailing comma | Add trailing comma after last element | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 6 | [ ] | LOW | internal/stream/severity.go:22 | The NormalizeSeverity doc-comment claims "Every consumer normalizes through this single helper so their lookups stay identical and the fan-out/reconcile casing asymmetry cannot reappear." That invariant is not held: disagree.go (248 282 298 322 463) reconcile.go (108) and gate.go (45 49) still do raw un-normalized SeverityRank lookups. A misleading comment is worse than none - a future reader will trust the guarantee and skip defensive normalization. | Either make the claim true by normalizing the remaining raw lookup sites or soften the wording to "consumers SHOULD normalize." Verify by grepping SeverityRank[ for any key not wrapped in NormalizeSeverity. Prefer making it true (bundle with the disagree.go and reconcile/gate items). | maintainancy | 15 | code-review | greta, claude, Reviewer | HIGH |
| 6 | [x] | LOW | internal/stream/severity_test.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 6 | [ ] | LOW | internal/stream/severity_test.go:5 | Unnecessary blank line after package declaration | Remove blank line | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 6 | [ ] | LOW | internal/stream/severity_test.go:11 | Comparison lacks braces for single-line if | Add trailing comma after last element | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 6 | [ ] | LOW | internal/stream/severity_test.go:22 | Test lacks edge case for whitespace-only input | Add trailing comma after last element | maintainancy | 5 | code-review | Reviewer | MEDIUM |
| 6 | [x] | LOW | internal/stream/severity_test.go:43 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| 7 | [x] | LOW | internal/verify/severity.go:1 | Missing package documentation | Add package comment describing purpose | maintainancy | 3 | code-review | Reviewer | MEDIUM |
| 7 | [ ] | LOW | internal/verify/severity.go:5 | Unnecessary blank line after function declaration | Add debug log when minSeverity is unknown | maintainancy | 5 | code-review | Reviewer | MEDIUM |
| 7 | [x] | LOW | internal/verify/severity.go:17 | Missing final newline | Add newline at end of file | maintainancy | 1 | code-review | Reviewer | MEDIUM |
| U | [ ] | SEVERITY | FILE:LINE | PROBLEM | FIX | CATEGORY | 0 | SOURCE | REVIEWERS | CONFIDENCE |

### [2026-06-16] From Sprint: epic-3.5

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [x] | LOW | internal/reconcile/merge.go:23 | reconcile.SeverityRank is a map-header alias of stream.SeverityRank so both names share one backing map; a mutation through either package would corrupt the other, relying on a read-only invariant that is documented but not enforced. | Add a test asserting the canonical map is never mutated, or expose rank via a getter to remove the shared mutable surface. | REGRESSION_RISK | 15 | execute-epic-independent |
| 1 | [x] | LOW | internal/reconcile/disagree.go:248 | spreadFromDisagreement does a raw non-normalized SeverityRank lookup on the parsed "<lo> vs <hi>" string; mergeSeverity now embeds canonical severities so mixed-case disagreements compute a real nonzero spread (intended behavior, but unpinned by a test). | Add a test pinning that a mixed-case severity split yields the canonical spread so the new behavior is locked. | REGRESSION_RISK | 15 | execute-epic-independent |
| 1 | [x] | LOW | internal/reconcile/merge.go:120 | When no group member has a known severity, mergeSeverity falls back to raw group[0].Severity verbatim while every known path now returns the normalized form, so merged Severity casing is inconsistent between the known and all-unknown cases. | Normalize the fallback too (max = stream.NormalizeSeverity(group[0].Severity)) or document that the all-unknown fallback is deliberately verbatim. | EDGE_CASES | 10 | execute-epic-independent |
| U | [ ] | LOW | internal/registry/config.go:128 | registry.normalizeSeverity is a 4th identical upper+trim copy of the canonical stream.NormalizeSeverity helper added by epic 3.5; the registry was scoped out of the consolidation because reviewSeverities is a presence-set, but the normalizer itself could still consume the shared helper. | Have registry import and call stream.NormalizeSeverity in place of its local normalizeSeverity (stream has zero internal deps, so registry->stream introduces no cycle); leave reviewSeverities as-is. | CROSS_CUTTING | 15 | execute-epic-cumulative |
| U | [ ] | LOW | internal/stream/severity.go:20 | The canonical SeverityRank map carries a read-only-after-init invariant in a comment only, with no test guard preventing a future caller from mutating the shared map (which would race across concurrent fan-out agents). | Add a stream test that snapshots the map and asserts it is unchanged after consumers run, or wrap it behind a Rank(sev) accessor. | OBSERVABILITY | 20 | execute-epic-independent |

### [2026-06-16] From Sprint: 3.4_scorecard-diagnostics-writer

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:118 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; the diagnostic already routes to the injectable writer, only fmt.Fprintf's own return is dropped; propagating it breaks Emit's never-fail contract) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:199 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; Append loop already surfaces firstErr; propagating the Fprintf return breaks the best-effort contract) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:235 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; verdictTallies already writes to injectable w; AC2 satisfied) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:248 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; verification-read-failed diagnostic writes to injectable w; AC2 satisfied) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:276 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; orphan-verdict diagnostic routes to injectable w, locked by TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | LOW | internal/scorecard/scorecard_test.go:291 | Epic 3.4 added unit tests proving EmitOpts.Diag / ReadOpts.Writer routing at the scorecard layer, but there is no test asserting the wiring itself — that the MCP handler passes a non-default writer or that the three CLI entry points pass cmd.ErrOrStderr(). This plumbing can silently regress (a future refactor swaps cmd.ErrOrStderr() back to a default and no test fails). (Deferred: Epic Plan 3.6 scorecard-wiring-tests) | Add a CLI-level test that drives a scorecard command against a deliberately-malformed store and asserts the read diagnostic reaches the command's ErrOrStderr buffer; once MCP diagnostics route through e.log, add a handler-level test asserting against a captured logger buffer. | testing | 60 | code-review | claude | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:114 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; over-long-line warning uses injectable w via diagWriter(opts.Writer); read continues; AC2 satisfied) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:145 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; decodeRecord writes to injectable w then returns (Record,bool) — no error cascade) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:155 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; schema-version skip writes to injectable w; identical to malformed-record path) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | LOW | internal/scorecard/store.go:194 | Redundant call to diagWriter (Wontfix: FALSE POSITIVE — diagWriter is the required typed-nil guard for the nil-able opts.Writer interface, not redundant; removing it reintroduces the panic fixed by commit 476c6d1) | Remove diagWriter call and use opts.Writer directly since ReadRecords already resolves it | performance | 2 | code-review | otto | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:211 | Error from diagWriter is silently discarded (Wontfix: FALSE POSITIVE — `_, _ = fmt.Fprintf` is the documented best-effort never-panic diagnostics contract at store.go:22-24; returning the write error would regress a successful read on a broken sink and logging is circular; confirmed working as designed via /sprint-clarification 97%) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| U | [/] | MEDIUM | internal/mcp/handlers.go:220 | The MCP engine carries a structured *slog.Logger (e.log) used for every other diagnostic in handleReconcile (e.g. the require_verified warning at line ~225), but scorecard diagnostics are routed to raw os.Stderr, so MCP-path scorecard write-failures/malformed-record/orphan-verdict warnings emit as unstructured plaintext that bypasses the logger's level filtering, formatting, and sink redirection. NOTE: this was a DELIBERATE, documented epic decision (Clarifications Q2: supply os.Stderr at the call site; adapting e.log to an io.Writer was explicitly OUT of scope), so this is enhancement debt, not a regression. (intent_note: deferred per epic Clarifications Q2 — adapting e.log to io.Writer is out of scope) (Wontfix: ACCEPTED ENHANCEMENT DEBT — handlers.go:220 is an unconditional EmitOpts{Diag: os.Stderr} call, not deferred logic; all five Epic 3.4 ACs are met and the comment at handlers.go:214-219 satisfies AC4; e.log→io.Writer adaptation is out of scope per Clarifications Q2; confirmed via /sprint-clarification 97%) | If MCP observability is later desired, adapt e.log into an io.Writer shim (slog-backed at Warn level) and pass it as Diag instead of os.Stderr, so MCP-path scorecard diagnostics flow through the same structured pipeline as the rest of the handler. Defer unless/until structured MCP diagnostics are required. | error-handling | 30 | code-review | claude | MEDIUM |

### [2026-06-15] From Sprint: 3.3_per_run_scorecard

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| Solo | [/] | LOW | internal/scorecard/store.go (FindByRunID adjacent-month notice; ReadRecords malformed-line notice); internal/scorecard/scorecard.go (Emit notices) | Store/emit diagnostics ("skipping malformed record", "run spans adjacent month files", "write failed") are written directly to the process-global `os.Stderr` rather than to a writer threaded from the cobra command (`cmd.ErrOrStderr()`). (Deferred: Epic Plan 3.4) | accept an `io.Writer` (or logger) on the store/emit entry points and have the CLI pass `cmd.ErrOrStderr()`, so warnings are capturable and redirectable. | ops | 0 | execute-sprint | execute-sprint | MEDIUM |

### [2026-06-14] From Sprint: 3.2_disagreement_radar

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| Solo | [/] | LOW | internal/reconcile/disagree.go:280 | Duplicate writeRadarSection rendering logic risks divergent escaping across packages (Deferred: .planning/epics/active/15.0_radar-renderer-consolidation.md) | Consolidate radar rendering into report package; reconcile should call report.writeRadarSection | security | 20 | code-review | greta | MEDIUM |
| 4 | [/] | LOW | internal/reconcile/disagree.go:350 | Duplicated radar section rendering logic (Deferred: Epic Plan 15.0) | Extract shared writeRadarSection to a common package | maintainability | 10 | code-review | bruce | MEDIUM |
| 4 | [/] | MEDIUM | internal/reconcile/disagree.go:354 | Duplicated radar markdown rendering diverges (Deferred: Epic Plan 15.0) | Extract shared writer or make reconcile use report package's escTrunc | maintainability | 10 | code-review | bruce | MEDIUM |
| Solo | [/] | LOW | internal/reconcile/disagree.go:413 | Redundant implementation of writeRadarSection (Deferred: .planning/epics/active/15.0_radar-renderer-consolidation.md) | Remove duplicate function from internal/reconcile and use internal/report | maintainability | 15 | code-review | otto | MEDIUM |
| 5 | [/] | MEDIUM | internal/report/disagree.go:47 | The radar markup is rendered by two divergent copies: writeRadarSection + formatScore in internal/report/disagree.go and a structurally different copy in internal/reconcile/disagree.go:389. They are intentionally not identical (report copy truncates via escTrunc + uses joinReviewers/reviewerOrUnknown; reconcile copy uses uncapped esc + joinOrNone), so a future markup change (new field, reordered bullets) must be made in both or the live `atcr report` radar and the archival reconciled/report.md silently drift. (Deferred: .planning/epics/active/15.0_radar-renderer-consolidation.md) | Extract one shared item renderer parameterized by a truncate-vs-verbatim flag and heading prefix; have both call sites delegate. Add a test that diffs the rendered markup of both paths on the same DisagreementsFile asserting the only intended difference is truncation. | correctness | 60 | code-review | bruce, claude | HIGH |

### [2026-06-14] From Sprint: 2.2_code_review_fanout_hardening

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| Solo | [/] | MEDIUM | internal/fanout/postprocess.go:14 | The severity-rank rubric {CRITICAL:4,HIGH:3,MEDIUM:2,LOW:1} is independently redefined in fanout/postprocess.go:17, reconcile/merge.go, verify, report, plus a set-form copy reviewSeverities in registry/config.go. postprocess looks up severityRank[strings.ToUpper(...)] while reconcile looks up the raw value, so a future severity change or non-canonical casing silently desyncs fan-out truncation from reconcile merging. The postprocess copy was newly added by Epic 2.2. (disagreement: LOW vs MEDIUM) (Deferred: Epic Plan 3.5) | Extract a single canonical severity package (or export from internal/stream) exposing the ordered rank map plus normalizeSeverity, and have registry/fanout/reconcile/verify/report consume it. Verify by deleting the local maps and confirming the suite passes with one source of truth. | maintainability | 120 | code-review | claude | MEDIUM |

### [2026-06-14] From Review: llmclient OpenAI-compatible tool handling

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|------------|
| U | [/] | LOW | internal/llmclient/chat.go:1 | No provider-conformance test matrix for the OpenAI-compatible surface. The client deliberately absorbs real wire divergence (string-encoded vs raw-object tool_call `arguments`, lenient finish_reason) but is exercised only against synthetic fixtures, so a regression against a specific provider's actual tool_call shape (OpenAI, litellm, Ollama, vLLM, Together) would not be caught. This is the robustness the official SDK is assumed to provide, achievable here without adopting it. | Add a recorded-fixture conformance suite: capture a real `tool_calls` response from each target provider and assert the parser (`ToolCallArguments`, `chatToolResponse` decode, finish_reason handling) yields identical engine-facing results. NOTE: scope is a few days, not a quick-win — consider promoting to a standalone test-remediation plan rather than resolving inline. | testing | 480 | review | claude | LOW |

### [2026-06-14] From Sprint: 3.0_adversarial_verification

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|------------|
| 7 | [/] | LOW | internal/verify/pipeline.go:238 | The rich VerificationResult built in verifyFinding never populates TrippedBudgets (invokeSkeptic folds tripped budgets only into free-text Notes), base.Model is hard-coded to skeptics[0].Config.Model even when another skeptic produced the winning verdict, and the skip-already-verified rebuild (pipeline.go:199) re-synthesizes records from the compact on-disk block losing Model/DurationMs/TrippedBudgets — so verification.json's structured audit fields degrade or misattribute on multi-skeptic and no-op re-runs (extends the model-attribution gap of TD-011). Also: Model is empty on the no_eligible_skeptic / tool_harness_unavailable early-return paths. [intent: deferred per sprint-plan TD-011] | Thread the tripped-budget slice and the winning skeptic's model up from invokeSkeptic into base.TrippedBudgets/base.Model (join models when multiple voters agree, mirroring joinSkeptics), and carry Model/DurationMs/TrippedBudgets forward for skipped findings instead of synthesizing a lossy record. | correctness | 120 | execute-sprint | execute-sprint, claude | HIGH |

### [2026-06-13] From Sprint: 2.0_tool_using_reviewers

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|------------|
| 5 | [/] | LOW | internal/tools/snapshot.go | AC 03-02 Scenario 5 and AC 03-03 Scenarios 4-5 require `manifest.json` `stages.review` to record `snapshot_mode` (live/worktree), `head_sha`, and `snapshot_worktree_path`. (intent_note: deferred per sprint-plan §2.5.A (manifest review-stage recording is Phase 5 work); Deferred to Epic Plan 2.1) | when wiring `SnapshotFor` into the agent loop, record `snapshot_mode`/`head_sha`/`snapshot_worktree_path` into `internal/payload/manifest.go` review stage and add the manifest assertion tests from AC 03-02/03-03. | testing | 0 | execute-sprint | execute-sprint | MEDIUM |
