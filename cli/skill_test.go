package cli

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/skills"
)

// execSkillExport executes `atcr skill export` with args, returning combined
// stdout+stderr and the error. It drives the real command tree so flag parsing,
// usage-error classification, and RunE all participate.
func execSkillExport(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"skill", "export"}, args...))
	err := root.Execute()
	return out.String(), err
}

// TestSkillExport_RoundTripsByteIdentical (AC5) — exporting into an empty
// directory produces a tree an agent can load with zero further steps: the same
// filenames as skills/atcr/, each byte-identical to the shipped source. This is
// the property that lets the documented install collapse from two cp lines to one
// command.
func TestSkillExport_RoundTripsByteIdentical(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")

	_, err := execSkillExport(t, "--dir", dest)
	require.NoError(t, err, "export into an empty directory must succeed")

	srcDir := filepath.Join(repoRootDir(t), "skills", skills.SkillDir)
	srcEntries, err := os.ReadDir(srcDir)
	require.NoError(t, err)

	var want []string
	for _, e := range srcEntries {
		require.False(t, e.IsDir(), "the shipped skill directory must stay flat, found subdirectory %q", e.Name())
		want = append(want, e.Name())
	}
	require.NotEmpty(t, want, "no files found in the shipped skill directory")

	var got []string
	require.NoError(t, filepath.WalkDir(dest, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(dest, path)
		if relErr != nil {
			return relErr
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	}))
	assert.ElementsMatch(t, want, got, "exported file set must match skills/%s/ exactly", skills.SkillDir)

	for _, name := range want {
		src, err := os.ReadFile(filepath.Join(srcDir, name))
		require.NoError(t, err)
		dst, err := os.ReadFile(filepath.Join(dest, name))
		require.NoError(t, err, "exported tree is missing %s", name)
		assert.Equal(t, string(src), string(dst), "%s must be byte-identical to the shipped source", name)
	}
}

// TestSkillExport_UnknownHarnessIsAUsageError (AC6) — an unrecognized --harness
// exits non-zero, lists the harnesses it does know, and names --dir as the escape
// hatch. It never guesses a path, and it writes nothing.
func TestSkillExport_UnknownHarnessIsAUsageError(t *testing.T) {
	dest := t.TempDir()
	t.Chdir(dest)

	out, err := execSkillExport(t, "--harness", "nonesuch")
	require.Error(t, err, "an unknown harness must fail")
	assert.Equal(t, 2, exitCode(err), "an unknown harness is a usage error (exit 2)")

	msg := out + err.Error()
	for name := range skillHarnesses {
		assert.Contains(t, msg, name, "the error must list the known harness %q", name)
	}
	assert.Contains(t, msg, "--dir", "the error must name --dir as the escape hatch for an unlisted harness")

	entries, readErr := os.ReadDir(dest)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "an unknown harness must write nothing")
}

// TestSkillExport_HarnessPathsArePinned (AC6) — every harness resolves to its
// documented project path, and to its documented user path under --user. These are
// external conventions that will drift; pinning them here is what makes the drift
// visible in CI rather than silently shipping a path no harness reads. The two
// non-obvious ones are the point of the test: antigravity's user-level path is
// ~/.gemini/config/skills/ (a Gemini CLI legacy, NOT ~/.agents/), and opencode's
// is ~/.config/opencode/skills/ (NOT ~/.opencode/).
func TestSkillExport_HarnessPathsArePinned(t *testing.T) {
	want := map[string]struct{ project, user string }{
		"claude":      {".claude/skills/atcr", "~/.claude/skills/atcr"},
		"codex":       {".codex/skills/atcr", "~/.codex/skills/atcr"},
		"kimi":        {".kimi/skills/atcr", "~/.kimi/skills/atcr"},
		"opencode":    {".opencode/skills/atcr", "~/.config/opencode/skills/atcr"},
		"antigravity": {".agents/skills/atcr", "~/.gemini/config/skills/atcr"},
		"agents":      {".agents/skills/atcr", "~/.agents/skills/atcr"},
	}

	assert.Len(t, skillHarnesses, len(want), "harness table size changed — update the pinned expectations")

	for name, exp := range want {
		got, ok := skillHarnesses[name]
		require.True(t, ok, "harness %q must be known", name)
		assert.Equal(t, exp.project, got.project, "harness %q project path", name)
		assert.Equal(t, exp.user, got.user, "harness %q user path", name)
	}
}

