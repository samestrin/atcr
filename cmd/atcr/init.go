package main

import (
	"fmt"
	"io"
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
			return runInit(".", force, cmd.OutOrStdout())
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and persona files")
	return cmd
}

// runInit writes .atcr/config.yaml and the editable persona files under dir.
// Without force, an existing config is a hard error and nothing is touched.
func runInit(dir string, force bool, out io.Writer) error {
	configPath := filepath.Join(dir, ".atcr", "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		if !force {
			return usageError(fmt.Errorf("config already exists at .atcr/config.yaml — use --force to overwrite"))
		}
		_, _ = fmt.Fprintln(out, "Overwriting existing configuration and persona files")
	}

	personasDir := filepath.Join(dir, ".atcr", "personas")
	if err := os.MkdirAll(personasDir, 0o755); err != nil {
		return fmt.Errorf("cannot create .atcr/: %w", err)
	}

	var created []string
	write := func(path, content string) error {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		created = append(created, path)
		return nil
	}

	roster := personas.Names()
	if err := write(configPath, registry.DefaultProjectConfigYAML(roster)); err != nil {
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
