package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/skills"
)

// skillHarness holds the install directories a single agent harness reads skills
// from: project is repo-root-relative, user is absolute and may lead with `~`
// (expanded against the real home directory at resolve time, never at init, so
// the table stays a comparable literal for the pinning test).
type skillHarness struct {
	project string
	user    string
}

// skillHarnesses is the single harness→path table. Everything about where an
// export lands is decided here; keep it the only place these paths appear so
// external convention drift is one edit, and cli/skill_test.go's pinning test is
// what makes that drift visible instead of silent.
//
// Two cross-reading conventions make this cheaper than one export per tool:
//
//   - .claude/skills/ is read natively by Claude Code, Kimi CLI, and opencode
//     (Kimi merges brand directories kimi > claude > codex; opencode scans
//     .claude/skills/*/SKILL.md directly), so the default already serves three.
//   - .agents/skills/ is the vendor-neutral path, read by Kimi CLI, opencode, and
//     Antigravity CLI. It is exposed as `agents` for a single tool-agnostic install.
//
// Two user-level paths are deliberately non-obvious and would be wrong if guessed:
// antigravity's is ~/.gemini/config/skills/ (a Gemini CLI → Antigravity CLI
// legacy, NOT ~/.agents/), and opencode's is ~/.config/opencode/skills/ (NOT
// ~/.opencode/). Verified 2026-07-28; worth re-checking each release.
var skillHarnesses = map[string]skillHarness{
	"claude":      {project: ".claude/skills/atcr", user: "~/.claude/skills/atcr"},
	"codex":       {project: ".codex/skills/atcr", user: "~/.codex/skills/atcr"},
	"kimi":        {project: ".kimi/skills/atcr", user: "~/.kimi/skills/atcr"},
	"opencode":    {project: ".opencode/skills/atcr", user: "~/.config/opencode/skills/atcr"},
	"antigravity": {project: ".agents/skills/atcr", user: "~/.gemini/config/skills/atcr"},
	"agents":      {project: ".agents/skills/atcr", user: "~/.agents/skills/atcr"},
}

// knownHarnesses returns the harness names sorted, for deterministic error text.
func knownHarnesses() []string {
	names := make([]string, 0, len(skillHarnesses))
	for name := range skillHarnesses {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveSkillDest resolves where the skill tree should be written.
//
// An explicit dir wins outright and is used verbatim — it is the destination
// directory itself, not a parent to append a skill name onto (matching every
// other --dir/--output-dir in this CLI). Because it wins outright, harness is not
// validated when dir is set: that is precisely how an unlisted harness stays
// reachable.
func resolveSkillDest(harness string, user bool, dir string) (string, error) {
	if dir != "" {
		// A quoted or config-supplied --dir '~/...' would otherwise create a
		// directory literally named `~` under the cwd. The flag's own help text
		// uses a ~/ example, so it must expand like the table paths do.
		expanded, err := expandHome(dir)
		if err != nil {
			return "", fmt.Errorf("resolving home directory in --dir %q: %w", dir, err)
		}
		return expanded, nil
	}

	h, ok := skillHarnesses[harness]
	if !ok {
		return "", usageError(fmt.Errorf(
			"unknown --harness %q: known harnesses are %s; for anything else pass --dir <path> to write the skill directory explicitly",
			harness, strings.Join(knownHarnesses(), ", ")))
	}

	if !user {
		return h.project, nil
	}
	// expandHome (quickstart.go) turns the table's leading `~/` into the real home
	// directory. Only the user-level branch reaches it — a project-relative path
	// has no `~` — so a failure here always names the user-level install path.
	expanded, err := expandHome(h.user)
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the %s user-level install path: %w", harness, err)
	}
	return expanded, nil
}

// dirIsNonEmpty reports whether path exists and holds at least one meaningful
// entry. A missing path is empty, not an error: creating it is the ordinary
// case. Dot-prefixed entries (.DS_Store, editor swap files) are OS metadata,
// not content, and never make a directory non-empty. A path that exists but is
// not a directory is a usage error, reported as such rather than surfacing a
// raw ENOTDIR from the subsequent read.
func dirIsNonEmpty(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, usageError(fmt.Errorf(
			"destination %s exists and is not a directory; pass --dir <path> to name a directory instead", path))
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			return true, nil
		}
	}
	return false, nil
}

