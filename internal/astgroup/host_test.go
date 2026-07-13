package astgroup

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func collectFuncNames(n Node, out *[]string) {
	if n.Kind == "func" && n.Name != "" {
		*out = append(*out, n.Name)
	}
	for _, c := range n.Children {
		collectFuncNames(c, out)
	}
}

func TestHost_ParseGo(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()

	p, err := h.Parser("go")
	require.NoError(t, err)

	root, err := p.Parse([]byte("package p\n\nfunc A() {}\n\nfunc B() {}\n"))
	require.NoError(t, err)
	require.Equal(t, "file", root.Kind)

	var names []string
	collectFuncNames(root, &names)
	require.ElementsMatch(t, []string{"A", "B"}, names)
}

func TestHost_ParsePython(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()

	p, err := h.Parser("python")
	require.NoError(t, err)

	root, err := p.Parse([]byte("def a():\n    pass\n\nclass C:\n    def m(self):\n        return 1\n"))
	require.NoError(t, err)
	require.Equal(t, "module", root.Kind)

	var names []string
	collectFuncNames(root, &names)
	require.Contains(t, names, "a")
	require.Contains(t, names, "m")
}

func TestHost_ParsePythonMultiLineHeader(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()

	p, err := h.Parser("python")
	require.NoError(t, err)

	// A def whose signature spans multiple physical lines: the first physical
	// line ("def wrapped(") does not end in ':'. The parser must still recognize
	// it as a single function header and nest the body under it, rather than
	// scattering the parameter lines and the body into sibling blocks (which would
	// fabricate covering blocks and mis-group findings on those lines).
	src := "def wrapped(\n" +
		"    a,\n" +
		"    b,\n" +
		"):\n" +
		"    return a + b\n"
	root, err := p.Parse([]byte(src))
	require.NoError(t, err)
	require.Equal(t, "module", root.Kind)

	require.Len(t, root.Children, 1, "multi-line def header must yield exactly one top-level block")
	fn := root.Children[0]
	require.Equal(t, "func", fn.Kind)
	require.Equal(t, "wrapped", fn.Name)
	require.NotEmpty(t, fn.Children, "function body must nest under the folded multi-line def header")
}

func TestLanguageForExt(t *testing.T) {
	require.Equal(t, "go", LanguageForExt(".go"))
	require.Equal(t, "python", LanguageForExt(".py"))
	require.Equal(t, "", LanguageForExt(".rb")) // Ruby is intentionally out of scope
	require.Equal(t, "", LanguageForExt(""))

	// Brace languages (epic 13.4): TypeScript/JavaScript family, PHP, Rust, Bash.
	for _, ext := range []string{".ts", ".tsx", ".cts", ".mts", ".js", ".jsx", ".mjs"} {
		require.Equalf(t, "ts", LanguageForExt(ext), "ext %s should map to ts", ext)
	}
	require.Equal(t, "php", LanguageForExt(".php"))
	require.Equal(t, "rust", LanguageForExt(".rs"))
	require.Equal(t, "bash", LanguageForExt(".sh"))
	require.Equal(t, "bash", LanguageForExt(".bash"))

	// Brace languages (epic 13.6): Java, Kotlin, C/C++, C#.
	require.Equal(t, "java", LanguageForExt(".java"))
	for _, ext := range []string{".kt", ".kts"} {
		require.Equalf(t, "kotlin", LanguageForExt(ext), "ext %s should map to kotlin", ext)
	}
	for _, ext := range []string{".c", ".cpp", ".cc", ".cxx", ".h", ".hpp"} {
		require.Equalf(t, "cpp", LanguageForExt(ext), "ext %s should map to cpp", ext)
	}
	require.Equal(t, "csharp", LanguageForExt(".cs"))
}

// findNode reports whether the tree rooted at n contains a node with the given
// kind and name. An empty want name matches any name for that kind.
func findNode(n Node, kind, name string) bool {
	if n.Kind == kind && (name == "" || n.Name == name) {
		return true
	}
	for _, c := range n.Children {
		if findNode(c, kind, name) {
			return true
		}
	}
	return false
}

