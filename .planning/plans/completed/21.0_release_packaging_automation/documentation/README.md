# Plan Documentation References

**Created:** July 12, 2026 02:06:36PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- [version-tagging-strategy.md](version-tagging-strategy.md) `[CRITICAL]` — the two independent version variables (`internal/version.Version`, `cmd/atcr`'s local `version`) and the bare `vX.Y.Z` tag convention that formalizes `CHANGELOG.md`'s existing epic-number-as-semver history, disjoint from Epic 8.0's `reconcile/vX.Y.Z` namespace.
- [goreleaser-configuration.md](goreleaser-configuration.md) `[CRITICAL]` — how to configure `.goreleaser.yaml`'s Go builder: dual `-X` ldflags entries stamping both version variables from the same tag, and the `GOOS`/`GOARCH` build matrix.
- [ci-workflow-reuse.md](ci-workflow-reuse.md) `[IMPORTANT]` — the `based_on:` header-comment convention `.github/workflows/reconcile-module.yml` established for reusing `ci.yml`'s runner/checkout/setup-go/lint steps verbatim, and the full workflow-inventory tag-collision check the new `release.yml` must pass.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `.planning/specifications/packages/goreleaser.md`
- **Codebase Discovery:** `.planning/plans/active/21.0_release_packaging_automation/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`
- **Live code citations:** `internal/version/version.go`, `cmd/atcr/version.go`, `.github/workflows/reconcile-module.yml`, `.github/workflows/ci.yml`, `.github/workflows/hermes-auto-merge.yml`, `.github/workflows/refresh-synthetic-manifest.yml`, `.golangci.yml`, `CHANGELOG.md`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
