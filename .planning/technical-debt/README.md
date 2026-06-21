# Technical Debt Tracking

This file is a staging area for small technical debt items discovered during development. Items are triaged and moved to individual sprint documents in `sprints/` as they are prioritized.

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 1 | 0 |
| MEDIUM | 2 | 19 | 0 |
| LOW | 10 | 17 | 0 |


**Last Modified:** 2026-06-21 | **Open Items:** 12 | **Deferred Items:** 37 | **Resolved Items:** 0 | **Total Items:** 49

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



### [2026-06-20] From Sprint: 5.2_diff_caching_incremental_reviews

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [ ] | MEDIUM | internal/fanout/engine.go:596 | Missing FallbackFrom in synthesized cache-hit Result | Add FallbackFrom: a.FallbackFrom to the Result struct | correctness | 5 | code-review | bruce | MEDIUM |

### [2026-06-20] From Sprint: epic-5.2

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [ ] | LOW | internal/cache/store.go:115 | Eviction does a full ReadDir+Stat of the cache dir on every Put even when under the cap; O(n) per write scales poorly if the cache accumulates thousands of entries | Maintain a running total-size counter and skip the directory scan when it is under the cap, or evict only on a periodic/threshold basis | PERFORMANCE | 30 | execute-epic-cumulative |

### [2026-06-20] From Sprint: epic-5.0

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [ ] | LOW | internal/report/render.go:317 | The "File not found" warning format string is duplicated across internal/reconcile/emit.go (writeFindingsList) and internal/report/render.go (writePathWarning) in separate packages | Extract a shared constant/helper only if a common low-level rendering package emerges; a cross-package dependency is not justified for one format string today | CROSS_CUTTING | 15 | execute-epic-cumulative |
| U | [ ] | MEDIUM | internal/reconcile/reconcile.go:26 | Validation root is hardcoded to "." at every call site, so "atcr reconcile <path>" for a review of another repo, or running from a non-repo-root CWD, falsely flags every finding as "file not found" | Thread the reviewed repo root explicitly or add a --repo flag, applied consistently with the verify stage which uses the same "." convention | EDGE_CASES | 60 | execute-epic-independent |
| U | [ ] | LOW | internal/reconcile/emit.go:146 | The reconciled 9-column findings.txt (RenderText) does not carry PathValid/PathWarning, so a consumer reading findings.txt rather than findings.json/report.md loses the hallucination warning | Document the intentional omission in RenderText, or fold the warning into the EVIDENCE column the way the Disagreement annotation already is | INTEGRATION | 15 | execute-epic-independent |
| U | [ ] | LOW | internal/stream/validate.go:46 | os.Stat is case-insensitive on macOS/Windows default filesystems, so a case-only path typo (Parser.go vs parser.go) resolves as present and is not flagged | Add a case-exact existence check comparing against the real dirent name per path segment | EDGE_CASES | 30 | execute-epic-independent |
| U | [ ] | LOW | internal/report/render.go:318 | The warning label "File not found" is hardcoded and keyed off PathWarning != "" rather than rendering the PathWarning value, so a future non-default warning would render text inconsistent with findings.json path_warning | Render esc(f.PathWarning) so the human report always matches the machine field | REGRESSION_RISK | 15 | execute-epic-independent |

### [2026-06-20] From Sprint: epic-4.9

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 4 | [ ] | LOW | internal/fanout/secrets.go:28 | SecretValues resolves os.Getenv only at redactor-construction time, so a key set or rotated after correlateAndRedact/reviewContext runs will not be added to the exact-value scrub list and could leak verbatim. | Document the construction-time snapshot contract explicitly as a known limitation; acceptable given keys are resolved before any provider call. | EDGE_CASES | 5 | execute-epic-independent |
| 4 | [ ] | LOW | internal/fanout/secrets.go:18 | The minSecretLen=8 guard admits any 8-39 char misconfigured/self-hosted key value into the verbatim ReplaceAll scrub list, so a short generic key could over-redact unrelated log substrings that contain it. | Accept the documented tradeoff (real provider keys are 32+); if self-hosted short keys become a concern, raise the floor or scope the verbatim match to header-adjacent contexts. | EDGE_CASES | 15 | execute-epic-independent |
| 4 | [ ] | LOW | internal/fanout/secrets.go:24 | SecretValues silently skips both unset and sub-8-char env values with no diagnostic, so an operator who fat-fingers a key env name or sets a too-short test key gets no signal that exact-value redaction is inactive for that slot. | Optionally emit a debug-level (never the value) note when a configured APIKeyEnv resolves empty or below the floor, so a misconfigured redaction path is observable. | OBSERVABILITY | 30 | execute-epic-independent |

