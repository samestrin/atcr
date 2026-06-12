package payload

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fatal git failure (here: not a repository) must propagate from isBinary
// rather than being silently reported as "not binary" (TD-010).
func TestIsBinary_FatalGitErrorPropagates(t *testing.T) {
	g := &gitRunner{ctx: context.Background(), dir: t.TempDir()}
	_, err := g.isBinary("HEAD~1", "HEAD", "a.go")
	require.Error(t, err)
}

// A fatal git failure must propagate from functionContextFile rather than
// being masked as the zero-hunk fallback (TD-010).
func TestFunctionContextFile_FatalGitErrorPropagates(t *testing.T) {
	g := &gitRunner{ctx: context.Background(), dir: t.TempDir()}
	_, _, err := g.functionContextFile("HEAD~1", "HEAD", "a.go")
	require.Error(t, err)
}

// An empty diff (path unchanged in base..head) stays the non-fatal fallback:
// ok=false with a nil error.
func TestFunctionContextFile_NoDiffIsFallbackNotError(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "changed.go", goFileV1)
	write(t, dir, "stable.go", "package p\n\nfunc S() int { return 1 }\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "changed.go", goFileV2)
	head := commitAll(t, dir, "v2")

	g := &gitRunner{ctx: context.Background(), dir: dir}
	out, ok, err := g.functionContextFile(base, head, "stable.go")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, out)
}

// An unchanged path is "not binary" with a nil error.
func TestIsBinary_NoDiffIsNotBinaryNotError(t *testing.T) {
	dir := initRepo(t)
	write(t, dir, "changed.go", goFileV1)
	write(t, dir, "stable.go", "package p\n\nfunc S() int { return 1 }\n")
	base := commitAll(t, dir, "v1")
	write(t, dir, "changed.go", goFileV2)
	head := commitAll(t, dir, "v2")

	g := &gitRunner{ctx: context.Background(), dir: dir}
	bin, err := g.isBinary(base, head, "stable.go")
	require.NoError(t, err)
	assert.False(t, bin)
}
