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
user, and memory / CPU / PID resource caps plus a wall-clock timeout. For the
full list of what the container enforces, see
[What the sandbox guarantees](execution.md#what-the-sandbox-guarantees) in the
execution reference — this page does not restate those mechanics, so there is a
single source of truth for them.

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
different in *what* it mounts: because validation must confirm the patch you just
applied, it mounts the **already-patched live working tree** at `/work` — not a
snapshot. The distinction matters: you are validating the mutated tree, not a
clean copy.

The **mount mode is still read-only**. `/work` is mounted `:ro`, exactly as
`--exec` mounts its snapshot; the patch is applied to your working tree on the
host *before* the container starts, and the container validates that
already-mutated tree without being able to mutate it further. Build caches and
temp files still work because `HOME`, `TMPDIR`, `GOCACHE`, `GOTMPDIR`, and
`XDG_CACHE_HOME` are redirected into a writable `/scratch` tmpfs overlay, so the
default `go build` / `go test` validation path never needs to write into `/work`.

**Limitation (read-only `/work`):** a validation command that needs to write
*into the working tree itself* — for example one that emits a coverage profile
into the tree, runs code generation, or does `lint --fix` — cannot do so, because
`/work` is read-only inside the container. Such a command fails inside the
sandbox even though it would succeed on the host. Point those writes at
`/scratch`, or run that step outside `--auto-fix`'s sandboxed validation.

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

- **`apply_target`** — the working-tree path the patch is applied to, resolved
  against the repo root when relative. **Empty defaults to the repo root.**
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
