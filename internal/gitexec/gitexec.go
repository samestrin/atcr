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
	"os/exec"
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
