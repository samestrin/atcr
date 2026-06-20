package atomicfs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteJSON_RoundTripsIndented(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	type rec struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	want := rec{Name: "alpha", Count: 3}

	if err := WriteJSON(path, want); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Indented output (two-space) so artifacts stay human-diffable, matching the
	// reconcile/verify renderers, and a trailing newline like the other writers.
	if !strings.Contains(string(data), "\n  \"name\": \"alpha\"") {
		t.Errorf("expected two-space indented JSON, got:\n%s", data)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("expected trailing newline, got:\n%q", data)
	}

	var got rec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

func TestWriteJSON_OverwritesAtomicallyNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	if err := os.WriteFile(path, []byte("{\"stale\":true}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteJSON(path, map[string]int{"fresh": 1}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "stale") {
		t.Errorf("overwrite did not replace prior content: %s", data)
	}

	// The atomic temp file (.<base>.tmp-*) must not survive a successful write.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".out.json.tmp-") {
			t.Errorf("temp file leaked after successful write: %s", e.Name())
		}
	}
}

func TestWriteFileAtomic_HappyPathWritesExactBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	want := []byte("exact-bytes\x00\n")

	if err := WriteFileAtomic(path, want); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("content mismatch: got %q want %q", got, want)
	}
}

func TestWriteFileAtomic_MissingParentDirErrors(t *testing.T) {
	dir := t.TempDir()
	// Parent directory does not exist, so CreateTemp fails and the error is
	// surfaced rather than swallowed; the target must not be created.
	path := filepath.Join(dir, "no-such-subdir", "data.json")
	if err := WriteFileAtomic(path, []byte("x")); err == nil {
		t.Fatal("expected error writing into a missing parent dir, got nil")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file at %s, stat err = %v", path, err)
	}
}

func TestWriteJSON_UnmarshalableValueErrorsAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	// A channel cannot be marshaled; WriteJSON must fail before touching path.
	if err := WriteJSON(path, make(chan int)); err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file written on marshal error, stat err = %v", err)
	}
}

// TestWriteJSON_FailedWritePreservesExistingFile is half of Epic 4.7 AC6: a
// write that fails (here, a marshal error) aborts before any temp is renamed over
// the target, so the existing file is never truncated or corrupted.
func TestWriteJSON_FailedWritePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verification.json")
	original := []byte("{\n  \"state\": \"original-intact\"\n}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteJSON(path, make(chan int)); err == nil {
		t.Fatal("expected marshal error, got nil")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("a failed write must leave the existing file byte-identical: got %q want %q", got, original)
	}
}

// TestWriteFileAtomic_OrphanedTempDoesNotCorruptTarget is the other half of AC6:
// a process killed mid-write (after CreateTemp + partial Write, before Rename)
// leaves an orphaned .<base>.tmp-* sibling. Readers address the exact target
// filename, so the target stays complete; a later atomic write replaces it
// wholesale and never merges the orphan's partial bytes.
func TestWriteFileAtomic_OrphanedTempDoesNotCorruptTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	original := []byte("complete-original\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate the SIGKILL-orphaned partial temp an interrupted write would leave.
	orphan := filepath.Join(dir, ".data.json.tmp-deadbeef")
	if err := os.WriteFile(orphan, []byte("half-written-garbag"), 0o644); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	// The target is untouched by the orphaned temp.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("orphaned temp must not affect the target: got %q", got)
	}

	// A subsequent atomic write replaces the target wholesale with complete data.
	if err := WriteFileAtomic(path, []byte("complete-new\n")); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after write: %v", err)
	}
	if string(got2) != "complete-new\n" {
		t.Errorf("atomic rename must replace the target wholesale: got %q", got2)
	}
	if strings.Contains(string(got2), "garbag") {
		t.Errorf("the orphan's partial bytes must never merge into the target: got %q", got2)
	}
}

func TestBackupToDotBak_File(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "verification.json")
	if err := os.WriteFile(src, []byte("current\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A stale .bak must be replaced, not error out.
	if err := os.WriteFile(src+".bak", []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	bak, err := BackupToDotBak(src)
	if err != nil {
		t.Fatalf("BackupToDotBak: %v", err)
	}
	if bak != src+".bak" {
		t.Errorf("backup path = %q, want %q", bak, src+".bak")
	}
	data, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("ReadFile backup: %v", err)
	}
	if string(data) != "current\n" {
		t.Errorf("backup content = %q, want the current generation", data)
	}
	// Source is preserved in place (copy, not move).
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source must remain in place after backup: %v", err)
	}
}

