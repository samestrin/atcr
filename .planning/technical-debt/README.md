# Technical Debt Tracking

This file is a staging area for small technical debt items discovered during development. Items are triaged and moved to individual sprint documents in `sprints/` as they are prioritized.

## Stats

| Severity | Open | Deferred | Resolved |
|----------|------|----------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 1 | 3 | 0 |
| MEDIUM | 5 | 30 | 0 |
| LOW | 2 | 34 | 0 |

**Last Modified:** 2026-07-05 | **Open Items:** 8 | **Deferred Items:** 67 | **Resolved Items:** 0 | **Total Items:** 75

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


### [2026-07-05] From Review: pr-105-recovery

Recovered from stale, never-merged PR #105 (`td/2026-06-26`, 46 commits, 9 days behind `main` and conflicting). Before closing that PR, each of its fixes was checked against current `main`; most were already independently re-implemented, but these were not — they are re-added here as fresh items for `/resolve-td` rather than merging the stale branch directly.

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | HIGH | internal/tdmigrate/shard.go:116 | WriteShards prunes every `*.yaml` file already present in dir before renaming staged shards into place, without checking whether a given file is actually shard output (Accepted 2026-07-05: won't-fix — PR #107 (merged, squash 9bd724d7) independently revisited this exact function on 2026-06-27 (adding the atomic tmp+rename staging), and reaffirmed rather than changed the "dir is owned entirely by this tool" doc comment; `TestWriteShards_IdempotentPrune` already encodes whole-directory ownership as the intended contract. `items/` is documented in SCHEMA.md as exclusively used for TD shards, so this is a deliberate design choice, not a gap.) | Skip files that don't parse as a valid shard (or otherwise verify a file is this tool's own prior output) before removing an existing `*.yaml` during the prune step | CORRECTNESS | 30 | pr-105-recovery |
| 1 | [ ] | HIGH | internal/tdmigrate/validate.go:22 | DecodeShardStrict decodes only the first YAML document via a single `dec.Decode` call, so a shard file that accidentally contains a second `---`-separated document silently truncates to the first — the trailing document is lost with no error | Call `dec.Decode` again after the first successful decode and error unless it returns `io.EOF`, rejecting multi-document shard files | CORRECTNESS | 30 | pr-105-recovery |
| 1 | [ ] | MEDIUM | internal/tdmigrate/shard.go:140, internal/tdmigrate/validate.go:45 | LoadShards and ValidateDir both call `filepath.Glob` directly on dir — Go's Glob returns `(nil, nil)` for a missing directory, so a mistyped or missing `items/` path is silently treated as "0 shards" instead of failing | Stat dir first and return an error if it does not exist (or is not a directory) before globbing | EDGE_CASES | 25 | pr-105-recovery |
| 1 | [ ] | MEDIUM | internal/tdmigrate/shard.go:73 | WriteShards never validates that a Shard's Date field matches `YYYY-MM-DD` before using it to compose the shard's filename | Validate `s.Date` against the same date-format pattern used by `sectionHeader` before calling ShardFilename, and error on mismatch | EDGE_CASES | 20 | pr-105-recovery |
| 1 | [ ] | MEDIUM | internal/tdmigrate/parse.go:41 | ParseREADME's `driftHeader` regex requires a colon after the source type (`From ([^:]+):`), so a header with no colon at all matches neither `sectionHeader` nor `driftHeader` and is silently skipped — any data rows that follow get mis-attributed to whichever shard was previously open (or dropped if none was) | Add a catch-all malformed-header pattern (any `### [date] From` line not matched by sectionHeader) and return a loud parse error instead of falling through | EDGE_CASES | 25 | pr-105-recovery |
| 1 | [ ] | MEDIUM | internal/tdmigrate/parse.go:28 | ParseREADME's flush() only appends cur to shards when `len(cur.Items) > 0`, so a section header with zero parseable data rows is silently dropped rather than surfaced as an error | Track whether a section had zero items and return a parse error identifying the empty section by date/label instead of silently omitting it | EDGE_CASES | 20 | pr-105-recovery |
| 1 | [ ] | MEDIUM | internal/tdmigrate/parse.go:43 | ParseREADME skips any line not starting with a pipe character inside an open section, so a genuine data row that lost its leading pipe (e.g. a copy-paste error) is silently dropped as if it were prose, instead of erroring | Detect a line that looks like a data row (interior pipe-separated cells matching the expected column count) but is missing its leading pipe, and error instead of silently continuing | EDGE_CASES | 25 | pr-105-recovery |
| 1 | [ ] | LOW | internal/tdmigrate/item.go:106 | Item.Validate() ranges over a `map[string]string` of required fields, so when 2+ required fields are blank the reported "is required" field name is nondeterministic across runs | Iterate required fields in a fixed, declared order (slice of field/value pairs) instead of a map | MAINTAINABILITY | 15 | pr-105-recovery |
| 1 | [ ] | LOW | internal/tdmigrate/run.go:112 | runGenerate/runValidate's `fs.Parse(args)` treats `-h`/`--help` like any other parse error, exiting 2 with usage on stderr, instead of the conventional exit 0 with usage on stdout | Check `errors.Is(err, flag.ErrHelp)` after Parse and return 0 (with usage written to stdout) instead of falling through to the generic error path | EDGE_CASES | 20 | pr-105-recovery |

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
| 3 | [/] | LOW | internal/debt/add.go:162 | A single AppendItem re-reads and re-parses the README three times and writes it twice, and SyncShards regenerates the entire shard store via a full migrate. Bulk-adding N items in a loop is effectively O(N) full-store rewrites and 2N shard-directory rewrites. (Deferred: Epic Plan 18.1) | Expose a batch-append that mutates the README once, syncs shards once, and refreshes stats once for a batch; keep the single-item path for the interactive case. Verify by benchmarking a 100-item batch against 100 single adds. | performance | 120 | code-review | claude | MEDIUM |

### [2026-07-03] From Sprint: 17.0_auto_merged_fixes

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 4 | [/] | MEDIUM | internal/verify/localvalidate.go:81 | exec.CommandContext SIGKILLs only the direct child on timeout; a validation command that spawns subprocesses (e.g. sh -c make ...) leaves grandchildren orphaned and still running after the deadline, as cmd.WaitDelay only force-closes the parent pipe fds and does not reap the process group (intent_note: deferred per sprint-plan §2.5 (TD-006)) (Deferred: TD-006 / sprint-plan.md:296 — document, do not implement process-group kill now) | Set cmd.SysProcAttr Setpgid true and kill the whole group (syscall.Kill(-pid, SIGKILL)) on the cancel/timeout path (unix build-tagged) so grandchildren are reaped too | performance | 0 | execute-sprint | execute-sprint | MEDIUM |

### [2026-07-02] From Sprint: 15.1_leaderboard-cost-na-rendering

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | LOW | internal/scorecard/export.go:275-281 | Export groups records by the scrubbed persona/model, and scrubField's absolute-path regexes can reduce an entirely path-like input string to empty. Two genuinely distinct reviewer identities that both happen to be path-like scrub to the same empty string and silently merge into one aggregated group, blending the costPer/SurvivedSkepticRate/FindingsRaisedAvg values this epic's change is meant to make trustworthy. Adjacent to, not caused by, this epic, but feeds the same grouping key costPer's group totals depend on. (Won't fix: by-design -- scrubField intentionally merges identities that scrub-equal, locked in by TestExport_DistinctIdentitiesMergeWhenTheyScrubEqual, export_test.go:296) | By-design behavior, not a bug: grouping keys off the scrubbed identity is deliberate (scrubField strips PII/paths/secrets before export), and TestExport_DistinctIdentitiesMergeWhenTheyScrubEqual locks the merge-on-equal-scrub as an anti-regression invariant. No source change. | error-handling | 60 | code-review | claude | MEDIUM |

### [2026-07-01] From Sprint: 14.3_diff_chunking_context

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | MEDIUM | internal/fanout/review.go:941 | After the T3 refactor extracted renderAgent, the add closure re-implements mode resolution (forceMode/EffectivePayloadMode, payload lookup, not-found/no-payload errors) at review.go:818-831, but buildAgent (line 941) still contains the identical resolution logic and now has NO production caller — only tests (retry_wiring_test.go, review_scope_inject_test.go, review_test.go, review_tools_test.go) reference it (verified: grep buildAgent internal/ excluding _test shows only the definition). This is dead production code plus two divergent copies of the same mode/payload-resolution logic that will silently drift. (Deferred 2026-07-01: Epic Plan 14.4_fanout_resolution_dedup — kept as-is; buildAgent has no production caller, duplication accepted for now.) | Either delete buildAgent and update the tests to drive buildSlots/renderAgent, or have the add closure call buildAgent for the single-chunk/bulk path so there is one resolution site. Verify no non-test caller exists before deleting and that fallback-wiring tests still pass. | maintainability | 60 | code-review | claude | MEDIUM |

### [2026-06-30] From Sprint: epic-14.1

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | MEDIUM | internal/fanout/grounding.go:54 | out-of-scope category is a blanket grounding-gate exemption in all modes, so an adversarial or hallucinating model can launder a fabricated finding past AC2 by tagging category=out-of-scope in diff/blocks mode | Restrict the out-of-scope exemption to files mode (or require the cited FILE:LINE/EVIDENCE to anchor before honoring it in diff/blocks) — DEFERRED: conflicts with clarification Q2 which chose blanket exemption because out-of-scope is annotated-not-promoted by the reconciler downstream | correctness | 30 | execute-epic-independent |
| 1 | [/] | MEDIUM | internal/fanout/grounding.go:47 | groundFindings ignores Result.Tools and Result.PayloadMode, so a tool-enabled or files-mode agent citing a real line it legitimately read outside the changed range is dropped unless it tags out-of-scope or quotes a changed line in EVIDENCE | Thread Tools/PayloadMode into the gate and relax the range/evidence requirement for tool/files agents — DEFERRED: the out-of-scope prompt convention already instructs such agents to tag read-but-unchanged citations, which the gate exempts | correctness | 45 | execute-epic-independent |
| 1 | [/] | LOW | internal/fanout/review.go:470 | computeGroundingData builds a fresh gitRunner and re-runs validateRange + --name-status + --unified=0 for a range the payload builder already diffed, duplicating ~4 git subprocesses per review and per resume | Reuse the payload builder's memoized rangeChunks/gitRunner for the same base..head instead of recomputing the zero-context diff | performance | 45 | execute-epic-independent |

### [2026-06-29] From Sprint: 13.4_brace_language_parsers

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | LOW | internal/astgroup/parsers/src/braceparser/main.go:1 | Wasm ABI (alloc/free/parse/emit/pins) duplicated across three parser modules | Extract shared ABI into common guest package | maintainability | 60 | code-review | bruce | MEDIUM |

### [2026-06-28] From Sprint: epic-13.4

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | LOW | internal/astgroup/parsers/src/braceparser/main.go:20 | The alloc/free/emit/pins guest ABI is duplicated across three parser sources (goparser, pyparser, braceparser); the threshold the existing code documents for extraction (parser count > 2) is now crossed | Extract the shared guest ABI into a package referenced via go.mod replace directives coordinated in build.sh | MAINTAINABILITY | 60 | execute-epic-stage3 |

### [2026-06-28] From Sprint: epic-13.3

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | reconcile/pagerank.go:181 | Authority promotion is silent: no counter or Summary stat records that a HIGH came from authority rather than reviewer count, so a misfiring promotion is only derivable as HIGH-with-a-single-reviewer (Deferred: Epic Plan 13.4) | Add a Summary stat (e.g. AuthorityPromoted int) counting authority-promoted findings for observability; out of v1 scope since the clarification fixed the wire schema | OBSERVABILITY | 30 | execute-epic-independent |

### [2026-06-27] From Sprint: 13.1_ast_plugin_architecture

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [/] | LOW | internal/astgroup/parsers/src/goparser/main.go:39 | The guest pins alloc'd buffers and hands the host int32(uintptr(unsafe.Pointer(&b[0]))) as a stable guest offset, valid only because the current Go GC is non-moving. This is an undocumented runtime invariant, not a language guarantee; a future moving GC makes the packed pointer a dangling offset with no detection. The unsafe alloc/free/emit/node boilerplate is copy-pasted across goparser, pyparser, and the 13.4 brace parsers, so any ABI fix must be applied N times. (Deferred: extraction premature at 2 parsers; epic scope fixed at Go+Python — a future-remedy note is in each parser's ABI section, revisit if parser count grows.) | Extract the shared alloc/free/emit/pins ABI into one internal guest package imported by every parser main.go, and add a build-time note pinning the GC assumption (or switch to an explicitly reserved arena). | maintainability | 120 | code-review | claude | MEDIUM |
| U | [/] | MEDIUM | internal/astgroup/parsers/src/pyparser/main.go:112 | scanTripleQuotes and stripComment are quote/escape-unaware. A triple-quote appearing inside a # comment, or a # appearing inside a string literal, flips the multi-line-string state machine the wrong way, so arbitrary spans of real code get classified as string content and dropped from significantLines, silently erasing blocks. The heuristic disclaimer documents the risk but does not bound it: on unusual source the structural hash diverges from the true AST. (Deferred: accepted PoC scope; heuristic limitation already documented at pyparser/main.go:72-78 and 152-156, degrades to proximity grouping.) | Run scanTripleQuotes only over the code portion (skip after an unquoted #) and skip triple-quotes occurring inside single-line strings. Add regression fixtures: a # containing a triple-quote, and a string literal containing #. | security | 120 | code-review | claude | MEDIUM |