// TestSkillExport_ResolveDestination (AC6) — the resolver honors --user, expands a
// leading ~ against the real home directory, and lets --dir win outright without
// consulting the harness table at all (so an unlisted harness stays reachable).
func TestSkillExport_ResolveDestination(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	project, err := resolveSkillDest("claude", false, "")
	require.NoError(t, err)
	assert.Equal(t, ".claude/skills/atcr", project)

	user, err := resolveSkillDest("opencode", true, "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "opencode", "skills", "atcr"), user,
		"a leading ~ must expand against the real home directory")

	explicit, err := resolveSkillDest("nonesuch", true, "/tmp/somewhere/atcr")
	require.NoError(t, err, "--dir must bypass harness validation entirely")
	assert.Equal(t, "/tmp/somewhere/atcr", explicit, "--dir is the literal destination, with no skill name appended")
}

// TestSkillExport_RefusesNonEmptyDestinationWithoutForce (AC7) — an existing
// non-empty destination is never silently clobbered, and the refusal names the
// path it would have written so the user can act on it. --force overwrites.
func TestSkillExport_RefusesNonEmptyDestinationWithoutForce(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	existing := filepath.Join(dest, "SKILL.md")
	require.NoError(t, os.WriteFile(existing, []byte("do not clobber me"), 0o644))

	out, err := execSkillExport(t, "--dir", dest)
	require.Error(t, err, "a non-empty destination must be refused without --force")
	assert.Contains(t, out+err.Error(), dest, "the refusal must name the path it would have written")

	kept, readErr := os.ReadFile(existing)
	require.NoError(t, readErr)
	assert.Equal(t, "do not clobber me", string(kept), "the refused export must not have written anything")

	_, err = execSkillExport(t, "--dir", dest, "--force")
	require.NoError(t, err, "--force must permit the overwrite")
	overwritten, readErr := os.ReadFile(existing)
	require.NoError(t, readErr)
	assert.Equal(t, skills.SkillMD, string(overwritten), "--force must replace the existing file with the shipped SKILL.md")
}

// TestSkillExport_EmptyExistingDestinationIsAllowed (AC7) — refusal is keyed on
// the destination being NON-empty. `mkdir -p` followed by an export is the
// ordinary install path and must not need --force.
func TestSkillExport_EmptyExistingDestinationIsAllowed(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	require.NoError(t, os.MkdirAll(dest, 0o755))

	_, err := execSkillExport(t, "--dir", dest)
	require.NoError(t, err, "an existing but empty destination must not require --force")

	_, statErr := os.Stat(filepath.Join(dest, "SKILL.md"))
	assert.NoError(t, statErr, "SKILL.md must have been written")
}

// TestSkillExport_ReportsDestinationAndFileCount — the success path tells the user
// where the tree landed, so the next step (pointing an agent at it) needs no
// guessing.
func TestSkillExport_ReportsDestinationAndFileCount(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")

	out, err := execSkillExport(t, "--dir", dest)
	require.NoError(t, err)
	assert.Contains(t, out, dest, "success output must name the destination")
}

// TestSkillExport_DocumentedInDispatcher (AC6, routing-drift) — `atcr skill` is a
// real top-level command, so SKILL.md's Commands table must route it; the
// bidirectional check in skill_routing_test.go enforces the other direction.
func TestSkillExport_DocumentedInDispatcher(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootDir(t), "skills", skills.SkillDir, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "`atcr skill`", "SKILL.md must route the skill command")
}

// TestSkillExport_InstallDocReplacesManualCopy (AC5) — docs/skill-usage.md's
// Installation section prescribes the export command and no longer hands the user
// a `cp` pipeline to get wrong.
func TestSkillExport_InstallDocReplacesManualCopy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootDir(t), "docs", "skill-usage.md"))
	require.NoError(t, err)
	doc := string(raw)

	install := sectionOf(doc, "## Installation")
	require.NotEmpty(t, install, "docs/skill-usage.md must have an Installation section")

	assert.Contains(t, install, "atcr skill export", "Installation must prescribe the export command")
	assert.NotContains(t, install, "cp ", "Installation must no longer hand the user a cp pipeline")

	for name := range skillHarnesses {
		assert.Contains(t, install, name, "Installation must document the %q harness", name)
	}
	assert.Contains(t, install, ".agents/skills", "Installation must document the vendor-neutral .agents path")
}

