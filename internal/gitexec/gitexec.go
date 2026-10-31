// Package gitexec is the single, hardened constructor for every host git
// subprocess ATCR runs. It closes the SYSTEM and USER-GLOBAL half of the "host git
// subprocess hijack" pillar of the Indirect Sandbox Escape threat: an earlier
// --auto-fix pass (or any other write into the working tree) can leave behind a
// poisoned machine-wide /etc/gitconfig or user-global ~/.gitconfig carrying a
// malicious core.pager, diff.external, alias, or credential.helper entry, which the
// next host git invocation would silently execute with full developer privileges,
// outside the sandbox.
//
// Every git subprocess constructed through CommandFn/CommandContextFn injects
// GIT_CONFIG_NOSYSTEM=1 (ignore /etc/gitconfig) and GIT_CONFIG_GLOBAL=/dev/null
// (ignore the user-global config) additively over the inherited environment, so
// no system/global config entry can hijack the child. diff-family subcommands
// additionally pass --no-ext-diff at their call site to neutralize a poisoned
// diff.external (that flag is diff-command-specific and is added to argv by the
// caller, not baked in here).
//
// Scope boundary — the repo-LOCAL .git/config is NOT neutralized here: git always
// reads it, and GIT_CONFIG_NOSYSTEM/GLOBAL do not disable it. That vector is closed
// upstream by internal/security.IsProtectedPath refusing --auto-fix writes into
// .git/, so a poisoned local config is never written in the first place. That
// protection is FORFEITED when the operator passes --allow-config-edits (which
// disables the pathguard gate): after such a run a poisoned local .git/config
// (core.pager, alias.*, core.hooksPath — none of which --no-ext-diff stops) can
// hijack a subsequent gitexec command. Callers honoring --allow-config-edits must
// account for that residual risk.
//
// Invariant (AC4): no bare exec.Command("git", ...) / exec.CommandContext(ctx,
// "git", ...) call site may remain outside this package. CommandFn and
// CommandContextFn are exported as package-level vars — not funcs — so tests can
// substitute a call-recording fake without spawning real git, mirroring the
// resolveHeadSHAFn/removeFn/writeFileAtomicFn testability pattern already used in
// internal/autofix.
package gitexec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// CommandFn builds a hardened `git <arg...>` command. It is a package-level var so
// a test can replace it with a fake; restore the original in a defer.
var CommandFn = func(arg ...string) *exec.Cmd {
	return hardenEnv(exec.Command("git", arg...))
}

// CommandContextFn builds a hardened, context-bound `git <arg...>` command. The
// context bounds and cancels the child exactly as exec.CommandContext does.
var CommandContextFn = func(ctx context.Context, arg ...string) *exec.Cmd {
	return hardenEnv(exec.CommandContext(ctx, "git", arg...))
}

