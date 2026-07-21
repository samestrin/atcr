package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/autofix"
	"github.com/samestrin/atcr/internal/ghaction"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/sandbox"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/spf13/cobra"
)

// autofix.go is the CLI-level gate and orchestration for the opt-in `--auto-fix`
// flow (Sprint 17.0, Story 6). It wires the already-built local write-path
// (internal/autofix), the post-apply validation gate (internal/verify), and the
// GitHub Git Data API client (internal/ghaction) into a single, fail-closed
// entry point behind `atcr review --auto-fix`. It builds on those packages'
// public APIs only and never reaches into their internals. The one hard safety
// invariant it encodes: no GitHub-mutating call is reachable until local
// validation has passed (runAutoFix below).

// defaultValidationTimeout bounds one post-apply validation run when the operator
// configures no auto_fix.validate_timeout. ~2 min matches the sprint-design
// Performance budget; a defined default here means a Phase-5 caller never passes
// a zero timeout into RunConfiguredValidation (which would be an immediate false
// TimedOut — TD-008).
const defaultValidationTimeout = 2 * time.Minute

// addAutoFixFlags registers the opt-in `--auto-fix` flag (off by default) plus
// the GitHub credential flags the gate resolves (mirroring `atcr github`'s
// --repo/--token/--api-url). Registering unset string flags adds no behavior to
// the default path — `--auto-fix` absent leaves every existing review invocation
// byte-identical (AC 06-01).
func addAutoFixFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("auto-fix", false,
		"opt-in: after review, apply each finding's fix, validate locally, and open a GitHub PR only if validation passes; "+
			"refuses without a validation command + apply target + GitHub token/repo (token needs contents:write and pull_requests:write) "+
			"+ a preflighted [sandbox] block (validation is sandboxed by default; pass --no-sandbox to run it unsandboxed on the host). "+
			"Sandboxed validation mounts the tree read-only, so a validate_command that writes under the project dir (e.g. npm/cargo builds) hits EROFS, fails closed, and reverts the fix — effectively Go-only today unless you --no-sandbox. "+
			"Only findings at or above --fail-on are fixed, so the default fail_on: HIGH applies HIGH+ fixes only — lower --fail-on to include MEDIUM/LOW. "+
			"Each run opens a NEW pull request on a fresh timestamped branch (create-per-run; it does not update a prior auto-fix PR). "+
			"Requires local HEAD to be pushed and to equal the PR base branch (the base commit and commit parent are resolved from local HEAD). "+
			"When --auto-fix succeeds, the --fail-on exit gate is bypassed because the intent is to remediate, not to fail CI. "+
			"If branch/commit creation succeeds and a later step fails, the remote branch (and possibly commit) is left behind and must be deleted manually.")
	cmd.Flags().Bool("no-sandbox", false,
		"DANGER: disables container isolation for --auto-fix validation. By default --auto-fix runs the "+
			"validation command (e.g. `go build`/`npm test`) inside a sandbox container so LLM-generated code "+
			"cannot touch the host. Passing --no-sandbox runs that LLM-generated validation directly on the host "+
			"machine with your privileges, with no isolation — only use it where Docker is unavailable and you "+
			"accept the risk. Meaningless without --auto-fix. Prints a security warning to stderr on every run.")
	cmd.Flags().String("repo", "", "owner/name target repository for --auto-fix (default: $GITHUB_REPOSITORY)")
	cmd.Flags().String("token", "", "GitHub token with contents:write + pull_requests:write for --auto-fix (default: $GITHUB_TOKEN)")
	cmd.Flags().String("api-url", "", "GitHub REST API base for --auto-fix (default: $GITHUB_API_URL or https://api.github.com)")
}

// resolveAutoFixSandboxFn is the sandbox resolver validateAutoFixBackend calls,
// indirected through a package var (like resolveHeadSHAFn) so a test can count
// invocations — the load-bearing proof that --no-sandbox skips resolution
// entirely rather than calling a resolver that no-ops (AC 03-02). Production
// points at the real verify.ResolveAutoFixSandbox.
var resolveAutoFixSandboxFn = verify.ResolveAutoFixSandbox

