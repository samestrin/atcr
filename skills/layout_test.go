package skills

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installRoot is the shipped-skill install root: one directory per skill, each
// named for its SKILL.md `name:` frontmatter. Plural so a second skill needs no
// further rename; install via `atcr skill export`, not a bare `cp` of the
// install root (that would copy this package's Go source files too).
const installRoot = "skills"

// skillsDir returns the absolute path to the install root.
func skillsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), installRoot)
}

// TestLayout_OneSkillMDPerSkillDirectory (AC1) — every SKILL.md under the install
// root sits directly inside a skill directory whose name equals its `name:`
// frontmatter, and no skill directory holds a nested second SKILL.md. The nested
// debt-resolve SKILL.md this epic flattened declared `name: atcr-debt-resolve` from
// a directory named `debt-resolve/`, matching neither its own frontmatter nor the
// parent's naming; this test is what keeps that from coming back.
func TestLayout_OneSkillMDPerSkillDirectory(t *testing.T) {
	root := skillsDir(t)

	var found []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	}), "walk %s", root)

	require.NotEmpty(t, found, "no SKILL.md found under %s/", installRoot)

	for _, rel := range found {
		parts := strings.Split(rel, "/")
		require.Len(t, parts, 2,
			"%s/%s must be exactly one level deep (<skill-name>/SKILL.md) — no nested SKILL.md", installRoot, rel)

		dirName := parts[0]
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err, "read %s", rel)

		name := frontmatterField(t, string(raw), "name")
		assert.Equal(t, dirName, name,
			"%s/%s declares name %q but lives in directory %q — directory name must match the frontmatter",
			installRoot, rel, name, dirName)
	}
}

// TestLayout_EverySkillDirectoryShipsInTree (AC1) — every skill directory on
// disk under the install root must also be embedded in skills.Tree, or
// `atcr skill export` silently ships a subset: the layout is justified as "a
// second skill needs no further rename", but `//go:embed atcr` embeds only
// the one directory, so a second skill would build green and export nothing.
func TestLayout_EverySkillDirectoryShipsInTree(t *testing.T) {
	root := skillsDir(t)
	entries, err := os.ReadDir(root)
	require.NoError(t, err, "read %s/", installRoot)

	var onDisk []string
	for _, e := range entries {
		if e.IsDir() {
			onDisk = append(onDisk, e.Name())
		}
	}
	require.NotEmpty(t, onDisk, "no skill directories under %s/", installRoot)

	assert.Empty(t, dirsMissingFromFS(Tree, onDisk),
		"skill directories on disk but absent from skills.Tree — export would silently drop them")
}

// dirsMissingFromFS returns the directory names not present at the top level
// of fsys — the gap between the on-disk install root and the embedded tree.
func dirsMissingFromFS(fsys fs.FS, names []string) []string {
	var missing []string
	for _, name := range names {
		if _, err := fs.ReadDir(fsys, name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

// TestLayout_TreeGapIsDetected — the disk↔Tree guard must actually fire: a
// second skill directory present on disk but absent from the embedded FS
// (exactly the atcr-lite mutation) is reported, not waved through.
func TestLayout_TreeGapIsDetected(t *testing.T) {
	tree := fstest.MapFS{
		"atcr/SKILL.md": &fstest.MapFile{Data: []byte("x")},
	}
	assert.Equal(t, []string{"atcr-lite"}, dirsMissingFromFS(tree, []string{"atcr", "atcr-lite"}),
		"a second on-disk skill missing from the tree must be reported")
	assert.Empty(t, dirsMissingFromFS(tree, []string{"atcr"}),
		"the shipped skill itself must not be flagged")
}

// TestLayout_NoStaleSkillPathReferences (AC8) — no tracked source, doc, or test
// file still points at the pre-rename singular install root (the "skill" directory
// without its trailing "s"). CHANGELOG.md is exempt: historical entries record what
// the paths were at the time and are not rewritten.
//
// This file scans ITSELF like any other: the pattern is assembled from parts and
// every comment and failure message here avoids spelling the stale path literally,
// so the scanner has no blind spot where a real stale reference could hide.
func TestLayout_NoStaleSkillPathReferences(t *testing.T) {
	root := repoRoot(t)

	// Three forms the pre-rename tree actually used all have to be covered, or the
	// guard passes on exactly the references it exists to catch:
	//
	//  1. a directory reference, bare or path-prefixed. Markdown relative links
	//     were the bulk of these and every one led with a dot or a slash, so the
	//     leading-character class must ADMIT `.` and `/` rather than exclude them.
	//  2. the Go import path, which has no trailing slash at all: the module path
	//     followed by the old package name and then a quote or whitespace.
	//  3. the quoted path-segment form used by filepath.Join, where the old
	//     directory name is a string argument followed by a skill filename.
	//  4. the same Join form WITHOUT a following filename literal, which form 3
	//     misses. It is scoped to a Join call on purpose: the old directory name
	//     is also the name of the `atcr skill` command, so a bare quoted
	//     occurrence is usually a legitimate command name, not a path.
	//
	// Every pattern is assembled from parts, and this file names the old directory
	// nowhere as a literal, so the guard scans itself without self-matching.
	old := "skill"
	stale := regexp.MustCompile(strings.Join([]string{
		`(^|[^a-zA-Z0-9_-])` + old + `/`,
		`atcr/` + old + `["'\s]`,
		`"` + old + `"\s*,\s*"[A-Za-z0-9._-]+\.md"`,
		`Join\([^)]*"` + old + `"`,
	}, "|"))

	// Scope is every TRACKED file, which is what this test's contract says and
	// what a filesystem walk kept failing to deliver: .githooks/ fell to the
	// dot-directory rule while .github was admitted, extensionless files (the
	// hooks themselves) and .txt/.toml/.patch files sat outside the extension
	// allowlist, and testdata/ was skipped wholesale. A stale reference planted
	// in any of those survived the scan. git is the authority on what is
	// tracked, so ask git rather than maintaining skip lists that drift.
	var offenders []string
	for _, rel := range trackedFiles(t, root) {
		// CHANGELOG.md is exempt: historical entries record the paths as they
		// were and are not rewritten.
		if rel == "CHANGELOG.md" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(readErr, os.ErrNotExist) {
			continue // tracked but deleted in the work tree
		}
		require.NoError(t, readErr, "read %s", rel)
		if !utf8.Valid(raw) {
			continue // binary fixture (the .wasm parsers) — no source text to go stale
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if stale.MatchString(line) {
				offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}

	assert.Empty(t, offenders,
		"stale references to the pre-rename install root remain (it is now %s/%s/):\n%s",
		installRoot, SkillDir, strings.Join(offenders, "\n"))
}

// trackedFiles returns every path git tracks under root, repo-relative and
// slash-separated. Without git there is no "tracked" to scan, so the caller
// skips rather than silently falling back to a filesystem walk — the fallback
// would reintroduce exactly the blind spots this replaced.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()

	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files failed in %s (%v) — cannot enumerate tracked files", root, err)
	}

	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	require.NotEmpty(t, files, "git tracks no files under %s", root)
	return files
}

// frontmatterField extracts a single-line `key: value` field from a markdown
// file's leading YAML frontmatter block. Returns "" when the file has no
// frontmatter or the key is absent.
func frontmatterField(t *testing.T, doc, key string) string {
	t.Helper()
	if !strings.HasPrefix(doc, "---\n") {
		return ""
	}
	end := strings.Index(doc[4:], "\n---")
	if end < 0 {
		return ""
	}
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(.+)$`)
	m := re.FindStringSubmatch(doc[4 : 4+end])
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
