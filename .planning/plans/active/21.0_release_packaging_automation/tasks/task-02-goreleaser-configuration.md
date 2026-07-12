# Task 02: GoReleaser Configuration with Dual Version Stamping

**Source:** Plan 21.0 – Debt Item #2
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement
`atcr` has no goreleaser configuration and no way to cross-compile, archive, or checksum release binaries from a tag. Worse, the repo has two independent version variables that a release build must stamp identically or the CLI and the leaderboard submission envelope will silently disagree about what version produced a given result:

1. `internal/version/version.go:14-16` — `var Version = "0.0.0"`, module-wide, read by the leaderboard submission envelope (Epic 10.0). Its own doc comment already documents the intended stamping convention: `-ldflags "-X github.com/samestrin/atcr/internal/version.Version=1.2.3"`.
2. `cmd/atcr/version.go:10-40` — a separate, package-local `var version = ""` in `main`, resolved by `atcrVersion()` (fallback chain: ldflags → `debug.ReadBuildInfo()` module version → VCS revision → `"dev"`). Stamped via `-X main.version=1.2.3`. This var does **not** read `internal/version.Version` — the two are fully independent.

Without a config that stamps both from the same tag value in the same build, AC2 and AC3 cannot be satisfied.

## Solution Overview
Author a `.goreleaser.yaml` at the repo root with a Go `builds:` block that: targets `./cmd/atcr` as the main package, includes **two** `-X` ldflags entries (one per version variable above, both templated from `{{.Version}}`), and relies on goreleaser's default `GOOS`/`GOARCH` matrix (`darwin, linux, windows` / `386, amd64, arm64`) since this repo has no stated need to deviate from it. Verify the config with a local, non-publishing `goreleaser release --snapshot --clean` dry run that confirms both ldflags targets land correctly in the resulting binaries before Task 3 wires the tag-triggered CI workflow around it.

## Technical Implementation
### Steps
1. Create `.goreleaser.yaml` at the repo root (`/Users/samestrin/Documents/GitHub/atcr/.goreleaser.yaml`) with a `builds:` block:
   - `main: ./cmd/atcr`
   - `binary: atcr`
   - `env: [CGO_ENABLED=0]`
   - `ldflags:` list containing both:
     - `-s -w -X github.com/samestrin/atcr/internal/version.Version={{.Version}} -X main.version={{.Version}}`
   - **`{{.Version}}` vs `{{.Tag}}` — a decision to make deliberately, not by default.** goreleaser's `{{.Version}}` strips the leading `v` from the tag (tag `v1.2.3` → `.Version` = `1.2.3`); `{{.Tag}}` keeps it (`v1.2.3`). The two Go vars' own doc comments encode *different* expectations: `internal/version.Version`'s comment (internal/version/version.go:7) shows the stamp as `1.2.3` (v-stripped), while `cmd/atcr`'s comment (cmd/atcr/version.go:11) shows `main.version=v1.2.3` (with `v`). Separately, `atcrVersion()`'s `debug.ReadBuildInfo()` fallback (cmd/atcr/version.go:25-28) returns the module version *with* the `v` prefix as Go records it — so a `go install github.com/samestrin/atcr/cmd/atcr@v1.2.3` build reports `v1.2.3`. Stamping both `-X` entries with `{{.Version}}` (as [documentation/goreleaser-configuration.md](../documentation/goreleaser-configuration.md)'s Quick Reference suggests) makes `atcr --version` print `1.2.3`, which diverges in the `v` prefix from a `go install`-built binary and from `cmd/atcr/version.go:11`'s own example. Pick one deliberately: (a) stamp `internal/version.Version` with `{{.Version}}` and `main.version` with `{{.Tag}}` so each matches its own doc comment and the CLI matches the `go install` path; or (b) stamp both with `{{.Version}}` and accept `atcr --version` = `1.2.3`. Either satisfies AC2; the Step 4 snapshot dry run must confirm the actual `atcr --version` output matches what Task 1's documented convention implies, and the two stamped values must still agree on the numeric `X.Y.Z` portion (the `v` prefix is the only allowed difference). If (a) is chosen, update [documentation/goreleaser-configuration.md](../documentation/goreleaser-configuration.md)'s Quick Reference table (which currently shows `{{.Version}}` for both) in a follow-up so the doc and config stay aligned.
   - Omit `goos`/`goarch` to accept the documented defaults (`darwin, linux, windows` / `386, amd64, arm64`), unless a local dry run in Step 3 surfaces a build failure on a specific `GOOS`/`GOARCH` combination (e.g. a platform-incompatible dependency), in which case exclude only that combination and note why in a config comment.
   - Consider replacing goreleaser's default `-X main.date={{.Date}}` stamp with `{{.CommitDate}}` (or omitting the date stamp) for reproducible artifacts — not a hard requirement, but call out the decision in a config comment either way so the choice is traceable.
