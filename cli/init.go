package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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
				if offline {
					return usageError(errors.New("--refresh-personas cannot be combined with --offline; a refresh never fetches"))
				}
				// A refresh touches only the built-in persona copies: no
				// config.yaml, no .gitignore, no community fetch.
				return runRefreshPersonas(".", dryRun, out, atomicfs.WriteFileAtomic)
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

// runRefreshPersonas brings every built-in persona copy under dir's
// .atcr/personas (`_base.md` included) back to the embedded content (Epic
// 35.16.16). It first lists each copy that differs from the embedded one,
// marking those that also differ from the stamp as edited; with dryRun it stops
// there and writes nothing. A copy that is missing is not recreated, and
// config.yaml, .gitignore and community personas are never touched. Git is the
// undo: the generated .atcr/.gitignore keeps personas/ tracked.
//
// Each file is written by writeFile, which the command passes as
// atomicfs.WriteFileAtomic: an atomic temp-file write and rename, so a symlinked
// copy is replaced by a regular file and its target is left untouched. (A test
// passes a writer that fails partway.) The stamp is written last, merged into
// the stamp as it is on disk then, so a run that fails partway leaves the stamp
// as it was and a re-run finishes the job. A missing .atcr/personas or one that
// cannot be written to is a named error, raised before any file changes.
func runRefreshPersonas(dir string, dryRun bool, out io.Writer, writeFile func(path string, data []byte) error) error {
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
		state         copyState
		edited        bool // read, and differs from its stamp entry
		stamped       bool
	}
	stamp, _ := registry.ReadPersonaStamp(personasDir)
	// recorded holds the stamp entries this run sets; only these are merged
	// into the stamp as it is on disk when the run writes it.
	recorded := map[string]string{}
	stampFixed := 0 // current copies whose stamp entry this run corrects
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
		// Only a regular file is read. A symlink, FIFO or device is always
		// replaced, so its content is never needed — and reading through a
		// link to /dev/zero or a FIFO would hang.
		var fileHash string
		f.state = copyNonRegular
		if info.Mode().IsRegular() {
			if fileHash, f.state, err = hashPersonaCopy(path, info); err != nil {
				return fmt.Errorf("cannot read %s: %w", path, err)
			}
		}
		if f.state == copyRegular && fileHash == embeddedHash {
			// Already current. Record it so a re-run after an interrupted
			// refresh leaves an accurate entry for a file the first run wrote.
			if stampHash != embeddedHash {
				recorded[f.file] = embeddedHash
				stampFixed++
			}
			continue
		}
		f.stamped = stamped
		// A copy that was not read cannot be known to be edited by hand.
		f.edited = f.state == copyRegular && stamped && fileHash != stampHash
		todo = append(todo, f)
	}

	if len(todo) == 0 {
		switch {
		case stampFixed == 0:
			_, _ = fmt.Fprintln(out, "Persona files are current; nothing to refresh")
			return nil
		case dryRun:
			_, _ = fmt.Fprintf(out, "Persona files are current; would refresh the stamp for %d file(s)\n", stampFixed)
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
			case f.state == copyNonRegular:
				note = " (not a regular file — it will be replaced by one)"
			case f.state == copyOversized:
				note = " (over 1 MiB, not read — it will be replaced)"
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

	// Prove the directory is writable before changing any persona file, so a
	// read-only directory fails with every file and the stamp as they were. A
	// stamp-only refresh skips the probe: its one write is atomic on its own.
	if len(todo) > 0 {
		if err := probeWritable(personasDir); err != nil {
			return err
		}
	}

	for _, f := range todo {
		// The rename replaces the directory entry itself, so a symlink becomes
		// a regular file and its target is never written through.
		path := filepath.Join(personasDir, f.file)
		if err := writeFile(path, []byte(f.content)); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		recorded[f.file] = registry.HashPersonaContent([]byte(f.content))
	}
	if err := writePersonaStampMerged(personasDir, recorded); err != nil {
		return err
	}
	if len(todo) > 0 {
		_, _ = fmt.Fprintf(out, "Refreshed %d persona file(s); review the changes with git diff\n", len(todo))
	} else {
		_, _ = fmt.Fprintf(out, "Persona files are current; refreshed the stamp for %d file(s)\n", stampFixed)
	}
	return nil
}

// maxPersonaCopyBytes caps how much of an on-disk persona copy the refresh
// reads to hash it. A persona file is a few KB; .atcr/personas/ is tracked, so
// a cloned repo can carry a huge one, and reading it whole must not be possible.
const maxPersonaCopyBytes = 1 << 20

// copyState says whether a persona copy was read, and if not, why not.
type copyState int

const (
	copyRegular    copyState = iota // a regular file, read and hashed
	copyNonRegular                  // a symlink, FIFO, device or directory: not read
	copyOversized                   // a regular file over maxPersonaCopyBytes: not read whole
)

// hashPersonaCopy hashes the persona copy at path, which an earlier Lstat
// reported as checked. It opens path once and stats the held handle, so a copy
// swapped for a symlink, FIFO or other file after the check is never read
// through: the handle is not a regular file, or not the file that was checked,
// and the copy is reported copyNonRegular. The open is non-blocking, so a FIFO
// swapped in does not wait for a writer. At most maxPersonaCopyBytes+1 bytes are
// read; a file over the cap is reported copyOversized. Only a copyRegular copy
// has a hash.
func hashPersonaCopy(path string, checked fs.FileInfo) (string, copyState, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", copyRegular, err
	}
	defer func() { _ = f.Close() }()
	held, err := f.Stat()
	if err != nil {
		return "", copyRegular, err
	}
	if !held.Mode().IsRegular() || !os.SameFile(checked, held) {
		return "", copyNonRegular, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPersonaCopyBytes+1))
	if err != nil {
		return "", copyRegular, err
	}
	if len(data) > maxPersonaCopyBytes {
		return "", copyOversized, nil
	}
	return registry.HashPersonaContent(data), copyRegular, nil
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

	// Track every unit file this run wrote so a failure rolls back exactly those
	// files (all-or-nothing). A persona's paths are recorded only AFTER InstallUnit
	// succeeds, each with the os.Lstat identity it had then; rollback removes a
	// path only while its identity still matches. Another process may create or
	// replace a file at one of these paths before the rollback runs, and that file
	// is left alone. The failing persona's paths are never recorded: nothing proves
	// this run wrote them, and writePersonaUnit already removes its own partial
	// .yaml on failure.
	type writtenFile struct {
		path string
		info fs.FileInfo
	}
	var written []writtenFile
	rollback := func() {
		for _, w := range written {
			cur, err := os.Lstat(w.path)
			if err == nil && os.SameFile(cur, w.info) && cur.ModTime().Equal(w.info.ModTime()) {
				_ = os.Remove(w.path)
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
		if err := commpersonas.InstallUnit(client, baseURL, name, destDir); err != nil {
			rollback()
			return fmt.Errorf("failed to install community persona %q: %w%s", name, err, offlineHint)
		}
		// A binding-only persona has no .md, so a missing path is not recorded.
		for _, path := range []string{yamlPath, mdPath} {
			if info, err := os.Lstat(path); err == nil {
				written = append(written, writtenFile{path: path, info: info})
			}
		}
		_, _ = fmt.Fprintf(out, "Installed %s (community)\n", name)
	}
	return nil
}

// fileExists reports whether path is present. A non-ENOENT stat error is treated
// as "present" so the never-overwrite guard errs toward skipping the install
// rather than writing over a file it could not inspect.
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
	return runInitRoster(dir, force, builtins.Names(), builtins.Get, out, errOut)
}

