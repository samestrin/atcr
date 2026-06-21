package stream

import (
	"log/slog"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/log"
)

// FileIndex is the candidate file index built once per reconcile run from
// `git ls-files` (Epic 5.4 AC1). It is the missing primitive the tiered path
// matcher reasons against: with the repo's real, tracked file list in hand,
// case-exact matching, wrong-directory detection, and typo suggestion all
// become lookups rather than filesystem probes.
//
// All keys are slash-normalized (git emits forward slashes on every platform;
// reviewer-cited paths are normalized on the way in) so matching is
// platform-independent. The index is read-only after BuildFileIndex returns.
type FileIndex struct {
	tracked  map[string]struct{} // exact relpath set
	basename map[string][]string // basename -> tracked relpaths
	dirFiles map[string][]string // dir -> basenames tracked directly under it
	folded   map[string][]string // lower(relpath) -> tracked relpaths (case-fold)
}

// BuildFileIndex runs `git ls-files` under root and builds the candidate index.
//
// root must be the repository root (not a subdirectory). The returned tracked
// paths are relative to root, and callers expect Finding.File paths to share
// that base. Passing a subdirectory would make git emit paths relative to that
// subdirectory while the caller still supplied repo-root-relative paths,
// silently producing zero useful matches. Today all callers pass "." / the
// repo root; if subdir roots ever become supported, reconcile the path bases
// between the index and the caller before matching.
//
// It returns nil — signalling "degrade to existence-only, no suggestion" — when
// root is empty, root is not a git repository, or git is unavailable. A repo
// with no tracked files yields a non-nil but empty index.
func BuildFileIndex(root string) *FileIndex {
	return BuildFileIndexWithLogger(root, nil)
}

// BuildFileIndexWithLogger is BuildFileIndex with an explicit diagnostic sink.
// The nil-return contract is unchanged — an empty, non-repo, or git-unavailable
// root still degrades to "existence-only, no suggestion" — but the underlying
// git failure is logged at WARN before returning nil, so a silently disabled
// path matcher in CI (git missing, corrupt repo, timeout) is distinguishable
// from a healthy run. An empty root is the legitimate "validation disabled" case
// and is NOT logged. A nil logger is treated as a discard sink.
func BuildFileIndexWithLogger(root string, logger *slog.Logger) *FileIndex {
	if logger == nil {
		logger = log.Discard()
	}
	if strings.TrimSpace(root) == "" {
		return nil
	}
	// -z gives NUL-delimited paths so filenames with spaces/newlines survive.
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		// Not a git repo, git missing, or other failure: graceful degradation.
		// Log before discarding so a silently disabled path matcher in CI is not
		// mistaken for a healthy run (the failure was previously swallowed).
		logger.Warn("path index disabled: git ls-files failed; degrading to existence-only validation",
			"root", root, "err", err)
		return nil
	}
	return indexFromPaths(strings.Split(string(out), "\x00"))
}

// toSlashKeys normalizes a relpath to forward-slash form regardless of the
// build OS. filepath.ToSlash only converts OS-native separators, so on Unix it
// leaves backslashes untouched; reviewer-cited paths may contain backslashes,
// which must normalize to match git's slash-only output.
func toSlashKeys(p string) string {
	return filepath.ToSlash(strings.ReplaceAll(p, "\\", "/"))
}

// indexFromPaths builds the index maps from raw (possibly NUL-split, possibly
// non-slash) relpaths. Split from BuildFileIndex so the resolver logic can be
// tested with synthetic path sets — notably the case-ambiguity scenario, which
// is physically unconstructable on a case-insensitive filesystem.
func indexFromPaths(raw []string) *FileIndex {
	idx := &FileIndex{
		tracked:  make(map[string]struct{}),
		basename: make(map[string][]string),
		dirFiles: make(map[string][]string),
		folded:   make(map[string][]string),
	}
	for _, r := range raw {
		rel := r
		if rel == "" {
			continue
		}
		rel = toSlashKeys(rel)
		if _, seen := idx.tracked[rel]; seen {
			continue
		}
		idx.tracked[rel] = struct{}{}

		base := path.Base(rel)
		idx.basename[base] = append(idx.basename[base], rel)

		d := path.Dir(rel) // "." for top-level files
		idx.dirFiles[d] = append(idx.dirFiles[d], base)

		fold := strings.ToLower(rel)
		idx.folded[fold] = append(idx.folded[fold], rel)
	}
	return idx
}

// Has reports whether relpath is a byte-exact tracked file.
func (x *FileIndex) Has(relpath string) bool {
	if x == nil {
		return false
	}
	_, ok := x.tracked[toSlashKeys(relpath)]
	return ok
}

// ByBasename returns the tracked relpaths whose basename equals base.
func (x *FileIndex) ByBasename(base string) []string {
	if x == nil {
		return nil
	}
	return x.basename[base]
}

// DirBasenames returns the basenames tracked directly under dir (slash-form,
// no trailing slash; "." for the repo root). Empty when the directory tracks no
// files — which Tier 2 reads as "directory does not exist in the repo".
func (x *FileIndex) DirBasenames(dir string) []string {
	if x == nil {
		return nil
	}
	return x.dirFiles[toSlashKeys(dir)]
}

// HasDir reports whether any tracked file lives directly under dir.
func (x *FileIndex) HasDir(dir string) bool {
	if x == nil {
		return false
	}
	return len(x.dirFiles[toSlashKeys(dir)]) > 0
}

// ByFold returns the tracked relpaths equal to relpath under ASCII-lowercase
// matching (not full Unicode case folding). A correctly-cased citation returns
// itself; a case-typo returns the real path(s).
func (x *FileIndex) ByFold(relpath string) []string {
	if x == nil {
		return nil
	}
	return x.folded[strings.ToLower(toSlashKeys(relpath))]
}
