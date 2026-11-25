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

`internal/security` exports `IsProtectedPath(path, root string) bool`, a
**denylist path matcher** with no side effects beyond the symlink resolution it
needs; `root` anchors that symlink-resolution layer. It is the shape-analogue of
`internal/validation`'s `FilePath` denylist, but scoped to **repo-relative
host-execution/config paths** rather than absolute system directories.

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
hardening layers *over* them, it does not replace them. All seven production call
sites — `cli/autofix.go`, `internal/fanout/review.go`,
`internal/gitrange/resolver.go`, `internal/payload/diff.go`,
`internal/personas/submit.go`, `internal/stream/fileindex.go`, and
`internal/tools/snapshot.go` — construct their commands exclusively through
`gitexec`. A whole-tree regression test fails
the build if a bare `exec.Command("git", ...)` reappears anywhere outside the
wrapper, so the guarantee cannot silently erode. (The two intentionally
out-of-scope files — `internal/verify/localvalidate.go` and
`internal/sandbox/docker.go` — are excluded by that test with an inline
rationale.)

## Non-blocking review warnings: build-script changes

Not every risky change is a protected-path write. A patch that touches a
**build script** (`Makefile`, `*.sh`, `package.json`, `Dockerfile`, `Jenkinsfile`,
or a CI config outside `.github/` such as `.gitlab-ci.yml` or `.circleci/`), is
legitimate often enough that blocking it would be wrong — but it deserves a
second look.

`internal/security`'s `FlagsForReview(path string) (bool, string)`
is a **non-blocking** check: it never refuses a patch and never errors. It reports
whether a successfully-applied entry landed on a build-script path, along with a
human-readable reason. It deliberately does **not** flag executable-bit changes:
the apply pipeline writes every file through `atomicfs.WriteFileAtomic` (fixed
`0644`) and the GitHub commit hardcodes blob mode `100644`, so a diff's exec-bit
change never lands in the working tree or the PR — flagging it would warn about a
change that does not happen. `atcr` already never auto-merges an `--auto-fix` change —
it always opens a pull request and leaves the merge to a human — so human review
is already the terminal gate. `FlagsForReview` makes elevated-risk patches
**visible in that existing review** rather than adding a new gate: when any
applied entry is flagged, `--auto-fix` appends a `## Review Warnings` section to
the generated PR body naming the flagged path(s) and reason. A patch with no
flagged paths leaves the PR body byte-identical to today.

## Payload egress: the `secret_guard` scan

A separate surface from the host-integrity controls above: what a review sends
*out*. Before a review prompt leaves for a provider, atcr scans it for
credentials and applies the `secret_guard` policy (`off`, `warn`, `redact` or
`block`; default `redact`, settable in the registry or per project). `block`
aborts the run rather than send a prompt holding a credential — and fails
closed when the scan itself could not be trusted. Warnings and block errors
name the agent, the secret kinds and the files, never the value.

The guard covers the **review prompt and tool-loop results** — the payload
(diff, claim ledger, context definitions, prefetched context) and the
sprint-plan scope constraint sent to a review agent or its fallback, followed
by the tool results and model turns added during the tool loop. The review
prompt is scanned once when it is built; each later request scans the new
messages under the same policy. Under `redact` every request is still sent
redacted, rewritten from each message's first scan. Persona text is not
scanned. Debate seats and the verify skeptic and fix-executor prompts are
scanned too. Debate seats
resolve `secret_guard` the way a review does (the project overrides the
registry); the verify skeptic and fix-executor prompts use the registry's (the
embedded default `redact` when the registry sets none), and the project tier is
not applied to them. Doctor probes are not scanned:
they send only fixed probe text, never repository content. One more input to
verify is covered: the deterministic SAST findings a review saves to
`sast.json` (the scanner messages and rules the Fixer reads through the
reconciled report) pass the same policy before the file is written.
The threat-map mapper prompt (the changed-file list) is scanned too, under the
run's `secret_guard`, project tier included; under `block` a list holding a
credential is not sent, and the failed mapper means the panel reviews every
file.