// runInitRoster is runInit with the roster and its persona prompts supplied by
// the caller: roster is written to config.yaml and each name's prompt, read
// from personaText, is scaffolded under .atcr/personas. `atcr init` passes the
// embedded built-ins; `atcr quickstart --preset local` passes the local
// community personas, whose prompts are embedded too.
func runInitRoster(dir string, force bool, roster []string, personaText func(string) (string, error), out, errOut io.Writer) error {
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

	// recorded holds the stamp entries for the persona files this run writes;
	// they are merged into the stamp as it is on disk when it is written.
	recorded := map[string]string{}

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
			recorded[filepath.Base(path)] = registry.HashPersonaContent([]byte(content))
		}
		return nil
	}

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
		content, err := personaText(name)
		if err != nil {
			return err
		}
		if err := write(filepath.Join(personasDir, name+".md"), content, true); err != nil {
			return err
		}
	}
	if len(recorded) > 0 {
		if err := writePersonaStampMerged(personasDir, recorded); err != nil {
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

// writePersonaStampMerged writes the persona stamp with entries (file name to
// content hash) set. It re-reads the stamp just before the write and keeps every
// entry on disk that entries does not set, so a run that wrote the stamp while
// this one was writing its files keeps its entries. There is no lock: two writes
// that land in the same instant can still race, but the window is the read and
// rename of one small file, not a whole run. A stamp that cannot be read is
// rebuilt from entries alone, as a first write would be.
func writePersonaStampMerged(personasDir string, entries map[string]string) error {
	stamp, ok := registry.ReadPersonaStamp(personasDir)
	if !ok {
		stamp = registry.NewPersonaStamp()
	}
	for name, hash := range entries {
		stamp.Files[name] = hash
	}
	if err := registry.WritePersonaStamp(personasDir, stamp); err != nil {
		return fmt.Errorf("failed to write %s: %w", registry.PersonaStampPath(personasDir), err)
	}
	return nil
}

// refreshProbePrefix starts the name of the temp file the refresh creates to
// prove .atcr/personas is writable; os.CreateTemp ends it with random digits.
const refreshProbePrefix = ".refresh-probe-"

// probeWritable proves personasDir is writable by creating and removing a temp
// file in it. It first removes any probe file a run that died mid-probe left in
// the tracked directory (only names os.CreateTemp could have made: the prefix
// and then digits), and the removal of its own is deferred, so it leaves none
// behind on any return.
func probeWritable(personasDir string) error {
	leftovers, _ := filepath.Glob(filepath.Join(personasDir, refreshProbePrefix+"*"))
	for _, path := range leftovers {
		suffix := strings.TrimPrefix(filepath.Base(path), refreshProbePrefix)
		if suffix != "" && strings.Trim(suffix, "0123456789") == "" {
			_ = os.Remove(path)
		}
	}
	probe, err := os.CreateTemp(personasDir, refreshProbePrefix+"*")
	if err != nil {
		return fmt.Errorf("cannot refresh personas: %s is not writable: %w", personasDir, err)
	}
	defer func() { _ = os.Remove(probe.Name()) }()
	if err := probe.Close(); err != nil {
		return fmt.Errorf("cannot refresh personas: %w", err)
	}
	return nil
}
