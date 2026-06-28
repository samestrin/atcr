package reconcile

import (
	"os"
	"path/filepath"
	"testing"

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
