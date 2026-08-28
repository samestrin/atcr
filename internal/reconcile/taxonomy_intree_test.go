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

// A Category* constant whose value is not a string literal (a rune, an integer)
// is not a vocabulary member. The Kind check in stringLiteral is what excludes
// it: without that check a rune literal unquotes successfully and is admitted
// silently. Referencing it from the slice must therefore be an error naming the
// constant — not a vocabulary that quietly gained a member.
func TestInTreeCategories_RejectsNonStringCategoryValues(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"
	CategoryRune        = 'a'
)

var categories = []string{
	CategoryCorrectness,
	CategoryRune,
}
`), 0o644))

	_, err := inTreeCategories(dir)
	require.Error(t, err, "a rune literal is not a string value: CategoryRune must not be admitted to the vocabulary")
	assert.Contains(t, err.Error(), "CategoryRune",
		"the error must name the constant whose value is not a string literal")
}

// A directory that declares no vocabulary must be an error, never an empty
// slice: an empty result compared against a doc table would report the table as
// wrong when the real fault is that the source was not read.
func TestInTreeCategories_EmptyDirectoryIsAnError(t *testing.T) {
	_, err := inTreeCategories(t.TempDir())
	assert.Error(t, err, "a directory with no categories slice must not read as an empty vocabulary")
}

// A directory that cannot be read at all must be an error, for the same reason:
// an unreadable source tree must never read as an empty vocabulary.
func TestInTreeCategories_UnreadableDirectoryIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not make a directory unreadable")
	}
	dir := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Chmod(dir, 0o000))
	defer func() { _ = os.Chmod(dir, 0o755) }() // restore so t.TempDir cleanup can remove it

	_, err := inTreeCategories(dir)
	assert.Error(t, err, "an unreadable directory must be an error, not an empty vocabulary")
	assert.Contains(t, err.Error(), "read "+dir,
		"the error must report the read failure, not a downstream symptom like a missing slice")
}

// A syntactically invalid .go file must be an error: silently skipping it would
// drop whatever vocabulary it declared and report the doc table as wrong.
func TestInTreeCategories_InvalidGoFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte("package reconcile\n\nconst (\n"), 0o644))

	_, err := inTreeCategories(dir)
	assert.Error(t, err, "a file that does not parse must be an error, not a silently skipped file")
	assert.Contains(t, err.Error(), "parse",
		"the error must report the parse failure, not a downstream symptom like a missing slice")
}

// A categories slice that references an identifier no Category* constant
// declares must be an error naming the identifier: resolving it to an empty
// string would admit a blank member to the vocabulary.
func TestInTreeCategories_UndeclaredIdentifierIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"
)

var categories = []string{
	CategoryCorrectness,
	CategoryMissing,
}
`), 0o644))

	_, err := inTreeCategories(dir)
	require.Error(t, err, "a slice element no constant declares must be an error, not an empty member")
	assert.Contains(t, err.Error(), "CategoryMissing",
		"the error must name the undeclared identifier")
}

// A bare string literal in the categories slice is a legitimate member — it is
// how a value can be listed without a named constant — and must be carried into
// the vocabulary as-is.
func TestInTreeCategories_BareStringLiteralElementIsAMember(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"
)

var categories = []string{
	CategoryCorrectness,
	"bare-literal",
}
`), 0o644))

	got, err := inTreeCategories(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"correctness", "bare-literal"}, got,
		"a bare string literal element must be carried into the vocabulary in its declared position")
}

// A slice element that is neither a Category* identifier nor a string literal
// (a call, an arithmetic expression) cannot be resolved to a value and must be
// an error rather than a silently dropped or invented member.
func TestInTreeCategories_UnresolvableElementIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"
)

var categories = []string{
	CategoryCorrectness,
	len("computed"),
}
`), 0o644))

	_, err := inTreeCategories(dir)
	assert.Error(t, err, "an element that is neither an identifier nor a string literal must be an error")
}

// The partition must follow the comment-marked blocks, in declared order, and
// must not invent a block for a constant that carries no leading comment.
// A comment that opens a run of non-Category* constants (a note between groups)
// opens an EMPTY block, which the drop filter must remove: the partition stays
// exactly three blocks, never four.
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

	// A note between groups, introducing no category.
	BlockNote = "note"
)

var categories = []string{
	CategoryCorrectness,
	CategoryLogic,
	CategoryDocs,
	CategoryOther,
}
`), 0o644))

	got, err := inTreeCategoryBlocks(dir)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"correctness", "logic"}, {"docs"}, {"other"}}, got,
		"each comment-marked run of constants is one block, in declared order — a comment-opened run with no Category* member drops to nothing")
}

// A Category* const that opens a parenthesized const block BEFORE any leading
// comment belongs to no block. The len(blocks) == 0 guard is what keeps it out
// of the partition; the sibling NoMarkedBlocksIsAnError fixture cannot pin it
// because its non-parenthesized const is rejected earlier. This fixture's loose
// const IS parenthesized, so only the guard stands between it and the partition.
func TestInTreeCategoryBlocks_LeadingSpecWithoutCommentIsNotABlock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	CategoryLoose = "loose"

	// Defect classes.
	CategoryCorrectness = "correctness"
)

var categories = []string{
	CategoryCorrectness,
}
`), 0o644))

	got, err := inTreeCategoryBlocks(dir)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"correctness"}}, got,
		"a constant declared before any leading comment belongs to no block and must be skipped")
}

