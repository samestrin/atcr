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

// execSkillExport executes `atcr skill export` with args, returning stdout and
// stderr separately plus the error, so tests can assert WHICH stream a message
// lands on (warnings must not pollute a piped stdout). It drives the real
// command tree so flag parsing, usage-error classification, and RunE all
// participate.
func execSkillExport(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"skill", "export"}, args...))
	err := root.Execute()
	return out.String(), errOut.String(), err
}

// TestSkillExport_RoundTripsByteIdentical (AC5) — exporting into an empty
// directory produces a tree an agent can load with zero further steps: the same
// filenames as skills/atcr/, each byte-identical to the shipped source. This is
// the property that lets the documented install collapse from two cp lines to one
// command.
func TestSkillExport_RoundTripsByteIdentical(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")

	_, _, err := execSkillExport(t, "--dir", dest)
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

	out, errOut, err := execSkillExport(t, "--harness", "nonesuch")
	require.Error(t, err, "an unknown harness must fail")
	assert.Equal(t, 2, exitCode(err), "an unknown harness is a usage error (exit 2)")

	msg := out + errOut + err.Error()
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

// TestSkillExport_EmptyDirFlagIsAUsageError — an explicitly empty `--dir ""` —
// the normal result of `--dir "$SKILLS_DIR"` with an unset or typo'd variable —
// must be rejected as a usage error, not silently treated as "no --dir given"
// while the export writes to the harness's default path.
func TestSkillExport_EmptyDirFlagIsAUsageError(t *testing.T) {
	dest := t.TempDir()
	t.Chdir(dest)

	out, errOut, err := execSkillExport(t, "--dir", "")
	require.Error(t, err, "a set-but-empty --dir must fail rather than fall back to the harness path")
	assert.Equal(t, 2, exitCode(err), "a set-but-empty --dir is a usage error (exit 2)")
	assert.Contains(t, out+errOut+err.Error(), "--dir", "the error must name the flag at fault")

	_, statErr := os.Stat(filepath.Join(dest, ".claude"))
	assert.Error(t, statErr, "nothing may be written under the default harness path when --dir was supplied")
}

// TestSkillExport_UserHarnessWritesToUserPath — the --user flag's only other
// coverage is a direct resolveSkillDest call, so a broken flag wire-up (the
// `user := false` mutation) would ship silently. This drives the real command
// tree: HOME is redirected to a temp dir, and the export must land in the
// harness's user-level path under it, not the project path under the cwd.
func TestSkillExport_UserHarnessWritesToUserPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	t.Chdir(work)

	out, _, err := execSkillExport(t, "--harness", "codex", "--user")
	require.NoError(t, err, "--user export must succeed")

	userDest := filepath.Join(home, ".codex", "skills", "atcr")
	assert.Contains(t, out, userDest, "the export must report the user-level destination")
	_, statErr := os.Stat(filepath.Join(userDest, "SKILL.md"))
	assert.NoError(t, statErr, "--user must write to the user-level path")
	_, statErr = os.Stat(filepath.Join(work, ".codex"))
	assert.Error(t, statErr, "--user must not write to the project path under the cwd")
}

// TestSkillExport_RefusesNonEmptyDestinationWithoutForce (AC7) — an existing
// non-empty destination is never silently clobbered, and the refusal names the
// path it would have written so the user can act on it. --force overwrites.
func TestSkillExport_RefusesNonEmptyDestinationWithoutForce(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	existing := filepath.Join(dest, "SKILL.md")
	require.NoError(t, os.WriteFile(existing, []byte("do not clobber me"), 0o644))

	out, errOut, err := execSkillExport(t, "--dir", dest)
	require.Error(t, err, "a non-empty destination must be refused without --force")
	assert.Contains(t, out+errOut+err.Error(), dest, "the refusal must name the path it would have written")

	kept, readErr := os.ReadFile(existing)
	require.NoError(t, readErr)
	assert.Equal(t, "do not clobber me", string(kept), "the refused export must not have written anything")

	_, _, err = execSkillExport(t, "--dir", dest, "--force")
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

	_, _, err := execSkillExport(t, "--dir", dest)
	require.NoError(t, err, "an existing but empty destination must not require --force")

	_, statErr := os.Stat(filepath.Join(dest, "SKILL.md"))
	assert.NoError(t, statErr, "SKILL.md must have been written")
}