### [2026-06-20] From Sprint: 4.7.1_backup-swap-hardening

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 3 | [/] | MEDIUM | internal/fanout/reviewdir.go:318 | backupExisting() unconditionally RemoveAll's <path>.bak.old and <path>.bak.new at entry, but forceBackupOutputDir() only calls guardForeignBackup(dir+".bak") — the two staging siblings are not guarded. On an arbitrary --output-dir a user who owns <output-dir>.bak.old or <output-dir>.bak.new has it silently deleted by --force, re-opening the Epic 4.7 "never silently delete user data" contract. NOTE: epic clarification Q4 deliberately scoped the guard to .bak only (atcr-internal suffixes); this finding re-raises that decision as worth reconsidering, not a defect against the recorded scope. (intent_note: deferred per epic §Clarifications Q4 — extending guardForeignBackup to .bak.old/.bak.new is out of scope) (Won't-fix 2026-06-20: binding Epic 4.7.1 Q4 — guard stays scoped to .bak; .bak.old/.bak.new are atcr-owned staging names cleared by entry-time RemoveAll; confirmed via /sprint-clarification 95%) | In forceBackupOutputDir() (reviewdir.go:440) extend the guard to guardForeignBackup(dir+".bak.old") and guardForeignBackup(dir+".bak.new") before backupExisting, or refuse if either exists non-empty. Add a test pre-creating a foreign <output-dir>.bak.old with user content and assert --force errors instead of deleting it. | security | 60 | code-review | claude | MEDIUM |
| 3 | [/] | MEDIUM | internal/fanout/reviewdir.go:373 | The advertised "an interrupted swap never leaves the user with neither generation" invariant is conditional on a best-effort SILENT restore succeeding. If restorePriorBackup's os.Rename(backupOld,backup) fails (e.g. backup partially recreated, or perms change), the only surviving copy is stranded under .bak.old — which the very next run's entry-time RemoveAll(backupOld) (line 318) then deletes. Same pattern at the copy site swapStagedBackup (atomic.go:159-167) feeding atomic.go:97 RemoveAll(bakOld). (Won't-fix 2026-06-20: observability half done (restore failure now logged); the lone-.bak.old-as-generation redesign is rejected per Epic 4.7.1 Q3 — .bak.old is atcr-owned temp deleted at entry, one-generation contract + TestBackupExisting_CleansStaleStagingStragglers stand; confirmed via /sprint-clarification 95%) | Surface restore failures loudly (log/return) instead of dropping them, and/or do not auto-RemoveAll .bak.old at entry when .bak is absent — treat a lone .bak.old as the surviving generation to recover. Add a test where restore fails and a subsequent run is asserted not to delete the only surviving copy. | correctness | 120 | code-review | claude | MEDIUM |
| 3 | [/] | LOW | internal/fanout/reviewdir.go:388 | backupCrossDevice's inner os.Rename(backupNew,backup) relies on backupNew and backup sharing a filesystem, an invariant that holds only by naming coincidence (both are siblings of path). If anyone later relocates backupNew under path, the inner rename silently becomes cross-device and returns a raw EXDEV to the user. (Won't-fix 2026-06-20: same-fs invariant holds by construction — backup and backupNew are both siblings of path; a runtime guard would be unreachable dead code today and is already documented at reviewdir.go:415-419; confirmed via /sprint-clarification 90%) | Add a test that forces renameFn to return syscall.EXDEV and makes the copy fail (a copy seam, or unreadable src), staging a prior .bak first; assert the prior .bak content is restored intact, the live tree survives, and .bak.new is cleaned up. Cover the copy-failure leg at minimum. | testing | 120 | code-review | claude | MEDIUM |
| 3 | [ ] | LOW | internal/fanout/reviewdir_test.go:458 | Test-coverage gaps in the failed-swap/cleanup paths: TestBackupExisting_FailedSwapPreservesPriorBak asserts no .bak.old straggler but not .bak.new; the non-ErrNotExist Lstat(backup) error branch (reviewdir.go:333-335) is untested; the entry-time RemoveAll straggler-cleanup failure legs (reviewdir.go:318-323) are untested. Each is a real error branch a regression could silently break. | Add assert.NoDirExists for src+".bak.new" at reviewdir_test.go:458; add a perms-based test forcing Lstat(backup) to fail with a non-ErrNotExist error; add a test where .bak.old cannot be removed and assert the typed "clearing stale staging backup" error. Skip on root/CI where perms are not enforced. | testing | 30 | code-review | claude | MEDIUM |

