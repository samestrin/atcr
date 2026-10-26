// Package webhook is the core of the `atcr webhook` daemon: it turns a trigger
// from a chat or ticketing service into a review of a GitHub pull request.
package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/gitexec"
)

const (
	// remoteName is the remote of an allowlisted checkout the PR head is fetched from.
	remoteName = "origin"
	// branchPrefix namespaces every branch a job creates in an allowlisted
	// checkout, so cleanup can never delete a branch the operator owns.
	branchPrefix = "atcr-webhook/"
	// cleanupTimeout bounds the git commands that remove a job's worktree and
	// branch. They run after the job's own context may already be done.
	cleanupTimeout = 30 * time.Second
)

var (
	// ErrRepoNotAllowed is returned for a repository that webhook.repos does not list.
	ErrRepoNotAllowed = errors.New("repository is not in webhook.repos")
	// ErrBadPRNumber is returned for a pull request number below 1.
	ErrBadPRNumber = errors.New("pull request number must be positive")
	// ErrBadCheckout is returned when the configured checkout of an allowlisted
	// repository is not an absolute path to a git repository.
	ErrBadCheckout = errors.New("configured checkout is not a git repository")
)

// Checkout is a throwaway worktree holding the head of one pull request.
type Checkout struct {
	// Dir is the worktree's root. It is removed when the job returns.
	Dir string
	// Head is the full SHA of refs/pull/<n>/head as fetched.
	Head string
	// Branch is the short name of the branch the worktree is on, under
	// branchPrefix in the allowlisted checkout. It is deleted when the job returns.
	Branch string
}

// WithPRCheckout fetches refs/pull/<number>/head of repo's allowlisted checkout
// into a new branch, checks that branch out into a worktree under scratch (the
// system temp directory when empty) and runs job in it. The worktree, its
// directory and its branch are removed when job returns, errors or panics, and
// when ctx is cancelled or times out at any step, so a killed job leaves no
// half-written branch or worktree behind. A cleanup failure is joined to the
// returned error.
//
// The operator's checkout is only fetched into: its own branches, index and
// working tree are never touched.
func WithPRCheckout(ctx context.Context, repos map[string]string, repo string, number int, scratch string, job func(context.Context, *Checkout) error) (err error) {
	checkout, ok := repos[repo]
	if !ok {
		return fmt.Errorf("%w: %s", ErrRepoNotAllowed, repo)
	}
	if number < 1 {
		return fmt.Errorf("%w: %d", ErrBadPRNumber, number)
	}
	if !filepath.IsAbs(checkout) {
		return fmt.Errorf("%w: %s (path is not absolute)", ErrBadCheckout, repo)
	}
	if _, gerr := git(ctx, checkout, "rev-parse", "--git-dir"); gerr != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return fmt.Errorf("%w: %s", ErrBadCheckout, repo)
	}
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}

	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	branch := branchPrefix + "pr-" + strconv.Itoa(number) + "-" + suffix
	base, err := os.MkdirTemp(scratch, "atcr-pr-")
	if err != nil {
		return fmt.Errorf("webhook: create worktree directory: %w", err)
	}
	co := &Checkout{Dir: filepath.Join(base, "tree"), Branch: branch}
	defer func() {
		if cerr := removeCheckout(ctx, checkout, base, co); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	ref := "refs/pull/" + strconv.Itoa(number) + "/head"
	if _, err := git(ctx, checkout, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head",
		remoteName, "+"+ref+":refs/heads/"+branch); err != nil {
		return ctxErrOr(ctx, fmt.Errorf("webhook: fetch %s of %s: %w", ref, repo, err))
	}
	head, err := git(ctx, checkout, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return ctxErrOr(ctx, fmt.Errorf("webhook: resolve fetched %s of %s: %w", ref, repo, err))
	}
	co.Head = head
	if _, err := git(ctx, checkout, "worktree", "add", "--quiet", co.Dir, branch); err != nil {
		return ctxErrOr(ctx, fmt.Errorf("webhook: add worktree for %s of %s: %w", ref, repo, err))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return job(ctx, co)
}

// removeCheckout removes co's worktree, the directory base that holds it and
// co's branch. Each step runs even when an earlier one failed, under a fresh
// timeout, because ctx may be the cancelled context that ended the job.
func removeCheckout(ctx context.Context, checkout, base string, co *Checkout) error {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	var errs []error
	if _, err := os.Stat(co.Dir); err == nil {
		// A failure here is covered by RemoveAll plus prune below.
		_, _ = git(cctx, checkout, "worktree", "remove", "--force", co.Dir)
	}
	if err := os.RemoveAll(base); err != nil {
		errs = append(errs, fmt.Errorf("webhook: remove worktree directory: %w", err))
	}
	if _, err := git(cctx, checkout, "worktree", "prune"); err != nil {
		errs = append(errs, fmt.Errorf("webhook: prune worktrees: %w", err))
	}
	// update-ref -d succeeds when the branch was never created.
	if _, err := git(cctx, checkout, "update-ref", "-d", "refs/heads/"+co.Branch); err != nil {
		errs = append(errs, fmt.Errorf("webhook: delete branch %s: %w", co.Branch, err))
	}
	return errors.Join(errs...)
}

// commandContextFn builds every git command a job runs. It is a var so a test
// can cancel a job at a chosen git step.
var commandContextFn = gitexec.CommandContextFn

// git runs `git <arg...>` against the repository whose working tree is dir and
// returns trimmed stdout. The command is pinned to dir, so an inherited GIT_DIR
// cannot retarget it, never prompts for credentials, and runs no hook: a pull
// request can add a hook under a tracked core.hooksPath such as .githooks, and
// `worktree add` would otherwise run its post-checkout with the daemon's rights.
func git(ctx context.Context, dir string, arg ...string) (string, error) {
	cmd := commandContextFn(ctx, append([]string{"-c", "core.hooksPath=/dev/null"}, arg...)...)
	cmd.Dir = dir
	gitexec.PinRepo(cmd, dir)
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ctxErrOr returns ctx's error when ctx is done, since a git command killed by
// its context reports only "signal: killed", and err otherwise.
func ctxErrOr(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

// randomSuffix keeps two jobs on the same pull request from sharing a branch
// or a worktree.
func randomSuffix() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("webhook: random branch suffix: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
