package gitrange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/gitexec"
)

// Detection mode strings recorded in Resolution.DetectionMode and written to
// manifest.json so downstream packages read provenance from disk.
const (
	ModeExplicit    = "explicit"
	ModeMergeCommit = "merge_commit"
	ModeAuto        = "auto"
)

// Sentinel errors let callers discriminate resolution failures
// programmatically; the wrapped message carries the user-facing guidance.
var (
	// ErrEmptyRange is returned when base and head enclose zero commits. It is
	// raised before any provider call so a no-op review never clears a CI gate.
	ErrEmptyRange = errors.New("empty range")
	// ErrShallowClone is returned on a shallow repository. The resolver never
	// auto-unshallows — that is a destructive network op the user must opt into.
	ErrShallowClone = errors.New("shallow clone")
	// ErrNoDefaultBranch is returned when every default-branch probe misses.
	ErrNoDefaultBranch = errors.New("no default branch")
	// ErrInvalidRef is returned when a user-supplied ref does not resolve.
	ErrInvalidRef = errors.New("invalid git ref")
	// ErrNotARepository is returned when repoDir is not inside a git work tree.
	ErrNotARepository = errors.New("not a git repository")
)

// Options carries the user's range intent. Exactly one branch of the decision
// tree fires based on which fields are set; the CLI layer guarantees --head
// never appears without --base and neither combines with --merge-commit. A
// base without a head defaults the head to HEAD.
type Options struct {
	Base        string
	Head        string
	MergeCommit string
}

// Resolution is the concrete base..head pair every downstream package consumes.
// It is written verbatim into manifest.json. CommitCount is always ≥1: a zero
// count is reported as ErrEmptyRange before a Resolution is ever constructed.
type Resolution struct {
	Base          string    `json:"base"`
	Head          string    `json:"head"`
	DetectionMode string    `json:"detection_mode"`
	DefaultBranch string    `json:"default_branch,omitempty"`
	CommitCount   int       `json:"commit_count"`
	Shallow       bool      `json:"shallow"`
	ResolvedAt    time.Time `json:"resolved_at"`
}

// defaultBranchProbes is the ordered fallback list walked when origin/HEAD is
// not configured. Ordered by observed frequency in real repos.
var defaultBranchProbes = []string{"origin/main", "origin/master", "main", "master"}

// Resolve walks the decision tree (explicit → merge-commit → auto) and returns
// the resolved range. Every git invocation is argv-only (no shell) and bound to
// ctx so a cancelled run leaves no orphaned git process.
func Resolve(ctx context.Context, repoDir string, opts Options) (*Resolution, error) {
	g := &gitRunner{ctx: ctx, dir: repoDir}

	// Shallow guard first: the probe doubles as the not-a-repository check.
	shallow, err := g.isShallow()
	if err != nil {
		return nil, err
	}
	if shallow {
		return nil, fmt.Errorf("%w detected: cannot review incomplete history; run `git fetch --unshallow` to enable full history access", ErrShallowClone)
	}

	var base, head, mode, defaultBranch string

	switch {
	case opts.Base != "" || opts.Head != "":
		if opts.Base == "" {
			return nil, fmt.Errorf("%w: --head requires --base", ErrInvalidRef)
		}
		headRef := opts.Head
		if headRef == "" {
			headRef = "HEAD" // base-only: the natural CI-gate invocation
		}
		if base, err = g.resolveRef(opts.Base); err != nil {
			return nil, err
		}
		if head, err = g.resolveRef(headRef); err != nil {
			return nil, err
		}
		mode = ModeExplicit

	case opts.MergeCommit != "":
		// base is SHA^ — the merge commit's FIRST parent (the tip of the branch
		// the PR merged into). A merge commit's second parent is the merged-in
		// branch; reviewing the first-parent delta matches how a merge-gate reads
		// "what landed on the target branch". Non-merge or octopus commits where
		// SHA^ is not the intended base are out of scope for this mode.
		if head, err = g.resolveRef(opts.MergeCommit); err != nil {
			return nil, err
		}
		if base, err = g.resolveRef(opts.MergeCommit + "^"); err != nil {
			return nil, err
		}
		mode = ModeMergeCommit

	default:
		if defaultBranch, err = g.detectDefaultBranch(); err != nil {
			return nil, err
		}
		if base, err = g.mergeBase(defaultBranch, "HEAD"); err != nil {
			return nil, err
		}
		if head, err = g.resolveRef("HEAD"); err != nil {
			return nil, err
		}
		mode = ModeAuto
	}

	if base == head {
		return nil, fmt.Errorf("%w: base and head are the same commit (%s)", ErrEmptyRange, shortSHA(base))
	}
	count, err := g.commitCount(base, head)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: no commits between base %s and head %s", ErrEmptyRange, shortSHA(base), shortSHA(head))
	}

	return &Resolution{
		Base:          base,
		Head:          head,
		DetectionMode: mode,
		DefaultBranch: defaultBranch,
		CommitCount:   count,
		Shallow:       false,
		ResolvedAt:    time.Now().UTC(),
	}, nil
}