### [2026-06-20] From Sprint: epic-4.7.1

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [ ] | LOW | internal/atomicfs/atomic.go:139 | CopyPath documents that a directory dst must not already exist, but copyTree uses MkdirAll and copyFile uses O_TRUNC, so a pre-existing dst is silently merged/overwritten; the not-exist invariant is enforced by callers (a prior RemoveAll), not by CopyPath itself. | Enforce the precondition inside CopyPath (stat dst and error if present) or soften the doc comment to state CopyPath merges into an existing dst. | EDGE_CASES | 15 | execute-epic-independent |

### [2026-06-19] From Sprint: 4.7_idempotency

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 3 | [/] | MEDIUM | internal/fanout/reviewdir.go:284 | backupExisting() does os.RemoveAll(backup) then os.Rename(path, backup); BackupToDotBak() does os.RemoveAll(bak) then copyTree. If the rename/copy fails (cross-filesystem EXDEV when --output-dir and its .bak sibling are on different mounts, disk-full mid-copy, or SIGKILL), the single prior backup generation is already destroyed while the new backup is absent or partial. The live/original tree is preserved (good), but the one recoverable prior .bak is lost for no benefit — counter to the safe-to-retry goal. (disagreement: LOW vs MEDIUM) (Deferred: Epic Plan 4.7.1) | Stage the new backup first: copy/rename into <path>.bak.new (or rename old .bak aside to .bak.old), confirm it is complete, then swap and only then remove the old generation. For backupExisting, attempt os.Rename before RemoveAll so a failed rename leaves the old .bak intact; detect EXDEV and fall back to copy+remove. Add a fault-injection test asserting the prior .bak survives a failed swap. | correctness | 120 | code-review | code-reviewer, claude | HIGH |

### [2026-06-19] From Sprint: 4.5_circuit_breaker

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | MEDIUM | internal/fanout/metrics.go:37 | recordAgentOutcome zeroes apiCalls for any result whose error unwraps to context.DeadlineExceeded/Canceled, assuming no request was made. But a per-agent timeout routinely fires AFTER real HTTP round-trips (and mid tool-loop after several Chat turns already hit the wire), so atcr_api_calls_total undercounts real provider traffic exactly when a provider is degraded. The no-request assumption is only provably safe for CircuitOpenError and pre-first-send cancellation. (Deferred: Epic Plan 4.11) | Use max(1, r.Turns) for the deadline case instead of a flat 0, or thread a real calls-attempted counter out of the client rather than inferring from the terminal error class. Keep the apiCalls=0 path only for CircuitOpenError. | performance | 60 | code-review | claude | MEDIUM |

