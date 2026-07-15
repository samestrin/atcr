package payload

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/log"
)

// writeIgnore writes name with content into dir, failing the test on error.
func writeIgnore(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestNewIgnoreMatcher_NoFiles(t *testing.T) {
	m := newIgnoreMatcher(t.TempDir(), log.Discard())
	if m.active() {
		t.Fatal("expected inactive matcher when no ignore files exist")
	}
	if m.match("anything.go") {
		t.Fatal("inactive matcher must not match")
	}
}

func TestIgnoreMatcher_Gitignore(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, ".gitignore", "vendor/\n*.log\n")
	m := newIgnoreMatcher(dir, log.Discard())

	if !m.active() {
		t.Fatal("expected active matcher")
	}
	for _, p := range []string{"vendor/pkg/x.go", "app.log"} {
		if !m.match(p) {
			t.Errorf("expected %q to be ignored by .gitignore", p)
		}
	}
	if m.match("cmd/main.go") {
		t.Error("cmd/main.go must not be ignored")
	}
}

// Gitignore negation ("!") is honored within the .gitignore source itself.
func TestIgnoreMatcher_GitignoreNegation(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, ".gitignore", "*.log\n!keep.log\n")
	m := newIgnoreMatcher(dir, log.Discard())

	if !m.match("a.log") {
		t.Error("a.log should be ignored")
	}
	if m.match("keep.log") {
		t.Error("keep.log should be re-included by .gitignore negation")
	}
}

// .atcrignore is additive to .gitignore's exclusions.
func TestIgnoreMatcher_AtcrignoreAdditive(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, ".gitignore", "vendor/\n")
	writeIgnore(t, dir, ".atcrignore", "go.sum\ndocs/\n")
	m := newIgnoreMatcher(dir, log.Discard())

	for _, p := range []string{"vendor/x.go", "go.sum", "docs/readme.md"} {
		if !m.match(p) {
			t.Errorf("expected %q to be ignored", p)
		}
	}
	if m.match("internal/payload/ignore.go") {
		t.Error("source file must not be ignored")
	}
}

// A "!" negation line in .atcrignore is dropped: it must NOT re-include a file,
// so the additive exclusion on the same pattern still applies.
func TestIgnoreMatcher_AtcrignoreNoNegation(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, ".atcrignore", "build.txt\n!build.txt\n")
	m := newIgnoreMatcher(dir, log.Discard())

	if !m.match("build.txt") {
		t.Error("build.txt must stay ignored — .atcrignore negation is not supported")
	}
}

// .atcrignore alone (no .gitignore) still activates the matcher.
func TestIgnoreMatcher_AtcrignoreOnly(t *testing.T) {
	dir := t.TempDir()
	writeIgnore(t, dir, ".atcrignore", "package-lock.json\n")
	m := newIgnoreMatcher(dir, log.Discard())

	if !m.active() {
		t.Fatal("expected active matcher from .atcrignore alone")
	}
	if !m.match("package-lock.json") {
		t.Error("package-lock.json should be ignored")
	}
}
