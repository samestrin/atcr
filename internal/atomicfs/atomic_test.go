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
