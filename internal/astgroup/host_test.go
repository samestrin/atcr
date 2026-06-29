package astgroup

import (
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
}

// TestHost_BraceParsersLoadAndParse proves each embedded brace .wasm instantiates
// and recovers a function block for representative source of its language — the
// end-to-end check that the build-tag-selected table reached the binary the host
// loads for that language id.
func TestHost_BraceParsersLoadAndParse(t *testing.T) {
	cases := []struct{ lang, src string }{
		{"ts", "function f() {\n  const x = 1\n  return x\n}\n"},
		{"php", "<?php\nfunction f() {\n  $x = 1;\n  return $x;\n}\n"},
		{"rust", "fn f() -> i32 {\n  let x = 1;\n  x\n}\n"},
		{"bash", "f() {\n  local x=1\n  echo $x\n}\n"},
	}
	h := NewHost()
	defer func() { _ = h.Close() }()
	for _, c := range cases {
		p, err := h.Parser(c.lang)
		require.NoErrorf(t, err, "parser for %q should be registered", c.lang)
		root, err := p.Parse([]byte(c.src))
		require.NoErrorf(t, err, "parse %q", c.lang)
		require.Equalf(t, "file", root.Kind, "%q root kind", c.lang)
		var hasFunc bool
		for _, ch := range root.Children {
			if ch.Kind == "func" {
				hasFunc = true
			}
		}
		require.Truef(t, hasFunc, "%q: expected a func block, got %+v", c.lang, root.Children)
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
