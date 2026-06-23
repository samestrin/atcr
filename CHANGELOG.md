## [Technical Debt] - 2026-06-23

### Fixed

- Made `BuildFileIndex` cancellation-aware so `git ls-files` honors context cancellation.
- Honored context cancellation before skeptic dispatch in the verify pipeline.
- Added best-effort cleanup of superseded `.bak` backups in `internal/atomicfs`.
- Validated the `--output` path for `atcr leaderboard --export` through `validation.FilePath`.
- Capped server-advertised `Retry-After` values to prevent unbounded or overflowing backoff waits.
- Scrubbed `base_url` userinfo from `atcr doctor` `HTTPStatusError` snippets.
- Stripped markdown link/image injection vectors from the GitHub Action defang path.
- Warned on `GITHUB_OUTPUT` open/write failures in `atcr github` instead of swallowing errors.
- Redacted GitHub tokens from `internal/ghaction` error messages.
- Replaced `math/rand` with `crypto/rand` for the skeptic prompt-injection sentinel.

*Shipped via /resolve-td + /finalize-td*

## [Production-Readiness Review] - 2026-06-23

Pre-Epic-8 production-readiness audit (component axis, adversarially verified across the five subsystem clusters). Verdict: ship-with-fixes — 0 critical, 1 high, 11 hardening items. The high-severity item is fixed below; the 11 hardening items are logged to technical-debt tracking.

### Fixed

- `atcr github --inline-comments` now paginates the existing-comment fetch in `ListReviewComments`. The GitHub REST list endpoint returns at most 30 comments per page by default, so on a pull request already carrying more than 30 ATCR inline comments the dedup pass silently missed the older comments and re-posted duplicates on every run. The fetch now walks every page (`per_page=100`) until a short page signals the end.

*Shipped via /code-review production-readiness audit*

## [Technical Debt] - 2026-06-23

### Fixed

- Verified deduplication count correctness on the fallback comment path.
- Suppressed stdout summary when all fallback comments are 422-skipped.
- Ensured fallback hard errors return a coded `exitFailure`.
- Verified partial-fallback hard error count remains correct.
- Confirmed `BuildCheckOutput` returns a neutral conclusion for non-empty findings when the threshold is empty.
- Documented non-atomicity and dedup-on-retry idempotency in the fallback comment path.
- Paced fallback comment posts and honored cancellation between them.
- Capped individual fallback comment posts to bound GitHub API calls.
- Noted that 404 fallback may mask a bad `--pr` value in `cmd/atcr/github.go`.

*Shipped via /resolve-td + /finalize-td*

## [7.6.0] - 2026-06-22

GitHub Action API efficiency improvements for posting reconciled findings to pull requests.

### Added

- `atcr github --inline-comments` now falls back to posting comments individually when the batched review endpoint is unavailable (HTTP 404/405), so inline comments work on older GitHub Enterprise versions that do not support it. A per-comment off-diff rejection (422) stays a non-fatal skip, matching the batch path.

### Changed

- Removed a redundant gate-conclusion computation in the `github` command: `BuildCheckOutput` now returns the conclusion and blocking-finding count it already computes, so the findings slice is traversed once instead of twice.

*Shipped via /execute-epic (epic 7.6)*

## [Technical Debt] - 2026-06-22

### Fixed

- Distinguished absent from malformed reconciled findings in `readReconciledFindings` with separate exit codes.
- Treated GitHub API 422 errors from `CreatePRReview` as non-fatal via a new `APIError` type.
- Replaced raw conclusion literals in `internal/ghaction` with typed constants.
- Backtick-wrapped `cell()` output in `internal/ghaction/render.go` to neutralize markdown injection.
- Removed duplicate `FixAttribution` grammar from `internal/ghaction/render.go`.
- Added fetch-depth and event-type guard steps to the composite GitHub Action.
- Pinned the `escTrunc` 500-rune cap boundary and closed related false-positive TD citations.
- Locked in dispatcher reuse and read_file dispatch assertions in executor tests.
- Documented `FixWarning` and `ToolBudgetBytes` ownership boundaries.
- Tightened `looksLikeNonGoBraces` documentation and residual config false-positive notes.

*Shipped via /resolve-td + /finalize-td*

## [7.5.0] - 2026-06-22

Refined the local syntax guard so it no longer raises a spurious "invalid syntax" warning on a generated fix that is unfenced JSON/config rather than Go. Fix-quality flagging is unchanged for actual Go.

### Fixed

- The Epic 7.1 syntax guard no longer flags an unfenced multi-line JSON/config object (block braces, quoted keys) as `invalid_syntax`. Detection keys on JSON object-member shape (a quoted key at line start with no Go declaration keyword) and only ever *suppresses* a flag — it can never add one — so the conservative-recall guarantee is preserved and no previously-clean fix becomes newly flagged.

*Shipped via /execute-epic (epic 7.5)*

## [7.4.0] - 2026-06-22

Added an opt-in agent mode for the executor: instead of generating a fix from a single fixed code snippet, the executor can now explore the codebase with read-only tools before proposing a change — better for cross-file findings whose fix depends on a type, interface, or caller defined elsewhere. Default behavior is unchanged.

### Added

- `agent_mode` (default `false`) and `max_tool_calls` (default `10`, bounded `1..1000`) on the `executor:` registry block. When `agent_mode: true`, the executor borrows the skeptics' already-open read-only tool harness and runs a `read_file`/`grep`/`list_files` loop to gather context before proposing the minimal fix, reusing the existing snapshot (no second checkout). On reaching the tool-call budget it emits the best fix from the context gathered; a tool-loop timeout, provider error, or unparseable response records a fix warning on the finding and never fails the run. When the harness is unavailable it degrades to the unchanged snippet path with a logged warning.

*Shipped via /execute-epic (epic 7.4)*

## [7.3.0] - 2026-06-22

Shipped a maintained GitHub Action that runs the atcr panel on a pull request and surfaces findings where developers work — a PR check that gates the merge and, optionally, inline comments rendering the suggested fix.

### Added

- A composite GitHub Action (`action.yml`) that builds atcr, runs `atcr review` + `atcr reconcile` on a pull request, and posts the result as a PR check honoring `--fail-on`, with opt-in inline comments. Context and inputs flow through `env:` to avoid Actions script injection.
- `atcr github` subcommand: renders reconciled findings onto a pull request as a check run (with a findings table and a pass/fail conclusion), and — behind the default-off `--inline-comments` toggle — posts inline comments formatted "ATCR found: <problem>. Fix: <fix>. Suggested by: <executor>", parsing the executor from the finding's evidence attribution token.
- `docs/github-action.md` documenting usage, inputs, required permissions, the `fetch-depth: 0` requirement, and a manual real-PR smoke-test procedure; referenced from `docs/ci-integration.md`.

*Shipped via /execute-epic (epic 7.3)*

## [7.2.0] - 2026-06-22

Consolidated the two divergent disagreement-radar renderers into a single shared, parameterized implementation, eliminating the risk of escaping/formatting drift between the reconciled and display report paths. Rendered output is unchanged.

### Changed

- The disagreements radar is now produced by one shared renderer in `internal/reconcile` (`WriteRadarSection` / `WriteRadarItems`), parameterized by a heading prefix and a free-text body renderer (`RadarTextRenderer`). `internal/report` calls it with display-oriented truncation (`escTrunc`, 500-rune cap); the reconciled report passes verbatim escaping (`esc`). Output is byte-identical.

### Removed

- The duplicate radar-rendering code in `internal/report` (`writeRadarSection`, `writeRadarItems`) and its now-dead `formatScore` and `reviewerOrUnknown` helpers.

*Shipped via /execute-epic (epic 7.2)*

## [Technical Debt] - 2026-06-22

### Fixed

- Narrowed `blockOpenRe` so it only matches a single trailing block-open brace, closing the false-fire on any brace-ending line.
- Hardened `fenceRe` to accept `c#`/`f#` language tags, require an anchored close fence, support four-backtick fences, and tolerate trailing whitespace.
- Capped the input size in `validateGoFixSyntax` so oversized fixes cannot cause unbounded parsing cost.
- Guarded `generateFixes` against a nil registry to prevent runtime panics.
- Documented the `FixWarning` ownership boundary in `generateFixes`.
- Locked in `FixWarning` JSON round-tripping, markdown golden rendering, and injection/truncation coverage with tests.
- Corrected the `InvalidShortAssign` rationale and characterized the conservative-recall boundary.
- Recorded the conservative-recall WONTFIX rationale in `parseGoFix` and `extractFencedCode`.
- Removed the misleading "compile" wording from the syntax-guard header (it parses only, no type checking).
- Resolved TD items 137, 94, and 167; deferred item 130 per clarification answers.
- Created epic 7.5 syntax-guard refinements and pointed the deferred TD row at it.

*Shipped via /resolve-td + /finalize-td*

## [7.1.0] - 2026-06-22

A fast, local validation layer that checks generated fixes are syntactically valid before they are presented, so syntax-broken fixes are caught immediately without running the full test suite.

### Added

- Local Go syntax guard for generated fixes: each executor-generated fix is parsed with `go/parser` before it is presented. A fix that is plausibly Go code yet fails to parse is flagged with an `invalid_syntax: <parser error>` warning while the attempted fix stays visible.
- Fix-generation warnings (including the new `invalid_syntax` flag) now surface in the markdown review report.

### Changed

