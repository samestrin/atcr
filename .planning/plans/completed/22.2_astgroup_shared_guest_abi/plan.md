# Plan 22.2: Extract Shared Wasm Guest ABI

## Plan Overview
**Plan Type:** tech-debt
**Last Modified:** 2026-07-13
**Plan Goal:** Eliminate the triplicated Wasm guest ABI (alloc/free/emit/pins) boilerplate across `goparser`, `pyparser`, and `braceparser` by extracting it into one shared, isolated Go module, and pin the non-moving-GC pointer-packing assumption in a single documented location.
**Target Users:** N/A (internal technical debt; benefits future parser-plugin authors and maintainers of `internal/astgroup`)
**Framework/Technology:** Go 1.26 wasip1/wasm reactor modules (isolated per-parser `go.mod`), wazero host runtime

## Planning Deliverables

### Tasks
- **Location:** [`tasks/`](tasks/)
- **Status:** Generated `/create-tasks @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
- [Task 01: Create Shared guestabi Module](tasks/task-01-create-shared-guestabi-module.md)
- [Task 02: Wire goparser and pyparser to guestabi](tasks/task-02-wire-goparser-pyparser-to-guestabi.md)
- [Task 03: Wire braceparser to guestabi and verify full build](tasks/task-03-wire-braceparser-and-verify-build.md)

## Technical Debt Analysis Summary

Three independent TD rows (from sprints 13.1, 13.3, and 13.4) have flagged the same ~29-line alloc/free/emit/pins ABI block duplicated verbatim across `goparser/main.go`, `pyparser/main.go`, and `braceparser/main.go`. Each copy already cross-references the others and documents the same non-moving-GC pointer-packing assumption, explicitly deferring extraction until the parser count exceeded two — a threshold now crossed with the addition of `braceparser`. The refactor is mechanical: move `pins`/`alloc`/`free`/`emit` into a new isolated Go module (`internal/astgroup/parsers/src/guestabi`) that mirrors the existing per-parser module isolation convention, and wire it in via `go.mod` require+replace directives following the pattern already established by the root module's reconciler library extraction (Epic 8.0). No behavioral change occurs — the `wasmexport` surface each parser exposes to the wazero host is unchanged.

## Source Technical Debt Items

| File | Line | Sprint | Note |
|------|------|--------|------|
| `internal/astgroup/parsers/src/braceparser/main.go` | 1 | 13.4 | ABI (alloc/free/parse/emit/pins) duplicated across three parser modules |
| `internal/astgroup/parsers/src/braceparser/main.go` | 20 | 13.4 | Duplication now above the extraction threshold |
| `internal/astgroup/parsers/src/goparser/main.go` | 39 | 13.1 | Guest pins alloc'd buffers via unsafe pointer packing, assumes non-moving GC |

## Components Touched

- `internal/astgroup/parsers/src/guestabi` — new shared module (created).
- `internal/astgroup/parsers/src/goparser` — `main.go` imports `guestabi`; local pins/alloc/free/emit block replaced by thin `wasmexport` wrappers.
- `internal/astgroup/parsers/src/pyparser` — `main.go` imports `guestabi`; same replacement.
- `internal/astgroup/parsers/src/braceparser` — `main.go` imports `guestabi`; same replacement.
- `build.sh` — verification only. No functional changes expected (`go build` resolves local `replace` directives automatically); confirm it still produces all `.wasm` binaries and refreshes `SHA256SUMS` (see Technical Planning Notes and Risk Mitigation #3).

## Out of Scope

- Switching to a moving GC or an explicitly reserved allocation arena. Documenting the non-moving-GC pointer-packing assumption once, in the shared package, is in scope; changing the allocation strategy is not.
- Any change to parser-specific structural-hash logic — the separate `pyparser` quote-awareness gap is tracked under epic 22.3.

## Technical Planning Notes

- New isolated module `internal/astgroup/parsers/src/guestabi` keeps wasm-only code out of the parent module's `go build ./...`, matching the existing per-parser isolation pattern (each parser has its own `go.mod`, excluded from the root module).
- `//go:wasmexport` must stay declared in each parser's own `package main` (Go's wasip1 reactor ABI requirement); shared code provides the implementations the wrapper functions delegate to.
- The `node` struct (`Kind`/`Name`/`StartLine`/`EndLine`/`Children`) is structurally identical across all three parsers but stays locally defined per parser as each parser's own wire-contract type; the shared `Emit` helper takes `any` rather than a canonical node type, avoiding a coupling the epic did not request.
- `go.mod` wiring mirrors the root module's existing `require github.com/samestrin/atcr/reconcile` + `replace github.com/samestrin/atcr/reconcile => ./reconcile` pattern (`go.mod:37-41`) from Epic 8.0's reconciler library extraction.
- No `build.sh` changes are expected beyond an optional documentation comment — `go build` resolves local `replace` directives automatically; verify this holds during implementation.

## Implementation Strategy

Create the new `guestabi` module first with its ABI implementation and the pinned non-moving-GC doc comment, verified by `go build`/`go vet` in isolation. Wire each parser's `go.mod` with the require+replace directive one at a time, replacing that parser's local pins/alloc/free/emit block with thin `wasmexport` wrappers delegating to `guestabi`, and confirm `go build ./...` still succeeds for that parser's wasm target after each change. After all three parsers are wired, run the full `internal/astgroup` test suite unchanged to confirm no behavioral regression, and rebuild the vendored `.wasm` binaries via `build.sh` to refresh `SHA256SUMS`.

## Recommended Packages

No high-ROI packages identified — this extraction uses only stdlib (`unsafe`, `encoding/json`) already in use by all three parsers.

## Planning Success Criteria

- `goparser`, `pyparser`, and `braceparser` each import the shared guest ABI package instead of defining their own alloc/free/emit/pins boilerplate.
- The non-moving-GC pointer-packing assumption is documented once, in the shared package, not repeated per-parser.
- `go build ./...` succeeds for all three parser Wasm modules.
- Existing `internal/astgroup` tests pass unchanged.

## Risk Mitigation

1. **Risk:** `wasmexport`'s package-main requirement complicates a clean extraction. **Mitigation:** keep thin wrapper functions in each parser's `main` package; only the implementation body moves to `guestabi`.
2. **Risk:** `go.mod` replace misconfiguration silently breaks the wasm build for one parser. **Mitigation:** build and test each parser individually after wiring, not just once at the end.
3. **Risk:** `build.sh`'s per-directory `go build` invocation assumes each parser directory is self-contained; a new sibling-module dependency could break resolution. **Mitigation:** verify `build.sh` still produces all ten `.wasm` binaries correctly before considering the task done.

## Next Steps
1. `/find-documentation @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
2. `/create-documentation @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
3. `/create-tasks @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
4. `/design-sprint @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
5. `/create-sprint @.planning/plans/active/22.2_astgroup_shared_guest_abi/`
