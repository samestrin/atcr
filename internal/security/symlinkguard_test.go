package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefuseSymlinkUnder(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := RefuseSymlinkUnder(root, ".atcr/reviews"); err != nil {
		t.Fatalf("absent path: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".atcr", "reviews"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RefuseSymlinkUnder(root, ".atcr/reviews"); err != nil {
		t.Fatalf("real dirs: %v", err)
	}

	// A symlinked leaf.
	if err := os.Remove(filepath.Join(root, ".atcr", "reviews")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".atcr", "reviews")); err != nil {
		t.Fatal(err)
	}
	err := RefuseSymlinkUnder(root, ".atcr/reviews")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked .atcr/reviews: err = %v, want a symlink refusal", err)
	}

	// A symlinked parent, including a dangling one.
	root2 := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root2, ".atcr")); err != nil {
		t.Fatal(err)
	}
	if err := RefuseSymlinkUnder(root2, ".atcr/reviews"); err == nil {
		t.Fatal("dangling symlinked .atcr must be refused")
	}

	// A symlink in root itself is the caller's choice, not refused.
	link := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := RefuseSymlinkUnder(link, ".atcr"); err != nil {
		t.Fatalf("symlinked root: %v", err)
	}
}
