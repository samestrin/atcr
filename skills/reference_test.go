package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mdRef matches a backticked bare markdown filename — `host-review.md`,
// `debt-resolve.md`. It deliberately requires the name to contain no `/`, so a
// legitimate repo-relative prose citation like `docs/findings-format.md` is not
// mistaken for a claim that the file sits beside SKILL.md.
var mdRef = regexp.MustCompile("`([A-Za-z0-9._-]+\\.md)`")

// reviewArtifacts are bare `.md` names the skill mentions as OUTPUT of a review
// run — they live under `.atcr/reviews/<id>/`, are produced at runtime, and are
// never siblings of SKILL.md. Everything else a shipped skill names by bare
// filename is an on-demand reference the agent is expected to load, so it must
// resolve in the same directory.
var reviewArtifacts = map[string]bool{
	"report.md": true, // reconciled/report.md — the rendered report
	"review.md": true, // sources/<reviewer>/review.md — a reviewer's narrative
}

// findBrokenMarkdownRefs returns every markdown reference in content that
// does not resolve as a sibling of the citing file (dir). Self-references
// and runtime review artifacts are skipped. checked counts the references
// actually verified, so callers can prove the scan was non-vacuous.
func findBrokenMarkdownRefs(repoRoot, dir, fileName string, content []byte) (broken []string, checked int) {
	for _, m := range mdRef.FindAllStringSubmatch(string(content), -1) {
		ref := m[1]
		if ref == fileName || reviewArtifacts[ref] {
			continue // self-reference, or a runtime review artifact
		}
		if _, err := os.Stat(filepath.Join(dir, ref)); err != nil {
			broken = append(broken, ref)
			continue
		}
		checked++
	}
	return broken, checked
}

// TestReference_OnDemandFilesAreTrueSiblings (AC2) — every bare `<name>.md`
// SKILL.md points at exists in SKILL.md's own directory. The pre-flatten
// debt-resolve route told the agent to read `CONVENTIONS.md` from a subdirectory
// that had no such file; it worked only because an agent reading the whole
// installed tree stumbled onto the copy one level up. This asserts the property
// that made that bug possible is gone, for every on-demand reference in every
// shipped skill.
func TestReference_OnDemandFilesAreTrueSiblings(t *testing.T) {
	root := skillsDir(t)

	entries, err := os.ReadDir(root)
	require.NoError(t, err, "read %s/", installRoot)

	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, err := os.ReadDir(dir)
		require.NoError(t, err, "read %s/%s/", installRoot, e.Name())

		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, f.Name()))
			require.NoError(t, err, "read %s", f.Name())

			broken, n := findBrokenMarkdownRefs(repoRoot(t), dir, f.Name(), raw)
			assert.Empty(t, broken,
				"%s/%s/%s references %v, which are not sibling files in that directory",
				installRoot, e.Name(), f.Name(), broken)
			checked += n
		}
	}
	require.Positive(t, checked, "no on-demand sibling references found to verify")
}

// TestReference_OnDemandRefForms — the extraction must recognize every
// natural citation form, not only the backticked bare name: an optional ./
// prefix, a bare prose token, a bolded name, a markdown link target, and a
// path-prefixed form. A form the regex misses is a form a stale reference
// can hide in.
func TestReference_OnDemandRefForms(t *testing.T) {
	line := "see `./ghost.md` and plain ghost.md and **ghost.md** and [x](ghost.md) and `sub/ghost.md` here"
	matches := mdRef.FindAllStringSubmatch(line, -1)
	got := make([]string, 0, len(matches))
	for _, m := range matches {
		got = append(got, m[len(m)-1])
	}
	assert.Equal(t, []string{"ghost.md", "ghost.md", "ghost.md", "ghost.md", "sub/ghost.md"}, got,
		"every natural citation form must be extracted, not just backticked bare names")
}

