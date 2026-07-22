# Auto-fix sandboxed validation (`--auto-fix`)

`atcr review --auto-fix` applies an LLM-generated patch to your working tree,
runs a post-apply validation command to confirm the fix builds and passes tests,
and automatically reverts the filesystem on failure. That validation step runs
**untrusted, model-authored code** — a hallucinated or prompt-injected `init()`
function or pre-build hook would otherwise execute with the same privileges as
the `atcr` process. To close that gap, `--auto-fix`'s validation runs inside the
same container isolation `--exec` uses.

## Sandboxed by default

Unlike `--exec` (which is strictly opt-in), `--auto-fix`'s post-apply validation
is **sandboxed by default — there is no flag to opt in**. When you pass
`--auto-fix`, the validation command is resolved and preflighted against a
container backend before any patch is validated, and it runs inside an ephemeral
container with the same guarantees `--exec` provides: no network, a read-only
root filesystem, all Linux capabilities dropped, no-new-privileges, a non-root
user, and memory / CPU / PID resource caps plus a wall-clock timeout. That
summary names *what* is enforced; for *how* each guarantee is enforced (the
`docker run` flags, the preflight, the scratch overlay), see
[What the sandbox guarantees](execution.md#what-the-sandbox-guarantees) in the
execution reference, which is the authoritative source for the container
mechanics this page deliberately does not duplicate.

Because sandboxing is on by default, `--auto-fix` **fails closed**: if no
`sandbox:` block is configured (or the backend fails its preflight) and you did
not explicitly pass `--no-sandbox`, the command hard-errors rather than silently
falling back to running the validation command on the host. Sandbox resolution
is the fourth checked piece of the `--auto-fix` startup gate, joined into the
same all-or-nothing usage error as the apply target, the validation command, and
the GitHub credentials — so a missing sandbox is reported alongside any other
missing piece in one message.

The only way to run `--auto-fix` validation directly on the host is the explicit
[`--no-sandbox`](#opting-out---no-sandbox) opt-out, described below.

## What runs in the sandbox — and how it differs from `--exec`

`--exec` mounts a **pristine, read-only snapshot** of the review's code at
`/work` and demonstrates a finding against it. `--auto-fix`'s validation is
different in *what* it validates: because validation must confirm the patch you
just applied, it works against the **already-patched live working tree** — not a
snapshot. The distinction matters: you are validating the mutated tree, not a
clean copy.

**`/work` is writable for `--auto-fix` validation, via an ephemeral copy.** The
patched working tree is mounted **read-only at `/src`**, and `/work` is backed by
a fresh writable `tmpfs` that the container seeds with `cp -a /src/. /work/`
before your `validate_command` runs; the command then executes against that
writable `/work` copy. Because the snapshot side (`/src`) stays read-only for the
container's entire lifetime and every write lands in the throwaway `/work` tmpfs
— which, along with everything written into it, dies with the container — **no
host file is ever mutated** by validation, exactly as with `--exec`. This is an
internal behavior of `--auto-fix`'s validation path (the overlay is requested by
the validation runner, not an operator-facing config option); `--exec` keeps its
strict read-only `/work` mount unchanged. Build caches and temp files still
redirect into a separate writable `/scratch` tmpfs (`HOME`, `TMPDIR`, `GOCACHE`,
`GOTMPDIR`, `XDG_CACHE_HOME`), so the `go build` / `go test` validation path keeps
working exactly as before.

**Non-Go validators are supported.** Because a `validate_command` runs against
the writable `/work` copy, commands that write *under the project directory* —
`npm run build` → `dist/`, `cargo build` → `target/`, Python `__pycache__`,
bundlers, most codegen — now succeed instead of failing with `EROFS`. Those
writes land in the ephemeral `/work` tmpfs and are discarded with the container,
so a valid non-Go fix is validated and its PR opened rather than silently
reverted.

> **Image requirement.** The ephemeral-copy overlay runs your `validate_command`
> inside `/bin/sh -c 'cp -a /src/. /work/ && cd /work && exec "$@"'`, so the
> validation image must provide **`/bin/sh` and `cp`** — true for `alpine`- and
> `golang`-family images, but **not** for `distroless`/`scratch` images, which
> ship neither. If your image has no shell, base it on one that does. The `/work`
> tmpfs is sized by an internal default and, like every tmpfs, counts against the
> container's `--memory` cap, so a validation that copies a large source tree or
> emits a large build output may need a higher `sandbox.memory` (as a rule of
> thumb, `memory ≥ /work size + build working set`).

## Configuring auto-fix — the `auto_fix:` block

The optional `auto_fix:` block in `.atcr/config.yaml` supplies the
config-derived pieces of the `--auto-fix` flow. The block is **optional**: its
mere presence enables nothing (the `--auto-fix` flag must still be passed), and a
config with no `auto_fix:` block at all is valid — every field falls back to a
default.

```yaml
# .atcr/config.yaml
sandbox:
  backend: docker            # required for --auto-fix by default (see above)
  image: golang:1.25         # MUST be present locally (runs are network-isolated)
  test_command: [go, test, ./...]
auto_fix:
  apply_target: .            # where the patch is applied (default: repo root)
  validate_command: [go, build, ./...]   # post-apply validation argv
  validate_timeout: "2m"     # Go duration bounding one validation run
```

The block has exactly three fields:

- **`apply_target`** — the working-tree path the patch is applied to. **Empty
  defaults to the repo root, which is currently the only accepted value:** a
  relative value is resolved against the repo root and must resolve to the root
  itself. A subdirectory target is rejected with a usage error, because fixes are
  committed with repo-root-relative paths.
- **`validate_command`** — the post-apply validation command as an explicit argv
  (a list of tokens, never a shell string), e.g. `[go, build, ./...]`. **Empty
  falls back to the single built-in default**, which is the Go build command when
  a `go.mod` is present at the apply target.
- **`validate_timeout`** — bounds one validation run, written as a **Go duration
  string** (e.g. `"2m"`, `"90s"`). An **empty value inherits the gate's ~2 minute
  default**. A **zero or negative value is rejected at config-load time** (not at
  run time), so a misconfigured timeout fails fast rather than producing an
  immediate false timeout mid-run.

> **Note on the `sandbox:` block requirement.** Because validation is sandboxed
> by default, `--auto-fix` needs a `sandbox:` block (with an `image` and
> `test_command`) just as `--exec` does — the container image is where your
> validation command runs. If Docker is genuinely unavailable in your
> environment, use [`--no-sandbox`](#opting-out---no-sandbox) to accept the risk
> of host execution instead.

## Opting out (`--no-sandbox`)

The `--no-sandbox` flag is the **only** way to run `--auto-fix`'s validation
outside the container. It is a command-line flag on `atcr review`; there is no
config-file equivalent — nothing in the `auto_fix:` block (or anywhere in
`.atcr/config.yaml`) can disable the sandbox.

**What it does.** Passing `--no-sandbox` disables the container-isolation
validation path entirely: the resolver and its preflight are skipped, and the
post-apply validation command runs **directly on the host** instead. That means
the untrusted, potentially LLM-hallucinated or prompt-injected validation code
executes with the **full privileges of the `atcr` process** — none of the
container guarantees apply. It has network access, a writable filesystem, the
process's own capabilities, and no non-root confinement — the exact protections
listed in
[What the sandbox guarantees](execution.md#what-the-sandbox-guarantees) are all
removed. This page does not re-list them so that description stays a single
source of truth.

**It warns on every run.** Every `--no-sandbox` invocation prints a security
warning to stderr — not only the first time, and not gated behind any
"seen once" state. If you script `--no-sandbox` into a loop, expect the warning
on each run; that is deliberate, so the reduced isolation can never go unnoticed.

**When it is acceptable.** The intended use is environments where Docker is
unavailable — for example a CI runner or workstation with no Docker daemon, where
the sandboxed-by-default path cannot preflight a backend at all. Choosing
`--no-sandbox` there is choosing to **accept** that the validation command runs
un-isolated on the host: only do it when you already trust the environment and
the code under validation, or have other host-level containment around the
`atcr` process. If Docker is available, prefer the default sandboxed path.
