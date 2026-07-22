// Package security guards ATCR's host-repository writes against Indirect Sandbox
// Escape (a.k.a. Host Trust Transposition) attacks. A contained sandbox execution
// is bypassed not by breaking the sandbox, but by writing a malicious configuration
// artifact into the workspace (e.g. .git/config's core.pager, a .githooks/pre-commit
// script, a .github/workflows/ CI definition, or a .vscode/tasks.json) that a
// host-side tool — Git CLI, CI runner, or IDE — later executes with full developer
// privileges once the sandboxed review ends.
//
// IsProtectedPath is the reusable predicate that identifies those host-execution
// and configuration paths so callers (the --auto-fix patch-apply gate, non-blocking
// PR-review flags) can refuse or flag writes to them.
//
// Scope: this package matches REPO-RELATIVE configuration paths (.git/, .githooks/,
// .github/workflows/, .vscode/, .idea/, .env*, .planning/, .atcr, and CI defs). It is
// deliberately distinct from internal/validation.FilePath, which blocks ABSOLUTE
// system directories (/etc, /proc, /sys). Neither is a substitute for OS-level
// permission enforcement; they are defense-in-depth input guards.
package security

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrProtectedPath is the sentinel returned (wrapped) by callers that refuse to
// write a protected path. IsProtectedPath itself returns only a bool; this sentinel
// lets a refusing caller (e.g. internal/autofix's apply gate) wrap it with %w so
// its own callers and tests can errors.Is against a single, stable identity rather
// than string-matching the refusal message. It carries no path of its own — the
// wrapping caller adds the offending path to the error text.
var ErrProtectedPath = errors.New("path is protected by workspace-integrity policy")

// protectedDirs is the set of repo-relative path SEGMENTS whose presence anywhere
// in a path marks the whole path as protected. Matching is on whole path segments
// (never bare substring), so .gitignore, .githubx/, and .vscode-custom/ do NOT
// false-positive against .git, .github, and .vscode respectively. Names are compared
// case-insensitively (see IsProtectedPath) so a case-insensitive host filesystem
// cannot smuggle a write through .GIT/config or .VSCode/tasks.json.
var protectedDirs = map[string]bool{
	".git":      true, // hooks, config (core.hooksPath, core.pager, url.insteadOf), refs
	".githooks": true, // custom core.hooksPath target
	".vscode":   true, // editor tasks.json / launch.json auto-execution
	".idea":     true, // JetBrains run configurations
	".planning": true, // ATCR's own planning state
	".atcr":     true, // ATCR's own config (file or directory)
	".circleci": true, // CircleCI pipeline config — auto-executes on next push like .github/workflows
}

// protectedFiles is the set of repo-relative single-file config artifacts whose
// exact segment name (case-insensitive) marks a path as protected regardless of
// directory depth. These are CI definitions that execute on the next push.
var protectedFiles = map[string]bool{
	".gitlab-ci.yml": true, // GitLab CI pipeline definition
}

// buildScriptBasenames is the advisory-only (non-blocking) soft list of build/
// packaging file BASENAMES whose touch a human reviewer should notice before
// approving an --auto-fix PR — they run with elevated trust the next time they
// are invoked. This is deliberately distinct from the protectedDirs/protectedFiles
// BLOCKING blocklist above: FlagsForReview never refuses a write, it only reports.
// Names are compared case-insensitively (see isBuildScriptPath).
var buildScriptBasenames = map[string]bool{
	"makefile":     true, // make target definitions
	"package.json": true, // npm scripts / lifecycle hooks
	"dockerfile":   true, // image build steps
	"jenkinsfile":  true, // Jenkins pipeline definition
}

// buildScriptCISegments is the advisory-only set of CI-definition path SEGMENTS
// living OUTSIDE .github/ (which the blocking blocklist already owns via
// .github/workflows and .gitlab-ci.yml). A path already on IsProtectedPath's
// blocklist (e.g. .gitlab-ci.yml) only ever reaches FlagsForReview if
// --allow-config-edits let it through the earlier gate — this is advisory
// visibility for whatever the protected-path gate already allowed, not a second
// copy of that gate. No CI provider is added beyond the set epic 32.4 named.
var buildScriptCISegments = map[string]bool{
	".gitlab-ci.yml": true, // GitLab CI (file) — also on the blocking blocklist
	".circleci":      true, // CircleCI config dir — also on the blocking blocklist (protectedDirs)
}

