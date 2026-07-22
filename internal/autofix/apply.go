// Package autofix applies model-generated unified diffs to the working tree and
// reverts them, wrapping go-gitdiff (hunk matching) and internal/atomicfs
// (crash-safe writes/backups) behind a small, library-agnostic surface. It is
// the local write-path the opt-in --auto-fix flow builds on: apply → validate →
// revert-or-continue → branch/commit/PR. No caller of this package imports
// go-gitdiff directly, so the patch-apply engine stays swappable.
package autofix

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/security"
)

// BackupMap records, for each successfully-applied entry, the mapping from the
// absolute target path to the absolute backup path atomicfs.BackupToDotBak
// produced for it. A non-empty value points at a .bak holding the pre-patch bytes
// (a modify or delete of an existing regular file). An empty value means the
// pre-patch state was "absent" — a file created by this run, or a delete whose
// target was already gone — so Story 3's Revert routes it to removal rather than a
// copy-back. This sentinel is kept unambiguous by refuseSymlinkLeaf, which rejects
// an in-tree symlink leaf target at the write boundary (atomicfs.BackupToDotBak
// would otherwise Lstat-skip it and also yield an empty value; see TD-005).
// Absolute paths are stored so Revert is self-contained and needs no working-tree
// root of its own.
type BackupMap map[string]string

// ReviewFlag records, for a single successfully-applied entry, that the patch did
// something a human reviewer should notice before approving the generated PR — an
// executable-bit change or a build-script touch (security.FlagsForReview). It is
// purely advisory: unlike the protected-path gate it never blocks an apply, it only
// annotates the PR body. Path is the repo-relative entry path; Reason names why it
// was flagged. Only successfully-applied entries are recorded (a flagged-but-failed
// entry never appears — see ApplyPatch), so a warning never names a file that was
// reverted.
type ReviewFlag struct {
	Path   string
	Reason string
}

// removeFn is the file-removal primitive ApplyPatch uses for deletion entries,
// indirected through a package var so a test can drive the removal-failure branch
// (AC 01-01 Error Scenario 5) deterministically. In production it is os.Remove.
var removeFn = os.Remove

// writeFileAtomicFn is the atomic-write primitive ApplyPatch uses for modify/create
// entries, indirected through a package var so a test can drive the write-failure
// branch (TD-006) deterministically. In production it is atomicfs.WriteFileAtomic.
var writeFileAtomicFn = atomicfs.WriteFileAtomic

// ApplyPatch applies each entry's per-file unified-diff Body to its target path
// under root, using go-gitdiff for hunk matching and atomicfs for crash-safe
// backup-then-atomic-write. Entries are processed independently: one entry's
// failure never rolls back or corrupts an entry that already succeeded, and the
// batch does not short-circuit on the first error. Every touched existing file
// is backed up (atomicfs.BackupToDotBak) before it is overwritten or removed, so
// a later Revert can restore file-by-file.
//
// The returned BackupMap holds one entry per SUCCESSFULLY-applied file (keyed by
// absolute target path); files that failed are reported only through the returned
// error, so the caller can tell exactly which files landed. The error, when
// non-nil, aggregates every per-file failure (via errors.Join), not just the
// first — callers map it to a non-zero exit code.
//
// The returned []ReviewFlag is a non-blocking advisory list: one entry per
// SUCCESSFULLY-applied file whose patch flipped an executable bit or touched a
// build-script path (security.FlagsForReview). It never affects success/failure —
// it is collected per-entry and merged into the result only when that entry
// applied cleanly, so a flagged-but-failed (and reverted) entry is never reported.
// The caller (cmd/atcr auto-fix) surfaces it as a "## Review Warnings" PR-body
// section; an empty slice leaves the PR body unchanged.
//
// root is the working-tree root every FileEntry.Path is resolved against. As
// defense-in-depth (the primary traversal guard is payload.BuildEntriesFromDiff's
// isSafeDiffContentPath upstream), ApplyPatch re-checks at the write boundary that
// each cleaned path stays inside root and refuses any entry that escapes it.
//
// allowConfigEdits controls the workspace-integrity gate: by default (false) any
// entry whose path targets a protected host-execution or configuration artifact
// (security.IsProtectedPath — .git/, .githooks/, .github/workflows/, .vscode/,
// .idea/, .env*, .planning/, .atcr, CI defs) is refused with a security error
// wrapping security.ErrProtectedPath, closing the Host Trust Transposition gap
// where an LLM-generated patch writes a trigger a host-side tool later executes.
// Passing true bypasses that gate (the --allow-config-edits operator escape valve);
// it does not relax the traversal/symlink guards, which always apply. The refusal
// is per-entry like every other failure, so a protected entry never blocks a clean
// sibling in the same batch.
func ApplyPatch(root string, entries []payload.FileEntry, allowConfigEdits bool) (BackupMap, []ReviewFlag, error) {
	bm := make(BackupMap)
	var flags []ReviewFlag
	var errs []error
	for i := range entries {
		// Collect this entry's advisory flags into a per-entry buffer so they are
		// merged into the returned slice ONLY when the entry applies cleanly — a
		// flagged-but-failed entry (reverted below) must not surface in the PR body.
		var entryFlags []ReviewFlag
		abs, bak, err := applyOne(root, entries[i], allowConfigEdits, &entryFlags)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		bm[abs] = bak
		flags = append(flags, entryFlags...)
	}
	if len(errs) > 0 {
		return bm, flags, fmt.Errorf("autofix: %d of %d entries failed: %w",
			len(errs), len(entries), errors.Join(errs...))
	}
	return bm, flags, nil
}

