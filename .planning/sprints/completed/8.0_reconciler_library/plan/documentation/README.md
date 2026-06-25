# Plan Documentation References

**Created:** June 23, 2026 10:46:19AM
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

### Critical

- **[CRITICAL] [Go Module & Standard Library](go-module-stdlib.md)** — Go 1.25, the nested-module + `replace` directive structure, the stdlib-only constraint, and the stdlib packages the reconcile library relies on (`sort`, `strings`, `encoding/json`). `context`/`sync` are ATCR consumer concerns, not part of the library.
- **[CRITICAL] [Reconciler Public API & Verification Interface](reconciler-api-verification.md)** — The lifted-as-is public API surface (`Reconcile`, `Source`, `Finding`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, verdict constants) and the verification contract (verdicts, confidence v2, gate precedence) defining the public/private boundary.
- **[CRITICAL] [JSON Format Adapter (reconcile-json/v1)](json-adapter.md)** — The `encoding/json` adapter converting an external finding stream into `[]Source` and a `Result` back out, using the independently-versioned `reconcile-json/v1` schema.

### Important

- **[IMPORTANT] [Testing with testify](testing-testify.md)** — Testify `assert`/`require` usage, `*_test.go` co-location, table-driven subtests, the runnable godoc example (AC#5), and the AC#3 byte-identical-fixture mandate.
- **[IMPORTANT] [Linting & CI/CD](linting-ci.md)** — golangci-lint config and the dual-coverage module CI (`reconcile-module.yml` on tag push + PR-time `./reconcile` job in `ci.yml`), reusing the self-hosted `[gauntlet]` runner and Go 1.25 setup.

### Reference

- **[REFERENCE] [Documentation Source Index](source.md)** — Generated index of the specification and codebase-discovery sources used to produce this documentation set.

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:**
  - `.planning/specifications/packages/go.md` (Go 1.25.0 stdlib)
  - `.planning/specifications/packages/standard-library.md` (ATCR stdlib subsystem map)
  - `.planning/specifications/packages/testify.md` (v1.11.1 testing framework)
  - `.planning/specifications/design-concepts/adversarial-verification-interface.md` (Epic 3.0 verification contract)
  - `.planning/specifications/packages/golangci-lint.md` (v2.12.2 linter + CI)
- **Codebase Discovery:** [../codebase-discovery.json](../codebase-discovery.json)
- **Specifications:** .planning/specifications/

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)
