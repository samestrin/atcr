# Task 03: Wire braceparser to guestabi and verify full build

**Source:** Plan 22.2 – Debt Item #3
**Priority:** P2 | **Effort:** S | **Type:** Refactor

## Problem Statement
`internal/astgroup/parsers/src/braceparser/main.go` still defines its own copy of the guest ABI boilerplate — the `pins` map plus `alloc`, `free`, and `emit` (lines 28-42 and 61-69) — byte-for-byte equivalent (modulo the locally-typed `node` argument to `emit`) to the copies already extracted from `goparser` and `pyparser` in Task 02. The doc comment at `main.go:14-20` explicitly calls this out: the duplication crossed the extraction threshold once `braceparser` became the third parser, and the fix was deferred pending the shared `guestabi` module (Task 01) and its cross-module `go.mod` wiring. Once `braceparser` is wired, the plan's three source TD rows (sprints 13.1, 13.3, 13.4) are fully resolved, but that isn't confirmed until the whole `internal/astgroup` suite passes unchanged and the vendored `.wasm` binaries are rebuilt and re-verified against a fresh `SHA256SUMS`.

## Solution Overview
Apply the same require+replace + thin-wrapper pattern used for `goparser`/`pyparser` in Task 02 to `braceparser`: add a `require`+`replace => ../guestabi` pair to `braceparser/go.mod`, delete the local `pins`/`alloc`/`free`/`emit` block from `main.go`, and replace it with thin `//go:wasmexport alloc`/`free` wrappers that delegate to `guestabi.Alloc`/`guestabi.Free`, plus a call to `guestabi.Emit` wherever `emit(...)` was called, and switch `parse()`'s `buf, ok := pins[ptr]` buffer lookup to `buf, ok := guestabi.Lookup(ptr)` (the same swap Task 02 makes for `goparser`/`pyparser`; `pins` is now unexported in `guestabi`). Then close out the plan: run the full `internal/astgroup` test suite unchanged, regenerate all ten vendored `.wasm` binaries plus `SHA256SUMS` via `build.sh`, and confirm `TestEmbeddedParsersMatchManifest` passes against the regenerated binaries.

## Technical Implementation
### Steps
1. In `internal/astgroup/parsers/src/braceparser/go.mod`, add the require+replace pair pointing at the sibling `guestabi` module, mirroring the pattern used for `goparser`/`pyparser` in Task 02 and the root `go.mod:37-41` reconciler-extraction precedent:
   ```go
   require github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi v0.0.0

   replace github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi => ../guestabi
   ```
   (Use whatever module path Task 01 actually assigned to `guestabi/go.mod` — confirm by reading that file rather than assuming.)