// gitRunner executes git argv against a fixed directory and context.
type gitRunner struct {
	ctx context.Context
	dir string
}

// run executes `git -C <dir> args...` and returns trimmed stdout. A non-zero
// exit surfaces stderr; the not-a-repository case is mapped to ErrNotARepository
// so callers get the AC-mandated message. LC_ALL=C pins git's stderr to English
// so the not-a-repo detection survives a non-English locale, and a cancelled
// context surfaces as ctx.Err() rather than a generic git failure.
func (g *gitRunner) run(args ...string) (string, error) {
	full := append([]string{"-C", g.dir}, args...)
	cmd := gitexec.CommandContextFn(g.ctx, full...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if ctxErr := g.ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("git %s cancelled: %w", strings.Join(args, " "), ctxErr)
		}
		stderr := strings.TrimSpace(errOut.String())
		if strings.Contains(stderr, "not a git repository") {
			return "", fmt.Errorf("%w (or any of the parent directories): .git", ErrNotARepository)
		}
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(out.String()), nil
}

// CurrentBranch returns the checked-out branch name in repoDir, or "" on a
// detached HEAD or any error — callers derive a review id and fall back to
// "detached" when this is empty, so a failure here is non-fatal.
func CurrentBranch(ctx context.Context, repoDir string) string {
	g := &gitRunner{ctx: ctx, dir: repoDir}
	// --quiet + exit code: on detached HEAD `symbolic-ref` exits nonzero and run
	// returns an error, which we map to "".
	out, err := g.run("symbolic-ref", "--short", "--quiet", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// isShallow reports whether the repository is a shallow clone. It is the first
// probe and therefore also surfaces ErrNotARepository for non-repo dirs.
func (g *gitRunner) isShallow() (bool, error) {
	out, err := g.run("rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	return out == "true", nil
}

// resolveRef verifies ref points at a commit and returns its full SHA. A miss is
// reported as ErrInvalidRef with the AC-mandated message.
func (g *gitRunner) resolveRef(ref string) (string, error) {
	// --end-of-options prevents a ref beginning with '-' from being parsed as a
	// git flag (option injection); ^{commit} forces commit-ish resolution.
	out, err := g.run("rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil || out == "" {
		return "", fmt.Errorf("%w: '%s' does not resolve to a commit", ErrInvalidRef, ref)
	}
	return out, nil
}

// detectDefaultBranch probes origin/HEAD via symbolic-ref, then walks the static
// fallback list. Returns the matching ref name (e.g. "origin/main").
func (g *gitRunner) detectDefaultBranch() (string, error) {
	if out, err := g.run("symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		// out is like "refs/remotes/origin/main"; trim to "origin/main".
		return strings.TrimPrefix(out, "refs/remotes/"), nil
	}
	for _, ref := range defaultBranchProbes {
		if out, err := g.run("rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}"); err == nil && out != "" {
			return ref, nil
		}
	}
	return "", fmt.Errorf("%w: could not detect default branch: tried origin/HEAD, origin/main, origin/master, local main, local master", ErrNoDefaultBranch)
}

// mergeBase returns the best common ancestor of a and b.
func (g *gitRunner) mergeBase(a, b string) (string, error) {
	out, err := g.run("merge-base", a, b)
	if err != nil {
		return "", fmt.Errorf("resolving merge-base of %s and %s: %w", a, b, err)
	}
	// Guard against an exit-0/empty-stdout edge (unrelated histories): an empty
	// base would let rev-list silently fabricate a bogus range.
	if out == "" {
		return "", fmt.Errorf("resolving merge-base of %s and %s: no common ancestor", a, b)
	}
	return out, nil
}

// commitCount returns the number of commits in base..head.
func (g *gitRunner) commitCount(base, head string) (int, error) {
	out, err := g.run("rev-list", "--count", base+".."+head)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("parsing commit count %q: %w", out, err)
	}
	return n, nil
}

// shortSHA abbreviates a full SHA for human-facing error messages.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
