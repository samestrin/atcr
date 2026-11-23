// Command changelog assembles the fragments in changelog.d/ into CHANGELOG.md.
//
//	go run ./cmd/changelog assemble --version X.Y.Z --date YYYY-MM-DD [--dry-run]
//
// Each fragment is a top-level file changelog.d/<category>-<slug>.md holding
// one or more "- " bullets, where <category> is added, changed, deprecated,
// removed, fixed or security. assemble inserts a "## [X.Y.Z] - date" section
// after the "## [Unreleased]" section, grouped by category in Keep a Changelog
// order, then deletes the fragments it assembled. README.md and dotfiles are
// ignored; directories and symlinks are skipped, never followed. CHANGELOG.md
// is written to a temporary file and renamed into place before any fragment is
// deleted, so a crash leaves the fragments where they were. It refuses, writing
// nothing, when a complete fragment bullet (with its indented continuation
// lines) is already in CHANGELOG.md, as a fragment left by a failed delete is.
// --dry-run prints the section and writes nothing.
//
//	go run ./cmd/changelog check --base <commit> --head <commit>
//
// check is the CI gate for a pull request: it fails a PR that changes a
// non-test file under cli/, internal/, cmd/, reconcile/ or personas/ without
// adding a fragment (unless the "no-changelog" label or a "changelog: none"
// line in the PR body opts out), and a PR that edits the Unreleased section of
// CHANGELOG.md directly. The one Unreleased edit it allows is a move: the PR
// only removes "- " bullets (with any "### " headings left empty and blank
// lines), and each removed bullet appears verbatim in a fragment the same PR
// adds. The PR body and labels are read from the
// CHANGELOG_PR_BODY and CHANGELOG_PR_LABELS (JSON array) environment variables.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// categories is the Keep a Changelog order; the fragment prefix is the
// lowercase name and the section heading the capitalised one.
var categories = []string{"added", "changed", "deprecated", "removed", "fixed", "security"}

var versionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

const unreleasedHeading = "## [Unreleased]"

// removeFragment deletes an assembled fragment; a test swaps it to fail.
var removeFragment = os.Remove

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "check" {
		return runCheck(args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "assemble" {
		_, _ = fmt.Fprintln(stderr, "usage: changelog check --base <commit> --head <commit> | changelog assemble --version X.Y.Z --date YYYY-MM-DD [--dry-run] [--dir changelog.d] [--changelog CHANGELOG.md]")
		return 2
	}
	fs := flag.NewFlagSet("changelog assemble", flag.ContinueOnError)
	fs.SetOutput(stderr)
	version := fs.String("version", "", "release version, X.Y.Z (required)")
	date := fs.String("date", "", "release date, YYYY-MM-DD (required)")
	dryRun := fs.Bool("dry-run", false, "print the section; write nothing and delete nothing")
	dir := fs.String("dir", "changelog.d", "fragment directory")
	changelog := fs.String("changelog", "CHANGELOG.md", "changelog file to update")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *version == "" || *date == "" || fs.NArg() > 0 {
		_, _ = fmt.Fprintln(stderr, "usage: changelog assemble --version X.Y.Z --date YYYY-MM-DD [--dry-run]")
		return 2
	}
	if !versionRE.MatchString(*version) {
		_, _ = fmt.Fprintf(stderr, "changelog: invalid version %q: want X.Y.Z\n", *version)
		return 2
	}
	if _, err := time.Parse("2006-01-02", *date); err != nil {
		_, _ = fmt.Fprintf(stderr, "changelog: invalid date %q: want YYYY-MM-DD\n", *date)
		return 2
	}
	if err := assemble(*dir, *changelog, *version, *date, *dryRun, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "changelog:", err)
		return 1
	}
	return 0
}

type fragment struct {
	path    string
	name    string
	bullets string // trimmed body, ends without newline
}

func assemble(dir, changelogPath, version, date string, dryRun bool, stdout io.Writer) error {
	frags, err := readFragments(dir)
	if err != nil {
		return err
	}
	if len(frags) == 0 {
		return fmt.Errorf("no fragments in %s", dir)
	}
	section := renderSection(version, date, frags)
	if dryRun {
		_, err := io.WriteString(stdout, section)
		return err
	}
	old, err := os.ReadFile(changelogPath)
	if err != nil {
		return err
	}
	if err := refuseConsumed(changelogPath, string(old), frags); err != nil {
		return err
	}
	updated, err := insertSection(string(old), version, section)
	if err != nil {
		return err
	}
	if err := writeAtomic(changelogPath, updated); err != nil {
		return err
	}
	// CHANGELOG.md already has the section, so a re-run would fail with
	// "already has a section": remove every fragment we can, then name the rest.
	var failed []string
	for _, c := range categories {
		for _, f := range frags[c] {
			if err := removeFragment(f.path); err != nil {
				failed = append(failed, fmt.Sprintf("  %s: %v", f.path, err))
			}
		}
	}
	if len(failed) > 0 {
		noun := "fragments"
		if len(failed) == 1 {
			noun = "fragment"
		}
		return fmt.Errorf("%s has the %s section but %d %s not removed:\n%s\nto recover, delete them by hand; the section is already written, so a re-run will refuse until the leftover fragments are deleted",
			changelogPath, version, len(failed), noun, strings.Join(failed, "\n"))
	}
	return nil
}

