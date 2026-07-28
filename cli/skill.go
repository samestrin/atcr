package cli

import (
	"errors"
	"fmt"
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
		return dir, nil
	}

	h, ok := skillHarnesses[harness]
	if !ok {
		return "", usageError(fmt.Errorf(
			"unknown --harness %q: known harnesses are %s; for anything else pass --dir <path> to write the skill directory explicitly",
			harness, strings.Join(knownHarnesses(), ", ")))
	}

	path := h.project
	if user {
		path = h.user
	}
	// expandHome (quickstart.go) turns the table's leading `~/` into the real home
	// directory; a project-relative path has no `~` and passes through unchanged.
	expanded, err := expandHome(path)
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the %s user-level install path: %w", harness, err)
	}
	return expanded, nil
}

// dirIsNonEmpty reports whether path exists and holds at least one entry. A
// missing path is empty, not an error: creating it is the ordinary case.
func dirIsNonEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
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

func runSkillExport(cmd *cobra.Command, _ []string) error {
	harness := mustFlag(cmd, "harness")
	dir := mustFlag(cmd, "dir")
	user, _ := cmd.Flags().GetBool("user")
	force, _ := cmd.Flags().GetBool("force")

	dest, err := resolveSkillDest(harness, user, dir)
	if err != nil {
		return err
	}

	nonEmpty, err := dirIsNonEmpty(dest)
	if err != nil {
		return fmt.Errorf("inspecting destination %s: %w", dest, err)
	}
	if nonEmpty && !force {
		return usageError(fmt.Errorf(
			"destination %s already exists and is not empty; re-run with --force to overwrite it", dest))
	}

	written, err := writeSkillTree(dest)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Exported %d skill file(s) to %s\n", written, dest); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Point your agent at %s, or restart it if it indexes skills at startup.\n", dest)
	return err
}

// writeSkillTree writes the embedded skill directory's contents to dest and
// returns the number of files written. The contents are copied, not the directory
// itself, because a harness skill directory is named for the skill (dest is
// already `.../atcr`). Files land 0644 inside a 0755 directory: this is
// instruction text an agent reads, never a secret and never executable.
func writeSkillTree(dest string) (int, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return 0, fmt.Errorf("creating destination %s: %w", dest, err)
	}

	written := 0
	err := fs.WalkDir(skills.Tree, skills.SkillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skills.SkillDir, path)
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

		data, err := skills.Tree.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", path, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		written++
		return nil
	})
	if err != nil {
		return written, err
	}
	if written == 0 {
		return 0, fmt.Errorf("no skill files embedded under %s — this is a build defect", skills.SkillDir)
	}
	return written, nil
}