// warnNoSandbox writes the --no-sandbox security warning to out. It is
// deliberately NOT memoized — no sync.Once, no package-level "seen" bool, no
// env/state gate — which is the exact opposite of the read-once ATCR_TELEMETRY
// notice (main.go telemetryEnabledFromEnv). That precedent warns once by design;
// this one must fire on EVERY --no-sandbox invocation so an operator can never
// lose sight of the fact that untrusted, LLM-generated validation code is running
// directly on the host with no container isolation (AC 03-03). It writes to
// stderr (via the caller's cmd.ErrOrStderr()) so it never corrupts structured
// stdout payloads.
func warnNoSandbox(out io.Writer) {
	_, _ = fmt.Fprintln(out, "WARNING: --no-sandbox is set — auto-fix validation is running WITHOUT container isolation. "+
		"Untrusted, LLM-generated validation commands execute directly on this host with your privileges. "+
		"Only pass --no-sandbox where Docker is unavailable and you accept that risk.")
}

// autoFixBackend holds the fully-resolved backend the gate validated, so
// runAutoFix consumes it without re-resolving any piece (AC 06-03).
type autoFixBackend struct {
	applyTarget     string // absolute working-tree path the patch is applied to
	validateArgv    []string
	validateTimeout time.Duration
	owner           string
	repo            string
	token           string
	apiURL          string
	// sandboxBackend is the resolved-and-preflighted container backend the
	// validation step runs inside (the gate's fourth checked piece, AC 02-03).
	// When non-nil, validation is sandboxed. A nil here is NOT sufficient to run
	// on the host: host execution additionally requires noSandbox (below) to be
	// true, so a nil backend without the explicit opt-out is a fail-closed refusal
	// rather than a silent unsandboxed fallback (epic 32.2 Task 1). Under the
	// default sandboxed-on posture an unresolvable sandbox is a hard gate refusal,
	// so in production a nil here only ever accompanies noSandbox==true.
	sandboxBackend sandbox.Backend
	// noSandbox records that the operator explicitly passed --no-sandbox: it is the
	// ONLY thing that authorizes runAutoFix to run validation unsandboxed on the
	// host. It is set true solely on the warned opt-out path in
	// validateAutoFixBackend; a directly-constructed backend (a future caller or a
	// test) that leaves it false and supplies no sandboxBackend is refused at the
	// dispatch, decoupling the fail-closed guarantee from the gate hard-coding
	// enabled=true (epic 32.2 Task 1, replacing AC 01-03's nil→host baseline).
	noSandbox bool
}

// resolveValidateTimeout picks the effective validation timeout: an operator-
// configured auto_fix.validate_timeout wins; otherwise defaultValidationTimeout.
// The value is validated at config load (AutoFixConfig.Validate), so a parse
// error here is defensive.
func resolveValidateTimeout(af *registry.AutoFixConfig) (time.Duration, error) {
	if af != nil {
		if t := strings.TrimSpace(af.ValidateTimeout); t != "" {
			d, err := time.ParseDuration(t)
			if err != nil {
				return 0, fmt.Errorf("auto_fix.validate_timeout %q is not a valid duration: %w", af.ValidateTimeout, err)
			}
			if d <= 0 {
				return 0, fmt.Errorf("auto_fix.validate_timeout must be positive, got %q", af.ValidateTimeout)
			}
			return d, nil
		}
	}
	return defaultValidationTimeout, nil
}

