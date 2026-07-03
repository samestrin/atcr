package autofix

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/samestrin/atcr/internal/atomicfs"
	"github.com/samestrin/atcr/internal/log"
)

// copyPathFn is the restore primitive RevertPatch uses to copy a .bak back over
// its original path, indirected through a package var so tests can drive the
// restore-failure branch (AC 03-02 / 03-04) deterministically. In production it is
// atomicfs.CopyPath. (removeFn — the file-removal seam RevertPatch and
// CleanupBackups share — is declared in apply.go.)
var copyPathFn = atomicfs.CopyPath

// RevertPatch restores every file recorded in bm to its exact pre-patch state
// after local validation fails, so a rejected auto-fix never leaves the working
// tree patched-but-broken. It is the synchronous safety gate the --auto-fix
// orchestrator must clear before any GitHub-mutating call is reachable.
//
// Each entry is routed by its BackupMap value:
//   - non-empty backup path -> the pre-patch bytes live in the .bak, so
//     atomicfs.CopyPath(backupPath, originalPath) copies them back over the
//     patched (or removed) file, then the now-redundant .bak is removed;
//   - empty backup path -> the pre-patch state was "absent" (a file created by
//     this run, or an already-gone delete; a symlink leaf is refused at apply
//     time so it never reaches here — see refuseSymlinkLeaf, TD-005), so the file
//     is removed to leave the tree as if the patch never applied. An
//     already-absent file is tolerated.
//
// Every entry is attempted even when an earlier one fails — a partial revert must
// still restore every file it can, so failure is localized to the smallest set.
// Each failure is both logged at Warn (operational visibility) AND collected; a
// non-empty return is an errors.Join aggregate naming every diverged file and its
// backup path, which the orchestrator maps to a non-zero exit. A nil return means
// the tree is fully restored.
//
// Precondition (TD-010): bm MUST be an ApplyPatch-produced BackupMap. ApplyPatch
// containment-validates every path on the write side (containedPath), so this
// function trusts each target/backup path and does NOT re-check containment. Do
// not hand it a hand-built or externally mutated map: a target or backup path
// outside the working-tree root would be copied or removed without a boundary
// check. The defense-in-depth re-check is intentionally omitted because the map is
// never externally sourced in the --auto-fix flow.
func RevertPatch(ctx context.Context, bm BackupMap) error {
	var errs []error
	for target, bak := range bm {
		if bak == "" {
			// Created by this run (or already-gone delete): pre-patch state was
			// absent, so ensure it is absent again.
			if err := removeFn(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.FromContext(ctx).Warn("autofix revert: could not remove patch-created file",
					"file", target, "err", err)
				errs = append(errs, fmt.Errorf("failed to remove created file %s: %w", target, err))
			}
			continue
		}
		if err := copyPathFn(bak, target); err != nil {
			// Leave the .bak in place: it holds the only surviving pre-patch copy.
			log.FromContext(ctx).Warn("autofix revert: could not restore file from backup",
				"file", target, "backup", bak, "err", err,
				"recovery", "pre-patch content is preserved at backup; restore it manually")
			errs = append(errs, fmt.Errorf("failed to restore %s from backup %s: %w", target, bak, err))
			continue
		}
		// Restore landed — drop the redundant backup (best-effort; a stranded .bak
		// is inert and does not fail the revert).
		if err := removeFn(bak); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.FromContext(ctx).Warn("autofix revert: restored file but could not remove its backup",
				"backup", bak, "err", err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("revert failed for %d file(s): %w", len(errs), errors.Join(errs...))
	}
	return nil
}

// CleanupBackups removes every .bak recorded in bm after local validation
// SUCCEEDS, so backups do not accumulate across repeated --auto-fix runs. It is
// deliberately best-effort and never fails the run: the live files are already
// correct and validated, so a stranded backup is a cosmetic artifact, not a
// correctness risk. An already-absent .bak is tolerated (fs.ErrNotExist); any
// other removal failure is logged at Warn and skipped. Entries with an empty
// backup path (patch-created files) have no .bak and are left untouched — deleting
// a live validated file is Revert's job on the failure path, never cleanup's.
func CleanupBackups(ctx context.Context, bm BackupMap) {
	for _, bak := range bm {
		if bak == "" {
			continue
		}
		if err := removeFn(bak); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.FromContext(ctx).Warn("autofix cleanup: could not remove backup",
				"backup", bak, "err", err,
				"note", "the live file is already validated; only the backup artifact is stranded")
		}
	}
}
