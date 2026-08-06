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
	Explicit  string // an operator-supplied root (--repo, or the MCP repo argument)
	ReviewDir string // the review artifact directory whose manifest.json may record a root
	AllowCWD  bool   // may this entry point fall back to "." when nothing else resolves?
	// RequireMarker tightens the explicit tier for entry points whose root is
	// model-supplied rather than operator-typed: the named directory must also
	// carry a repo-root marker (.git or .atcr), closing the "create <any existing
	// dir>/.atcr/debt/" primitive a tool argument would otherwise give. The CLI
	// leaves it false — its explicit value has already been through
	// normalizeRepoFlag and a human typed it (TD internal/localdebt/root.go:49).
	RequireMarker bool
	Diag          io.Writer // best-effort diagnostics sink; nil is treated as io.Discard
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

	// Tier 1: explicit. The default check is existence only, no repo-root marker:
	// the caller named this root deliberately, and requiring a marker would reject
	// a legitimate root the operator knows about (a fresh directory that will hold
	// .atcr) for no gain.
	//
	// That justification holds for the CLI, where the value has already been
	// through normalizeRepoFlag and a human typed it. It does NOT hold over MCP,
	// where in.Repo is model-supplied — an unmarked existing directory would make
	// "create <some existing dir>/.atcr/debt/" reachable from a tool argument. The
	// MCP entry point therefore sets RequireMarker, and the tier then runs the
	// same validateRepoRoot re-validation the manifest tier gets (closing TD-023).
	if opts.Explicit != "" {
		// A SUPPLIED-but-blank root ("   " — the shape an unset or partially
		// quoted shell variable produces, e.g. --repo "$REPO " with REPO unset)
		// is an invalid claim, not an absent one: the operator named a root, and
		// acting as if they named nothing is the one silent transition the
		// no-fall-through contract above forbids.
		if strings.TrimSpace(opts.Explicit) == "" {
			_, _ = fmt.Fprintf(diag, "localdebt: repo root is blank (whitespace only); skipping local debt persistence\n")
			return "", false
		}
		explicit := strings.TrimSpace(opts.Explicit)
		if opts.RequireMarker {
			// A model-supplied root that is ITSELF a symlink is refused, for the
			// same reason HasRepoRootMarker Lstats its markers: the link is not
			// the repository, it is a pointer at one, and everything downstream
			// (existingDir's Stat, the write path's MkdirAll) follows it. A link
			// repointed after this check moves the whole store with it. The
			// caller who legitimately wants that directory can name it directly
			// (TD internal/mcp/tools.go:79).
			if abs, err := filepath.Abs(explicit); err == nil {
				if info, lerr := os.Lstat(abs); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
					_, _ = fmt.Fprintf(diag, "localdebt: repo root %q is a symlink; name the repository directory itself; skipping local debt persistence\n", explicit)
					return "", false
				}
			}
			if _, ok := existingDir(explicit); !ok {
				_, _ = fmt.Fprintf(diag, "localdebt: repo root %q does not exist or is not a directory; skipping local debt persistence\n", explicit)
				return "", false
			}
			// The marker UNION, not .git specifically: this root was named by a
			// caller, so a repo marked only by .atcr is a legitimate answer. The
			// stricter rule below applies to roots no human named.
			if root, ok := validateRepoRoot(explicit, false); ok {
				return root, true
			}
			_, _ = fmt.Fprintf(diag, "localdebt: repo root %q carries no repository marker (.git/.atcr); skipping local debt persistence\n", explicit)
			return "", false
		}
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
		// A MISSING manifest falls through (nobody asserted anything); an
		// UNREADABLE one does not — it is an invalid claim whose invalidity
		// happens to be unreadable, and the invalid-claim rule above admits no
		// fall-through. Stat first: ReadManifest reports its not-exist case with
		// a message that does not wrap fs.ErrNotExist, so the returned error
		// alone cannot separate "no manifest" from "manifest present but
		// unparseable".
		if _, statErr := os.Stat(filepath.Join(opts.ReviewDir, "manifest.json")); statErr == nil {
			if m, err := fanout.ReadManifest(opts.ReviewDir); err == nil {
				if recorded := strings.TrimSpace(m.Root); recorded != "" {
					// requireGit: a MACHINE-recorded root must carry .git
					// specifically, not the .git-or-.atcr union every other tier
					// accepts (TD internal/localdebt/root.go:133). Nobody typed this
					// value — the review path stamped it from the engine root, which
					// cli/serve.go hardcodes to "." — and the directory it names
					// NECESSARILY contains .atcr/, because atcr just wrote the review
					// into it. Under the union that made tier 2 re-validate the
					// server's own CWD, the one directory AllowCWD:false exists to
					// refuse, so an artifacts-only tree could self-validate into a
					// store write. .git is the marker no atcr operation creates.
					if root, ok := validateRepoRoot(recorded, true); ok {
						return root, true
					}
					// Redact to the base name. Unlike the explicit tier's value, which
					// the caller just typed, this path was written into an artifact file
					// (possibly on another machine) and can embed a username; on the MCP
					// path this sink is bare os.Stderr, which a calling agent captures.
					// Same reduction withLock applies to the lock path (lock.go:69-72).
					_, _ = fmt.Fprintf(diag, "localdebt: manifest repo root %q is no longer a valid repository root (copied or stale artifacts?); skipping local debt persistence\n", filepath.Base(recorded))
					return "", false
				}
			} else {
				_, _ = fmt.Fprintf(diag, "localdebt: review manifest is unreadable (%v); skipping local debt persistence\n", basePathErr(err))
				return "", false
			}
		} else if !os.IsNotExist(statErr) {
			_, _ = fmt.Fprintf(diag, "localdebt: review manifest cannot be inspected (%v); skipping local debt persistence\n", basePathErr(statErr))
			return "", false
		}
		// A missing manifest, and a manifest with an empty root, fall through to
		// tier 3: nothing was claimed, so nothing is stale.
	}

	// Tier 3: the CWD's REPO ROOT — the same marker walk the store's readers run
	// (cli/debt.go debtStoreDir → debtRepoRoot), through the same exported helper so
	// there is no second copy of the rule to drift.
	//
	// Returning the literal "." here is what split the two halves: DefaultDir(".")
	// resolves against the process CWD, so a reconcile from a subdirectory wrote
	// <cwd>/.atcr/debt while every reader walked up and read <repo-root>/.atcr/debt.
	// The run's whole backlog was invisible, and the stray <cwd>/.atcr became a
	// repo-root marker for every later walk from that subtree.
	//
	// With NO marker anywhere up the tree the answer is still the bare ".", byte-for-
	// byte the pre-manifest behavior — that is what keeps the existing suite (whose
	// isolate() helper chdirs into a bare temp dir with no .git) reading the store it
	// just wrote.
	if opts.AllowCWD {
		if cwd, err := os.Getwd(); err == nil {
			if root, found := FindRepoRoot(cwd); found {
				return root, true
			}
		}
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
// existing directory carrying a repo-root marker. The marker pair reuses the
// repo-root definition already established at cli/root.go rather than inventing a
// second one.
//
// The marker test itself lives in HasRepoRootMarker, which the store's readers
// (cli/debt.go's debtRepoRoot walk) call as well: one predicate, so writer and
// reader cannot disagree about which directory is the repo root.
//
// requireGit narrows that pair to .git alone, and exists for exactly one caller: the
// manifest tier, whose root was recorded by a MACHINE rather than named by a person
// (TD internal/localdebt/root.go:133). Every atcr run creates .atcr/ in whatever
// directory it works in, so under the union an artifacts tree vouches for itself;
// .git is the marker atcr never writes, which is what makes it evidence. It is
// deliberately NOT the default — the reader walk and the CWD tier keep the union, or
// a repo whose only marker is .atcr would split writer from reader all over again.
func validateRepoRoot(path string, requireGit bool) (string, bool) {
	abs, ok := existingDir(path)
	if !ok {
		return "", false
	}
	if requireGit {
		if hasGitMarker(abs) {
			return abs, true
		}
		return "", false
	}
	if HasRepoRootMarker(abs) {
		return abs, true
	}
	return "", false
}

// HasRepoRootMarker reports whether dir carries a repository-root marker. It is
// the ONE definition of that question for the local debt store: the writer
// re-validates a manifest-recorded root through it (validateRepoRoot) and the
// CLI readers walk up the tree with it (cli/debt.go debtRepoRoot). Two
// definitions is how a symlinked .git came to validate for the writer while
// being invisible to the reader, sending `debt add` and a reconcile to different
// stores in the same checkout.
//
// The two markers are checked differently, and the asymmetry is deliberate. A
// .git entry counts whether it is a directory or a FILE, because a linked
// worktree and a submodule both record their root with a .git file — rejecting
// those would refuse to persist in exactly the setups where a developer is most
// likely to be running from somewhere other than the main checkout. An .atcr
// marker must be a DIRECTORY, because that is the only form atcr itself ever
// creates; accepting a stray .atcr file would let an arbitrary directory pass as
// a repository root and weaken the stale-claim re-validation this design rests on.
//
// Both use Lstat, so a SYMLINK is not a marker in either form: a link pointing at
// an arbitrary directory must not pass as a repository root, and git never
// creates one. That matches cli/root.go's hardening for the shared walk.
// FindRepoRoot walks up from start (inclusive) and returns the first directory
// carrying a repo-root marker. found == false means there is no marker anywhere up
// the tree, which every caller treats as "keep the pre-walk behavior" rather than as
// an error.
//
// It is exported because the store's WRITER (ResolveStoreRoot's CWD tier) and its
// READERS (cli/debt.go debtRepoRoot) must answer "which directory is the repo root"
// identically. They already share the marker predicate; sharing the walk too is what
// closes the remaining gap — a second copy of the loop is how the two halves came to
// disagree in the first place (TD internal/localdebt/root.go:89).
func FindRepoRoot(start string) (string, bool) {
	for dir := filepath.Clean(start); ; {
		if HasRepoRootMarker(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func HasRepoRootMarker(dir string) bool {
	if hasGitMarker(dir) {
		return true
	}
	if info, err := os.Lstat(filepath.Join(dir, ".atcr")); err == nil && info.IsDir() {
		return true
	}
	return false
}

// hasGitMarker reports whether dir carries the .git half of the marker pair, with
// HasRepoRootMarker's exact rules: a directory OR a regular file (a linked worktree
// and a submodule both record their root with a .git FILE), never a symlink.
//
// It is the whole marker test for a machine-recorded root (validateRepoRoot's
// requireGit), and half of it everywhere else — one implementation, so the strict
// and permissive forms cannot drift on what counts as .git.
func hasGitMarker(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}