// validateAutoFixBackend is the single, all-or-nothing gate for `--auto-fix`. It
// resolves and shape-checks, in one pass, the three required backend pieces —
// (1) an apply target (an existing directory), (2) a validation command (Story
// 2's config or Go default), and (3) GitHub token + owner/name repo (flags or
// env) — plus the validation timeout. It performs only local checks (env/flag
// reads, config-shape parsing, one os.Stat); it never makes a network call or
// executes the validation command.
//
// Precondition it CANNOT verify (TD-001): the token must carry contents:write +
// pull_requests:write. Confirming that needs a live API call, which this
// local-only gate deliberately avoids, so the scope is a documented precondition
// (here and on the --auto-fix flag) rather than a runtime smoke-check; an
// under-scoped token surfaces only as a GitHub 403 at the first mutating call.
//
// Any missing/malformed piece makes the whole
// run refuse: every failure is collected and returned as one usage error (exit
// 2) naming every missing piece, so a half-configured backend fails fast before
// any file is touched. The one exception is the sandbox check (4): it shells out
// to docker and spawns a throwaway container, so it runs only when the cheap
// local checks (1-3) passed — a run already refusing does not pay that cost.
// On success it returns the fully-resolved backend and nil.
func validateAutoFixBackend(cmd *cobra.Command, proj *registry.ProjectConfig, repoRoot string) (autoFixBackend, error) {
	var be autoFixBackend
	var missing []string

	var af *registry.AutoFixConfig
	if proj != nil {
		af = proj.AutoFix
	}

	// (1) Apply target — must be an existing directory that resolves to the repo
	// root itself. The resolved path is made absolute so it is independent of the
	// caller's CWD (runReview passes repoRoot="."; TD-019). A subdirectory target
	// is refused: the patch is applied and validated under applyTarget but the
	// commit sends repo-root-relative paths, so a non-root target would certify a
	// file GitHub never receives (autofix.go:274 commit-path mismatch). Until path
	// translation is implemented, only the repo root is accepted.
	applyTarget := "."
	if af != nil && strings.TrimSpace(af.ApplyTarget) != "" {
		applyTarget = strings.TrimSpace(af.ApplyTarget)
	}
	absTarget := applyTarget
	if !filepath.IsAbs(absTarget) {
		absTarget = filepath.Join(repoRoot, applyTarget)
	}
	absTarget, absErr := filepath.Abs(absTarget)
	absRoot, rootErr := filepath.Abs(repoRoot)
	info, statErr := os.Stat(absTarget)
	switch {
	case absErr != nil || rootErr != nil:
		missing = append(missing, fmt.Sprintf(
			"apply target: cannot resolve %q to an absolute path (set auto_fix.apply_target in .atcr/config.yaml)", applyTarget))
	case statErr != nil || !info.IsDir():
		missing = append(missing, fmt.Sprintf(
			"apply target: working tree path %q not found or not a directory (set auto_fix.apply_target in .atcr/config.yaml)", applyTarget))
	case absTarget != absRoot:
		missing = append(missing, fmt.Sprintf(
			"apply target: %q must resolve to the repository root; a subdirectory apply_target is not supported because fixes are committed with repo-root-relative paths (set auto_fix.apply_target to the repo root)", applyTarget))
	default:
		be.applyTarget = absTarget
	}

	// (2) Validation command — configured argv wins, else Story 2's Go default;
	// a project with neither is a hard refusal (never validate silently).
	var configured []string
	if af != nil {
		configured = af.ValidateCommand
	}
	if argv, err := verify.ResolveValidateCommand(configured, repoRoot); err != nil {
		missing = append(missing,
			"validation command: no command configured and no default for this project type (set auto_fix.validate_command in .atcr/config.yaml)")
	} else {
		be.validateArgv = argv
	}
	if to, err := resolveValidateTimeout(af); err != nil {
		missing = append(missing, err.Error())
	} else {
		be.validateTimeout = to
	}

	// (3) GitHub repo + token — flags win over env (envOr), shape-only checks.
	repoFlag, _ := cmd.Flags().GetString("repo")
	if owner, repo, err := parseRepo(envOr(repoFlag, "GITHUB_REPOSITORY")); err != nil {
		missing = append(missing, err.Error())
	} else {
		be.owner, be.repo = owner, repo
	}
	tokenFlag, _ := cmd.Flags().GetString("token")
	if token := envOr(tokenFlag, "GITHUB_TOKEN"); token == "" {
		missing = append(missing, "a GitHub token is required (pass --token or set GITHUB_TOKEN)")
	} else {
		be.token = token
	}
	apiURLFlag, _ := cmd.Flags().GetString("api-url")
	be.apiURL = envOr(apiURLFlag, "GITHUB_API_URL")
	// Shape-check the api-url now (local, no network) so a malformed or insecure
	// value is a fail-closed refusal before any file is touched, rather than
	// surfacing lazily at the first HTTP call in ghaction.Client.baseURL (TD-014).
	if err := ghaction.ValidateAPIURL(be.apiURL); err != nil {
		missing = append(missing, err.Error())
	}

	// (4) Sandbox backend — the validation step's container isolation, resolved and
	// preflighted here so an unconfigured/unreachable sandbox refuses at the gate
	// alongside the other pieces rather than surfacing mid-run. Under the default
	// sandboxed-on posture `enabled` is true, so a nil/failing sandbox is a hard
	// refusal that joins the same `missing` slice (append-or-assign, no early
	// return) — mirroring resolveExec's ResolveExecBackend call but inverting the
	// polarity (verify.go:54). Preflight shells out to the local `docker` binary
	// only (no GitHub/HTTP call of the gate's own), so the "no GitHub network before
	// the gate passes" invariant holds; note that `docker` itself honors DOCKER_HOST
	// and may contact a remote/DinD daemon, so "local" here means "no direct network
	// I/O by this gate", not "docker never talks to a remote daemon". The
	// --no-sandbox opt-out (Story 3) short-circuits this whole check before resolution.
	var sandboxConfig *registry.SandboxConfig
	if proj != nil {
		sandboxConfig = proj.Sandbox
	}
	// The --no-sandbox opt-out short-circuits the entire sandbox check BEFORE any
	// resolution: the resolver is never called (not even to no-op on a disabled
	// flag), so no Docker/`sandbox:` config is required and be.sandboxBackend stays
	// nil — validation will run directly on the host (AC 03-02). The bypass is
	// scoped to sandbox resolution only; the other three checks above already ran
	// and stay enforced. It is gated strictly on true (not "flag was set"), so
	// --no-sandbox=false is identical to the flag being absent.
	noSandbox, _ := cmd.Flags().GetBool("no-sandbox")
	if noSandbox {
		// Record the explicit opt-out on the backend: it is the ONLY thing that
		// authorizes runAutoFix to run validation unsandboxed on the host — a nil
		// sandboxBackend alone is refused at the dispatch (epic 32.2 Task 1). This is
		// the single setter of be.noSandbox.
		be.noSandbox = true
		// Fire the security warning the moment the bypass is chosen — unconditional
		// and non-memoized (every invocation), so the operator can never lose sight
		// of running untrusted LLM-generated validation on the host (AC 03-03).
		warnNoSandbox(cmd.ErrOrStderr())
	} else if len(missing) == 0 {
		// Resolve/preflight the sandbox only when the cheap local checks above
		// passed: resolution shells out to docker and spawns a throwaway
		// container, so a run already refusing on a missing apply-target /
		// validate-command / repo / token must not pay that cost before exit 2.
		// cmd.Context() is nil on a bare command that was never executed (cobra
		// does not default it); production always reaches here via ExecuteContext,
		// so this guard only backstops direct callers and tests. Preflight (a local
		// `docker` subprocess) needs a real ctx.
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if backend, err := resolveAutoFixSandboxFn(ctx, true, sandboxConfig); err != nil {
			missing = append(missing, fmt.Sprintf("sandbox: %s", err.Error()))
		} else {
			be.sandboxBackend = backend
		}
	}

	if len(missing) > 0 {
		return autoFixBackend{}, usageError(fmt.Errorf("--auto-fix cannot run: %s", strings.Join(missing, "; ")))
	}
	return be, nil
}

