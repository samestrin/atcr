package astgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Parser parses source bytes into a structural Node tree. Implementations are
// safe for concurrent use by the caller (calls into a single wasm instance are
// serialized internally).
type Parser interface {
	Parse(src []byte) (Node, error)
}

// Host owns a wazero runtime and a cache of compiled, instantiated parser
// plugins keyed by language. A parser is compiled and instantiated at most once
// per language (the compiled-module + live-instance cache that satisfies the
// <10ms repeat-parse NFR) and reused for every subsequent parse. Host is safe
// for concurrent use.
type Host struct {
	ctx     context.Context
	runtime wazero.Runtime

	mu      sync.Mutex
	parsers map[string]*wasmParser
}

// NewHost creates a Host with a fresh wazero runtime (pure Go, zero CGO) and WASI
// preview1 imports instantiated. Call Close to release it.
func NewHost() *Host {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	return &Host{ctx: ctx, runtime: rt, parsers: map[string]*wasmParser{}}
}

// Parser returns the cached parser for lang, compiling and instantiating its
// embedded .wasm plugin on first use. It errors if no plugin is registered for
// lang. The returned Parser is reused across calls (pointer-stable per language).
func (h *Host) Parser(lang string) (Parser, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if p, ok := h.parsers[lang]; ok {
		return p, nil
	}

	path, ok := builtinParsers[lang]
	if !ok {
		return nil, fmt.Errorf("astgroup: no parser plugin for language %q", lang)
	}
	wasm, err := parserFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("astgroup: read embedded %s: %w", path, err)
	}

	compiled, err := h.runtime.CompileModule(h.ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("astgroup: compile %s: %w", lang, err)
	}
	// Reactor module: run _initialize (set up the Go runtime) but not _start.
	cfg := wazero.NewModuleConfig().WithStartFunctions("_initialize").WithName(lang)
	mod, err := h.runtime.InstantiateModule(h.ctx, compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("astgroup: instantiate %s: %w", lang, err)
	}

	p := &wasmParser{
		ctx:    h.ctx,
		mod:    mod,
		alloc:  mod.ExportedFunction("alloc"),
		free:   mod.ExportedFunction("free"),
		parse:  mod.ExportedFunction("parse"),
		memory: mod.Memory(),
	}
	if p.alloc == nil || p.free == nil || p.parse == nil || p.memory == nil {
		return nil, fmt.Errorf("astgroup: plugin %s missing required exports", lang)
	}
	h.parsers[lang] = p
	return p, nil
}

// Close releases the wazero runtime and all instantiated parsers.
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runtime.Close(h.ctx)
}

// wasmParser wraps one instantiated parser plugin. A wasm module instance is not
// safe for concurrent calls, so every Parse is serialized by mu.
type wasmParser struct {
	ctx    context.Context
	mod    api.Module
	alloc  api.Function
	free   api.Function
	parse  api.Function
	memory api.Memory

	mu sync.Mutex
}

// maxSourceBytes bounds the source a plugin will parse. Files larger than this
// are pathological for code review; rejecting them (the caller falls back to
// line-proximity grouping) caps guest memory and parse time without needing a
// full execution-timeout watchdog.
const maxSourceBytes = 1 << 23 // 8 MiB

func (p *wasmParser) Parse(src []byte) (Node, error) {
	if len(src) > maxSourceBytes {
		return Node{}, fmt.Errorf("astgroup: source too large (%d bytes > %d)", len(src), maxSourceBytes)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	n := uint32(len(src))
	if n == 0 {
		n = 1 // alloc rejects zero; an empty source still yields a root node
	}

	res, err := p.alloc.Call(p.ctx, uint64(n))
	if err != nil {
		return Node{}, fmt.Errorf("astgroup: alloc: %w", err)
	}
	ptr := uint32(res[0])
	defer p.free.Call(p.ctx, uint64(ptr))

	if len(src) > 0 {
		if !p.memory.Write(ptr, src) {
			return Node{}, fmt.Errorf("astgroup: write src out of range (ptr=%d len=%d)", ptr, len(src))
		}
	}

	pr, err := p.parse.Call(p.ctx, uint64(ptr), uint64(len(src)))
	if err != nil {
		return Node{}, fmt.Errorf("astgroup: parse call: %w", err)
	}
	packed := pr[0]
	rptr := uint32(packed >> 32)
	rlen := uint32(packed)
	defer p.free.Call(p.ctx, uint64(rptr))

	out, ok := p.memory.Read(rptr, rlen)
	if !ok {
		return Node{}, fmt.Errorf("astgroup: read result out of range (ptr=%d len=%d)", rptr, rlen)
	}

	var root Node
	if err := json.Unmarshal(out, &root); err != nil {
		return Node{}, fmt.Errorf("astgroup: decode node tree: %w", err)
	}
	if root.Kind == "error" {
		return root, fmt.Errorf("astgroup: parser error: %s", root.Name)
	}
	return root, nil
}
