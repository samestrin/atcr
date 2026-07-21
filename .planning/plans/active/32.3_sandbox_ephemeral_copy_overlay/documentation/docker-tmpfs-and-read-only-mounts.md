# Docker Mount Semantics: `--read-only`, `--tmpfs`, and Read-Only Bind Mounts

Excerpts from the official Docker documentation that Plan 32.3's writable-overlay
mechanism depends on, with links back to the source material. The plan's core
constraint — *`/work` has no writable backing under the container's global
`--read-only` rootfs flag without an explicit new `--tmpfs` mount* — follows
directly from the semantics documented here.

## `--tmpfs` mounts: ephemeral, memory-backed, removed with the container

Source: [Docker docs — tmpfs mounts](https://docs.docker.com/engine/storage/tmpfs/)

> If you're running Docker on Linux, you have a third option: tmpfs mounts.
> When you create a container with a tmpfs mount, the container can create
> files outside the container's writable layer.
>
> As opposed to volumes and bind mounts, a tmpfs mount is temporary, and only
> persisted in the host memory. When the container stops, the tmpfs mount is
> removed, and files written there won't be persisted.

**Why it matters here:** this is exactly the "ephemeral copy" property the plan
needs — the writable `/work` overlay dies with the container, so no host file is
ever mutated (Acceptance Criterion: *"only the ephemeral `/work` tmpfs copy is
writable, and it dies with the container"*). It also confirms tmpfs is the right
tool for `/scratch` today and `/work` under `Writable:true`.

> **Mounting over existing data:** If you create a tmpfs mount into a directory
> in the container in which files or directories exist, the pre-existing files
> are obscured by the mount.

**Why it matters here:** mounting `--tmpfs /work` hides whatever the image had at
`/work`; the copied-in source tree (`cp -a /src/. /work/`) is the only content
`/work` will have. Nothing from the image at that path is reachable.

### `--tmpfs` syntax and the options the plan uses

Source: [Docker docs — tmpfs mounts, Options for `--tmpfs`](https://docs.docker.com/engine/storage/tmpfs/#options-for---tmpfs)

```console
$ docker run --tmpfs <mount-path>[:opts]
```

Relevant options from the official table:

| Option | Description |
|--------|-------------|
| `rw`   | Creates a read-write tmpfs mount (default behavior). |
| `exec` | Allows the execution of executable binaries in the mounted file system. |
| `size` | Specifies the size of the tmpfs mount, for example, `size=64m`. |

**Why it matters here:** the plan's new mount is
`--tmpfs /work:rw,exec,size=<cfg.WorkSize>` — mirroring the existing
`--tmpfs /scratch:rw,exec,size=<cfg.ScratchSize>` mount in
`internal/sandbox/docker.go`. `exec` is required so build tools can execute
binaries/scripts from the working tree; `size=` is what the new
`DockerConfig.WorkSize` default feeds (sized for a full source-tree copy, unlike
`ScratchSize`'s build-cache-sized `64m`).

**Limitation to know:** per the same page, tmpfs mounts are *"only available if
you're running Docker on Linux."* This matches the sandbox's existing `/scratch`
tmpfs dependency, so the new `/work` tmpfs adds no new platform constraint beyond
what Epic 11.0/32.0 already accepted.

## `--read-only`: the container root filesystem is read-only

Source: [`docker run` reference — `--read-only`](https://docs.docker.com/reference/cli/docker/container/run/#read-only)

`docker run --read-only` mounts the container's root filesystem as read-only.
Writable locations must be added explicitly on top of it — in this codebase that
is done exclusively via `--tmpfs` mounts.

**Why it matters here:** `dockerRunArgs` (`internal/sandbox/docker.go:117`)
already passes a global `--read-only` flag, so the container rootfs — including
any ordinary path like `/work` — is not writable. Renaming the existing
`-v` bind target from `/work` to `/src` without adding a new `--tmpfs /work`
mount would leave `/work` as a plain path on the read-only rootfs and `cp -a`
would still fail with `EROFS` (this was correction #3 from the epic's first
`/refine-epic` pass; see `original-requirements.md` → Refinements). The only
currently-writable location today is the explicit `/scratch` tmpfs.

## Bind mounts with `:ro`: the read-only snapshot

Source: [Docker docs — Bind mounts](https://docs.docker.com/engine/storage/bind-mounts/)

A bind mount (`-v <host-path>:<container-path>[:opts]`) propagates a host
directory into the container; the `:ro` option makes it read-only inside the
container.

**Why it matters here:**

- `Writable:false` (default, used by `--exec`): `-v <SnapshotDir>:/work:ro` —
  unchanged from today, pinned byte-identical by
  `TestDockerRunArgs_HardeningFlagsPresent` (`internal/sandbox/sandbox_test.go:55`).
- `Writable:true` (`--auto-fix` after T4): `-v <SnapshotDir>:/src:ro` — the
  snapshot stays read-only for its entire lifetime at `/src`; only the tmpfs
  copy at `/work` is writable.

The mount-spec injection guard in `RunSpec.validate()`
(`internal/sandbox/sandbox.go:55-63`, rejecting `:` in `SnapshotDir`) exists
precisely because the snapshot dir is interpolated into this bind-mount spec;
that guard is unaffected by the target rename.

## Related global specifications

- `.planning/specifications/packages/standard-library.md` — atcr's conventions
  for `os/exec` (used to drive the `docker` CLI) and `context` (run timeouts).
- `.planning/specifications/packages/testify.md` — assertion framework used by
  the new T5 tests (`internal/sandbox` already uses testify
  `assert`/`require`).