- The guard is deliberately conservative: free-form prose change-instructions and explicitly non-Go fenced blocks pass through unflagged, so a legitimate fix is never falsely marked invalid.

*Shipped via /execute-epic (epic 7.1)*

## [Technical Debt] - 2026-06-22

### Fixed

- Added a `---` delimiter between executor instructions and finding data in `buildFixPrompt`, closing the prompt-injection surface for forged metadata lines.
- Guarded `buildFixPrompt` against a nil executor pointer to prevent runtime panics.
- Enforced a maximum rule-count cap on executor `rules` so the list cannot defeat the per-rule length limit.
- Rejected Unicode control characters and line separators in executor persona, name, rules, and agent scope.
- Fixed persona default re-derivation in `buildFixPrompt` by relying on the loaded-registry invariant.
- Rejected `NaN` and `Inf` values in executor `temperature` validation.
- Documented the `system_prompt` control-character trust boundary and the direct-call temperature caveat in `registry.md`.

*Shipped via /resolve-td + /finalize-td*

## [7.0.1] - 2026-06-22

Executor model configuration — tightly control fix-generation determinism and style from the registry without editing ATCR source.

### Added

- Executor `temperature` setting (validated to `[0, 2]`) — controls the API temperature for fix generation. It is sent on every fix call and defaults to `0.0` when omitted, so generated fixes are deterministic and reproducible.
- Executor `system_prompt` override — when set, it fully replaces the default `"You are <persona>, a code-fix executor…"` framing (the `persona` is superseded for that call); the finding metadata, rules, and code snippet are still appended after it. Capped at 4096 characters.
- Executor `rules` list — project coding guidelines appended to the fix prompt as a constraints block so generated fixes match your conventions (and pass linters on the first CI run). Each rule is validated at load: non-empty, free of control characters, and at most 512 characters.

### Changed

- The executor now always sends a `temperature` to the provider (defaulting to `0.0`), whereas previously it sent none and inherited the provider's own — often non-deterministic — default. Existing `executor:` configs without an explicit `temperature` will now produce deterministic fixes; set `temperature` explicitly to opt into a higher value.

*Shipped via /execute-epic (epic 7.0.1)*

## [Technical Debt] - 2026-06-22

### Fixed

- Removed the unused `ExecutorConfig.BatchFixes` field to eliminate a silently misleading registry option.
- Hardened `validateExecutor` provider and model checks with `strings.TrimSpace` so whitespace-only values report a clear missing-field error.
- Made agent and executor role validation case-insensitive and canonicalized stored role values.
- Validated executor `persona` at load time (control characters and length cap) and documented `buildFixPrompt` untrusted-data boundaries.
- De-scoped epic 7.0 AC9 (fix-generation performance benchmark) with rationale against the fake-completer test harness.
- Added `ExecutorConfig.EffectiveFixMinSeverity()` and replaced duplicated inline fix-severity-floor fallbacks.
- Replaced substring-based executor attribution with a delimited-token guard and validated executor names against the agents map.
- Applied an effective per-call executor timeout even when `fix_timeout` is nil, preventing unbounded blocking on a hung provider.
- Parallelized fix generation with a bounded worker pool, capping wall-clock cost for many eligible findings.
- Bailed `generateFixes` early on context cancellation so SIGINT stops new executor calls promptly.
- Cleared stale `FixWarning` values when a fix succeeds, removing contradictory finding records on re-runs.
- Logged `fix_snippet_unavailable` debug warnings instead of silently swallowing snippet-read failures.
- Removed a redundant `TrimSpace` short-circuit in `readFixSnippet`.
- Constructed the executor client lazily after the first fix-eligible finding, avoiding unused client allocation.
- De-scoped epic 7.0 AC10 (quantitative cost analysis) and added a reflection drift-guard test for `JSONFindings()` field coverage.

*Shipped via /resolve-td + /finalize-td*

## [7.0.0] - 2026-06-21

Executor model for fix generation — multi-model review breadth plus a single, more capable model that generates fixes for the findings worth fixing.

### Added

- Optional top-level `executor:` registry block: a single model that generates fixes for verified findings during `atcr verify`, `atcr review --verify`, and the `atcr_verify` MCP tool. Opt-in and fully backward-compatible — with no `executor:` block, no fix phase runs and behavior is unchanged.
- Fix targeting gates on HIGH-or-better confidence (so skeptic-confirmed `VERIFIED` findings are included) AND severity at or above `min_severity_for_fix` (default MEDIUM). The executor reads a code snippet around each finding from the review snapshot for context, writes the fix into the finding's `fix` column, and appends `fix by <name>` to its evidence (no new column — the 9-column schema is preserved).
- Executor configuration: `min_severity_for_fix`, `batch_fixes`, and `fix_timeout`; a `fix_warning` field on findings recording non-fatal fix-generation failures; and the `reconcile.ConfidenceAtOrAbove` confidence-ordering helper.
- Example registries (`examples/registry-with-executor.yaml`, `examples/registry-without-executor.yaml`) and an executor section in `docs/registry.md`.

### Notes

- Fix generation is failure-isolated: an executor error or empty completion leaves the reviewer's own fix in place and never fails the verify run.
- Posting these fixes as inline PR comments is owned by Epic 7.3 (the GitHub Action), which consumes the `fix` column populated here.

*Shipped via /execute-epic (epic 7.0)*

## [Technical Debt] - 2026-06-21

### Fixed

- Hardened `filterMergedClusters` against empty `ClusterID` lookups and added debug logging of suppressed-item counts.
- Refused empty-ID cluster merges in `applyOneClusterMerge` to prevent re-debate loops on malformed clusters.
- Collapsed duplicated `clusterDisplayProblem` logic into a single `reconcile.ClusterDisplayProblem` helper and added a round-trip test to pin the coupling.
- Added a location-keyed fallback for drifted gray-zone items so merged clusters stay merged even when the representative problem text diverges.
- Detected colliding display keys in `indexClusters` with a sentinel so distinct clusters sharing the same canonical key no longer overwrite each other.
- Avoided empty cluster-index allocation and documented nil-safety for `filterMergedClusters`.
- Extended `filterMergedClusters` tests to cover mixed legacy/new-ID survivor cases and separate-ruling `ClusterID` absence.
- Updated `JSONFinding` type docs to reflect the additive `cluster_id`/`cluster_merged` schema extensions and cross-referenced the `ClusterID` field scope.
- Pinned the reconcile-path-never-stamps-cluster-fields invariant with a test to guard AC1 byte-identity.

*Shipped via /resolve-td + /finalize-td*

## [6.2.0] - 2026-06-21

### Added

- `cluster_id` field on `findings.json` records (omitempty): the stable, content-addressed ID of the gray-zone cluster that produced an inline-merged survivor. Non-merged records stay byte-identical to pre-6.2 output.

### Fixed

- Gray-zone merge idempotency is now keyed on cluster identity (`cluster_id`) rather than File+Line alone, so a second distinct cluster co-located at the same canonical File+Line is no longer over-suppressed once the first is merged.

*Shipped via /execute-epic (epic 6.2)*

## [6.1.0] - 2026-06-21

### Added

- Inline application of judge gray-zone cluster rulings: a `merge` ruling now physically unions the cluster's member findings in `findings.json` during the debate stage (no `adjudication.json` round-trip), and a `separate` ruling leaves them unmerged.
- `cluster_merged` marker on merged findings so a re-run never re-merges an already-applied cluster.

### Changed

- Gray-zone cluster decisions are applied inline (Option A) instead of being recorded only for the Skill adjudication path; the authored `adjudication.json` path is unchanged and remains a manual override.

*Shipped via /execute-epic (epic 6.1)*

## [Technical Debt] - 2026-06-21

### Fixed

- Documented that debate `pickDistinct` enforces distinctness by exact model string only.
- Left finding severity untouched when a split ruling carries no `settled_severity`.
- Validated debate verdict enum before persisting `Verification` so empty/invalid verdicts cannot be written.
- Generated the debate prompt-injection sentinel from `crypto/rand` with 128-bit entropy.
- Added a bounded worker pool for debate item processing to parallelize provider calls.
- Preserved original verify skeptic list and notes when `applyRulings` mutates `Verification`.
- Flattened untrusted newlines inside debate item blocks to block in-block prompt injection.
- Rendered a `challenge-survived` badge on upheld and split contested findings.
- Truncated unresolved reason text in the contested report with `escTrunc`.
- Annotated overturned findings as excluded so severity tags no longer appear live.
- Disclosed single-model fallback usage at the contested section level and on unresolved items.
- Allowed `--require-verified` alongside `--debate` without requiring `--verify` in the CLI.
- Consolidated debate trigger defaulting to a single source of truth in `ResolveConfig`.
- Rejected `--single-model` on `atcr review` when `--debate` is not set.
- Added a shared representative-problem function for `BuildDisagreements` and `clusterDisplayProblem`, pinning the gray-zone cluster coupling with a round-trip test.
- Fixed `applyOneClusterMerge` cross-line drift recovery so an unrelated lone finding at a member location is no longer absorbed into the merge.
- Skipped single-member clusters in the merge-applied count, removing the misleading "could not be applied" warning.
- Captured and logged errors from reading `ambiguous.json` instead of silently degrading to zero clusters.
- Normalized judge `cluster_decision` input before the merge/separate equality check.
- Added a defensive assertion that gray-zone member locations never collide with the single-finding rulings keyspace.
- Recorded a distinct no-decision reason in `debate.json` for empty or unparseable judge cluster rulings.
- Preserved each member's existing `Disagreement` lower bound in `MergeJSONFindings` so merged records keep the full reviewer tension range.
- Preserved the first non-empty `PathWarning`/`PathSuggestion` across cluster members instead of copying only the first group member.
- Split and deduplicated comma-joined skeptic names in `mergeVerification`, guarding against empty tokens and corrupted provenance.
- Replaced the evidence-join loop in `MergeJSONFindings` with `strings.Join`.