// IsProtectedPath reports whether path targets a host-execution or configuration
// artifact that must not be written by an --auto-fix patch (see the package doc for
// the threat model). It performs boundary-safe, case-insensitive matching against a
// repo-relative blocklist and returns bool (never an error): an empty or otherwise
// unmatched path is reported as not-protected (false).
//
// Normalization has two layers:
//
//  1. filepath.Clean collapses "./" and resolves ".." lexically, so a traversal
//     form such as "foo/../.git/config" is caught as ".git/config". This layer
//     needs no I/O and works for not-yet-created files — the common --auto-fix case
//     where a patch creates a new file that does not yet exist on disk.
//  2. filepath.EvalSymlinks (against the deepest existing ancestor, rejoining the
//     not-yet-created tail) catches a symlink whose target resolves into a protected
//     directory (AC3). This performs a read-only stat/readlink; it has no other side
//     effects and degrades to layer 1 when nothing on the path exists yet. A
//     repo-relative path is anchored against root before this resolution so the
//     symlink lookup is correct regardless of the process CWD (TD-003).
//
// root is the working-tree root the caller is applying into (apply.go passes its
// applyOne root). It is used ONLY to anchor the layer-2 symlink resolution; layer-1
// segment matching is CWD-independent and ignores it. Pass "" when there is no root
// context or the path is already absolute — resolution then falls back to the path
// as-given.
//
// The blocklist is matched on whole path segments, so lookalikes (.gitignore,
// .githubx/, .vscode-custom/, README.planning.md) are correctly NOT protected.
// Callers are expected to pass paths relative to the repository root, matching how
// internal/autofix's apply path already operates; absolute paths that contain a
// protected segment are also matched.
func IsProtectedPath(path, root string) bool {
	if path == "" {
		return false
	}
	clean := filepath.Clean(path)
	// Layer 1: lexical match on the cleaned path (handles not-yet-created files
	// and ".."-traversal collapsed by Clean). Segment matching is relative-form and
	// CWD-independent, so it needs no root.
	if isProtectedSegments(clean) {
		return true
	}
	// Layer 2: resolve on-disk symlinks and re-check, catching a symlink whose target
	// lands inside a protected directory. A repo-relative path is anchored against root
	// (the working-tree root the caller is applying into) before resolution, so the
	// symlink lookup is correct regardless of the process CWD (TD-003). When root is
	// empty or path is already absolute, resolution falls back to path as-given.
	target := clean
	if root != "" && !filepath.IsAbs(clean) {
		target = filepath.Join(root, clean)
	}
	if resolved, ok := resolveSymlinks(target); ok && resolved != target {
		if isProtectedSegments(resolved) {
			return true
		}
	}
	return false
}

// isProtectedSegments reports whether any whole segment of the (already cleaned)
// path is a protected directory, a protected CI file, or an .env* artifact. The
// path is slash-normalized so the split is correct on every OS. Comparison is
// case-insensitive so a case-insensitive host filesystem cannot bypass the guard.
func isProtectedSegments(clean string) bool {
	segs := strings.Split(filepath.ToSlash(clean), "/")
	for i, s := range segs {
		if s == "" || s == "." || s == ".." {
			continue
		}
		seg := s
		if runtime.GOOS == "windows" {
			// Windows silently strips trailing dots/spaces and honors NTFS Alternate
			// Data Stream (":") aliases, so `.git.`, `.git ` and `.git::$INDEX_ALLOCATION`
			// all resolve into the protected dir on a Windows host. Collapse each
			// segment to its base name before comparison so those forms cannot bypass
			// the guard (autofix writes files directly, so git's protectNTFS never runs).
			seg = normalizeWindowsSegment(s)
		}
		low := strings.ToLower(seg)
		// dotenv secrets and direnv config: exactly ".env", any ".env.<suffix>"
		// (.env.local, .env.production, .env.example.txt), and ".envrc". Scoped so
		// legitimately-named segments that merely START with ".env" — .environments/,
		// .envoy/, .envision — do NOT false-positive (TD-002).
		if low == ".env" || strings.HasPrefix(low, ".env.") || low == ".envrc" {
			return true
		}
		if protectedFiles[low] {
			return true
		}
		if protectedDirs[low] {
			return true
		}
		// .github is protected only at its host-executing subtrees — /workflows
		// (CI definitions) and /actions (local/composite actions whose action.yml
		// runs arbitrary shell via `runs.using: composite` when a trusted workflow
		// references ./.github/actions/<name>). It is NOT protected wholesale, so
		// .github/CODEOWNERS, .github/dependabot.yml, and .github/ISSUE_TEMPLATE/ are
		// not host-executing and stay writable.
		if low == ".github" && i+1 < len(segs) &&
			(strings.EqualFold(segs[i+1], "workflows") || strings.EqualFold(segs[i+1], "actions")) {
			return true
		}
	}
	return false
}