// TestHost_BraceParsersLoadAndParse proves each embedded brace .wasm instantiates
// AND that the build-tag-selected per-language keyword table actually reached the
// binary the host loads for that language id — not merely that some .wasm parses.
//
// Every source is language-DISTINCTIVE: each asserts either a keyword-derived
// kind (class/switch/for) or a trailing-identifier name that the shared funcParen
// path alone cannot fabricate. funcParen only ever yields kind "func", so a
// class/switch/for assertion can only pass if that language's keyword table was
// compiled in. This closes the gap the previous all-generic sources left open:
// because `void f()` / `int f()` / `fun f()` all recover a func under ANY brace
// table, the old test would still pass if all eight .wasm files were identical
// copies of one build. The cases below would NOT.
func TestHost_BraceParsersLoadAndParse(t *testing.T) {
	type kn struct{ kind, name string }
	cases := []struct {
		lang string
		src  string
		want []kn // every (kind,name) must be present in the parsed tree
	}{
		// Arrow function => func is a TS-only feature (arrowFunc); under any other
		// brace table this `=>` header degrades to an anonymous block, not a func.
		{"ts", "const add = (a, b) => {\n  const s = a + b\n  return s\n}\n", []kn{{"func", ""}}},
		// `class` keyword + funcParen-named method; the class kind needs the table.
		{"php", "<?php\nclass Repo {\n  function find($id) {\n    return $id;\n  }\n}\n", []kn{{"class", "Repo"}, {"func", "find"}}},
		// `impl` maps to a named class only in the Rust table; funcParen-named fn.
		{"rust", "impl Widget {\n  fn build(&self) -> i32 {\n    let x = 1;\n    x\n  }\n}\n", []kn{{"class", "Widget"}, {"func", "build"}}},
		// `name() {` recovers a named func via funcParen in the Bash table.
		{"bash", "deploy() {\n  local x=1\n  echo $x\n}\n", []kn{{"func", "deploy"}}},
		// `record` -> class is Java-distinctive; the inner `void f()` verifies the
		// trailing-identifier funcParen naming through the compiled wasm.
		{"java", "record Pair() {\n  void f() {\n    g();\n  }\n}\n", []kn{{"class", "Pair"}, {"func", "f"}}},
		// `fun` -> named func and `when` -> switch are both Kotlin table mappings.
		{"kotlin", "fun process(v: Int) {\n  when (v) {\n    1 -> a()\n    else -> b()\n  }\n}\n", []kn{{"func", "process"}, {"switch", ""}}},
		// `struct` -> named class is the C/C++ table mapping.
		{"cpp", "struct Point {\n  int x;\n  int y;\n};\n", []kn{{"class", "Point"}}},
		// funcParen-named method + `foreach` -> for, the C# table mapping.
		{"csharp", "void Run() {\n  foreach (var it in items) {\n    total += it;\n  }\n}\n", []kn{{"func", "Run"}, {"for", ""}}},
	}
	h := NewHost()
	defer func() { _ = h.Close() }()
	for _, c := range cases {
		p, err := h.Parser(c.lang)
		require.NoErrorf(t, err, "parser for %q should be registered", c.lang)
		root, err := p.Parse([]byte(c.src))
		require.NoErrorf(t, err, "parse %q", c.lang)
		require.Equalf(t, "file", root.Kind, "%q root kind", c.lang)
		for _, w := range c.want {
			require.Truef(t, findNode(root, w.kind, w.name),
				"%q: expected a %q node named %q (proves the %q keyword table reached the wasm), got %+v",
				c.lang, w.kind, w.name, c.lang, root.Children)
		}
	}
}

func TestHost_ParseInvalidGoReturnsError(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)

	// Not valid Go (no package clause): plugin emits an "error" node and Parse
	// surfaces it as an error so the caller falls back to line proximity.
	_, err = p.Parse([]byte("this is not go source"))
	require.Error(t, err)
}

func TestHost_ParseEmptySource(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("python")
	require.NoError(t, err)

	root, err := p.Parse(nil)
	require.NoError(t, err)
	require.Equal(t, "module", root.Kind)
}

