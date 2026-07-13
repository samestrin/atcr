# Task 02: Wire goparser and pyparser to guestabi

**Source:** Plan 22.2 – Debt Item #2
**Priority:** P2 | **Effort:** M | **Type:** Refactor

## Problem Statement
The Wasm guest ABI — a `pins` map plus `alloc`/`free`/`emit` implementations totaling ~29 lines, including the doc comment pinning the non-moving-GC pointer-packing assumption — is copy-pasted verbatim in `goparser/main.go` (lines 41-66, 189-197) and `pyparser/main.go` (lines 37-51, 307-315). Each copy already cross-references the other and documents the same assumption, which was explicitly flagged in both files as "revisit once parser count grows beyond two" — a threshold Task 01's `braceparser` wiring (Task 03) now crosses. Task 01 created the shared `internal/astgroup/parsers/src/guestabi` module holding the canonical implementation; `goparser` and `pyparser` still define their own local copies and are not yet wired to it.

## Solution Overview
Wire `goparser` and `pyparser` to the `guestabi` module created in Task 01, one parser at a time, mirroring the root module's existing local-replace pattern (`go.mod:37-41`: `require github.com/samestrin/atcr/reconcile` + `replace github.com/samestrin/atcr/reconcile => ./reconcile`). For each parser: add the `require`+`replace` directives to its `go.mod`, delete the local `pins`/`alloc`/`free`/`emit` implementation, replace it with thin `//go:wasmexport` wrapper functions in `package main` that delegate to `guestabi`'s exported API, and re-point `parse()`'s `buf, ok := pins[ptr]` buffer lookup to `buf, ok := guestabi.Lookup(ptr)` (the `pins` map is now unexported in `guestabi`). The `node` struct and all parsing logic stay untouched and locally defined per parser. `//go:wasmexport alloc` / `//go:wasmexport free` must remain declared in each parser's own `package main` — Go's wasip1 reactor ABI requires the exported functions to live in the compiled command's package, so only the implementation bodies move to `guestabi`.

Build and test each parser individually right after wiring it, before moving to the next — this catches a misconfigured `replace` directive (wrong relative path, stale module name) immediately instead of after both parsers are touched.

## Technical Implementation
### Steps

1. **Wire `goparser/go.mod`** (`internal/astgroup/parsers/src/goparser/go.mod`): append, mirroring root `go.mod:37-41`:
   ```go
   require github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi v0.0.0

   replace github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi => ../guestabi
   ```
   Confirm the exact module path and exported API (`Alloc`/`Free`/`Emit`, plus the `Lookup(p int32) ([]byte, bool)` accessor Task 01 specifies for reading back a pinned buffer by pointer — `parse()` needs this) against what Task 01 actually produced in `internal/astgroup/parsers/src/guestabi`; adjust names below to match if Task 01 named them differently.