// sectionOf returns the body of the markdown section introduced by heading, up to
// the next heading at the same level. Returns "" when the heading is absent.
func sectionOf(doc, heading string) string {
	start := strings.Index(doc, heading)
	if start < 0 {
		return ""
	}
	rest := doc[start+len(heading):]
	level := strings.Repeat("#", strings.Count(heading, "#"))
	if end := strings.Index(rest, "\n"+level+" "); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestSkillExport_WarnsAboutLeftoverFiles — export overwrites but never prunes,
// so an install predating the debt-resolve flatten keeps its nested
// debt-resolve/SKILL.md. That leftover declares its own skill name and a harness
// would load it as a second skill — exactly the defect the flatten removed. The
// export must name what it left behind rather than leave the user with a silently
// broken tree.
func TestSkillExport_WarnsAboutLeftoverFiles(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	legacy := filepath.Join(dest, "debt-resolve")
	require.NoError(t, os.MkdirAll(legacy, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "SKILL.md"),
		[]byte("---\nname: atcr-debt-resolve\n---\n"), 0o644))

	out, err := execSkillExport(t, "--dir", dest, "--force")
	require.NoError(t, err)

	assert.Contains(t, out, "debt-resolve/SKILL.md",
		"the export must name the leftover file it did not write")
	assert.Contains(t, out, "warning:", "leftovers must be reported as a warning")

	_, statErr := os.Stat(filepath.Join(legacy, "SKILL.md"))
	assert.NoError(t, statErr, "export must warn about leftovers, never delete them")
}

// TestSkillExport_CleanDestinationWarnsAboutNothing — the warning must not fire on
// the ordinary path, or it becomes noise every user learns to ignore.
func TestSkillExport_CleanDestinationWarnsAboutNothing(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")

	out, err := execSkillExport(t, "--dir", dest)
	require.NoError(t, err)
	assert.NotContains(t, out, "warning:", "a clean export must emit no leftover warning")
}

// TestSkillExport_StaleScanFailureIsVisible — warnStaleSkillFiles exists to catch
// leftover files, so when the scan itself fails (e.g. an unreadable subdirectory
// mid-walk) the safety net is silently absent. The walk error must surface as a
// warning rather than return quietly, or a broken tree looks exactly like a clean
// one.
func TestSkillExport_StaleScanFailureIsVisible(t *testing.T) {
	var errOut strings.Builder
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	warnStaleSkillFiles(&errOut, missing, map[string]bool{})

	assert.Contains(t, errOut.String(), "warning:",
		"a failed leftover-file scan must be visible, not silent")
}

// TestSkillExport_ReplacesSymlinkRatherThanWritingThroughIt — os.WriteFile follows
// a symlink, which would push skill content through the link and clobber a file
// OUTSIDE the destination while still exiting 0. A user who symlinked an installed
// skill file back to a source checkout would hit this without doing anything
// adversarial, so every write must land inside the destination.
func TestSkillExport_ReplacesSymlinkRatherThanWritingThroughIt(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "atcr")
	require.NoError(t, os.MkdirAll(dest, 0o755))

	victim := filepath.Join(tmp, "victim.md")
	require.NoError(t, os.WriteFile(victim, []byte("untouched"), 0o644))
	require.NoError(t, os.Symlink(victim, filepath.Join(dest, "SKILL.md")))

	_, err := execSkillExport(t, "--dir", dest, "--force")
	require.NoError(t, err)

	survived, readErr := os.ReadFile(victim)
	require.NoError(t, readErr)
	assert.Equal(t, "untouched", string(survived),
		"export must not write through a symlink to a file outside the destination")

	inside, readErr := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	require.NoError(t, readErr)
	assert.Equal(t, skills.SkillMD, string(inside), "the symlink must have been replaced by a real file")
}

// TestSkillExport_DestinationIsAFileIsAUsageError — a destination that exists as a
// regular file is a mistake in the invocation, so it must exit 2 with a message
// naming the problem, not leak a raw ENOTDIR at exit 1.
func TestSkillExport_DestinationIsAFileIsAUsageError(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	require.NoError(t, os.WriteFile(dest, []byte("i am a file"), 0o644))

	out, err := execSkillExport(t, "--dir", dest)
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "a non-directory destination is a usage error")
	assert.Contains(t, out+err.Error(), "not a directory", "the message must name the actual problem")
}

// TestSkillExport_DirExpandsLeadingTilde — --dir is documented with a ~/ example,
// so a quoted or config-supplied ~/... must resolve against the home directory
// rather than creating a directory literally named "~" under the cwd.
func TestSkillExport_DirExpandsLeadingTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := resolveSkillDest("claude", false, "~/.someagent/skills/atcr")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".someagent", "skills", "atcr"), got,
		"a leading ~/ in --dir must expand, not become a literal directory name")
}

// TestSkillExport_DefaultHarnessWritesToProjectPath — every other export test
// passes --dir, which skips harness resolution entirely; this exercises the
// default, most common invocation end to end, from resolved path to file on disk.
func TestSkillExport_DefaultHarnessWritesToProjectPath(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := execSkillExport(t)
	require.NoError(t, err, "the default invocation must succeed")

	_, statErr := os.Stat(filepath.Join(".claude", "skills", "atcr", "SKILL.md"))
	assert.NoError(t, statErr, "the default harness must write to its documented project path")
}