### [2026-06-26] From Sprint: 11.0_executing_reviewers

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 3 | [/] | MEDIUM | internal/tools/dispatch.go:146 | Tool gating for run_tests/run_script lives only in fanout.wireToolDefs (what the model is TOLD about); Dispatcher.Execute looks up d.handlers[name] with no check that the call was offered to the calling agent, and EnableExecution registers the exec handlers on the single dispatcher shared by the whole pool. The read-only guarantee for non-exec agents is therefore advisory, not structural — if any future caller enables exec non-uniformly across agents sharing one dispatcher, a non-exec agent could invoke run_script by simply naming it. No live exploit today: the sole exec caller, verify, sets exec uniformly for all skeptics. (Deferred: Epic Plan 11.1) | Pass the agent's allowed tool set (or Exec flag) into Execute and reject any call whose name was not offered to this agent, or gate the exec handlers behind a per-call capability rather than a globally-registered handler. Verify with a test where a non-exec agent emits a run_script tool_call and asserts it is refused. | security | 120 | code-review | claude | MEDIUM |

### [2026-06-25] From Sprint: epic-11.0

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/tools/exec_tools.go:69 | Execution tools are gated per-agent only at the definition level (wireToolDefs); the shared per-run dispatcher will execute a run_tests/run_script call from any agent once EnableExecution is wired. The sandbox isolates every run identically so this is not a containment gap, but a non-designated agent could still incur execution cost. (Deferred: .planning/epics/active/11.1_dispatcher-structural-gating.md — exec_tools.go:69 is a data struct, not a gating point; the offering-layer gate is already structural, and a runtime per-call guard is the multi-file change scoped to Epic 11.1) | Thread agent exec-eligibility into the dispatcher (or add a per-call guard) so only designated agents execute, for precise cost attribution. | SECURITY | 30 | execute-epic-stage3 |

