package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/quickstart"
	"github.com/samestrin/atcr/internal/registry"
	builtins "github.com/samestrin/atcr/personas"
)

// quickstartOpts carries the inputs for an `atcr quickstart` run. Streams and
// the browser-open hook are injectable so the interactive flow is unit-testable
// without a TTY or a real browser.
type quickstartOpts struct {
	dir    string
	force  bool
	open   bool
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	openFn func(string) error
}

// newQuickstartCmd builds `atcr quickstart`: the interactive onboarding wizard.
// It scaffolds the same .atcr/ workspace as `atcr init` (reusing its writers),
// then layers on an interactive synthetic-provider + key-env setup and a CI
// workflow scaffold so a new user reaches their first review quickly.
func newQuickstartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Interactive onboarding: scaffold config, provider, and a CI workflow",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			open, err := cmd.Flags().GetBool("open")
			if err != nil {
				return err
			}
			return runQuickstart(quickstartOpts{
				dir:    ".",
				force:  force,
				open:   open,
				in:     cmd.InOrStdin(),
				out:    cmd.OutOrStdout(),
				errOut: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and workflow files")
	cmd.Flags().Bool("open", false, "open the provider signup page in a browser")
	return cmd
}

// runQuickstart orchestrates the onboarding wizard. It first reuses `atcr init`'s
// writers to lay down .atcr/config.yaml and the editable personas, then (in later
// steps) sets up the synthetic provider, guides the user through the API-key env
// var, and scaffolds a CI workflow.
func runQuickstart(o quickstartOpts) error {
	if err := runInit(o.dir, o.force, o.out, o.errOut); err != nil {
		return err
	}

	manifest, err := quickstart.LoadManifest()
	if err != nil {
		return err
	}

	// The project roster init just wrote lists the persona names; define one
	// synthetic-bound agent per persona so the roster resolves.
	if err := writeSyntheticRegistry(o, manifest, builtins.Names()); err != nil {
		return err
	}
	return nil
}

// writeSyntheticRegistry writes the synthetic provider + agents to the user
// registry (~/.config/atcr/registry.yaml). It is non-destructive: an existing
// registry is never clobbered without --force. When one exists and force is off,
// the generated block is printed for the user to merge by hand rather than
// silently overwriting their providers/agents.
func writeSyntheticRegistry(o quickstartOpts, m *quickstart.Manifest, roster []string) error {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return err
	}
	content := quickstart.RegistryYAML(m, roster)

	_, statErr := os.Lstat(regPath)
	switch {
	case statErr == nil && !o.force:
		// Exists and no force: do not touch it. Show the block to merge.
		_, _ = fmt.Fprintf(o.errOut, "\nA registry already exists at %s — not overwriting it (use --force to replace).\n", regPath)
		_, _ = fmt.Fprintln(o.errOut, "Add the following synthetic provider + agents to it manually:")
		_, _ = fmt.Fprintf(o.out, "\n%s\n", content)
		return nil
	case statErr != nil && !errors.Is(statErr, fs.ErrNotExist):
		return fmt.Errorf("cannot check %s: %w", regPath, statErr)
	}

	if err := os.MkdirAll(filepath.Dir(regPath), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(regPath), err)
	}
	if err := os.WriteFile(regPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", regPath, err)
	}
	_, _ = fmt.Fprintf(o.out, "  created %s\n", regPath)
	return nil
}
