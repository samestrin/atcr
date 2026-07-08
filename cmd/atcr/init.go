package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	commpersonas "github.com/samestrin/atcr/internal/personas"
	"github.com/samestrin/atcr/internal/registry"
	builtins "github.com/samestrin/atcr/personas"
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
			offline, err := cmd.Flags().GetBool("offline")
			if err != nil {
				return err
			}
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := runInit(".", force, out, errOut); err != nil {
				return err
			}
			if offline {
				// Scaffold from embedded built-ins only; make zero network calls.
				return nil
			}
			dir, err := personasDir()
			if err != nil {
				return err
			}
			return installCommunityPersonas(personasClient, commpersonas.BaseURL(), dir, builtins.Names(), out, errOut)
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and persona files")
	cmd.Flags().Bool("offline", false, "skip the community persona fetch; scaffold from embedded built-ins only")
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
	for _, name := range builtins.Names() {
		targets = append(targets, filepath.Join(personasDir, name+".md"))
	}
	return targets
}

// offlineHint is appended to fetch-failure errors so a first-run user always has
// a clear escape hatch to a working, network-free workspace.
const offlineHint = " — retry, or run with --offline to use the embedded built-in personas"

// installCommunityPersonas fetches the community index from baseURL and installs
// each roster persona present in it as a self-contained unit (<name>.yaml plus a
// co-located <name>.md when the persona ships a custom prompt) into destDir, the
// resolver's community pin dir. The pin is the fetched YAML's own version field
// (read back by `personas list`/`upgrade`). An empty index is a hard, non-silent
// error; a roster persona the index does not advertise is skipped with a warning
// (init still succeeds).
//
// The roster install is all-or-nothing: any fetch/validation failure aborts with
// a descriptive error (naming the failure and suggesting --offline) and rolls back
// every persona file this run created, so a mid-roster failure never leaves a
// partial install behind. Pre-existing files are not touched by the rollback.
func installCommunityPersonas(client commpersonas.HTTPClient, baseURL, destDir string, roster []string, out, errOut io.Writer) error {
	entries, err := commpersonas.FetchIndex(client, baseURL)
	if err != nil {
		return fmt.Errorf("failed to fetch community personas: %w%s", err, offlineHint)
	}
	if len(entries) == 0 {
		return fmt.Errorf("community persona index is empty: no personas to install%s", offlineHint)
	}
	indexed := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		indexed[e.Name] = struct{}{}
	}

	// Track every unit file this run might create, with whether it pre-existed, so
	// a failure rolls back exactly the files this run created — never a pre-existing
	// one (all-or-nothing). Candidates are recorded BEFORE the install call so the
	// currently-failing persona's own partial write is included in the rollback,
	// not just prior successes.
	type rbCandidate struct {
		path       string
		preExisted bool
	}
	var candidates []rbCandidate
	rollback := func() {
		for _, c := range candidates {
			if !c.preExisted && fileExists(c.path) {
				_ = os.Remove(c.path)
			}
		}
	}

	for _, name := range roster {
		if _, ok := indexed[name]; !ok {
			_, _ = fmt.Fprintf(errOut, "persona %q not found in community index — skipping\n", name)
			continue
		}
		yamlPath := filepath.Join(destDir, filepath.FromSlash(name)+".yaml")
		mdPath := strings.TrimSuffix(yamlPath, ".yaml") + ".md"
		// Never overwrite an existing on-disk persona (AC 01-05): a hand-edited or
		// previously-pinned unit is left untouched; only missing ones install. The
		// guard covers EITHER file so a lone hand-edited <name>.md (no sibling
		// .yaml) is not silently clobbered by the install.
		if fileExists(yamlPath) || fileExists(mdPath) {
			_, _ = fmt.Fprintf(errOut, "persona %q already installed — leaving it untouched\n", name)
			continue
		}
		candidates = append(candidates,
			rbCandidate{yamlPath, fileExists(yamlPath)},
			rbCandidate{mdPath, fileExists(mdPath)},
		)

		if err := commpersonas.InstallUnit(client, baseURL, name, destDir); err != nil {
			rollback()
			return fmt.Errorf("failed to install community persona %q: %w%s", name, err, offlineHint)
		}
		_, _ = fmt.Fprintf(out, "Installed %s (community)\n", name)
	}
	return nil
}

// fileExists reports whether path is present. A non-ENOENT stat error is treated
// as "present" so the rollback bookkeeping errs toward NOT deleting a file it did
// not clearly create (a pre-existing file must never be removed by a rollback).
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

// runInit writes .atcr/config.yaml and the editable persona files under dir.
// Without force, ANY existing target file is a hard error and nothing is
// touched. Warnings go to errOut; the created-files report goes to out.
func runInit(dir string, force bool, out, errOut io.Writer) error {
	targets := initTargets(dir)
	var existing []string
	for _, path := range targets {
		_, err := os.Lstat(path)
		switch {
		case err == nil:
			existing = append(existing, path)
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("cannot check %s: %w", path, err)
		}
	}
	if len(existing) > 0 && !force {
		rels := make([]string, 0, len(existing))
		for _, p := range existing {
			rel := strings.TrimPrefix(p, dir+string(filepath.Separator))
			rels = append(rels, rel)
		}
		msg := fmt.Sprintf("existing files would be overwritten: %s — use --force to overwrite", strings.Join(rels, ", "))
		return usageError(errors.New(msg))
	}
	if len(existing) > 0 {
		_, _ = fmt.Fprintln(errOut, "Regenerating configuration (existing persona files are preserved)")
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
	// write creates path with content. preserve marks a persona file, which is
	// NEVER overwritten if it already exists — even under --force (AC 01-05 /
	// Phase 3 Clarification Q1): hand edits survive. --force only overwrites the
	// non-persona scaffold (config.yaml, .gitignore). An existing persona file
	// (regular or symlink) is skipped, so a pre-planted symlink is never written
	// through either.
	write := func(path, content string, preserve bool) error {
		if preserve {
			if _, err := os.Lstat(path); err == nil {
				return nil // exists → preserve untouched, skip
			} else if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("cannot check %s: %w", path, err)
			}
		}
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

	roster := builtins.Names()
	if err := write(targets[0], registry.DefaultProjectConfigYAML(roster), false); err != nil {
		return err
	}

	if err := write(filepath.Join(dir, ".atcr", ".gitignore"), atcrGitignore, false); err != nil {
		return err
	}

	base, err := builtins.Base()
	if err != nil {
		return err
	}
	if err := write(filepath.Join(personasDir, "_base.md"), base, true); err != nil {
		return err
	}
	for _, name := range roster {
		content, err := builtins.Get(name)
		if err != nil {
			return err
		}
		if err := write(filepath.Join(personasDir, name+".md"), content, true); err != nil {
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