// autoFixGitHub is the subset of ghaction.Client runAutoFix drives, extracted as
// an interface so the orchestration's sequencing guarantee can be tested with a
// call-recording fake (and the Phase 6 httptest stub) without a live client.
// *ghaction.Client satisfies it.
type autoFixGitHub interface {
	CreateBranch(ctx context.Context, owner, repo, branch, sha string) error
	CreateCommit(ctx context.Context, owner, repo string, req ghaction.CommitRequest) (string, error)
	FindOpenPullRequest(ctx context.Context, owner, repo, branch string) (int, bool, error)
	CreatePullRequest(ctx context.Context, owner, repo string, req ghaction.PullRequestRequest) (int, error)
	UpdatePullRequest(ctx context.Context, owner, repo string, prNumber int, req ghaction.PullRequestRequest) error
}

// autoFixRun is the fully-resolved unit of work runAutoFix executes: the gated
// backend, the fix entries to apply, and the commit/PR metadata. Branch, BaseSHA,
// Title, Body, and Message are caller-supplied (atcr-generated, never diagnostics-
// sourced — TD-013) so no credential leaks into the commit message.
type autoFixRun struct {
	Backend autoFixBackend
	Entries []payload.FileEntry
	// BaseSHA is the parent commit the fix commit is built on (the branch is
	// created at it); Base is the target branch the pull request merges INTO
	// (e.g. "main"). They are distinct: a commit parent vs. a PR base.
	BaseSHA string
	Base    string
	Branch  string
	Title   string
	Body    string
	Message string
}

