package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A member added to the in-tree vocabulary must be visible to the doc guard in
// EVERY build environment. This probes the pure reader against a synthetic
// module directory rather than the real one, so the assertion holds whether or
// not the real in-tree source currently differs from the released pin.
func TestInTreeCategories_ReadsDeclaredOrderIncludingUnreleasedMembers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	CategoryCorrectness = "correctness"
	CategoryBrandNew    = "brand-new"
	CategoryOther       = "other"
)

var categories = []string{
	CategoryCorrectness,
	CategoryBrandNew,
	CategoryOutOfScope,
	CategoryOther,
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "merge.go"), []byte(`package reconcile

const CategoryOutOfScope = "out-of-scope"
`), 0o644))

	got, err := inTreeCategories(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"correctness", "brand-new", "out-of-scope", "other"}, got,
		"the reader must return the declared slice order, including a member that has not been released yet")
}

// A directory that declares no vocabulary must be an error, never an empty
// slice: an empty result compared against a doc table would report the table as
// wrong when the real fault is that the source was not read.
func TestInTreeCategories_EmptyDirectoryIsAnError(t *testing.T) {
	_, err := inTreeCategories(t.TempDir())
	assert.Error(t, err, "a directory with no categories slice must not read as an empty vocabulary")
}

// The partition must follow the comment-marked blocks, in declared order, and
// must not invent a block for a constant that carries no leading comment.
func TestInTreeCategoryBlocks_FollowsCommentMarkedBlocks(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"
	CategoryLogic       = "logic"

	// Cross-cutting concerns.
	CategoryDocs = "docs"

	// Control values.
	CategoryOther = "other"
)
`), 0o644))

	got, err := inTreeCategoryBlocks(dir)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"correctness", "logic"}, {"docs"}, {"other"}}, got,
		"each comment-marked run of constants is one block, in declared order")
}