// applyOne applies a single entry and returns the absolute target path plus the
// backup path recorded for it on success. Every error is wrapped with the entry's
// Path so the aggregated batch error names each failing file. No disk write
// happens until parse+apply have succeeded, so a parse/apply failure leaves no
// backup and no partial write behind.
func applyOne(root string, e payload.FileEntry, allowConfigEdits bool, flags *[]ReviewFlag) (absTarget, backupPath string, err error) {
	abs, err := containedPath(root, e.Path)
	if err != nil {
		return "", "", err
	}
	// Workspace-integrity gate (fail-closed): refuse a patch whose path — though
	// contained within root — targets a protected host-execution/config artifact
	// that a host-side tool would later execute (Host Trust Transposition). Placed
	// immediately after containment and before refuseSymlinkLeaf/parse/backup/write
	// so the refusal fires before any filesystem effect. Checks e.Path (the repo-
	// relative, diff-declared path) because security.IsProtectedPath matches on
	// repo-relative segments; containedPath has already validated it against root.
	if !allowConfigEdits && security.IsProtectedPath(e.Path, root) {
		return "", "", fmt.Errorf("autofix: refusing to write %q: path is protected by workspace-integrity policy (pass --allow-config-edits to override): %w", e.Path, security.ErrProtectedPath)
	}
	if serr := refuseSymlinkLeaf(abs, e.Path); serr != nil {
		return "", "", serr
	}

	files, _, perr := gitdiff.Parse(strings.NewReader(e.Body))
	if perr != nil {
		return "", "", fmt.Errorf("autofix: parsing diff for %q: %w", e.Path, perr)
	}
	if len(files) != 1 {
		return "", "", fmt.Errorf("autofix: parsing diff for %q: expected exactly one file section, got %d", e.Path, len(files))
	}
	f := files[0]

	// Non-blocking advisory flags (epic 32.4 Task 6): record a build-script touch so
	// runAutoFix can surface a "## Review Warnings" PR-body section. This never blocks
	// the apply — FlagsForReview only reports. It evaluates e.Path only (no file mode):
	// the apply pipeline writes every file through atomicfs.WriteFileAtomic (fixed 0644)
	// and the GitHub commit hardcodes blob mode 100644, so a diff's exec-bit change
	// never lands and flagging it would warn about a change that does not happen. The
	// append lands in the per-entry buffer ApplyPatch only keeps when this entry
	// ultimately applies cleanly.
	if flagged, reason := security.FlagsForReview(e.Path); flagged {
		*flags = append(*flags, ReviewFlag{Path: e.Path, Reason: reason})
	}

	// Deletion (+++ /dev/null): back up then remove. Branched on the delete
	// marker, never inferred from an empty apply result, so it routes to file
	// removal rather than a zero-byte atomic write.
	if f.IsDelete {
		bak, berr := atomicfs.BackupToDotBak(abs)
		if berr != nil {
			return "", "", fmt.Errorf("autofix: backing up %q before apply: %w", e.Path, berr)
		}
		if rerr := removeFn(abs); rerr != nil {
			if errors.Is(rerr, os.ErrNotExist) {
				return abs, bak, nil // already gone; deletion is idempotent
			}
			// The delete failed but the backup was staged; clean it up so it is not
			// left untracked (TD-006).
			if bak != "" {
				_ = removeFn(bak)
			}
			return "", "", fmt.Errorf("autofix: removing %q: %w", e.Path, rerr)
		}
		return abs, bak, nil
	}

	// Modify / create: read current content (empty for a new file).
	var src []byte
	if !f.IsNew {
		src, err = os.ReadFile(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", "", fmt.Errorf("autofix: target %q does not exist but diff expects a modification (old side is not /dev/null)", e.Path)
			}
			return "", "", fmt.Errorf("autofix: reading %q: %w", e.Path, err)
		}
	}

	// A create diff (old side /dev/null) must not clobber an existing file.
	// This mirrors git apply's refusal and keeps the create-vs-modify routing
	// unambiguous (TD-004).
	if f.IsNew {
		if _, err := os.Stat(abs); err == nil {
			return "", "", fmt.Errorf("autofix: refusing to create %q: target already exists", e.Path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("autofix: checking %q before create: %w", e.Path, err)
		}
	}

	var out bytes.Buffer
	if aerr := gitdiff.Apply(&out, bytes.NewReader(src), f); aerr != nil {
		// Any non-nil apply error is a hard per-file failure — no partial-confidence
		// apply. See the retained gitdiff_contract_test.go drift invariant.
		return "", "", fmt.Errorf("autofix: applying patch to %q: %w", e.Path, aerr)
	}

	// Back up the pre-patch file before overwriting. A missing source (new file)
	// or a symlink is a documented no-op returning ("", nil).
	bak, berr := atomicfs.BackupToDotBak(abs)
	if berr != nil {
		return "", "", fmt.Errorf("autofix: backing up %q before apply: %w", e.Path, berr)
	}

	if werr := writeFileAtomicFn(abs, out.Bytes()); werr != nil {
		// The write failed but the backup was staged; clean it up so it is not
		// left untracked (TD-006).
		if bak != "" {
			_ = removeFn(bak)
		}
		return "", "", fmt.Errorf("autofix: writing %q: %w", e.Path, werr)
	}
	return abs, bak, nil
}

