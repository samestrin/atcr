## [22.2.0] - 2026-07-13

Extract the duplicated Wasm guest ABI (alloc/free/emit/pins) shared by the `goparser`, `pyparser`, and `braceparser` AST plugins into one internal `guestabi` module, with the non-moving-GC pointer-packing assumption documented in a single canonical location.

### Changed

- `goparser`, `pyparser`, and `braceparser` now import a shared `guestabi` package instead of each defining its own alloc/free/emit/pins boilerplate.

### Fixed

- Guarded `goparser`/`pyparser`'s `parse()` against negative-length host inputs, matching `braceparser`'s existing bounds check.
- Freed the result pin on the oversized-result reject path in the astgroup host, closing a pin leak on that error branch.
- Added the missing `//go:build wasip1` tag to `pyparser/main.go` so host-GOOS tooling no longer breaks on the shared `guestabi` import.
- Raised `build.sh`'s Go-version guard to 1.26 to match the parser modules' `go.mod` directive.

*Shipped via /execute-sprint (sprint 22.2)*

## [22.1.0] - 2026-07-12

Thread the reviewed-repo root through `atcr reconcile` and `atcr verify` via a new `--repo` flag, so validating findings against a repo other than the current directory (or running from a non-repo-root CWD) no longer falsely flags every finding as "file not found."

### Added

- `atcr reconcile --repo <path>` — validates finding file paths against the given repo root instead of the current directory. Defaults to `.`, preserving prior behavior.
- `atcr verify --repo <path>` — threads the same reviewed-repo root into the skeptics' code snapshot and the evidence redactor, kept consistent with `atcr reconcile --repo`. Defaults to `.`.

### Fixed

- An explicit empty `--repo` on either command now normalizes to the current directory rather than silently disabling path validation.

*Shipped via /execute-epic (epic 22.1)*

## [22.0.0] - 2026-07-12

Reap the entire process group when a local auto-fix validation command times out, so grandchild processes spawned by shells like `sh -c make ...` are killed instead of left running past the deadline.

### Fixed

- Validation-command timeouts now SIGKILL the whole process group on unix (via `Setpgid` + a group-targeted cancel) instead of only the direct child, reaping shell-spawned grandchildren that previously survived the deadline. `cmd.WaitDelay` is retained as a platform-independent pipe backstop; Windows keeps the existing direct-child behavior.

*Shipped via /execute-epic (epic 22.0)*

## [21.0.0] - 2026-07-12

Automate release packaging: a tag-triggered GoReleaser pipeline that builds reproducible, version-stamped binaries and publishes a GitHub Release on `vX.Y.Z` tag push, plus the release-process documentation and CI hardening around it.

### Added

- `.goreleaser.yaml` build configuration with dual ldflags version/commit/date stamping, `-trimpath`, and reproducible archive mtimes.
- Tag-triggered release workflow (`.github/workflows/release.yml`) that runs GoReleaser and publishes a GitHub Release on `vX.Y.Z` tag push.
- `docs/release-process.md` documenting the `vX.Y.Z` tagging convention, the local `goreleaser release --snapshot --clean` dry-run prerequisite, and the git release strategy.

### Changed

- Hardened the release workflow: scoped the tag trigger glob to real release tags and disabled cancel-in-progress so an in-flight publish is never aborted.

### Security

- Pinned every GitHub Actions `uses:` reference and the GoReleaser binary to exact commit SHAs across `release.yml`, `ci.yml`, and `reconcile-module.yml` (TD-006).

## [20.1.0] - 2026-07-12

Give standalone/public `atcr` users the review-and-fix loop the private `.planning/technical-debt/` + `/resolve-td` pipeline already provides, via a local `.atcr/`-scoped technical-debt store and an autonomous `atcr debt resolve` skill route.

### Added

- New `internal/localdebt` append-only JSONL store package (structurally modeled on `internal/scorecard`) with dedup by `FindingID` and tolerant reads.
- `atcr reconcile` persistence hook that appends reconciled findings across runs, with a `--no-local-debt` opt-out.
- `atcr debt resolve` CLI subcommand and `skill/debt-resolve/SKILL.md` documenting the four-stage RED→GREEN→ADVERSARIAL→REFACTOR resolution cycle.
- Shared `skill/CONVENTIONS.md` extracted from `skill/SKILL.md` for reuse across public skills.
- Documented the new `atcr debt resolve` capability in `docs/skill-usage.md`.

### Fixed

- Reconcile no longer persists gate-excluded or path-warned findings to the local debt store.
- `localdebt` record decoding now rejects records missing required `run_id` or `id` fields.
- `ReadAll` no longer leaks an absolute shard path in non-ENOENT open errors.

## [20.0.0] - 2026-07-11

Rewrite the standalone `skill/SKILL.md` into a single `/atcr <command>` dispatcher skill for public OSS distribution, and lock the private-skill `--output-dir` + `atcr reconcile` backend contract with a repo-local regression test.

### Added

- `/atcr <command>` dispatcher: `skill/SKILL.md` now routes to the full live `atcr` command surface instead of a single linear review-only flow, with detailed Host Review, Ambiguity Adjudication, and Findings Format instructions moved to on-demand secondary files (`skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md`).
- A repo-local `cmd/atcr/backend_contract_test.go` locking the documented `--output-dir` + `atcr reconcile` output tree (including id-or-path resolution) that private `.planning/` skill consumers depend on.
- A bidirectional test asserting the dispatcher's routed commands and the live Cobra command registry never drift apart.
- `docs/external-migration.md` documenting the private `claude-prompts` skill migration to the dispatcher pattern as a manual operator follow-up.

### Changed

- Completed the README `## Commands` table to list all 22 registered commands.
- Aligned README's documented Go version to 1.25 to match `go.mod`.
- `skill/findings-format.md` now inlines the minimal findings contract so it stays self-contained in a standalone skill install, rather than depending on a repo-relative `docs/` path.
- Documented the shipped `--sprint-plan` scope-constraint flag in the backend-contract docs (previously described as unshipped).

## [19.10.0] - 2026-07-11

Size each multi-agent reviewer's payload to its own model's token window and degrade gracefully via a configurable `on_overflow` policy instead of shipping every reviewer the same global byte budget — fixing the confirmed failure where an oversized diff returned findings from only 1 of 11 reviewers.

### Added

- Per-model context-window resolver (`ContextWindowTokens`) sizing each reviewer's payload to its own model's token window, reserving the output-token budget so input can no longer overflow a small-window model by exactly the unreserved output cap.
- Window-aware diff chunking: an over-window payload is delivered whole across appropriately-sized chunks per model (more chunks for a small-window model, fewer for a large one) instead of being shed — zero content loss on the default degradation path.
- Configurable `on_overflow` degradation policy (`chunk` default, `truncate`, `fallback`, `fail`) with per-agent dispatch, replacing the previous hardcoded shed-or-fail behavior.
- Fallback-model provenance recording in `summary.json`/`status.json` so a reviewer served by a substituted model is never silently counted as an independent distinct reviewer.
- Load-scaled request timeout (per-call and aggregate, across both serial and parallel lanes) so a small-window model fanned into many chunks on a slow backend no longer hits the flat timeout wall.
- Per-agent diagnosability fields (`effective_budget`, `resolved_window`, `reserved_output_tokens`, `chunk_count`, `degradation_action`) recorded for every reviewer in `summary.json`.
- Configurable `max_sprint_plan_bytes` setting (`.atcr/config.yaml`, default 64KB) replacing the previous hardcoded 16KB sprint-plan scope-constraint limit.
- A standalone, env-coupled live-audit harness (`examples/19.10-live-audit.sh`) that re-runs a known-bad diff range against the real model roster and hard-gates on zero context-window overflows.

### Fixed

- The fan-out diff cache now folds each reviewer's effective sizing/chunk plan into its cache key, so a per-agent-sized payload can never be served a stale, differently-sized cache hit.
- The sprint-plan `--sprint-plan` SCOPE CONSTRAINT block is now counted and capped against each reviewer's own budget, closing an overflow it could otherwise reintroduce on small-window models.
- A reviewer whose entire payload exceeds its model's window is no longer dispatched with an indistinguishable-from-healthy status; the overflow is now recorded explicitly.
- Reconcile's findings CONFIDENCE score is recomputed after fallback-model de-weighting, so two reviewers silently served by the same substituted model no longer inflate a finding's confidence.
- Assorted hardening: a negative output-token count can no longer inflate a reviewer's byte budget; an oversized final diff chunk at the chunk-count ceiling is now flagged instead of shipped silently oversized; a pathological sprint-plan byte limit can no longer silently blank the plan or integer-overflow the read ceiling; the live-audit skip guard now checks the specific previously-failing agents rather than any reachable agent.

## [19.9.0] - 2026-07-10

Add a GitHub-native `atcr personas submit <name>` command that runs the local fixture gate then forks the repo and opens a pull request via `gh`, plus a `submitted` status for two-tier curation of community persona contributions.

### Added

- `atcr personas submit <name>`: runs the existing fixture gate locally, then forks `samestrin/atcr` and opens a pull request via the user's own `gh` CLI session — no marketplace, website, or hosted registry.
- A `submitted` status for persona contributions, orthogonal to the existing `Source` provenance field, marking a fixture-passing but unvetted submission pending maintainer graduation into the vetted `personas/community/` library.
- Documentation for the `submit` subcommand and the `submitted` → graduated two-tier curation model in `docs/personas-install.md` and `docs/personas-authoring.md`.

## [19.8.0] - 2026-07-10

Add an opt-in, off-by-default CI workflow that auto-merges mechanical persona slug-bump PRs, plus the hermes maintenance-agents configuration doc covering host provisioning, drift-judgment classification, and the drafting-agent contract for maintainer-side model/persona upkeep.

