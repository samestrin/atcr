package atomicfs

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests exercise the defensive error-return branches of the atomic write
// and copy helpers using real filesystem fault conditions (a destination that
// is a directory, a missing parent dir, a parent that is a regular file, an
// unreadable source subtree) rather than injecting a fake filesystem. They lift
// per-package coverage of the error paths without adding any production seam.
// Mid-stream temp-file failures (Write/Chmod/Close on an already-open *os.File)
// are deliberately left untested: they only occur on disk-full/quota/hardware
// faults that cannot be provoked deterministically without a fake fs, and the
// branches are simple `return err` passthroughs.

// TestWriteFileAtomic_RenameOverExistingDirErrors covers the final os.Rename
// failure: the temp file is created, written, chmod'd, and closed cleanly, but
// renaming a regular file over an existing directory fails, and WriteFileAtomic
// surfaces that error instead of swallowing it.
func TestWriteFileAtomic_RenameOverExistingDirErrors(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "iam-a-dir")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := WriteFileAtomic(target, []byte("payload")); err == nil {
		t.Fatal("expected error renaming temp file over an existing directory, got nil")
	}
}

// TestCopyFile_DestinationOpenErrorPropagates covers copyFile's dst OpenFile
// error return: a destination under a non-existent parent directory cannot be
// created even with O_CREATE, so the open fails before any bytes are copied.
func TestCopyFile_DestinationOpenErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(dir, "missing-subdir", "dst.txt")
	if err := copyFile(src, dst, 0o644); err == nil {
		t.Fatal("expected error opening dst under a missing parent dir, got nil")
	}
}

// TestCopyTree_MkdirAllErrorPropagates covers copyTree's os.MkdirAll error: when
// the destination root cannot be created because its parent is a regular file,
// the first WalkDir callback (the src root directory) fails.
func TestCopyTree_MkdirAllErrorPropagates(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	blocker := filepath.Join(base, "blocker") // a regular file masquerading as a dir parent
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if err := copyTree(src, filepath.Join(blocker, "dst")); err == nil {
		t.Fatal("expected MkdirAll error when dst parent is a file, got nil")
	}
}

// TestCopyTree_WalkDirErrorPropagates covers copyTree's WalkDir error branch: an
// unreadable sub-directory in the source surfaces a permission error from the
// walk, which copyTree returns rather than swallowing.
func TestCopyTree_WalkDirErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root traverses unreadable directories")
	}
	base := t.TempDir()
	src := filepath.Join(base, "src")
	locked := filepath.Join(src, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if err := copyTree(src, filepath.Join(base, "dst")); err == nil {
		t.Fatal("expected walk error on unreadable subdir, got nil")
	}
}
