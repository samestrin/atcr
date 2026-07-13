//go:build wasip1

// Package guestabi is the shared WebAssembly guest ABI for the astgroup parser
// plugins. It holds the single, canonical implementation of the alloc/free/emit
// memory protocol that goparser, pyparser, and braceparser each drive from their
// own package main via thin //go:wasmexport wrappers.
//
// The //go:wasmexport directives themselves do NOT live here: Go's wasip1 reactor
// ABI requires the exported functions to be declared in each compiled command's
// own package main, so only the implementation bodies are shared. Each parser
// exposes `alloc`/`free`/`parse` in its own package and delegates the bodies to
// Alloc/Free (and Emit/Lookup) here.
package guestabi

import (
	"encoding/json"
	"unsafe"
)

// pins keeps alloc'd buffers reachable so the Go GC cannot reclaim memory the
// host still references. The wasm linear memory IS this program's heap, so a
// pinned slice's first-element pointer is a stable guest offset. This assumes
// the wasm-targeting Go GC remains non-moving (true for Go 1.21+ wasip1/wasm);
// review this assumption before upgrading the toolchain.
//
// This package is the single extracted copy of the alloc/free/emit/pins ABI that
// was previously duplicated in each parser plugin. A future moving GC (or an
// explicitly reserved arena) would break the pointer-packing trick above and
// must replace this package's internals only — the parsers' wasmexport surface
// stays unchanged.
var pins = map[int32][]byte{}

// Alloc returns a guest pointer to n writable bytes, pinned against GC.
func Alloc(n int32) int32 {
	if n <= 0 {
		n = 1
	}
	b := make([]byte, n)
	p := int32(uintptr(unsafe.Pointer(&b[0])))
	pins[p] = b
	return p
}

// Free releases a previously alloc'd pointer.
func Free(p int32) { delete(pins, p) }

// Lookup returns the buffer pinned at guest pointer p and whether it exists. It
// is the only exported read-back path into the unexported pins map, so a parser's
// parse() can recover its input buffer without touching the map directly.
func Lookup(p int32) ([]byte, bool) {
	b, ok := pins[p]
	return b, ok
}

// Emit marshals v to JSON, pins the result, and returns (resPtr<<32 | resLen).
// On marshal failure it falls back to a minimal error sentinel. It accepts any
// value so every parser can reuse it regardless of its concrete node type.
func Emit(v any) int64 {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{"kind":"error","name":"marshal"}`)
	}
	p := Alloc(int32(len(b)))
	copy(pins[p], b)
	return (int64(p) << 32) | int64(len(b))
}
