package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/atomicfs"
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
			refresh, err := cmd.Flags().GetBool("refresh-personas")
			if err != nil {
				return err
			}
			dryRun, err := cmd.Flags().GetBool("dry-run")
			if err != nil {
				return err
			}
			out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if refresh {
				if force {
					return usageError(errors.New("--refresh-personas cannot be combined with --force"))
				}
				// A refresh touches only the built-in persona copies: no
				// config.yaml, no .gitignore, no community fetch.
				return runRefreshPersonas(".", dryRun, out)
			}
			if dryRun {
				return usageError(errors.New("--dry-run requires --refresh-personas"))
			}
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
			// nil roster → install the community set the fetched index publishes
			// (Option B / TD-011); builtins.Names() is the embedded-scaffold roster,
			// not the community fetch-and-pin roster.
			return installCommunityPersonas(personasClient, commpersonas.BaseURL(), dir, nil, out, errOut)
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing configuration and persona files")
	cmd.Flags().Bool("offline", false, "skip the community persona fetch; scaffold from embedded built-ins only")
	cmd.Flags().Bool("refresh-personas", false, "overwrite built-in persona copies under .atcr/personas that differ from the embedded ones, hand edits included")
	cmd.Flags().Bool("dry-run", false, "with --refresh-personas: list the files it would overwrite and write nothing")
	return cmd
}

// refreshWriteFile writes one refreshed persona file. It is a variable so a test
// can fail a write partway through a refresh.
var refreshWriteFile = atomicfs.WriteFileAtomic