func TestBackupToDotBak_Directory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "reconciled")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "findings.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "deep.txt"), []byte("deep\n"), 0o600); err != nil {
		t.Fatalf("seed nested: %v", err)
	}
	// Stale backup with a file that must NOT survive the replace.
	if err := os.MkdirAll(src+".bak", 0o755); err != nil {
		t.Fatalf("mkdir stale: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src+".bak", "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	bak, err := BackupToDotBak(src)
	if err != nil {
		t.Fatalf("BackupToDotBak: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(bak, "findings.json"))
	if err != nil || string(got) != "[]\n" {
		t.Errorf("top-level file not copied: data=%q err=%v", got, err)
	}
	gotNested, err := os.ReadFile(filepath.Join(bak, "nested", "deep.txt"))
	if err != nil || string(gotNested) != "deep\n" {
		t.Errorf("nested file not copied: data=%q err=%v", gotNested, err)
	}
	if _, err := os.Stat(filepath.Join(bak, "old.txt")); !os.IsNotExist(err) {
		t.Errorf("stale backup content must be replaced, stat err = %v", err)
	}
	// Source tree preserved (copy semantics).
	if _, err := os.Stat(filepath.Join(src, "findings.json")); err != nil {
		t.Errorf("source must remain after backup: %v", err)
	}
}

func TestBackupToDotBak_DirectorySkipsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "reconciled")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "findings.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A symlink inside the tree must be skipped (not followed) by the backup.
	if err := os.Symlink(filepath.Join(src, "findings.json"), filepath.Join(src, "link.json")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	bak, err := BackupToDotBak(src)
	if err != nil {
		t.Fatalf("BackupToDotBak: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bak, "findings.json")); err != nil {
		t.Errorf("regular file must be copied: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(bak, "link.json")); !os.IsNotExist(err) {
		t.Errorf("symlink must be skipped, not copied; lstat err = %v", err)
	}
}

func TestBackupToDotBak_MissingSourceIsNoop(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "absent")

	bak, err := BackupToDotBak(src)
	if err != nil {
		t.Fatalf("expected no error for missing source, got %v", err)
	}
	if bak != "" {
		t.Errorf("expected empty backup path for missing source, got %q", bak)
	}
	if _, err := os.Stat(src + ".bak"); !os.IsNotExist(err) {
		t.Errorf("no backup may be created for a missing source, stat err = %v", err)
	}
}

func TestBackupToDotBak_SymlinkSourceIsSkipped(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, src); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	// A symlinked source must be skipped (not followed via its target), matching
	// the regular-file/directory-only contract; otherwise the target's bytes are
	// silently backed up under the link's name.
	bak, err := BackupToDotBak(src)
	if err != nil {
		t.Fatalf("BackupToDotBak: %v", err)
	}
	if bak != "" {
		t.Errorf("expected empty backup path for a symlink source, got %q", bak)
	}
	if _, err := os.Lstat(src + ".bak"); !os.IsNotExist(err) {
		t.Errorf("no backup may be created for a symlink source, lstat err = %v", err)
	}
}

// TestBackupToDotBak_FailedFileCopyPreservesOldBackup is a fault-injection test
// for crash-safe replacement: if the copy into the staging temp fails, the prior
// .bak generation must remain intact and no staging temp must be left behind.
func TestBackupToDotBak_FailedFileCopyPreservesOldBackup(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read unreadable files")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "verification.json")
	if err := os.WriteFile(src, []byte("current\n"), 0o000); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Pre-existing backup that must survive a failed copy.
	if err := os.WriteFile(src+".bak", []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed old backup: %v", err)
	}

	_, err := BackupToDotBak(src)
	if err == nil {
		t.Fatal("expected error copying unreadable source, got nil")
	}

	got, err := os.ReadFile(src + ".bak")
	if err != nil {
		t.Fatalf("old backup should still exist: %v", err)
	}
	if string(got) != "old\n" {
		t.Errorf("old backup was corrupted: %q", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.tmp-") {
			t.Errorf("staging temp leaked after failed backup: %s", e.Name())
		}
	}
}

// TestBackupToDotBak_FailedDirCopyPreservesOldBackup is the directory-tree
// counterpart of the file fault-injection test above.
func TestBackupToDotBak_FailedDirCopyPreservesOldBackup(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read unreadable files")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "reconciled")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "findings.json"), []byte("current\n"), 0o000); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Pre-existing backup tree that must survive a failed copy.
	oldBak := src + ".bak"
	if err := os.MkdirAll(oldBak, 0o755); err != nil {
		t.Fatalf("mkdir old bak: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldBak, "legacy.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("seed old backup: %v", err)
	}

	_, err := BackupToDotBak(src)
	if err == nil {
		t.Fatal("expected error copying unreadable nested file, got nil")
	}

	got, err := os.ReadFile(filepath.Join(oldBak, "legacy.txt"))
	if err != nil {
		t.Fatalf("old backup should still exist: %v", err)
	}
	if string(got) != "old\n" {
		t.Errorf("old backup was corrupted: %q", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.tmp-") {
			t.Errorf("staging temp leaked after failed backup: %s", e.Name())
		}
	}
}

// TestBackupToDotBak_NoStagingLeakAfterSuccess verifies that the staging temp
// sibling is renamed into place and never left behind after a successful backup.
func TestBackupToDotBak_NoStagingLeakAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "verification.json")
	if err := os.WriteFile(src, []byte("current\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(src+".bak", []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	if _, err := BackupToDotBak(src); err != nil {
		t.Fatalf("BackupToDotBak: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.tmp-") {
			t.Errorf("staging temp leaked after successful backup: %s", e.Name())
		}
	}
}

// TestBackupToDotBak_RenameFailurePreservesPriorBak is the Epic 4.7.1 AC4
// rename-step fault test for the copy-based site: the copy succeeds but the swap
// (staged->bak rename) fails, and the prior .bak must survive intact with no
// staging artifacts left behind. The failure is injected through the renameFn
// seam so the otherwise-microsecond same-dir rename window is deterministic.
func TestBackupToDotBak_RenameFailurePreservesPriorBak(t *testing.T) {
	for _, tc := range []struct {
		name  string
		isDir bool
	}{
		{"directory", true},
		{"file", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "review")
			bak := src + ".bak"

			if tc.isDir {
				if err := os.MkdirAll(src, 0o755); err != nil {
					t.Fatalf("seed src dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new\n"), 0o644); err != nil {
					t.Fatalf("seed src content: %v", err)
				}
				if err := os.MkdirAll(bak, 0o755); err != nil {
					t.Fatalf("seed prior .bak dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(bak, "prior.txt"), []byte("prior\n"), 0o644); err != nil {
					t.Fatalf("seed prior .bak content: %v", err)
				}
			} else {
				if err := os.WriteFile(src, []byte("new\n"), 0o644); err != nil {
					t.Fatalf("seed src file: %v", err)
				}
				if err := os.WriteFile(bak, []byte("prior\n"), 0o644); err != nil {
					t.Fatalf("seed prior .bak file: %v", err)
				}
			}

			orig := renameFn
			renameFn = func(_, _ string) error { return os.ErrPermission }
			defer func() { renameFn = orig }()

			if _, err := BackupToDotBak(src); err == nil {
				t.Fatal("expected error on injected swap-rename failure, got nil")
			}

			// AC4: the prior .bak must survive the failed swap.
			priorPath := bak
			if tc.isDir {
				priorPath = filepath.Join(bak, "prior.txt")
			}
			got, err := os.ReadFile(priorPath)
			if err != nil {
				t.Fatalf("prior .bak did not survive failed swap: %v", err)
			}
			if string(got) != "prior\n" {
				t.Errorf("prior .bak corrupted after failed swap: %q", got)
			}

			// No atcr-owned staging artifact may leak.
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("ReadDir: %v", err)
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), ".bak.tmp-") {
					t.Errorf("staging temp leaked after failed swap: %s", e.Name())
				}
				if strings.HasSuffix(e.Name(), ".bak.old") {
					t.Errorf(".bak.old straggler leaked after failed swap: %s", e.Name())
				}
			}
		})
	}
}