// TestSkillExport_ReportsDestinationAndFileCount — the success path tells the user
// where the tree landed, so the next step (pointing an agent at it) needs no
// guessing.
func TestSkillExport_ReportsDestinationAndFileCount(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")

	out, _, err := execSkillExport(t, "--dir", dest)
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
// the next heading at the same level. Returns "" when the heading is absent. The
// heading is matched anchored to a line boundary and the terminator level is
// derived from the matched line — a deeper same-named heading (### X contains
// ## X as raw text) must not shift or truncate the scanned region.
func sectionOf(doc, heading string) string {
	idx := strings.Index("\n"+doc, "\n"+heading)
	if idx < 0 {
		return ""
	}
	line := doc[idx:] // haystack offset absorbs the prepended "\n"
	level := strings.Repeat("#", len(line)-len(strings.TrimLeft(line, "#")))
	rest := line[len(heading):]
	if end := strings.Index(rest, "\n"+level+" "); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestSectionOf_AnchorsHeadingToLineBoundary — sectionOf must not match a heading
// as raw text: "### Installation" contains "## Installation", so an unanchored
// search would shift the scanned region into the deeper section's body and weaken
// the doc assertions that rely on it.
func TestSectionOf_AnchorsHeadingToLineBoundary(t *testing.T) {
	doc := strings.Join([]string{
		"# Doc",
		"",
		"### Installation",
		"DEEPER BODY",
		"",
		"## Installation",
		"REAL BODY",
		"",
		"## Next",
		"TAIL",
	}, "\n")

	install := sectionOf(doc, "## Installation")

	assert.Contains(t, install, "REAL BODY")
	assert.NotContains(t, install, "DEEPER BODY",
		"a deeper same-named heading must not shift the scanned region")
	assert.NotContains(t, install, "TAIL", "the section ends at the next same-level heading")
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

	out, errOut, err := execSkillExport(t, "--dir", dest, "--force")
	require.NoError(t, err)

	assert.Contains(t, errOut, "debt-resolve/SKILL.md",
		"the export must name the leftover file it did not write")
	assert.Contains(t, errOut, "warning:", "leftovers must be reported as a warning")
	assert.NotContains(t, out, "warning:", "the leftover warning must go to stderr, not pollute a piped stdout")

	_, statErr := os.Stat(filepath.Join(legacy, "SKILL.md"))
	assert.NoError(t, statErr, "export must warn about leftovers, never delete them")
}

// TestSkillExport_WarnsDespiteIncompleteLeftoverScan — one unreadable
// subdirectory aborts the walk with an error, but the leftovers found BEFORE
// the error must still be named: silently dropping them contradicts the whole
// point of the warning (a leftover nested SKILL.md loads as a second skill).
// The scan's failure must also be admitted, or the report reads as complete.
func TestSkillExport_WarnsDespiteIncompleteLeftoverScan(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	unreadable := filepath.Join(dest, "unreadable")
	require.NoError(t, os.MkdirAll(unreadable, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(unreadable, "trapped.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "leftover.md"), []byte("old"), 0o644))
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	_, errOut, err := execSkillExport(t, "--dir", dest, "--force")
	require.NoError(t, err)

	assert.Contains(t, errOut, "leftover.md",
		"leftovers found before the walk error must still be reported")
	assert.Contains(t, errOut, "incomplete",
		"an aborted leftover scan must say it did not finish")
}

// TestSkillExport_IgnoresOSMetadataFiles — a destination holding only OS
// metadata (Finder's .DS_Store, editor swap files) is not "non-empty" in any
// sense the user cares about: it must not demand --force, and the export must
// not report the metadata as a leftover skill file. On macOS merely opening
// the installed skills folder in Finder would otherwise produce a permanent
// confusing warning on every subsequent export.
func TestSkillExport_IgnoresOSMetadataFiles(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, ".DS_Store"), []byte("finder"), 0o644))

	out, errOut, err := execSkillExport(t, "--dir", dest)
	require.NoError(t, err, "a directory holding only dot-prefixed OS metadata must not require --force")
	assert.NotContains(t, errOut, "warning:", "OS metadata must not be reported as a stale skill file")
	assert.NotContains(t, out+errOut, ".DS_Store", "OS metadata must not be named in the output")

	_, statErr := os.Stat(filepath.Join(dest, "SKILL.md"))
	assert.NoError(t, statErr, "the export must have written the skill tree")
}

// TestSkillExport_DirectoryCollidingWithSkillFile — a directory sharing a name
// with an embedded skill file (a pre-flatten debt-resolve/, or a user-created
// SKILL.md/) is a name collision the user must resolve, not something to
// delete. The command's contract is "overwrites but never prunes": removing an
// EMPTY such directory is an undocumented prune, and failing on a NON-EMPTY one
// with a raw ENOTEMPTY (exit 1) misclassifies a usage mistake as a system
// error. Both forms must exit 2 naming the collision.
func TestSkillExport_DirectoryCollidingWithSkillFile(t *testing.T) {
	for _, tc := range []struct {
		name     string
		populate bool
	}{
		{"empty directory", false},
		{"non-empty directory", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "atcr")
			collision := filepath.Join(dest, "CONVENTIONS.md")
			require.NoError(t, os.MkdirAll(collision, 0o755))
			if tc.populate {
				require.NoError(t, os.WriteFile(filepath.Join(collision, "mine.md"), []byte("user data"), 0o644))
			}

			out, errOut, err := execSkillExport(t, "--dir", dest, "--force")
			require.Error(t, err, "a directory colliding with an embedded file name must fail, not be pruned")
			assert.Equal(t, 2, exitCode(err), "a name collision is a usage-grade error (exit 2)")
			assert.Contains(t, out+errOut+err.Error(), "CONVENTIONS.md", "the error must name the collision")

			info, statErr := os.Lstat(collision)
			require.NoError(t, statErr, "the colliding directory must be left in place")
			assert.True(t, info.IsDir(), "the colliding directory must not have been replaced")
		})
	}
}

