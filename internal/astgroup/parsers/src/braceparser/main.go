//go:build wasip1

// This file is the WebAssembly reactor entrypoint for the brace parser: the
// guest ABI (alloc/free/parse/emit + the pin map) plus the parse wasmexport that
// drives the language-agnostic scanner in parse_core.go with the build-tag-
// selected `active` config (active_*.go). Compiled GOOS=wasip1 GOARCH=wasm
// -buildmode=c-shared, once per language (-tags ts|php|rust|bash), by build.sh.
//
// Memory protocol (matches the goparser/pyparser plugins and the astgroup host):
//   - alloc(n) returns a guest pointer to n writable bytes, pinned against GC.
//   - parse(ptr, n) parses src=memory[ptr:ptr+n] and returns (resPtr<<32|resLen).
//   - free(ptr) releases a previously alloc'd pointer.
//
// This alloc/free/emit/pins ABI (~29 lines) is duplicated from the goparser and
// pyparser plugins. Extracting it into a shared guest package is the right remedy
// now that the parser source count is three (Go + Python + brace); it is captured
// as a technical-debt item rather than done here because it needs cross-module
// go.mod replace coordination in build.sh, which is out of this epic's
// "add parsers and registry entries only" scope. See goparser/main.go for the
// non-moving-GC assumption the pin map relies on.
package main

import (
	"encoding/json"
	"unsafe"
)

var pins = map[int32][]byte{}

//go:wasmexport alloc
func alloc(n int32) int32 {
	if n <= 0 {
		n = 1
	}
	b := make([]byte, n)
	p := int32(uintptr(unsafe.Pointer(&b[0])))
	pins[p] = b
	return p
}

//go:wasmexport free
func free(p int32) { delete(pins, p) }

//go:wasmexport parse
func parse(ptr int32, n int32) int64 {
	buf, ok := pins[ptr]
	if !ok || n < 0 || int(n) > len(buf) {
		return emit(node{Kind: "error", Name: "bad pointer"})
	}
	src := buf[:n]

	// Empty input has no structure to recover. Emit a bare file node — matching
	// the goparser/pyparser empty-source contract — so the host treats empty
	// source as an empty tree rather than a parse error.
	if len(src) == 0 {
		return emit(node{Kind: "file", StartLine: 1, EndLine: 1})
	}
	return emit(parseSource(src, active))
}

func emit(n node) int64 {
	b, err := json.Marshal(n)
	if err != nil {
		b = []byte(`{"kind":"error","name":"marshal"}`)
	}
	p := alloc(int32(len(b)))
	copy(pins[p], b)
	return (int64(p) << 32) | int64(len(b))
}

func main() {}