### [2026-06-23] From Sprint: 8.0_reconciler_library

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 2 | [/] | HIGH | internal/reconcile/discover.go:25 | `internal/reconcile` keeps its discovery `Source` (Name + `[]stream.Finding` + Skipped + SkippedFiles); the library now defines a public `Source` (Name + `[]Finding`) | moved `Reconcile` takes the library `Source`; discovery output (`discover.Source`) is converted to `reconcile.Source` in the adapter/discovery layer | correctness | 0 | execute-sprint | execute-sprint | MEDIUM |
| U | [/] | MEDIUM | unknown:0 | [Story 01 / Story 06] DoD item not implemented: both CI jobs (root ci.yml + reconcile-module PR-time job) must be marked as REQUIRED status checks on the main branch-protection rule. The CI workflow deliverables they depend on are all present and verified; only the protection-rule toggle is unset. The two story-level [ ] boxes (AC 01-06, AC 06-02) are the same single external action. (intent_note: deferred per sprint-plan Final Phase / dod-completion-summary.md (external repo-admin action)) | Configure branch protection in GitHub repo Settings -> Branches: add the root CI job and the reconcile-module PR-time job as required status checks. External repo-admin UI action (post-merge), not a source-tree change; documented deferred in dod-completion-summary.md. | docs | 15 | code-review | claude | MEDIUM |