// The partition restates category.go's block structure, so a cosmetic refactor
// of ANOTHER file — wrapping merge.go's CategoryOutOfScope in `const ( ... )`,
// the style merge.go already uses for Sev* and Conf* — must not move it.
// out-of-scope is excluded by name, not by the syntax of its declaration.
func TestInTreeCategoryBlocks_MergeGoDeclarationStyleDoesNotMoveThePartition(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"

	// Control values.
	CategoryOther = "other"
)

var categories = []string{
	CategoryCorrectness,
	CategoryOutOfScope,
	CategoryOther,
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "merge.go"), []byte(`package reconcile

const (
	// CategoryOutOfScope tags a finding as outside the reviewed change.
	CategoryOutOfScope = "out-of-scope"
)
`), 0o644))

	got, err := inTreeCategoryBlocks(dir)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"correctness"}, {"other"}}, got,
		"how merge.go declares out-of-scope (parenthesized or not) must not move the partition")
}

// A constant that reaches a comment-marked block but never the categories slice
// is a fault in the SLICE, not in the doc table — yet the two doc guards read
// different authorities (the slice vs the const blocks), so without a cross-check
// both blame the table and no table content satisfies them. The partition reader
// must fail here, naming the slice.
func TestInTreeCategoryBlocks_BlockMemberAbsentFromSliceIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"
	CategoryForgot      = "forgot"
)

var categories = []string{
	CategoryCorrectness,
}
`), 0o644))

	_, err := inTreeCategoryBlocks(dir)
	require.Error(t, err, "a block member absent from the categories slice must be an error, not a doc-table mismatch")
	assert.Contains(t, err.Error(), "absent from the categories slice",
		"the error must name the categories slice as the side at fault")
}

// An UNPARENTHESIZED `const CategoryX = "..."` carries its leading comment on
// GenDecl.Doc, not on the ValueSpec — so vs.Doc is nil, no new block opens, and
// without an Lparen guard the constant is folded into the LAST parenthesized
// block. That fold re-creates the doc-guard deadlock this guard exists to
// remove: the folded member fails the slice cross-check, or worse, moves the
// partition a doc row is pinned against. Unparenthesized declarations are not
// block-bearing syntax; they must be skipped entirely.
func TestInTreeCategoryBlocks_UnparenthesizedConstIsNotABlock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"

	// Control values.
	CategoryOther = "other"
)

// Late additions.
const CategoryLate = "late"

var categories = []string{
	CategoryCorrectness,
	CategoryOther,
}
`), 0o644))

	got, err := inTreeCategoryBlocks(dir)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"correctness"}, {"other"}}, got,
		"an unparenthesized const carries its comment on GenDecl.Doc (vs.Doc == nil) and must be skipped, not folded into the last block")
}

// The reverse direction of the sibling cross-check: a Category* constant that
// reaches the categories slice but no comment-marked block is a fault in the
// BLOCKS, not the doc table — inTreeCategories walks the whole directory while
// inTreeCategoryBlocks reads category.go alone, so a constant declared in any
// other file (merge.go's CategoryOutOfScope is the precedent) enters the
// vocabulary, makes the doc guard demand a row for it, yet never reaches a
// block, leaving its Group cell unpinned. out-of-scope is exempt: it is a
// routing value declared outside category.go by design, and its Group cell is
// pinned separately.
func TestInTreeCategoryBlocks_SliceMemberAbsentFromBlocksIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const (
	// Defect classes.
	CategoryCorrectness = "correctness"

	// Control values.
	CategoryOther = "other"
)

var categories = []string{
	CategoryCorrectness,
	CategoryStray,
	CategoryOutOfScope,
	CategoryOther,
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "merge.go"), []byte(`package reconcile

const CategoryOutOfScope = "out-of-scope"
const CategoryStray = "stray"
`), 0o644))

	_, err := inTreeCategoryBlocks(dir)
	require.Error(t, err, "a slice member that appears in no comment-marked block must be an error, not a silently unpinned Group cell")
	assert.Contains(t, err.Error(), "stray",
		"the error must name the member that no block declares")
	assert.NotContains(t, err.Error(), "out-of-scope",
		"out-of-scope is exempt: it is a routing value declared outside category.go by design")
}

// Same contract as its sibling: a directory that marks no block must be an
// error, never an empty partition. An empty partition would make the Group
// column guard vacuously pass instead of reporting that nothing was read.
func TestInTreeCategoryBlocks_NoMarkedBlocksIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "category.go"), []byte(`package reconcile

const CategoryLoose = "loose"
`), 0o644))

	_, err := inTreeCategoryBlocks(dir)
	assert.Error(t, err, "constants outside any comment-marked block must not read as a partition")
}
