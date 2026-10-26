package fanout

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestCitationDrift_CitedLineHoldsTheNamedIdentifier guards the hard file:line
// citations that point into internal/fanout/review.go from other files, plus the
// budget.go and rendered.go citations in internal/payload/claims.go. Nothing used
// to enforce them: when review.go shifted, a citation silently re-aimed at
// whatever line moved into its place, and the comment went on confidently
// describing an unrelated construct. The same set was re-pinned by hand twice and
// drifted again both times (Epic 52.0), so this test makes the check mechanical.
//
// Placement: this guard lives in internal/fanout, not internal/reconcile.
// CLAUDE.md puts doc-vs-code drift tests in internal/reconcile, but this one is a
// code-vs-code guard — almost every citation it reads is a Go comment pointing at
// Go source — and the source TD row (internal/payload/claims.go:642) prescribes
// internal/fanout. It reads every file by path, so no import cycle is possible
// whichever package it sits in.
//
// Accepted citation grammar. A citation is PATH:N or PATH:N-M, where PATH names
// one of the guarded target files (see citationScope) either repo-relative
// (internal/fanout/review.go) or bare (review.go, resolved against the citing
// file's own directory). It must name an identifier in one of two forms:
//
//	IDENT (PATH:N)      keepSmallestEntry (internal/fanout/review.go:4320-4346)
//	(IDENT, PATH:N)     (splitMarkedEntries, internal/payload/rendered.go:83-86)
//
// Backticks are allowed around IDENT and around PATH:N (the markdown form), and
// the identifier may end the previous line when a comment wraps. A bare
// (un-backticked) IDENT must look like code — an underscore, or both cases with
// an upper-case letter past the first (keepSmallestEntry, AllDropped) — so a
// prose word is not mistaken for one: not "loop" in "the funding loop
// (budget.go:175-181)", not "See", not an emphasised "POPULATES". Backtick any
// other identifier to name it (`budget`, `Truncation`). Any other shape — no parentheses, no
// identifier, "TD row PATH:N" — is unparseable and fails loudly, because a
// citation this test cannot read is one it cannot guard.
//
// The check: line N of the cited file (the FIRST line of a range) must hold IDENT
// as a whole word. The first line, not anywhere in the range: a one-line
// insertion above a function shifts it to N+1, which is still inside N-M, and an
// anywhere-in-range check would wave that drift through.
func TestCitationDrift_CitedLineHoldsTheNamedIdentifier(t *testing.T) {
	const repoRoot = "../.."
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Fatalf("repo root not found at %s from internal/fanout: %v", repoRoot, err)
	}

	total := 0
	for _, s := range citationScope {
		src := readRepoFile(t, filepath.Join(repoRoot, s.file))
		cites, problems := checkCitations(repoRoot, s.file, src, s.targets)
		total += cites
		for _, p := range problems {
			t.Error(p)
		}
	}
	// A pattern that silently stops matching would pass every file with nothing
	// checked. Some in-scope citations keep a line number by design, so the
	// count must never fall to zero.
	if total == 0 {
		t.Fatalf("no citations found in any in-scope file; the citation pattern no longer matches, so this guard checks nothing")
	}
}

// citationScope is the set of citing files and, for each, the target files whose
// line citations it carries. Epic 52.0 scoped it: the cross-package review.go
// citations, chunker.go's two (same package, kept by the owner), the two in
// docs/payload-modes.md, and claims.go's same-package budget.go and rendered.go
// citations. The ~40 in-package review.go citations elsewhere in
// internal/fanout and cli are out of scope.
var citationScope = []struct {
	file    string
	targets []string
}{
	{"internal/payload/claims.go", []string{"review.go", "budget.go", "rendered.go"}},
	{"internal/fanout/chunker.go", []string{"review.go"}},
	{"docs/payload-modes.md", []string{"review.go"}},
	{"internal/payload/prefetch.go", []string{"review.go"}},
	{"internal/payload/sizing.go", []string{"review.go"}},
	{"internal/payload/scope_vocabulary_test.go", []string{"review.go"}},
	{"internal/doctor/run.go", []string{"review.go"}},
	{"internal/hookobs/hookobs.go", []string{"review.go"}},
	{"internal/hookobs/hookobs_test.go", []string{"review.go"}},
	{"internal/registry/config_thinking_enabled_test.go", []string{"review.go"}},
}