### Added

- `.github/workflows/hermes-auto-merge.yml`: opt-in, off-by-default CI workflow that auto-merges mechanical persona slug-bump PRs only after the required CI check passes, gated by a fail-closed structural path/label filter that never keys on PR authorship.
- `docs/hermes-maintenance-agents.md`: documents the hermes maintenance-agent configuration surface, host provisioning runbook, judgment classification rule, and drafting agent contract.

### Fixed

- Closed a TOCTOU gap in the auto-merge workflow by pinning the PR's head SHA across the file filter, CI-status gate, and merge step.
- Auto-merge filter now rejects renamed files, closing a `.md`→`.yaml` bypass, and requires the `github-actions` app slug on the CI check-run to prevent a name-collision bypass.
- Auto-merge workflow now retries the check-runs API with backoff and fails closed on exhaustion, matches `hermes:mechanical` labels case-insensitively, bounds runtime with a job-level timeout, restricts merges to PRs targeting `main`, and handles an inaccessible PR gracefully.

## [19.7.0] - 2026-07-09

Layer live, auto-updating model resolution over persona `model` bindings — personas now bind to a vendor family/channel and resolve to a concrete slug recorded in a lock, so reviews stay reproducible by default and only change on an explicit `atcr personas upgrade`.

### Added

- `atcr personas upgrade` now resolves family/channel bindings against the live OpenRouter catalog and reports a before→after slug per persona.
- `atcr models check [--json]` reports model drift (newer family member available), deprecation, and missing-slug conditions with machine-readable output and exit codes.
- `atcr models refresh` regenerates the checked-in catalog snapshot from the live OpenRouter catalog (maintainer-only, never runs in CI).
- Persona bindings support a family/channel grammar (`<family>@stable`/`@latest`, `pin:<slug>`) resolved via provider aliases, a created-timestamp vendor scan, or an explicit pin that never floats.
- A major-version model jump now gates on the persona's fixture still passing and surfaces a "verify" flag; a minor jump auto-locks.

### Changed

- Online `atcr init`/`atcr quickstart` now derive the installed community persona roster from the fetched index itself, instead of a hardcoded built-in list, eliminating misleading "not found in community index" warnings.
- `atcr personas upgrade` classifies OpenRouter fetch failures as usage errors (exit code 2), matching `atcr models`' exit-code contract.

### Fixed

- A single malformed community-index entry no longer aborts online `init`/`quickstart` for every user (invalid names are now skipped with a warning).
- `appendExport` now warns when appending an API key to an existing group/other-readable shell profile.
- `atcr models check` no longer reports a false "missing" condition for alias-bound personas.
- Model family-prefix detection now correctly handles a non-trailing version segment (e.g. `gpt-5.4-mini`).
- Model version extraction now handles vendor-glued version tokens (qwen/glm), preventing a bound persona's lock from silently freezing.
- `atcr models refresh` now persists only substantive catalog entries and bounds its env-override snapshot read.
- Persona binding configuration now rejects control characters and oversized values at load time.

## [19.6.0] - 2026-07-08

Make the community persona channel canonical (fetched from `samestrin/atcr`, not compiled into the binary), add structured provider/model metadata for discover-by-model search, ship a model-indexed human-named persona library, and lead onboarding docs with the monetizing Synthetic path.

### Added

- Model-indexed community persona library: 10 new personas bound to specific provider/model pairs across Claude, GPT, Gemini, DeepSeek, Qwen, Kimi, and GLM, each with vendor-tuned prompt phrasing and a passing fixture.
- `atcr personas search --model`/`--provider` flags for structured discover-by-model search, plus Provider/Model columns in search output.
- `--offline` flag for `atcr init`/`atcr quickstart` to scaffold from embedded built-ins without a network fetch.

### Changed

- Default community persona registry now fetches from `samestrin/atcr` (fetch-and-pinned, with `atcr personas upgrade` to advance the pin) instead of the external `atcr/personas` registry.
- Renamed built-in personas `sentinel`→`sasha`, `tracer`→`penny`, `idiomatic`→`ingrid` (all-human-names convention; `ingrid` generalized beyond Go).
- `README.md` Quickstart now leads with the Synthetic onboarding path; frontier-provider personas repositioned as opt-in "bring your own key."
- `atcr personas list` now labels sources consistently across all three tiers (project/community/built-in).

### Fixed

- `init`/`quickstart` no longer overwrite existing on-disk persona files, even with `--force`.
- Sanitized control characters in persona search/list table output.
- Guarded persona install/fetch paths against symlink escapes and added retry/backoff for transient 5xx/429 fetch errors.

*Shipped via /execute-sprint (sprint 19.6)*

## [Technical Debt] - 2026-07-06

### Fixed

- Reviewer runs no longer parse a truncated model response twice — the parsed-finding count is now cached on the result and reused instead of being recomputed on each lookup.
- `mergeResultGroup` now aggregates the `ResponseTruncated` flag across all chunks in a group instead of only reflecting the first chunk's truncation state.
- `ResponseTruncated` is now always serialized in `status.json`/`summary.json`, even when false, so downstream field-presence checks can distinguish "not truncated" from "field missing."
- The snippet-generation fix path now prioritizes a truncated-response flag over a generic error, so a truncated fix is reported as truncated rather than as an unrelated failure.
- `mergeResultGroup` no longer drops chunked findings due to a stale parsed-finding memo.
- Corrected comments describing `truncated_zero_findings` to state it tracks GROUNDED vs RAW count divergence, and resolved two related documentation technical-debt items.
- Disabled `actions/setup-go`'s network cache on self-hosted gauntlet CI runners.

## [19.5.0] - 2026-07-06

Detect a truncated model response (`finish_reason=length`) that yields no usable output and route it into the existing fallback chain, instead of silently recording it as a clean "no issues found" review or an empty fix.

### Added

- Reviewer runs now fail over on a truncated, zero-finding response: a reviewer whose reply hit the token ceiling with nothing parseable is marked failed so the slot's fallback agent runs, instead of recording a silent false all-clear. A truncated reply that still parsed at least one finding keeps its findings and is flagged.
- Per-agent `response_truncated` marker and a run-level `truncated_zero_findings` count in `status.json`/`summary.json`, so runaway rates are observable across runs.

### Fixed

- The fixer/executor no longer silently emits a no-op "success" when its response is truncated: a cut-off fix is flagged (`fix generation truncated`) and never presented as a usable patch.
- A truncated runaway response is no longer written to the diff cache, so it cannot be replayed as a clean review on a later same-diff run.

*Shipped via /execute-epic (epic 19.5)*

## [Technical Debt] - 2026-07-06

### Fixed

- `internal/history/shard.go`: `LoadAll` now returns legacy and sharded history entries in correct chronological order instead of interleaving them incorrectly.
- `internal/history/shard.go`: `LoadShards` no longer silently skips unreadable shard files, and glob metacharacters in shard paths are now handled correctly.
- `atcr review`/`atcr resume`: deduplicated the history-recording logic into a shared `recordHistory` helper backed by centralized `history.ShardDir`/`LegacyLedgerPath`, so `atcr history` correctly merges legacy and sharded entries without mutating the legacy ledger.

## [19.4.0] - 2026-07-05

Time-based sharding for the findings history so it can be committed to version control (Team Edition) without a single, ever-growing file bloating the repo.

### Added

- Findings history is now sharded by month: each `atcr review`/`atcr resume` run appends to `.planning/history/YYYY-MM.jsonl` (the month is derived from the run time in UTC). Once a month rolls over, its shard stops receiving writes, so old shards no longer churn new git blobs.
- `atcr history` transparently queries across every monthly shard — no need to name a shard — before applying the `--since` and `--package` filters.

### Changed

- Findings history moved from the git-ignored `.atcr/findings-history.jsonl` to the version-controlled `.planning/history/` directory, so a team can commit and share trend data. A pre-existing `.atcr/findings-history.jsonl` is still read in place as read-only legacy data and merged into query results; it is never moved or rewritten.

*Shipped via /execute-epic (epic 19.4)*

## [Technical Debt] - 2026-07-05

### Fixed

- `internal/registry/overlay.go`: the insecure-registry warning is now keyed per distinct URL instead of a single process-global `sync.Once`, so a long-lived process that loads two different insecure `http` registry URLs warns for both instead of only the first.
- `internal/registry/overlay.go`: registry-fetch redirects now re-validate the URL scheme (and re-emit the insecure-URL warning) on every hop instead of only the initial URL, closing a path where an `https` registry could be silently downgraded to `http` via a redirect.
- `internal/registry/overlay.go`: documented that the package-level test seams (`remoteFetchTimeout`, `remoteRegistryBodyLimit`, `insecureRegistryWarnWriter`) must not be mutated by parallel tests.

## [19.2.0] - 2026-07-05

### Added

- `ATCR_REGISTRY_URL`: point atcr at a remote `registry.yaml` to share one team registry. When set, the user-level registry is fetched over HTTP (the config-repo pattern — commit `registry.yaml` to a shared repo and point every workstation and CI runner at its raw URL) instead of read from `~/.config/atcr/registry.yaml`; unset keeps the local-file behavior unchanged.

### Changed

- The remote registry flows through the same strict parser, validation, and env-var key contract as a local one: API keys are never read from the registry file (only `api_key_env` names travel — a literal `api_key` is a hard load error), and a set-but-unreachable/invalid URL fails loudly with no silent fallback to a local copy. Only the user registry is remote; the project overlay and trust store stay local. A non-`https` registry URL draws a one-time warning, and fetch errors and warnings redact embedded credentials and query-string tokens.

