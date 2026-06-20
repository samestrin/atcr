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