// TestSkillExport_PartialWriteReportsWhatLanded — a mid-walk failure leaves the
// destination half-updated, and the error must say so: name that the tree is
// partially updated and list at least one file that was already replaced, or
// the user cannot tell a half-written tree from an untouched one. A non-empty
// directory named after the 4th-walked embedded file forces the abort after
// three files have landed.
func TestSkillExport_PartialWriteReportsWhatLanded(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")
	collision := filepath.Join(dest, "debt-resolve.md")
	require.NoError(t, os.MkdirAll(collision, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(collision, "mine.md"), []byte("user data"), 0o644))

	out, errOut, err := execSkillExport(t, "--dir", dest, "--force")
	require.Error(t, err, "a mid-walk collision must fail the export")

	msg := out + errOut + err.Error()
	assert.Contains(t, msg, "partially updated", "the error must state the tree is half-updated")
	assert.Contains(t, msg, "CONVENTIONS.md", "the error must name a file that was already replaced")
}

// TestSkillExport_CleanDestinationWarnsAboutNothing — the warning must not fire on
// the ordinary path, or it becomes noise every user learns to ignore.
func TestSkillExport_CleanDestinationWarnsAboutNothing(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "atcr")

	out, errOut, err := execSkillExport(t, "--dir", dest)
	require.NoError(t, err)
	assert.NotContains(t, out+errOut, "warning:", "a clean export must emit no leftover warning")
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

	_, _, err := execSkillExport(t, "--dir", dest, "--force")
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

	out, errOut, err := execSkillExport(t, "--dir", dest)
	require.Error(t, err)
	assert.Equal(t, 2, exitCode(err), "a non-directory destination is a usage error")
	assert.Contains(t, out+errOut+err.Error(), "not a directory", "the message must name the actual problem")
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

	_, _, err := execSkillExport(t)
	require.NoError(t, err, "the default invocation must succeed")

	_, statErr := os.Stat(filepath.Join(".claude", "skills", "atcr", "SKILL.md"))
	assert.NoError(t, statErr, "the default harness must write to its documented project path")
}