### [2026-06-22] From Sprint: 7.3_github_action_pr_integration

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 6 | [/] | MEDIUM | cmd/atcr/github.go:148-167 | Sequential inline comment posting is slow for PRs with many findings (Deferred: Epic Plan 7.6) | Use GitHub's batch POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews endpoint | performance | 45 | code-review | bruce | MEDIUM |
| 6 | [/] | LOW | cmd/atcr/github.go:148-167 | Conclusion is computed twice for the same findings/failOn inputs (Deferred: Epic Plan 7.6) | Use GitHub's batch POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews endpoint | performance | 45 | code-review | bruce | MEDIUM |

### [2026-06-22] From Sprint: epic-7.1

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | MEDIUM | internal/verify/syntaxguard.go:130 | An unfenced multi-line JSON/config snippet with block braces can still satisfy looksLikeGoCode and be parsed as Go, producing a spurious invalid_syntax flag on non-Go content (residual after heuristic hardening). | Detect obviously non-Go brace content (JSON object / key:value lines) before treating block braces as a Go signal; deferred as a separate design refinement (unfenced non-Go fixes are rare; fenced non-Go is already handled). [Deferred 2026-06-22 to Epic Plan 7.5 syntax-guard-refinements per clarification] | EDGE_CASES | 30 | execute-epic-independent |