*Shipped via /execute-epic (epic 19.2)*

## [Technical Debt] - 2026-07-05

### Fixed

- `atcr audit-report`: no longer accepts `--pr 0` and silently aggregates unrelated local (non-PR) runs under a bogus "PR #0" report — non-positive PR numbers are now rejected as a usage error.
- `atcr audit-report`: an unknown `--pr` and a missing `--pr` flag now both exit with the same usage-error exit code as sibling commands, instead of a plain exit-1 error.
- Audit ledger writes that fail now print a visible stderr warning in addition to the existing log warning, so a systematically failing compliance ledger (permissions/full disk) is no longer silent.
- `atcr review --resume` no longer appends a duplicate audit record when re-running an already-complete review.
- An explicit `--pr 0` or `--pr -1` on `atcr review` no longer loses to a `GITHUB_REF`-derived PR number — an explicit flag always wins.
- The audit compliance report no longer silently drops findings with a blank/whitespace severity from its totals; they are now counted under an `UNKNOWN` column, matching `atcr history`'s behavior.
- The audit compliance report's empty-input handling now matches `atcr history`'s contract instead of carrying an unreachable, contradictory empty-state branch.
- Fixed a `sanitizeCell` escaping gap in the audit report renderer that could let a backslash-pipe sequence corrupt the rendered table.
- Fixed unescaped HTML and backticks in audit report cells (stored-injection hardening for a compliance artifact).
- Audit records are no longer silently dropped when the reviewed pool findings file has a malformed or unrecognized version header.
- The audit compliance report now renders full base/head commit SHAs instead of a truncated 12-character prefix.
- Corrected the audit package's documentation to no longer overstate tamper-evidence guarantees it does not implement, and documented the audit ledger's CLI-only hook scope and unbounded-growth read tradeoff.
- Documented the CWD==repo-root operating assumption for `atcr` subcommands (config load, git-range resolution, and audit/history ledger writes all assume the command runs from the repository root).
- Technical-debt README: audited and corrected deferral citations — genuinely deferred items now cite a real epic plan (added epics for process-group reaping on validation timeout, `reconcile`/`verify` `--repo` threading, shared Wasm guest ABI extraction, quote-aware `pyparser` scanning, grounding gitrunner reuse, and a backup-swap test seam), two mis-citations were corrected, and 23 rows citing already-completed work were removed.

## [19.1.0] - 2026-07-05

### Added

- Audit trail: every `atcr review` run now appends one tamper-evident record — run timestamp, resolved base/head SHA, PR number, and a findings-by-severity summary — to an append-only `.atcr/audit.log.jsonl` compliance ledger (a repo-level accumulator, written regardless of `--output-dir`). Resumed reviews (`--resume`) record too.
- `atcr review --pr <n>`: stamp a pull-request number on the run's audit record; falls back to parsing `GITHUB_REF` (`refs/pull/<n>/...`) when unset.
- `atcr audit-report --pr <n>`: render a one-page markdown compliance report of a PR's recorded review runs (SHAs, timestamps, findings summary); a PR with no recorded runs exits non-zero with a clear message.
- `internal/audit` package: audit record type with an append-only JSONL writer, a tolerant reader, per-run capture, and a markdown compliance-report renderer.

*Shipped via /execute-epic (epic 19.1)*

## [Technical Debt] - 2026-07-05

### Fixed

- `internal/tdmigrate`: `DecodeShardStrict` now rejects a shard file containing a second YAML document instead of silently truncating to the first (a harmless trailing `---` marker with no content is still accepted).
- `internal/tdmigrate`: `LoadShards` and `ValidateDir` now error on a missing shard directory instead of silently treating it as zero shards.
- `internal/tdmigrate`: `WriteShards` now rejects a shard `Date` that isn't in `YYYY-MM-DD` format instead of composing an invalid file path from it.
- `internal/tdmigrate`: `ParseREADME` now hard-errors on a malformed section header (bad date, or missing the colon after the source type) instead of silently skipping it.
- `internal/tdmigrate`: `ParseREADME` now hard-errors on a section with zero parseable data rows instead of silently dropping it.
- `internal/tdmigrate`: `ParseREADME` now hard-errors on a data row that lost its leading pipe instead of silently discarding it as prose.
- `internal/tdmigrate`: `Item.Validate` now reports the first blank required field in a fixed, deterministic order instead of a nondeterministic one.
- `td-migrate migrate/generate/validate`: `-h`/`--help` now exits 0 with usage on stdout instead of exiting 2 with usage on stderr.

## [Technical Debt] - 2026-07-04

### Fixed

- `internal/history`: `ParseSince` now rejects out-of-range duration values instead of silently accepting them.
- `internal/history`: `Load` no longer fails outright when the ledger contains an oversized line — it now skips the line instead.
- `internal/history`: deduplication now keeps the maximum severity for a repeated finding instead of whichever record happened to load first.
- `internal/history`: a warning is now logged when history capture skips a malformed pool row, instead of discarding it silently.
- `internal/history`: `review --resume` now appends its findings to the history ledger, closing a gap where resumed reviews were missing from the trend data.
- `atcr history` now works correctly when run from a subdirectory instead of only from the repo root.
- Corrected an overstated atomicity guarantee in the `Append` doc comment for O_APPEND writes.
- `RenderTable`: cell values now escape pipe and control characters, preventing markdown injection from unescaped input.

## [19.0.0] - 2026-07-04

Persisted review findings to a durable, queryable ledger and added an `atcr history` command so a package's finding trend can be inspected over time.

### Added

