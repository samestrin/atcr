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
//
// pins is intentionally uncapped — no size limit, no eviction. Unbounded growth
// is bounded in practice by the astgroup host, which for every call
// unconditionally frees BOTH the input-buffer alloc and the Emit result-buffer
// alloc (see host.go Parse: the deferred free of the input ptr and of the result
// rptr) and discards the whole instance on any guest trap. So pins holds at most
// the allocations in flight for the current call, not one per lifetime call. A
// guest-side cap or LRU eviction is deliberately out of scope (the epic excludes
// allocation-strategy changes) and would be unsafe to add here: eviction has no
// signal for which pins the host still holds, and a rejecting cap would require
// every caller — the parser wrappers and Emit — to check a sentinel none check
// today. (One narrow host-side gap is tracked separately: an oversized result
// >maxResultBytes returns before host.go registers its result-free defer.)
var pins = map[int32][]byte{}

// Alloc returns a guest pointer to n writable bytes, pinned against GC.
//
// Alloc does NOT cap n; an over-large n is left to fail as an ordinary Go
// allocation. The astgroup host already bounds its own call pattern — it rejects
// sources over defaultMaxSourceBytes (8 MiB) before calling alloc, and results
// over maxResultBytes (64 MiB) after Emit — so no host-driven call reaches a
// pathological n. A guest-internal cap is deliberately deferred: it would need a
// coordinated audit of all four call sites (the three parser wrappers and Emit)
// to handle a rejection sentinel none check today, and is out of this sprint's
// allocation-strategy scope.
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
//
// Lookup does NOT bounds-check the length a caller intends to slice from the
// returned buffer: it returns the raw pinned slice as-is. Callers MUST validate
// any n they index buf[:n] against len(buf) (e.g. int(n) < 0 || int(n) > len(buf))
// before slicing — the parsers' parse() keep this guard at the call site, since
// folding it into Lookup would require Lookup to take the length and decide an
// error sentinel, which the wasmexport parse ABI has no room to surface.
func Lookup(p int32) ([]byte, bool) {
	b, ok := pins[p]
	return b, ok
}

// Emit marshals v to JSON, pins the result, and returns (resPtr<<32 | resLen).
// On marshal failure it falls back to a minimal error sentinel. It accepts any
// value so every parser can reuse it regardless of its concrete node type.
//
// Emit pins a FRESH result buffer on every call (via Alloc) and does NOT free it:
// the returned resPtr stays pinned until somebody calls Free(resPtr). The astgroup
// wazero host owns that free — it defers free(rptr) after reading the result bytes
// (see host.go Parse) — because the guest cannot observe when the host has
// finished copying the result out of linear memory. A guest-side reuse/arena
// strategy that avoided per-call pinning is deferred to a future guestabi
// hardening pass; for now the contract is: the caller of Emit does not free, the
// host that decodes the packed return does.
func Emit(v any) int64 {
	b, err := json.Marshal(v)
	if err != nil {
		// Defensive only: every caller passes a node-shaped struct, and
		// json.Marshal cannot fail on those (it fails on chan/func/complex/cyclic
		// values, none of which the node tree contains). This branch is not
		// expected to be exercised by any current parser, so it is left uncovered
		// rather than restructured out of this wasip1-only package for testability.
		b = []byte(`{"kind":"error","name":"marshal"}`)
	}
	p := Alloc(int32(len(b)))
	copy(pins[p], b)
	// int64(p) << 32 is safe even when p (int32) has its high bit set (a guest
	// address >= 2 GiB): the left shift discards int64(p)'s upper 32 bits — where
	// sign extension would live — before they reach the packed high half, so the
	// host's uint32(packed >> 32) reconstructs p's exact bit pattern. Masking with
	// int64(uint32(p)) would be a bit-for-bit no-op, so it is intentionally omitted.
	return (int64(p) << 32) | int64(len(b))
}
