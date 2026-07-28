# atcr — project guidance

## `.planning/` is intentionally gitignored (local-only)

The entire `.planning/` tree — TD README, sprints, plans, code-review outputs — is
**deliberately gitignored in this repo** (`.gitignore`: "kept locally, but never
committed"). It is local working state, not part of atcr's git history.

**Local-only does not mean unprotected:** `.planning/` is backed up **hourly to an
unraid RAID10 array**, so its durability is handled out-of-band. Do not commit it to
git "for safety" — that is already covered.

This is by design and is **in tension with the planning/TD skills**, which are
generic and assume those artifacts are commit-able. In atcr specifically:

- Skills **read and write** `.planning/…` as local files — that is expected and correct.
- Their **git steps that stage/commit `.planning/` files are intentional no-ops** here.
  Concretely: `/finalize-td` stripping resolved `[x]` rows "so the cleanup rides the
  PR", `/resolve-td` checkbox flips (`[ ]`→`[x]`), and `/reconcile-code-review` writing
  the README all persist **locally only** — they never enter a commit or PR.
- The skills still deliver their core value: the actual **code fixes** (tracked source
  and test files) do commit and ride the branch/PR normally.

**Do not** try to `git add` / force-commit ignored `.planning/` files, and do not
report a TD-README change as "committed" — only tracked code/test/docs changes ride
commits and PRs. A `git add -A` will correctly skip `.planning/` (the ignore is also a
safety net against sweeping local planning state into a commit).
