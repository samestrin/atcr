# Install Script Conventions

`[IMPORTANT]`

## Overview

`install.sh` is the one confirmed net-new distribution artifact for this epic (AC4). No install script exists anywhere in the repo today; the only documented install path is `go install github.com/samestrin/atcr/cmd/atcr@latest` (README.md Quickstart, docs/skill-usage.md). The script's sole purpose is to give external developers a single copy-paste command that lands the `atcr` binary on their `PATH`.

Per prior epic decisions, this script must **not** take on binary packaging, release automation, version pinning, or goreleaser logic — that work is explicitly descoped to Epic 21.0 (Release & Packaging Automation).

## Requirements

- Wrap the existing `go install github.com/samestrin/atcr/cmd/atcr@latest` path.
- Require only a working Go toolchain as a prerequisite.
- Place (or confirm placement of) the installed binary on the user's `PATH`.
- Exit non-zero with a clear message if `go install` fails.
- No release-channel selection, no checksum verification, no OS/arch detection beyond what `go install` already handles.

## Style Precedent

The only shell script shipped in the repo is `examples/ci-gate.sh`. Use it as a style reference for:

- Shebang (`#!/usr/bin/env bash`).
- `set -euo pipefail` error handling.
- Descriptive comments.
- Clear success/failure messages to stdout/stderr.

## Installation Targets

| Target | Path | Notes |
|---|---|---|
| Primary | `$(go env GOPATH)/bin/atcr` | Default `go install` destination. |
| PATH check | Any directory in `$PATH` | Script should warn if `$(go env GOPATH)/bin` is not on `$PATH`, not silently succeed. |

## Quick Reference

| Concern | Convention | Source |
|---|---|---|
| Install command | `go install github.com/samestrin/atcr/cmd/atcr@latest` | README.md Quickstart, docs/skill-usage.md |
| Error handling | `set -euo pipefail` | examples/ci-gate.sh style precedent |
| Scope guard | No release automation / pinning / packaging | original-requirements.md Out of Scope; codebase-discovery.json architecture_recommendations |
| Self-test after install | `atcr version` or `atcr doctor` | original-requirements.md AC4; docs/skill-usage.md |

## Related Documentation

- [../original-requirements.md](../original-requirements.md) — AC4 and Out of Scope sections that define what `install.sh` must and must not do.
- [../../../../../docs/skill-usage.md](../../../../../docs/skill-usage.md) — existing standalone skill install/usage guide that already satisfies AC2.
- [../../../../../examples/ci-gate.sh](../../../../../examples/ci-gate.sh) — nearest shell style precedent in the repo.
- [../codebase-discovery.json](../codebase-discovery.json) — confirms no install script exists and recommends a minimal `go install` wrapper.
