package astgroup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHost_RepeatParseUnder10ms validates the NFR: once a plugin is compiled and
// instantiated (cached), repeated parses of the same language carry <10ms
// overhead. The first Parser() call pays compile+instantiate; subsequent parses
// reuse the live instance.
func TestHost_RepeatParseUnder10ms(t *testing.T) {
	h := NewHost()
	defer h.Close()

	p, err := h.Parser("go")
	require.NoError(t, err)

	src := []byte("package p\nfunc A() { _ = 1 }\nfunc B() { _ = 2 }\n")
	_, err = p.Parse(src) // warm
	require.NoError(t, err)

	const n = 50
	start := time.Now()
	for i := 0; i < n; i++ {
		_, err := p.Parse(src)
		require.NoError(t, err)
	}
	avg := time.Since(start) / n
	require.Less(t, avg, 10*time.Millisecond, "repeat parse averaged %v, want <10ms", avg)
}

// TestHost_RuntimeOverrideNewLanguage proves the "drop in a new .wasm file"
// business criterion: a parser placed in the override directory is loaded for a
// language id that is NOT in the embedded registry.
func TestHost_RuntimeOverrideNewLanguage(t *testing.T) {
	// Reuse the embedded Go parser bytes as a stand-in plugin under a new id.
	wasm, err := parserFS.ReadFile("parsers/go.wasm")
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "golike.wasm"), wasm, 0o644))

	h := NewHost(WithOverrideDir(dir))
	defer h.Close()

	// Not registered as a builtin — only loadable via the override dir.
	_, err = h.Parser("golike")
	require.NoError(t, err)

	root, err := mustParse(t, h, "golike", []byte("package p\nfunc Hi() {}\n"))
	require.NoError(t, err)
	var names []string
	collectFuncNames(root, &names)
	require.Equal(t, []string{"Hi"}, names)
}

// TestHost_OverrideFallsBackToEmbedded confirms that with an override dir set but
// no matching file, the embedded plugin is still used.
func TestHost_OverrideFallsBackToEmbedded(t *testing.T) {
	h := NewHost(WithOverrideDir(t.TempDir()))
	defer h.Close()

	_, err := h.Parser("go") // no go.wasm in override dir → embedded
	require.NoError(t, err)
}

func TestHost_RejectsUnsafeLanguageId(t *testing.T) {
	h := NewHost(WithOverrideDir(t.TempDir()))
	defer h.Close()
	for _, bad := range []string{"../etc/passwd", "a/b", "..", "Go!"} {
		_, err := h.Parser(bad)
		require.Error(t, err, "lang %q must be rejected", bad)
	}
}

func mustParse(t *testing.T, h *Host, lang string, src []byte) (Node, error) {
	t.Helper()
	p, err := h.Parser(lang)
	require.NoError(t, err)
	return p.Parse(src)
}