Before the scan, credential files never enter a payload at all: `.env` and
`.env.*`, `.envrc`, `.netrc`, `.pgpass`, `.git-credentials`, `.npmrc`,
`.pypirc`, `credentials`, SSH private keys and `.pem`/`.key`/`.p12`/`.pfx`/
`.jks`/`.keystore` files are dropped in diff and full-repo mode, even with
`--no-ignore`.

It is one layer of three, and the docs describe them together in
[docs/providers.md](providers.md#privacy-and-redaction-pii--secrets): a
**pre-commit** secret scanner keeps credentials out of the diff at commit time,
atcr's **egress guard** is the last checkpoint before a prompt leaves the
machine, and **presidio** at the proxy masks residual PII that is not a
credential.

## atcr's own state directory: symlinks are refused

atcr writes its own state under fixed repository paths, and a cloned
repository can ship any of them as a symlink. `atcr review` refuses to create
a review directory when `.atcr` or `.atcr/reviews` is a symlink, and
`atcr init` (including `--force` and `--refresh-personas`) refuses when `.atcr`
or `.atcr/personas` is one, so neither writes outside the project. An explicit
`--output-dir` that is a symlink is refused the same way.

## Summary

| Control | Layer | Default | Bypass |
| --- | --- | --- | --- |
| Pathguard protected-path gate | `--auto-fix` write path | Fail-closed (refuse) | `--allow-config-edits` (warns every run) |
| `internal/gitexec` env hardening | Every host `git` subprocess | Always on | None |
| `FlagsForReview` review warnings | Generated PR body | Advisory only | N/A (never blocks) |

Together these ensure an LLM-generated patch cannot silently plant a host-executed
trigger, cannot hijack an `atcr` git call through a poisoned system/global/repo
config, and cannot slip a build-script change past human review
unremarked.

---

## Embedding hooks: the model-invocation observation seam

`atcr` v0.2.0 added `cli.MainWithHooks`, an exported entry point that lets an
embedding binary observe every model invocation. It exists so a wrapper — such
as a private enterprise build — can construct a compliance audit trail without
vendoring the command tree or reaching into `internal/`.

It is worth understanding as a data-egress surface, because that is what it is.

### It is opt-in and off by default

Nothing is observed unless a caller deliberately builds against the `cli`
package, calls `MainWithHooks` instead of `Main`, and registers an observer. The
stock `atcr` binary calls `Main`, registers nothing, and installs no observer —
its behaviour, exit codes, signal handling, and telemetry drain are byte-for-byte
what they were before the seam existed. There is no flag, environment variable,
or config key that can turn observation on for a binary that did not compile it
in.

### What a registered observer receives

Per model invocation: the run id, reviewer persona, and pipeline stage; the model
and provider identity; the exact prompt and response; the full message history
and requested tool calls on multi-turn (tool-enabled) turns; provider-reported
token counts, estimated cost, and sampling parameters; HTTP attempt count,
timing, and any error.

**The payload is not limited to the diff under review.** For tool-enabled agents
the message history contains `role:"tool"` results, which are the raw output of
the agent's tools: whole file contents from anywhere in the repository,
directory listings, grep hits, and — where script execution is enabled — command
stdout that may contain environment-derived values. Treat the whole payload as
source-code-sensitive.

The one thing deliberately scrubbed is credentials embedded in a configured
provider URL: `https://user:pass@host/v1` reaches an observer as
`https://host/v1`, matching what the client does before building a request. API
keys are never part of the payload; they are resolved from the environment at
invocation time and never logged.

### Where the boundary of responsibility sits

The core applies no masking, redaction, or truncation, deliberately — a generic
review tool cannot know which values a given deployment considers secret. Any
consumer that persists, forwards, or logs these fields owns redacting them, the
access controls of wherever they land, and the retention obligations that follow.

A consumer hook cannot alter the run it observes: sampling parameters are
snapshotted rather than aliased, and a panicking observer is recovered at the
seam, reported on stderr, and the run continues with an unchanged exit code.
There is no error return, by design — an audit problem must never change the
outcome of a command that has nothing to do with auditing. A deployment that
requires "no audit, no run" must enforce that before invoking the CLI. The
observer does receive the invocation's context, so a sink performing I/O can
honour the run's cancellation and deadline.

### Coverage, and two gaps worth knowing

Observed: `atcr review` including its `--verify`, `--debate`, and `--auto-fix`
stages; `atcr review --resume`; the standalone `atcr verify` and `atcr debate`;
`atcr benchmark run`; and the equivalent tools served by `atcr serve`.

Not observed:

- **`atcr doctor`** — its provider self-test uses a separate completer interface
  and never reaches the seam.
- **Diff-cache hits** — a cached reviewer output is replayed without a model
  call, so nothing is observed even though the content reaches the deliverable.
  Run with `--no-cache` when a complete per-run record matters.

### The review-complete observer

`cli.Hooks` has a second field, `ReviewComplete`. A registered
`cli.ReviewCompleteObserver` gets one `cli.ReviewReport` per successful review,
after the review's artifacts are written. It fires for `atcr review` (fresh
and `--resume`), after the history and audit records are on disk, and for the
MCP review tool served by `atcr serve`, which writes no history or audit
records. A failed review does not fire it, and neither does a standalone
`atcr reconcile`.

