# Technical Debt Tracking

This file is a staging area for small technical debt items discovered during development. Items are triaged and moved to individual sprint documents in `sprints/` as they are prioritized.

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 2 | 0 |
| MEDIUM | 0 | 20 | 0 |
| LOW | 0 | 27 | 0 |

**Last Modified:** 2026-07-06 | **Open Items:** 0 | **Deferred Items:** 49 | **Resolved Items:** 0 | **Total Items:** 49

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

## Sharded Storage Format (`items/`) — additive, Epic 12.1

As of Epic 12.1, every item in the dated table below is **also** stored as a
structured YAML file under [`items/`](items/), **sharded by source** — one file
per `### [date] From <Sprint|Review>: <label>` section (e.g.
`items/2026-06-26_epic-11.2.yaml`). A single review producing 50–100 findings is
therefore **one** shard file, not 50–100, and two concurrent review/sprint runs
each write their own new file, so they never merge-conflict on TD storage.

This is **additive and not yet canonical**:

- **The Markdown table below remains authoritative.** All existing tooling (the
  `td_*` MCP binaries and the TD skills) reads/writes this table unchanged. The
  shards are generated *alongside* it and are not yet machine-read by any tool.
- The cutover that makes the shards canonical (and updates the binaries/skills)
  is deferred to a follow-on epic (18.0 / 12.3). No tooling changed in 12.1.

**Tooling** — `cmd/td-migrate` (logic in `internal/tdmigrate/`):

| Command | Effect |
|---------|--------|
| `go run ./cmd/td-migrate migrate`  | Parse this README table → (re)write the shards under `items/`. Idempotent: prunes its own prior `*.yaml` output. |
| `go run ./cmd/td-migrate generate` | Read the shards → print a regenerated ToC table to **stdout** (never overwrites this README). |
| `go run ./cmd/td-migrate validate` | Strict-load + schema-check every shard; a malformed shard fails **loudly** (non-zero exit). |

The shard schema, field semantics, and the YAML-safety guarantees are documented
in [`items/SCHEMA.md`](items/SCHEMA.md). Round-trip fidelity (table → shards →
table with zero data loss) is proven by the Go test suite in
`internal/tdmigrate/`, not by a committed generated artifact.


### [2026-07-05] From Sprint: 19.4_history_time_sharding

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | HIGH | cmd/atcr/review.go:285 | Multiple concurrent atcr review processes will append to the same YYYY-MM.jsonl simultaneously without synchronization, causing interleaved JSON lines and ledger corruption (Accepted 2026-07-06: won't-fix — concurrent-append O_APPEND safety is a project-wide TD-004 tradeoff shared by all 5 append-only ledgers (audit, debate, scorecard, tools, history), outside epic 19.4's scope; matches the identical writer.go precedent accepted 2026-07-04 at README:112. Low probability (small per-batch writes are atomic on Linux/macOS O_APPEND) and low impact (a torn line is skipped by Load, no cross-data corruption); a cross-platform lock dependency is not justified for one ledger.) | Acquire a file lock (flock) on the shard or a dedicated lockfile before opening for append | concurrency | 40 | code-review | brad | MEDIUM |
| 3 | [/] | MEDIUM | internal/history/shard.go:32 | LoadAll/LoadShards read and JSON-unmarshal every monthly shard on disk, then Filter discards records older than the --since cutoff. The justification for monthly sharding is the YYYY-MM filenames, but the read path throws that structure away: a repo with years of history running the common --since 30d (or the 90d default) still parses every archived month it will immediately drop. (Deferred 2026-07-06: not worth doing now — LOW-value perf optimization; the epic's 1-2yr history bound caps the scan at 12-24 small files. If revisited, add an additive LoadShardsSince(dir, cutoff) variant rather than changing LoadShards/LoadAll signatures, which would ripple into 7+ existing test call sites for no measured benefit.) | Prune glob results in LoadShards by comparing each shard's YYYY-MM stem against the cutoff month before calling Load; pass a lower-bound month into LoadShards/LoadAll (or add a windowed variant) so shards entirely before the window are never opened. Verify with a benchmark over 60 seeded shards and --since 30d reading at most 1-2 files. | performance | 60 | code-review | claude, dax, brad | HIGH |
| 3 | [/] | LOW | internal/history/writer.go:39 | Append does an unlocked O_APPEND write and documents that a large buffer split across multiple write() syscalls can be torn by a concurrent append. Epic 19.4 does not change the mechanism but changes exposure: history is now git-tracked and the project is run as multiple concurrent sessions appending to the same YYYY-MM.jsonl within a month. A torn line is silently dropped by Load's malformed-line skip, so lost findings are invisible; sharding by month did not reduce intra-month contention. (Accepted 2026-07-06: won't-fix — same history.Append write site and same decision as cmd/atcr/review.go:285 (resolved in lockstep); no cross-platform lock dependency added. Mirrors the 2026-07-04 accepted precedent for this exact O_APPEND pattern.) | If a hard guarantee is wanted wrap the shard write in an advisory file lock (flock) consistent with the mkdir-flock pattern used in the planning tooling; otherwise document that concurrent same-month runs can lose records. Verify with a stress test of N concurrent Appends and a line-count assertion. | error-handling | 60 | code-review | claude | MEDIUM |

