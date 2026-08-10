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

## Reviewer LLMs are flat-rate — do not ration them

Every agent in `~/.config/atcr/registry.yaml` resolves through the **`litellm`
provider** (`base_url: http://192.168.68.109:4000/v1`, key in `$LITELLM_API_KEY`,
already exported in the shell). That proxy is **flat-rate**, so a real `atcr review`,
`atcr benchmark run`, or full-panel fan-out costs nothing per call.

**Do not skip, sample, or shrink a run to save tokens, and do not ask permission to
spend on one.** Run the whole suite against real models — that is the point of having
them. The proxy's own config lives on `orchestrator.lan:~/docker/litellm/`; add or
repoint models there, not by degrading a test run here.

Practical notes:

- `curl $BASE/v1/models -H "Authorization: Bearer $LITELLM_API_KEY"` lists what is
  actually served (27 models as of 2026-08-07), including recent additions like
  `kimi-k3` (agent `kai`) and `qwen3.8-max` (agent `brad`). Prefer the strong models.
- Some upstreams behind the proxy are quota-limited (Alibaba, Moonshot); every
  quota-limited primary has a cross-provider `-backup` agent, so a dry plan degrades
  coverage rather than failing the panel.
- Real runs are **slow** — a full-panel or multi-case benchmark run routinely exceeds
  10 minutes and will blow a foreground command timeout. Run it in the background, and
  pass `--checkpoint` where the command supports it so an interrupted run resumes
  instead of re-paying the wall-clock.
- Project config resolves off the **literal cwd** with no upward repo-root search,
  while the model/persona registry resolves independently of cwd. So a scratch working
  directory carrying its own minimal `.atcr/config.yaml` plus an absolute
  `--suite-path` gives an isolated run that still uses the real registered models and
  mutates no shared repo state — important because concurrent sessions share this tree.