Unlike `ModelInvocation`, the report is not source-code-sensitive: it holds the
run id, review directory, base and head SHAs, PR number, the partial flag and
agent counts, and, only when the review reconciled in-process (for example with
`--fail-on`), the finding counts by severity. It carries no diff and no findings
text. Its contract is the same: no error return, a panic is recovered and
reported on stderr, and the review keeps its exit code.

```go
type reviewSink struct{}

func (reviewSink) OnReviewComplete(ctx context.Context, rep cli.ReviewReport) {
	// e.g. post rep.RunID and rep.FindingsBySeverity to a dashboard
}

func main() {
	hooks := cli.Hooks{ReviewComplete: reviewSink{}}
	os.Exit(cli.MainWithHooks(context.Background(), os.Stdout, os.Stderr, hooks))
}
```

`cli/hooks_external_test.go` registers an observer this way from a separate
test package that sees only the exported API.

### The fix-applied observer

`cli.Hooks` has a third field, `FixApplied`. A registered
`cli.FixAppliedObserver` gets one `cli.FixApplied` per `atcr review --auto-fix`
run whose patch applied and passed local validation. It fires once the fix is
kept in the working tree and before any GitHub call, so a later GitHub failure
does not take it back. A run that reverts does not fire it: an apply failure, a
failed or timed-out validation, a validation command that cannot start, and the
refusal to validate unsandboxed without `--no-sandbox` all report nothing.

The report holds the run id (the same value as `ModelInvocation.RunID`) and, for
each applied file, its path relative to the repository root and the unified diff
applied to it, verbatim. That diff is model-written source code, so it is
sensitive in the same way as `ModelInvocation`: redact it before you persist or
forward it. The contract is the same as the other observers: no error return, a
panic is recovered and reported on stderr, and the run keeps its exit code.

```go
type fixSink struct{}

func (fixSink) OnFixApplied(ctx context.Context, fa cli.FixApplied) {
	for _, f := range fa.Files {
		// e.g. record fa.RunID, f.Path and a redacted f.Diff
	}
}

func main() {
	hooks := cli.Hooks{FixApplied: fixSink{}}
	os.Exit(cli.MainWithHooks(context.Background(), os.Stdout, os.Stderr, hooks))
}
```

See the godoc on `cli.Hooks`, `cli.ModelInvocation`, `cli.ReviewReport`,
`cli.FixApplied`, and `cli.MainWithHooks` for the field-level contract.

### Reading dismissed findings

`cli.ExportWontfixRecords` is a read-only seam for an embedder that needs the
findings developers dismissed with `atcr debt resolve --status wontfix
--reason "<reason>"`. It returns one `cli.WontfixRecord` per finding whose
effective status is `wontfix` (reason, file, line, category, and the
reviewer personas that raised it), sorted by id, from either store layout.
`WontfixExportOptions.Store` has the meaning of `atcr debt list --store`; left
empty, the store is resolved from `debt.store` the way the debt commands
resolve it. The seam adds, changes and removes no record, and it sends
nothing anywhere. The reason is free text and the file is a repository path,
so an embedder that forwards either owns that exposure.