2. In `internal/astgroup/parsers/src/braceparser/main.go`:
   - Remove the `var pins = map[int32][]byte{}` declaration (line 28) and the `alloc`/`free` function bodies (lines 30-42), replacing them with thin `//go:wasmexport` wrappers that call into `guestabi`, e.g.:
     ```go
     //go:wasmexport alloc
     func alloc(n int32) int32 { return guestabi.Alloc(n) }

     //go:wasmexport free
     func free(p int32) { guestabi.Free(p) }
     ```
   - Remove the local `emit` helper (lines 61-69) and replace its call sites (`parse`'s `emit(node{...})` calls and `emit(parseSource(src, active))`) with `guestabi.Emit(...)`.
   - In `parse()` (currently `buf, ok := pins[ptr]` at `braceparser/main.go:46`), switch the buffer lookup to `guestabi.Lookup` (Task 01 specifies `func Lookup(p int32) ([]byte, bool)`): `buf, ok := guestabi.Lookup(ptr)`. Confirm the name against `guestabi.go`. This mirrors the Task 02 swap for `goparser`/`pyparser` and is required because `pins` is removed from `main.go`.
   - Update the `import` block to add the `guestabi` package and drop `unsafe` and `encoding/json` — both are used only by the ABI block being removed (`alloc` uses `unsafe`; `emit` uses `encoding/json`). Verified: `parse_core.go` imports only `bytes`+`strings` and `configs.go` has no imports, so neither sibling file uses `encoding/json` or `unsafe`; each file owns its own import block, so removing them from `main.go` cannot break the other files. `main.go` still references the `node` struct (defined in `parse_core.go`, same package — no import needed) at the `guestabi.Emit(node{...})` call sites.
   - Trim the doc comment at lines 3-20 to drop the now-resolved "captured as a technical-debt item" paragraph (lines 14-20), keeping the memory-protocol description (lines 9-12) and updating it to note the ABI is now provided by `guestabi` (matching however Task 02 worded this for `goparser`/`pyparser`).
3. Build and test `braceparser` in isolation before touching anything else: from `internal/astgroup/parsers/src/braceparser`, run `GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -tags ts -o /tmp/braceparser-ts.wasm .` (or equivalent single-tag smoke build) to confirm the replace directive resolves and the wasmexport wrappers compile before running the full `build.sh`.
4. Optionally add a one-line documentation comment to `internal/astgroup/parsers/build.sh` noting the parser directories now depend on the sibling `guestabi` module via `go.mod` replace directives (no functional change — `go build` already resolves local replace directives automatically without any `build.sh` change).
5. Regenerate all ten vendored `.wasm` binaries and refresh `SHA256SUMS` by running `internal/astgroup/parsers/build.sh` from repo root, confirming the script's build log lists all ten targets: `go`, `python`, `ts`, `php`, `rust`, `bash`, `java`, `kotlin`, `cpp`, `csharp`.
6. Run the full `internal/astgroup` package test suite unchanged: `go test ./internal/astgroup/...` — this must include `embed_test.go` (`TestEmbeddedParsersMatchManifest`), `host_test.go`, and `crosscompile_test.go`, all passing against the regenerated binaries with no test file modifications.
7. Run `go build ./...` from repo root to confirm the parser wasm modules' isolated `go.mod` files (excluded from the root module) don't break the top-level build, and run `go vet ./...` for a final sanity check.
8. Verify no source or embedded-binary drift remains: `git status` should show the three parsers' `main.go`/`go.mod` changes plus the regenerated `.wasm` files and `SHA256SUMS` as the only diffs — no test files touched.

## Files to Create/Modify
- `internal/astgroup/parsers/src/braceparser/main.go` – remove local `pins`/`alloc`/`free`/`emit`, add thin `//go:wasmexport` wrappers delegating to `guestabi`, update doc comment and imports.
- `internal/astgroup/parsers/src/braceparser/go.mod` – add `require` + `replace => ../guestabi` directive.
- `internal/astgroup/parsers/build.sh` – optional doc-comment-only update noting the new sibling-module dependency (no functional change).
- `internal/astgroup/parsers/go.wasm`, `python.wasm`, `ts.wasm`, `php.wasm`, `rust.wasm`, `bash.wasm`, `java.wasm`, `kotlin.wasm`, `cpp.wasm`, `csharp.wasm` – regenerated by `build.sh` (byte-identical unless the ABI change alters compiled output; commit regardless since the source changed).
- `internal/astgroup/parsers/SHA256SUMS` – regenerated checksum manifest, committed alongside the `.wasm` files.

## Documentation Links
(none — no specifications matched this plan; see .planning/plans/active/22.2_astgroup_shared_guest_abi/documentation/source.md)

## Related Files (from codebase-discovery.json)
- `internal/astgroup/parsers/src/braceparser/main.go`
- `internal/astgroup/parsers/build.sh`
- `internal/astgroup/embed_test.go`
- `internal/astgroup/parsers/SHA256SUMS`

## Success Criteria
- [ ] `braceparser/main.go` no longer defines `pins`, `alloc`, `free`, or `emit` locally — only thin `//go:wasmexport` wrappers delegating to `guestabi`.
- [ ] `braceparser/main.go`'s `parse()` looks up the pinned buffer via `buf, ok := guestabi.Lookup(ptr)` instead of the removed local `pins` map.
- [ ] `braceparser/go.mod` has a `require` + `replace => ../guestabi` directive matching the pattern used in `goparser`/`pyparser`.
- [ ] `go build ./...` succeeds for all three parser Wasm modules (go, python, brace-tagged variants).
- [ ] `build.sh` produces all ten `.wasm` binaries (go/python/ts/php/rust/bash/java/kotlin/cpp/csharp) and a refreshed `SHA256SUMS`.
- [ ] `TestEmbeddedParsersMatchManifest` (internal/astgroup/embed_test.go:15) passes against the regenerated binaries.
- [ ] The full `internal/astgroup` test suite (embed_test.go, host_test.go, crosscompile_test.go) passes unchanged — no test files modified.

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- No new unit tests required — this is a pure refactor with an unchanged `wasmexport` surface; existing tests must pass without modification.

**Integration Tests:**
- `go test ./internal/astgroup/...` exercising `embed_test.go` (`TestEmbeddedParsersMatchManifest`), `host_test.go` (wazero host round-trips through the regenerated `braceparser`-derived `.wasm` binaries for every brace-family language), and `crosscompile_test.go`.
- Manual single-tag smoke build (`GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -tags ts .` from `braceparser/`) before running the full `build.sh`, to catch a broken replace directive early without waiting on all ten builds.

**Test Files:**
- `internal/astgroup/embed_test.go`
- `internal/astgroup/host_test.go`
- `internal/astgroup/crosscompile_test.go`

## Risk Mitigation
- **Risk:** `go.mod` replace misconfiguration silently breaks the wasm build for `braceparser` specifically (it builds once per language tag, unlike `goparser`/`pyparser`'s single build). **Mitigation:** run the isolated single-tag smoke build (Step 3) before invoking `build.sh` for all eight brace-family targets, and confirm `build.sh`'s log lists all ten target names.
- **Risk:** Forgetting to commit the regenerated `.wasm` binaries alongside `SHA256SUMS` leaves `TestEmbeddedParsersMatchManifest` failing in CI even though it passes locally post-build. **Mitigation:** `git status` check in Step 8 confirms both the `.wasm` files and `SHA256SUMS` are staged together with the source changes.
- **Risk:** Removing `encoding/json` or `unsafe` imports from `main.go` when another file in `package main` (`parse_core.go`, `configs.go`) still needs them causes a compile error. **Mitigation:** check sibling files in the same package before trimming imports (Step 2).

## Dependencies
- Task 01 (Create Shared guestabi Module) must be complete first; should run after Task 02

## Definition of Done
- `braceparser` imports the shared `guestabi` package instead of defining its own alloc/free/emit/pins boilerplate, matching `goparser` and `pyparser`.
- `go build ./...` succeeds for all three parser Wasm modules.
- `build.sh` regenerates all ten `.wasm` binaries and a refreshed `SHA256SUMS`, committed together.
- `TestEmbeddedParsersMatchManifest` and the full `internal/astgroup` test suite pass unchanged against the regenerated binaries.
- All three plan acceptance criteria are satisfied: shared-package adoption across all three parsers, successful `go build ./...`, and unchanged passing tests.