// newSkillCmd builds `atcr skill`: install the Agent Skill that ships inside this
// binary. The skill tree is embedded (package skills) purely for build-time
// verification until now; export is what makes that embedded copy reachable, so
// installing no longer means hand-copying files out of a source checkout.
func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the atcr Agent Skill that ships inside this binary",
		Long: "atcr skill installs the Agent Skill embedded in this binary.\n" +
			"`atcr skill export` writes the skill directory to the location your agent\n" +
			"harness reads skills from, so no source checkout or manual copy is needed.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newSkillExportCmd())
	return cmd
}

func newSkillExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write the embedded skill tree to your agent harness's skills directory",
		Long: "atcr skill export writes the embedded skill directory to disk, ready to load.\n\n" +
			"--harness selects a known install convention (default claude). --user switches\n" +
			"from the project-level path to the user-level one. --dir overrides both and is\n" +
			"always available for a harness this table does not list.\n\n" +
			"Note that .claude/skills/ is also read natively by Kimi CLI and opencode, and\n" +
			"the vendor-neutral .agents/skills/ (--harness agents) is read by Kimi CLI,\n" +
			"opencode, and Antigravity CLI — so one export usually serves several tools.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runSkillExport,
	}
	cmd.Flags().String("harness", "claude",
		"target harness install convention: "+strings.Join(knownHarnesses(), "|"))
	cmd.Flags().Bool("user", false,
		"write to the harness's user-level skills directory instead of the project-level one")
	cmd.Flags().String("dir", "",
		"write to this directory instead, overriding --harness/--user; it is the skill directory itself, e.g. ~/.foo/skills/atcr")
	cmd.Flags().Bool("force", false,
		"overwrite an existing non-empty destination")
	return cmd
}

// runSkillExport writes the embedded skill tree to the resolved destination.
//
// The non-empty check and the write are deliberately NOT atomic: there is a
// TOCTOU window between dirIsNonEmpty and writeSkillTree with no lock or O_EXCL,
// so --force is advisory — a concurrent export, an editor, or a second process
// can populate dest in between and the write proceeds. The check exists to stop
// the common case (clobbering an existing install by accident), not to
// serialize concurrent writers; at this scale a lock file is not warranted.
func runSkillExport(cmd *cobra.Command, _ []string) error {
	harness := mustFlag(cmd, "harness")
	dir := mustFlag(cmd, "dir")
	user, _ := cmd.Flags().GetBool("user")
	force, _ := cmd.Flags().GetBool("force")

	// A set-but-empty --dir (`--dir "$UNSET_VAR"`) is an invocation mistake, not
	// "no --dir given": resolveSkillDest treats "" as absent and the export would
	// silently land on the harness's default path. Changed() is the same idiom
	// audit_report.go and debt_resolve.go use for set-vs-empty.
	if cmd.Flags().Changed("dir") && dir == "" {
		return usageError(fmt.Errorf(
			"--dir was supplied empty (an unset variable in --dir \"$VAR\" is the usual cause); omit --dir to use the harness path, or pass a non-empty directory"))
	}

	dest, err := resolveSkillDest(harness, user, dir)
	if err != nil {
		return err
	}

	nonEmpty, err := dirIsNonEmpty(dest)
	if err != nil {
		if exitCode(err) == exitUsage {
			return err // already a usage error with an actionable message
		}
		return fmt.Errorf("inspecting destination %s: %w", dest, err)
	}
	if nonEmpty && !force {
		return usageError(fmt.Errorf(
			"destination %s already exists and is not empty; re-run with --force to overwrite it", dest))
	}

	written, err := writeSkillTree(skills.Tree, skills.SkillDir, dest)
	if err != nil {
		// A partial write leaves a half-updated tree; report what was NOT
		// written as well, so the failure leaves the same leftover inventory
		// the success path does.
		if len(written) > 0 {
			warnStaleSkillFiles(cmd.ErrOrStderr(), dest, written)
		}
		return err
	}
	warnStaleSkillFiles(cmd.ErrOrStderr(), dest, written)

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Exported %d skill file(s) to %s\n", len(written), dest); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Point your agent at %s, or restart it if it indexes skills at startup.\n", dest)
	return err
}