// TestReference_GhostReferencesAreCaught — the check must actually fail on
// references to files that do not exist, in every citation form, and the
// reviewArtifacts exemption must not leak into files that have no business
// citing runtime review outputs.
func TestReference_GhostReferencesAreCaught(t *testing.T) {
	dir := t.TempDir()
	repo := t.TempDir()

	broken, _ := findBrokenMarkdownRefs(repo, dir, "host-review.md", []byte(
		"load `./ghost.md`, plain ghost.md, **ghost.md**, [x](ghost.md), `sub/ghost.md`, and `report.md`"))
	assert.ElementsMatch(t,
		[]string{"ghost.md", "ghost.md", "ghost.md", "ghost.md", "sub/ghost.md", "report.md"},
		broken, "every unresolvable reference form must be reported")

	// The artifact exemption is scoped to the files that legitimately cite
	// runtime outputs: host-review.md may name its own review.md narrative,
	// but no file gets a blanket pass on report.md, and CONVENTIONS.md has
	// no exemption at all.
	exempt, _ := findBrokenMarkdownRefs(repo, dir, "host-review.md", []byte(
		"write your narrative to `review.md`"))
	assert.Empty(t, exempt, "host-review.md legitimately cites its own review.md output")

	leaked, _ := findBrokenMarkdownRefs(repo, dir, "CONVENTIONS.md", []byte(
		"see `review.md`"))
	assert.Equal(t, []string{"review.md"}, leaked,
		"the review.md artifact exemption must not leak into CONVENTIONS.md")

	// Path-form citations resolve repo-relative (docs/findings-format.md),
	// not against the citing file's directory.
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "docs", "findings-format.md"), []byte("x"), 0o644))
	repoRel, _ := findBrokenMarkdownRefs(repo, dir, "findings-format.md", []byte(
		"see `docs/findings-format.md` for background"))
	assert.Empty(t, repoRel, "repo-relative path citations must resolve against the repo root")
}

// TestReference_DebtResolveIsAPlainReference (AC3) — debt-resolve.md is an
// on-demand reference file reached through SKILL.md's `atcr debt` routing row, not
// a second independently-invocable skill. It therefore carries no YAML frontmatter
// (matching host-review.md and every other on-demand file) and no standalone
// trigger phrase that would make a harness offer it directly.
func TestReference_DebtResolveIsAPlainReference(t *testing.T) {
	assert.False(t, strings.HasPrefix(DebtResolveMD, "---\n"),
		"debt-resolve.md must carry no YAML frontmatter — it is a reference file, not a skill")
	assert.NotContains(t, DebtResolveMD, "name: atcr-debt-resolve",
		"debt-resolve.md must not declare a top-level skill name")

	for _, trigger := range []string{
		"Use when a standalone atcr user asks",
		"Loaded on demand from the atcr dispatcher",
	} {
		assert.NotContains(t, DebtResolveMD, trigger,
			"debt-resolve.md must not carry the standalone trigger phrase %q", trigger)
	}
}

// TestReference_DebtRowPointsAtFlattenedFile (AC3) — SKILL.md's `atcr debt` row
// documents the resolve route in the same noun-verb order as the CLI
// (`atcr debt resolve`) and points at the flattened sibling. This replaces the
// pre-flatten `debt-resolve/SKILL.md` pointer; `atcr debt resolve` stays a
// subcommand extension INSIDE the existing `atcr debt` row rather than becoming a
// second dispatcher row, because cli/skill_routing_test.go's skillRoutingRow regex
// captures a single word after `atcr ` and would silently ignore a three-word row.
func TestReference_DebtRowPointsAtFlattenedFile(t *testing.T) {
	assert.Contains(t, SkillMD, "`debt-resolve.md`",
		"SKILL.md must point at the flattened on-demand debt-resolve.md")
	assert.NotContains(t, SkillMD, "debt-resolve/SKILL.md",
		"the pre-flatten nested pointer must be gone")
	assert.Contains(t, SkillMD, "atcr debt resolve",
		"SKILL.md must document the resolve route in CLI noun-verb order")

	row := debtRow(t)
	assert.Contains(t, row, "atcr debt resolve",
		"the resolve route must live inside the existing `atcr debt` row, not a separate row")
	assert.Contains(t, row, "`debt-resolve.md`",
		"the `atcr debt` row must carry the pointer to debt-resolve.md")
}

// TestReference_NoPrivateSkillCitations (AC3) — the shipped skill never cites the
// private planning-pipeline skills by name. They are not part of atcr's release
// surface, so a standalone user reading the installed tree cannot act on them.
func TestReference_NoPrivateSkillCitations(t *testing.T) {
	for name, md := range map[string]string{
		"SKILL.md":                  SkillMD,
		"debt-resolve.md":           DebtResolveMD,
		"CONVENTIONS.md":            ConventionsMD,
		"host-review.md":            HostReviewMD,
		"ambiguity-adjudication.md": AmbiguityAdjudicationMD,
		"findings-format.md":        FindingsFormatMD,
	} {
		for _, private := range []string{"/resolve-td", "/finalize-td", "/execute-code-review", "/reconcile-code-review"} {
			assert.NotContains(t, md, private,
				"%s cites the private skill %q, which is not part of atcr's release surface", name, private)
		}
	}
}

// debtRow returns the single `atcr debt` routing-table row from SKILL.md.
func debtRow(t *testing.T) string {
	t.Helper()
	for _, line := range strings.Split(SkillMD, "\n") {
		if strings.HasPrefix(line, "| `atcr debt`") {
			return line
		}
	}
	t.Fatal("SKILL.md has no `atcr debt` routing-table row")
	return ""
}
