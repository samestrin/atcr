# Security Architecture — workspace integrity & indirect sandbox escape

`atcr`'s `--auto-fix` flow applies **untrusted, LLM-generated** patches to your
working tree and runs host-side `git` commands against your repository. That
makes it a target for a class of attack that does not need to break the
validation sandbox at all. This page documents the workspace-integrity controls
that close it: the pathguard protected-path blocklist, the `--allow-config-edits`
escape valve, the `internal/gitexec` host-git hardening, and the non-blocking
executable-bit / build-script review warnings.

For the container-isolation side of `--auto-fix` (the sandboxed validation step),
see [Auto-fix sandboxed validation](auto-fix.md) and
[What the sandbox guarantees](execution.md#what-the-sandbox-guarantees). This page
covers the **host** surface those documents deliberately leave out: what happens
to your repository *after* a sandboxed review ends.

## The threat: Host Trust Transposition

Recent disclosures (Pillar Security; CVE-2026-48124) against AI coding agents
show that the sandbox is often not where the attack lands. The agent runs its
review inside a confined container, but a prompt-injected or hallucinated patch
writes a **configuration artifact into the workspace** — `.git/config` setting
`core.pager` or `core.hooksPath`, a `.githooks/pre-commit` script, a
`.github/workflows/*.yml` job, or `.vscode/settings.json`. Nothing executes yet,
so the sandboxed validation step passes cleanly. Then the review ends, and a
host-side tool — the Git CLI, a CI runner, or an editor — reads that modified
file and executes its payload with the **full privileges of the developer**,
entirely outside the sandbox that just "contained" the run.

This is **Host Trust Transposition**: trust placed in the sandbox is transposed
onto host-side tools that read workspace files after the fact. The sandbox never
broke; the escape was *indirect*.

`atcr` addresses this with two host-side controls (a write-path gate and a
git-subprocess hardening layer) plus one visibility control (review warnings).

## Pathguard: the protected-path write gate

`internal/security` exports `IsProtectedPath(path string) bool`, a
**denylist path matcher** with no side effects beyond the symlink resolution it
needs. It is the shape-analogue of `internal/validation`'s `FilePath` denylist,
but scoped to **repo-relative host-execution/config paths** rather than absolute
system directories.

`internal/autofix`'s `applyOne` calls it as a **fail-closed gate** at the single
host-repo write choke point — immediately after the existing path-containment
check and before any delete, modify, or create branch. A patch entry whose path
is protected is refused with a wrapped `security.ErrProtectedPath`
(`errors.Is`-testable), and — because the apply loop isolates per-entry errors —
one refused entry never blocks the sibling entries in the same patch.

By default, `--auto-fix` refuses to create or modify any file under:

- `.git/` and `.githooks/` — Git config, hooks, and the pager/hookspath vectors
- `.github/workflows/`, `.gitlab-ci.yml`, and other CI definitions
- `.vscode/` and `.idea/` — editor-executed settings and task definitions
- `.env*` — secrets and direnv-style auto-executed environment files
- `.planning/` — `atcr`'s own planning state
- `.atcr` — `atcr`'s own configuration

Matching is **boundary-safe**: it compares whole path segments (never a bare
`strings.HasPrefix`), so a lookalike such as `.gitignore` or `.githubx/` is *not*
blocked, while `./x`, bare `x`, `../`-traversal, and symlink-traversal forms all
normalize to the same decision. The gate checks the diff-declared, repo-relative
path, so an attacker cannot slip past it by dressing the same target in a
different path spelling.

## `--allow-config-edits`: the operator escape valve

Some operators legitimately need `--auto-fix` to touch a protected path — for
example refactoring a CI workflow or editor config as an intentional change.
`--allow-config-edits` (a bool flag on `atcr review`, off by default) is the
**only** supported way to do that. When set, it bypasses the pathguard gate so a
protected-path patch entry is applied instead of refused.

It follows the `--no-sandbox` precedent exactly:

- **Off by default.** With the flag absent, behavior is byte-identical to the
  fail-closed gate — protected paths are refused.
- **Warns on every run.** Every `--allow-config-edits` invocation prints a
  security warning to stderr — not only the first time, and not gated behind any
  "seen once" state. If you script it into a loop, expect the warning each run;
  that is deliberate, so the weakened gate can never go unnoticed.
- **Config has no equivalent.** There is no `.atcr/config.yaml` field that
  disables the pathguard gate; the bypass is a deliberate, per-invocation
  command-line decision only.

Only pass `--allow-config-edits` when you are intentionally reviewing an
`--auto-fix` change to a build, CI, or editor config **and accept that the patch
can land a trigger that executes on your host** the next time git, a CI runner, or
an editor reads it. It is meaningless without `--auto-fix`.

## Host-git hardening: `internal/gitexec`

Pathguard stops a *poisoned* config from being written by `--auto-fix`. But a
`.git/config` planted by an earlier run (or by any other means) is only dangerous
when a later `git` subprocess *reads* it — and `atcr` runs host-side `git` from
several places, not just the auto-fix pass. `internal/gitexec` is the single
shared wrapper around `exec.Command` / `exec.CommandContext` that **every** host
`git` invocation routes through. It unconditionally injects, additively over the
process environment:

- **`GIT_CONFIG_NOSYSTEM=1`** — ignore the system-wide `git` config
  (`/etc/gitconfig`), so a compromised system config cannot inject behavior.
- **`GIT_CONFIG_GLOBAL=/dev/null`** — ignore the user's global `git` config
  (`~/.gitconfig`), so per-user config (aliases, pagers, hooks) cannot hijack an
  `atcr` git call.
- **`--no-ext-diff`** (on the diff-family invocations) — never shell out to an
  external diff driver a poisoned config might name.

Each call site keeps its own pre-existing environment customizations
(`LC_ALL=C` / `LANG=C`, credential-helper overrides) as additive appends — the
hardening layers *over* them, it does not replace them. All six production call
sites — `cmd/atcr/autofix.go`, `internal/fanout/review.go`,
`internal/gitrange/resolver.go`, `internal/payload/diff.go`,
`internal/personas/submit.go`, and `internal/stream/fileindex.go` — construct
their commands exclusively through `gitexec`. A whole-tree regression test fails
the build if a bare `exec.Command("git", ...)` reappears anywhere outside the
wrapper, so the guarantee cannot silently erode. (The two intentionally
out-of-scope files — `internal/verify/localvalidate.go` and
`internal/sandbox/docker.go` — are excluded by that test with an inline
rationale.)

## Non-blocking review warnings: executable-bit & build-script changes

Not every risky change is a protected-path write. A patch that flips a file's
**executable bit**, or that touches a **build script** (`Makefile`, `*.sh`,
`package.json`, `Dockerfile`, `Jenkinsfile`, or a CI config outside `.github/`
such as `.gitlab-ci.yml` or `.circleci/`), is legitimate often enough that
blocking it would be wrong — but it deserves a second look.

`internal/security`'s `FlagsForReview(path string, oldMode, newMode int) (bool, string)`
is a **non-blocking** check: it never refuses a patch and never errors. It reports
whether a successfully-applied entry changed the executable bit
(`oldMode&0111 != newMode&0111`) or landed on a build-script path, along with a
human-readable reason. `atcr` already never auto-merges an `--auto-fix` change —
it always opens a pull request and leaves the merge to a human — so human review
is already the terminal gate. `FlagsForReview` makes elevated-risk patches
**visible in that existing review** rather than adding a new gate: when any
applied entry is flagged, `--auto-fix` appends a `## Review Warnings` section to
the generated PR body naming the flagged path(s) and reason. A patch with no
flagged paths leaves the PR body byte-identical to today.

## Summary

| Control | Layer | Default | Bypass |
| --- | --- | --- | --- |
| Pathguard protected-path gate | `--auto-fix` write path | Fail-closed (refuse) | `--allow-config-edits` (warns every run) |
| `internal/gitexec` env hardening | Every host `git` subprocess | Always on | None |
| `FlagsForReview` review warnings | Generated PR body | Advisory only | N/A (never blocks) |

Together these ensure an LLM-generated patch cannot silently plant a host-executed
trigger, cannot hijack an `atcr` git call through a poisoned system/global/repo
config, and cannot slip an executable-bit or build-script change past human review
unremarked.
