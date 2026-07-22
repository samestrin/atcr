# Plan 32.3 Documentation

Local documentation excerpts for the Sandbox Writable Overlay plan — quick access
to the reference material the implementation depends on, with links back to the
original sources. Ordered by importance to the sprint.

## Documents

1. **[current-sandbox-guarantees.md](current-sandbox-guarantees.md)** — Excerpts of
   ATCR's own documented read-only-`/work` guarantees (`docs/execution.md`,
   `docs/auto-fix.md`, the `internal/sandbox` package doc, and
   `internal/verify/autofix_exec.go`), each annotated PRESERVE (`--exec` control
   group) or UPDATE (T6 scope). This is the contract map: what the regression
   tests must keep true and what the docs pass must rewrite.
2. **[docker-tmpfs-and-read-only-mounts.md](docker-tmpfs-and-read-only-mounts.md)** —
   Official Docker documentation excerpts for `--tmpfs` (syntax, `rw`/`exec`/`size=`
   options, ephemerality), the global `--read-only` rootfs flag, and `:ro` bind
   mounts — the exact mount semantics behind the `Writable:true` overlay design.
3. **[source.md](source.md)** — Source index produced by `/find-documentation`
   and corrected by `/refine-docs`: every source above with its relevance and
   reason, plus the global-spec inventory note.

## Scope Notes

- `.planning/specifications/` has no standard covering Docker/container mount
  internals; the Docker docs excerpted in file 2 are the source of truth for the
  mechanism. The plan's stdlib (`os/exec`, `context`) and test (testify) idioms
  follow `.planning/specifications/packages/standard-library.md` and
  `.../testify.md`.
- These are planning documents only — implementation work happens in the sprint;
  T6 applies the doc updates mapped in file 1 to the real `docs/` tree.