### [2026-06-18] From Sprint: epic-4.3

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | LOW | internal/validation/validation.go:62 | System-dir denylist covers only /etc, /proc, /sys, leaving /boot, /dev, /root, /var and ~/.ssh writable via --output-dir / --output (deliberate Option-B permissive choice) (Won't-fix: intended Option-B policy per epic 4.3 clarifications 2026-06-18; revisit only on a concrete isolation requirement) | If stronger isolation is later required, switch to an allowlist anchored at the repo/.atcr root instead of a denylist | SECURITY | 30 | execute-epic-independent |
| 1 | [/] | LOW | internal/validation/validation.go:86 | Severity and Enum validators are shipped but wired to nothing (ParseSeverity/ValidFormat remain the live paths); they exist only to satisfy AC5/AC7 and future use (Won't-fix: intentionally public for AC5/AC7 per epic 4.3 clarifications; deletion breaks ACs, wire-in out of scope) | Revisit and delete if no caller adopts them within a release, or wire them in where duplication can be removed | OVER_ENGINEERING | 15 | execute-epic-independent |

### [2026-06-18] From Sprint: epic-4.2

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/registry/attribution.go:55 | attribute() recurses and reconstructs the join even for a single-error errors.Join (which still satisfies Unwrap() []error), a small avoidable cost on the load-time validation path | Optionally short-circuit when len(children)==1 if profiling ever flags it (WON'T-FIX 2026-06-18: trigger unmet — error-path only, never hit on normal load; no perf AC in epic 4.2) | INTEGRATION | 5 | execute-epic-independent |

### [2026-06-18] From Sprint: epic-4.1.2

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | MEDIUM | internal/fanout/review.go:361 | If a server shutdown (or CLI SIGINT) fires after all agents already succeeded but before ExecuteReview's interrupted := errors.Is(ctx.Err(), context.Canceled) check, a fully-completed run is stamped Interrupted=true and status.go:216 overrides RunCompleted to RunInterrupted (a false interrupted; inverse of AC4). Pre-existing in the CLI-shared path, newly reachable via MCP shutdown. | Gate the interrupted marker on at least one agent ending in StatusTimeout/cancelled rather than purely on parent ctx.Err()==Canceled. NOTE: touches CLI-shared review.go (out of scope for epic 4.1.2's MCP-only change); window is microscopic and outcome benign (resume no-ops a complete run) - separate design. (WON'T-FIX 2026-06-18: --resume self-healing via ClearInterrupted (resume.go:220) already recovers a stale interrupted-on-complete; revisit in a backlog sprint if insufficient) | CORRECTNESS | 30 | execute-epic-independent |

### [2026-06-17] From Sprint: epic-4.1

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | MEDIUM | internal/mcp/handlers.go:88 | Serve-mode background fan-out runs under context.WithoutCancel(ctx) so a SIGINT to the MCP server never cancels or marks an in-flight detached review interrupted; it is allowed to finish (intended MCP design) but never gets the interrupted marker CLI mode promises. (Deferred: Epic Plan 4.1.2) | Decide whether detached MCP reviews should be marked interrupted on server shutdown; if so, thread a cancellable/interrupt-aware context or post-hoc marker into the background review path — a separate design from this CLI-focused epic. | REGRESSION_RISK | 60 | execute-epic-independent |

### [2026-06-17] From Sprint: 4.0_structured_logging

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 1 | [/] | HIGH | cmd/atcr/review.go:172 | Both review entry points call `NewRedactor(root)` with ZERO configured secrets — cmd/atcr/review.go:172 and internal/mcp/handlers.go:87 pass only the path root, never the registry API keys. The exact-value secret-scrubbing loop in Redact is therefore dead in production; redaction relies entirely on the `sk-`/`Bearer` regexes, which miss raw provider keys that lack those prefixes (Google `AIzaSy...`, Azure `api-key`, JWTs `eyJ...`). The AC5 "no API key in log output" guarantee holds only for sk-/Bearer-shaped keys; the passing integration test likely uses an sk- key, masking the gap. (Deferred: Epic Plan 4.9 secret-value-redaction) | Thread resolved registry API key values into `NewRedactor(root, keys...)` at both review.go:172 and handlers.go:87 (keys are discoverable from prep.Slots / cfg.Registry). Add an integration test using a non-sk-shaped key (e.g. `AIzaSy...`) asserting it is redacted. | security | 120 | code-review | claude | MEDIUM |

### [2026-06-16] From Sprint: 3.5_severity-rank-consolidation

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 4 | [/] | LOW | internal/reconcile/severity_consolidation_test.go:18 | Test name too long (Won't fix: rejected by maintainer — breaks file's TestSubject_Behavior naming convention) | Shorten to TestMergeNormalizesMixedCase | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [/] | LOW | internal/reconcile/severity_consolidation_test.go:30 | Test name too long (Won't fix: rejected by maintainer — breaks file's TestSubject_Behavior naming convention) | Shorten to TestGrayZoneNormalizesMixedCase | maintainancy | 2 | code-review | Reviewer | MEDIUM |
| 4 | [/] | LOW | internal/reconcile/severity_consolidation_test.go:42 | Test name too long (Won't fix: rejected by maintainer — breaks file's TestSubject_Behavior naming convention) | Shorten to TestMergeNoDisagreementOnCase | maintainancy | 2 | code-review | Reviewer | MEDIUM |

### [2026-06-16] From Sprint: epic-3.5

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/stream/severity.go:20 | The canonical SeverityRank map carries a read-only-after-init invariant in a comment only, with no test guard preventing a future caller from mutating the shared map (which would race across concurrent fan-out agents). (Won't fix: structural guard — reconcile copy-on-init at merge.go:29-31 means no consumer writes stream.SeverityRank directly; grep confirms zero write sites across consumers. Snapshot test trips the over-simplification gate; Rank() accessor cascades to 14+ direct-lookup sites — both out of pure-consolidation scope.) | Add a stream test that snapshots the map and asserts it is unchanged after consumers run, or wrap it behind a Rank(sev) accessor. | OBSERVABILITY | 20 | execute-epic-independent |

### [2026-06-16] From Sprint: 3.4_scorecard-diagnostics-writer

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:118 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; the diagnostic already routes to the injectable writer, only fmt.Fprintf's own return is dropped; propagating it breaks Emit's never-fail contract) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:199 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; Append loop already surfaces firstErr; propagating the Fprintf return breaks the best-effort contract) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:235 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; verdictTallies already writes to injectable w; AC2 satisfied) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:248 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; verification-read-failed diagnostic writes to injectable w; AC2 satisfied) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/scorecard.go:276 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; orphan-verdict diagnostic routes to injectable w, locked by TestEmit_OrphanVerdictDiagnosticRoutesToDiagWriter) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | LOW | internal/scorecard/scorecard_test.go:291 | Epic 3.4 added unit tests proving EmitOpts.Diag / ReadOpts.Writer routing at the scorecard layer, but there is no test asserting the wiring itself — that the MCP handler passes a non-default writer or that the three CLI entry points pass cmd.ErrOrStderr(). This plumbing can silently regress (a future refactor swaps cmd.ErrOrStderr() back to a default and no test fails). (Deferred: Epic Plan 3.6 scorecard-wiring-tests) | Add a CLI-level test that drives a scorecard command against a deliberately-malformed store and asserts the read diagnostic reaches the command's ErrOrStderr buffer; once MCP diagnostics route through e.log, add a handler-level test asserting against a captured logger buffer. | testing | 60 | code-review | claude | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:114 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; over-long-line warning uses injectable w via diagWriter; read continues; AC2 satisfied) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:145 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; decodeRecord writes to injectable w then returns (Record,bool) — no error cascade) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:155 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; schema-version skip writes to injectable w; identical to malformed-record path) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | LOW | internal/scorecard/store.go:194 | Redundant call to diagWriter (Wontfix: FALSE POSITIVE — diagWriter is the required typed-nil guard for the nil-able opts.Writer interface, not redundant; removing it reintroduces the panic fixed by commit 476c6d1) | Remove diagWriter call and use opts.Writer directly since ReadRecords already resolves it | performance | 2 | code-review | otto | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:211 | Error from diagWriter is silently discarded (Wontfix: FALSE POSITIVE — `_, _ = fmt.Fprintf` is the documented best-effort never-panic diagnostics contract at store.go:22-24; returning the write error would regress a successful read on a broken sink and logging is circular; confirmed working as designed via /sprint-clarification 97%) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| U | [/] | MEDIUM | internal/mcp/handlers.go:220 | The MCP engine carries a structured *slog.Logger (e.log) used for every other diagnostic in handleReconcile (e.g. the require_verified warning at line ~225), but scorecard diagnostics are routed to raw os.Stderr, so MCP-path scorecard write-failures/malformed-record/orphan-verdict warnings emit as unstructured plaintext that bypasses the logger's level filtering, formatting, and sink redirection. NOTE: this was a DELIBERATE, documented epic decision (Clarifications Q2: supply os.Stderr at the call site; adapting e.log to an io.Writer was explicitly OUT of scope), so this is enhancement debt, not a regression. (intent_note: deferred per epic Clarifications Q2 — adapting e.log to io.Writer is out of scope) (Wontfix: ACCEPTED ENHANCEMENT DEBT — handlers.go:220 is an unconditional EmitOpts{Diag: os.Stderr} call, not deferred logic; all five Epic 3.4 ACs are met and the comment at handlers.go:214-219 satisfies AC4; e.log→io.Writer adaptation is out of scope per Clarifications Q2; confirmed via /sprint-clarification 97%) | If MCP observability is later desired, adapt e.log into an io.Writer shim (slog-backed at Warn level) and pass it as Diag instead of os.Stderr, so MCP-path scorecard diagnostics flow through the same structured pipeline as the rest of the handler. Defer unless/until structured MCP diagnostics are required. | error-handling | 30 | code-review | claude | MEDIUM |