// remoteLeftoverNotice appends a cleanup hint to errors returned after
// CreateBranch has succeeded. At that point the remote branch exists (and a
// commit may have been pushed), but local RevertPatch cannot undo remote state,
// so the operator must delete the branch manually if they do not want it left
// behind.
func remoteLeftoverNotice(branch string) string {
	return fmt.Sprintf(" (remote branch %q was created and may contain a commit; delete it manually to avoid leaving it behind)", branch)
}

// validationOutputTail renders a bounded tail of a failed validation run's
// captured output for the failure error. On the sandbox path stdout+stderr have
// already been collapsed into Stdout (documented stream-collapse), so this is the
// only diagnostic the operator gets about WHY the fix was rejected; on the host
// path it is the command's stdout. The capture is already capped (1 MiB) at the
// verify layer; the tail bounds what the error message carries.
func validationOutputTail(stdout string) string {
	const maxTail = 4000
	t := strings.TrimSpace(stdout)
	if t == "" {
		return ""
	}
	if len(t) > maxTail {
		t = t[len(t)-maxTail:]
	}
	return "\nvalidation output (tail):\n" + t
}

// runAutoFix executes the gated pipeline: apply → validate → revert-or-continue →
// branch/commit/PR. Its single invariant: no GitHub-mutating call is reachable
// until local validation has PASSED. On an apply failure or a validation failure
// (non-zero exit, timeout, or a command that cannot start) it reverts every
// touched file and returns without making any GitHub call. Only after a clean
// validation does it clean up the backups and open (or update) the PR.
func runAutoFix(ctx context.Context, out io.Writer, gh autoFixGitHub, run autoFixRun) error {
	be := run.Backend

	bm, applyErr := autofix.ApplyPatch(be.applyTarget, run.Entries)
	if applyErr != nil {
		// Some files may have landed before the failure; restore the tree and
		// never touch GitHub.
		if rerr := autofix.RevertPatch(ctx, bm); rerr != nil {
			return fmt.Errorf("auto-fix: applying patch failed AND revert failed: %w; original apply error: %v", rerr, applyErr)
		}
		return fmt.Errorf("auto-fix: applying patch (working tree reverted, no GitHub changes made): %w", applyErr)
	}

	// Route validation through the resolved sandbox backend when one is present
	// (the default sandboxed-on posture), else run it directly on the host ONLY when
	// the operator explicitly opted out via --no-sandbox (be.noSandbox). A nil
	// backend WITHOUT that opt-out fails closed rather than falling back to the host:
	// this supersedes AC 01-03's original nil→host zero-behavior baseline, decoupling
	// the fail-closed guarantee from the gate hard-coding enabled=true so a
	// gate-bypassing caller cannot inherit a silent unsandboxed run (epic 32.2 Task 1;
	// AC 03-02 still governs the opt-out path). Both non-refusing paths return the
	// identical verify.ValidationResult contract, so the three post-call branches
	// below (verr != nil / !res.Passed() / success) are consumed unchanged regardless
	// of which path produced the result.
	//
	// Accepted taxonomy divergence for exits 125-127: on the host path a validation
	// command exiting 125/126/127 is an ordinary non-zero program exit and lands in
	// the !res.Passed() branch as "validation failed (exit N)". On the sandbox path
	// docker reserves 125-127 for its own launch faults (daemon/exec/permission), so
	// the backend reclassifies exit >=125 as a runtime fault (docker.go:258) ->
	// StartError -> the verr != nil "cannot run validation" branch instead (AC 01-02
	// documents 125-127 -> StartError as intentional). Same command intent, different
	// branch and operator wording — deliberate, since docker cannot tell a program's
	// 125-127 apart from its own, not a divergence in the shared contract.
	var res verify.ValidationResult
	var verr error
	switch {
	case be.sandboxBackend != nil:
		res, verr = verify.RunSandboxedValidation(ctx, be.sandboxBackend, be.validateArgv, be.applyTarget, be.validateTimeout)
	case be.noSandbox:
		// Host path — reachable ONLY via the explicit --no-sandbox opt-out, which is
		// the sole setter of be.noSandbox (validateAutoFixBackend). AC 01-03/03-02.
		res, verr = verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)
	default:
		// Fail closed: a nil sandbox backend WITHOUT the --no-sandbox opt-out must
		// never silently run unsandboxed on the host. This decouples the fail-closed
		// guarantee from the gate hard-coding enabled=true — a future/test caller that
		// constructs autoFixBackend directly and bypasses the gate is refused here, not
		// silently run on the host (epic 32.2 Task 1). The patch has already been
		// applied above, so revert it and make no GitHub call, exactly like a
		// validation failure.
		if rerr := autofix.RevertPatch(ctx, bm); rerr != nil {
			return fmt.Errorf("auto-fix: refusing unsandboxed host validation without --no-sandbox AND revert failed: %w", rerr)
		}
		return fmt.Errorf("auto-fix: refusing to run validation unsandboxed on the host without an explicit --no-sandbox opt-out (no sandbox backend was resolved); working tree reverted, no GitHub changes made")
	}
	if verr != nil {
		// Could not even validate: fail closed exactly like a validation failure.
		if rerr := autofix.RevertPatch(ctx, bm); rerr != nil {
			return fmt.Errorf("auto-fix: cannot validate AND revert failed: %w; validation error: %v", rerr, verr)
		}
		return fmt.Errorf("auto-fix: cannot run validation (working tree reverted, no GitHub changes made): %w", verr)
	}
	if !res.Passed() {
		if rerr := autofix.RevertPatch(ctx, bm); rerr != nil {
			return fmt.Errorf("auto-fix: validation failed AND revert failed: %w", rerr)
		}
		if res.TimedOut {
			// A timeout leaves ExitCode at 0 (both translation paths), so the
			// generic exit-code wording would read "failed (exit 0)" — name the
			// timeout explicitly instead.
			return fmt.Errorf("auto-fix: local validation timed out after %s; working tree reverted, no GitHub changes made%s",
				be.validateTimeout, validationOutputTail(res.Stdout))
		}
		return fmt.Errorf("auto-fix: local validation failed (exit %d); working tree reverted, no GitHub changes made%s",
			res.ExitCode, validationOutputTail(res.Stdout))
	}

	// Validation passed — the tree is trustworthy. Drop the now-redundant backups
	// (best-effort) and proceed to the remote mutation, which is unreachable above.
	autofix.CleanupBackups(ctx, bm)

	// A pull request needs a non-empty base branch to merge into. Guard it BEFORE
	// creating the branch/commit so a missing base can never leave an orphan
	// branch and commit behind on the remote (GitHub 422s an empty base).
	if strings.TrimSpace(run.Base) == "" {
		return fmt.Errorf("auto-fix: no base branch resolved for the pull request; no GitHub changes made")
	}

	files, err := commitFilesFrom(be.applyTarget, run.Entries)
	if err != nil {
		return fmt.Errorf("auto-fix: preparing commit files: %w", err)
	}

	if err := gh.CreateBranch(ctx, be.owner, be.repo, run.Branch, run.BaseSHA); err != nil {
		return fmt.Errorf("auto-fix: creating branch %q: %w", run.Branch, err)
	}
	if _, err := gh.CreateCommit(ctx, be.owner, be.repo, ghaction.CommitRequest{
		Branch:    run.Branch,
		Message:   run.Message,
		ParentSHA: run.BaseSHA,
		Files:     files,
	}); err != nil {
		return fmt.Errorf("auto-fix: committing fix to %q: %w%s", run.Branch, err, remoteLeftoverNotice(run.Branch))
	}

	prReq := ghaction.PullRequestRequest{Head: run.Branch, Base: run.Base, Title: run.Title, Body: run.Body}
	num, found, err := gh.FindOpenPullRequest(ctx, be.owner, be.repo, run.Branch)
	if err != nil {
		return fmt.Errorf("auto-fix: checking for an existing pull request on branch %q: %w%s", run.Branch, err, remoteLeftoverNotice(run.Branch))
	}
	if found {
		if err := gh.UpdatePullRequest(ctx, be.owner, be.repo, num, prReq); err != nil {
			return fmt.Errorf("auto-fix: updating pull request #%d: %w%s", num, err, remoteLeftoverNotice(run.Branch))
		}
		_, _ = fmt.Fprintf(out, "auto-fix: updated pull request #%d on %s/%s\n", num, be.owner, be.repo)
		return nil
	}
	created, err := gh.CreatePullRequest(ctx, be.owner, be.repo, prReq)
	if err != nil {
		return fmt.Errorf("auto-fix: opening pull request for branch %q: %w%s", run.Branch, err, remoteLeftoverNotice(run.Branch))
	}
	_, _ = fmt.Fprintf(out, "auto-fix: opened pull request #%d on %s/%s\n", created, be.owner, be.repo)
	return nil
}