// TestCopyPath_File verifies CopyPath copies a regular file's bytes and perms to
// a fresh destination.
func TestCopyPath_File(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o640); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(dir, "dst.txt")
	if err := CopyPath(src, dst); err != nil {
		t.Fatalf("CopyPath: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("content = %q, want payload", data)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat dst: %v", err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("perm = %v, want 0640", fi.Mode().Perm())
	}
}

// TestCopyPath_Directory verifies CopyPath recreates a directory tree at a fresh
// destination, preserving nested content.
func TestCopyPath_Directory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	dst := filepath.Join(dir, "dst")
	if err := CopyPath(src, dst); err != nil {
		t.Fatalf("CopyPath: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "sub", "f.txt"))
	if err != nil {
		t.Fatalf("read nested: %v", err)
	}
	if string(data) != "nested" {
		t.Errorf("nested content = %q, want nested", data)
	}
}

// TestCopyPath_MissingSourceErrors verifies CopyPath surfaces the Lstat error for
// an absent source rather than silently succeeding.
func TestCopyPath_MissingSourceErrors(t *testing.T) {
	dir := t.TempDir()
	if err := CopyPath(filepath.Join(dir, "nope"), filepath.Join(dir, "dst")); err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}

// TestCopyPath_NonRegularSourceErrors verifies CopyPath refuses a non-regular,
// non-directory source (e.g. a symlink) instead of dereferencing it.
func TestCopyPath_NonRegularSourceErrors(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	err := CopyPath(link, filepath.Join(dir, "dst"))
	if err == nil {
		t.Fatal("expected error for symlink source, got nil")
	}
	if !strings.Contains(err.Error(), "not a regular file or directory") {
		t.Errorf("error = %v, want 'not a regular file or directory'", err)
	}
}
