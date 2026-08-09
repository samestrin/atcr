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
`sandbox:` block is configured (or the backend fails its preflight and no
[`sandbox.fallback`](#os-level-fallback-sandboxfallback-os-level) is configured)
and you did not explicitly pass `--no-sandbox`, the command hard-errors rather
than silently falling back to running the validation command on the host. Even
with a fallback configured, the substitute is another *sandbox* — never the host.
Sandbox resolution
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
a fresh writable `tmpfs` that the container seeds with `cp -R /src/. /work/`
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
> inside `/bin/sh -c 'cp -R /src/. /work/ || exit 125; cd /work && exec "$@"'`, so
> the validation image must provide **`/bin/sh` and `cp`** — true for `alpine`- and
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
  fallback: os-level         # OPTIONAL opt-in; omit for today's fail-closed behavior
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
> environment, you have two options, in order of preference: the still-contained
> [`sandbox.fallback: os-level`](#os-level-fallback-sandboxfallback-os-level)
> opt-in below, or [`--no-sandbox`](#opting-out---no-sandbox) to accept the risk
> of unsandboxed host execution instead.

## OS-level fallback (`sandbox.fallback: os-level`)

`sandbox.fallback` is an **opt-in** config field that names a second backend to
try when Docker fails its preflight. It uses the operating system's own process
confinement — `sandbox-exec` on macOS, `bwrap` (Bubblewrap) on Linux — instead of
a container, so a machine with no Docker daemon can still run validation under
real containment. It applies to **both** `--auto-fix` validation and `--exec`
reproduction, since both resolve their backend through the same preflight.

```yaml
# .atcr/config.yaml
sandbox:
  backend: docker            # still the primary; the fallback is only a fallback
  image: golang:1.25
  test_command: [go, test, ./...]
  fallback: os-level         # the ONLY accepted value
```

**It is never automatic.** ATCR does not infer this from your host — not from a
missing `docker` binary, not from a CI environment variable. All of the following
must hold before the fallback engages: you wrote `fallback: os-level` in config,
Docker's preflight has already failed, and that failure was not caused by your own
cancellation (an interrupted run is refused outright rather than retried under a
different backend). It is never chosen as a first-choice backend while Docker is
healthy. Any **non-empty** value other than `os-level` is rejected at config-load
time; a blank or whitespace-only value is treated as unset.

**With `fallback` unset, nothing changes.** That is the default and the shape of
every config written before the field existed: a Docker preflight failure remains
a hard refusal, and `--no-sandbox` remains the only way to run validation outside
a sandbox. The fail-closed posture described in
[Sandboxed by default](#sandboxed-by-default) is untouched for anyone who does not
opt in.

**When neither backend is usable, the run is refused.** If Docker's preflight
fails and the OS-level backend then fails its own preflight too (no
`sandbox-exec`/`bwrap`, or the platform cannot provide containment), `--auto-fix`
hard-errors with a message naming both failures. It does **not** quietly degrade
to unsandboxed host execution. A configured-but-broken fallback is a refusal, not
a bypass.

**It logs when it engages.** Switching backends changes your isolation model, so
ATCR emits a warning naming the backend, the Docker error that triggered it, and
what is no longer enforced. Note this is a **log line, not a banner**: unlike
`--no-sandbox`'s unconditional stderr warning, it goes through the context logger
and is therefore suppressible with `ATCR_LOG_LEVEL=error`.

### What you give up relative to Docker

The OS-level backend is a genuine improvement over `--no-sandbox` — the run still
gets no network egress, and no access to `$HOME` (so `~/.ssh` stays unreadable).
It is **not** equivalent to the container backend: it has no image, no root
filesystem to remount, and no capability set to drop, and on macOS it does not get
the virtual-machine boundary Docker Desktop provides. So the guarantee list in
[What the sandbox guarantees](execution.md#what-the-sandbox-guarantees) applies
only to the Docker backend; that page names the narrower OS-level set separately.
Concretely, opting in accepts all of the following:

- **Runs as the invoking user**, not as the unprivileged `uid 65534` the container
  backend uses.
- **No resource caps.** `sandbox.memory`, `sandbox.cpus`, and `sandbox.pids_limit`
  still pass config validation but are **not enforced** by this backend. Every
  hardened container default is likewise dropped: `--cap-drop ALL`,
  `--security-opt no-new-privileges`, and the read-only root filesystem.
- **`sandbox.image` is ignored.** There is no container, so `test_command` /
  `validate_command` runs against the **host's** toolchain rather than the declared
  image's. If that toolchain is not reachable the command exits `127`, which a
  reviewing model can misread as a genuine validation failure rather than a missing
  tool. On Linux, "reachable" is narrower than your `PATH` suggests: the sandbox
  binds only `/usr` plus `/bin`, `/sbin`, `/lib`, and `/lib64`, so a toolchain
  installed under `/opt`, `/nix`, `/home/linuxbrew`, or a version-manager shim in
  `$HOME` (mise, asdf, nvm) is invisible inside the sandbox even though `PATH`
  still resolves it on the host.
- **`/tmp` is not writable on either platform.** On Linux the run gets a fresh,
  ephemeral `--tmpfs /tmp`. On macOS `sandbox-exec` cannot provide one, so the
  profile simply grants no write rule for `/tmp` and a write there is denied.
  `TMPDIR`, `HOME`, `GOCACHE` and `GOTMPDIR` point into the per-run scratch
  directory, so anything following the environment works; a command that
  hardcodes bare `/tmp` fails on macOS with `Operation not permitted`.
- **Go dependency resolution needs a warm module cache.** The sandbox has no
  network, so a non-vendored dependency cannot be downloaded during a run.
  `GOMODCACHE` points at a persistent, atcr-owned cache
  (`<user cache dir>/atcr/oslevel-modcache`) mounted **read-only**, so a cache
  warmed on the host is reused by every later run — but nothing warms it
  automatically yet, and until it is warmed a `go build` with an external
  dependency fails with `dial tcp: ... no such host`. Vendored trees are
  unaffected. The cache is read-only on purpose: a persistent directory that
  model-authored code could write would let one run poison what later runs
  compile.
- **The platform binary is resolved only from `/usr/bin`, `/bin`, `/usr/sbin`, and
  `/sbin` — never from `$PATH`.** This is deliberate (a `$PATH` lookup is an
  injection surface for the very code being contained), but it means a `bwrap`
  installed under `/usr/local/bin` — a source build, Homebrew-on-Linux, or Nix —
  is **not** found, and the fallback preflight fails.
- **Validation cost scales with working-tree size, and very large trees are
  refused.** Because the validation step needs a writable tree, each run makes a
  full host-side copy of the snapshot before executing. That copy is bounded at
  **2 GiB and 500,000 entries**; a tree exceeding either ceiling fails the run
  rather than validating a partial copy. Entries the copy cannot read are skipped
  with a warning, so an unreadable file does not abort the run.

`sandbox.fallback` and `--no-sandbox` are **separate, non-overlapping** escape
hatches, and neither implies the other: the flag accepts fully unsandboxed host
execution, while this config field substitutes a still-contained backend.

## Opting out (`--no-sandbox`)

The `--no-sandbox` flag is the **only** way to run `--auto-fix`'s validation
unsandboxed, directly on the host. (`sandbox.fallback: os-level` also runs
validation outside a container, but under OS-level confinement rather than none —
it is not an opt-out.) It is a command-line flag on `atcr review`; there is no
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