// commitFilesFrom reads the post-validation content of each applied entry into a
// CommitFile. A file that no longer exists after apply is a deletion (null-SHA
// tree entry); otherwise its current on-disk bytes are the commit content.
func commitFilesFrom(root string, entries []payload.FileEntry) ([]ghaction.CommitFile, error) {
	files := make([]ghaction.CommitFile, 0, len(entries))
	for _, e := range entries {
		abs := filepath.Join(root, e.Path)
		content, err := os.ReadFile(abs)
		if err != nil {
			if os.IsNotExist(err) {
				files = append(files, ghaction.CommitFile{Path: e.Path, Deleted: true})
				continue
			}
			return nil, fmt.Errorf("reading applied file %q: %w", e.Path, err)
		}
		files = append(files, ghaction.CommitFile{Path: e.Path, Content: string(content)})
	}
	return files, nil
}

// resolveHeadSHAFn resolves the commit SHA the auto-fix commit is parented on.
// A package var so a test can substitute it (the real thing shells out to git,
// which is not hermetic in a bare temp dir). In production it reads HEAD.
var resolveHeadSHAFn = func(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %q: %w", dir, err)
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("git rev-parse HEAD in %q returned no SHA", dir)
	}
	return sha, nil
}

// newAutoFixGitHubFn builds the GitHub client orchestrateAutoFix hands to
// runAutoFix. A package var so a test can substitute a call-recording fake (the
// real client talks to the GitHub API, which is not hermetic). In production it
// returns the live client.
var newAutoFixGitHubFn = func(apiURL, token string) autoFixGitHub {
	return &ghaction.Client{APIURL: apiURL, Token: token}
}

