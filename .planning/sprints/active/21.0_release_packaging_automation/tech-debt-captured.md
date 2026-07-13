# Tech Debt Captured — Sprint 21.0 Release & Packaging Automation

Deferred items surfaced during `/execute-sprint`. Read by `/execute-code-review` (pre-seeded into the adversarial TD stream, tagged `SOURCE=execute-sprint`).

## TD-001 — Contract table omits fully-qualified ldflags target (MEDIUM)
**Origin:** Phase 1, task 1.3 gate review, 2026-07-12
**File:** docs/release-process.md:67
**Issue:** The build-time stamping-contract table names the internal target as `internal/version.Version`, but a goreleaser `-ldflags -X` entry requires the fully-qualified import path `github.com/samestrin/atcr/internal/version.Version`. `main.version` (line 66) is directly usable, so the table is asymmetric. A wrong `-X` path fails silently (build succeeds, var stays at the `0.0.0` placeholder).
**Why accepted:** Not a live risk — Task 02's own spec (sprint-plan.md 2.1 step 1) already carries the correct fully-qualified `-X github.com/samestrin/atcr/internal/version.Version={{.Version}}` path verbatim, so Phase 2 implements from the authoritative instruction, not this table. This is a doc-clarity/self-consistency improvement only.
**Fix in:** Phase 4 (Task 04 doc pass) or a follow-up docs edit — spell out the exact `-X` target `github.com/samestrin/atcr/internal/version.Version` in the table's Variable cell so the doc is self-contained.

## TD-002 — Contract table hard-codes drift-prone line numbers (LOW)
**Origin:** Phase 1, task 1.3 gate review, 2026-07-12
**File:** docs/release-process.md:66
**Issue:** The stamping-contract table anchors to source line numbers (`cmd/atcr/version.go:14`, `internal/version/version.go:16`) that are correct today but will silently drift if either file changes before a later reader relies on them.
**Why accepted:** Low impact — both files also link to the source and the symbols are stable; line drift degrades precision but not correctness.
**Fix in:** Phase 4 (Task 04 doc pass) or a follow-up docs edit — anchor to symbol names (`var version`, `var Version`) alongside or instead of the brittle line numbers.

## TD-003 — GoReleaser build omits `-trimpath` (MEDIUM)
**Origin:** Phase 2, task 2.1.A adversarial review, 2026-07-12
**File:** .goreleaser.yaml:29
**Issue:** The `builds:` block has no `flags: [-trimpath]`. Without it, Go embeds the absolute build path (e.g. `/Users/samestrin/...` locally, or the CI checkout/GOPATH path) into the shipped binary. This both leaks build-machine paths into public release artifacts and prevents same-tag builds from being byte-identical across build hosts, so omitting the date stamp alone does not deliver full reproducibility.
**Why accepted:** Below the sprint's CRITICAL/HIGH inline-fix bar. The leak is low-severity (paths, not secrets), the snapshot build succeeds, and the config comment was corrected to no longer overclaim byte-reproducibility. `-trimpath` is a safe one-line follow-up.
**Fix in:** A follow-up build/config edit — add `flags: [-trimpath]` to the `builds:` block and re-run `goreleaser release --snapshot --clean` to confirm.

## TD-004 — Release archives not timestamp-pinned via `mod_timestamp` (LOW)
**Origin:** Phase 2, task 2.1.A adversarial review, 2026-07-12
**File:** .goreleaser.yaml:29
**Issue:** No `mod_timestamp` is set on the build, so release archives (tar.gz/zip) and their checksums are stamped with wall-clock mtimes at package time. Even when the binaries match, the archives will differ between builds of the same tag.
**Why accepted:** Low impact — affects archive/checksum reproducibility only, not the binaries themselves; below the CRITICAL/HIGH inline-fix bar.
**Fix in:** A follow-up config edit — set `mod_timestamp: "{{ .CommitTimestamp }}"` on the `builds:` block to pin artifact timestamps to the commit.

## TD-005 — Date-stamp comment overstates what omitting ldflags removes (LOW)
**Origin:** Phase 2, task 2.3 gate review, 2026-07-12
**File:** .goreleaser.yaml:15
**Issue:** The comment says supplying an explicit ldflags list "REPLACES goreleaser's defaults (which include `-X main.date={{.Date}}`), removing wall-clock build time from the binary." But `cmd/atcr` declares no `main.date` var, so goreleaser's default `-X main.date` was always a silent linker no-op — no wall-clock time was ever embedded via that symbol. The config's actual behavior (no date stamped) is correct; only the rationale's framing overclaims what was removed.
**Why accepted:** LOW, no functional or phase-exit impact — the binary behavior is exactly as intended. Deferred per the gate MEDIUM/LOW action rule.
**Fix in:** A follow-up docs/config edit — reword the comment to note `main.date` is not a defined symbol, so the effect is simply that no build date is stamped (not the removal of an active stamp).

