package personas

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/repository"
)

// canonicalRepo is the upstream target every community submission forks and opens
// its PR against. It is a fixed constant — never user-supplied — so it can never
// carry an attacker-influenced repo argument into a gh invocation.
const canonicalRepo = "samestrin/atcr"

// PRRequest carries the structured inputs for opening the submission PR. Fields
// are typed (never raw shell strings) so a caller cannot assemble an ad-hoc,
// injectable gh argument list (AC 02-03 Security).
type PRRequest struct {
	Repo   string // canonical upstream target, e.g. "samestrin/atcr"
	Head   string // "<forkOwner>:<branch>" the PR is opened from
	Branch string // head branch name on the fork
	Title  string
	Body   string // source-persona attribution; the PR author is the submitter identity
}

// GitHubSubmitter is the injectable seam over every gh/git interaction used by
// `atcr personas submit`. The default implementation (from NewGitHubSubmitter)
// shells out via gh.ExecContext and git; tests substitute a stub recording call
// order and returning canned results, with zero real gh process or network calls
// (AC 02-03). It is defined independently of internal/ghaction.Client (the fixed-
// bot flow), so a change to one integration point cannot alter the other.
type GitHubSubmitter interface {
	// CheckPrecondition verifies gh is on PATH and authenticated before any
	// fork/branch/PR work (AC 02-01).
	CheckPrecondition(ctx context.Context) error
	// Fork forks repo under the invoking user; an already-existing fork is a
	// non-fatal reuse (AC 02-02 Scenario 2).
	Fork(ctx context.Context, repo string) error
	// PushBranch copies the persona unit named name (resolved under personasDir)
	// into a working tree, commits it on branch, and pushes to the user's fork,
	// returning the "<forkOwner>:<branch>" head reference for the PR.
	PushBranch(ctx context.Context, branch, personasDir, name string) (head string, err error)
	// CreatePR opens the pull request and returns its URL; an already-existing PR
	// for the branch yields that PR's URL rather than an error (AC 02-02 Edge 1).
	CreatePR(ctx context.Context, req PRRequest) (url string, err error)
}

// Submit sequences the GitHub side of `personas submit` behind gh: precondition
// check → fork → branch/push → PR-create, each exactly once, short-circuiting on
// the first failure with a distinct, actionable message (AC 02-02). It performs no
// local gate work (SubmitGate's job) and writes no `submitted` marker (Phase 3).
// The returned string is the PR URL.
func Submit(ctx context.Context, gh GitHubSubmitter, personasDir, name string) (string, error) {
	// Defense in depth: never trust the caller's gate. Re-validate the name
	// before it can reach a branch ref, a file path, or any gh argument.
	if err := validatePersonaName(name); err != nil {
		return "", err
	}
	if err := gh.CheckPrecondition(ctx); err != nil {
		return "", err
	}
	// Confirm the fixed target reference is well-formed before shelling out.
	if _, err := repository.Parse(canonicalRepo); err != nil {
		return "", fmt.Errorf("invalid canonical repo %q: %w", canonicalRepo, err)
	}
	if err := gh.Fork(ctx, canonicalRepo); err != nil {
		return "", fmt.Errorf("failed to fork %s: %w", canonicalRepo, err)
	}
	branch := submissionBranch(name)
	head, err := gh.PushBranch(ctx, branch, personasDir, name)
	if err != nil {
		return "", fmt.Errorf("failed to push branch to fork: %w", err)
	}
	url, err := gh.CreatePR(ctx, PRRequest{
		Repo:   canonicalRepo,
		Head:   head,
		Branch: branch,
		Title:  fmt.Sprintf("Submit community persona: %s", name),
		Body:   submissionPRBody(name),
	})
	if err != nil {
		return "", fmt.Errorf("branch pushed to fork, but PR creation failed: %w; retry with 'gh pr create' or re-run 'atcr personas submit %s'", err, name)
	}
	if strings.TrimSpace(url) == "" {
		return "", fmt.Errorf("PR reported created but gh returned no URL; verify at https://github.com/%s/pulls", canonicalRepo)
	}
	return url, nil
}

// submissionBranch derives a ref-safe head branch from a (possibly namespaced)
// persona name, collapsing '/' so the result is a single segment under
// persona-submit/.
func submissionBranch(name string) string {
	return "persona-submit/" + strings.ReplaceAll(name, "/", "-")
}