2. **Replace `goparser/main.go`'s ABI block** (`internal/astgroup/parsers/src/goparser/main.go`):
   - Delete the `pins` var and its doc comment, and the `alloc`/`free` implementations at lines 41-66.
   - Add the import `"github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi"`; drop the now-unused `"unsafe"` import (only `alloc` used it) and the `"encoding/json"` import (only `emit` used it, and that logic moves into `guestabi.Emit`).
   - Add thin wrappers in place of the deleted block:
     ```go
     //go:wasmexport alloc
     func alloc(n int32) int32 { return guestabi.Alloc(n) }

     //go:wasmexport free
     func free(p int32) { guestabi.Free(p) }
     ```
   - In `parse()` (currently `buf, ok := pins[ptr]` at `goparser/main.go:70`), switch the buffer lookup to `guestabi.Lookup` (Task 01 specifies `func Lookup(p int32) ([]byte, bool)`): `buf, ok := guestabi.Lookup(ptr)`. Confirm the name against `guestabi.go` and adjust only if Task 01 named it differently.
   - Replace the local `emit` helper (lines 189-197) with a one-line delegate: `func emit(n node) int64 { return guestabi.Emit(n) }` (the `node` struct itself stays locally defined; `guestabi.Emit` takes `any` per the plan's design note).

3. **Build and test `goparser` before touching `pyparser`:**
   ```sh
   cd internal/astgroup/parsers/src/goparser && GOOS=wasip1 GOARCH=wasm go build ./... && GOOS=wasip1 GOARCH=wasm go vet ./...
   ```
   `go vet` needs the same `GOOS=wasip1 GOARCH=wasm` env as `go build` (the `&&` runs it as a separate command, so the env vars do not carry over): `goparser/main.go` carries `//go:build wasip1`, so a default-GOOS `go vet ./...` skips the file entirely and vets nothing. Fix any replace-directive or import issue here before proceeding — do not move to `pyparser` until this passes clean.

4. **Wire `pyparser/go.mod`** (`internal/astgroup/parsers/src/pyparser/go.mod`): same `require`+`replace` pair as step 1, module path unchanged (both parsers point at the same sibling `guestabi` module via their own relative `../guestabi`).

5. **Replace `pyparser/main.go`'s ABI block** (`internal/astgroup/parsers/src/pyparser/main.go`): same transformation as step 2, applied to its ABI block at lines 37-51 (`pins`/`alloc`/`free`) and lines 307-315 (`emit`). `pyparser`'s `parse()` (`buf, ok := pins[ptr]` at `pyparser/main.go:54`) gets the same `buf, ok := guestabi.Lookup(ptr)` swap. Drop `"unsafe"` and `"encoding/json"` imports; keep `"strings"` (still used by the indentation/tokenizing logic). Add the `guestabi` import. Note `pyparser/main.go` has no `//go:build wasip1` tag (unlike `goparser`/`braceparser`), so the step-6 vet MUST run under `GOOS=wasip1` — a default-GOOS vet would try to compile pyparser under the host GOOS and fail resolving the tagged `guestabi` import.

6. **Build and test `pyparser`:**
   ```sh
   cd internal/astgroup/parsers/src/pyparser && GOOS=wasip1 GOARCH=wasm go build ./... && GOOS=wasip1 GOARCH=wasm go vet ./...
   ```
   Both `go build` and `go vet` need `GOOS=wasip1 GOARCH=wasm`: `pyparser/main.go` has no `//go:build wasip1` tag, so a default-GOOS `go vet ./...` would attempt to compile pyparser under the host GOOS, hit the tagged `guestabi` import, and fail with "no Go files matching build constraints". Under `GOOS=wasip1` the `guestabi` import resolves and the vet type-checks the wrappers.

7. **Confirm no regression in the parent module:** from the repo root, run `go build ./...` and `go test ./internal/astgroup/...` — both parsers' isolated `go.mod` already exclude them from the root module's build graph (per the "Isolated module" doc comment in each `go.mod`), and the vendored `.wasm` binaries embedded via `go:embed` are unchanged until `build.sh` is re-run (out of scope for this task — deferred to whichever task rebuilds/vendors the binaries), so this step should be a no-op confirming nothing broke.

## Files to Create/Modify
- `internal/astgroup/parsers/src/goparser/go.mod` – add `require`+`replace` for `guestabi`.
- `internal/astgroup/parsers/src/goparser/main.go` – delete local `pins`/`alloc`/`free`/`emit` block; add thin `wasmexport` wrappers delegating to `guestabi`; update `parse()`'s buffer lookup; adjust imports.
- `internal/astgroup/parsers/src/pyparser/go.mod` – add `require`+`replace` for `guestabi`.
- `internal/astgroup/parsers/src/pyparser/main.go` – same transformation as `goparser/main.go`.

## Documentation Links
(none — no specifications matched this plan; see .planning/plans/active/22.2_astgroup_shared_guest_abi/documentation/source.md)

## Related Files (from codebase-discovery.json)
- `internal/astgroup/parsers/src/goparser/main.go`
- `internal/astgroup/parsers/src/pyparser/main.go`
- `go.mod`

## Success Criteria
- [x] `goparser/main.go` no longer defines its own `pins`/`alloc`/`free`/`emit` implementation; its `wasmexport` wrappers delegate to `guestabi`.
- [x] `pyparser/main.go` no longer defines its own `pins`/`alloc`/`free`/`emit` implementation; its `wasmexport` wrappers delegate to `guestabi`.
- [x] `goparser/go.mod` and `pyparser/go.mod` each carry a `require`+`replace` pair pointing at `../guestabi`, mirroring the root module's `reconcile` pattern.
- [x] `GOOS=wasip1 GOARCH=wasm go build ./...` succeeds independently in both `internal/astgroup/parsers/src/goparser` and `internal/astgroup/parsers/src/pyparser`.
- [x] `braceparser` is untouched by this task (reserved for Task 03).
- [x] Root `go build ./...` and `go test ./internal/astgroup/...` still pass unchanged.

## Manual Code Review
- [x] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- No new unit tests required — this is a mechanical extraction with no behavioral change. `guestabi` is a wasip1-only package with no test-harness pattern (Task 01 adds no `*_test.go`); correctness is verified by successful `GOOS=wasip1 GOARCH=wasm go build`/`go vet` per parser plus the unchanged `internal/astgroup` regression suite.

**Integration Tests:**
- `GOOS=wasip1 GOARCH=wasm go build ./...` run individually in `goparser/` and `pyparser/` after each is wired, confirming the `replace` directive resolves and the wrapper functions compile against `guestabi`'s exported API.
- `go test ./internal/astgroup/...` from the repo root, confirming the existing host-side test suite (which exercises the already-vendored `.wasm` binaries, unaffected by this source-level refactor until `build.sh` is re-run) passes unchanged.

**Test Files:**
- `internal/astgroup/*_test.go` (existing host-side suite; run as a regression check, not modified by this task)

## Risk Mitigation
- **Risk:** A misconfigured `replace` directive (wrong relative path or stale module name) silently breaks the wasm build for one parser. **Mitigation:** build and `go vet` each parser individually immediately after wiring it (steps 3 and 6), not once at the end after both are touched.
- **Risk:** `guestabi`'s actual exported API (from Task 01) doesn't exactly match the assumed `Alloc`/`Free`/`Lookup`/`Emit` names used in this task's steps. **Mitigation:** step 1 explicitly calls out confirming the real API against Task 01's output before writing the wrapper code; adjust call sites to match without altering the wrapper functions' `wasmexport` signatures (those are fixed by the wazero host contract).
- **Risk:** Removing the `"unsafe"`/`"encoding/json"` imports without checking for other uses breaks the build. **Mitigation:** both parsers use each import exclusively within the ABI block being removed (verified by reading the current `main.go` files in full before editing); `go build` will fail loudly on any missed usage.

## Dependencies
- Task 01 (Create Shared guestabi Module) must be complete first

## Definition of Done
- [x] `goparser` and `pyparser` both import and delegate to `guestabi`; no local ABI implementation remains in either.
- [x] Both parsers' `go.mod` files carry the `require`+`replace` wiring and build cleanly with `GOOS=wasip1 GOARCH=wasm go build ./...`.
- [x] Root `go build ./...` and `go test ./internal/astgroup/...` pass with no changes required outside the four modified files.
- [x] `braceparser` remains untouched, ready for Task 03.