var (
	// identBefore matches the "IDENT (" form at the end of the text preceding a
	// citation; identInParens matches the "(IDENT, " form.
	identBefore   = regexp.MustCompile("(`?)([A-Za-z_][A-Za-z0-9_]*)`?\\s*\\(`?$")
	identInParens = regexp.MustCompile("\\((`?)([A-Za-z_][A-Za-z0-9_]*)`?,\\s*`?$")
	closesParen   = regexp.MustCompile("^`?\\)")
	commentLead   = regexp.MustCompile(`^[\s/*#>-]*`)
)

// citationPattern matches PATH:N or PATH:N-M for the given target basenames.
func citationPattern(targets []string) *regexp.Regexp {
	quoted := make([]string, len(targets))
	for i, tg := range targets {
		quoted[i] = regexp.QuoteMeta(tg)
	}
	return regexp.MustCompile(`((?:[A-Za-z0-9_.-]+/)*(?:` + strings.Join(quoted, "|") + `)):(\d+)(?:-(\d+))?`)
}

// checkCitations parses every citation of targets in src (the contents of
// citing, a repo-relative path) and returns how many it found and one message
// per citation that is unparseable or stale.
func checkCitations(repoRoot, citing, src string, targets []string) (int, []string) {
	pat := citationPattern(targets)
	lines := strings.Split(src, "\n")
	cache := map[string][]string{}
	var problems []string
	count := 0
	for i, line := range lines {
		for _, m := range pat.FindAllStringSubmatchIndex(line, -1) {
			// A longer name that merely ends in a target ("preview.go") is not
			// a citation of it.
			if m[0] > 0 && strings.ContainsAny(line[m[0]-1:m[0]], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./-") {
				continue
			}
			count++
			site := fmt.Sprintf("%s:%d", citing, i+1)
			cite := line[m[0]:m[1]]

			prev := ""
			if i > 0 {
				prev = commentLead.ReplaceAllString(lines[i-1], "")
			}
			ctx := strings.TrimRight(prev, " \t") + " " + commentLead.ReplaceAllString(line[:m[0]], "")
			ident, ok := namedIdent(ctx)
			if !ok || !closesParen.MatchString(line[m[1]:]) {
				problems = append(problems, fmt.Sprintf(
					"%s: citation %s is unparseable: it must name an identifier, as `IDENT (%s)` or `(IDENT, %s)` "+
						"(see this test's doc comment for the grammar)", site, cite, cite, cite))
				continue
			}

			path := line[m[2]:m[3]]
			target := filepath.Join(filepath.Dir(citing), path)
			if strings.Contains(path, "/") {
				target = path
			}
			n, _ := strconv.Atoi(line[m[4]:m[5]])
			end := n
			if m[6] >= 0 {
				end, _ = strconv.Atoi(line[m[6]:m[7]])
			}

			tl, seen := cache[target]
			if !seen {
				b, err := os.ReadFile(filepath.Join(repoRoot, target))
				if err == nil {
					tl = strings.Split(string(b), "\n")
				}
				cache[target] = tl
			}
			switch {
			case tl == nil:
				problems = append(problems, fmt.Sprintf(
					"%s: citation %s names %s, but %s does not exist (a bare file name resolves against the citing file's directory)",
					site, cite, ident, target))
			case n < 1 || end < n || end > len(tl):
				problems = append(problems, fmt.Sprintf(
					"%s: citation %s names %s, but %s has %d lines, so the range is invalid", site, cite, ident, target, len(tl)))
			case !regexp.MustCompile(`\b` + regexp.QuoteMeta(ident) + `\b`).MatchString(tl[n-1]):
				problems = append(problems, fmt.Sprintf(
					"%s: citation %s names %s, but %s:%d does not hold it:\n\t%s\n"+
						"Re-pin the line number to where %s now sits; if %s was renamed, that is a legitimate cause — update the identifier too.",
					site, cite, ident, target, n, strings.TrimSpace(tl[n-1]), ident, ident))
			}
		}
	}
	return count, problems
}

// namedIdent extracts the identifier a citation names from the text that
// precedes it, under the grammar in the test's doc comment.
func namedIdent(ctx string) (string, bool) {
	for _, re := range []*regexp.Regexp{identInParens, identBefore} {
		if m := re.FindStringSubmatch(ctx); m != nil {
			backticked, ident := m[1] == "`", m[2]
			if !backticked && !looksLikeCode(ident) {
				return "", false
			}
			return ident, true
		}
	}
	return "", false
}

// looksLikeCode reports whether a bare word reads as a Go identifier rather
// than prose: it has an underscore, or mixes cases with an upper-case letter
// after the first character.
func looksLikeCode(word string) bool {
	if strings.Contains(word, "_") {
		return true
	}
	return strings.ContainsAny(word[1:], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") &&
		strings.ContainsAny(word, "abcdefghijklmnopqrstuvwxyz")
}