- `atcr history`: query the finding-history ledger with `--since` (a duration window that supports `d`/`w` units in addition to Go's `h`/`m`/`s`) and `--package` (a separator-aware path-prefix filter), rendering a markdown table of finding counts by severity per package. Absent or fully-filtered history exits 0 with a "no history" notice rather than an error.
- `internal/history`: an append-only JSONL ledger at `.atcr/findings-history.jsonl` recording one record per finding per review run — run timestamp, package, severity, a stable content id (hash of file+line+problem, severity-independent), file, and category — deduped by id within a run.

### Changed

- `atcr review`: every successful run now appends its findings to `.atcr/findings-history.jsonl`. The write is non-fatal (a history failure never fails an otherwise-successful review) and always targets the repo-level `.atcr/`, independent of `--output-dir`.

*Shipped via /execute-epic (epic 19.0)*

## [Technical Debt] - 2026-07-04

### Fixed

- `internal/reconcile`: bounded `review.md` reads and `extractSection` growth, and hardened matching against source-file drift, preventing unbounded memory growth and misattributed narratives when a `review.md` grows large or its filename drifts.
- `internal/reconcile`: removed a dead tier-1 branch in `anchorTier` that could never execute.
- `internal/reconcile`: rejected non-positive line numbers in `anchorTier`, preventing spurious proximity matches from line-zero anchors.
- `internal/reconcile`: `extractSection` now absorbs a list-item marker line when the anchor sits on a continuation line, preventing malformed markdown extraction.
- `internal/reconcile`: logged a warning when `review.md` narratives exist but match zero findings, surfacing a previously silent no-op.
- `internal/reconcile`: pre-indexed `review.md` anchors so `matchNarrative` scans only candidate lines instead of rescanning the full narrative per finding, fixing an O(findings × narrativeBytes) performance issue.
- `internal/reconcile`: switched per-line anchor dedup to a slice-based approach, avoiding a map allocation.

## [18.2.0] - 2026-07-04

Enriched the shared reconciliation engine so each finding's originating `review.md` narrative survives past reconciliation, giving downstream technical-debt-resolution consumers the reviewer's reasoning without re-deriving it from raw review logs.

### Added

- Reconciler: `reconciled/findings.json` findings now carry a `justification` field — the narrative section extracted best-effort from the finding's originating source `review.md`, matched by `FILE:LINE` — plus a `source_report` back-reference (`{path, line, section}`) pointing at that review.md section. Both are additive and `omitempty`, so a finding with no matched narrative stays byte-identical to prior output.
- `docs/findings-format.md`: documents the new fields under the JSON form section, and notes under Source discovery that a source's `review.md` is now read at reconcile time.

### Changed

- The reconcile pipeline reads each source's `review.md` (alongside its `findings.txt`) and correlates it to findings by `FILE:LINE`. A line-level, proximity-bounded match is required, so an unrelated same-file mention never attaches a misleading narrative. The pair lands only in `findings.json` — never the technical-debt table's frozen `Problem` cell (per Epic 18.1's column-stability constraint).

*Shipped via /execute-epic (epic 18.2)*

## [Technical Debt] - 2026-07-04

### Fixed

- `internal/reconcile`: logged a debug summary (anchored/total counts) of symbol anchors stamped in `stampSymbolAnchors`, closing a gap where anchoring outcomes were invisible without a debugger.
- `docs/technical-debt-format.md`: documented that the `(symbolName)` anchor is rendered verbatim by every in-repo consumer of `JSONFinding.Problem` — the GitHub Action PR comment, check-run summary table, and human-readable report — not only the technical-debt table.

## [18.1.0] - 2026-07-04

Anchored each reconciled finding to the name of its enclosing code symbol so technical-debt items keep a stable relocation key when intervening edits drift their line numbers.

### Added

- Reconciler: each finding's `problem` is now prefixed with `(symbolName)` — the nearest enclosing named AST block (function/method/class/type) — resolved via the existing `internal/astgroup` parsers. The anchor is a drift-stable `RELOCATE_KEY` for downstream technical-debt resolution.
- `docs/technical-debt-format.md`: documents the `(symbolName)` Problem-cell contract — placement, resolution semantics, graceful degradation, table safety, and how a consumer parses it.

### Changed

- The anchor is omitted (Problem cell byte-identical to before) when no named block resolves — unsupported language, absent/unparseable file, file-level finding, or `ATCR_DISABLE_AST_GROUPING` — and when the resolved name would not be table-safe, so the technical-debt table format stays backward-compatible.

*Shipped via /execute-epic (epic 18.1)*

## [Technical Debt] - 2026-07-03

### Fixed

- `atcr debt add`: seed the interactive wizard from partial flags when run on a TTY instead of discarding them.
- `atcr debt add`: name the specific missing flags on a partial invocation rather than emitting a generic error.
- `atcr debt add`: validate the `--date` value as `YYYY-MM-DD`, rejecting malformed dates that produced bogus month buckets.
- `atcr debt add`: normalize status and source-type enums so casing no longer causes inconsistent values.
- `atcr debt add`: surface scanner errors from the wizard prompt instead of silently ignoring them.
- `internal/debt`: serialize `AppendItem` with a shared mkdir-based README lock, and roll back the README when a post-write shard sync or stats refresh fails.
- `atcr debt dashboard`: distinguish a zero `--top` cap from an empty backlog, and reject the conflicting `--check --stdout` combination.
- `atcr debt dashboard`: sanitize `File` and `Component` cells so table-breaking content is escaped, and distinguish filenames containing spaces from prose in the `Component` column.
- `atcr debt`: guard the table truncation helper against a non-positive width, and skip shard sync when `--check` is set so a drift check no longer mutates the working tree.
- `internal/tdmigrate`: preserve content between the Stats table and the Last Modified line in `RefreshStats`, validate the month prefix before bucketing in `monthHistogram`, and correct the `Sort` doc comment to match the implemented tiebreak chains.

## [18.0.0] - 2026-07-03

Added an `atcr debt` command namespace for querying, capturing, and reporting technical debt directly from the Epic-12.1 sharded store, making the structured YAML shards a read source for the first time.

### Added

- `atcr debt list`: filter (severity/status/category/component/group) and sort (severity/age/est/file) technical-debt items into a terminal table; `--sync` regenerates the shards from the authoritative README before reading.
- `atcr debt add`: file a new item into the authoritative README table and regenerate the shards in one step — flag-driven for scripting, with an interactive wizard on a TTY when required flags are omitted. Keeps the README's Stats summary self-consistent after each add.
- `atcr debt dashboard`: generate a deterministic, read-only aggregated Markdown dashboard (totals, by severity, by component, by age, top priority) at `.planning/technical-debt/DASHBOARD.md`, with a `--check` drift mode for CI and pre-commit hooks and secret-token scrubbing of finding text.
- `internal/debt`: a query/aggregation/render layer over `internal/tdmigrate`, reusing its Item/Shard types, shard loader, and README↔shard migration rather than re-parsing the format.
- `docs/technical-debt.md`: `atcr debt` usage plus pre-commit and GitHub Actions integration examples.

*Shipped via /execute-epic (epic 18.0)*

## [17.0.0] - 2026-07-03

Added an opt-in `--auto-fix` flow that closes the loop from detection to a ready-to-review pull request: ATCR now applies the fixes it generates, validates them locally, reverts automatically on failure, and only then orchestrates a Git branch, commit, and PR through the GitHub API.

### Added

- `atcr review --auto-fix`: opt-in flag (off by default) that parses a model-generated diff, applies it to the local working tree, runs a configurable local validation command, reverts on failure, and — only after validation passes — creates a branch, commit, and pull request. Guarded by an all-or-nothing refuse-without-backend gate.
- `internal/autofix`: safe patch apply and revert over `atomicfs`, wrapping `go-gitdiff` — per-file backups, symlink-escape guard at the write boundary, refusal to clobber an existing target on a create diff, and full restore of touched files when validation fails.
- `internal/verify`: configurable local validation runner with a conservative exit-code-only pass/fail gate, default timeout handling, and directory/argv guards.
- `internal/ghaction.Client`: `CreateBranch`, `CreateCommit`, `CreatePullRequest`, and `UpdatePullRequest` via the GitHub Git Data API, reusing existing retry/backoff/redaction plumbing, with an open-PR existence check before creating a duplicate.
- A commented `auto_fix` stanza documented in the `atcr init` config template.

### Changed

- `CreateCommit` now redacts the commit message before sending it to GitHub and guards against an empty parent tree SHA.
- `sendDo`/`get` honor `Retry-After` on 403 secondary rate limits; `FindOpenPullRequest` requests the maximum page size.

### Fixed

- `copyFile` now honors permissions on an existing destination so backup/restore round trips preserve file mode.
- Validation errors (`ErrWaitDelay`/`context.Canceled`) are classified as run failures rather than start errors.
- A stranded backup left on apply write/delete failure is now cleaned up; duplicate-path skipped fixes are surfaced.
- The `--auto-fix` gate rejects a malformed or insecure `api-url` and resolves the apply target to an absolute, repo-root-only path.
- No GitHub-mutating call is reachable before local validation passes; the empty-selection message distinguishes all-below-threshold from no-fix.

*Shipped via /execute-sprint (sprint 17.0)*

## [Technical Debt] - 2026-07-02

### Fixed

- Resolved a scanner error silently discarded in `cmd/atcr/quickstart.go`'s `readLine`, so I/O failures during interactive prompts are now surfaced instead of swallowed.
- Extracted an `expandHome` helper so `~/`-prefixed paths expand consistently across the quickstart flow.
- Fixed `appendExport` leaving the profile world-readable after appending an API key; the file is now chmod'd to `0600`.
- Fixed invalid `SignupURL` handling and referral-link fragment mishandling by validating and building the link via `url.Parse`.
- Fixed the agents header rendering bare when the roster is empty.
- Closed a symlink-follow race in quickstart's check-then-write file creation by switching to an atomic `O_EXCL` create.
- Fixed a symlink profile that could leak the API key into `.atcr` by resolving symlinks before the `profileIsAtcrOwned` guard.
- Fixed a case-insensitive bypass of the `.ATCR` ownership guard by comparing ownership via inode identity instead of string prefix.
- Fixed unchecked control characters in provider fields by scanning them during manifest validation.
- Fixed unsafe plain YAML scalars (model ids, provider fields) being accepted by manifest validation; they are now rejected.
- Fixed an inlined GitHub Actions expression in a workflow run line by passing `base_ref` via an environment variable instead.
- Pinned the refresh-manifest workflow's actions to commit SHAs.
- Corrected a stale prefix-comparison comment in `profileIsAtcrOwned`.
- Documented why the quickstart scaffold intentionally keeps `go install @latest` unpinned.

## [16.0.0] - 2026-07-02

Added `atcr quickstart`, an interactive onboarding wizard that scaffolds a working synthetic-provider setup so a new user reaches their first review without hand-editing `registry.yaml`.

### Added

- `atcr quickstart` command: reuses `atcr init`'s writers for the `.atcr/` workspace, then sets up the synthetic provider + agents in `~/.config/atcr/registry.yaml`, walks the user through the `LLM_SYNTHETIC_API_KEY` environment variable (the key is never written into any atcr-owned file), and scaffolds a `.github/workflows/atcr.yml` CI workflow. Flags: `--open` (open the signup link in a browser) and `--force` (regenerate existing files).
- A bundled synthetic-provider manifest and a scheduled GitHub Action that refreshes it from the live `/models` endpoint.

### Changed

- The referral signup link is shown as an OSC-8 terminal hyperlink; the scaffolded workflow and generated registry both key off the synthetic provider.

*Shipped via /execute-epic (epic 16.0)*

## [Technical Debt] - 2026-07-02

### Fixed

- Clamped the running cost accumulator in benchmark export to prevent +Inf/NaN from propagating into scorecard JSON output.
- Restored `os.Stderr` correctly when a resumed benchmark run errors during stderr capture.
- Excluded empty-`Expected` cases from the `CorroborationRate` denominator so it reflects only cases with a defined expected result.
- Guarded invalid `CostUSD` values in `scoreOne` and removed duplicate `normalize()` calls in the matched-findings loop.
- Clamped negative `CostUSD` before computing `CostPerCorroboratedFindingUSD`.

## [15.1.0] - 2026-07-02

Fixed a public leaderboard schema ambiguity where a paid-but-ineffective reviewer (real cost, zero corroborated findings) rendered byte-identical to a genuinely free reviewer.

### Changed

- `cost_per_corroborated_finding_usd` in the public leaderboard/benchmark submission schema is now `omitempty`: the key is omitted entirely when there are zero corroborated findings (cost-per is undefined), and present with a real value — including `0.0` — only when corroborated findings exist. This mirrors the existing `survived_skeptic_rate` pattern.
- Updated the benchmark scorer (`atcr benchmark run`/`export`) to compute the identical N/A representation as the production leaderboard export path.

### Fixed

- A paid reviewer that matched zero planted/defect categories no longer reads identically to a free reviewer on the public leaderboard.

*Shipped via /execute-epic (epic 15.1)*

## [Technical Debt] - 2026-07-01

### Fixed

- Validated documented CLI flags per-command instead of against a global flag union, catching flags documented on the wrong subcommand.
- Fixed subcommand validation to catch bypasses where flags appear after the subcommand token.
- Fixed docs-index link parsing to strip quoted titles before matching, avoiding false negatives on titled index links.
- Anchored config-block detection in the docs audit to headings or fenced YAML keys, rejecting prose-only false matches.
- Strengthened the architecture-doc guard test to assert load-bearing reconciler facts rather than just keyword presence.

## [15.0.0] - 2026-07-01

Audited the documentation against the current engine and eliminated the drift found, so `docs/` is a trustworthy single source of truth for the upcoming website build.

### Added

- `docs/architecture.md` — an architecture overview of the multi-model reviewer panel and the deterministic reconcile → verify → debate pipeline.
- `docs/README.md` — a documentation index that links every doc, establishing `docs/` as the single source of truth.
- Documentation-audit regression tests that assert the docs reference only real CLI commands, subcommands, and flags, and that the docs index covers every doc.

### Fixed

- Corrected the persona-resolution chain in `docs/registry.md` to stop documenting a non-existent `--task-message` CLI flag; that override is an internal resolution seam, not a flag.

*Shipped via /execute-epic (epic 15.0)*

## [Technical Debt] - 2026-07-01

### Fixed

- Hardened the `buildOneAgent` fan-out review test helper to return an explicit error when its single-slot roster assumption is violated (empty or chunked config) instead of panicking on `slots[0]` or silently returning a partial-payload agent.
- Renamed the stale `TestBuildAgent_*` fan-out tests to `TestBuildOneAgent_*` to match the actual `buildOneAgent` helper seam after `buildAgent` was removed in epic 14.4.

## [14.4.0] - 2026-07-01

Removed the orphaned `buildAgent` helper and consolidated fan-out reviewer mode/payload resolution to a single production seam, eliminating divergent duplicate logic.

### Changed

- Fan-out agent mode/payload resolution now lives solely in `buildSlots`' slot-build path (via `renderAgent`); its tests were retargeted at that production seam, so future payload-mode or error-message changes only need updating in one place.

### Removed

- Dead `buildAgent` function in `internal/fanout/review.go`, which had no production caller after epic 14.3 inlined resolution into `buildSlots`.

*Shipped via /execute-epic (epic 14.4)*

## [14.3.0] - 2026-07-01

Added an opt-in context-aware diff chunking strategy so large reviews can be split into smaller, attention-friendly chunks per reviewer without inflating API cost by default.

### Added

- `review_strategy` setting (`bulk` | `chunked`, default `bulk`): `chunked` bin-packs each reviewer persona's diff into multiple context-limited calls to curb the attention-degradation hallucination that large single-prompt diffs trigger, while `bulk` keeps the whole diff in one prompt per persona to bound API cost. Resolves once per run across the registry and project config tiers.
- Per-agent `max_context_lines` (default 1500) capping a single chunk's diff line count, letting each model use a context window tuned to its capabilities and cost profile. Bin packing groups multiple files per call to minimize request duplication; a file larger than the cap is sent as its own chunk (never split) with a warning.

### Changed

- In `chunked` mode, all of a persona's chunk findings merge into a single reviewer source, so a multi-chunk review attributes to one persona and the consensus filter counts it as one voter rather than many.

*Shipped via /execute-epic (epic 14.3)*

## [Technical Debt] - 2026-07-01

### Fixed

- Broadened the consensus-filter security exemption to recognize common security synonyms (`vulnerability`, `auth`, `injection`) so genuine security singletons are not dropped as uncorroborated noise.
- Normalized the ambiguous sidecar wire shape for isolated findings so DBSCAN-isolated noise and consensus-filtered singletons both emit a raw per-source finding with `Reviewer` set and `Reviewers`/`Confidence` cleared.
- Added a regression test confirming PageRank authority-promoted singletons survive the consensus filter.
- Recorded partial chunk coverage as `UnreviewedChunks` in `summary.json`.
- Gated the oversize-file warning on single-file diffs and warned when chunked reviews produced no actionable files.
- Bounded per-agent chunk count via `maxChunksPerAgent`.
- Applied neutral truncation to individual chunks instead of copying whole-payload truncation into every chunk.
- Suppressed oversized-file warning on resume rebuilds.
- Hoisted redundant chunk scans in `internal/fanout/review.go`.
- Recognized combined-diff boundary markers in the chunker.
- Validated `review_strategy` values during project-config loading.
- Fixed `FallbackFrom` being overwritten during chunk fallback.
- Fixed serial chunked `DurationMS` under-counting.
- Clamped non-positive `EffectiveMaxContextLines` to the default value.
- Fixed trailing partial-line undercount in chunk boundaries.
- Fixed no-prefix diff chunking.
- Clarified that `review_strategy` has no CLI tier.
- Corrected a misleading `max_context_lines` comment in `internal/registry/config.go`.

## [14.2.0] - 2026-06-30

Made the reconciliation layer a strict gatekeeper against hallucinated technical-debt items by filtering uncorroborated singleton findings and hardening the host review prompt.

### Added

- The reconciler now applies a consensus filter: on a panel of at least three distinct reviewers, a finding raised by only a single reviewer (below `HIGH` confidence) is routed to the ambiguous sidecar instead of the promoted `findings.json` and the CI gate — unless it is security-related, `HIGH`/`CRITICAL` severity, out-of-scope, or independently confirmed. A `consensus_filtered` count in `summary.json` and the report records how many were routed. The filter is inert below three reviewers, so the single-API-key (host + 1 pool persona) workflow is untouched. Reviewer count is measured across findings, not source directories, so it fires correctly even though pool personas are flattened into one `pool` source.

### Changed

- The host review prompt now instructs the host to aggressively reject any finding it cannot ground in the diff, and frames ambiguity adjudication as a strict gatekeeper against false positives — never merging an ungrounded finding into a real one.

*Shipped via /execute-epic (epic 14.2)*

## [Technical Debt] - 2026-06-30

### Fixed

- Recorded `grounding_enabled` and its disabled reason in pool `summary.json`.
- Documented that renamed-file old-path citations are dropped as ungrounded.
- Corrected grounding-drop comment to stop claiming `status.json` parity with `enforceConstraints`.
- Exported `reconcile.LineProximity` and bound grounding tolerance to it.
- Raised grounding evidence floor to 18 and counted runes instead of bytes so boilerplate/multibyte evidence no longer grounds.
- Made `changed` a required nil-able parameter for `WritePool`/`findingsFor`/`writeResumedAgents`.
- Capped evidence length to prevent amplification in grounding.
- Resolved `a/` `b/` prefix over-normalization in grounding.
- Resolved empty `ChangedLines` map fail-open bug in grounding.
- Stated hard-drop rule in `scopeFiles` for out-of-range findings.
- Qualified `scopeChangedOnly` discard clause by grounding condition.
- Reused `hunkHeaderRe` in `parseFileChange` for consistent hunk detection.

## [14.1.0] - 2026-06-30

Grounded multi-agent review findings in the actual diff so reviewer models can no longer report hallucinated technical-debt items.

### Added

- `atcr review` now runs a grounding gate that drops any finding whose cited `FILE:LINE` is not anchored in the reviewed patch — the fabricated-file and invented-line hallucinations that flooded large reviews — before findings reach the reconciler. A finding is kept when its line falls within a changed range (with a ±3-line tolerance for reviewer drift), when its evidence quotes a changed line, or when it is tagged `CATEGORY out-of-scope`; the per-agent drop count is logged to stderr. The gate covers both fresh and resumed reviews and is disabled only for the range-less `atcr reconcile <dir>` path, which has no diff to check against.

### Changed

- Reviewer persona prompts now require every finding to cite an exact `FILE:LINE` drawn from the diff and forbid reporting code outside the changed lines, with `CATEGORY out-of-scope` the sole sanctioned way to raise a genuine pre-existing issue in unchanged code.

*Shipped via /execute-epic (epic 14.1)*

## [13.6.0] - 2026-06-30

Expanded AST-isomorphism finding grouping to four more brace-based language families.

### Added

- AST-isomorphism finding grouping now covers Java (`.java`), Kotlin (`.kt`/`.kts`), C/C++ (`.c`/`.cpp`/`.cc`/`.cxx`/`.h`/`.hpp`), and C# (`.cs`) source, so `atcr reconcile` clusters findings in those languages by their enclosing code block across line-number drift instead of falling back to ±3 line proximity.

### Changed

- The shared brace parser now names methods that carry modifiers/return types (e.g. `public void execute()`, `void Foo::bar()`) so they group as named blocks, and treats `"""` triple-quoted strings (Kotlin multiline strings, Java text blocks, C# raw string literals) as opaque so their braces never skew block structure.

*Shipped via /execute-epic (epic 13.6)*

## [Technical Debt] - 2026-06-30

### Fixed

- Fixed missing `authority_promoted` count in report output.
- Hardened `countAuthorityFlips` oracle to mirror the base-confidence flip predicate.
- Fixed `classifyHeader` else-if chains being misclassified by preferring `else` classification.
- Fixed control headers (e.g., `if`/`while`/`for`) being misnamed as functions in `parse_core.go`.
- Rejected statement-keyword prefixed call headers in `funcParenName` so they no longer masquerade as functions.
- Extended `funcParenName` reserved-word guard to cover `try`, `synchronized`, `using`, `lock`, and `fixed`.
- Locked control-word `funcParen` guards for C# and Java to prevent false function positives.
- Pinned triple-quote degrade edges for empty, unterminated, and four-quote literals.
- Documented and degraded Java escaped triple-quote and C# nested interpolation limitations.
- Documented `funcParenName` expression-statement false positives and added coverage tests.
- Added per-language keyword→kind mapping tests and verified tables reach each brace WASM.
- Added `funcParen` coverage for trailing-token cases and Kotlin constructor configurations.
- Updated brace parser header comments for all eight supported languages.
- Made the parser build script portable on macOS.
- Resolved `.h` / `.hpp` Objective-C note handling in `LanguageForExt`.

## [13.5.0] - 2026-06-29

### Added

- Added an `authority_promoted` field to the reconcile run summary
  (`reconcile-json/v1`) recording how many findings PageRank authority promotion
  raised from MEDIUM to HIGH confidence in a run — observability for the epic 13.3
  promotion signal, which was previously silent.

### Changed

- The reconcile summary wire schema gained the additive `authority_promoted` key.
  This is a v2 wire-schema change (sanctioned by this epic); existing consumers are
  unaffected unless they decode `reconcile-json/v1` with strict unknown-field
  rejection.

*Shipped via /execute-epic (epic 13.5)*

## [Technical Debt] - 2026-06-29

### Fixed

- Fixed PHP attributes (`#[...]`) being swallowed as comments in the brace parser.
- Corrected typed arrow annotations (`foo<T>() => ...`) being mislabeled as functions.
- Stopped Bash arithmetic shifts (`<<` / `>>`) from being misdetected as heredoc starts.
- Added handling for Bash brace expansion so it no longer opens spurious blocks.
- Fixed heredoc strip-tab logic so PHP `<<<` heredocs no longer incorrectly strip leading tabs.
- Fixed multi-byte escape sequences (`\u{7f}`) being dropped due to `charLiteralLen` returning zero.
- Relaxed `funcParenName` so TypeScript method modifiers no longer defeat function detection.
- Fixed `identAfter` to skip generic parameters after keywords like `impl`, preserving the real identifier name.
- Stopped `classifyHeader` from treating `catch (e)` as a function when TypeScript `funcParen` is enabled.
- Corrected `classifyHeader` arrow-function detection to consider arrow position rather than just the last `=>` index.
- Reduced inefficient string allocation in the brace parser core scanner.
- Documented regex-as-unhandled in the brace parser normal state and added a dedicated CI job for the brace parser module.

## [13.4.0] - 2026-06-28

### Added

- AST-isomorphism grouping now covers TypeScript/JavaScript (`.ts`/`.tsx`/`.cts`/`.mts`/`.js`/`.jsx`/`.mjs`), PHP (`.php`), Rust (`.rs`), and Bash (`.sh`/`.bash`): duplicate findings in those languages group by their smallest covering block across line drift > 3, instead of falling back to the ±3 line-proximity gate.
- A shared brace-block parser (`braceparser`) compiled once per language into a vendored wasip1 `.wasm`, parameterized by a per-language keyword/comment/string/heredoc table — adding the next brace language is a config-table plus corpus change, not new parser code.

*Shipped via /execute-epic (epic 13.4)*

## [Technical Debt] - 2026-06-28

### Fixed

- Capped distinct reviewers per agreement group and sorted them deterministically, eliminating an unbounded quadratic loop in `addAgreement`
- Stored PageRank edge weights as `float64` to avoid repeated int-to-float conversions
- Replaced manual absolute-value delta logic with `math.Abs` for consistent convergence
- Precomputed the authority baseline once per reconcile run and added an explicit authority map lookup before promotion
- Renamed the `any` variable to `hasAgreement` to avoid shadowing the Go builtin
- Preallocated the `allGroups` slice with known capacity
- Updated `Confidence` documentation to reflect that `HIGH` can result from authority promotion, not only multiple reviewers
- Annotated dangling-mass redistribution as defensive-only for the always-connected agreement graph
- Documented the strict authority baseline boundary assumption
- Added invariant tests for symmetric K4/K5/cycle graphs, disconnected components, never-agreed isolated findings, and run-twice determinism
- Reworded `BenchmarkModelAuthority` to characterize rather than validate the NFR
- Fixed undefined `merged` variable and removed dead code in `reconcile/reconcile.go`

## [13.3.0] - 2026-06-28

### Added

- Deterministic per-run PageRank "authority" signal over a model-agreement graph (nodes are models, count-weighted edges are cross-model agreements), built once per reconcile run with a damped power iteration (damping 0.85, strict iteration backstop) — fully offline and stdlib-only.

### Changed

- Confidence scoring now factors model authority: an isolated (single-reviewer) finding is promoted from `MEDIUM` to `HIGH` when its model's run authority exceeds the uniform `1/N` baseline. Promotion is one-directional (authority never lowers confidence), and confidence stays identical to the prior reviewer-count behavior when a run has no cross-model agreement.

*Shipped via /execute-epic (epic 13.3)*

## [Technical Debt] - 2026-06-28

### Fixed

- Cached per-cluster relation and similarity matrix once and read it from all three sites in `reconcile/dedupe.go`
- Collapsed unattributed findings to one density source so duplicate hallucinations no longer self-corroborate
- Namespaced real versus anonymous source keys disjointly to block reviewer spoofing
- Capped location clusters at `maxClusterSize`, degrading to singletons to prevent unbounded memory growth
- Locked DBSCAN single-pass neighbor invariant and documented why no adjacency cache is needed
- Added complete-linkage acceptance to `bipartiteGroups` to prevent merge-strength chain over-merges
- Short-circuited `hungarianAssign` single-row/column paths to an O(n) argmin
- Added a maximum matrix size guard to the Hungarian algorithm
- Added `AmbiguousCount` and `NoiseCount` fields to the reconciler summary
- Skipped gray recording for findings that share an AST key
- Validated key length in `dedupeCluster`
- Deduplicated the DBSCAN seed queue to avoid O(n²) growth
- Added a safety iteration limit to the Hungarian algorithm
- Derived `noMatchSentinel` from the distance ceiling
- Resolved documentation accuracy for bipartite matching

## [13.2.0] - 2026-06-28

The reconciler now deduplicates findings with optimal bipartite matching instead of greedy clustering, and deterministically isolates uncorroborated single-model noise into the ambiguity sidecar — so consensus findings stay trustworthy and the sidecar can be trusted as strictly uncorroborated noise.

### Changed

- Reconciler deduplication now aligns findings across models with Kuhn-Munkres (Hungarian) bipartite matching, replacing the greedy single-linkage clustering that could transitively over-merge non-duplicate findings; each consensus issue gathers at most one finding per model
- Duplicate edge weights combine the 13.1 AST-isomorphism signal (structurally isomorphic findings match at distance 0) with token-set Jaccard distance, computed in-tree with no new dependencies and no floating-point boundary drift

### Added

- DBSCAN-based noise isolation: a single model's uncorroborated finding standing alone where other models corroborate each other is deterministically moved to the `ambiguous.json` sidecar and removed from the consensus output, with no arbitrary cluster-count threshold

*Shipped via /execute-epic (epic 13.2)*

## [Technical Debt] - 2026-06-28

### Fixed

- Documented AST grouping behavior, the `ATCR_DISABLE_AST_GROUPING` opt-out, and Python clause-sibling grouping
- Wired the reconciler to a shared wasm parser Host to amortize wazero runtime across reconciles
- Eliminated serialized parsing in the AST grouper by moving parse work outside the mutex with per-file singleflight
- Memoized group keys by line and Merkle hashes by address to reduce redundant AST work
- Resolved lazy AST grouper construction, canonicalized cache paths, and normalized cross-platform path checks
- Gave sibling non-block wrappers distinct addresses so sibling function literals no longer collide
- Hardened wasm parser lifecycle: enforced parse timeouts, detected use-after-close, recreated modules after timeout, and discarded instances after guest-call traps
- Added resource guards: capped wasm linear memory at 256 MiB, bounded alloc/parse result sizes, and made `maxSourceBytes` configurable
- Hardened AST safety: bounded MerkleHash recursion depth, rejected over-deep decoded trees, and logged degradation instead of silently falling back to line proximity
- Fixed Python parsing edge cases: folded multi-line bracket-continued headers, corrected tab-stop indent, and improved compound-keyword `isHeader` detection
- Fixed Go parser edge cases: handled empty-input file nodes, dropped dead TypeSpec cases, and added a wasip1 build guard
- Added a committed SHA256SUMS manifest for embedded `.wasm` parsers and regenerated it during build
- Marked wazero as a direct dependency and pinned the Go toolchain in `parsers/build.sh`
- Guarded benchmark recall against division by zero and relaxed AST false-positive assertion to <1%

## [13.1.0] - 2026-06-27

AST plugin architecture for the reconciler: findings that refer to the same logical code block now group together even when their reported line numbers drift, using language parsers compiled to WebAssembly and run on a zero-CGO wazero host.

### Added

- AST-isomorphism grouping for reconciler findings: a finding's line is mapped to its smallest covering AST block and a structural Merkle hash of that block becomes the grouping key, so whitespace/blank-line/model line-skew no longer splits duplicate findings
- WebAssembly parser plugins for Go and Python, vendored and embedded via `go:embed` and executed on a pure-Go wazero runtime (no CGO; the binary still cross-compiles to every target out of the box)
- A runtime parser-override directory so new languages can be enabled by dropping in a `.wasm` file, and an `ATCR_DISABLE_AST_GROUPING` env opt-out that reverts to the legacy line-proximity behavior

### Changed

- The reconciler now uses AST block identity as the primary clustering signal, with ±3 line proximity as the fallback when no parser is available for a finding's language (benchmark: AST recall 1.00 / precision 1.00 vs line-proximity recall ~0.79 with false merges of adjacent-but-different blocks)

*Shipped via /execute-epic (epic 13.1)*

## [Technical Debt] - 2026-06-27

### Fixed

- Documented the closed caller set for `WithExecEligibility` and restricted lint references to `fanout`/`verify` only
- Made exec-name lint match whole tokens instead of substrings, and documented the non-exhaustive verb list
- Made `WriteShards` stage shard writes to temp files and atomically swap, preventing partial wipes of `items/` on failure
- Added a warning when `Item.Notes` are dropped from a regenerated ToC
- Made `generate`/`validate` reject migrate-only `--readme` via the items-only flag set
- Made `sprintPlanPath` assert `--sprint-plan` flag existence in `cmd/atcr/review.go`
- Made resume recover the scope constraint from the persisted artifact
- Called `resolveScopeConstraint` in `PrepareResume` so the scope constraint is preserved on resume
- Wrote `scope-constraint.txt` artifact in `finalizePreparedReview` for scoped reviews
- Capped `SCOPE CONSTRAINT` plan body relative to `PayloadByteBudget`
- Neutralized `BEGIN`/`END` markers in `ScopeConstraint` to prevent delimiter-collision injection
- Made `registerExec` reject tool names not in `ExecutionTools` instead of panicking

## [12.2.0] - 2026-06-27

### Added

- **Sprint-plan scoping (epic 12.2):** `atcr review --sprint-plan <path>` accepts a markdown sprint/epic plan and injects it as a `SCOPE CONSTRAINT` block immediately before the diff in every reviewer's prompt, so reviewer personas suppress findings for changes unrelated to the plan's active work items (dependency bumps, formatter-only reformatting, mechanical refactors). The constraint is a soft scope — genuinely critical out-of-scope issues are still reported. Restores the capability dropped in epic 12.0.
- The plan is read once per review and capped at 16 KiB on a UTF-8 boundary before injection, so it cannot inflate agent prompts past the payload byte budget. A missing or empty plan is ignored (the review proceeds diff-wide); an unreadable plan warns on stderr and proceeds. Because the constraint is part of the rendered prompt, the diff cache key invalidates automatically when the plan changes.
- The ingestion path (`PrepareReviewFromDiff`) honors `--sprint-plan` too; `ReviewRequest.SprintPlanPath` exposes the field to library and MCP callers.

*Shipped via /execute-epic (epic 12.2)*

## [12.1.0] - 2026-06-26

### Added

- **Technical-debt storage sharded by source (epic 12.1):** every item in `.planning/technical-debt/README.md` is now also stored as a structured YAML file under `.planning/technical-debt/items/`, one shard per `### [date] From <Sprint|Review>:` section. A 50–100 finding review is one shard file (not 50–100), and concurrent review/sprint runs each write their own file, so they no longer merge-conflict on TD storage. Each item supports unconstrained multi-line `problem`/`fix`/`notes`.
- Added `cmd/td-migrate` (logic in `internal/tdmigrate/`): `migrate` (README table → shards), `generate` (shards → regenerated ToC table to stdout), and `validate` (strict-load + schema-check; a malformed shard fails loudly). Migration of the live corpus is lossless (28 sections / 55 items round-tripped), proven by the Go test suite including an adversarial YAML footgun corpus.
- Documented the sharded format additively in the TD README and added `items/SCHEMA.md` (shard schema, file naming, YAML-safety guarantees).

*Additive and not yet canonical: the README Markdown table remains authoritative and machine-read by all existing tooling; the shards are generated alongside it. The canonical cutover is deferred to a follow-on epic.*

*Shipped via /execute-epic (epic 12.1)*

## [12.0.0] - 2026-06-26

### Added

- **Code-review skill integration (epic 12.0):** atcr is now the multi-agent reviewer backend for the external `execute-code-review` skill, replacing the legacy `llm-support` backend. The skill drives `atcr review --output-dir` + `atcr reconcile`; its reconcile step consumes the 8-column per-source pool stream directly. Validated end-to-end against a live sprint review (real fan-out → reconcile → cross-source REVIEWERS/CONFIDENCE merge into the technical-debt store).
- Added `atcr --version` flag and `atcr version` subcommand (both report the same string, derived from ldflags, the installed module version, or the VCS revision) — previously `atcr --version` exited 2 with "unknown flag", which broke downstream pre-flight checks that probe the binary that way.
- Added `docs/code-review-backend.md` documenting the `--output-dir` contract for driving atcr as the reviewer backend of a separate code-review skill or pipeline (invocation, output tree, 8-column pool vs 9-column reconciled streams, partial-run and finding-count behavior).

*Skill backend integration (epic 12.0) — atcr-side enablers and documentation; no behavioral change to review/reconcile.*

## [Technical Debt] - 2026-06-26

### Fixed

- Fixed a data race between `Dispatcher.SetLimits` and `capResult` by guarding `limits` (and the exec backend fields) under the dispatcher mutex
- Replaced the zero-size `struct{}` context keys with distinct `iota`-typed keys, eliminating a latent collision where Go may give two zero-size keys the same address
- Made the verify determinism re-run's exec-eligibility grant structural at the function boundary (`reproduceAgain` now takes an explicit `exec` flag), so a non-exec flow cannot re-introduce a non-exec → exec escalation
- Rejected `run_tests` with an empty configured test command and `run_script`/`run_tests` with a non-positive execution timeout, instead of forwarding an unusable spec to the sandbox

### Added

- Surfaced exec-eligibility refusals operator-side via a context-injected `RefusalLogger` sink (wired in the fan-out tool loop), so an operator can see when a read-only agent attempts `run_tests`/`run_script` — previously the refusal reached only the model

*Resolved from the unmerged td/2026-06-26 run, rebased onto the Epic 11.2 exec-registration boundary*

## [11.2.0] - 2026-06-26

### Changed

- Closed the registration-side gap in the structural exec boundary: the public `Dispatcher.RegisterTool` API now rejects execution-verb tool names (run/exec/eval/shell), so a code-executing handler can no longer be registered ungated — Epic 11.1 had hardened only the dispatch-time gate
- Routed `EnableExecution` through a single trusted `registerExec` path that atomically co-sets each exec handler with its `execTools` gate under one lock, superseding the prior gate-first ordering with strict atomicity (no fail-open window)

### Added

- Invariant test asserting every execution tool is gated with no orphan gates, plus a behavioral test that an exec-named handler offered to the public API is refused, never silently ungated

*Shipped via /execute-epic (epic 11.2)*

## [11.1.0] - 2026-06-26

### Changed

- Made the read-only tool boundary structural at the dispatch layer: `Dispatcher.Execute` now refuses `run_tests`/`run_script` from any agent not granted exec eligibility, independent of how callers wire `EnableExecution`
- Threaded per-agent exec eligibility through the fan-out tool loop and the verify determinism re-run, default-deny (fail-closed)
- Closed a fail-open window in `EnableExecution` by marking exec tools gated before registering their handlers

*Shipped via /execute-epic (epic 11.1)*

## [Technical Debt] - 2026-06-26

### Fixed

- Redacted `evidence_exec` output at the stamp site and wired the `Redactor` at all verify entry points
- Gated `evidence_exec` on deterministic two-run reproduction before surfacing execution evidence
- Parsed skeptic exec results and stamped `evidence_exec` into findings via repro write-back
- Clamped `run_script` per-call timeout to `execTimeout` and rejected flag-like `run_tests` targets
- Capped `run_script` content size to prevent unbounded input
- Validated sandbox `Memory`/`CPUs` fields in registry loading
- Validated memory/CPU caps against host resources via `docker info` in Preflight
- Named each container and ran `docker kill` on timeout to reclaim caps and avoid orphaned containers
- Lazily initialized the DockerBackend concurrency semaphore so struct literals cannot fail open
- Gated the `Reproduced` badge on a confirmed verdict and non-zero exit code
- Reordered `resolveExec` after `LoadReviewConfig` and accepted the loaded `ProjectConfig`

## [11.0.0] - 2026-06-25

### Added

- Opt-in `--exec` execution-reproduction mode (`atcr verify --exec`, `atcr review --verify --exec`): lets adversarial skeptics reproduce findings by running tests/scripts inside a container-isolated sandbox. Off by default; refuses to run without a configured `[sandbox]` block in `.atcr/config.yaml` that passes a preflight check
- `internal/sandbox`: a pluggable, Docker-backed executor that runs every command in an ephemeral container with no network, a read-only snapshot mounted at `/work`, all Linux capabilities dropped, `no-new-privileges`, a non-root user, memory/CPU/PID caps, a writable `/scratch` tmpfs, a per-run timeout, and a bound on concurrent container spawns
- `run_tests` / `run_script` tools, offered only to execution-enabled agents (the read-only tool set is unchanged for every other agent)
- `evidence_exec` block on findings (`command`, `exit_code`, `output_excerpt`) and a "Reproduced" badge in `report.md`; a reproduced finding is `VERIFIED` by the existing confidence/gate rules. Determinism is enforced by a two-run rule so flaky tests cannot poison evidence
- `docs/execution.md` documenting the opt-in flow, sandbox guarantees, and security posture

### Notes

- This release ships the execution building blocks (sandbox, tools, `--exec` gate, `evidence_exec` data model, two-run determinism, badge renderer), all tested. The final wiring that automatically stamps `evidence_exec` from a live skeptic reproduction is gated behind the Epic 11.0 security review, since it executes untrusted, model-authored code

*Shipped via /execute-epic (epic 11.0)*

## [Technical Debt] - 2026-06-25

### Fixed

- Hardened benchmark checkpoint resume against nil rosters, duplicate/out-of-range/empty case IDs, and oversized reads; integrity validation now fails closed on mismatch
- Made resumed benchmark runs replay scored cases with zero additional LLM cost and produce byte-identical `RunResult` output compared to an uninterrupted run
- Guarded checkpoint case-id drift with a dedicated sentinel and ensured no LLM calls are spent before the drift guard
- Included persona and usage/latency details in the checkpoint roster signature so replay reproduces the original run conditions
- Switched checkpoint serialization to compact JSON and made saves durable via atomic temp-file + rename
- Fixed silent resume paths in `cmd/atcr/benchmark_run.go` and documented the no-shared-checkpoint-path constraint
- Clarified `Score.Expected` non-empty requirement, `GeneratedAt` wall-clock behavior, and the accepted O(n²) checkpoint I/O trade-off

## [10.3.0] - 2026-06-25

### Added

- `atcr benchmark run --checkpoint <path>`: opt-in run checkpointing — each case's scored outcome is durably recorded (atomic temp-file + rename) before the next case begins, so a transient mid-suite failure no longer forfeits the completed, already-paid-for work of earlier cases
- Re-running the same suite with the same `--checkpoint` resumes from the first unscored case: already-scored cases are replayed from the checkpoint with zero additional LLM cost, and the resumed run produces a byte-identical run-result to an uninterrupted one
- Resume is guarded by suite identity (reproducibility hash + suite + version) and reviewer-roster identity (each agent and its configured model); any drift fails closed rather than silently mixing inconsistent work into a new run

*Shipped via /execute-epic (epic 10.3)*

## [10.2.0] - 2026-06-25

### Added

- `atcr benchmark run --suite-path <dir> [--out <file>]`: executes a benchmark suite through the review pipeline (via the diff-file ingestion path), scores each reviewer's findings against every case's planted-defect `expected_categories`, and writes the suite-tagged `benchmark.RunResult` that `atcr benchmark export` consumes — completing the `run → export` loop
- Benchmark scorer folds per-reviewer category recall into the shared public reviewer schema's `corroboration_rate` (with `findings_raised_avg` for volume), re-scrubbing each record via `scorecard.ScrubPublicRecord`; `generated_at` is injectable so two runs over the same suite and transcript are byte-identical

### Fixed

- Corrected the malformed hunk header in the `suite-valid` fixture's `case-02.diff` so it parses as a valid unified diff

*Shipped via /execute-epic (epic 10.2)*

## [Technical Debt] - 2026-06-25

### Fixed

- Trailing empty lines and no-newline markers no longer abort diff section parsing
- Inflated loose hunk counts are now rejected instead of silently swallowing the next file
- Diff ingestion hot path uses IndexByte walking and pre-sized builders to cut allocations
- Traversal and absolute paths in diff content are rejected as unsafe
- Spaced git-header paths now parse correctly via symmetric midpoint detection
- Diff-file paths that symlink outside the working tree are rejected
- `--no-prefix` binary git headers recover the symmetric path correctly
- Roster and mutual-exclusion checks hoisted into shared `validateReviewRequest`; `readCapped` extracted to pin TOCTOU +1/recheck invariant
- Combined/merge diffs rejected with a clear unsupported-format diagnostic; partial truncation at `PrepareReviewFromDiff` now logged
- `PrepareReviewFromDiff` error messages use parameter names; removed unused `t.Setenv` in test

## [10.1.0] - 2026-06-25

### Added

- Diff-file ingestion path: the review pipeline can now build a payload from a standalone unified diff — both loose `--- `/`+++ `/`@@ ` diffs and full `git diff` patches — instead of only a git `base..head` range, via `payload.BuildEntriesFromDiff` / `BuildEntriesFromDiffFile` and the exported `fanout.PrepareReviewFromDiff`
- Diff ingestion preserves byte-exact round-trip parity with the source diff, enforces a size cap, and rejects path traversal on the file-path variant; the resulting `PreparedReview` is accepted unchanged by `ExecuteReview`, with every reviewer seeing the ingested diff regardless of its configured payload mode

*Shipped via /execute-epic (epic 10.1)*

## [Technical Debt] - 2026-06-25

### Fixed

- Hardened benchmark manifest `Load` against symlink traversal and non-regular diff files
- Rejected empty and duplicate `expected_categories` during suite validation
- Blocked dot-path components in `isSafeRelPath`
- Capped reproducibility-hash diff reads and streamed hashing via `io.Copy`
- Eliminated redundant manifest loads in reproducibility hashing
- Added overflow-safe even-count median computation and documented integer-floor p50 behavior
- Fixed `--output` help text in the leaderboard command
- Hardened `scrubField` backstop against glued FHS roots, UNC paths, no-TLD emails, and `sk_`/`AIza` key prefixes
- Re-scrubbed reviewer PII in `BuildSubmission` via the public-record scrubber
- Omitted `survived_skeptic_rate` when no verification verdicts or stored rates support it
- Clamped non-finite floats (NaN/Inf) in `clampNonNegF` and `clampRate`
- Documented safe-to-ignore error paths for hash writes and `GetString`
- Documented the benchmark privacy model's export-time re-scrub backstop
- Added suite-field validation and documented the `GetString` convention
- Used `rr.GeneratedAt` for `submitted_at` in benchmark exports
- Closed clarified technical-debt items (5 via Q1-Q5, 9 post-clarification, 12 from model-eval review)
- Added regression coverage for by-design merge of distinct identities that scrub equal

## [10.0.0] - 2026-06-24

### Added

- `atcr benchmark` command with `verify` (validate a suite manifest + print its reproducibility hash) and `export` (emit a suite-tagged public submission, distinct from the production export) subcommands
- `internal/benchmark` package: versioned suite-manifest contract (`Load`/`Validate` with a path-traversal guard), a deterministic content-based reproducibility hash, the run-result input contract, and the suite-tagged submission envelope
- `internal/version` package supplying `atcr_version` for public submissions (ldflags-overridable; `0.0.0` in dev builds)
- `docs/benchmark.md` benchmark-suite guide (suite contract, verify, export, run-result contract, privacy model)

### Changed

- Reshaped the public leaderboard submission schema (`atcr leaderboard --export`) to the Epic 10.0 spec: envelope `{submission_schema, atcr_version, submitted_at, reviewers[]}`; per-reviewer `{model, persona, runs, findings_raised_avg, corroboration_rate, survived_skeptic_rate, cost_per_corroborated_finding_usd, latency_p50_ms}`
- Shrank the public submission allowlist: dropped the `filters` envelope, token counts, `role`, `index`, and the corroborated/solo/verified/refuted internals; `cost_per_corroborated_finding_usd` and `latency_p50_ms` are now real metrics (cost÷corroborated; true median) rather than the prior total/mean
- `survived_skeptic_rate` is now omitted entirely when no verification ran (distinguishable from a genuine `0.0`)
- Decoupled the public submission schema version (`submission_schema`) from the on-disk store schema version (`schema_version`)
- Updated `docs/scorecard.md` to document the new submission schema and privacy model

*Note: `atcr benchmark run` (live execution + scoring) and the curated standard-v1 suite content are deferred to Epic 10.1 and the external benchmark-suite repo.*

*Shipped via /execute-epic (epic 10.0)*

## [9.0.0] - 2026-06-24

### Added

- `internal/personas` lifecycle package with install, upgrade, list, search, score, and bundle support
- `atcr personas` CLI command with 6 subcommands: install, upgrade, list, search, test, init
- Language-aware two-partition skeptic routing in the verification pipeline
- Domain bundle support with `bundle/` install delegation
- `list --scores` corroboration display for reviewer-aggregated persona scoring
- Embedded bonus personas: sentinel, tracer, idiomatic
- `AgentConfig.Language` field with extension normalization via `normalizeExt`
- Persona install and authoring documentation with registry cross-references

### Changed

- Score-map key normalized to lowercase in `SelectEligibleSkeptics` for case-insensitive routing
- Per-reviewer score aggregation with bundle member deduplication in personas list

### Fixed

- Guarded `AgentConfig.Persona` against control characters and over-length values
- Hardened persona install with atomic writes, restricted permissions (0o600/0o700), and symlink rejection at destination
- Added HTTP timeout and body-size cap to personas fetch client
- Validated bundle manifest members up front before processing
- Resolved NaN score ordering hazard in skeptic selection
- Guarded dotfile inputs in `SelectEligibleSkeptics` to prevent spurious language matches
- Stripped all leading dots in `NormalizeLanguageToken` for consistent canonicalization
- Fixed `isNewer` to treat mixed semver validity as up-to-date
- Surfaced `versionOf` unmarshal errors and aborted upgrade on corrupt YAML
- Guarded `Total==0` edge case in persona test command to warn instead of false-pass
- Fixed empty bundle argument and empty search keyword edge cases

*Shipped via /execute-sprint (sprint 9.0_persona_ecosystem)*

## [8.0.0] - 2026-06-23

Extracted `internal/reconcile` into a standalone nested Go module with its own `go.mod`, independent CI scorecard, and JSON adapter. All consumer packages now import types, severity helpers, and core reconcile logic from the library rather than from the top-level module.

### Added

- `internal/reconcile` nested Go module with `replace` directive, full type/IO split, and independent CI scorecard job
- JSON adapter for the reconcile library with input routing, validation, and edge-case handling (non-object/non-array rejection, required-field validation)
- `Verification.Valid()` method to enforce the verdict enum contract
- Length-prefix encoding for `AmbiguousID` fields to prevent NUL byte field-boundary aliasing

### Changed

- All consumer packages (`cmd/atcr`, `mcp`, `ghaction`, `report`, `debate`, `verify`, `registry`, `fanout`) now import types and severity helpers from `internal/reconcile`
- `AmbiguousHash` returns a sentinel error instead of panicking on marshal failure
- `sortMerged` uses a `Problem` tiebreak for a strict total order

### Fixed

- Source JSON wire names normalized to `lower_snake` via JSON struct tags
- `MaxEstMinutes` calculation corrected for empty and negative estimate groups
- `PerSourceCounts` populated in `sampleResult` for byte-stable golden fixture output
- `ConfidenceForVerdict` normalizes non-canonical prior confidence pass-through
- `Decode` validates required finding fields before accepting input
- Escaped source names in the markdown Summary section in `emit.go`
- `firstNonSpace` replaced with `bytes.TrimLeft` to correctly handle empty input
- Pre-push hook extended to include the reconcile module test suite
- CI reconcile module job granted `permissions: contents: read`

*Shipped via /execute-sprint (sprint 8.0_reconciler_library)*

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