// TestHost_ParseEmptySourceGo pins the Go plugin's empty-input contract
// symmetrically with the Python one above: empty/nil source is a bare root node
// ("file"), not a parse error. Empty Go source has no declarations, so treating
// it as an empty tree (rather than erroring out and forcing line-proximity
// fallback) keeps the empty-input behavior consistent across plugins.
func TestHost_ParseEmptySourceGo(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)

	for _, src := range [][]byte{nil, {}} {
		root, err := p.Parse(src)
		require.NoError(t, err)
		require.Equal(t, "file", root.Kind)
	}
}

func TestHost_UnknownLanguage(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()

	_, err := h.Parser("cobol")
	require.Error(t, err)
}

func TestHost_PyParseTabIndentColumn(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("python")
	require.NoError(t, err)

	// Line 2 is indented with space+tab — the tab advances to the next multiple
	// of 8, so the column is 8; line 3 uses 8 spaces (also column 8). Both must
	// sit at the same level (children of the `if`). The old flat tab=+8 made
	// space+tab column 9, splitting the two lines across levels.
	root, err := p.Parse([]byte("if x:\n \ta = 1\n        b = 2\n"))
	require.NoError(t, err)
	require.Len(t, root.Children, 1, "the if is the only top-level block")
	ifNode := root.Children[0]
	require.Equal(t, "if", ifNode.Kind)
	require.Len(t, ifNode.Children, 2, "both equally-indented body lines are children of the if")
}

func TestHost_PyParseColonNotHeaderUnlessCompound(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("python")
	require.NoError(t, err)

	// A bare `"key":` line ends with ':' but is not a compound-statement header,
	// so it must NOT adopt the following indented line as a child block.
	root, err := p.Parse([]byte("x = 1\n\"key\":\n    y = 2\n"))
	require.NoError(t, err)
	var keyLine *Node
	for i := range root.Children {
		if root.Children[i].StartLine == 2 {
			keyLine = &root.Children[i]
		}
	}
	require.NotNil(t, keyLine, `the "key": line is a top-level node`)
	require.Empty(t, keyLine.Children, "a non-compound colon line must not open a block")
}

func TestHost_ParseHonorsTimeout(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)

	// A pathological source must not be able to hang a parser indefinitely: with
	// an impossibly small per-parse budget, Parse must surface a timeout error
	// instead of running the guest to completion.
	old := parseTimeout
	parseTimeout = time.Nanosecond
	defer func() { parseTimeout = old }()

	_, err = p.Parse([]byte("package p\nfunc A() {}\n"))
	require.Error(t, err, "Parse must enforce parseTimeout and abort the guest call")
}

func TestHost_ParserDiscardedAfterParseTrap(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)

	// Force a guest trap via an impossibly small per-parse budget.
	old := parseTimeout
	parseTimeout = time.Nanosecond
	_, err = p.Parse([]byte("package p\nfunc A() {}\n"))
	parseTimeout = old
	require.Error(t, err)

	// A trapped instance may have an inconsistent pin map / linear memory, so it
	// must be discarded: its module is closed and the next Parser call returns a
	// fresh, usable instance rather than reusing the poisoned one.
	require.True(t, p.(*wasmParser).mod.IsClosed(), "trapped parser module must be closed")

	p2, err := h.Parser("go")
	require.NoError(t, err)
	require.NotSame(t, p.(*wasmParser), p2.(*wasmParser), "a fresh instance must replace the trapped parser")
	root, err := p2.Parse([]byte("package p\nfunc A() {}\n"))
	require.NoError(t, err)
	require.Equal(t, "file", root.Kind)
}

func TestHost_MaxSourceBytesConfigurable(t *testing.T) {
	h := NewHost(WithMaxSourceBytes(16))
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)

	// Source within the custom limit parses successfully.
	_, err = p.Parse([]byte("package p\n"))
	require.NoError(t, err)

	// Source above the custom limit is rejected.
	_, err = p.Parse([]byte("package p\nfunc A() {}\nfunc B() {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "source too large")
}

func TestHost_MaxSourceBytesZeroRejects(t *testing.T) {
	h := NewHost(WithMaxSourceBytes(0))
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)

	// A zero byte limit must not be bypassed by the empty-source alloc(1) path.
	_, err = p.Parse(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "maxSourceBytes must be positive")
}

