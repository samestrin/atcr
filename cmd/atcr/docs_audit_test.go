package main

// Documentation-audit regression guards (epic 15.0). These tests treat the
// compiled command tree and the real config schema as the source of truth and
// assert that the prose in docs/ (plus the root README.md) never drifts away
// from it. They exist so a future rename/removal that forgets the docs fails CI
// instead of shipping stale instructions to the website build that consumes
// docs/ as its single source of truth.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// repoRootDir ascends from the test working directory (the package dir,
// cmd/atcr) until it finds go.mod, returning the repository root.
func repoRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// auditedMarkdown returns the set of markdown files this epic owns as the
// documentation source of truth: every docs/*.md plus the root README.md.
// testdata fixtures and READMEs elsewhere in the tree are intentionally out of
// scope (epic 15.0 clarifications).
func auditedMarkdown(t *testing.T) map[string]string {
	t.Helper()
	root := repoRootDir(t)
	out := map[string]string{}
	docs, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	paths := append(docs, filepath.Join(root, "README.md"))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(b)
	}
	return out
}

// docCommandTokens extracts subcommand tokens from command invocations that
// actually begin with `atcr ` — inline code spans that start with atcr, and
// fenced-block lines that start with atcr (optionally behind a `$ ` shell
// prompt). Anchoring to the start of a span/line keeps ordinary prose (e.g. the
// sentence "atcr is local-first") and cross-line adjacencies from registering as
// bogus command references.
func docCommandTokens(md string) []string {
	var toks []string
	reCmd := regexp.MustCompile(`^atcr ([a-z][a-z-]+)`)
	capture := func(s string) {
		if m := reCmd.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
			toks = append(toks, m[1])
		}
	}
	for _, m := range regexp.MustCompile("`([^`\n]+)`").FindAllStringSubmatch(md, -1) {
		capture(m[1])
	}
	inFence := false
	for _, ln := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			continue
		}
		capture(strings.TrimPrefix(strings.TrimSpace(ln), "$ "))
	}
	return toks
}

// canonicalCommands walks the whole command tree and returns every command name
// (top-level and nested), plus cobra's auto-registered help/completion.
func canonicalCommands() map[string]bool {
	names := map[string]bool{"help": true, "completion": true}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			names[sub.Name()] = true
			walk(sub)
		}
	}
	walk(newRootCmd())
	return names
}

// canonicalFlags walks the whole command tree and returns every long flag name
// registered on any command (local + persistent).
func canonicalFlags() map[string]bool {
	flags := map[string]bool{}
	add := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) { flags[f.Name] = true })
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		add(c.LocalFlags())
		add(c.PersistentFlags())
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
	return flags
}

// TestDocsReferenceOnlyRealCommands asserts that every `atcr <subcommand>` token
// appearing inside a code span or fenced block in the audited docs names a real
// command in the compiled tree (AC1).
func TestDocsReferenceOnlyRealCommands(t *testing.T) {
	cmds := canonicalCommands()
	for path, content := range auditedMarkdown(t) {
		for _, tok := range docCommandTokens(content) {
			if !cmds[tok] {
				t.Errorf("%s references `atcr %s` but %q is not a real command", path, tok, tok)
			}
		}
	}
}

// TestDocsClaimedFlagsAreReal asserts that every flag the docs explicitly call a
// "flag" via the “ `--x` flag “ idiom is a real flag on some command in the
// compiled tree (AC1). This catches prose that documents a CLI flag which does
// not exist, without matching docker/git flags used in unrelated examples.
func TestDocsClaimedFlagsAreReal(t *testing.T) {
	flags := canonicalFlags()
	re := regexp.MustCompile("`--([a-z][a-z-]+)`\\s+flag")
	for path, content := range auditedMarkdown(t) {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			name := m[1]
			if !flags[name] {
				t.Errorf("%s documents `--%s` as a flag but no such CLI flag exists", path, name)
			}
		}
	}
}
