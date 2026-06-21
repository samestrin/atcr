package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/personas"
)

// newInitCmd builds `atcr init`: write the project config and editable
// persona files from embedded defaults.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write .atcr/config.yaml and editable persona files",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			return runInit(".", force, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and persona files")
	return cmd
}

// atcrGitignore is dropped at .atcr/.gitignore by `atcr init` so the runtime
// outputs atcr writes under .atcr/ (the diff cache, up to cache_max_bytes, and
// reviewer outputs — both can hold source snippets and review prose) are never
// accidentally committed, even by end users who never manually ignore .atcr/.
// The editable config.yaml and personas/ alongside this file stay tracked.
const atcrGitignore = `# Written by atcr init. Runtime outputs under .atcr/ — do not commit.
# The editable config.yaml and personas/ alongside this file stay tracked.
cache/
reviews/
`

// initTargets returns every path `atcr init` writes under dir, config first.
func initTargets(dir string) []string {
	personasDir := filepath.Join(dir, ".atcr", "personas")
	targets := []string{
		filepath.Join(dir, ".atcr", "config.yaml"),
		filepath.Join(dir, ".atcr", ".gitignore"),
		filepath.Join(personasDir, "_base.md"),
	}
	for _, name := range personas.Names() {
		targets = append(targets, filepath.Join(personasDir, name+".md"))
	}
	return targets
}

// runInit writes .atcr/config.yaml and the editable persona files under dir.
// Without force, ANY existing target file is a hard error and nothing is
// touched. Warnings go to errOut; the created-files report goes to out.
func runInit(dir string, force bool, out, errOut io.Writer) error {
	targets := initTargets(dir)
	anyExist := false
	for _, path := range targets {
		_, err := os.Lstat(path)
		switch {
		case err == nil:
			anyExist = true
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("cannot check %s: %w", path, err)
		}
	}
	if anyExist && !force {
		return usageError(errors.New("config already exists at .atcr/config.yaml — use --force to overwrite"))
	}
	if anyExist {
		_, _ = fmt.Fprintln(errOut, "Overwriting existing configuration and persona files")
	}

	personasDir := filepath.Join(dir, ".atcr", "personas")
	if err := os.MkdirAll(personasDir, 0o755); err != nil {
		return fmt.Errorf("cannot create .atcr/: %w", err)
	}
	// Pin documented modes regardless of process umask.
	for _, d := range []string{filepath.Join(dir, ".atcr"), personasDir} {
		if err := os.Chmod(d, 0o755); err != nil {
			return fmt.Errorf("cannot set permissions on %s: %w", d, err)
		}
	}

	var created []string
	write := func(path, content string) error {
		if force {
			// Drop any existing file (or symlink — never write through one).
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("failed to write %s: %w", path, err)
			}
		}
		// O_EXCL makes concurrent inits fail loudly instead of clobbering
		// each other silently.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		_, werr := f.WriteString(content)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			return fmt.Errorf("failed to write %s: %w", path, errors.Join(werr, cerr))
		}
		if err := os.Chmod(path, 0o644); err != nil {
			return fmt.Errorf("cannot set permissions on %s: %w", path, err)
		}
		created = append(created, path)
		return nil
	}

	roster := personas.Names()
	if err := write(targets[0], registry.DefaultProjectConfigYAML(roster)); err != nil {
		return err
	}

	if err := write(filepath.Join(dir, ".atcr", ".gitignore"), atcrGitignore); err != nil {
		return err
	}

	base, err := personas.Base()
	if err != nil {
		return err
	}
	if err := write(filepath.Join(personasDir, "_base.md"), base); err != nil {
		return err
	}
	for _, name := range roster {
		content, err := personas.Get(name)
		if err != nil {
			return err
		}
		if err := write(filepath.Join(personasDir, name+".md"), content); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintln(out, "Initialized atcr workspace:")
	for _, path := range created {
		_, _ = fmt.Fprintf(out, "  created %s\n", path)
	}
	_, _ = fmt.Fprintln(out, "Next: define providers and agents in ~/.config/atcr/registry.yaml (see docs/registry.md)")
	return nil
}