func TestHost_MemoryLimitFailsGracefully(t *testing.T) {
	// A tiny memory limit must make the parser fail gracefully — a returned error
	// at instantiate or parse time — rather than panicking or letting the guest
	// balloon host memory. One page (64 KiB) is far below a Go wasm runtime's need.
	h := NewHost(WithMaxMemoryPages(1))
	defer func() { _ = h.Close() }()

	p, err := h.Parser("go")
	if err != nil {
		// Instantiation refused under the limit: graceful.
		return
	}
	_, err = p.Parse([]byte("package p\nfunc A() {}\n"))
	require.Error(t, err, "parse under an unsatisfiable memory limit must error, not OOM/panic")
}

func TestHost_MemoryLimitAllowsNormalParse(t *testing.T) {
	// The default memory limit must not interfere with ordinary parsing.
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)
	root, err := p.Parse([]byte("package p\nfunc A() {}\nfunc B() {}\n"))
	require.NoError(t, err)
	require.Equal(t, "file", root.Kind)
}

func TestHost_ParserCachedAndReused(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()

	p1, err := h.Parser("go")
	require.NoError(t, err)
	p2, err := h.Parser("go")
	require.NoError(t, err)
	require.Same(t, p1, p2, "parser instance must be cached and reused (compiled-module cache)")

	// Reused instance stays correct across repeated parses.
	for i := 0; i < 3; i++ {
		root, err := p1.Parse([]byte("package p\nfunc Z() {}\n"))
		require.NoError(t, err)
		var names []string
		collectFuncNames(root, &names)
		require.Equal(t, []string{"Z"}, names)
	}
}

func TestHost_CloseDrainsInFlightParse(t *testing.T) {
	h := NewHost()
	p, err := h.Parser("go")
	require.NoError(t, err)

	// Start a parse and immediately close the host. Close must wait for the
	// parse to finish (or see the closed flag) rather than racing the module.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = p.Parse([]byte("package p\nfunc A() {}\n"))
	}()

	require.NoError(t, h.Close())
	<-done
}

func TestHost_ParserAfterClose(t *testing.T) {
	h := NewHost()
	_, err := h.Parser("go")
	require.NoError(t, err)
	require.NoError(t, h.Close())

	// A second Close must be safe (idempotent).
	require.NoError(t, h.Close())

	// Parser calls after Close must return a clear error instead of a closed
	// module that fails opaquely inside parse.Call.
	_, err = h.Parser("go")
	require.Error(t, err)
	require.Contains(t, err.Error(), "closed")
}

// TestHost_BraceParseBadPointerAndNegativeN exercises braceparser's parse() error
// path directly: a never-allocated pointer (Lookup returns false) and a
// negative n must both yield the "bad pointer" error node rather than trapping
// the guest. The host's public Parse always allocates a valid pointer and passes
// len(src) >= 0, so these ABI-level guards are only reachable by calling the
// exported parse function with a bogus pointer / negative length directly
// (same internal-access pattern as TestHost_ParserDiscardedAfterParseTrap).
func TestHost_BraceParseBadPointerAndNegativeN(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("ts")
	require.NoError(t, err)
	wp := p.(*wasmParser)
	ctx := wp.ctx

	decode := func(packed uint64) Node {
		rptr := uint32(packed >> 32)
		rlen := uint32(packed)
		out, ok := wp.memory.Read(rptr, rlen)
		require.True(t, ok, "read error-node result from guest memory")
		// Free the result buffer the guest pinned for this error node so the
		// test does not leak guest pins across the two sub-cases.
		_, _ = wp.free.Call(ctx, uint64(rptr))
		var n Node
		require.NoError(t, json.Unmarshal(out, &n))
		return n
	}

	// 1. Bogus pointer: never alloc'd, so Lookup returns false -> error node.
	res, err := wp.parse.Call(ctx, 99999, 0)
	require.NoError(t, err, "bad pointer must return an error node, not trap")
	require.Len(t, res, 1)
	n := decode(res[0])
	require.Equal(t, "error", n.Kind)
	require.Equal(t, "bad pointer", n.Name)

	// 2. Valid pointer but negative n: the n < 0 guard must reject it. 0xFFFFFFFF
	// is the int32 bit pattern for -1 (wazero takes the low 32 bits of the i64
	// arg for the i32 parameter).
	allocRes, err := wp.alloc.Call(ctx, 1)
	require.NoError(t, err)
	ptr := uint32(allocRes[0])
	defer func() { _, _ = wp.free.Call(ctx, uint64(ptr)) }()
	res, err = wp.parse.Call(ctx, uint64(ptr), 0xFFFFFFFF)
	require.NoError(t, err, "negative n must return an error node, not trap")
	require.Len(t, res, 1)
	n = decode(res[0])
	require.Equal(t, "error", n.Kind)
	require.Equal(t, "bad pointer", n.Name)
}