2. Add an `archives:` block (name template, format) and a `checksum:` block using goreleaser's standard defaults — no repo-specific customization is required beyond what `builds:` needs.
3. Confirm the `goreleaser` CLI is available locally (`goreleaser --version`; install via `go install github.com/goreleaser/goreleaser/v2@latest` or `brew install goreleaser` if missing — it is a standalone CLI, not a `go.mod` dependency), then run `goreleaser release --snapshot --clean` from the repo root as a non-publishing dry run.
4. Inspect the dry-run output under `dist/`: extract or run one built binary (e.g. the `darwin_amd64` or host-matching build) and confirm `atcr version` / `atcr --version` reports the expected snapshot version string — and that its `v`-prefix matches the `{{.Version}}` vs `{{.Tag}}` decision made in Step 1. Then verify `internal/version.Version` was stamped to the same numeric value: it is not exposed by any CLI flag (read only by the leaderboard submission envelope), so confirm it via binary inspection, e.g. `strings <dist/.../atcr> | grep -F '<snapshot-version>'` (the snapshot version string should appear in the binary for each stamped var), or by building a throwaway `go run` snippet that imports `github.com/samestrin/atcr/internal/version` and prints `version.Version` with the same `-ldflags` goreleaser used. Both ldflags targets must agree on the numeric `X.Y.Z` portion.
5. Do not commit the `dist/` output directory; if goreleaser doesn't already ignore it, confirm `dist/` is covered by `.gitignore` and add it if missing.

## Files to Create/Modify
- `.goreleaser.yaml` – create; root-level goreleaser config with dual `-X` ldflags stamping `internal/version.Version` and `main.version` from the same tag.
- `.gitignore` – modify only if `dist/` (goreleaser's local output directory) is not already excluded.

## Documentation Links
- [GoReleaser Configuration](../documentation/goreleaser-configuration.md)
- [Version & Tagging Strategy](../documentation/version-tagging-strategy.md)

## Related Files (from codebase-discovery.json)
- `internal/version/version.go` — module-wide `Version` var, ldflags target 1.
- `cmd/atcr/version.go` — package-local `version` var and `atcrVersion()` resolution chain, ldflags target 2.
- `cmd/atcr/main.go` — `newRootCmd()` sets `Version: atcrVersion()` at line 136 (unchanged by this task; confirms no new Go code is needed beyond the ldflags — AC2 is a build-time wiring gap, not a code gap).
- `go.mod` — module path `github.com/samestrin/atcr`, confirms the ldflags package path for target 1.
- `.github/workflows/reconcile-module.yml` — precedent for this repo's only other release-shaped config; not directly reused here but confirms no conflicting `.goreleaser.yaml` or release tooling already exists.

## Success Criteria
- [ ] `.goreleaser.yaml` exists at the repo root and is valid goreleaser YAML.
- [ ] The `builds:` block's `ldflags` includes both `-X github.com/samestrin/atcr/internal/version.Version={{.Version}}` and `-X main.version={{.Version}}`.
- [ ] `goreleaser release --snapshot --clean` completes successfully against the current `main` branch with no tag pushed.
- [ ] A binary produced by the snapshot run reports the expected snapshot version via `atcr version` / `atcr --version`.
- [ ] `internal/version.Version` is confirmed stamped to the same snapshot version value as `main.version`, in the same build.
- [ ] Cross-platform matrix (`darwin, linux, windows` × `386, amd64, arm64`, minus any documented exclusion) builds without error.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- N/A — `.goreleaser.yaml` is a declarative config file, not Go code; no unit test applies.

**Integration Tests:**
- Local `goreleaser release --snapshot --clean` dry run (non-publishing) verifying:
  - The build completes for the full default `GOOS`/`GOARCH` matrix (or documented subset).
  - Both `-X` ldflags targets stamp correctly: `atcr version` / `atcr --version` on a built binary matches the snapshot version, and `internal/version.Version` (checked via a throwaway build/run or binary inspection) matches the same value.
  - Archives and checksums are produced under `dist/` without error.

**Test Files:**
- N/A — verified via local `goreleaser release --snapshot --clean` dry run, not a go test file.

## Risk Mitigation
- **Only one of the two version variables gets stamped, leaving the CLI and the leaderboard envelope reporting different versions from the same release.** Mitigated by explicitly enumerating both `-X` ldflags targets in `.goreleaser.yaml` and verifying both in the local snapshot dry run (Step 4) before Task 3 wires the real tag-triggered workflow.
- **Non-reproducible builds from the default `-X main.date={{.Date}}` stamp.** Considered in Step 1; either switch to `{{.CommitDate}}` or omit the date stamp, with the decision documented inline in the config.
- **`goreleaser` CLI unavailable in the local dev environment.** Documented install path in Step 3 (`go install` or `brew`); this is a one-time local verification step and does not block the config file itself from being authored and committed.

## Dependencies
- Task 01 — Versioning & Tagging Strategy (establishes the bare `vX.Y.Z` tag convention this config's `{{.Version}}` template values derive from at release time; not a hard code dependency, but the documented convention this config assumes).

## Definition of Done
- `.goreleaser.yaml` committed at the repo root with the dual-ldflags `builds:` block, `archives:`, and `checksum:` sections.
- Local `goreleaser release --snapshot --clean` dry run passes, producing cross-platform binaries under `dist/` (git-ignored).
- Both `internal/version.Version` and `cmd/atcr`'s `main.version` confirmed stamped to the same value from a single snapshot build.
- No changes required to `internal/version/version.go`, `cmd/atcr/version.go`, or `cmd/atcr/main.go` — AC2 is satisfied purely by build-time ldflags wiring, per plan.md's Technical Planning Notes.
