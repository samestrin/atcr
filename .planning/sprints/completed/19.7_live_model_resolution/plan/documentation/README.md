# Plan Documentation References

**Created:** July 08, 2026 05:45:39PM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- [openrouter-catalog-api.md](openrouter-catalog-api.md) — **[CRITICAL]** OpenRouter Catalog & Completions API: model schema fields, the missing stability flag, `expiration_date` deprecation signal, and `~`-prefixed alias resolution behavior (bears directly on AC1/AC2/AC3/AC5).
- [existing-resolver-patterns.md](existing-resolver-patterns.md) — **[CRITICAL]** Existing Codebase Patterns to Reuse: the `fetch()` retry template, the `Upgrade()`/`isNewer()` extension seam, the additive-schema convention, the `ResolvePersona` boundary rule, command-registration points, lock persistence, and the AC7 two-call-site drift risk.

- [catalog-snapshot-fixture.md](catalog-snapshot-fixture.md) — **[IMPORTANT]** Catalog Snapshot Fixture Discipline: checked-in `testdata/catalog_snapshot.json`, zero-live-network CI testing via `httptest`, and the on-demand refresh command (AC8).
- [models-check-command.md](models-check-command.md) — **[IMPORTANT]** `atcr models check` Command Design: drift/deprecation/missing-slug reporting, exit codes, `--json` output shape, and registration next to the personas command family (AC5).
- [semver-version-comparison.md](semver-version-comparison.md) — **[IMPORTANT]** Semantic Version Comparison (`golang.org/x/mod/semver`): the existing `isNewer` API and how AC6's major/minor gate builds on top of it via `semver.Major`.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `.planning/specifications/packages/utilities.md`; https://openrouter.ai/docs/guides/overview/models; https://openrouter.ai/docs/quickstart#using-the-openrouter-api
- **Codebase Discovery:** `.planning/plans/active/19.7_live_model_resolution/codebase-discovery.json`
- **Specifications:** `.planning/specifications/`

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