// TestHost_GoParseBadPointer exercises goparser's parse() bad-pointer path
// directly: a never-allocated pointer (Lookup returns false) must yield the
// "bad pointer" error node rather than trapping the guest. The host's public
// Parse always allocates a valid pointer, so this ABI guard is only reachable
// by calling the exported parse function with a bogus pointer directly. (The
// negative-n path is covered by TestHost_GoParseNegativeN.)
func TestHost_GoParseBadPointer(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)
	wp := p.(*wasmParser)
	ctx := wp.ctx

	res, err := wp.parse.Call(ctx, 99999, 0)
	require.NoError(t, err, "bad pointer must return an error node, not trap")
	require.Len(t, res, 1)
	rptr := uint32(res[0] >> 32)
	rlen := uint32(res[0])
	out, ok := wp.memory.Read(rptr, rlen)
	require.True(t, ok, "read error-node result from guest memory")
	_, _ = wp.free.Call(ctx, uint64(rptr))
	var n Node
	require.NoError(t, json.Unmarshal(out, &n))
	require.Equal(t, "error", n.Kind)
	require.Equal(t, "bad pointer", n.Name)
}

// TestHost_PyParseBadPointer exercises pyparser's parse() bad-pointer path
// directly: a never-allocated pointer (Lookup returns false) must yield the
// "bad pointer" error node rather than trapping the guest. The host's public
// Parse always allocates a valid pointer, so this ABI guard is only reachable
// by calling the exported parse function with a bogus pointer directly. (The
// negative-n path is covered by TestHost_PyParseNegativeN.)
func TestHost_PyParseBadPointer(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("python")
	require.NoError(t, err)
	wp := p.(*wasmParser)
	ctx := wp.ctx

	res, err := wp.parse.Call(ctx, 99999, 0)
	require.NoError(t, err, "bad pointer must return an error node, not trap")
	require.Len(t, res, 1)
	rptr := uint32(res[0] >> 32)
	rlen := uint32(res[0])
	out, ok := wp.memory.Read(rptr, rlen)
	require.True(t, ok, "read error-node result from guest memory")
	_, _ = wp.free.Call(ctx, uint64(rptr))
	var n Node
	require.NoError(t, json.Unmarshal(out, &n))
	require.Equal(t, "error", n.Kind)
	require.Equal(t, "bad pointer", n.Name)
}

// TestHost_GoParseNegativeN exercises goparser's parse() negative-n guard: a
// valid pointer with a negative n (int32 -1, passed as 0xFFFFFFFF over the i64
// arg) must yield the "bad pointer" error node rather than slicing buf[:n] and
// trapping the guest with slice-out-of-range. This mirrors braceparser's
// negative-n sub-case (TestHost_BraceParseBadPointerAndNegativeN) — goparser
// previously lacked the `int(n) < 0` guard, so this pins the fix.
func TestHost_GoParseNegativeN(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)
	wp := p.(*wasmParser)
	ctx := wp.ctx

	allocRes, err := wp.alloc.Call(ctx, 1)
	require.NoError(t, err)
	ptr := uint32(allocRes[0])
	defer func() { _, _ = wp.free.Call(ctx, uint64(ptr)) }()

	res, err := wp.parse.Call(ctx, uint64(ptr), 0xFFFFFFFF)
	require.NoError(t, err, "negative n must return an error node, not trap")
	require.Len(t, res, 1)
	rptr := uint32(res[0] >> 32)
	rlen := uint32(res[0])
	out, ok := wp.memory.Read(rptr, rlen)
	require.True(t, ok, "read error-node result from guest memory")
	_, _ = wp.free.Call(ctx, uint64(rptr))
	var n Node
	require.NoError(t, json.Unmarshal(out, &n))
	require.Equal(t, "error", n.Kind)
	require.Equal(t, "bad pointer", n.Name)
}

