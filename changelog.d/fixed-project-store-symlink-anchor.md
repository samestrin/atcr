- A project-tier debt store reached through a symlinked spelling of its path
  (for example `/var` and `/private/var` on macOS) now keeps its repository-root
  anchor, and the store is created through a handle on the repository root, so
  a root removed while the store is being created is refused instead of
  recreated. A store refused under the grandparent rule now says it is not a
  project-tier store. (epic 136 T1)