### [2026-06-22] From Sprint: 7.0.1_executor_model_configuration

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 7 | [/] | LOW | internal/registry/config.go:524 | Potential prompt injection via system_prompt (Won't-fix: config.go:531–534 explicitly documents that control chars are intentionally NOT rejected in system_prompt; the --- delimiter added at executor.go:225 eliminates the CRLF metadata-forgery surface; otto's fix conflicts with the documented design decision) | Add control character validation to SystemPrompt | security | 15 | code-review | otto | MEDIUM |

### [2026-06-21] From Sprint: epic-6.0

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| 1 | [/] | MEDIUM | internal/debate/debate.go:165 | Gray-zone cluster merge/separate rulings are recorded in debate.json but not physically applied to findings.json; clusters still resolve via the existing adjudication path (Deferred: Epic Plan 6.1) | Wire the judge cluster decision into the reconcile adjudication application so unattended runs auto-merge gray-zone clusters inline | INTEGRATION | 60 | execute-epic-stage3 |

### [2026-06-20] From Sprint: epic-5.2

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/cache/store.go:115 | Eviction does a full ReadDir+Stat of the cache dir on every Put even when under the cap; O(n) per write scales poorly if the cache accumulates thousands of entries (Won't-fix 2026-06-21: scan runs serially under the store mutex and LLM calls dominate latency; no Epic 5.2 perf criterion requires O(1) Put — added state not justified at LOW) | Maintain a running total-size counter and skip the directory scan when it is under the cap, or evict only on a periodic/threshold basis | PERFORMANCE | 30 | execute-epic-cumulative |

### [2026-06-20] From Sprint: epic-5.0

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |
|-------|---|----------|------|---------|-----|----------|-------------|--------|
| U | [/] | LOW | internal/report/render.go:317 | The "File not found" warning format string is duplicated across internal/reconcile/emit.go (writeFindingsList) and internal/report/render.go (writePathWarning) in separate packages | Extract a shared constant/helper only if a common low-level rendering package emerges; a cross-package dependency is not justified for one format string today (Won't-fix 2026-06-21: two independent format strings across packages with no shared dependency; the recorded fix confirms extraction is not justified at this scope) | CROSS_CUTTING | 15 | execute-epic-cumulative |
| U | [/] | MEDIUM | internal/reconcile/reconcile.go:26 | Validation root is hardcoded to "." at every call site, so "atcr reconcile <path>" for a review of another repo, or running from a non-repo-root CWD, falsely flags every finding as "file not found" | Thread the reviewed repo root explicitly or add a --repo flag, applied consistently with the verify stage which uses the same "." convention (Deferred 2026-06-21: the narrow Root: os.Getwd() variant is a no-op — filepath.Abs(".") already equals the CWD, so it would not fix the non-repo-root / other-repo case; the real fix is to plumb the reviewed-repo path explicitly via a --repo flag threaded through the reconcile and verify call sites, est 60) | EDGE_CASES | 60 | execute-epic-independent |

### [2026-06-20] From Sprint: 4.7.1_backup-swap-hardening

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| 3 | [/] | MEDIUM | internal/fanout/reviewdir.go:318 | backupExisting() unconditionally RemoveAll's <path>.bak.old and <path>.bak.new at entry, but forceBackupOutputDir() only calls guardForeignBackup(dir+".bak") — the two staging siblings are not guarded. On an arbitrary --output-dir a user who owns <output-dir>.bak.old or <output-dir>.bak.new has it silently deleted by --force, re-opening the Epic 4.7 "never silently delete user data" contract. NOTE: epic clarification Q4 deliberately scoped the guard to .bak only (atcr-internal suffixes); this finding re-raises that decision as worth reconsidering, not a defect against the recorded scope. (intent_note: deferred per epic §Clarifications Q4 — extending guardForeignBackup to .bak.old/.bak.new is out of scope) (Won't-fix 2026-06-20: binding Epic 4.7.1 Q4 — guard stays scoped to .bak; .bak.old/.bak.new are atcr-owned staging names cleared by entry-time RemoveAll; confirmed via /sprint-clarification 95%) | In forceBackupOutputDir() (reviewdir.go:440) extend the guard to guardForeignBackup(dir+".bak.old") and guardForeignBackup(dir+".bak.new") before backupExisting, or refuse if either exists non-empty. Add a test pre-creating a foreign <output-dir>.bak.old with user content and assert --force errors instead of deleting it. | security | 60 | code-review | claude | MEDIUM |
| 3 | [/] | MEDIUM | internal/fanout/reviewdir.go:373 | The advertised "an interrupted swap never leaves the user with neither generation" invariant is conditional on a best-effort SILENT restore succeeding. If restorePriorBackup's os.Rename(backupOld,backup) fails (e.g. backup partially recreated, or perms change), the only surviving copy is stranded under .bak.old — which the very next run's entry-time RemoveAll(backupOld) (line 318) then deletes. Same pattern at the copy site swapStagedBackup (atomic.go:159-167) feeding atomic.go:97 RemoveAll(bakOld). (Won't-fix 2026-06-20: observability half done (restore failure now logged); the lone-.bak.old-as-generation redesign is rejected per Epic 4.7.1 Q3 — .bak.old is atcr-owned temp deleted at entry, one-generation contract + TestBackupExisting_CleansStaleStagingStragglers stand; confirmed via /sprint-clarification 95%) | Surface restore failures loudly (log/return) instead of dropping them, and/or do not auto-RemoveAll .bak.old at entry when .bak is absent — treat a lone .bak.old as the surviving generation to recover. Add a test where restore fails and a subsequent run is asserted not to delete the only surviving copy. | correctness | 120 | code-review | claude | MEDIUM |
| 3 | [/] | LOW | internal/fanout/reviewdir.go:388 | backupCrossDevice's inner os.Rename(backupNew,backup) relies on backupNew and backup sharing a filesystem, an invariant that holds only by naming coincidence (both are siblings of path). If anyone later relocates backupNew under path, the inner rename silently becomes cross-device and returns a raw EXDEV to the user. (Won't-fix 2026-06-20: same-fs invariant holds by construction — backup and backupNew are both siblings of path; a runtime guard would be unreachable dead code today and is already documented at reviewdir.go:415-419; confirmed via /sprint-clarification 90%) | Add a test that forces renameFn to return syscall.EXDEV and makes the copy fail (a copy seam, or unreadable src), staging a prior .bak first; assert the prior .bak content is restored intact, the live tree survives, and .bak.new is cleaned up. Cover the copy-failure leg at minimum. | testing | 120 | code-review | claude | MEDIUM |
| 3 | [/] | LOW | internal/fanout/reviewdir_test.go:458 | Test-coverage gaps in the failed-swap/cleanup paths: TestBackupExisting_FailedSwapPreservesPriorBak asserts no .bak.old straggler but not .bak.new; the non-ErrNotExist Lstat(backup) error branch (reviewdir.go:333-335) is untested; the entry-time RemoveAll straggler-cleanup failure legs (reviewdir.go:318-323) are untested. Each is a real error branch a regression could silently break. | Add assert.NoDirExists for src+".bak.new" at reviewdir_test.go:458; add a perms-based test forcing Lstat(backup) to fail with a non-ErrNotExist error; add a test where .bak.old cannot be removed and assert the typed "clearing stale staging backup" error. Skip on root/CI where perms are not enforced. (Partial 2026-06-21: gaps 1 (.bak.new assertion) and 3 (RemoveAll(.bak.old) failure) covered; gap 2 (non-ErrNotExist Lstat(backup)) deferred — needs an lstatFn production seam since the staging siblings share a parent dir so perms cannot isolate it) | testing | 30 | code-review | claude | MEDIUM |

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
| U | [/] | MEDIUM | internal/mcp/handlers.go:220 | The MCP engine carries a structured *slog.Logger (e.log) used for every other diagnostic in handleReconcile (e.g. the require_verified warning at line ~225), but scorecard diagnostics are routed to raw os.Stderr, so MCP-path scorecard write-failures/malformed-record/orphan-verdict warnings emit as unstructured plaintext that bypasses the logger's level filtering, formatting, and sink redirection. NOTE: this was a DELIBERATE, documented epic decision (Clarifications Q2: supply os.Stderr at the call site; adapting e.log to an io.Writer was explicitly OUT of scope), so this is enhancement debt, not a regression. (intent_note: deferred per epic Clarifications Q2 — adapting e.log to an io.Writer is out of scope) (Wontfix: ACCEPTED ENHANCEMENT DEBT — handlers.go:220 is an unconditional EmitOpts{Diag: os.Stderr} call, not deferred logic; all five Epic 3.4 ACs are met and the comment at handlers.go:214-219 satisfies AC4; e.log→io.Writer adaptation is out of scope per Clarifications Q2; confirmed via /sprint-clarification 97%) | If MCP observability is later desired, adapt e.log into an io.Writer shim (slog-backed at Warn level) and pass it as Diag instead of os.Stderr, so MCP-path scorecard diagnostics flow through the same structured pipeline as the rest of the handler. Defer unless/until structured MCP diagnostics are required. | error-handling | 30 | code-review | claude | MEDIUM |

### [2026-06-15] From Sprint: 3.3_per-run_scorecard

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [/] | LOW | internal/scorecard/store.go (FindByRunID adjacent-month notice; ReadRecords malformed-line notice); internal/scorecard/scorecard.go (Emit notices) | Store/emit diagnostics ("skipping malformed record", "run spans adjacent month files", "write failed") are written directly to the process-global `os.Stderr` rather than to a writer threaded from the cobra command (`cmd.ErrOrStderr()`). (Deferred: Epic Plan 3.4) | accept an `io.Writer` (or logger) on the store/emit entry points and have the CLI pass `cmd.ErrOrStderr()`, so warnings are capturable and redirectable. | ops | 0 | execute-sprint | execute-sprint | MEDIUM |

### [2026-06-14] From Sprint: 3.2_disagreement_radar

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [/] | LOW | internal/reconcile/disagree.go:280 | Duplicate writeRadarSection rendering logic risks divergent escaping across packages (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md) | Consolidate radar rendering into report package; reconcile should call report.writeRadarSection | security | 20 | code-review | greta | MEDIUM |
| 4 | [/] | LOW | internal/reconcile/disagree.go:350 | Duplicated radar section rendering logic (Deferred: Epic Plan 7.2) | Extract shared writeRadarSection to a common package | maintainability | 10 | code-review | bruce | MEDIUM |
| 4 | [/] | MEDIUM | internal/reconcile/disagree.go:354 | Duplicated radar markdown rendering diverges (Deferred: Epic Plan 7.2) | Extract shared writer or make reconcile use report package's escTrunc | maintainability | 10 | code-review | bruce | MEDIUM |
| U | [/] | LOW | internal/reconcile/disagree.go:413 | Redundant implementation of writeRadarSection (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md) | Remove duplicate function from internal/reconcile and use internal/report | maintainability | 15 | code-review | otto | MEDIUM |
| 5 | [/] | MEDIUM | internal/report/disagree.go:47 | The radar markup is rendered by two divergent copies: writeRadarSection + formatScore in internal/report/disagree.go and a structurally different copy in internal/reconcile/disagree.go:389. They are intentionally not identical (report copy truncates via escTrunc + uses joinReviewers/reviewerOrUnknown; reconcile copy uses uncapped esc + joinOrNone), so a future markup change (new field, reordered bullets) must be made in both or the live `atcr report` radar and the archival reconciled/report.md silently drift. (Deferred: .planning/epics/active/7.2_radar-renderer-consolidation.md) | Extract one shared item renderer parameterized by a truncate-vs-verbatim flag and heading prefix; have both call sites delegate. Add a test that diffs the rendered markup of both paths on the same DisagreementsFile asserting the only intended difference is truncation. | correctness | 60 | code-review | bruce, claude | HIGH |

### [2026-06-14] From Sprint: 2.2_code_review_fanout_hardening

| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source | Reviewers | Confidence |
|-------|---|----------|------|---------|-----|----------|-------------|--------|---------|----------|
| U | [/] | MEDIUM | internal/fanout/postprocess.go:14 | The severity-rank rubric {CRITICAL:4,HIGH:3,MEDIUM:2,LOW:1} is independently redefined in fanout/postprocess.go:17, reconcile/merge.go, verify, report, plus a set-form copy reviewSeverities in registry/config.go. postprocess looks up severityRank[strings.ToUpper(...)] while reconcile looks up the raw value, so a future severity change or non-canonical casing silently desyncs fan-out truncation from reconcile merging. The postprocess copy was newly added by Epic 2.2. (disagreement: LOW vs MEDIUM) (Deferred: Epic Plan 3.5) | Extract a single canonical severity package (or export from internal/stream) exposing the ordered rank map plus normalizeSeverity, and have registry/fanout/reconcile/verify/report consume it. Verify by deleting the local maps and confirming the suite passes with one source of truth. | maintainability | 120 | code-review | claude | MEDIUM |

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
