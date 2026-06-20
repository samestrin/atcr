package stream

import (
	"os"
	"path/filepath"
	"strings"
)

// PathNotFoundWarning is the warning stamped on a finding whose file does not
// exist under the validation root (Epic 5.0 AC2). The text is part of the
// machine contract surfaced in findings.json (path_warning) and rendered into
// the human reports.
const PathNotFoundWarning = "file not found"

// ValidatePath stamps f.PathValid and f.PathWarning by checking whether f.File
// exists under root. It is the single existence check Epic 5.0 layers on top of
// parsing — paths emitted by reviewers are otherwise opaque labels, so a
// hallucinated path (a typo, or a real file in the wrong directory) is parsed
// and reported as if it were correct.
//
// Semantics, chosen so a finding is never silently lost or falsely flagged:
//   - File exists           -> PathValid=true,  PathWarning=""
//   - File is absent         -> PathValid=false, PathWarning=PathNotFoundWarning
//   - Empty File             -> left untouched (nothing to validate)
//   - Stat fails for another reason (e.g. permissions) -> left untouched; an
//     indeterminate result must not masquerade as "file not found".
//
// root defaults to "." (the process working directory, which is the repo root
// for every atcr entry point — see internal/verify, which threads the same "."
// root) when empty. A nil finding is a no-op.
func ValidatePath(f *Finding, root string) {
	if f == nil {
		return
	}
	if strings.TrimSpace(f.File) == "" {
		return // no path to validate
	}
	if root == "" {
		root = "."
	}
	joined := filepath.Join(root, f.File)
	// Refuse to escape the validation root. filepath.Join cleans the path, so a
	// traversal ("../../x") or absolute ("/etc/passwd") File could otherwise stat
	// a file outside the reviewed repo — an existence oracle, and a path outside
	// the repo is not a valid review location anyway. Such a path is flagged
	// invalid rather than probed. Lexical containment (no EvalSymlinks) is
	// proportionate: this is existence-only and never reads file contents.
	if rel, err := filepath.Rel(root, joined); err != nil ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		f.PathValid = false
		f.PathWarning = PathNotFoundWarning
		return
	}
	_, err := os.Stat(joined)
	switch {
	case err == nil:
		f.PathValid = true
		f.PathWarning = ""
	case os.IsNotExist(err):
		f.PathValid = false
		f.PathWarning = PathNotFoundWarning
	default:
		// Indeterminate (permission, I/O): leave the finding unflagged rather
		// than assert a "not found" we cannot prove.
	}
}

// ValidatePaths stamps every finding in the slice in place (see ValidatePath).
func ValidatePaths(findings []Finding, root string) {
	for i := range findings {
		ValidatePath(&findings[i], root)
	}
}