// runRefreshPersonas brings every built-in persona copy under dir's
// .atcr/personas (`_base.md` included) back to the embedded content (Epic
// 35.16.16). It first lists each copy that differs from the embedded one,
// marking those that also differ from the stamp as edited; with dryRun it stops
// there and writes nothing. A copy that is missing is not recreated, and
// config.yaml, .gitignore and community personas are never touched. Git is the
// undo: the generated .atcr/.gitignore keeps personas/ tracked.
//
// Each file is written by an atomic temp-file write and rename, so a symlinked
// copy is replaced by a regular file and its target is left untouched. The
// stamp is written last, so a run that fails partway leaves the stamp as it was
// and a re-run finishes the job. A missing .atcr/personas or one that cannot be
// written to is a named error, raised before any file changes.
func runRefreshPersonas(dir string, dryRun bool, out io.Writer) error {
	personasDir := filepath.Join(dir, ".atcr", "personas")
	info, err := os.Stat(personasDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("cannot refresh personas: %s does not exist — run atcr init first", personasDir)
	case err != nil:
		return fmt.Errorf("cannot refresh personas: %w", err)
	case !info.IsDir():
		return fmt.Errorf("cannot refresh personas: %s is not a directory", personasDir)
	}

	type refresh struct {
		file, content string
		edited        bool // differs from its stamp entry
		stamped       bool
	}
	stamp, hadStamp := registry.ReadPersonaStamp(personasDir)
	if !hadStamp {
		stamp = registry.NewPersonaStamp()
	}
	stampChanged := false
	var todo []refresh
	base, err := builtins.Base()
	if err != nil {
		return err
	}
	files := []refresh{{file: "_base.md", content: base}}
	for _, name := range builtins.Names() {
		content, err := builtins.Get(name)
		if err != nil {
			return err
		}
		files = append(files, refresh{file: name + ".md", content: content})
	}
	for _, f := range files {
		path := filepath.Join(personasDir, f.file)
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue // no copy to refresh
		} else if err != nil {
			return fmt.Errorf("cannot check %s: %w", path, err)
		}
		embeddedHash := registry.HashPersonaContent([]byte(f.content))
		stampHash, stamped := stamp.Files[f.file]
		var fileHash string
		if data, err := os.ReadFile(path); err == nil {
			fileHash = registry.HashPersonaContent(data)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cannot read %s: %w", path, err)
		} // a dangling symlink has no content: it differs
		if fileHash == embeddedHash && info.Mode().IsRegular() {
			// Already current. Record it so a re-run after an interrupted
			// refresh leaves an accurate entry for a file the first run wrote.
			if stampHash != embeddedHash {
				stamp.Record(f.file, []byte(f.content))
				stampChanged = true
			}
			continue
		}
		f.stamped = stamped
		f.edited = stamped && fileHash != stampHash
		todo = append(todo, f)
	}

	if len(todo) == 0 {
		_, _ = fmt.Fprintln(out, "Persona files are current; nothing to refresh")
		if dryRun || !stampChanged {
			return nil
		}
	} else {
		verb := "Refreshing"
		if dryRun {
			verb = "Would refresh"
		}
		_, _ = fmt.Fprintf(out, "%s %d persona file(s) in %s:\n", verb, len(todo), personasDir)
		for _, f := range todo {
			note := ""
			switch {
			case f.edited:
				note = " (edited by hand — your changes will be overwritten)"
			case !f.stamped:
				note = " (no stamp, edits unknown)"
			}
			_, _ = fmt.Fprintf(out, "  %s%s\n", f.file, note)
		}
		if dryRun {
			return nil
		}
	}

	// Prove the directory is writable before changing anything, so a
	// read-only directory fails with every file and the stamp as they were.
	probe, err := os.CreateTemp(personasDir, ".refresh-probe-*")
	if err != nil {
		return fmt.Errorf("cannot refresh personas: %s is not writable: %w", personasDir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	for _, f := range todo {
		// The rename replaces the directory entry itself, so a symlink becomes
		// a regular file and its target is never written through.
		path := filepath.Join(personasDir, f.file)
		if err := refreshWriteFile(path, []byte(f.content)); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		stamp.Record(f.file, []byte(f.content))
	}
	if err := registry.WritePersonaStamp(personasDir, stamp); err != nil {
		return fmt.Errorf("failed to write %s: %w", registry.PersonaStampPath(personasDir), err)
	}
	if len(todo) > 0 {
		_, _ = fmt.Fprintf(out, "Refreshed %d persona file(s); review the changes with git diff\n", len(todo))
	}
	return nil
}

// atcrGitignore is dropped at .atcr/.gitignore by `atcr init` so the runtime
// outputs atcr writes under .atcr/ (the diff cache, up to cache_max_bytes,
// reviewer outputs — both can hold source snippets and review prose — and the
// finding-history ledger) are never accidentally committed, even by end users who
// never manually ignore .atcr/. The editable config.yaml and personas/ alongside
// this file stay tracked.
//
// history/ is listed for a stronger reason than tidiness: internal/history.Append
// creates shards 0600 and their dir 0700 because the ledger is local, uncommitted
// state that records which packages accrue which findings. Committing it defeats
// that outright — git carries no mode beyond +x, so a fresh clone recreates the
// shards at 0644/0755 under the default umask. This entry is what makes that
// premise true for a user who never ignores .atcr/ by hand.
const atcrGitignore = `# Written by atcr init. Runtime outputs under .atcr/ — do not commit.
# The editable config.yaml and personas/ alongside this file stay tracked.
cache/
reviews/
history/
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
const offlineHint = " — retry with --force --offline to use the embedded built-in personas"

// installCommunityPersonas fetches the community index from baseURL and installs
// each roster persona present in it as a self-contained unit (<name>.yaml plus a
// co-located <name>.md when the persona ships a custom prompt) into destDir, the
// resolver's community pin dir. The pin is the fetched YAML's own version field
// (read back by `personas list`/`upgrade`). An empty index is a hard, non-silent
// error; a roster persona the index does not advertise is skipped with a warning
// (init still succeeds).
//
// The roster reconciliation is Option B (closes 19.6's TD-011 HIGH): when roster
// is nil — the sole production caller shape from both `init` (init.go) and
// `quickstart` (quickstart.go) — the install set is derived from the fetched
// index's own entries, so online init/quickstart install exactly what the index
// publishes and self-heal as it grows (no hardcoded builtins.Names() list to keep
// in lockstep, no misleading "not found" warnings). Both call sites pass nil, so
// this derivation is expressed in exactly ONE shared location and the two cannot
// drift the way TD-006/TD-007 did. A non-nil roster restricts the install to those
// names (a name absent from the index is skip-with-warning) — the shape tests use
// and the escape hatch that keeps the genuine-absence warning path reachable.
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
	// Pre-filter index entries whose name fails persona-name validation (empty or
	// containing characters outside [a-zA-Z0-9_/-]): skip-with-warning here so one
	// malformed index entry does not reach InstallUnit->validatePersonaName and trip
	// the all-or-nothing rollback that would abort init/quickstart for every user.
	// This is distinct from the AC-07-03 rollback on genuine per-unit INSTALL failures.
	indexed := make(map[string]struct{}, len(entries))
	indexOrder := make([]string, 0, len(entries))
	for _, e := range entries {
		if !commpersonas.ValidName(e.Name) {
			_, _ = fmt.Fprintf(errOut, "persona %q from the community index has an invalid name — skipping\n", e.Name)
			continue
		}
		indexed[e.Name] = struct{}{}
		indexOrder = append(indexOrder, e.Name)
	}

	// Option B reconciliation: a nil roster means "install what the index
	// publishes." Derive it from the validated, already-fetched entries (no extra
	// network round-trip), preserving index order so the install is deterministic.
	if roster == nil {
		roster = indexOrder
	}

	// Track every unit file this run might create so a failure rolls back exactly
	// the files this run created — never a pre-existing one (all-or-nothing).
	// Candidates are recorded BEFORE the install call so the currently-failing
	// persona's own partial write is included in the rollback, not just prior
	// successes. The file-exists guard above has already proven both paths absent
	// when a candidate is recorded, so pre-existence tracking is unnecessary.
	var candidates []string
	rollback := func() {
		for _, path := range candidates {
			if fileExists(path) {
				_ = os.Remove(path)
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
		candidates = append(candidates, yamlPath, mdPath)

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
//
// It also records the persona stamp (.atcr/personas/.stamp.json, Epic 35.16.16):
// each persona file it writes is entered with the sha256 of the content written,
// and the entries of files it preserved are kept as they were. The stamp is
// written last, after the files it describes. A workspace with no stamp is never
// retro-stamped: a run that writes no persona file leaves a missing stamp missing.
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

	stamp, hadStamp := registry.ReadPersonaStamp(personasDir)
	if !hadStamp {
		stamp = registry.NewPersonaStamp()
	}
	stamped := false

	var created []string
	// write creates path with content. preserve marks a persona file, which is
	// NEVER overwritten if it already exists — even under --force (AC 01-05 /
	// Phase 3 Clarification Q1): hand edits survive. --force only overwrites the
	// non-persona scaffold (config.yaml, .gitignore). An existing persona file
	// (regular or symlink) is skipped, so a pre-planted symlink is never written
	// through either. A persona file it does write is recorded in the stamp; a
	// preserved one keeps whatever entry the stamp already held for it.
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
		if preserve {
			stamp.Record(filepath.Base(path), []byte(content))
			stamped = true
		}
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
	if stamped {
		if err := registry.WritePersonaStamp(personasDir, stamp); err != nil {
			return fmt.Errorf("failed to write %s: %w", registry.PersonaStampPath(personasDir), err)
		}
	}

	_, _ = fmt.Fprintln(out, "Initialized atcr workspace:")
	for _, path := range created {
		_, _ = fmt.Fprintf(out, "  created %s\n", path)
	}
	_, _ = fmt.Fprintln(out, "Next: define providers and agents in ~/.config/atcr/registry.yaml (see docs/registry.md)")
	return nil
}