// warnStaleSkillFiles reports anything in dest that this export did not write.
// Export overwrites rather than prunes — deleting files a user put in their own
// skills directory is not this command's call — but silence would be worse than
// the mess: an install predating Epic 35.5's flatten leaves behind a nested
// debt-resolve/SKILL.md declaring its own skill name, which is precisely the
// defect the flatten removed and which a harness would happily load alongside the
// real one. Naming the leftovers lets the user delete them deliberately.
// Dot-prefixed entries (.DS_Store, editor swap files) are OS metadata, not
// skill files, and are never reported.
func warnStaleSkillFiles(errOut io.Writer, dest string, written map[string]bool) {
	var stale []string
	err := filepath.WalkDir(dest, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dest && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, relErr := filepath.Rel(dest, path)
		if relErr != nil {
			return relErr
		}
		if rel = filepath.ToSlash(rel); !written[rel] {
			stale = append(stale, rel)
		}
		return nil
	})
	if len(stale) > 0 {
		sort.Strings(stale)
		_, _ = fmt.Fprintf(errOut,
			"warning: %s holds %d file(s) this export did not write, left in place: %s\n",
			dest, len(stale), strings.Join(stale, ", "))
		_, _ = fmt.Fprintln(errOut,
			"         If any are from an older atcr skill install, delete them — a leftover nested SKILL.md is loaded as a second skill.")
	}
	if err != nil {
		// The walk aborted mid-scan. What was found before the error is still
		// reported above — discarding it would contradict the point of the
		// warning — and the scan's failure must be admitted, or a broken tree
		// looks exactly like a clean one.
		_, _ = fmt.Fprintf(errOut,
			"warning: leftover scan of %s was incomplete (%v); other leftovers may be unreported\n", dest, err)
	}
}

// writeSkillTree writes the contents of src's root directory to dest and
// returns the set of destination-relative paths it wrote. The contents are
// copied, not the directory itself, because a harness skill directory is named
// for the skill (dest is already `.../atcr`). Files land 0644 inside a 0755
// directory: this is instruction text an agent reads, never a secret and never
// executable.
//
// Production always passes skills.Tree and skills.SkillDir; the parameters exist
// so tests can drive a NESTED source tree. The embedded tree is flat today, so
// the subdirectory branch below is otherwise unreachable — and an untestable
// path-safety guard is one nobody notices breaking.
//
// Error contract: the returned map always holds exactly what was written
// before the failure — the full set on a mid-walk partial write, nil when no
// write was attempted — so the caller can inventory a half-updated tree.
// (An empty embedded tree cannot reach the success path: fs.WalkDir errors on
// the missing root long before the walk body runs.)
func writeSkillTree(src fs.FS, root, dest string) (map[string]bool, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, fmt.Errorf("creating destination %s: %w", dest, err)
	}

	written := map[string]bool{}
	err := fs.WalkDir(src, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))

		if d.IsDir() {
			if rel == "." {
				return nil
			}
			return os.MkdirAll(target, 0o755)
		}

		data, err := fs.ReadFile(src, path)
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", path, err)
		}
		// Remove any existing entry before writing. os.WriteFile follows a
		// symlink and would write the skill content THROUGH it, clobbering a file
		// outside the destination entirely — a real hazard for anyone who
		// symlinked an installed skill file back to a source checkout. Replacing
		// the entry keeps every write inside dest.
		//
		// A DIRECTORY sharing the file's name is never removed: deleting an
		// empty one would be an undocumented prune (the contract is "overwrites
		// but never prunes") and os.Remove on a non-empty one fails with a raw
		// ENOTEMPTY. Both are the user's call to resolve, so they are reported
		// as a usage-grade collision instead.
		info, statErr := os.Lstat(target)
		switch {
		case statErr == nil && info.IsDir():
			return usageError(fmt.Errorf(
				"replacing %s: a directory already exists with that name; move it aside and re-run", target))
		case statErr != nil && !errors.Is(statErr, os.ErrNotExist):
			return fmt.Errorf("inspecting %s: %w", target, statErr)
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("replacing %s: %w", target, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		written[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		// A mid-walk failure leaves the destination holding a mix of new and old
		// files. Name what did land so the user knows the tree is half-updated
		// rather than untouched.
		if len(written) > 0 {
			done := make([]string, 0, len(written))
			for name := range written {
				done = append(done, name)
			}
			sort.Strings(done)
			return written, fmt.Errorf(
				"%w (destination %s is now partially updated — these file(s) were already replaced: %s; fix the cause named above and re-run with --force, which the now non-empty destination requires)",
				err, dest, strings.Join(done, ", "))
		}
		return written, err
	}
	return written, nil
}