### [2026-07-05] From Sprint: epic-19.4

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | LOW | internal/history/shard.go:32 | LoadShards loads every shard into memory before Filter applies --since, so a narrow window still reads all shards (acceptable within the stated 1-2yr bound; no month-prefix pruning) (Deferred 2026-07-06: duplicate of the MEDIUM month-pruning item at this same line; same decision — defer, add LoadShardsSince additively if ever picked up.) | Skip shard files whose YYYY-MM stem is entirely older than now-since before loading, to keep long histories fast | UNDER_ENGINEERING | 30 | execute-epic-independent |

### [2026-07-05] From Sprint: epic-19.1

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | LOW | cmd/atcr/review.go:384 | The audit hook re-reads sources/pool/findings.txt that the history hook read moments earlier in the same review, so the pool file is parsed twice per review run | Parse the pool findings once in the CLI layer and pass the finding set to both history.RecordReview and audit.RecordReview, or expose a shared parse result (Won't-fix: accepted — the duplicate parse mirrors internal/history's independent capture pattern per epic design; not worth a cross-package signature refactor for a sub-ms, once-per-run gain.) | PERFORMANCE | 30 | execute-epic-cumulative |

### [2026-07-05] From Review: pr-105-recovery

Recovered from stale, never-merged PR #105 (`td/2026-06-26`, 46 commits, 9 days behind `main` and conflicting). Before closing that PR, each of its fixes was checked against current `main`; most were already independently re-implemented, but these were not — they are re-added here as fresh items for `/resolve-td` rather than merging the stale branch directly.

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | HIGH | internal/tdmigrate/shard.go:116 | WriteShards prunes every `*.yaml` file already present in dir before renaming staged shards into place, without checking whether a given file is actually shard output (Accepted 2026-07-05: won't-fix — PR #107 (merged, squash 9bd724d7) independently revisited this exact function on 2026-06-27 (adding the atomic tmp+rename staging), and reaffirmed rather than changed the "dir is owned entirely by this tool" doc comment; `TestWriteShards_IdempotentPrune` already encodes whole-directory ownership as the intended contract. `items/` is documented in SCHEMA.md as exclusively used for TD shards, so this is a deliberate design choice, not a gap.) | Skip files that don't parse as a valid shard (or otherwise verify a file is this tool's own prior output) before removing an existing `*.yaml` during the prune step | CORRECTNESS | 30 | pr-105-recovery |

### [2026-07-04] From Sprint: epic-19.0

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/history/reader.go:14 | Load reads the entire findings-history.jsonl into memory with no rotation/retention, so an unbounded ledger grows memory use over time (Deferred 2026-07-04: covered by epic 19.4 history_time_sharding; retention/rotation is out of scope for epic 19.0.) | Add optional retention/rotation or streaming aggregation if ledgers grow large (retention explicitly out of scope for epic 19.0) | PERFORMANCE | 60 | execute-epic-cumulative |
| U | [/] | LOW | internal/history/writer.go:35 | Append relies on a single O_APPEND write being atomic across concurrent atcr review runs; POSIX does not guarantee atomicity for multi-KB writes on regular files so simultaneous runs could interleave a JSONL line (Accepted 2026-07-04: won't-fix — cross-platform flock is out of scope for epic 19.0, which only requires sequential appends; the writer.go:38 doc-comment fix documents the risk.) | Take an advisory flock around the append (mirror the mkdir-flock pattern used for the TD README) or bound batch write size | EDGE_CASES | 45 | execute-epic-independent |

### [2026-07-04] From Sprint: epic-18.1

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | LOW | internal/reconcile/emit.go:211 | ambiguous.json gray-zone cluster Problem cells are not symbol-anchored while findings.json cells are - a now-harmless cosmetic asymmetry (debate correlation is anchor-insensitive as of the 18.1 HIGH fix) (Accepted: intentional by design — cluster members stay raw and StripSymbolAnchors normalizes for identity correlation, documented at symbol_anchor.go:82-90; no current ambiguous.json consumer needs the anchor. Revisit only if a TD ToC consumes ambiguous.json) | If the TD ToC ever consumes ambiguous.json, stamp toAmbiguousWire cluster problems via the same symbolResolver | CORRECTNESS | 30 | execute-epic-stage3 |
| 1 | [/] | LOW | internal/reconcile/symbol_anchor.go:56 | safeSymbolAnchor blacklists only table/parse-breakers (pipe, parens, whitespace), allowing markdown-active chars (backtick, brackets, angle) that render oddly but cannot corrupt the table or the parse (Accepted: leave permissive — no supported parser (Go, C-family, Python) can emit a name containing backtick/bracket/angle; generics and operators are stripped before Node.Name is set, so tightening guards unreachable input) | Leave permissive - a strict identifier whitelist would wrongly reject valid names like C++ Foo<T> or ~Dtor; revisit only if odd rendering is observed | SECURITY | 15 | execute-epic-independent |

### [2026-07-03] From Sprint: 18.0_technical_debt_tooling

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 3 | [/] | LOW | internal/debt/add.go:162 | A single AppendItem re-reads and re-parses the README three times and writes it twice, and SyncShards regenerates the entire shard store via a full migrate. Bulk-adding N items in a loop is effectively O(N) full-store rewrites and 2N shard-directory rewrites. (Deferred: .planning/epics/deferred/18.0.1_debt-bulk-add.md — was mis-cited as "Epic Plan 18.1", an unrelated epic; corrected 2026-07-05) | Expose a batch-append that mutates the README once, syncs shards once, and refreshes stats once for a batch; keep the single-item path for the interactive case. Verify by benchmarking a 100-item batch against 100 single adds. | performance | 120 | code-review | claude | MEDIUM |

### [2026-07-03] From Sprint: 17.0_auto_merged_fixes

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 4 | [/] | MEDIUM | internal/verify/localvalidate.go:81 | exec.CommandContext SIGKILLs only the direct child on timeout; a validation command that spawns subprocesses (e.g. sh -c make ...) leaves grandchildren orphaned and still running after the deadline, as cmd.WaitDelay only force-closes the parent pipe fds and does not reap the process group (Deferred: Epic Plan 22.0 process_group_reaping — TD-006/sprint-plan.md:296 was a bare TD-number reference with no backing plan; promoted to a real epic 2026-07-05) | Set cmd.SysProcAttr Setpgid true and kill the whole group (syscall.Kill(-pid, SIGKILL)) on the cancel/timeout path (unix build-tagged) so grandchildren are reaped too | performance | 0 | execute-sprint | execute-sprint | MEDIUM |

### [2026-07-02] From Sprint: 15.1_leaderboard-cost-na-rendering

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | LOW | internal/scorecard/export.go:275-281 | Export groups records by the scrubbed persona/model, and scrubField's absolute-path regexes can reduce an entirely path-like input string to empty. Two genuinely distinct reviewer identities that both happen to be path-like scrub to the same empty string and silently merge into one aggregated group, blending the costPer/SurvivedSkepticRate/FindingsRaisedAvg values this epic's change is meant to make trustworthy. Adjacent to, not caused by, this epic, but feeds the same grouping key costPer's group totals depend on. (Won't fix: by-design -- scrubField intentionally merges identities that scrub-equal, locked in by TestExport_DistinctIdentitiesMergeWhenTheyScrubEqual, export_test.go:296) | By-design behavior, not a bug: grouping keys off the scrubbed identity is deliberate (scrubField strips PII/paths/secrets before export), and TestExport_DistinctIdentitiesMergeWhenTheyScrubEqual locks the merge-on-equal-scrub as an anti-regression invariant. No source change. | error-handling | 60 | code-review | claude | MEDIUM |

### [2026-06-30] From Sprint: epic-14.1

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | MEDIUM | internal/fanout/grounding.go:54 | out-of-scope category is a blanket grounding-gate exemption in all modes, so an adversarial or hallucinating model can launder a fabricated finding past AC2 by tagging category=out-of-scope in diff/blocks mode | Restrict the out-of-scope exemption to files mode (or require the cited FILE:LINE/EVIDENCE to anchor before honoring it in diff/blocks) (Won't-fix: binding epic 14.1 Clarifications Q2 — out-of-scope is a deliberate, AC-traced (AC 06-04) exemption, "annotated, not promoted"; relabeled from a bare DEFERRED with no plan citation 2026-07-05) | correctness | 30 | execute-epic-independent |
| 1 | [/] | MEDIUM | internal/fanout/grounding.go:47 | groundFindings ignores Result.Tools and Result.PayloadMode, so a tool-enabled or files-mode agent citing a real line it legitimately read outside the changed range is dropped unless it tags out-of-scope or quotes a changed line in EVIDENCE | Thread Tools/PayloadMode into the gate and relax the range/evidence requirement for tool/files agents (Won't-fix: binding epic 14.1 Clarifications Q2 — the out-of-scope prompt convention already instructs such agents to tag read-but-unchanged citations, which the gate exempts; relabeled from a bare DEFERRED with no plan citation 2026-07-05) | correctness | 45 | execute-epic-independent |
| 1 | [/] | LOW | internal/fanout/review.go:470 | computeGroundingData builds a fresh gitRunner and re-runs validateRange + --name-status + --unified=0 for a range the payload builder already diffed, duplicating ~4 git subprocesses per review and per resume | Reuse the payload builder's memoized rangeChunks/gitRunner for the same base..head instead of recomputing the zero-context diff (Deferred: Epic Plan 22.4 grounding_gitrunner_reuse) | performance | 45 | execute-epic-independent |

### [2026-06-29] From Sprint: 13.4_brace_language_parsers

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | LOW | internal/astgroup/parsers/src/braceparser/main.go:1 | Wasm ABI (alloc/free/parse/emit/pins) duplicated across three parser modules | Extract shared ABI into common guest package (Deferred: Epic Plan 22.2 astgroup_shared_guest_abi) | maintainability | 60 | code-review | bruce | MEDIUM |

### [2026-06-28] From Sprint: epic-13.4

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | LOW | internal/astgroup/parsers/src/braceparser/main.go:20 | The alloc/free/emit/pins guest ABI is duplicated across three parser sources (goparser, pyparser, braceparser); the threshold the existing code documents for extraction (parser count > 2) is now crossed | Extract the shared guest ABI into a package referenced via go.mod replace directives coordinated in build.sh (Deferred: Epic Plan 22.2 astgroup_shared_guest_abi — duplicate of braceparser/main.go:1) | MAINTAINABILITY | 60 | execute-epic-stage3 |

### [2026-06-27] From Sprint: 13.1_ast_plugin_architecture

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [/] | LOW | internal/astgroup/parsers/src/goparser/main.go:39 | The guest pins alloc'd buffers and hands the host int32(uintptr(unsafe.Pointer(&b[0]))) as a stable guest offset, valid only because the current Go GC is non-moving. This is an undocumented runtime invariant, not a language guarantee; a future moving GC makes the packed pointer a dangling offset with no detection. The unsafe alloc/free/emit/node boilerplate is copy-pasted across goparser, pyparser, and the 13.4 brace parsers, so any ABI fix must be applied N times. (Deferred: Epic Plan 22.2 astgroup_shared_guest_abi — the "revisit if parser count grows" trigger has fired (3 parsers); duplicate of braceparser/main.go:1) | Extract the shared alloc/free/emit/pins ABI into one internal guest package imported by every parser main.go, and add a build-time note pinning the GC assumption (or switch to an explicitly reserved arena). | maintainability | 120 | code-review | claude | MEDIUM |
| U | [/] | MEDIUM | internal/astgroup/parsers/src/pyparser/main.go:112 | scanTripleQuotes and stripComment are quote/escape-unaware. A triple-quote appearing inside a # comment, or a # appearing inside a string literal, flips the multi-line-string state machine the wrong way, so arbitrary spans of real code get classified as string content and dropped from significantLines, silently erasing blocks. The heuristic disclaimer documents the risk but does not bound it: on unusual source the structural hash diverges from the true AST. (Deferred: Epic Plan 22.3 pyparser_quote_aware_scanning — heuristic limitation already documented at pyparser/main.go:72-78 and 152-156, degrades to proximity grouping.) | Run scanTripleQuotes only over the code portion (skip after an unquoted #) and skip triple-quotes occurring inside single-line strings. Add regression fixtures: a # containing a triple-quote, and a string literal containing #. | security | 120 | code-review | claude | MEDIUM |

### [2026-06-23] From Sprint: 8.0_reconciler_library

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [/] | MEDIUM | unknown:0 | [Story 01 / Story 06] DoD item not implemented: both CI jobs (root ci.yml + reconcile-module PR-time job) must be marked as REQUIRED status checks on the main branch-protection rule. The CI workflow deliverables they depend on are all present and verified; only the protection-rule toggle is unset. The two story-level [ ] boxes (AC 01-06, AC 06-02) are the same single external action. (External action — one-click GitHub repo Settings -> Branches toggle, not epic-sized code work; no epic plan needed. See dod-completion-summary.md for original context.) | Configure branch protection in GitHub repo Settings -> Branches: add the root CI job and the reconcile-module PR-time job as required status checks. External repo-admin UI action (post-merge), not a source-tree change. | docs | 15 | code-review | claude | MEDIUM |

### [2026-06-22] From Sprint: 7.0.1_executor_model_configuration

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 7 | [/] | LOW | internal/registry/config.go:524 | Potential prompt injection via system_prompt (Won't-fix: config.go:531–534 explicitly documents that control chars are intentionally NOT rejected in system_prompt; the --- delimiter added at executor.go:225 eliminates the CRLF metadata-forgery surface; otto's fix conflicts with the documented design decision) | Add control character validation to SystemPrompt | security | 15 | code-review | otto | MEDIUM |

### [2026-06-20] From Sprint: epic-5.2

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/cache/store.go:115 | Eviction does a full ReadDir+Stat of the cache dir on every Put even when under the cap; O(n) per write scales poorly if the cache accumulates thousands of entries (Won't-fix 2026-06-21: scan runs serially under the store mutex and LLM calls dominate latency; no Epic 5.2 perf criterion requires O(1) Put — added state not justified at LOW) | Maintain a running total-size counter and skip the directory scan when it is under the cap, or evict only on a periodic/threshold basis | PERFORMANCE | 30 | execute-epic-cumulative |

### [2026-06-20] From Sprint: epic-5.0

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/report/render.go:317 | The "File not found" warning format string is duplicated across internal/reconcile/emit.go (writeFindingsList) and internal/report/render.go (writePathWarning) in separate packages | Extract a shared constant/helper only if a common low-level rendering package emerges; a cross-package dependency is not justified for one format string today (Won't-fix 2026-06-21: two independent format strings across packages with no shared dependency; the recorded fix confirms extraction is not justified at this scope) | CROSS_CUTTING | 15 | execute-epic-cumulative |
| U | [/] | MEDIUM | internal/reconcile/reconcile.go:26 | Validation root is hardcoded to "." at every call site, so "atcr reconcile <path>" for a review of another repo, or running from a non-repo-root CWD, falsely flags every finding as "file not found" | Thread the reviewed repo root explicitly or add a --repo flag, applied consistently with the verify stage which uses the same "." convention (Deferred 2026-06-21: the narrow Root: os.Getwd() variant is a no-op — filepath.Abs(".") already equals the CWD, so it would not fix the non-repo-root / other-repo case; the real fix is to plumb the reviewed-repo path explicitly via a --repo flag threaded through the reconcile and verify call sites, est 60) (Deferred: Epic Plan 22.1 reconcile_verify_repo_root_threading) | EDGE_CASES | 60 | execute-epic-independent |

### [2026-06-20] From Sprint: 4.7.1_backup-swap-hardening

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 3 | [/] | MEDIUM | internal/fanout/reviewdir.go:318 | backupExisting() unconditionally RemoveAll's <path>.bak.old and <path>.bak.new at entry, but forceBackupOutputDir() only calls guardForeignBackup(dir+".bak") — the two staging siblings are not guarded. On an arbitrary --output-dir a user who owns <output-dir>.bak.old or <output-dir>.bak.new has it silently deleted by --force, re-opening the Epic 4.7 "never silently delete user data" contract. NOTE: epic clarification Q4 deliberately scoped the guard to .bak only (atcr-internal suffixes); this finding re-raises that decision as worth reconsidering, not a defect against the recorded scope. (intent_note: deferred per epic §Clarifications Q4 — extending guardForeignBackup to .bak.old/.bak.new is out of scope) (Won't-fix 2026-06-20: binding Epic 4.7.1 Q4 — guard stays scoped to .bak; .bak.old/.bak.new are atcr-owned staging names cleared by entry-time RemoveAll; confirmed via /sprint-clarification 95%) | In forceBackupOutputDir() (reviewdir.go:440) extend the guard to guardForeignBackup(dir+".bak.old") and guardForeignBackup(dir+".bak.new") before backupExisting, or refuse if either exists non-empty. Add a test pre-creating a foreign <output-dir>.bak.old with user content and assert --force errors instead of deleting it. | security | 60 | code-review | claude | MEDIUM |
| 3 | [/] | MEDIUM | internal/fanout/reviewdir.go:373 | The advertised "an interrupted swap never leaves the user with neither generation" invariant is conditional on a best-effort SILENT restore succeeding. If restorePriorBackup's os.Rename(backupOld,backup) fails (e.g. backup partially recreated, or perms change), the only surviving copy is stranded under .bak.old — which the very next run's entry-time RemoveAll(backupOld) (line 318) then deletes. Same pattern at the copy site swapStagedBackup (atomic.go:159-167) feeding atomic.go:97 RemoveAll(bakOld). (Won't-fix 2026-06-20: observability half done (restore failure now logged); the lone-.bak.old-as-generation redesign is rejected per Epic 4.7.1 Q3 — .bak.old is atcr-owned temp deleted at entry, one-generation contract + TestBackupExisting_CleansStaleStagingStragglers stand; confirmed via /sprint-clarification 95%) | Surface restore failures loudly (log/return) instead of dropping them, and/or do not auto-RemoveAll .bak.old at entry when .bak is absent — treat a lone .bak.old as the surviving generation to recover. Add a test where restore fails and a subsequent run is asserted not to delete the only surviving copy. | correctness | 120 | code-review | claude | MEDIUM |
| 3 | [/] | LOW | internal/fanout/reviewdir.go:388 | backupCrossDevice's inner os.Rename(backupNew,backup) relies on backupNew and backup sharing a filesystem, an invariant that holds only by naming coincidence (both are siblings of path). If anyone later relocates backupNew under path, the inner rename silently becomes cross-device and returns a raw EXDEV to the user. (Won't-fix 2026-06-20: same-fs invariant holds by construction — backup and backupNew are both siblings of path; a runtime guard would be unreachable dead code today and is already documented at reviewdir.go:415-419; confirmed via /sprint-clarification 90%) | Add a test that forces renameFn to return syscall.EXDEV and makes the copy fail (a copy seam, or unreadable src), staging a prior .bak first; assert the prior .bak content is restored intact, the live tree survives, and .bak.new is cleaned up. Cover the copy-failure leg at minimum. | testing | 120 | code-review | claude | MEDIUM |
| 3 | [/] | LOW | internal/fanout/reviewdir_test.go:458 | Test-coverage gaps in the failed-swap/cleanup paths: TestBackupExisting_FailedSwapPreservesPriorBak asserts no .bak.old straggler but not .bak.new; the non-ErrNotExist Lstat(backup) error branch (reviewdir.go:333-335) is untested; the entry-time RemoveAll straggler-cleanup failure legs (reviewdir.go:318-323) are untested. Each is a real error branch a regression could silently break. | Add assert.NoDirExists for src+".bak.new" at reviewdir_test.go:458; add a perms-based test forcing Lstat(backup) to fail with a non-ErrNotExist error; add a test where .bak.old cannot be removed and assert the typed "clearing stale staging backup" error. Skip on root/CI where perms are not enforced. (Partial 2026-06-21: gaps 1 (.bak.new assertion) and 3 (RemoveAll(.bak.old) failure) covered; gap 2 (non-ErrNotExist Lstat(backup)) deferred — needs an lstatFn production seam since the staging siblings share a parent dir so perms cannot isolate it) (Deferred: Epic Plan 22.5 backup_swap_lstat_seam_test) | testing | 30 | code-review | claude | MEDIUM |

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
| 1 | [/] | MEDIUM | internal/scorecard/store.go:114 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; over-long-line warning uses injectable w via diagWriter; read continues; AC2 satisfied) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:145 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; decodeRecord writes to injectable w then returns (Record,bool) — no error cascade) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:155 | Error from diagWriter is silently discarded (Wontfix: intentional best-effort `_, _ =` diagnostics discard; schema-version skip writes to injectable w; identical to malformed-record path) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| 1 | [/] | LOW | internal/scorecard/store.go:194 | Redundant call to diagWriter (Wontfix: FALSE POSITIVE — diagWriter is the required typed-nil guard for the nil-able opts.Writer interface, not redundant; removing it reintroduces the panic fixed by commit 476c6d1) | Remove diagWriter call and use opts.Writer directly since ReadRecords already resolves it | performance | 2 | code-review | otto | MEDIUM |
| 1 | [/] | MEDIUM | internal/scorecard/store.go:211 | Error from diagWriter is silently discarded (Wontfix: FALSE POSITIVE — `_, _ = fmt.Fprintf` is the documented best-effort never-panic diagnostics contract at store.go:22-24; returning the write error would regress a successful read on a broken sink and logging is circular; confirmed working as designed via /sprint-clarification 97%) | Log or return the error from fmt.Fprintf | error-handling | 5 | code-review | dax | MEDIUM |
| U | [/] | MEDIUM | internal/mcp/handlers.go:220 | The MCP engine carries a structured *slog.Logger (e.log) used for every other diagnostic in handleReconcile (e.g. the require_verified warning at line ~225), but scorecard diagnostics are routed to raw os.Stderr, so MCP-path scorecard write-failures/malformed-record/orphan-verdict warnings emit as unstructured plaintext that bypasses the logger's level filtering, formatting, and sink redirection. NOTE: this was a DELIBERATE, documented epic decision (Clarifications Q2: supply os.Stderr at the call site; adapting e.log to an io.Writer was explicitly OUT of scope), so this is enhancement debt, not a regression. (intent_note: deferred per epic Clarifications Q2 — adapting e.log to an io.Writer is out of scope) (Wontfix: ACCEPTED ENHANCEMENT DEBT — handlers.go:220 is an unconditional EmitOpts{Diag: os.Stderr} call, not deferred logic; all five Epic 3.4 ACs are met and the comment at handlers.go:214-219 satisfies AC4; e.log→io.Writer adaptation is out of scope per Clarifications Q2; confirmed via /sprint-clarification 97%) | If MCP observability is later desired, adapt e.log into an io.Writer shim (slog-backed at Warn level) and pass it as Diag instead of os.Stderr, so MCP-path scorecard diagnostics flow through the same structured pipeline as the rest of the handler. Defer unless/until structured MCP diagnostics are required. | error-handling | 30 | code-review | claude | MEDIUM |

### [2026-06-14] From Review: llmclient OpenAI-compatible tool handling

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|------------|
| U | [/] | LOW | internal/llmclient/chat.go:1 | No provider-conformance test matrix for the OpenAI-compatible surface. The client deliberately absorbs real wire divergence (string-encoded vs raw-object tool_call `arguments`, lenient finish_reason) but is exercised only against synthetic fixtures, so a regression against a specific provider's actual tool_call shape (OpenAI, litellm, Ollama, vLLM, Together) would not be caught. This is the robustness the official SDK is assumed to provide, achievable here without adopting it. | Add a recorded-fixture conformance suite: capture a real `tool_calls` response from each target provider and assert the parser (`ToolCallArguments`, `chatToolResponse` decode, finish_reason handling) yields identical engine-facing results. NOTE: scope is a few days, not a quick-win — consider promoting to a standalone test-remediation plan rather than resolving inline. (Deferred: .planning/epics/deferred/5.1_openai_compatible_conformance_suite.md — was uncited; corrected 2026-07-05) | testing | 480 | review | claude | LOW |
