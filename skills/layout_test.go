package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installRoot is the shipped-skill install root: one directory per skill, each
// named for its SKILL.md `name:` frontmatter. Plural so `cp -r skills/* <dest>`
// is correct in one line and a second skill needs no further rename.
const installRoot = "skills"

// skillsDir returns the absolute path to the install root.
func skillsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), installRoot)
}

// TestLayout_OneSkillMDPerSkillDirectory (AC1) — every SKILL.md under the install
// root sits directly inside a skill directory whose name equals its `name:`
// frontmatter, and no skill directory holds a nested second SKILL.md. The nested
// skill/debt-resolve/SKILL.md this epic flattened declared `name: atcr-debt-resolve`
// from a directory named `debt-resolve/`, matching neither its own frontmatter nor
// the parent's naming; this test is what keeps that from coming back.
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

// TestLayout_NoStaleSkillPathReferences (AC8) — no tracked source, doc, or test
// file still points at the pre-rename singular `skill/` directory. CHANGELOG.md is
// exempt: historical entries record what the paths were at the time and are not
// rewritten.
func TestLayout_NoStaleSkillPathReferences(t *testing.T) {
	root := repoRoot(t)

	// Assembled from parts so this file's own regexp source is not itself a hit.
	stale := regexp.MustCompile(`(^|[^a-zA-Z0-9_./-])` + "skill" + `/`)

	scanned := map[string]bool{".go": true, ".md": true, ".yml": true, ".yaml": true, ".json": true, ".sh": true}
	skipDirs := map[string]bool{"node_modules": true, "bin": true, "vendor": true, "testdata": true}

	var offenders []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Dot-directories (.git, .planning, .atcr, .github's workflows are
			// wanted though) and known build/dep trees are out of scope.
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") && name != ".github") {
				return fs.SkipDir
			}
			if skipDirs[name] {
				return fs.SkipDir
			}
			return nil
		}
		if !scanned[filepath.Ext(path)] {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "CHANGELOG.md" || rel == filepath.ToSlash(filepath.Join(installRoot, "layout_test.go")) {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if stale.MatchString(line) {
				offenders = append(offenders, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	}), "walk %s", root)

	assert.Empty(t, offenders,
		"stale singular `skill/` path references remain (the install root is now %s/atcr/):\n%s",
		installRoot, strings.Join(offenders, "\n"))
}

// itoa avoids pulling strconv in for a single call site in a failure message.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
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