// orchestrateAutoFix is the thin live bridge wired into `atcr review --auto-fix`:
// it turns the reconciled findings into apply entries, resolves the base commit
// and a fresh branch name, and hands everything to runAutoFix with a real
// GitHub client. The heavy end-to-end proof (sequencing against stubs) lives in
// Phase 6; this adapter only assembles the run. It is guarded entirely by the
// --auto-fix flag, so the default review path is byte-identical (AC 06-01).
func orchestrateAutoFix(ctx context.Context, out io.Writer, be autoFixBackend, reviewDir, threshold, baseBranch string) error {
	if strings.TrimSpace(baseBranch) == "" {
		return fmt.Errorf("auto-fix: could not resolve a base branch to open the pull request against")
	}
	findings, err := reconcile.ReadReconciledFindings(reviewDir)
	if err != nil {
		return fmt.Errorf("auto-fix: reading reconciled findings: %w", err)
	}
	entries, skipped, err := selectAutoFixEntries(findings, threshold)
	if err != nil {
		return fmt.Errorf("auto-fix: selecting fixes: %w", err)
	}
	if skipped > 0 {
		_, _ = fmt.Fprintf(out, "auto-fix: %d additional fix(es) for already-patched files were skipped this run\n", skipped)
	}
	if len(entries) == 0 {
		// Distinguish "nothing carried a fix" from "every fix was filtered out by
		// the --fail-on threshold" (the default fail_on: HIGH silently drops
		// MEDIUM/LOW fixes — TD-016), so a stock-init operator is not left
		// wondering why a MEDIUM-only run applied nothing.
		if threshold != "" && countFixBelowThreshold(findings, threshold) > 0 {
			_, _ = fmt.Fprintf(out, "auto-fix: every fix was below the --fail-on %s threshold; nothing to apply (lower --fail-on / auto_fix threshold to include them).\n", threshold)
		} else {
			_, _ = fmt.Fprintln(out, "auto-fix: no reconciled finding carried an applicable fix; nothing to apply.")
		}
		return nil
	}
	// Base commit AND commit parent both come from local HEAD. Precondition
	// (TD-021): local HEAD must be pushed and equal to the PR base branch. If HEAD
	// is unpushed, CreateBranch/CreateCommit fail with GitHub's 422/404; if HEAD is
	// a feature branch ahead of the base, the opened PR silently carries the
	// intervening commits. Resolving/verifying against the remote base is a
	// deliberate future enhancement, not done here.
	baseSHA, err := resolveHeadSHAFn(ctx, be.applyTarget)
	if err != nil {
		return fmt.Errorf("auto-fix: resolving base commit (ensure local HEAD is pushed and equals the PR base %q): %w", baseBranch, err)
	}
	// Create-per-run: a fresh timestamped branch each run — the user-approved
	// Phase-5 MVP (TD-015). runAutoFix's create-vs-update path is reachable only
	// when a caller reuses a branch name; this live adapter intentionally does not,
	// so each run opens a NEW PR rather than updating a stable one. Deterministic
	// one-PR-per-target naming would be a separate feature, not a TD fix.
	branch := fmt.Sprintf("atcr/auto-fix/%s", time.Now().UTC().Format("20060102-150405"))
	client := newAutoFixGitHubFn(be.apiURL, be.token)
	return runAutoFix(ctx, out, client, autoFixRun{
		Backend: be,
		Entries: entries,
		BaseSHA: baseSHA,
		Base:    baseBranch,
		Branch:  branch,
		Title:   "atcr: automated fixes",
		Body:    "Automated fixes applied by `atcr --auto-fix` after local validation passed.",
		Message: "fix: apply atcr auto-fix (validated locally)",
	})
}

