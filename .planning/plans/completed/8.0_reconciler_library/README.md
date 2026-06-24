## Overview
Extract ATCR's deterministic reconciler from `internal/reconcile` into a standalone, stdlib-only Go module (`github.com/samestrin/atcr/reconcile`) consumed by ATCR via a root `replace` directive. The plan lifts the existing public API as-is (`Reconcile(sources []Source, opts Options) Result` + `Source`/`Merged`/`Options`/`Result`/`Summary`), ships one JSON adapter, adds dual (Apache 2.0 / commercial) licensing, and wires independent module CI — turning the core architectural moat into a separable, licensable asset while keeping ATCR the reference implementation with zero behavioral change.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/8.0_reconciler_library/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/8.0_reconciler_library/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/8.0_reconciler_library/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/8.0_reconciler_library/`

## Timeline & Milestones
**Estimated duration: 3 weeks** (per epic).
- **M1 — Boundary + scaffold (Phase 0):** resolve public-API boundary (split `emit.go`/`discover.go` types from I/O; decide levenshtein scope; confirm ATCR adapter location); create `./reconcile/` + `go.mod` + root `replace` directive.
- **M2 — Core extraction + ATCR swap:** mechanically move pure types + logic (cluster, dedupe, merge, confidence, disagree, ambiguous, attribution, severity); swap ATCR imports behind a boundary adapter; full test corpus green and fixtures byte-identical.
- **M3 — Adapter + docs + licensing + CI:** JSON adapter round-tripping `atcr-findings/v1`; README + godoc example; Apache 2.0 `LICENSE` + `LICENSE-COMMERCIAL.md`; independent module CI on tag push; leaderboard methodology reference.

## Resource Requirements
- 1 backend developer (Sam); Go 1.25 toolchain.
- Existing reconcile test corpus + cross-epic fixture corpus (reconcile-summary.json / findings) as the no-behavioral-change oracle.

## Expected Outcomes
- A standalone, licensable `github.com/samestrin/atcr/reconcile` module other tools can embed without importing the ATCR binary.
- ATCR remains the reference implementation with byte-identical reconcile output.
- Embeddability proven via the JSON adapter; OSS + commercial licensing paths documented.
- Independent module CI on tag push; leaderboard methodology cites the library.

## Risk Summary
- **Behavior break (Low / High):** mitigated by lift-as-is + full test corpus + byte-identical fixtures.
- **Type/I/O split in `emit.go`/`discover.go` (Med / High):** mitigated by mechanical, compile-checked split (types first, I/O second).
- **13.x dedup-replacement epics landing mid-extraction (Med / Med):** mitigated by sequencing extraction first and pinning module semver.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/)
- [Acceptance Criteria](acceptance-criteria/) (25 ACs across 6 stories)
- [Sprint Design](sprint-design.md) (9/12 COMPLEX, 5 phases, 10 days)
- [Documentation](documentation/)

## Documentation References

Grounded documentation indexes (see [documentation/README.md](documentation/README.md)):

### Critical
- [Go Module & Standard Library](documentation/go-module-stdlib.md) — Go 1.25, nested module + `replace` directive, stdlib-only constraint.
- [Reconciler Public API & Verification Interface](documentation/reconciler-api-verification.md) — lifted-as-is public API + verification contract / public-private boundary.
- [JSON Format Adapter (reconcile-json/v1)](documentation/json-adapter.md) — `encoding/json` adapter + independent schema (AC#4).

### Important
- [Testing with testify](documentation/testing-testify.md) — test conventions + runnable godoc example (AC#5).
- [Linting & CI/CD](documentation/linting-ci.md) — golangci-lint + dual-coverage module CI (AC#7).
