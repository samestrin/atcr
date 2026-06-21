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
// for every atcr entry point — the cmd/atcr reconcile/review/resume commands all
// thread Root="." into reconcile, the sole caller) when empty. A nil finding is
// a no-op.
//
// Symlink safety (Epic 5.4 AC5): existence is resolved with filepath.EvalSymlinks
// and the result is re-checked for containment under the (also symlink-resolved)
// root. A repo symlink that points outside the repo therefore cannot turn a
// reviewer-controlled File into an existence oracle for the host filesystem —
// the bare os.Stat used in 5.0 followed such a link and reported the external
// file as present.
//
// Case sensitivity (Epic 5.4 AC3) is handled at the reconcile layer via the
// candidate index (CaseCorrection), not here: os.Stat/EvalSymlinks remain
// case-insensitive on the default macOS/Windows filesystems, so a case-only typo
// still resolves as present at this layer and is caught by the index instead.
//
// idx is the candidate file index for this reconcile run (Epic 5.4), built once
// from `git ls-files` and shared across every finding. It powers PathSuggestion
// and the case-only check; a nil idx (non-git repo, or git unavailable) cleanly
// degrades to 5.0 existence-only behavior with no suggestion.
func ValidatePath(f *Finding, root string, idx *FileIndex) {
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
	// invalid rather than probed (and never suggested — it is not a typo). This
	// lexical guard is cheap and runs before any filesystem call; the symlink-
	// resolved containment check below catches the cases lexical analysis cannot
	// (a path that escapes only after a symlinked segment is followed).
	if rel, err := filepath.Rel(root, joined); err != nil ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		f.PathValid = false
		f.PathWarning = PathNotFoundWarning
		return
	}
	// Tier 3 (case-only) is checked before existence: on a case-insensitive
	// filesystem os.Stat/EvalSymlinks would report a case-typo as present, so the
	// index — not the filesystem — is authoritative for case. A byte-exact
	// citation reports no mismatch and falls through to the existence check.
	if idx != nil {
		if suggestion, mismatch := idx.CaseCorrection(f.File); mismatch {
			f.PathValid = false
			f.PathWarning = PathNotFoundWarning
			f.PathSuggestion = suggestion // "" when ambiguous (multiple cases)
			return
		}
	}
	switch existsContained(root, joined) {
	case existsInside:
		f.PathValid = true
		f.PathWarning = ""
	case existsOutsideOrAbsent:
		f.PathValid = false
		f.PathWarning = PathNotFoundWarning
		if idx != nil {
			// Tier 1 (exact basename elsewhere) then Tier 2 (same-dir typo).
			f.PathSuggestion = idx.MissingSuggestion(f.File)
		}
	default: // existsIndeterminate
		// Indeterminate (permission, I/O): leave the finding unflagged rather
		// than assert a "not found" we cannot prove.
	}
}

type existence int

const (
	existsIndeterminate existence = iota
	existsInside
	existsOutsideOrAbsent
)

// existsContained reports whether joined resolves to a real file that still
// lives under root once every symlink in both paths is resolved. A path that is
// absent, or that escapes root via a symlink, is existsOutsideOrAbsent; a
// permission/IO error that proves nothing is existsIndeterminate.
func existsContained(root, joined string) existence {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		absJoined = joined
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		realRoot = absRoot // best effort; containment still re-checked below
	}
	resolved, err := filepath.EvalSymlinks(absJoined)
	switch {
	case err == nil:
		rel, rerr := filepath.Rel(realRoot, resolved)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return existsOutsideOrAbsent // escaped the repo via a symlink
		}
		return existsInside
	case os.IsNotExist(err):
		return existsOutsideOrAbsent
	default:
		return existsIndeterminate
	}
}