// selectAutoFixEntries is the thin bridge from reconciled findings to apply
// entries: each finding carrying a non-empty Fix (a unified diff) is parsed into
// FileEntry values via payload.BuildEntriesFromDiff. When threshold is set, only
// findings at or above it are included; otherwise every finding with a Fix is.
// A finding whose Fix is not a parseable diff is skipped, not fatal — one bad
// suggestion must not abort the whole auto-fix run. The returned skipped count is
// the number of duplicate-path fixes dropped after the first entry for that path,
// surfaced in orchestrateAutoFix so the operator knows a second fix was deferred.
func selectAutoFixEntries(findings []reconcile.JSONFinding, threshold string) ([]payload.FileEntry, int, error) {
	var entries []payload.FileEntry
	skipped := 0
	// One entry per target path per run: two findings whose fixes touch the same
	// file would otherwise produce two entries for one path, and ApplyPatch would
	// re-run BackupToDotBak on the already-patched file — clobbering the original
	// backup so RevertPatch could no longer restore the pre-patch state. Keeping
	// only the first fix per path preserves the revert-safety invariant (one
	// backup per file). A second fix for the same file is left for a later run.
	seen := make(map[string]bool)
	for _, f := range findings {
		if strings.TrimSpace(f.Fix) == "" {
			continue
		}
		if threshold != "" && !reconcile.AtOrAbove(f.Severity, threshold) {
			continue
		}
		fes, err := payload.BuildEntriesFromDiff(f.Fix)
		if err != nil {
			continue // an unparseable fix is skipped, not fatal
		}
		for _, e := range fes {
			if seen[e.Path] {
				skipped++
				continue // already have a fix for this file this run
			}
			seen[e.Path] = true
			entries = append(entries, e)
		}
	}
	return entries, skipped, nil
}

// countFixBelowThreshold reports how many findings carry a non-empty Fix but sit
// below threshold, so orchestrateAutoFix can tell "nothing carried a fix" apart
// from "every fix was filtered out by --fail-on" in its empty-selection message.
func countFixBelowThreshold(findings []reconcile.JSONFinding, threshold string) int {
	if threshold == "" {
		return 0
	}
	n := 0
	for _, f := range findings {
		if strings.TrimSpace(f.Fix) != "" && !reconcile.AtOrAbove(f.Severity, threshold) {
			n++
		}
	}
	return n
}
