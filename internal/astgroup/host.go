package astgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	ctx            context.Context
	runtime        wazero.Runtime
	overrideDir    string
	maxSourceBytes int
	maxMemoryPages uint32
	initErr        error // non-nil if WASI init failed; Parser then errors instead of panicking

	mu      sync.RWMutex
	closed  bool
	parsers map[string]*wasmParser
	parseWG sync.WaitGroup
}

// Option configures a Host at construction.
type Option func(*Host)

// WithOverrideDir makes the Host consult dir for a "<lang>.wasm" plugin before
// falling back to the embedded set. This is the runtime "drop in a new .wasm
// file" mechanism: a file placed in dir enables a language that need not be in
// the embedded registry, and shadows an embedded plugin of the same id.
func WithOverrideDir(dir string) Option {
	return func(h *Host) { h.overrideDir = dir }
}

// WithMaxSourceBytes sets the maximum source size a parser plugin will accept.
// Larger files fall back to line-proximity grouping. The default is 8 MiB.
func WithMaxSourceBytes(n int) Option {
	return func(h *Host) { h.maxSourceBytes = n }
}

// WithMaxMemoryPages caps the wasm linear memory (in 64 KiB pages) any parser
// instance may grow to. It bounds host RSS so a hostile or buggy plugin —
// especially an override-loaded one — cannot balloon toward wazero's multi-
// hundred-MiB/4 GiB default ceiling. The default is defaultMaxMemoryPages.
func WithMaxMemoryPages(pages uint32) Option {
	return func(h *Host) { h.maxMemoryPages = pages }
}

// NewHost creates a Host with a fresh wazero runtime (pure Go, zero CGO) and WASI
// preview1 imports instantiated. Call Close to release it. A WASI-init failure is
// recorded rather than panicked: Parser then returns that error, so a host wired
// into the production reconcile gate degrades to proximity grouping instead of
// crashing the reconcile.
func NewHost(opts ...Option) *Host {
	ctx := context.Background()
	h := &Host{ctx: ctx, maxSourceBytes: defaultMaxSourceBytes, maxMemoryPages: defaultMaxMemoryPages, parsers: map[string]*wasmParser{}}
	// Apply options before building the runtime so memory limits configured via
	// WithMaxMemoryPages take effect on the runtime config itself.
	for _, opt := range opts {
		opt(h)
	}
	cfg := wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(h.maxMemoryPages)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	h.runtime = rt
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		h.initErr = fmt.Errorf("astgroup: WASI init: %w", err)
	}
	return h
}

var (
	sharedHostOnce sync.Once
	sharedHost     *Host
)

// SharedHost returns the process-lifetime Host: a single wazero runtime whose
// compiled-and-instantiated parser plugins are amortized across every reconcile in
// a long-lived process (e.g. the MCP server), instead of paying full WASI plus
// per-language CompileModule/InstantiateModule on each RunReconcile call. It is
// constructed lazily on first use, so a process that never groups a parseable
// finding never instantiates the runtime. Host is safe for concurrent use, so the
// singleton may back concurrent Groupers. It is intentionally never closed — its
// lifetime is the process's, and the OS reclaims the runtime on exit; closing it
// would discard the compiled-module cache this singleton exists to preserve.
func SharedHost() *Host {
	sharedHostOnce.Do(func() { sharedHost = NewHost() })
	return sharedHost
}

// safeLang reports whether lang is a safe parser id: a non-empty run of
// lowercase letters, digits, and -_+ only. This keeps lang usable as a filename
// component and blocks path traversal (e.g. "../../etc") through the override dir.
func safeLang(lang string) bool {
	if lang == "" {
		return false
	}
	for _, r := range lang {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '+':
		default:
			return false
		}
	}
	return true
}

// loadWasm resolves the .wasm bytes for lang: an override-dir file takes
// precedence over the embedded registry. Returns an error if neither has it.
func (h *Host) loadWasm(lang string) ([]byte, error) {
	if !safeLang(lang) {
		return nil, fmt.Errorf("astgroup: invalid language id %q", lang)
	}
	if h.overrideDir != "" {
		p := filepath.Join(h.overrideDir, lang+".wasm")
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("astgroup: read override %s: %w", p, err)
		}
	}
	path, ok := builtinParsers[lang]
	if !ok {
		return nil, fmt.Errorf("astgroup: no parser plugin for language %q", lang)
	}
	b, err := parserFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("astgroup: read embedded %s: %w", path, err)
	}
	return b, nil
}

// Parser returns the cached parser for lang, compiling and instantiating its
// embedded .wasm plugin on first use. It errors if no plugin is registered for
// lang. The returned Parser is reused across calls (pointer-stable per language).
func (h *Host) Parser(lang string) (Parser, error) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, fmt.Errorf("astgroup: host is closed")
	}
	if h.initErr != nil {
		h.mu.RUnlock()
		return nil, h.initErr
	}
	if p, ok := h.parsers[lang]; ok && !p.mod.IsClosed() {
		h.mu.RUnlock()
		return p, nil
	}
	h.mu.RUnlock()

	// Not cached or module was closed: instantiate under write lock.
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, fmt.Errorf("astgroup: host is closed")
	}
	if p, ok := h.parsers[lang]; ok && !p.mod.IsClosed() {
		return p, nil
	}
	delete(h.parsers, lang)

	wasm, err := h.loadWasm(lang)
	if err != nil {
		return nil, err
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
		ctx:            h.ctx,
		mod:            mod,
		alloc:          mod.ExportedFunction("alloc"),
		free:           mod.ExportedFunction("free"),
		parse:          mod.ExportedFunction("parse"),
		memory:         mod.Memory(),
		maxSourceBytes: h.maxSourceBytes,
		host:           h,
	}
	if p.alloc == nil || p.free == nil || p.parse == nil || p.memory == nil {
		return nil, fmt.Errorf("astgroup: plugin %s missing required exports", lang)
	}
	h.parsers[lang] = p
	return p, nil
}

