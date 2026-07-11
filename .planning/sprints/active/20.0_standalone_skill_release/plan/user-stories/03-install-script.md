# User Story 3: Install Script

**Plan:** [20.0: Standalone ATCR Skill Distribution](../plan.md)

## User Story

**As an** external OSS developer evaluating atcr for the first time
**I want** a single copy-paste shell command that installs the `atcr` binary onto my `PATH`
**So that** I can start running multi-agent code reviews on my own repository without manually working out the correct `go install` invocation or checking my `PATH` configuration

## Story Context

- **Background:** `install.sh` is the one confirmed net-new distribution artifact in the entire repo (per plan.md Feature Analysis Summary and codebase-discovery.json) — no install script exists anywhere today. The only documented install path is `go install github.com/samestrin/atcr/cmd/atcr@latest`, referenced in README.md's Quickstart section and `docs/skill-usage.md`. This story wraps that existing path in a thin, scriptable convenience layer; it does not introduce a new installation mechanism.
- **Assumptions:** The target user already has a working Go toolchain installed (Go 1.24+, per plan.md Framework/Technology) — the script does not install or bootstrap Go itself. The default `go install` destination (`$(go env GOPATH)/bin`) is assumed to be the correct install location; no custom `--prefix`-style destination override is in scope.
- **Constraints:** Per two prior epic-level decisions (referenced in original-requirements.md Out of Scope and plan.md Risk Mitigation), `install.sh` must NOT perform release-channel selection, checksum verification, version pinning, OS/arch detection beyond what `go install` already handles, or any other release-automation behavior (that work is tracked separately in Epic 21.0). It must also not duplicate `atcr doctor`'s self-test role — the script only installs the binary and points the user at `atcr doctor`/`atcr version` for verification, per epic 1.2's prior naming decision that a new `atcr selftest`-style command has no precedent advantage. `examples/ci-gate.sh` is the only shell script currently shipped in the repo and is the mandatory style precedent (shebang, `set -euo pipefail`, comment density, message style).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** A new `install.sh` script exists at the repo root, wraps `go install github.com/samestrin/atcr/cmd/atcr@latest`, and is linked from README.md's Quickstart section alongside the existing `go install` instructions.
- **Measurable:** Running `./install.sh` (or `curl ... | bash` per the README link) on a machine with a working Go toolchain results in an `atcr` binary present at `$(go env GOPATH)/bin/atcr`; running it without Go on `PATH` exits non-zero with a clear, actionable stderr message; the script warns (without failing) when `$(go env GOPATH)/bin` is absent from `$PATH`.
- **Achievable:** The script is a thin wrapper (target under ~40 lines) around a single `go install` invocation plus prerequisite/PATH checks, matching the scope and complexity of the existing `examples/ci-gate.sh` precedent.
- **Relevant:** Directly satisfies AC4's install-script requirement (the one confirmed net-new distribution gap) without re-implementing the self-test or quick-start functionality that `atcr doctor`, `atcr quickstart`, and `docs/skill-usage.md` already provide.
- **Time-bound:** Completed within this sprint's Story 3 implementation slot, verified before `/create-acceptance-criteria` generates detailed Gherkin scenarios for this story.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-install-script-core-install.md) | Install Script Core Installation Flow | Integration |
| [03-02](../acceptance-criteria/03-02-install-script-prereq-and-path-checks.md) | Install Script Prerequisite and PATH Checks | Integration |
| [03-03](../acceptance-criteria/03-03-readme-quickstart-documentation.md) | README Quickstart Documentation for install.sh | E2E |

## Original Criteria Overview

1. `install.sh` exists at the repo root, is executable, and successfully installs `atcr` via `go install github.com/samestrin/atcr/cmd/atcr@latest` when a Go toolchain is present.
2. The script exits non-zero with a clear stderr message when the Go toolchain is missing or `go install` fails, and warns (without failing) when `$(go env GOPATH)/bin` is not on `$PATH`, suggesting `atcr doctor`/`atcr version` as a post-install check.
3. README.md's Quickstart section documents `install.sh` as an alternative/companion to the existing manual `go install` command, without removing or contradicting that existing instruction.

## Technical Considerations

- **Implementation Notes:** Follow `examples/ci-gate.sh` line-for-line style: `#!/usr/bin/env bash` shebang, `set -euo pipefail`, short descriptive header comment block, and explicit success/failure messages written to stdout (success) and stderr (failure). Structure: (1) check `go` is available via `command -v go`, exiting non-zero with a clear message if not; (2) run `go install github.com/samestrin/atcr/cmd/atcr@latest`, exiting non-zero with the underlying error surfaced if it fails; (3) compute `$(go env GOPATH)/bin` and check it against `$PATH`, printing a warning (not a failure) if absent; (4) print a success message suggesting `atcr doctor` or `atcr version` as the next step.
- **Integration Points:** README.md Quickstart section (add a link/mention alongside the existing `go install` command, per plan.md Technical Planning Notes); no changes to `cmd/atcr` or any Go source — this is a documentation- and shell-adjacent deliverable only.
- **Data Requirements:** None — no config, schema, or persisted state involved.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Scope creep into release-automation territory (checksum verification, version pinning, OS/arch detection) | Medium | Explicit constraint from original-requirements.md Out of Scope and plan.md Risk Mitigation carried into this story; AC review should reject any addition beyond the thin `go install` wrapper. |
| Script silently succeeds when the installed binary isn't actually reachable (PATH misconfiguration), leaving the user confused | Medium | Explicit `$PATH` check against `$(go env GOPATH)/bin` with a warning message, per Installation Targets table in `documentation/install-script-conventions.md`. |
| Duplicating `atcr doctor`'s self-test responsibility inside `install.sh` | Low | Story explicitly scopes the script to installation only; it suggests running `atcr doctor`/`atcr version` rather than reimplementing any verification logic. |

---

**Created:** July 11, 2026 01:48:34PM
**Status:** Draft - Awaiting Acceptance Criteria
