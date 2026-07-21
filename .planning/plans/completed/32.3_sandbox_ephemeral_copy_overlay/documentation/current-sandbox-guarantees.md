# Current Sandbox Guarantees and the Read-Only `/work` Claims

Excerpts of ATCR's own documentation and code comments that pin the sandbox's
current read-only-`/work` behavior, with links back to the source locations.
Plan 32.3 touches shared sandbox infrastructure, so each excerpt is annotated:
**PRESERVE** (must stay true for `--exec`, `Writable:false`) or **UPDATE**
(goes stale once `RunSandboxedValidation` sets `Writable:true`; in T6's scope).

## `--exec` guarantees — PRESERVE (must remain textually accurate)

Source: [`docs/execution.md:51-62`](../../../../../docs/execution.md) ("What the sandbox guarantees")

> - **Read-only snapshot** — the review's code snapshot is mounted read-only at
>   `/work`; the run cannot mutate your working tree. A writable `tmpfs` scratch
>   overlay is provided at `/scratch`.
> - **Read-only root filesystem**, **all capabilities dropped** (`--cap-drop ALL`),
>   **no new privileges** (`--security-opt no-new-privileges`), and a **non-root
>   user**.

Source: [`docs/execution.md:86-90`](../../../../../docs/execution.md)

> `/work` (the snapshot) is **read-only**; the only writable location is the
> `/scratch` tmpfs. `HOME`, `TMPDIR`, and the common build-cache vars
> (`GOCACHE`/`GOTMPDIR`/`XDG_CACHE_HOME`) are pointed at `/scratch` …

**Status:** PRESERVE. `--exec`'s two call sites (`internal/tools/exec_tools.go`)
never set `Writable`, so the zero value `false` keeps this text exactly true.
`codebase-discovery.json` lists `docs/execution.md` as `reference-only` — no edit
is in scope, but any implementation that would falsify these lines is a bug.

Source: [`internal/sandbox/sandbox.go:1-15`](../../../../../internal/sandbox/sandbox.go) (package doc)

> Every Backend MUST guarantee, for every Run:
>
>   - no network (the run cannot exfiltrate or call out),
>   - a read-only view of the snapshot (the run cannot mutate the work tree),
>   - resource caps (memory, CPU, PIDs) so a run cannot exhaust the host,
>   - non-root, dropped capabilities, and no-new-privileges.

**Status:** PRESERVE with nuance. Under `Writable:true` the *snapshot* (now at
`/src`) is still read-only and the host work tree still cannot be mutated — only
the ephemeral tmpfs copy is writable — so the MUST survives if the doc comment
is read as "the run cannot mutate the *host* work tree." T1's `Writable` field
doc comment should make that narrowing explicit rather than weakening the
package-level guarantee.

## `--auto-fix` claims — UPDATE (T6 scope; all go stale together)

Source: [`docs/auto-fix.md:38-53`](../../../../../docs/auto-fix.md) ("What runs in the sandbox — and how it differs from `--exec`")

> The **mount mode is still read-only**. `/work` is mounted `:ro`, exactly as
> `--exec` mounts its snapshot; the patch is applied to your working tree on the
> host *before* the container starts, and the container validates that
> already-mutated tree without being able to mutate it further.

**Status:** UPDATE (line 47 claim). Once T4 lands, `--auto-fix`'s `/work` is a
writable tmpfs copy and the snapshot moves to `/src:ro` — the "exactly as
`--exec`" comparison and the unconditional read-only claim become false.

Source: [`docs/auto-fix.md:55-60`](../../../../../docs/auto-fix.md) (standalone limitation paragraph)

> **Limitation (read-only `/work`):** a validation command that needs to write
> *into the working tree itself* — for example one that emits a coverage profile
> into the tree, runs code generation, or does `lint --fix` — cannot do so,
> because `/work` is read-only inside the container. … Point those writes at
> `/scratch`, or run that step outside `--auto-fix`'s sandboxed validation.

**Status:** UPDATE. This paragraph sits *between* the two ranges T6 originally
cited (38-53 and 62-70); `codebase-discovery.json` flags it in
`integration_gaps` as easy to miss, and plan.md's Refinement Decisions
(2026-07-21) explicitly extended T6's scope to cover it.

Source: [`docs/auto-fix.md:62-71`](../../../../../docs/auto-fix.md) (EROFS blockquote)

> **This makes sandboxed `--auto-fix` effectively Go-only today.** … many common
> non-Go validators write *under the project directory* — `npm run build` →
> `dist/`, `cargo build` → `target/`, bundlers, most codegen — and hit `EROFS`
> on the read-only `/work`. That failure is **indistinguishable from a genuine
> validation failure**: `--auto-fix` fails closed, reverts the applied patch,
> and opens no PR, so a perfectly valid fix is silently discarded.

**Status:** UPDATE. This is the bug statement the whole plan exists to remove;
the blockquote should be replaced by a description of the opt-in writable
overlay, including the new image requirement below.

Source: [`internal/verify/autofix_exec.go:47-55`](../../../../../internal/verify/autofix_exec.go) (`ResolveAutoFixSandbox` doc comment)

> Read-only /work limitation (effectively Go-only today): the validation runs
> with the patched working tree mounted read-only at /work … Until a writable
> build-output overlay exists, non-Go runners must redirect writes to /scratch,
> run outside sandboxed validation, or use --no-sandbox. See docs/auto-fix.md.

**Status:** UPDATE. A verbatim duplicate of the `docs/auto-fix.md` limitation,
found by the second `/refine-epic --deep` pass; must be rewritten in the same
pass as the docs so the two never disagree again.

## New constraint introduced by this plan — DOCUMENT (T6)

`Writable:true` Command mode wraps execution in
`/bin/sh -c 'cp -a /src/. /work/ && cd /work && exec "$@"' -- <command...>`.
Today's Command mode execs the image's argv directly with **no shell at all**
(`internal/sandbox/docker.go:146-147`), so the wrap adds an implicit requirement:
the operator's validation image must provide `/bin/sh` and a `cp` supporting
`-a` (true for `alpine`/`golang`-family images via busybox/coreutils; **false
for distroless/scratch images**). Failure mode on a shell-less image is a
container start/exec failure surfacing as a backend fault. T6 must note this in
`docs/auto-fix.md`'s sandbox-requirements section, and the `Writable`/`WorkSize`
field doc comments should repeat it (per plan.md's Refinement Decisions and
`codebase-discovery.json` → `integration_gaps`).
