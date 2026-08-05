package localdebt

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
)

// RootOpts are the inputs to ResolveStoreRoot: the caller's explicit override, the
// review directory whose manifest may carry a recorded root, whether this entry
// point may fall back to the process CWD, and the sink warnings are written to.
//
// AllowCWD is the entry-point policy knob rather than a global rule because the two
// entry points differ in kind, not in preference. The CLI's CWD is the reviewed repo
// by long-standing convention (that is what DefaultDir(".") has always meant), so it
// passes true. The MCP server's CWD is whatever launched the process and is
// meaningless by construction, so it passes false and an unresolvable root is a
// no-persist instead of a write to an unrelated directory.
type RootOpts struct {
	Explicit  string    // an operator-supplied root (--repo, or the MCP repo argument)
	ReviewDir string    // the review artifact directory whose manifest.json may record a root
	AllowCWD  bool      // may this entry point fall back to "." when nothing else resolves?
	Diag      io.Writer // best-effort diagnostics sink; nil is treated as io.Discard
}

// ResolveStoreRoot picks the repository root whose .atcr/debt store a reconcile
// persists to, applying an ordered precedence: explicit > manifest > CWD.
//
// ok == false means DO NOT PERSIST, and a warning has already been written to Diag.
// It is never an error return: persistence is best-effort at both entry points and
// never changes a command's exit code or an MCP tool's result.
//
// There is NO fall-through at any tier. A root that was named and turned out to be
// invalid is a stop signal, not a hint to try the next tier: falling through would
// convert a detectable no-op into an undetectable write to the wrong store, which is
// the exact failure the recorded-root design exists to prevent. That asymmetry is
// the whole contract — a MISSING claim falls through (an absent manifest root means
// nobody asserted anything), an INVALID claim does not.
func ResolveStoreRoot(opts RootOpts) (string, bool) {
	diag := opts.Diag
	if diag == nil {
		diag = io.Discard
	}

	// Tier 1: explicit. Existence only, no repo-root marker check: the caller named
	// this root deliberately, and requiring a marker would reject a legitimate root
	// the operator knows about (a fresh directory that will hold .atcr) for no gain.
	//
	// That justification is strongest for the CLI, where the value has already been
	// through normalizeRepoFlag and a human typed it. It is WEAKER over MCP, where
	// in.Repo is model-supplied: an unmarked existing directory is accepted, which
	// makes "create <some existing dir>/.atcr/debt/" reachable from a tool argument.
	// The impact is bounded (the server already writes review artifacts with the same
	// privileges) but the asymmetry is real and unaddressed — see TD-023.
	if explicit := strings.TrimSpace(opts.Explicit); explicit != "" {
		if root, ok := existingDir(explicit); ok {
			return root, true
		}
		_, _ = fmt.Fprintf(diag, "localdebt: repo root %q does not exist or is not a directory; skipping local debt persistence\n", explicit)
		return "", false
	}

	// Tier 2: the root recorded in the review manifest. A recorded root is a CLAIM,
	// not a fact — an artifact tree copied to another machine carries an absolute
	// path that resolves to nothing, or worse, to some unrelated directory — so it
	// is re-validated against a repo-root marker before it is trusted with a write.
	if opts.ReviewDir != "" {
		if m, err := fanout.ReadManifest(opts.ReviewDir); err == nil {
			if recorded := strings.TrimSpace(m.Root); recorded != "" {
				if root, ok := validateRepoRoot(recorded); ok {
					return root, true
				}
				_, _ = fmt.Fprintf(diag, "localdebt: manifest repo root %q is no longer a valid repository root (copied or stale artifacts?); skipping local debt persistence\n", recorded)
				return "", false
			}
		}
		// A missing, unreadable, or corrupt manifest, and a manifest with an empty
		// root, all fall through to tier 3: nothing was claimed, so nothing is stale.
	}

	// Tier 3: the process CWD, byte-for-byte the pre-manifest DefaultDir(".")
	// behavior. Deliberately unvalidated — adding a marker requirement here would
	// change CLI behavior beyond the AC and break the existing suite, whose isolate()
	// helper chdirs into a bare temp dir with no .git.
	if opts.AllowCWD {
		return ".", true
	}
	_, _ = fmt.Fprintf(diag, "localdebt: no repo root recorded in the review manifest and no repo argument given; skipping local debt persistence (pass repo=<path>)\n")
	return "", false
}

// existingDir cleans p to an absolute path and reports whether it is an existing
// directory. Used by the explicit tier, which checks existence but not repo-ness.
func existingDir(p string) (string, bool) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return filepath.Clean(abs), true
}

// validateRepoRoot reports whether path is still a plausible repository root: an
// existing directory carrying a .git or .atcr marker. The marker pair reuses the
// repo-root definition already established at cli/root.go rather than inventing a
// second one.
//
// The two markers are checked differently, and the asymmetry is deliberate. A .git
// entry counts whether it is a directory or a FILE, because a linked worktree and a
// submodule both record their root with a .git file — rejecting those would make the
// resolver refuse to persist in exactly the setups where a developer is most likely
// to be running from somewhere other than the main checkout. An .atcr marker must be
// a DIRECTORY, because that is the only form atcr itself ever creates; accepting a
// stray .atcr file would let an arbitrary directory pass as a repository root and
// weaken the stale-claim re-validation this whole design rests on.
func validateRepoRoot(path string) (string, bool) {
	abs, ok := existingDir(path)
	if !ok {
		return "", false
	}
	// .git: directory or file.
	if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
		return abs, true
	}
	// .atcr: directory only.
	if info, err := os.Stat(filepath.Join(abs, ".atcr")); err == nil && info.IsDir() {
		return abs, true
	}
	return "", false
}