*Shipped via /resolve-td + /finalize-td*

## [6.0.0] - 2026-06-21

Cross-examination (debate) stage: resolve reviewer disagreements through a bounded proposer/challenger/judge debate instead of a heuristic. Where reviewers dispute severity, the reconciler leaves a gray-zone cluster, or a skeptic vote ties, the judge's ruling — not severity-max — settles the finding, and survivors form the top confidence tier.

### Added

- `atcr debate [id-or-path]` command, `atcr review --verify --debate` chaining, and the `atcr_debate` MCP tool — all sharing one orchestrator. `--single-model` opts into a same-model persona fallback when fewer than three distinct models are available.
- `debate.*` registry config (`triggers`, `max_items`, `allow_single_model`), validated in lockstep with the config schema.
- A `challenge_survived` marker on upheld findings (folded into the existing `VERIFIED` tier, not a new tier), the `reconciled/debate.json` ruling record, and replayable per-item transcripts under `debate/`.
- A "Contested findings" report section showing judge rulings with one-line rationales, severity transitions, single-model disclosure, and recorded overflow.

### Changed

- Disputed findings — severity splits (≥2 tiers), gray-zone clusters, and verification disagreements — route through a debate that settles them: `uphold`/`split` confirm (split replaces severity-max with the judge's settled severity), `overturn` refutes. Distinct-model enforcement spans all three seats; the protocol is bounded (hard 3-turn cap, `max_items` cost cap with recorded overflow) and idempotent across re-runs.

*Shipped via /execute-epic (epic 6.0)*

## [Technical Debt] - 2026-06-21

### Fixed

- Documented `CopyPath`'s actual merge-into-existing-destination behavior.
- Documented the `SecretValues` construction-time snapshot contract for environment keys.
- Documented the `minSecretLen` over-redaction tradeoff.
- Documented the intentional `PathWarning` omission in `RenderText`.
- Rendered the actual `PathWarning` value for non-default warnings in human reports.
- Surfaced `SecretValues` misconfiguration (unset or below-minimum values) via returned warnings.
- Covered backup straggler-cleanup failure legs in tests.

*Shipped via /resolve-td + /finalize-td*

## [Technical Debt] - 2026-06-21

### Fixed

- Normalized backslash path separators in file-index lookups so cited paths match regardless of the build OS.
- Clarified that path case folding uses ASCII-lowercase, not full Unicode case folding.
- Prevented path-suggestion from offering a sibling correction when the cited path itself is tracked.
- Added a length-difference early-out before Levenshtein distance computation in Tier 2 suggestions.
- Surfaced corrupt cache-entry removal errors instead of failing silently.
- Documented the deliberate cache eviction error-swallow asymmetry under the store mutex.
- Added observability for indeterminate path existence checks via a metrics counter.
- Logged discarded git failures during file-index builds instead of swallowing them.
- Made path-escape containment checks separator-independent across platforms.
- Surfaced swallowed EvalSymlinks root errors via a metrics counter.
- Committed the `.atcr/` ignore rule so cache and review outputs are ignored in fresh clones and CI.
- Carried `ToolsRequested` on synthesized cache-hit results to match live invocation shape.
- Protected the newest cache entry during eviction so oversized writes survive their own eviction pass.
- Made `atcr init` emit `.atcr/.gitignore` so cache and reviews are never committed by default.
- Folded backend provider identity into diff-cache keys to prevent cross-provider cache collisions.

*Shipped via /resolve-td + /finalize-td*

## [5.4.0] - 2026-06-21

A flagged file path now comes with a way forward. When a reviewer cites a path that does not exist, the reconcile pipeline builds a one-time index of the repository's real tracked files (`git ls-files`) and suggests the file the finding most likely meant — a wrong directory, a basename typo, or a case-only difference — rendered as a `(did you mean …)` line next to the warning. Suggestions are advisory only: the original cited path is always preserved and never rewritten. The existence check is also now symlink-safe, closing an oracle where a repo symlink could report a file outside the repo as present, and case-only typos are caught even on case-insensitive macOS/Windows filesystems where they previously resolved silently.

### Added

- Candidate file index built once per reconcile run from `git ls-files` (gitignore-pruned, tracked files only), backing path-correction suggestions
- `path_suggestion` field on the `findings.json` record (omitempty — absent and byte-identical to prior output when there is no suggestion) and a `⚠️ File not found: <path> (did you mean <real>?)` line in `report.md`, `atcr report`, and the checklist and refuted views
- Tiered path matching: exact basename in another directory (no edit-distance threshold), a same-directory basename typo above a tuned similarity threshold (with a guard against pluralization/derivation siblings), and case-only differences

### Changed

- The finding existence check resolves symlinks and re-verifies containment under the repo root (`filepath.EvalSymlinks`) instead of a bare `os.Stat`, so a symlinked path segment can no longer probe for files outside the reviewed repository

### Fixed

- Case-only path typos (e.g. `Parser.go` for `parser.go`) are now flagged and corrected even on case-insensitive filesystems, where the prior existence check reported them as valid

*Shipped via /execute-epic (epic 5.4)*

## [5.2.0] - 2026-06-20

Repeated reviews of an unchanged diff no longer re-spend tokens. Each reviewer's output is now content-addressed under `.atcr/cache` and replayed on a re-run when the rendered prompt, model, and temperature are identical — so iterating with `atcr review` on a branch that has not changed (or changed elsewhere) skips the LLM call for every cached agent. Caching is scoped to the single-shot review fan-out only; tool-enabled agents (which read live code) and the verification stage are never cached. A failed or timed-out review is never cached.

### Added

- Diff cache for the review fan-out: an unchanged payload+model+temperature replays the prior reviewer output instead of calling the provider, cutting cost and latency on repeated runs. Cache keys derive from the full rendered prompt (which subsumes payload, persona, per-agent scope focus, and refs), the model id, and the temperature, so any change that alters the LLM input invalidates the entry
- `cache_max_bytes` config key (registry and project tiers; default 50 MiB, `0` = unbounded) bounding the cache with least-recently-used eviction
- `atcr review --no-cache` flag to bypass cache reads and force a fresh review (fresh results are still written back so subsequent runs benefit)
- `cache_hit` field in a per-agent `status.json` (present only on a replayed result) so a cache hit is auditable rather than indistinguishable from a live call

*Shipped via /execute-epic (epic 5.2)*

## [5.0.0] - 2026-06-20

Reviewer-hallucinated file paths are now caught. The reconcile pipeline validates each finding's file path against the reviewed repository and flags the ones that do not exist, so a good finding that cites a wrong path (a typo, or the right file in the wrong directory) is surfaced for correction instead of silently shipping an unopenable location. The finding is always preserved — validation only annotates, it never discards.

### Added

- File-path existence validation in the reconcile pipeline — a finding whose path does not resolve under the repo root is flagged with `path_warning: "file not found"` in `findings.json` and a `⚠️ File not found: <path>` line in `report.md`, `atcr report`, and the checklist view
- `path_valid` / `path_warning` fields on the `findings.json` record (`path_warning` is the authoritative signal; paths that escape the repo root are treated as invalid rather than probed)

*Shipped via /execute-epic (epic 5.0)*

## [4.11.0] - 2026-06-20

Per-call client telemetry now backs accurate API-call counting and per-call latency: the LLM client surfaces one record per HTTP attempt — retries included, with a precise wire-reached signal — up through the fan-out engine, fixing a counter undercount during provider degradation and populating a latency histogram.

### Added

- `atcr_api_call_duration_seconds` histogram recording the wall-clock latency of every completed HTTP attempt that reached the wire

### Changed

- `atcr_api_calls_total` now counts provider round-trips per HTTP attempt (retries included) and correctly counts a single-shot agent whose context expires mid-flight as 1 (previously 0); a request cancelled before any bytes were sent — or a circuit-open fail-fast — still counts 0. Dashboards comparing values across this change will see a one-time step up as retries begin to count.

*Shipped via /execute-epic (epic 4.11)*

## [4.9.0] - 2026-06-20

Exact-value secret redaction is now live in production: resolved registry API key values are threaded into the log redactor at both per-review construction sites, so provider keys are scrubbed by their actual value rather than relying solely on the `sk-`/`Bearer` token-shape patterns.

### Security

- Provider API keys that lack an `sk-`/`Bearer` prefix (Google `AIzaSy…`, Azure, JWTs) are now scrubbed by exact value from both `atcr review`/`--resume` and serve-mode logs, closing a gap where such keys could otherwise leak verbatim

### Added

- `PreparedReview.SecretValues` enumerates the resolved registry API key values (deduped, with a minimum-length guard against over-redaction) for the log redactor at the review, resume, and serve-mode construction sites

*Shipped via /execute-epic (epic 4.9)*

## [Technical Debt] - 2026-06-20

### Fixed

- `backupCrossDevice` now restores the prior `.bak` when the EXDEV fallback copy fails, covering the previously untested failure leg
- `backupCrossDevice` cleans up the staged `backupNew` on copy failure so a stranded `.bak.new` no longer survives a failed fallback
- `BackupToDotBak` sweeps stale `.bak.tmp-*` siblings at entry, reconciling artifacts from an earlier interrupted run
- `swapStagedBackup` and `restorePriorBackup` now log and surface rename failures instead of swallowing them silently
- `BackupToDotBak`'s staging branch now sets `staged` explicitly for non-regular, non-directory entries, eliminating the use-uninitialized path
- `backupCrossDevice` documents the accepted TOCTOU window between `Lstat` and the subsequent `Rename` given the single-user trust model
- `backupCrossDevice`'s postcondition comment no longer claims the cross-device fallback is atomic; documents the duplicated-tree window
- Vacate failure in `backupCrossDevice` now reports that the durable backup completed and names the preserved backup path

## [4.7.1] - 2026-06-20

### Fixed

- `--force` re-run backups are now crash-safe at both backup sites: a failed or interrupted swap (cross-filesystem `EXDEV`, disk-full, or `SIGKILL`) no longer destroys the prior `.bak` generation.
- `backupExisting` (review-dir move path) stages the prior `.bak` aside before swapping, restores it on failure, and falls back to copy-then-vacate on cross-filesystem `EXDEV`.
- `BackupToDotBak` (copy path) stages the prior generation to `.bak.old` and restores it if the staged-temp swap fails, closing the rename-window backup loss the prior implementation left open.
- Stale atcr-owned staging artifacts (`.bak.old`/`.bak.new`) from an interrupted run are reconciled away on the next `--force`, preserving the one-generation contract across a crash-then-retry sequence.

*Shipped via /execute-epic (epic 4.7.1)*

## [Technical Debt] - 2026-06-19

### Fixed

- Floored negative `initialBackoff` in `WithRetryOverride` to prevent fire-immediately behavior; added first-sleep clamp test
- Extracted `validateRetryBounds` helper to unify retry validation across config/agent/global paths with consistent error format
- Added per-agent `max_retries`/`initial_backoff_ms` to the invoke debug log for rate-limit diagnostics
- Documented the three-state retry-override sentinel contract in the Agent struct comment
- Added test coverage for MCP handler error paths (handleVerify, parseOptionalSeverity, rangeError, loadVerifyRegistry, handleReport, handleStatus, registerTool); raised `internal/mcp` coverage to 91.6%
- `BackupToDotBak` now skips symlink sources via `os.Lstat` instead of following them
- `copyFile` uses streaming `io.Copy` instead of unbounded in-memory reads
- Added `tmp.Sync()` and parent-directory fsync in `WriteFileAtomic` for crash durability
- `PrepareReview` now enforces the system-path reject (`/etc`, `/proc`, `/sys`) for all callers, not only the CLI
- `--force` with a derived id emits a stderr notice instead of silently no-oping
- `ScaffoldReviewDir`/`ScaffoldOutputDir` collision errors now name the id only, not the resolved filesystem path
- `forceBackupReviewDir`/`forceBackupOutputDir` return and emit the backup path on `--force`
- `guardForeignBackup` returns a specific error when the backup path is a regular file
- `guardForeignBackup`/`looksLikeReviewTree` now require a `manifest.json` provenance marker before treating a directory as an atcr backup
- `WriteVerification` documented as test-only seam; `BackupExistingVerification` unexported
- Scoped verify backup to `verification.json`-only; removed false `reconciled.bak/` coverage claim
- Moved `reconciled.bak` snapshot to just before `Emit`, after adjudication validation, so a failed validation no longer leaves a misleading backup
- Accepted 79.5% coverage for `internal/atomicfs` defensive-error branches as a documented exception

## [4.7.0] - 2026-06-19

Idempotency and safe retry: retrying a failed review no longer silently duplicates or overwrites artifacts. Existing review/reconcile/verify outputs are backed up before being overwritten, and atomic writes prevent partial writes from corrupting existing files.

### Added

- `--force` flag on `atcr review` that backs up an existing review directory to `<dir>.bak` and scaffolds a fresh one.
- `atomicfs.WriteJSON`, an atomic JSON writer (marshal + write-to-temp + rename) wrapping the existing `WriteFileAtomic`.

### Changed

- The review-directory-already-exists error now names both recovery paths: `--resume` to continue the prior run or `--force` to overwrite. Passing both `--resume` and `--force` is a usage error.
- `atcr reconcile` backs up an existing `reconciled/` directory to `reconciled.bak/` before re-emitting.
- `atcr verify` backs up an existing `verification.json` to `verification.json.bak` before re-writing.

### Fixed

- Atomic writes prevent a partial or aborted write from corrupting an existing file.
- `--force` on an arbitrary `--output-dir` refuses to clobber a foreign sibling `<dir>.bak` it did not create, instead of silently deleting it.

*Shipped via /execute-epic (epic 4.7)*

## [4.6.0] - 2026-06-19

Robust rate-limit & backoff handling: exposed the existing LLM retry engine (exponential backoff with jitter + `Retry-After` honoring) through configuration and raised the default retry budget so transient 429/5xx rate-limits are absorbed before a review chunk fails.

### Added

- Configurable `max_retries` (0–10) and `initial_backoff_ms` (1–30000) retry tunables at the registry (global) tier and per-agent tier, resolving over the shared-settings precedence chain like `timeout_secs`. Per-agent overrides take effect per call without rebuilding the shared client.

### Changed

- Raised the default LLM retry budget for the review/resume fan-out from 3 attempts to 6 (`max_retries` 5) so sustained rate-limiting is retried with backoff before a chunk is marked failed. A 429 is still owned by the backoff path and never trips the Epic 4.5 circuit breaker.

### Fixed

- Clamped the first retry backoff sleep to the 30s ceiling (previously only subsequent sleeps were capped).

*Shipped via /execute-epic (epic 4.6)*

## [Technical Debt] - 2026-06-19

### Fixed

- Guarded `gauge.Set` against NaN/Inf inputs to match `histogram.Observe`; updated `Registry.Reset` doc comment to include gauges
- Added debug warning when a tools-enabled production agent has an empty provider, making silent circuit breaker bypasses observable
- Documented in `classifyStatus` that `CircuitOpenError` deliberately classifies as `StatusFailed` so the fan-out chain advances to the fallback (AC6)
- Extracted `writeFamily` helper in `prometheus.go` to eliminate three near-identical family-grouping blocks
- Documented 3xx responses as health-neutral in the circuit breaker `send` switch
- Clamped non-positive `threshold` and `cooldown` values in `New()` to defaults so a zero-config breaker cannot trip on the first failure
- Documented the max probe slot-hold bound (= caller HTTP timeout) in `Allow`; corrected `ReleaseProbe` comment to accurately state it always clears `probeInFlight`
- Emitted structured log (`slog.Info`) on every circuit breaker state transition with provider name and new state
- Cached gauge pointer in `Breaker` at construction time to remove cross-package lock acquisition under `b.mu` on each state transition
- `Registry.Get("")` now returns a fresh throwaway always-closed breaker instead of caching a real stateful one under the empty key
- `isBreakerFailure` now distinguishes caller-initiated deadline expiry from a provider stall, preventing false breaker failures when the caller's own budget expires against a healthy provider
- Extended error snippet redaction to cover foreign fleet key prefixes (`AIza`, `gsk_`, `xai-`) in `HTTPStatusError.Snippet`
- Documented the deliberate ~8KB error-body drain bound as an accepted trade-off in `readErrorSnippet`
- Added deferred probe slot release in `send()` so a panic in `dispatch()` cannot wedge the circuit breaker half-open permanently

## [4.5.0] - 2026-06-19

### Added

- Per-provider circuit breaker: after 3 consecutive provider failures (5xx, timeouts, or connection-level transport errors) a provider's circuit opens and subsequent agents fail fast without wasted API calls, then recovers automatically via a 60-second half-open probe
- `CircuitOpenError` makes the fan-out engine treat an open circuit as a permanent failure and fall straight through to an agent's fallback chain
- `atcr_circuit_breaker_state{provider}` gauge (0 closed / 1 open / 2 half-open), documented in `docs/metrics.md`; adds a settable gauge primitive to the in-process metrics collector

### Changed

- 4xx provider responses — including 429 rate-limits and 401 auth errors — never trip the breaker; only outages do, and `atcr_api_calls_total` no longer counts a fail-fast circuit-open as a provider round-trip

*Shipped via /execute-epic (epic 4.5)*

## [Technical Debt] - 2026-06-19

### Fixed

- The end-of-review summary now prints on the all-agents-failed and `--resume` paths, not only on a fresh successful review
- `Agents X/Y` summary line now sources its denominator from the per-attempt `agents_total` registry so numerator and denominator share one granularity; counts are taken from a per-review registry diff rather than the process-global registry
- `WritePrometheus` snapshots registry pointers and renders lock-free, sorting each histogram once instead of per-quantile
- `escapeLabelValue` now escapes carriage returns, closing the Prometheus exposition-injection gap its doc comment claimed
- Unknown/empty finding severities are clamped to an `UNKNOWN` bucket instead of adding unbounded label cardinality
- `histogram.Percentile` caches its sorted snapshot and invalidates on each `Observe`, avoiding a full re-sort per call
- `histogram.Observe` drops NaN/Inf inputs so a single bad sample can no longer poison the sum or sort order
- API call counting no longer inflates for context-cancelled single-shot agents, and negative `Turns` / non-positive HTTP status codes can no longer corrupt the monotonic counters
- Recovered agent panics now record a terminal outcome and duration, preserving the `reviews_total == succeeded+failed+interrupted` invariant
- Review-duration output relabeled `Total elapsed` to reflect that it is wall-clock, not the fan-out window the histogram measures
- Ring-buffer full check uses `>=` and percentile ranking uses `math.Ceil` for clarity and boundary correctness
- Added `docs/metrics.md` cataloging counters/histograms, the CLI summary, and the `atcr_metrics` tool (local-only), linked from the README; documented the no-schema `atcr_metrics` registration and its accepted local-only security posture

## [4.4.0] - 2026-06-19

In-process metrics and observability: a new `internal/metrics` package collects counters and histograms with no external dependencies, the fan-out engine and review flow are instrumented, `atcr review` prints an end-of-review summary, and the MCP server exposes metrics in Prometheus text format.

### Added

- New `internal/metrics` package: monotonic counters and bounded histograms (10k-sample window) in a registry with a process-wide `DefaultRegistry`, plus Prometheus text exposition with label-value escaping
- Fan-out engine records agent invocation counts, per-agent latency, API calls (one per provider round-trip), API errors by HTTP status, and tool-call totals
- Review flow records review counts, total duration, and findings (total and by severity)
- `atcr review` prints an end-of-review summary: duration, agent success/failure/timeout counts, API calls, and findings by severity
- New `atcr_metrics` MCP tool returns the in-process metrics in Prometheus text format (cumulative since server start; local-only — do not expose the server publicly)

*Shipped via /execute-epic (epic 4.4)*

## [Technical Debt] - 2026-06-19

### Fixed

- `validation.FilePath` now rejects Windows volume/system paths (`C:\Windows`, `C:\Program Files`) in addition to Unix system dirs
- `report --output` resolves symlinks before validation and threads the validated absolute path to the write call, closing the symlink bypass
- Extended FilePath deny list to include macOS `/private/etc` and `/private/var` (symlink targets of `/etc` and `/var`) to prevent bypass on macOS
- `FilePath` traversal check is now segment-aware: rejects `/../` components but accepts legitimate `..`-in-filename paths (e.g. `my..file`)
- `GitRef` tightened to reject all control characters, shell/git metacharacters (`\ ? * [`), and a leading `-`, matching the doc comment guarantee
- Hoisted `Severity` valid-set map to package-level var to avoid a map allocation on every call

## [4.3.0] - 2026-06-18

Centralized input validation: a new `internal/validation` package gives the CLI consistent, field-aware validation errors, and path inputs are now rejected at the input layer before any work begins.

### Added

- New `internal/validation` package with validators for git refs, file paths, review IDs, severity levels, and enums; each returns a field-aware `ValidationError` that wraps as a usage error (exit 2)
- `atcr review --output-dir` and `atcr report --output` now reject paths under system directories (`/etc`, `/proc`, `/sys`) via the input-validation layer before execution, instead of failing late on the filesystem

*Shipped via /execute-epic (epic 4.3)*

## [Technical Debt] - 2026-06-18

### Fixed

- Added structured Warn log when MCP server shutdown interrupts an in-flight detached review, so serve-mode interruptions are now observable in logs (`internal/mcp/handlers.go:231`)
- Added Warn log in `shutdownReviews` before cancelling in-flight reviews, making server-shutdown events diagnosable from stderr alone (`internal/mcp/server.go:48`)
- Documented that `withShutdownCancel`'s returned cancel is safe to call twice — the second call via AfterFunc is a deliberate idempotent no-op (`internal/mcp/handlers.go:120`)
- Added assertion that config-validation failure exits before creating any review output directory, guarding the AC7 fail-fast guarantee against silent regression (`cmd/atcr/review_test.go:386`)
- Documented that the staged `validate → ValidateFallbacks` call order in `validateMerged` and `LoadRegistry` is intentional (`internal/registry/overlay.go:224`)

## [Technical Debt] - 2026-06-18

### Fixed

- Removed redundant `strings.TrimSpace` call from payload error string formatting (`internal/registry/config.go:248`)
- Replaced unidiomatic type assertion with `errors.As` for multi-error unwrapping in attribution (`internal/registry/attribution.go:48`)
- Fixed `validateProvider` silently appending an empty error slice for valid providers (`internal/registry/config.go:233`)
- Removed stale "Nothing to do here" comment from `walkFallbacks` after lead-in node blackening was added (`internal/registry/graph.go:67`)

## [4.2.0] - 2026-06-18

Configuration validation now reports every error at once, so a bad `registry.yaml` is fixed in a single pass instead of one error per run.

### Changed

- Registry validation (required fields, enum values, numeric ranges, review-constraint guardrails, and fallback-chain dangling/cycle checks) now accumulates and reports all faults together via `errors.Join` instead of stopping at the first; output is deterministic (providers and agents are validated in sorted order), and in a merged user+project config each fault still names the file that defined the offending entry

### Fixed

- A fallback graph with a lead-in chain feeding a cycle (e.g. `a→b`, `b↔c`, `d→a`) no longer panics `atcr` at config-load time; the cycle is reported as a clean validation error

*Shipped via /execute-epic (epic 4.2)*

## [Technical Debt] - 2026-06-18

### Fixed

- Added structured Warn log when MCP server shutdown interrupts an in-flight detached review (`review interrupted by server shutdown`) and when `shutdownReviews` begins cancelling in-flight reviews
- Documented that `withShutdownCancel`'s cancel function may run twice concurrently via AfterFunc — the second call is an intentional idempotent no-op
- Fixed `blockingCompleter` to return `context.Canceled` directly instead of `ctx.Err()` string; removed a timing-sensitive `RunInProgress` assertion in the disconnect shutdown test
- Documented that `shutdownCtx` is immutable after construction, clarifying concurrent access safety at `handlers.go:230`
- Added a Serve-level shutdown integration test exercising the `ctx.Err() != nil` discriminator through a transport seam, preventing regressions from boolean inversions or call-order changes
- Documented that `shutdownDrain` must comfortably exceed worst-case interrupt-flush latency to guarantee the on-disk interrupted status persists before process exit

## [4.1.2] - 2026-06-18

Serve-mode detached reviews are now marked `interrupted` on server shutdown, matching CLI semantics.

### Fixed

- A background MCP review still in flight when the server shuts down (SIGINT/SIGTERM) is now cancelled like the CLI's interrupt path and recorded `interrupted` on disk, instead of being left `in_progress` with no signal — a clean client disconnect still lets a near-complete review finish and be recorded `completed`

*Shipped via /execute-epic (epic 4.1.2)*

## [Technical Debt] - 2026-06-18

### Fixed

- `agentStatusName` now emits a structured Warn (with path) on a corrupt or unreadable `status.json` instead of silently re-running the agent (re-spending tokens) with no operator signal
- `RebuildPool` hard-fails on a completed agent's unparseable findings instead of silently dropping them; merges per-agent findings in roster order so a resumed pool matches an equivalent fresh run; rejects roster names that collapse to the same sanitized dirname; bounds per-agent `findings.txt` reads with a size limit; and writes a best-effort `Interrupted` marker when the final manifest write fails on an interrupted resume
- Rejected `status.json` symlinks that escape the review tree during the agent scan
- Recompute the Review tool-stage from the union of resumed statuses instead of preserving the original run's stage verbatim
- `writeResumedAgents` preserves a prior failed status (and its error) for agents the resumed engine never ran
- A signal-interrupted resume now emits a structured, `review_id`-correlated Warn, matching the fresh review path for log greppability
- Guarded a nil result from `gitrange.Resolve`; mapped `ErrEmptyRoster` to usage error (exit 2); rejected `--fresh`/`--thorough`/`--min-severity` when combined with `--resume`; and early-returned for empty pending slots
- Deduplicated the redaction/interrupt-reporting and review-stage-classifier logic shared by `runReview`/`runResume` and the fresh/resume manifest paths
- Test hygiene: `require.NoError` in fixtures, a checked `writeResumedAgents` error return for errcheck, a context timeout on `execResume`, and a `testReviewKeyEnv` constant

## [4.1.1] - 2026-06-18

Resume support: finish an interrupted or partially-failed review without re-spending tokens on the agents that already completed.

### Added

- `atcr review --resume <latest|id|path>` re-runs only the pending/failed agents of an existing review into the same directory, then reconciles — completed agents (those whose per-agent `status.json` records `ok`, including clean reviewers that found nothing) are skipped, so their tokens are never re-spent
- Resume locks the panel: it re-resolves the current git range and compares it (plus the configured roster) against the interrupted run's `manifest.json`, aborting with exit code 2 when the range or roster changed, so a resume can never mix inconsistent results or silently run a different panel

### Changed

- The interrupt notice now points at `atcr review --resume <id>` to finish the remaining agents, alongside `atcr status <id>` to inspect

*Shipped via /execute-epic (epic 4.1.1)*

## [4.1.0] - 2026-06-17

Graceful shutdown and signal handling: Ctrl-C during a review no longer loses completed work.

### Added

- SIGINT/SIGTERM handling: an interrupt cancels the root context so the reviewer fan-out drains cooperatively — no new agents start, and the results of agents that already finished are preserved on disk
- New `interrupted` review state reported by `atcr review`, `atcr status`, and the MCP `atcr_status` tool, distinguishing a signal-cancelled run from a clean completion, an all-agents failure, or a timeout
- A 10-second grace period after the first interrupt, after which the process force-exits with code 1 to guard against a hang

### Changed

- A signal-interrupted `atcr review` now prints how many agents completed and where the partial results were saved (`atcr status <id>` to inspect), then exits 1 — instead of dying immediately and discarding the partial run

*Shipped via /execute-epic (epic 4.1)*

## [Technical Debt] - 2026-06-17

### Fixed

- Anchored `sk-`/`Bearer` redaction patterns to token charsets to prevent over-redacting adjacent JSON fields; extended coverage to URL-encoded (`Bearer%20`), base64, and path-escaped secret forms
- Added ASCII case-fold prefilter and precomputed per-secret encodings for a zero-alloc no-match fast path in `Redact`; benchmarks document the sub-millisecond per-record target
- Redacted secrets in non-error `KindAny` attributes; non-secret values preserve native slog rendering
- Made `skKeyPattern` case-insensitive in `llmclient.Client` to match the log-package behavior
- Re-applied the no-redirect guard onto `WithHTTPClient`-injected clients to prevent `Authorization` header leaks on redirects
- Scrubbed secret-shaped tokens at root-logger construction so MCP base-logger lines are covered before any per-review redactor layer (AC5)
- Exempted `review_id`/`agent_name` correlation keys from value redaction to resolve AC9 vs AC5 tension
- Resolved symlinks in `resolveRedactRoot` so macOS real-form paths (`/private/var/...`) relativize correctly under the review root (AC6)
- Emitted a warn-level log line when `filepath.Abs` fails on the redact root so silent loss of path relativization is observable
- Based scorecard store paths in error messages to avoid absolute `~/.config/atcr` path disclosure
- Routed all `ExecuteReview` and `verify/pipeline.go` stderr warnings through the context logger, enforcing single-sink discipline
- Validated nil writer in `log.New` to surface misconfiguration at construction rather than first write
- Wrapped `LevelFromString`/`New` errors with exported `ErrInvalidLevel`/`ErrInvalidFormat` sentinels so callers can branch programmatically
- Bounded echoed `LOG_LEVEL` and `--log-format` strings in error messages to prevent unbounded user-input reflection
- Fixed `ClassifiedError.Error` nil-`Err` diagnostic to produce unmistakably diagnostic output instead of a bare classification label
- Clamped negative `maxRetries` to zero in `WithRetry` to prevent a nil-wrapped exhausted-retries error on zero-attempt loops
- Honored `Retry-After` header in `client.go` retry backoff on 429/503 responses; falls back to fixed exponential otherwise
- Clamped retry backoff to `maxBackoff` and added bounded jitter to prevent thundering-herd on concurrent rate-limit responses
- Bounded error-body drain in `readErrorSnippet` with `io.CopyN` to prevent unbounded reads on the error path
- Returned empty completion as a classified permanent error in `CompleteWithUsage` so callers fail loudly instead of propagating silent empty content
- Clamped oversized token counts to `math.MaxInt` instead of zero in `clampNonNegative` to avoid misreporting large valid counts as free
- Returned the shared `log.Discard()` singleton from per-call logger fallbacks, eliminating a handler allocation per agent invocation
- Renamed test `sentinel` to `errSentinel` (ST1012) and fixed SA4000 discard-cache identity assertion to clear lint gate failures
- Documented `PersistentPreRunE` bypass paths rely on the discard-logger fallback; scoped redaction claim to values and documented static-key invariant in `handler.go`

*Shipped via /resolve-td + /finalize-td*

## [Technical Debt] - 2026-06-16

### Fixed

- Removed the redundant third argument from `require.Equal` assertions in the scorecard and leaderboard wiring tests, eliminating suppressed failure messages
- Added `require.NoError` for the `f.WriteString` call in `seedMalformedStore` so test-setup failures surface immediately instead of masking downstream assertions
- Fixed `seedMalformedStore` to locate the month file via glob rather than duplicating `monthFromRunID`'s stem logic, preventing silent desync if month derivation changes
- Extracted a `logger()` nil-guard helper in `handlers.go` and replaced direct `e.log` field accesses throughout, preventing a nil-pointer panic when the engine carries no logger
- Exported `MsgMalformedSkip` and `MsgWriteFailed` constants from `internal/scorecard`; updated producers and all regression tests to reference them so a single diagnostic reword keeps every assertion green
- Softened write-failure test comments to describe the failure mechanism generically rather than citing the POSIX-specific `ENOTDIR` errno; documented that `engine.diagWriter`'s typed-nil guard is intentionally delegated to `scorecard.diagWriter`

*Shipped via /resolve-td + /finalize-td*

## [3.6.0] - 2026-06-16

### Added

- Regression tests that lock the scorecard diagnostics writer-wiring at the real call sites: the `leaderboard`, `scorecard`, and `reconcile` CLI commands route store diagnostics to `cmd.ErrOrStderr()`, and the MCP `reconcile` handler routes them to the engine's injected sink — a future refactor that drops the wired writer now fails a test

### Changed

- The MCP engine sources scorecard diagnostics from an injectable writer (defaulting to `os.Stderr`), so the diagnostics wiring is assertable in tests without changing serve-mode behavior

*Shipped via /execute-epic (epic 3.6)*

## [Technical Debt] - 2026-06-16

### Fixed

- Routed the registry's severity normalizer through the canonical `stream.NormalizeSeverity`, removing the fourth duplicate upper-and-trim copy so every package shares one definition
- Made each reconcile severity-rank lookup self-defending by normalizing at the lookup site (`sortMerged`, `AtOrAbove`, `spreadFromDisagreement`, `soloItem`, `severitySplitItem`, `verificationItem`, `grayZoneItem`, and the disagreement sort tiebreak), so a mixed-case or non-canonical severity can no longer score rank 0 and sort or gate incorrectly
- Normalized the `writeSummaryGrid` bucket key in report rendering so a mixed-case severity lands in its canonical bucket instead of the OTHER row
- Precomputed per-finding severity ranks before the report sort comparator, removing a per-comparison string allocation in the render path
- Copied the shared `SeverityRank` map locally and normalized the all-unknown merge fallback so merged severity casing stays consistent
- Added whitespace-only input coverage to the `NormalizeSeverity` test, plus package docs and formatting cleanups (final newlines, trailing commas, blank-line removals) across the severity-consumer files

*Shipped via /resolve-td + /finalize-td*

## [3.5.0] - 2026-06-16

### Changed

- Consolidated the severity-rank rubric (`{CRITICAL, HIGH, MEDIUM, LOW}`) and the `NormalizeSeverity` helper into a single canonical owner (`internal/stream`); reconcile, fan-out post-processing, verify, and report now consume that one definition instead of independently redefined rank maps and normalizers

### Fixed

- Fixed a severity casing asymmetry where reconcile compared raw severity values while fan-out normalized them, so a non-canonical or mixed-case severity could desync fan-out truncation from reconcile merging; all consumers now normalize identically before ranking

*Shipped via /execute-epic (epic 3.5)*

## [Technical Debt] - 2026-06-16

### Fixed

- Guarded the scorecard diagnostics writer against a typed-nil `io.Writer` (a non-nil interface wrapping a nil pointer), so such a value falls back to `os.Stderr` instead of panicking on first write
- Resolved the diagnostics writer once at the top of `FindByRunID` and reused it for the inner reads and the adjacent-month warning, instead of re-resolving it on each path
- Added cross-referencing doc notes between `ReadOpts.Writer` and `EmitOpts.Diag` marking the divergent field names as intentional, and documented their concurrency contract and local/trusted-sink assumption
- Corrected a stale `EmitForReconcile` doc comment to reflect the MCP handler passing `EmitOpts{Diag: os.Stderr}`
- Added the transient `.planning/.active_sprint` session marker to `.gitignore` so it can no longer be committed as a process artifact

*Shipped via /resolve-td + /finalize-td*

## [3.4.0] - 2026-06-16

### Changed

- Scorecard read and emit operational diagnostics now write to a caller-injectable writer (`ReadOpts.Writer` / `EmitOpts.Diag`, defaulting to `os.Stderr`) instead of the process-global `os.Stderr`; the CLI reconcile and leaderboard paths route them through `cmd.ErrOrStderr()` and the MCP reconcile path supplies `os.Stderr`, so diagnostics are now redirectable and assertable in tests

*Shipped via /execute-epic (epic 3.4)*

## [3.3.0] - 2026-06-16

### Added

- Per-run scorecard warns on orphan verification verdicts to surface potentially missed reviews

### Fixed

- Surfaced ignored fallback review constraints and enforced primary-wins precedence in review fanout
- Union all reviewers per finding key in verdict tallies, correctly crediting all reviewers of duplicate finding locations
- Explicit field assignment for `ExportFilters` to harden against `FilterOpts` field reorder

## [Technical Debt] - 2026-06-15

### Fixed

- Added bounds guard in `winningAttribution` loop to prevent out-of-range panic when `perSkeptic` is longer than `skeptics` or `perTripped` (`internal/verify/pipeline.go`)
- Sanitized newlines in `logSkepticFailure` detail field to prevent log-injection of forged `atcr: verify:` lines (`internal/verify/invoke.go`)
- Initialized `TrippedBudgets` to empty slice (not nil) at three locations in the verify pipeline so JSON serialization emits `[]` rather than `null`
- Corrected `runVerify` docstring: replaced false SERIALLY / no-concurrency-knob / tracked-as-TD-009 claims with an accurate description of the bounded worker pool (`internal/verify/pipeline.go`)
- Rejected registry scope entries containing control characters to prevent path-injection via crafted agent configs (`internal/registry/config.go`)
- Logged the discarded `ReadAmbiguousClusters` error to stderr in `LoadDisagreements` instead of silently dropping it (`internal/reconcile/disagree.go`)
- Removed redundant `base.Model = ""` assignments in `verifyFinding` (`internal/verify/pipeline.go`)
- Exported `reconcile.SeverityRank` and updated `internal/report` to reference the canonical ranking function, eliminating the duplicate rank table
- Fixed nil-`Verification` panic in `verificationItem` when a finding has no verification block (`internal/reconcile/disagree.go`)
- Fixed `scoreFor` integer overflow for out-of-bounds severity indices (`internal/reconcile/disagree.go`)
- Validated `SchemaVersion` major version in `ReadDisagreements` to reject incompatible schema versions with a clear error
- Eliminated double `BuildDisagreements` call in `Emit` by extracting a `renderMarkdown` helper (`internal/reconcile/disagree.go`)

## [Technical Debt] - 2026-06-14

### Fixed

- Fixed gray-zone member exclusion to key on file+line rather than problem text, eliminating rare double-surface when a merged cluster's problem string diverges after merge (`internal/reconcile/disagree.go:108`)
- Gray-zone clusters with all-unknown/blank severities now score above zero and always surface in the disagreement radar (`internal/reconcile/disagree.go:316`)
- `atcr report --disagreements --format json` now emits the disagreements file as JSON; unsupported `--format` combinations return a usage error instead of silently emitting markdown (`cmd/atcr/report.go:53`)
- Prior `verification.json` is now loaded lazily, eliminating spurious "metadata not carried forward" warnings when no findings are skipped in the current run (`internal/verify/pipeline.go:225`)
- `parseVerdict` now iterates candidate JSON objects to skip decoy braces, preventing prose-embedded `{}` from silently degrading a confirmed verdict to unverifiable (`internal/verify/verdict.go:81`)
- `aggregateVerdicts` now restricts the `Skeptic` field to winner names in clear-majority runs, so a dissenter's name no longer appears without corresponding reasoning (`internal/verify/votes.go:67`)
- `registryPath` now uses canonical resolved-path containment instead of substring matching, closing a balanced `a/..` traversal bypass (`internal/mcp/handlers.go:434`)
- `dropped_by_min_severity` and `truncated_by_max_findings` counters persisted in per-agent `status.json`, making post-processing volume reductions observable after the run (`internal/fanout/postprocess.go`)
- Extracted `reconcile.LoadDisagreements` shared helper, eliminating radar-build duplication between `cmd/atcr/report.go` and `internal/mcp/handlers.go`
- Replaced hand-rolled `itoa` helper with `strconv.Itoa` in `verify_test.go`
- Documented intentional `escTrunc` truncation policy in `writeRadarItems`

## [3.2.0] - 2026-06-14

### Added

- Disagreement radar: surfaces the highest-tension spots in a change — severity splits, solo findings, gray-zone clusters, and verification disagreements — ranked by severity spread × reviewer independence
- `atcr report --disagreements` focused view showing ranked tension spots with model positions side by side
- "Disagreements" section in the standard `report.md`, above the consensus findings and omitted when there are none
- `reconciled/disagreements.json` — a versioned cross-exam handoff artifact for downstream consumption

### Changed

- The markdown report (`atcr report`, `reconciled/report.md`, and the MCP report tool) now carries the disagreement radar above its findings; output is byte-identical for reviews with no disagreements

*Shipped via /execute-epic (epic 3.2)*

## [3.1.0] - 2026-06-14

### Fixed

- `verification.json` now records the winning skeptic's model in multi-vote runs instead of always the first skeptic's, and names every participant on a tie
- Tripped budgets are captured in the structured `trippedBudgets` field instead of only being buried in free-text notes
- Findings skipped on a re-run (already verified) retain their original `model`/`durationMs`/`trippedBudgets` rather than losing them, and no longer inherit metadata from a prior record whose verdict no longer matches
- `model` is left empty on the no-eligible-skeptic and tool-harness-unavailable paths, attributing a model only to skeptics that actually ran

*Shipped via /execute-epic (epic 3.1)*

## [3.0.0] - 2026-06-14

### Added

- `atcr verify` command with MCP tool, skeptic invocation pipeline, verdict parsing, vote aggregation, and `--require-verified` review gate
- Report v2 rendering with skeptic/refuted sections and `verification.md` output
- Confidence v2 scoring with artifact emission and gate counter updates
- `AgentsByRole` and `SelectEligibleSkeptics` for adversarial agent selection

### Fixed

- Mitigated prompt injection in finding fields via XML delimiters and randomized per-call sentinel tags
- Prevented triple-backtick injection using adaptive fence length in findings output
- Excluded skeptics with undefined provider at selection time
- Recorded all participating skeptic models in `verifyFinding`
- Surfaced `Truncated` flag on `ChatResponse` when `finish_reason=length`
- Corrected `FindingsProcessed` count to exclude no-eligible-skeptic findings
- Normalized verdict case/whitespace before enum switch in `parseVerdict`
- Resolved review gate ignoring `fail_on` from project config
- Fixed `IsFailing` threshold normalization and severity sorting in report grid
- Added `OTHER` buckets for non-canonical severity and confidence values in summary grid
- Preserved unknown fields during `manifest.json` round-trip in `UpdateManifestStage`
- Preserved large integer precision in `UpdateSummaryVerdicts` using `json.Decoder.UseNumber`

*Shipped via /execute-sprint (sprint 3.0_adversarial_verification)*

## [2.2.0] - 2026-06-13

### Added

- Per-agent review guardrails on `AgentConfig` in `~/.config/atcr/registry.yaml`, all optional and backward-compatible: `scope` (a list of categories injected into the persona prompt as a soft "Review Focus" hint — it steers a reviewer without dropping out-of-category findings), `min_severity` (a hard floor — findings below it are dropped from the agent's `findings.txt` before reconciliation), and `max_findings` (a hard cap — the agent's findings are truncated to the N most severe, so a flood of `LOW` items can never bury a `HIGH` one).
- `min_severity` and `max_findings` are enforced deterministically in the fan-out per-source path, right after the engine stamps the `REVIEWER` column from the registry agent key, with dropped/truncated counts logged to stderr. The reconciler stays source-agnostic. A fallback agent inherits its primary's constraints (the constraint follows the slot, like the persona prompt). Reviewer-identity stamping was already model-proof, so no change was needed there.

*Shipped via /execute-epic (epic 2.2)*

## [2.1.0] - 2026-06-13

### Added

- The `manifest.json` review stage now records the filesystem snapshot the tool-using reviewers ran against: `snapshot_mode` (`"live"` when head matched a clean HEAD on the fast path, `"worktree"` when a detached git worktree was created), `head_sha` (the resolved head the snapshot was taken at), and `snapshot_worktree_path` (the temporary worktree path, or `""` in live mode). These are backward-compatible additions present only when a review runs tool-enabled agents, so 1.x manifests and pure single-shot rosters are unchanged.

*Shipped via /execute-epic (epic 2.1)*

## [2.0.0] - 2026-06-13

### Fixed

- Normalized zero `Limits` fields to `DefaultLimits` in `NewDispatcher` to prevent silent unlimited-cap behavior
- Joined first and retry worktree-add errors via `errors.Join` so both causes are surfaced in the error message
- Used `filepath.EvalSymlinks` in `snapshotCleanupGuard` to handle symlinked TMPDIR on macOS/Windows
- Logged warning on malformed turn field in replay instead of silently discarding the decode error
- Used case-insensitive comparison for `.git` directory skip in grep and `list_files`
- Replaced single-turn oscillation guard with a 3-turn ring buffer to catch ABAB signal oscillation; `loop_hygiene` now recorded in `TrippedBudgets` on hygiene halt
- Fixed `OriginalBytes` to store byte count (not match count) in truncated grep results
- Wrapped `JailError` in `ToolError` so the `Execute` contract is maintained

*Shipped via /execute-sprint (sprint 2.0_tool_using_reviewers)*

## [1.9.0] - 2026-06-13

### Fixed

- A `WritePool` I/O fault that aborted persistence after one or more reviewer agents had already run could leave a `summary.json` with `partial: false` even though only a subset of per-agent artifacts reached disk. A later `atcr reconcile` (CLI or MCP) over that review could then walk the surviving artifacts and emit a non-partial verdict that silently dropped the unflushed agent. `PoolSummary` now carries a `failure_marker` field set only by the best-effort failure summary, and the shared partial-flag reader forces `partial: true` whenever the marker is present and at least one agent succeeded — so reconcile over a write-aborted review is always treated as partial. The `reconcile` package is unchanged; the correction lands entirely in the caller-side reader.

*Shipped via /execute-epic (epic 1.9)*

## [1.8.0] - 2026-06-12

### Added

- `atcr review --output-dir <path>` writes the full review tree (`manifest.json`, `payload/`, `sources/`, `reconciled/`) to an explicit path instead of `.atcr/reviews/<id>/`, so external orchestrators (skills, CI pipelines, wrapper scripts) can direct output to a location they control with no post-run file moves. A relative path resolves against the current directory; the target must be new or empty (a non-empty directory is rejected with exit 2 so existing content is never clobbered); `--output-dir` and `--id` are mutually exclusive. `atcr reconcile` and `atcr report` operate on the same path via their existing `[id-or-path]` argument — no new flag needed.

### Changed

- `--output-dir` runs do not update the `.atcr/latest` pointer, which continues to track interactive runs under `.atcr/reviews/` only.

*Shipped via /execute-epic (epic 1.8)*

## [1.7.0] - 2026-06-12

### Verified

- Closed the three Story-05 manual verification gates with one authorized live-provider review run: the orchestration loop (range → review → poll → host review → reconcile → report) verified end-to-end (AC 05-03), the host review's adversarial praise-free tone confirmed by independent inspection (AC 05-04), and ambiguity adjudication's merge/distinct sensibility and idempotency exercised through real `atcr reconcile` (AC 05-04). No code behavior changed.

### Notes

- The live run surfaced that the default 512 KiB payload byte budget yields ~155k-token payloads that exceed common model context/plan limits, and `atcr doctor`'s trivial-nonce probe does not catch it — captured as technical debt for a sizing-guideline/warning follow-up.

*Shipped via /execute-epic (epic 1.7)*

## [1.6.0] - 2026-06-12

### Changed

- Blocks and files payload builds now issue a constant number of git processes per review range instead of 4–5 per changed file: change classification is batched into one `--name-status`/`--numstat` pass, and each diff variant (`--function-context`, `--unified=10`, `--unified=0`, plain `-M`) is run once over the whole range and split per file on `diff --git` boundaries. Payload output is byte-identical to before — verbatim bodies, changed-region sentinels, rename pairing, and binary/deleted markers are unchanged

### Fixed

- The diff splitter keys each file chunk against the known changed-file list using only the `diff --git` header, so file content that mimics a diff header (a line rendering as `+++ b/...`) or a path containing spaces can no longer mis-attribute a chunk or silently empty a file's body; a chunk that matches no changed file is now logged rather than dropped

*Shipped via /execute-epic (epic 1.6)*

## [1.5.0] - 2026-06-12

### Added

- `stale` review status: `atcr status` and the `atcr_status` MCP tool now report a distinct, inferred terminal state for a review whose fan-out died — `summary.json` never appeared and the manifest's `started_at + timeout_secs` (plus a 60s grace margin) has elapsed. The Skill poll loop treats `stale` as terminal, so a dead review no longer reads `in_progress` forever and the orchestration loop can tell a running review from an orphaned one

### Changed

- `ReadReviewStatus` infers `stale` from the effective timeout persisted in `manifest.json`; manifests written without `timeout_secs` (zero value) have no inferable deadline and keep reporting `in_progress`, so the change is backward compatible. The `ReviewStatus` JSON shape is unchanged apart from the new `status` value — the MCP `StatusResult` alias and `atcr status` output stay compatible
- A review inferred `stale` is now rejected by the reconcile/report completeness guard, which would otherwise emit a verdict from a dead, partially-written agent set; the error guides the user to re-run rather than poll

### Fixed

- A post-fan-out persistence failure (a `WritePool` I/O error) now writes a best-effort failure marker so the review reports `failed` instead of being stuck `in_progress` forever; if even that marker cannot be written, stale inference promotes the review out of `in_progress` once its timeout elapses

*Shipped via /execute-epic (epic 1.5)*

## [1.4.0] - 2026-06-12

### Added

- `max_parallel` setting: bounds the fan-out engine's parallel lane with a buffered semaphore so a large roster cannot burst every provider call at once. Resolved through the usual `CLI --max-parallel > .atcr/config.yaml > registry.yaml > embedded default` precedence; `0` = unbounded escape hatch, negative is rejected as a usage error (exit 2). The serial lane is unaffected
- `--max-parallel N` flag on `atcr review`, a `max_parallel` key in the `atcr init` config template, and documentation in `docs/registry.md`

### Changed

- The embedded default for `max_parallel` is `10`, matching the prior effective fan-out for typical (≤10-agent) rosters. A roster larger than 10 agents that previously fired every call at once is now throttled to 10 concurrent provider calls by default — raise the cap or set `max_parallel: 0` to restore the old unbounded behavior

*Shipped via /execute-epic (epic 1.4)*

## [1.3.0] - 2026-06-11

### Added

- Project registry overlay: a repo can ship `.atcr/registry.yaml` defining its own providers and agents, merged over the user-level `~/.config/atcr/registry.yaml` so a clone is self-contained — no contributor has to mirror agent definitions by hand. Project entries shadow same-named user entries whole (no field-level merge); new names are added. Strictly parsed like every other config file
- `atcr trust` command to authorize project-defined providers: lists project providers and their status (`atcr trust`), pins one by the sha256 of its `(base_url, api_key_env)` pair (`atcr trust <name>` / `--all`) in `~/.config/atcr/trusted_providers.yaml`. Only the env var name is ever stored — never the key value
- Trust gate: a project-defined provider cannot receive a key until trusted, so a cloned repo cannot silently redirect a key to an arbitrary endpoint; `atcr review` and `atcr doctor` fail fast (exit 2) naming the provider and the `atcr trust` remedy. A project agent that references an existing user-defined provider needs no trust prompt
- Loud first-use banner naming each active project provider's `base_url` and key env on stderr
- `SOURCE` (user/project) provenance column in the `atcr doctor` table and a `source` field in its `--json` output, so overlay shadowing is visible rather than silent

### Changed

- Registry validation (roster references, fallback dangling/cycle checks, range checks) now runs over the merged user+project view; cross-tier fallback chains are supported, and every load error names the file that defined the offending entry (`registry.yaml` vs `.atcr/registry.yaml`)
- `docs/registry.md` documents the overlay, whole-entry merge semantics, the trust model, and a unified `CLI > project > user > embedded` precedence diagram now uniform across settings, personas, and definitions

*Shipped via /execute-epic (epic 1.3)*

## [1.2.0] - 2026-06-11

### Added

- `atcr doctor` command: self-tests every configured model endpoint with a trivial nonce prompt and a generous default token budget (2048, override with `--max-tokens`), so misconfigured providers, models, API keys, and base URLs are caught before a real review run burns time and tokens
- Per-agent doctor report (human table or `--json`) classifying each endpoint as `ok`, `ok_warning` (HTTP 200 but the marker is absent/empty — raise `--max-tokens`), `auth_failed`, `not_found`, `rate_limited`, `provider_error`, `network_error`, `timeout`, `missing_key`, or `invalid_config`; the key and base_url pre-flight checks run with no network call
- `atcr doctor` resolves the effective roster (agents + serial_agents, including fallback chains), deduplicates to distinct (provider, model, base_url) targets, and invokes each target at most once
- `--agents` flag to self-test a subset of the roster and `--timeout` for a per-call deadline independent of the review timeout
- Exit-code contract for `atcr doctor`: `0` when every agent has a working invocation path (primary or fallback), `1` when any agent has none, `2` for usage/configuration errors

### Changed

- `llmclient` now surfaces a structured `*HTTPStatusError` (HTTP status + bounded, secret-redacted body snippet) so callers can classify provider failures by status via `errors.As`, and exposes a per-call `max_tokens` request option (previously absent)

*Shipped via /execute-epic (epic 1.2)*

## [1.1.0] - 2026-06-11

### Added

- Reserved (parsed-and-validated, inert in 1.x) agent fields in the registry schema for the future agentic stages: `tools`, `max_turns`, `tool_budget_bytes`, `role`
- Reserved optional `verification` block (`{verdict, skeptic, notes}`) in `reconciled/findings.json` for the future adversarial-verification stage; absent in 1.x and tolerated on read
- `stages` array in `manifest.json`, recording `["review"]` in 1.x
- Reserved per-agent `turns`, `tool_calls`, `tool_bytes` counters in `status.json` (absent in 1.x)

### Changed

- Registry v1 parser now accepts the reserved agent fields (previously documented as rejected as unknown keys); they load and type-validate but remain inert, so a config can target a future stage without forcing a format-version break
- Documented every reserved field with its owning future epic in `docs/registry.md` and `docs/findings-format.md`

*Shipped via /execute-epic (epic 1.1)*

## [1.0.0] - 2026-06-11

### Added

- Go CLI binary with `review`, `reconcile`, `report`, `init`, `serve`, and `status` commands
- Fanout engine that dispatches review prompts to heterogeneous LLM reviewer personas
- Git range resolver supporting base/head refs, merge-commit SHA, and `.atcr/latest` pointer
- Reconciliation pipeline with dedup, cluster-merge, ambiguous detection, and confidence scoring
- MCP stdio server with tool schemas for review/reconcile/report/status integration
- Payload builders with diff extraction, persona templates, byte-budget truncation, and manifest generation
- Registry/config system with project overlay, persona resolution, precedence chain, and gate configuration
- Six embedded reviewer personas (bruce, greta, kai, mira, dax, otto) with shared base template
- Agent Skill definition for host-model review contribution and orchestration
- CI workflow with gofmt verification, golangci-lint, and race-enabled test suite
