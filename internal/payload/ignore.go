package payload

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Repo-root ignore sources consulted to keep generated/vendored/lockfile churn
// out of the review payload (Epic 26.0). Both are repo-root-only.
const (
	ignoreFileGit  = ".gitignore"
	ignoreFileAtcr = ".atcrignore"
)

// ignoreMatcher decides whether a changed file should be excluded from the
// review payload. It combines two repo-root sources, OR'd together so exclusion
// is purely additive:
//   - .gitignore: full gitignore semantics (including "!" negation between its
//     own patterns), matching what git itself would ignore.
//   - .atcrignore: repo-root-only, additive to .gitignore. "!" negation lines
//     are dropped so an .atcrignore entry can never re-include a file — it can
//     only add exclusions on top of .gitignore's.
//
// A path is ignored when EITHER source matches. Keeping the two as separate
// matchers OR'd together (rather than one merged pattern list) means a
// .gitignore negation cannot un-exclude an .atcrignore match and vice-versa —
// exactly the "purely additive" contract the epic specifies.
type ignoreMatcher struct {
	git  *gitignore.GitIgnore
	atcr *gitignore.GitIgnore
}

// newIgnoreMatcher loads the repo-root .gitignore and .atcrignore from dir. A
// missing file is a no-op (not an error): token-waste protection is best-effort,
// so an absent or unreadable ignore file disables that source rather than
// failing the review. Read/parse failures are logged at debug via logger.
func newIgnoreMatcher(dir string, logger *slog.Logger) *ignoreMatcher {
	return &ignoreMatcher{
		git:  loadGitignore(filepath.Join(dir, ignoreFileGit), logger),
		atcr: loadAtcrignore(filepath.Join(dir, ignoreFileAtcr), logger),
	}
}

// loadGitignore compiles path with full gitignore semantics. A missing file
// returns nil (source disabled); a parse failure is logged at debug and also
// disables the source rather than aborting the review.
func loadGitignore(path string, logger *slog.Logger) *gitignore.GitIgnore {
	if _, err := os.Stat(path); err != nil {
		return nil // absent (or unstattable) → no-op
	}
	gi, err := gitignore.CompileIgnoreFile(path)
	if err != nil {
		logger.Debug("payload: unreadable .gitignore, ignore filtering skips it", "path", path, "err", err)
		return nil
	}
	return gi
}

// loadAtcrignore reads .atcrignore line-by-line and strips "!" negation lines
// before compiling, enforcing the additive-only contract (no re-inclusion). A
// missing file returns nil; an unreadable file is logged at debug and disabled.
func loadAtcrignore(path string, logger *slog.Logger) *gitignore.GitIgnore {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Debug("payload: unreadable .atcrignore, ignore filtering skips it", "path", path, "err", err)
		}
		return nil // absent → no-op
	}
	var lines []string
	for _, ln := range strings.Split(string(data), "\n") {
		// Drop negation lines: .atcrignore is purely additive, so "!pat" must not
		// re-include a file already excluded. A leading backslash escapes a literal
		// "!" (gitignore rule 4) and is a real pattern, so it is kept.
		if strings.HasPrefix(strings.TrimSpace(ln), "!") {
			continue
		}
		lines = append(lines, ln)
	}
	if len(lines) == 0 {
		return nil
	}
	return gitignore.CompileIgnoreLines(lines...)
}

// active reports whether either source contributed patterns; when false the
// filter is a pure no-op and callers skip partitioning entirely.
func (m *ignoreMatcher) active() bool {
	return m != nil && (m.git != nil || m.atcr != nil)
}

// match reports whether path (repo-root-relative, forward slashes — the form
// git diff --name-status emits with core.quotePath=false) is excluded by either
// source.
func (m *ignoreMatcher) match(path string) bool {
	if m == nil {
		return false
	}
	if m.git != nil && m.git.MatchesPath(path) {
		return true
	}
	if m.atcr != nil && m.atcr.MatchesPath(path) {
		return true
	}
	return false
}
