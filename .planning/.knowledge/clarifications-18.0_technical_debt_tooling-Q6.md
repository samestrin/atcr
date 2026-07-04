---
id: mem-2026-07-03-dc0310
question: "Should new automation be wired into the tracked .githooks/, or shipped as a --check gate plus documented example?"
created: 2026-07-03
last_retrieved: ""
sprints: []
files: []
tags: [clarifications, epic-18.0_technical_debt_tooling, ci-cd, git-hooks, convention]
retrievals: 0
status: active
type: clarifications
---

# Should new automation be wired into the tracked .githooks/, 

## Decision

Do not add non-CI-mirroring steps to the tracked .githooks/. Those hooks are shared/opt-in (enabled per-clone via git config core.hooksPath .githooks), run for every contributor on every commit, and are deliberately scoped to fast CI-mirroring gates (gofmt/vet/build in pre-commit, full suite in pre-push). For new "can be hooked into a hook or CI" capability, build a deterministic output plus a `--check` mode that exits non-zero on drift, and DOCUMENT an opt-in snippet — do not edit the live hooks. Precedents: the gofmt -l staleness gate that exits 1 in CI, and the refresh-synthetic-manifest workflow that detects generated-file drift with git diff --quiet and opens a PR (a self-contained workflow kept out of the commit hooks).

Evidence:
- .githooks/README.md:6-13 (shared/opt-in, tracked), .githooks/pre-commit:2-6 & pre-push:5-7 (CI-mirroring scope)
- .github/workflows/ci.yml:31-37 (gofmt -l + exit 1 drift gate)
- .github/workflows/refresh-synthetic-manifest.yml:38-75 (out-of-band generated-file drift workflow)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A