// readFragments returns the fragments grouped by category, each group sorted
// by file name. A name that does not fit <category>-<slug>.md is an error.
func readFragments(dir string) (map[string][]fragment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]fragment{}
	for _, e := range entries { // ReadDir sorts by name
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "README.md" || !e.Type().IsRegular() {
			continue
		}
		cat, ok := categoryOf(name)
		if !ok {
			return nil, fmt.Errorf("%s: name must be <category>-<slug>.md with category one of %s", name, strings.Join(categories, ", "))
		}
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		body := strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n"))
		if body == "" {
			return nil, fmt.Errorf("%s: no bullet lines", name)
		}
		if !strings.HasPrefix(body, "- ") {
			return nil, fmt.Errorf("%s: must start with a \"- \" bullet", name)
		}
		out[cat] = append(out[cat], fragment{path: path, name: name, bullets: body})
	}
	return out, nil
}

// refuseConsumed fails when a fragment bullet is already in the changelog,
// which is what a fragment that survived a failed remove looks like: without
// this, the next assemble would render it again into the new section.
func refuseConsumed(changelogPath, changelog string, frags map[string][]fragment) error {
	have := map[string]bool{}
	for _, b := range splitBullets(changelog) {
		have[b] = true
	}
	var found []string
	for _, c := range categories {
		for _, f := range frags[c] {
			for _, b := range splitBullets(f.bullets) {
				if have[b] {
					first, _, _ := strings.Cut(b, "\n")
					found = append(found, fmt.Sprintf("  %s: %s", f.path, first))
				}
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	noun := "bullets are"
	if len(found) == 1 {
		noun = "bullet is"
	}
	return fmt.Errorf("%d fragment %s already in %s, likely left by an assemble that could not delete its fragments:\n%s\nto recover, delete those fragment files by hand, then re-run",
		len(found), noun, changelogPath, strings.Join(found, "\n"))
}

// splitBullets returns each complete "- " bullet in text with its indented
// continuation lines, joined by "\n" with trailing whitespace trimmed, so two
// bullets compare equal only when every line of both matches.
func splitBullets(text string) []string {
	var out []string
	var cur []string
	flush := func() {
		if cur != nil {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		switch {
		case strings.HasPrefix(line, "- "):
			flush()
			cur = []string{line}
		case cur != nil && line != "" && (line[0] == ' ' || line[0] == '\t'):
			cur = append(cur, line)
		default:
			flush()
		}
	}
	flush()
	return out
}

func categoryOf(name string) (string, bool) {
	base, ok := strings.CutSuffix(name, ".md")
	if !ok {
		return "", false
	}
	for _, c := range categories {
		if slug, ok := strings.CutPrefix(base, c+"-"); ok && slug != "" {
			return c, true
		}
	}
	return "", false
}

func renderSection(version, date string, frags map[string][]fragment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] - %s\n", version, date)
	for _, c := range categories {
		if len(frags[c]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n\n", strings.ToUpper(c[:1])+c[1:])
		for _, f := range frags[c] {
			b.WriteString(f.bullets)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// insertSection places section after the Unreleased body, before the next
// "## [" heading (or at the end of the file).
func insertSection(old, version, section string) (string, error) {
	if strings.Contains(old, "\n## ["+version+"]") {
		return "", fmt.Errorf("version %s already has a section", version)
	}
	i := strings.Index(old, "\n"+unreleasedHeading+"\n")
	if i < 0 && !strings.HasSuffix(old, "\n"+unreleasedHeading) {
		return "", errors.New("no \"" + unreleasedHeading + "\" heading")
	}
	start := i + 1 + len(unreleasedHeading)
	rest := old[start:]
	var head, tail string
	if j := strings.Index(rest, "\n## ["); j >= 0 {
		head, tail = old[:start+j+1], old[start+j+1:]
	} else {
		head, tail = old, ""
	}
	head = strings.TrimRight(head, "\n") + "\n\n"
	if tail != "" {
		section += "\n"
	}
	return head + section + tail, nil
}

func writeAtomic(path, content string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".changelog-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