### [2026-06-15] From Sprint: 3.3_per-run_scorecard

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| Solo | [/] | LOW | internal/scorecard/store.go (FindByRunID adjacent-month notice; ReadRecords malformed-line notice); internal/scorecard/scorecard.go (Emit notices) | Store/emit diagnostics ("skipping malformed record", "run spans adjacent month files", "write failed") are written directly to the process-global `os.Stderr` rather than to a writer threaded from the cobra command (`cmd.ErrOrStderr()`). (Deferred: Epic Plan 3.4) | accept an `io.Writer` (or logger) on the store/emit entry points and have the CLI pass `cmd.ErrOrStderr()`, so warnings are capturable and redirectable. | ops | 0 | execute-sprint | execute-sprint | MEDIUM |

### [2026-06-14] From Sprint: 3.2_disagreement_radar

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| Solo | [/] | LOW | internal/reconcile/disagree.go:280 | Duplicate writeRadarSection rendering logic risks divergent escaping across packages (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md) | Consolidate radar rendering into report package; reconcile should call report.writeRadarSection | security | 20 | code-review | greta | MEDIUM |
| 4 | [/] | LOW | internal/reconcile/disagree.go:350 | Duplicated radar section rendering logic (Deferred: Epic Plan 7.2) | Extract shared writeRadarSection to a common package | maintainability | 10 | code-review | bruce | MEDIUM |
| 4 | [/] | MEDIUM | internal/reconcile/disagree.go:354 | Duplicated radar markdown rendering diverges (Deferred: Epic Plan 7.2) | Extract shared writer or make reconcile use report package's escTrunc | maintainability | 10 | code-review | bruce | MEDIUM |
| Solo | [/] | LOW | internal/reconcile/disagree.go:413 | Redundant implementation of writeRadarSection (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md) | Remove duplicate function from internal/reconcile and use internal/report | maintainability | 15 | code-review | otto | MEDIUM |
| 5 | [/] | MEDIUM | internal/report/disagree.go:47 | The radar markup is rendered by two divergent copies: writeRadarSection + formatScore in internal/report/disagree.go and a structurally different copy in internal/reconcile/disagree.go:389. They are intentionally not identical (report copy truncates via escTrunc + uses joinReviewers/reviewerOrUnknown; reconcile copy uses uncapped esc + joinOrNone), so a future markup change (new field, reordered bullets) must be made in both or the live `atcr report` radar and the archival reconciled/report.md silently drift. (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md) | Extract one shared item renderer parameterized by a truncate-vs-verbatim flag and heading prefix; have both call sites delegate. Add a test that diffs the rendered markup of both paths on the same DisagreementsFile asserting the only intended difference is truncation. | correctness | 60 | code-review | bruce, claude | HIGH |

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