// containedPath joins p against root and confirms the result stays inside root
// both lexically AND after symlink resolution, returning the absolute target.
// This is a belt-and-suspenders re-check at the write boundary, not a replacement
// for the upstream payload traversal guard.
//
// The symlink-resolution pass mirrors payload's rejectDiffSymlinkEscape: a purely
// lexical check is defeated by a symlinked directory component inside root that
// points elsewhere (e.g. root/link -> /etc, entry path "link/passwd"), because
// os.ReadFile and atomicfs.WriteFileAtomic follow that symlink and would create
// their temp+rename in the link's real target. Resolving the parent directory
// (which must already exist — this package never mkdirs) against the resolved
// root closes that escape. A parent that does not resolve (a genuinely new
// subdirectory) is left to fail naturally at the write, since nothing can be
// written into a non-existent directory.
func containedPath(root, p string) (string, error) {
	abs := filepath.Join(root, p) // Join cleans the joined path
	if !contains(root, abs) {
		return "", escapeErr(p)
	}
	realRoot := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = r
	}
	if realParent, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		if !contains(realRoot, filepath.Join(realParent, filepath.Base(abs))) {
			return "", escapeErr(p)
		}
	}
	return abs, nil
}

// refuseSymlinkLeaf rejects an entry whose target IS an existing symlink. Backing
// up a symlink leaf is a documented no-op in atomicfs.BackupToDotBak (it Lstat-
// skips symlinks and returns ("", nil)), so applying a modify/delete through one
// would replace or unlink the link while recording an empty BackupMap value —
// indistinguishable from a freshly-created file, which Revert deletes rather than
// restores. Refusing it here keeps the empty-backup sentinel unambiguous
// ("pre-patch state was absent") so Revert's created-vs-restore routing is sound
// (TD-005). A non-symlink or absent target (Lstat error) passes; the symlinked
// directory *component* case is handled separately by containedPath.
func refuseSymlinkLeaf(abs, p string) error {
	info, lerr := os.Lstat(abs)
	if lerr != nil {
		return nil // absent (create) or unstattable — nothing to guard here
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("autofix: refusing to patch %q: target is a symlink", p)
	}
	return nil
}

// contains reports whether target is root itself or lies within it, by lexical
// relative-path inspection (both inputs are expected already absolute/cleaned).
func contains(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

func escapeErr(p string) error {
	return fmt.Errorf("autofix: refusing to write %q: path escapes working-tree root", p)
}