// normalizeWindowsSegment collapses a single path segment to the base name Windows
// itself would resolve it to: it strips an NTFS Alternate Data Stream suffix
// (everything from the first ':') and then the trailing dots/spaces Windows silently
// removes. So ".git::$INDEX_ALLOCATION", ".git." and ".git " all normalize to ".git",
// closing the segment-aliasing bypass of isProtectedSegments on a Windows host. It
// deliberately does NOT resolve 8.3 short-name aliases (e.g. "GITHUB~1" -> ".github"):
// the long name a short name maps to is assigned by the filesystem and cannot be
// recovered lexically, so that vector is out of scope here (layer-2 EvalSymlinks still
// catches it when the protected dir already exists). Applied only on Windows — on Unix
// a trailing dot/space or a ':' is a legitimately distinct, valid filename.
func normalizeWindowsSegment(s string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, ". ")
}

// resolveSymlinks resolves the symlinks in path against the deepest existing
// ancestor directory, rejoining any not-yet-created trailing components, and
// reports whether a resolution was obtained. It first tries the full path; if that
// does not exist yet (the common --auto-fix create case), it walks up to the nearest
// existing ancestor, resolves that, and rejoins the remainder — so protection still
// applies to a new file being created inside a symlinked protected directory. When
// no ancestor exists (or the path is not resolvable), it returns ("", false) and the
// caller relies on the lexical layer alone.
func resolveSymlinks(path string) (string, bool) {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r, true
	}
	dir := path
	var tail []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root or "." with nothing resolvable.
			return "", false
		}
		tail = append([]string{filepath.Base(dir)}, tail...)
		if r, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(append([]string{r}, tail...)...), true
		}
		dir = parent
	}
}

// FlagsForReview reports whether a patch touching path warrants extra reviewer
// attention, and if so a human-readable reason. It is purely ADVISORY: unlike
// IsProtectedPath it never blocks a write and never returns an error, so it is safe
// to call on any path. Callers surface its reason in the generated PR body rather
// than refusing the apply.
//
// One condition is flagged — a build-script path: path's basename is a known build/
// packaging file (Makefile, package.json, Dockerfile, Jenkinsfile), ends in the *.sh
// glob, or contains a CI-definition segment outside .github/ (.gitlab-ci.yml,
// .circleci/). Matching is boundary-safe (whole basename / whole segment, never a
// bare substring) so lookalikes (not-a-Makefile.txt, foo.shell, mySh.go,
// nested/package.json.bak) are correctly NOT flagged.
//
// It deliberately does NOT flag executable-bit changes: the apply pipeline writes
// every file through atomicfs.WriteFileAtomic (fixed 0644) and the GitHub commit
// hardcodes blob mode 100644, so a diff's exec-bit change never lands in the working
// tree or the PR — flagging it would warn about a change that does not happen. (That
// the pipeline silently discards exec-bit changes is tracked as separate tech debt.)
func FlagsForReview(path string) (bool, string) {
	if isBuildScriptPath(path) {
		return true, "build-script path"
	}
	return false, ""
}

// isBuildScriptPath reports whether path (repo-relative) targets a build/CI script
// on the advisory soft list. It matches the cleaned path's whole basename against
// buildScriptBasenames and the *.sh glob, and any whole path segment against
// buildScriptCISegments — boundary-safe, case-insensitive, never a bare substring.
func isBuildScriptPath(path string) bool {
	if path == "" {
		return false
	}
	segs := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	base := strings.ToLower(segs[len(segs)-1])
	if buildScriptBasenames[base] || strings.HasSuffix(base, ".sh") {
		return true
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			continue
		}
		if buildScriptCISegments[strings.ToLower(s)] {
			return true
		}
	}
	return false
}