// submissionPRBody builds the PR description. The submitter identity is the PR
// author (resolved by the authenticated gh session); the body adds the source
// persona name so downstream curation has attribution without this command owning
// the `submitted` status field (Phase 3).
func submissionPRBody(name string) string {
	return fmt.Sprintf("Community persona submission via `atcr personas submit`.\n\n"+
		"- Source persona: `%s`\n"+
		"- Submitted by: the authenticated `gh` user who opened this PR\n\n"+
		"This persona passed its local fixture gate. It lands as an unvetted `submitted` "+
		"entry pending maintainer graduation into the vetted community library.", name)
}

// ghPath and ghAuthStatus are the low-level seams checkGHPrecondition builds on,
// so a unit test can simulate "gh not on PATH" and "not authenticated" without a
// real gh binary (AC 02-01 / AC 02-03). Defaults wrap go-gh.
var (
	ghPath       = gh.Path
	ghAuthStatus = func(ctx context.Context) (string, error) {
		_, stderr, err := gh.ExecContext(ctx, "auth", "status")
		return stderr.String(), err
	}
)

// checkGHPrecondition verifies gh is installed and authenticated before any
// fork/branch/PR work. It never reads, stores, or logs the token — only gh's exit
// code and (credential-redacted) stderr. Error strings are dictated by AC 02-01.
func checkGHPrecondition(ctx context.Context) error {
	if _, err := ghPath(); err != nil {
		return fmt.Errorf("gh CLI not found on PATH; install it from https://cli.github.com")
	}
	stderr, err := ghAuthStatus(ctx)
	if err != nil {
		return fmt.Errorf("gh auth check failed: %s", strings.TrimSpace(stderr))
	}
	return nil
}

// forkAlreadyExists reports whether gh's fork stderr indicates the user already
// has a fork — the expected, non-fatal reuse case (AC 02-02 Scenario 2).
func forkAlreadyExists(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "already exists")
}

// existingPRURL extracts the URL gh prints when a pull request already exists for
// the pushed branch (AC 02-02 Edge Case 1: re-submission). It returns "" when the
// stderr is any other failure.
func existingPRURL(stderr string) string {
	if !strings.Contains(strings.ToLower(stderr), "already exists") {
		return ""
	}
	for _, f := range strings.Fields(stderr) {
		if strings.HasPrefix(f, "https://") && strings.Contains(f, "/pull/") {
			return f
		}
	}
	return ""
}

// NewGitHubSubmitter returns the production seam implementation backed by gh and
// git. cmd/atcr wires it into the personasGitHub package var; tests replace that
// var with a stub.
func NewGitHubSubmitter() GitHubSubmitter { return ghSubmitter{} }

// ghSubmitter is the default GitHubSubmitter: it shells out to the real gh binary
// (fork, auth, pr-create) and git (branch/push) under the invoking user's own
// credentials. It is stateless; each call is self-contained.
type ghSubmitter struct{}

func (ghSubmitter) CheckPrecondition(ctx context.Context) error { return checkGHPrecondition(ctx) }

func (ghSubmitter) Fork(ctx context.Context, repo string) error {
	_, stderr, err := gh.ExecContext(ctx, "repo", "fork", repo, "--remote=false", "--clone=false")
	if err != nil {
		if forkAlreadyExists(stderr.String()) {
			return nil // reusing an existing fork is expected
		}
		return ghError(err, stderr.String())
	}
	return nil
}

