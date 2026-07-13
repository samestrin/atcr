# Task 01: Create Shared guestabi Module

**Source:** Plan 22.2 – Debt Item #1
**Priority:** P2 | **Effort:** S | **Type:** Refactor

## Problem Statement
`internal/astgroup/parsers/src/goparser/main.go:39` documents the canonical copy of a duplicated Wasm guest ABI: a `pins map[int32][]byte` plus `alloc`/`free`/`emit` boilerplate, byte-for-byte repeated across `goparser` (lines 41-66, 189-197), `pyparser` (lines 37-51, 307-315), and `braceparser` (lines 28-42, 61-69). The project's own documented extraction threshold — "parser count > 2" — has now been crossed by the addition of `braceparser`, so the duplication should be consolidated. The non-moving-GC pointer-packing assumption underlying the `pins` map (guest pointers are packed as `int32(uintptr(unsafe.Pointer(&b[0])))`, valid only because Go's wasip1/wasm GC does not move heap objects) is currently repeated as a comment in each parser instead of living in one authoritative place.

## Solution Overview
Create a new isolated Go module at `internal/astgroup/parsers/src/guestabi/` that mirrors the existing per-parser isolated-module pattern (own `go.mod`, wasip1-only, excluded from the parent module's `go build ./...` via the nested-module boundary). This module holds the underlying implementation only — the unexported `pins` map and exported `Alloc`, `Free`, `Lookup`, and `Emit` functions — NOT the `//go:wasmexport` declarations themselves, since Go's wasip1 reactor ABI requires `//go:wasmexport` functions to live in each parser's own `package main`. `Emit` is generalized from `emit(n node) int64` to `Emit(v any) int64`, marshaling via `encoding/json` without any coupling to a parser's concrete node type, so all three parsers (with their differing node shapes) can reuse it. `Lookup(p int32) ([]byte, bool)` is the read-back accessor each parser's `parse()` needs in place of its former direct `pins[ptr]` index (`goparser/main.go:70`, `pyparser/main.go:54`, `braceparser/main.go:46`), since `pins` is now unexported; it is the only exported path that touches the map besides `Alloc`/`Free`/`Emit`'s internal use. The non-moving-GC doc comment carried from `goparser/main.go` lines 41-51 becomes the single pinned location for that assumption. This task only creates the new module; wiring `goparser`, `pyparser`, and `braceparser` to import it happens in Tasks 02 and 03.

## Technical Implementation
### Steps
1. Create the module directory `internal/astgroup/parsers/src/guestabi/`.
2. Write `internal/astgroup/parsers/src/guestabi/go.mod`, modeled on `internal/astgroup/parsers/src/goparser/go.mod`'s isolated-module convention: module path `github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi`, `go 1.26`, and the same explanatory header comment describing why a nested go.mod excludes this wasip1-only package from the parent module's `go build ./...` / `go test ./...`.
3. Write `internal/astgroup/parsers/src/guestabi/guestabi.go`:
   - `//go:build wasip1` build tag (matching `goparser/main.go:1`).
   - `package guestabi`.
   - Imports: `"encoding/json"` and `"unsafe"` — the same two stdlib imports the ABI block already pulls into each parser (`Emit` uses `json.Marshal`; `Alloc` uses `unsafe.Pointer`). No `//go:wasmexport` directive appears in this file.
   - Unexported `pins map[int32][]byte` var, carrying the non-moving-GC pointer-packing doc comment from `goparser/main.go` lines 41-51 verbatim (adjusted to refer to the shared package rather than repeating "duplicated in the pyparser plugin" — replace that paragraph with a note that this package IS the extracted, single copy).
   - Exported `func Alloc(n int32) int32` — same body as `goparser/main.go:55-63`, minus the `//go:wasmexport` directive.
   - Exported `func Free(p int32)` — same body as `goparser/main.go:66`, minus the `//go:wasmexport` directive.
   - Exported `func Lookup(p int32) ([]byte, bool)` — new accessor (no existing copy to mirror): returns the buffer pinned at guest pointer `p` and whether it exists, so each parser's `parse()` can replace its direct `pins[ptr]` map index with `buf, ok := guestabi.Lookup(ptr)`. Body: `b, ok := pins[p]; return b, ok` (mirrors a Go map comma-ok index). `pins` stays unexported; this is the only read-back path the parsers need.
   - Exported `func Emit(v any) int64` — generalized from `goparser/main.go:189-197`'s `emit(n node) int64`: `json.Marshal(v)` instead of `json.Marshal(n)`, falling back to the same `{"kind":"error","name":"marshal"}` sentinel on marshal failure, then `Alloc` + `copy` + pack `(int64(p) << 32) | int64(len(b))` exactly as today. (`Emit` reads `pins[p]` internally for the `copy` — fine, it lives in the same package as the unexported `pins`.)
4. Run `GOOS=wasip1 GOARCH=wasm go build ./...` and `GOOS=wasip1 GOARCH=wasm go vet ./...` from inside `internal/astgroup/parsers/src/guestabi/` to verify the new module compiles standalone. The `GOOS=wasip1` env is required: `guestabi.go` carries the `//go:build wasip1` tag (matching `goparser`), so under the default host GOOS the file is excluded and a plain `go build ./...`/`go vet ./...` compiles nothing — a vacuous no-op that would not actually verify the module.
5. Run `go build ./...` from the repository root to confirm the new nested module is correctly excluded from the parent module's build (it must not break `go build ./...` at the repo root, and it must not appear as a package the root module tries to compile).
6. Run the existing `internal/astgroup` test suite (`go test ./internal/astgroup/...` from the repo root) to confirm no regression — this task does not touch any parser's `main.go`, so behavior must be byte-for-byte unchanged.

## Files to Create/Modify
- `internal/astgroup/parsers/src/guestabi/go.mod` – new isolated module manifest
- `internal/astgroup/parsers/src/guestabi/guestabi.go` – new shared ABI implementation (unexported pins; exported Alloc, Free, Lookup, Emit) with pinned non-moving-GC doc comment

## Documentation Links
(none — no specifications matched this plan; see .planning/plans/active/22.2_astgroup_shared_guest_abi/documentation/source.md)

## Related Files (from codebase-discovery.json)
- `internal/astgroup/parsers/src/goparser/main.go`
- `internal/astgroup/parsers/src/goparser/go.mod`
- `go.mod`

## Success Criteria
- [x] `internal/astgroup/parsers/src/guestabi/go.mod` exists with its own module path, `go 1.26`, and an isolated-module explanatory comment
- [x] `internal/astgroup/parsers/src/guestabi/guestabi.go` exists with `//go:build wasip1`, `package guestabi`, unexported `pins`, and exported `Alloc`, `Free`, `Lookup(p int32) ([]byte, bool)`, `Emit(v any) int64`
- [x] `Lookup` is the only exported read-back path for a pinned buffer; `pins` stays unexported so parsers go through `guestabi.Lookup` in their `parse()` instead of touching the map directly
- [x] The non-moving-GC pointer-packing assumption is documented once, as a doc comment above `pins` in `guestabi.go`, matching the substance of `goparser/main.go` lines 41-51
- [x] `Emit` accepts `any` and is not coupled to any parser's concrete `node` type
- [x] `GOOS=wasip1 GOARCH=wasm go build ./...` and `GOOS=wasip1 GOARCH=wasm go vet ./...` succeed when run from inside `internal/astgroup/parsers/src/guestabi/` (the wasip1 env is required — see step 4; a default-GOOS build/vet skips the tagged file and verifies nothing)
- [x] `go build ./...` at the repository root still succeeds and does not attempt to compile the new nested module
- [x] `go test ./internal/astgroup/...` passes unchanged (no parser wiring changed in this task)
- [x] No `//go:wasmexport` directive appears anywhere in `guestabi.go`
- [x] No changes made to `goparser/main.go`, `pyparser/main.go`, `braceparser/main.go`, or any parser's `go.mod` in this task

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- None added in this task — `guestabi` is a wasip1-only package with no existing test harness pattern in the sibling parser modules (`goparser`, `pyparser`, `braceparser` have none either); correctness is verified by successful compilation under `GOOS=wasip1` and by Task 02/03 wiring reusing it without behavior change.

**Integration Tests:**
- Existing `internal/astgroup` host-side tests (e.g. `TestHost_ParseEmptySourceGo` and equivalents) continue to pass unmodified, since this task does not change any parser's wasm binary or wiring — it only stages the new shared package for later import.

**Test Files:**
- `internal/astgroup/*_test.go` (run via `go test ./internal/astgroup/...`; no new test files created by this task)

## Risk Mitigation
- **Risk:** Generalizing `emit(node) int64` to `Emit(v any) int64` could silently change JSON output shape for a non-struct-first-argument caller. **Mitigation:** implementation is a direct `json.Marshal(v)` passthrough — behavior for a `node`-shaped struct argument is byte-identical to today's `emit(n node)`; verified by Task 02/03 wiring producing unchanged wasm output.
- **Risk:** A moving GC in a future Go toolchain would silently break the `pins` pointer-packing trick. **Mitigation:** this task's core deliverable — pinning the non-moving-GC assumption as a doc comment on `pins` in the single shared location — makes this a documented, discoverable risk rather than a silent one, satisfying the epic's acceptance criterion directly.
- **Risk:** Nested go.mod misconfiguration could cause the new module to leak into the parent module's `go build ./...`, breaking the root build. **Mitigation:** step 5 explicitly verifies root-level `go build ./...` still succeeds and does not pull in the new module, mirroring the existing `goparser`/`pyparser`/`braceparser` isolation pattern.

## Dependencies
- None (this is the foundation task; Tasks 02 and 03 depend on it)

## Definition of Done
- [x] `internal/astgroup/parsers/src/guestabi/go.mod` and `guestabi.go` created and committed
- [x] `GOOS=wasip1 GOARCH=wasm go build ./...` and `GOOS=wasip1 GOARCH=wasm go vet ./...` succeed inside `internal/astgroup/parsers/src/guestabi/` (the wasip1 env is required — see step 4; a default-GOOS build/vet skips the tagged file and verifies nothing)
- [x] `guestabi` exports `Alloc`, `Free`, `Lookup`, and `Emit`; `pins` is unexported
- [x] `go build ./...` succeeds at the repository root with the new module present but excluded
- [x] `go test ./internal/astgroup/...` passes unchanged
- [x] Non-moving-GC pointer-packing assumption documented exactly once, in `guestabi.go`
- [x] No parser `main.go` or `go.mod` files modified in this task
