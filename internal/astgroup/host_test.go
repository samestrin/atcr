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

func TestLanguageForExt(t *testing.T) {
	require.Equal(t, "go", LanguageForExt(".go"))
	require.Equal(t, "python", LanguageForExt(".py"))
	require.Equal(t, "", LanguageForExt(".rb"))
	require.Equal(t, "", LanguageForExt(""))
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

func TestHost_UnknownLanguage(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()

	_, err := h.Parser("cobol")
	require.Error(t, err)
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