## TD-006 — Release-workflow actions pinned to mutable tags, not commit SHAs (LOW)
**Origin:** Phase 3, task 3.1.A adversarial review, 2026-07-12
**File:** .github/workflows/release.yml:53
**Issue:** Third-party actions are pinned to mutable major-version tags (`actions/checkout@v4`, `actions/setup-go@v5`, `goreleaser/goreleaser-action@v6`) and the goreleaser binary to a floating range (`version: '~> v2'`). In a `contents: write` workflow a moved tag or new v2.x could inject release-time code with write access. OpenSSF supply-chain hardening recommends full commit-SHA pins for write-scoped release workflows.
**Why accepted:** Below the CRITICAL/HIGH inline-fix bar and NOT a regression — this matches the pin form already used verbatim in `ci.yml` and `reconcile-module.yml`; hardening this workflow in isolation would diverge from the repo's established convention. Better addressed as a repo-wide action-pinning pass.
**Fix in:** A follow-up security/CI pass — pin each `uses:` to a full commit SHA (with a `# vX` trailing comment) across all workflows, and optionally pin the goreleaser binary to an exact `version: v2.x.y`.

## TD-007 — Release workflow uses `cancel-in-progress: true` on a publishing job (LOW)
**Origin:** Phase 3, task 3.1.A adversarial review, 2026-07-12
**File:** .github/workflows/release.yml:29
**Issue:** `concurrency.cancel-in-progress: true` on a publishing workflow means re-pushing the same tag ref cancels an in-flight run mid-publish, which can leave a partially-created public GitHub Release or partially-uploaded assets. Blast radius is limited to same-tag re-pushes (group keyed per `github.ref`), so different tags never collide.
**Why accepted:** Below the CRITICAL/HIGH inline-fix bar. `true` was chosen for consistency with `ci.yml:12-14` / `reconcile-module.yml:21-23`; same-tag re-push during an active publish is an unlikely maintainer action, and the first-cut process is a deliberate single push.
**Fix in:** A follow-up config edit — set `cancel-in-progress: false` on the release workflow's concurrency block so an in-progress publish is never interrupted.

## TD-008 — Trigger glob `v*` broader than the documented bare-vX.Y.Z convention (LOW)
**Origin:** Phase 3, task 3.1.A adversarial review, 2026-07-12
**File:** .github/workflows/release.yml:21
**Issue:** The trigger glob `v*` matches any v-prefixed tag (e.g. `vtest`, `v2-beta`), each spinning up a `contents: write` job on the self-hosted gauntlet runner, whereas the inline comments document a bare `vX.Y.Z` convention. Still provably disjoint from `reconcile/v*`, and goreleaser rejects non-semver tags, so practical exposure is a wasted runner job rather than a bad release.
**Why accepted:** Below the CRITICAL/HIGH inline-fix bar and intentional for now — `v*` mirrors the loose glob style of `reconcile-module.yml`'s `reconcile/v*`, and the goreleaser semver guard backstops accidental non-release tags. Tightening is a low-risk refinement, not a correctness fix.
**Fix in:** A follow-up config edit — narrow the filter to the actual convention, e.g. `v[0-9]*.[0-9]*.[0-9]*`, and confirm the intended tags still fire.

## TD-009 — Snapshot dry-run wording implies it stamps the release number (MEDIUM)
**Origin:** Phase 4, task 4.3 gate review, 2026-07-12
**File:** docs/release-process.md:110
**Issue:** The "Cutting a Release" dry-run step says to run `goreleaser release --snapshot --clean` and "confirm ... both -X ldflags targets resolve and agree on the numeric X.Y.Z". But in snapshot mode with no tag (the repo is forward-only, zero tags), goreleaser derives a pseudo-version from `git describe`, so it stamps a 0.0.0-class / previous-tag value — never the `vX.Y.Z` about to be released. Read against the earlier contract example ("a tag of v21.0.0 yields main.version = v21.0.0"), a first-time maintainer could expect the snapshot to show the release number and be confused at the very gate guarding a public, hard-to-retract Release. The mechanism is correct (both targets agree with each other, v-prefixed vs v-stripped); only the wording is imprecise.
**Why accepted:** Below the sprint's CRITICAL/HIGH gate inline-fix bar and not a correctness defect — the stamping mechanism is sound and sprint-plan.md's Phase 2 DoD already notes the snapshot "synthesizes .Version, so numeric agreement is verified via the mapping mechanism — a real tag yields identical X.Y.Z". Doc-clarity refinement only.
**Fix in:** A follow-up docs edit — reword step 2 to say the snapshot verifies the stamping *mechanism* (both vars resolve and agree with each other, v-prefix vs v-stripped), not the exact release number; optionally note only a real tag build stamps the actual `vX.Y.Z`.

## TD-010 — Cutting-a-Release procedure omits the local goreleaser install prerequisite (LOW)
**Origin:** Phase 4, task 4.3 gate review, 2026-07-12
**File:** docs/release-process.md:106
**Issue:** Step 2 of "Cutting a Release" invokes the `goreleaser` CLI for the local dry run, but the procedure never states goreleaser must be installed locally. The CI pipeline uses `goreleaser/goreleaser-action`, so the binary is not otherwise guaranteed on a maintainer's machine; a first-time maintainer following the doc verbatim could hit a command-not-found.
**Why accepted:** LOW, below the gate inline-fix bar — a missing local tool surfaces immediately and unambiguously (command not found), not a silent or irreversible failure.
**Fix in:** A follow-up docs edit — add a one-line prerequisite noting goreleaser must be installed locally (e.g. `go install github.com/goreleaser/goreleaser/v2@latest` or `brew install goreleaser`), pinned to the v2 line to match `.goreleaser.yaml`'s `version: 2` and `release.yml`'s `~> v2`.
