package astgroup

import (
	"testing"

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
	defer h.Close()

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
	defer h.Close()

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
	defer h.Close()
	p, err := h.Parser("go")
	require.NoError(t, err)

	// Not valid Go (no package clause): plugin emits an "error" node and Parse
	// surfaces it as an error so the caller falls back to line proximity.
	_, err = p.Parse([]byte("this is not go source"))
	require.Error(t, err)
}

func TestHost_ParseEmptySource(t *testing.T) {
	h := NewHost()
	defer h.Close()
	p, err := h.Parser("python")
	require.NoError(t, err)

	root, err := p.Parse(nil)
	require.NoError(t, err)
	require.Equal(t, "module", root.Kind)
}

func TestHost_UnknownLanguage(t *testing.T) {
	h := NewHost()
	defer h.Close()

	_, err := h.Parser("cobol")
	require.Error(t, err)
}

func TestHost_ParserCachedAndReused(t *testing.T) {
	h := NewHost()
	defer h.Close()

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