// TestHost_PyParseNegativeN exercises pyparser's parse() negative-n guard: a
// valid pointer with a negative n must yield the "bad pointer" error node rather
// than slicing string(buf[:n]) and trapping. pyparser previously lacked the
// `int(n) < 0` guard, so this pins the fix.
func TestHost_PyParseNegativeN(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("python")
	require.NoError(t, err)
	wp := p.(*wasmParser)
	ctx := wp.ctx

	allocRes, err := wp.alloc.Call(ctx, 1)
	require.NoError(t, err)
	ptr := uint32(allocRes[0])
	defer func() { _, _ = wp.free.Call(ctx, uint64(ptr)) }()

	res, err := wp.parse.Call(ctx, uint64(ptr), 0xFFFFFFFF)
	require.NoError(t, err, "negative n must return an error node, not trap")
	require.Len(t, res, 1)
	rptr := uint32(res[0] >> 32)
	rlen := uint32(res[0])
	out, ok := wp.memory.Read(rptr, rlen)
	require.True(t, ok, "read error-node result from guest memory")
	_, _ = wp.free.Call(ctx, uint64(rptr))
	var n Node
	require.NoError(t, json.Unmarshal(out, &n))
	require.Equal(t, "error", n.Kind)
	require.Equal(t, "bad pointer", n.Name)
}

// TestHost_FreeInvalidatesPointer proves guestabi.Free actually invalidates a
// pinned pointer: after alloc(n) then free(ptr), a parse(ptr, ...) can no longer
// resolve the buffer (Lookup returns !ok) and returns the "bad pointer" error
// node. This closes the coverage gap where Free was never verified to remove the
// pin — a leak or a no-op Free would otherwise pass every existing test.
func TestHost_FreeInvalidatesPointer(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)
	wp := p.(*wasmParser)
	ctx := wp.ctx

	allocRes, err := wp.alloc.Call(ctx, 8)
	require.NoError(t, err)
	ptr := uint32(allocRes[0])

	// Free the pin, then parse the now-dangling pointer. Lookup must miss.
	_, err = wp.free.Call(ctx, uint64(ptr))
	require.NoError(t, err)

	res, err := wp.parse.Call(ctx, uint64(ptr), 0)
	require.NoError(t, err, "parse of freed pointer must return an error node, not trap")
	require.Len(t, res, 1)
	rptr := uint32(res[0] >> 32)
	rlen := uint32(res[0])
	out, ok := wp.memory.Read(rptr, rlen)
	require.True(t, ok, "read error-node result from guest memory")
	_, _ = wp.free.Call(ctx, uint64(rptr))
	var n Node
	require.NoError(t, json.Unmarshal(out, &n))
	require.Equal(t, "error", n.Kind)
	require.Equal(t, "bad pointer", n.Name)
}

// TestHost_OversizedResultRejectsWithoutTrapping exercises Parse's
// maxResultBytes reject path: shrinking the cap makes an ordinary small parse
// trip the guard, without needing a >64 MiB result. It asserts the path returns
// the "result too large" error and — crucially — that it returns CLEANLY (no
// guest trap), so discardOnTrap does not fire and the SAME instance still parses
// afterward. The reject path is where the result-pointer pin was leaked (the free
// defer used to sit after the size-check return); the fix moves that defer ahead
// of the return. The leaked pin itself is not host-observable (the guest pins map
// is private), so this test guards the reject branch's reachability and its
// non-trapping, instance-reusing contract that the pin-free now rides on.
func TestHost_OversizedResultRejectsWithoutTrapping(t *testing.T) {
	orig := maxResultBytes
	maxResultBytes = 8 // any real parse result exceeds this
	defer func() { maxResultBytes = orig }()

	h := NewHost()
	defer func() { _ = h.Close() }()
	p, err := h.Parser("go")
	require.NoError(t, err)

	_, err = p.Parse([]byte("package p\nfunc F() {}\n"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "result too large")

	// Restore the cap: a trapped/discarded instance would fail here; a clean
	// reject leaves the reused instance fully usable.
	maxResultBytes = orig
	root, err := p.Parse([]byte("package p\nfunc G() {}\n"))
	require.NoError(t, err)
	require.Equal(t, "file", root.Kind)
}