// Close releases the wazero runtime and all instantiated parsers. It is safe
// to call multiple times; subsequent calls return nil. Close drains any
// in-flight Parse calls before tearing down the runtime.
func (h *Host) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.parsers = nil
	h.mu.Unlock()

	// Drain in-flight parses before closing the runtime.
	h.parseWG.Wait()
	return h.runtime.Close(h.ctx)
}

// wasmParser wraps one instantiated parser plugin. A wasm module instance is not
// safe for concurrent calls, so every Parse is serialized by mu. It holds a
// reference to its Host so Parse can participate in Host.Close draining.
type wasmParser struct {
	ctx            context.Context
	mod            api.Module
	alloc          api.Function
	free           api.Function
	parse          api.Function
	memory         api.Memory
	maxSourceBytes int
	host           *Host

	mu sync.Mutex
}

// defaultMaxSourceBytes bounds the source a plugin will parse by default. Files
// larger than this are pathological for code review; rejecting them (the caller
// falls back to line-proximity grouping) caps guest memory and parse time without
// needing a full execution-timeout watchdog.
const defaultMaxSourceBytes = 1 << 23 // 8 MiB

// defaultMaxMemoryPages caps each parser instance's wasm linear memory. 4096
// pages × 64 KiB = 256 MiB: ample for any source small enough to parse within
// parseTimeout (a 2 MiB source already exhausts the time budget first), yet 16×
// below wazero's ~4 GiB default ceiling so a runaway plugin cannot exhaust host
// memory.
const defaultMaxMemoryPages uint32 = 1 << 12 // 4096 pages = 256 MiB

// parseTimeout bounds a single guest parse call. Paired with the runtime's
// WithCloseOnContextDone, an exceeded deadline aborts and closes the offending
// instance; the caller (Grouper) then falls back to line-proximity grouping for
// that language rather than hanging the reconcile on a pathological source. It is
// a var, not a const, only so a test can shrink it to force the timeout path.
var parseTimeout = 5 * time.Second

// maxResultBytes caps the JSON a guest parser may return. This prevents a
// hostile or buggy plugin from claiming a multi-gigabyte result length and
// driving the host out of memory while reading wasm memory.
const maxResultBytes = 1 << 26 // 64 MiB

func (p *wasmParser) Parse(src []byte) (Node, error) {
	if p.maxSourceBytes <= 0 {
		return Node{}, fmt.Errorf("astgroup: maxSourceBytes must be positive")
	}
	if len(src) > p.maxSourceBytes {
		slog.Warn("astgroup: source too large, falling back to proximity grouping", "size", len(src), "max", p.maxSourceBytes)
		return Node{}, fmt.Errorf("astgroup: source too large (%d bytes > %d)", len(src), p.maxSourceBytes)
	}

	p.host.mu.RLock()
	if p.host.closed {
		p.host.mu.RUnlock()
		return Node{}, fmt.Errorf("astgroup: host is closed")
	}
	p.host.parseWG.Add(1)
	p.host.mu.RUnlock()
	defer p.host.parseWG.Done()

	p.mu.Lock()
	defer p.mu.Unlock()

	ctx, cancel := context.WithTimeout(p.ctx, parseTimeout)
	defer cancel()

	n := uint32(len(src))
	if n == 0 {
		// alloc rejects zero, so request one byte. The plugin itself decides what
		// an empty source means: some languages return a bare root node, others
		// emit an error node. Either way the caller falls back to proximity if
		// Parse returns an error.
		n = 1
	}

	res, err := p.alloc.Call(ctx, uint64(n))
	if err != nil {
		return Node{}, fmt.Errorf("astgroup: alloc: %w", err)
	}
	if len(res) == 0 {
		return Node{}, fmt.Errorf("astgroup: alloc returned no results")
	}
	ptr := uint32(res[0])
	defer func() { _, _ = p.free.Call(ctx, uint64(ptr)) }()

	if len(src) > 0 {
		if !p.memory.Write(ptr, src) {
			return Node{}, fmt.Errorf("astgroup: write src out of range (ptr=%d len=%d)", ptr, len(src))
		}
	}

	pr, err := p.parse.Call(ctx, uint64(ptr), uint64(len(src)))
	if err != nil {
		return Node{}, fmt.Errorf("astgroup: parse call: %w", err)
	}
	if len(pr) == 0 {
		return Node{}, fmt.Errorf("astgroup: parse returned no results")
	}
	packed := pr[0]
	rptr := uint32(packed >> 32)
	rlen := uint32(packed)
	if rlen > maxResultBytes {
		return Node{}, fmt.Errorf("astgroup: parser result too large (%d bytes > %d)", rlen, maxResultBytes)
	}
	defer func() { _, _ = p.free.Call(ctx, uint64(rptr)) }()

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
