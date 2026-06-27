package tdmigrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFixture(t *testing.T) (readmePath, itemsDir string) {
	t.Helper()
	dir := t.TempDir()
	readmePath = filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte(fixtureReadme), 0o644))
	itemsDir = filepath.Join(dir, "items")
	return readmePath, itemsDir
}

func TestMain_MigrateThenGenerate_Lossless(t *testing.T) {
	readmePath, itemsDir := writeFixture(t)

	var out, errOut bytes.Buffer
	code := Main([]string{"migrate", "-readme", readmePath, "-items", itemsDir}, &out, &errOut)
	require.Equal(t, 0, code, "stderr: %s", errOut.String())
	assert.Contains(t, out.String(), "Wrote 3 item file(s)")

	files, err := filepath.Glob(filepath.Join(itemsDir, "TD-*.md"))
	require.NoError(t, err)
	require.Len(t, files, 3)

	// A non-TD file in the dir must be ignored by generate.
	require.NoError(t, os.WriteFile(filepath.Join(itemsDir, "README.md"), []byte("# schema doc\n"), 0o644))

	out.Reset()
	errOut.Reset()
	code = Main([]string{"generate", "-items", itemsDir}, &out, &errOut)
	require.Equal(t, 0, code, "stderr: %s", errOut.String())

	// The generated table must round-trip back to the same items as the source.
	_, srcItems, err := ParseReadme(fixtureReadme)
	require.NoError(t, err)
	_, genItems, err := ParseReadme("# x\n\n" + out.String())
	require.NoError(t, err)
	require.Len(t, genItems, len(srcItems))
	for i := range srcItems {
		assert.Equal(t, srcItems[i].Problem, genItems[i].Problem, "item %d problem", i)
		assert.Equal(t, srcItems[i].Status, genItems[i].Status, "item %d status", i)
		assert.Equal(t, srcItems[i].HasReviewCols, genItems[i].HasReviewCols, "item %d hasReview", i)
		assert.Equal(t, srcItems[i].Reviewers, genItems[i].Reviewers, "item %d reviewers", i)
	}
}

func TestMain_NoArgsReturnsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main(nil, &out, &errOut)
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut.String(), "Usage:")
}

func TestMain_UnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main([]string{"frobnicate"}, &out, &errOut)
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut.String(), "unknown subcommand")
}

func TestMain_Help(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main([]string{"--help"}, &out, &errOut)
	assert.Equal(t, 0, code)
	assert.Contains(t, out.String(), "td-migrate")
}

func TestMigrate_ReadmeNotFound(t *testing.T) {
	_, err := Migrate(filepath.Join(t.TempDir(), "missing.md"), t.TempDir())
	require.Error(t, err)
}

func TestMigrate_MalformedRowSurfacesError(t *testing.T) {
	dir := t.TempDir()
	bad := "# T\n\n### [2026-06-26] From Sprint: x\n\n" +
		"| Group | | Severity | File | Problem | Fix | Category | Est Minutes | Source |\n" +
		"|---|---|---|---|---|---|---|---|---|\n" +
		"| 1 | [?] | LOW | f.go:1 | p | fix | C | 5 | s |\n"
	readme := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte(bad), 0o644))
	_, err := Migrate(readme, filepath.Join(dir, "items"))
	require.Error(t, err, "unknown checkbox must surface as an error")
}

func TestGenerate_EmptyDirProducesNoOutput(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, Generate(t.TempDir(), &out))
	assert.Empty(t, out.String())
}

func TestMigrate_PrunesStaleItemsButKeepsOtherFiles(t *testing.T) {
	readmePath, itemsDir := writeFixture(t)
	_, err := Migrate(readmePath, itemsDir)
	require.NoError(t, err)

	// Plant a stale tool-owned file and a non-tool file.
	stale := filepath.Join(itemsDir, "TD-9999-orphan.md")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))
	keep := filepath.Join(itemsDir, "README.md")
	require.NoError(t, os.WriteFile(keep, []byte("schema doc"), 0o644))

	n, err := Migrate(readmePath, itemsDir)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.NoFileExists(t, stale, "stale TD-*.md must be pruned on re-migrate")
	assert.FileExists(t, keep, "non-tool files must be preserved")
}

func TestLoadItems_ErrorsOnDuplicateOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"TD-0001-a.md", "TD-0002-b.md"} {
		it := sampleItem()
		it.Order = 5 // force a collision
		content, err := RenderItemFile(it)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	_, err := LoadItems(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate order")
}
