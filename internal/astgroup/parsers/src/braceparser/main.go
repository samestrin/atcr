//go:build wasip1

// This file is the WebAssembly reactor entrypoint for the brace parser: thin
// //go:wasmexport alloc/free/parse wrappers plus the parse logic that drives the
// language-agnostic scanner in parse_core.go with the build-tag-selected `active`
// config (active_*.go). The alloc/free/emit guest ABI now lives once in the
// shared guestabi package and these wrappers delegate to it. Compiled
// GOOS=wasip1 GOARCH=wasm -buildmode=c-shared, once per language
// (-tags ts|php|rust|bash|java|kotlin|cpp|csharp), by build.sh.
//
// Memory protocol (matches the goparser/pyparser plugins and the astgroup host):
//   - alloc(n) returns a guest pointer to n writable bytes, pinned against GC.
//   - parse(ptr, n) parses src=memory[ptr:ptr+n] and returns (resPtr<<32|resLen).
//   - free(ptr) releases a previously alloc'd pointer.
//
// See guestabi for the non-moving-GC assumption the pin map relies on.
package main

import (
	"github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi"
)

// alloc/free are the wasip1 reactor ABI entrypoints the astgroup host calls. Go
// requires //go:wasmexport functions to live in the compiled command's package
// main, so these thin wrappers stay here while their bodies — plus the pins map
// and the non-moving-GC pointer-packing assumption — live once in guestabi.

//go:wasmexport alloc
func alloc(n int32) int32 { return guestabi.Alloc(n) }

//go:wasmexport free
func free(p int32) { guestabi.Free(p) }

//go:wasmexport parse
func parse(ptr int32, n int32) int64 {
	buf, ok := guestabi.Lookup(ptr)
	if !ok || n < 0 || int(n) > len(buf) {
		return guestabi.Emit(node{Kind: "error", Name: "bad pointer"})
	}
	src := buf[:n]

	// Empty input has no structure to recover. Emit a bare file node — matching
	// the goparser/pyparser empty-source contract — so the host treats empty
	// source as an empty tree rather than a parse error.
	if len(src) == 0 {
		return guestabi.Emit(node{Kind: "file", StartLine: 1, EndLine: 1})
	}
	return guestabi.Emit(parseSource(src, active))
}

func main() {}
