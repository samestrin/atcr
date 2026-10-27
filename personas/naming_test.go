package personas

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// personaStemRe is the persona naming rule (docs/personas-authoring.md, "Persona
// naming: human first names"): a persona file stem is one lowercase word of
// letters only, so role slugs such as frontend-qa or foo_bar cannot return.
var personaStemRe = regexp.MustCompile(`^[a-z]+$`)

// personaNamingDirs are the directories whose .md and .yaml files are personas.
// Subdirectories (testdata/) are not scanned, and other file types (index.json)
// are out of scope.
var personaNamingDirs = []string{".", "community"}

// personaNamingExceptions are file names that are allowed to break the rule.
// _base.md is the shared template, not a persona (see expectedEmbeddedFiles).
var personaNamingExceptions = map[string]struct{}{
	"_base.md": {},
}

// badPersonaStems returns the dir-relative paths of the .md and .yaml files
// directly under dirs whose stem does not match personaStemRe and which are not
// in exceptions.
func badPersonaStems(t *testing.T, dirs []string, exceptions map[string]struct{}) []string {
	t.Helper()
	var bad []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := filepath.Ext(name)
			if ext != ".md" && ext != ".yaml" {
				continue
			}
			if _, ok := exceptions[name]; ok {
				continue
			}
			if !personaStemRe.MatchString(strings.TrimSuffix(name, ext)) {
				bad = append(bad, filepath.Join(dir, name))
			}
		}
	}
	sort.Strings(bad)
	return bad
}

func TestPersonaFileStemsAreOneLowercaseWord(t *testing.T) {
	bad := badPersonaStems(t, personaNamingDirs, personaNamingExceptions)
	assert.Empty(t, bad, "persona file stems must match %s (one lowercase word, letters only); rename these to a human first name", personaStemRe)
}

func TestPersonaFileStemsAreOneLowercaseWord_ScansBothDirsAndExtensions(t *testing.T) {
	root := t.TempDir()
	community := filepath.Join(root, "community")
	require.NoError(t, os.MkdirAll(filepath.Join(community, "testdata"), 0o755))
	for _, f := range []string{
		filepath.Join(root, "bruce.md"),
		filepath.Join(root, "_base.md"),
		filepath.Join(root, "role_mapper.md"),
		filepath.Join(root, "foo_bar.md"),
		filepath.Join(root, "notes.txt"),
		filepath.Join(community, "gerald.md"),
		filepath.Join(community, "gerald.yaml"),
		filepath.Join(community, "frontend-qa.yaml"),
		filepath.Join(community, "Liam.md"),
		filepath.Join(community, "index.json"),
		filepath.Join(community, "testdata", "bad_name.md"),
	} {
		require.NoError(t, os.WriteFile(f, nil, 0o644))
	}

	bad := badPersonaStems(t, []string{root, community}, personaNamingExceptions)
	assert.Equal(t, []string{
		filepath.Join(community, "Liam.md"),
		filepath.Join(community, "frontend-qa.yaml"),
		filepath.Join(root, "foo_bar.md"),
		filepath.Join(root, "role_mapper.md"),
	}, bad)
}
