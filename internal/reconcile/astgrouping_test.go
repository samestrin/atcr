package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/astgroup"
	"github.com/stretchr/testify/require"
)

func TestASTGrouperFor_GroupsDriftedGoFindings(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc F() {\n\ta := 1\n\tb := 2\n\tc := 3\n\td := 4\n\t_ = a + b + c + d\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644))

	g, cleanup := astGrouperFor(dir)
	defer cleanup()
	require.NotNil(t, g)

	// Lines 4 and 8 are 4 apart — beyond the ±3 proximity gate — but in the same
	// function body, so AST grouping keys them together.
	k1 := g.GroupKey(Finding{File: "x.go", Line: 4})
	k2 := g.GroupKey(Finding{File: "x.go", Line: 8})
	require.NotEmpty(t, k1)
	require.Equal(t, k1, k2)
}

func TestASTGrouperFor_DisabledByEnv(t *testing.T) {
	t.Setenv("ATCR_DISABLE_AST_GROUPING", "1")
	g, cleanup := astGrouperFor(t.TempDir())
	defer cleanup()
	require.Nil(t, g, "env opt-out reverts to proximity-only (nil grouper)")
}

func TestASTGrouperFor_FalsyEnvKeepsGroupingOn(t *testing.T) {
	// The opt-out is parsed as a boolean: only a truthy value disables grouping.
	// A falsy or unparseable value must KEEP grouping on, guarding against the
	// presence-only footgun where =false / =0 silently reverted to proximity.
	for _, v := range []string{"0", "false", "FALSE", "no-such-bool"} {
		t.Run(v, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "x.go"), []byte("package p\n"), 0o644))
			t.Setenv("ATCR_DISABLE_AST_GROUPING", v)
			g, cleanup := astGrouperFor(dir)
			defer cleanup()
			require.NotNil(t, g, "value %q must keep AST grouping on", v)
		})
	}
}

func TestASTGrouperFor_LazyInitOnSupportedExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package p\n"), 0o644))

	lg := newLazyGrouper(dir)
	var constructed bool
	lg.newGrouper = func(root string) *astgroup.Grouper {
		constructed = true
		return astgroup.NewGrouper(root)
	}

	// Unsupported extension: short-circuit without constructing the wazero runtime.
	key := lg.GroupKey(Finding{File: "readme.txt", Line: 1})
	require.Empty(t, key)
	require.False(t, constructed, "lazy grouper must not create a runtime for unsupported file extensions")

	// Supported extension: lazily construct the runtime on first use.
	key = lg.GroupKey(Finding{File: "main.go", Line: 1})
	require.NotEmpty(t, key)
	require.True(t, constructed, "lazy grouper must create a runtime for supported file extensions")

	require.NoError(t, lg.Close())
}

func TestASTGrouperFor_LazyCleanupBeforeInit(t *testing.T) {
	lg := newLazyGrouper(t.TempDir())
	// Cleanup before any GroupKey call must be safe and not create a runtime.
	require.NoError(t, lg.Close())
}