// FixtureCommandContext builds a hardened `git <arg...>` command for a TEST
// FIXTURE repository rooted at dir. It is for tests only: production git calls go
// through CommandFn/CommandContextFn.
//
// Git exports GIT_DIR into a hook's environment, and GIT_DIR beats `git -C`, so a
// fixture command run under a hook retargets at the surrounding repository (Epic
// 45.0: moved HEAD, stray commits, flipped core.bare). This constructor pins the
// command to dir with PinRepo and sets GIT_CEILING_DIRECTORIES to dir's parent as
// a defence for a caller that drops GIT_DIR and relies on cwd discovery; while
// GIT_DIR is pinned (as here) git does no discovery, so the ceiling is inert. The child runs in dir, so relative
// paths in arg resolve inside the fixture; a relative dir is resolved against the
// current directory first. It panics if that resolution fails, since a fixture
// that cannot be located must not silently run against the wrong directory.
//
// It calls exec.CommandContext directly rather than CommandContextFn, because
// tests replace that var with fakes and a fixture must always run real git.
//
// It also refuses (panics) a dir inside an atcr checkout, and gives the child the
// author and committer FixtureName <FixtureEmail>, which a caller may override by
// appending its own GIT_AUTHOR_*/GIT_COMMITTER_* to cmd.Env (keep the email under
// .invalid; a test enforces it). On 2026-10-30 a fixture identity, t@t.t, reached
// atcr's public history and showed a stranger's GitHub account as a contributor.
func FixtureCommandContext(ctx context.Context, dir string, arg ...string) *exec.Cmd {
	abs, err := filepath.Abs(dir)
	if err != nil {
		panic(fmt.Sprintf("gitexec: FixtureCommandContext: resolving fixture dir %q: %v", dir, err))
	}
	dir = abs
	if root, ok := atcrCheckout(dir); ok {
		panic(fmt.Sprintf("gitexec: FixtureCommandContext: fixture dir %q is inside the atcr checkout %q; "+
			"build fixtures under t.TempDir()", dir, root))
	}
	cmd := exec.CommandContext(ctx, "git", arg...)
	cmd.Dir = dir
	PinRepo(cmd, dir)
	cmd.Env = append(cmd.Env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir),
		"GIT_AUTHOR_NAME="+FixtureName, "GIT_AUTHOR_EMAIL="+FixtureEmail,
		"GIT_COMMITTER_NAME="+FixtureName, "GIT_COMMITTER_EMAIL="+FixtureEmail)
	return hardenEnv(cmd)
}

// FixtureName and FixtureEmail are the identity of every fixture commit. The
// email is under the reserved .invalid TLD (RFC 2606), so no account can own it.
const (
	FixtureName  = "atcr test"
	FixtureEmail = "test@example.invalid"
)

// atcrCheckout reports the nearest ancestor of dir (dir included) whose go.mod
// declares atcr's own module, i.e. a real atcr checkout or worktree.
func atcrCheckout(dir string) (string, bool) {
	for d := dir; ; d = filepath.Dir(d) {
		if b, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil {
			first, _, _ := strings.Cut(string(b), "\n")
			if strings.TrimSpace(first) == "module github.com/samestrin/atcr" {
				return d, true
			}
		}
		if filepath.Dir(d) == d {
			return "", false
		}
	}
}

// PinRepo points cmd at the repository whose working tree is dir and returns
// cmd. It sets GIT_DIR=<dir>/.git and GIT_WORK_TREE=<dir>, which win over any
// inherited value, and drops the other repo-locating vars a hook exports (the
// list .githooks/pre-push unsets): an inherited GIT_INDEX_FILE or
// GIT_OBJECT_DIRECTORY would still send the index or objects elsewhere. Use it
// for any git command on a repository ATCR itself creates, where `-C dir` alone
// loses to an inherited GIT_DIR. A relative dir is resolved against the current
// directory first.
func PinRepo(cmd *exec.Cmd, dir string) *exec.Cmd {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	env := slices.DeleteFunc(cmd.Environ(), func(kv string) bool {
		for _, k := range inheritedRepoVars {
			if strings.HasPrefix(kv, k+"=") {
				return true
			}
		}
		return false
	})
	cmd.Env = append(env,
		"GIT_DIR="+filepath.Join(dir, ".git"),
		"GIT_WORK_TREE="+dir,
	)
	return cmd
}

// inheritedRepoVars are the repo-locating vars, besides GIT_DIR and GIT_WORK_TREE,
// that PinRepo removes from the inherited environment.
var inheritedRepoVars = []string{
	"GIT_INDEX_FILE",
	"GIT_PREFIX",
	"GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
}

// hardenEnv appends GIT_CONFIG_NOSYSTEM=1 and GIT_CONFIG_GLOBAL=/dev/null onto the
// command's inherited environment (cmd.Environ(), never a nil slice, so the child
// still inherits PATH, HOME, etc.) and returns cmd so a caller may chain further
// additive customizations (e.g. cmd.Env = append(cmd.Environ(), "LC_ALL=C")).
// Because the two hardening vars are appended last, they win over any inherited
// GIT_CONFIG_* and survive a subsequent additive append.
func hardenEnv(cmd *exec.Cmd) *exec.Cmd {
	cmd.Env = append(cmd.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	return cmd
}
