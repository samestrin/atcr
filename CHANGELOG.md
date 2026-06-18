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