func (ghSubmitter) PushBranch(ctx context.Context, branch, personasDir, name string) (string, error) {
	owner, err := currentGHUser(ctx)
	if err != nil {
		return "", err
	}
	workDir, err := os.MkdirTemp("", "atcr-submit-*")
	if err != nil {
		return "", fmt.Errorf("creating work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	forkURL := fmt.Sprintf("https://github.com/%s/atcr.git", owner)
	if err := runGit(ctx, "", "clone", "--depth", "1", forkURL, workDir); err != nil {
		return "", err
	}
	if err := runGit(ctx, workDir, "checkout", "-b", branch); err != nil {
		return "", err
	}
	if err := copyPersonaUnit(personasDir, name, workDir); err != nil {
		return "", err
	}
	if err := runGit(ctx, workDir, "add", "-A"); err != nil {
		return "", err
	}
	if err := runGit(ctx, workDir, "commit", "-m", "Submit community persona: "+name); err != nil {
		return "", err
	}
	// --force-with-lease (not --force): a re-submission updates the branch but
	// refuses to clobber commits the user pushed to their own PR branch since.
	if err := runGit(ctx, workDir, "push", "--force-with-lease", "origin", branch); err != nil {
		return "", err
	}
	return owner + ":" + branch, nil
}

func (ghSubmitter) CreatePR(ctx context.Context, req PRRequest) (string, error) {
	stdout, stderr, err := gh.ExecContext(ctx, "pr", "create",
		"--repo", req.Repo,
		"--head", req.Head,
		"--title", req.Title,
		"--body", req.Body)
	if err != nil {
		if url := existingPRURL(stderr.String()); url != "" {
			return url, nil // a PR already exists for this branch (re-submission)
		}
		return "", ghError(err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// currentGHUser resolves the authenticated user's login so the fork clone URL and
// PR head can be constructed. It never logs the raw gh output.
func currentGHUser(ctx context.Context) (string, error) {
	stdout, stderr, err := gh.ExecContext(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("could not resolve gh user: %w", ghError(err, stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ghError builds a single error from a gh/git failure that preserves BOTH the
// underlying error (so a context timeout is detectable with errors.Is and never
// surfaces as a blank message when the killed process left no stderr) and the
// captured stderr text (redacted by gh itself). stderr is elided when empty.
func ghError(err error, stderr string) error {
	if s := strings.TrimSpace(stderr); s != "" {
		return fmt.Errorf("%w: %s", err, s)
	}
	return err
}

// copyPersonaUnit resolves the persona unit under personasDir and writes it into
// the fork working tree at personas/community/<name>.yaml via the symlink-refusing
// atomic writer.
func copyPersonaUnit(personasDir, name, workDir string) error {
	src, err := personaPath(personasDir, name)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading persona %q: %w", name, err)
	}
	dest := filepath.Join(workDir, "personas", "community", filepath.FromSlash(name)+".yaml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("preparing submission tree: %w", err)
	}
	return writeFileAtomic(dest, data)
}

// runGit runs a git subcommand under a bounded context, capturing stderr for a
// step-specific error message. dir is the working tree ("" for repo-less clone).
func runGit(ctx context.Context, dir string, args ...string) error {
	c := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		c.Dir = dir
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), ghError(err, stderr.String()))
	}
	return nil
}

// SubmitGate runs the local pre-submission gate for name and reports whether the
// persona may proceed to a fork+PR. It is the reused, network-free front half of
// `atcr personas submit`: it validates the persona name with the same guard the
// install/remove paths use (validatePersonaName), then runs the fixture gate via
// runner. A nil return means the persona cleared the gate; a non-nil error means
// submission is blocked and no GitHub interaction (fork/branch/PR) must occur.
//
// The three blocking conditions, in order:
//   - invalid name — rejected before any resolution, closing the path-traversal
//     class already fixed for install/remove;
//   - no fixture (outcome.HasFixture is false) — a submission-specific, blocking
//     error (deliberately distinct from `personas test`'s softer, non-blocking
//     "No fixture defined" wording), since an unvetted prompt with no fixture
//     cannot clear the gate;
//   - fixture not fully passing (outcome.Passed != outcome.Total).
//
// A zero-case fixture (HasFixture true, Total 0) satisfies Passed == Total
// (0 == 0), so the sole failing predicate does not trip and the persona proceeds
// — an explicit choice: the gate blocks only a genuine fixture failure, never an
// empty fixture that already rendered.
func SubmitGate(name string, runner FixtureRunner) error {
	if err := validatePersonaName(name); err != nil {
		return err
	}
	outcome, err := TestPersona(name, runner)
	if err != nil {
		return err
	}
	if !outcome.HasFixture {
		return fmt.Errorf("cannot submit %q: no fixture defined — add a fixture before submitting", name)
	}
	if outcome.Passed != outcome.Total {
		return fmt.Errorf("cannot submit %q: fixture failed (%d/%d cases passed)", name, outcome.Passed, outcome.Total)
	}
	return nil
}
