# Using atcr as a code-review backend (`--output-dir`)

atcr can serve as the multi-agent reviewer backend for a separate code-review
skill or pipeline (for example, an external sprint-verification skill that
fans out to a reviewer pool and then merges the findings into its own
technical-debt store). This is distinct from atcr's bundled `+1 reviewer`
skill (see [skill-usage.md](skill-usage.md)); here atcr is invoked as a
subprocess and the caller owns the downstream merge.

This page documents the contract that backend integration relies on: the
invocation, the output tree, the consumable findings formats, and the
behavioral notes a caller must account for.

## Invocation

Two commands, run from the repository under review (so atcr can validate each
finding's cited file path against the working tree):

```sh
atcr review --output-dir "${OUT_DIR}" [--base <ref> --head <ref> | --merge-commit <sha>]
atcr reconcile "${OUT_DIR}"
```

- `--output-dir` writes the full review tree to `${OUT_DIR}` instead of the
  default `.atcr/reviews/<id>/`, and does **not** update `.atcr/latest`. It is
  mutually exclusive with `--id`.
- Range flags map directly: `--base`/`--head` for an explicit range, or
  `--merge-commit <sha>` (base = `<sha>^`, head = `<sha>`). With no range flag,
  atcr reviews the current branch against the detected default branch.
- atcr reads its roster, providers, and timeouts from `.atcr/config.yaml` in
  the reviewed repo and the user registry at `~/.config/atcr/registry.yaml`.
  The caller does not pass these.

## Pre-flight

- **Binary present:** `atcr --version` (exits 0 and prints `atcr version <v>`)
  or `command -v atcr`. atcr also ships an `atcr version` subcommand that
  prints the same string.
- **Repo initialized:** `atcr review` requires `.atcr/config.yaml` in the
  reviewed repo and hard-fails without it
  (`no roster found: .atcr/config.yaml not found ... run 'atcr init'`). Catch
  this up front rather than mid-fan-out.

## Output tree

After both commands, `${OUT_DIR}` contains:

```
${OUT_DIR}/
  manifest.json                  # review provenance, roster, timing
  payload/                       # what the reviewers saw
  sources/
    pool/
      raw/agent/<agent>/         # per-agent review.md, findings.txt, status.json
      findings.txt               # merged pool stream — 8 columns, REVIEWER per row
      summary.json               # per-agent tallies + run status
  reconciled/
    findings.txt                 # reconciled stream — 9 columns (REVIEWERS + CONFIDENCE)
    findings.json                # structured form (verification, path warnings, exec evidence)
    report.md                    # human-readable report
    summary.json                 # reconcile tallies
    ambiguous.json               # gray-zone clusters
    disagreements.json           # severity-conflict radar
```

A backend caller typically verifies these four files exist before consuming:
`sources/pool/findings.txt`, `sources/pool/summary.json`,
`reconciled/findings.txt`, `reconciled/summary.json`.

## Which findings format to consume

Two pipe-delimited streams are available; both begin with a
`# atcr-findings/v1` version header (comment lines are skipped by parsers).
See [findings-format.md](findings-format.md) for the full column spec.

| Stream | Columns | Use when |
|--------|---------|----------|
| `sources/pool/findings.txt` | 8: `SEVERITY\|FILE:LINE\|PROBLEM\|FIX\|CATEGORY\|EST_MINUTES\|EVIDENCE\|REVIEWER` | You want to merge atcr's **per-reviewer** findings alongside other sources and recompute REVIEWERS-union + CONFIDENCE yourself. The 8-column shape is the common per-source contract. |
| `reconciled/findings.txt` | 9: `…\|REVIEWERS\|CONFIDENCE` | You want atcr's already-collapsed, confidence-scored result and will not re-merge across other sources. |

Most pipeline integrations consume the **8-column pool stream**: it preserves
atcr's individual reviewer attribution (the `REVIEWER` column carries the agent
name, e.g. `bruce`), so a downstream reconciler can cluster atcr's reviewers
together with other sources and compute confidence across the whole set rather
than ingesting atcr's pre-collapsed blob.

## summary.json fields

`reconciled/summary.json` carries the fields a caller usually surfaces:

- `total_findings` — reconciled finding count.
- `sources_scanned` / `per_source_counts` — which sources contributed and how
  many each.
- `partial` — `true` if any agent failed or timed out (the run still produced
  results from the agents that succeeded).
- `clusters_collapsed`, `severity_disagreements` — merge diagnostics.
- `authority_promoted` — count of findings PageRank authority promotion raised
  from MEDIUM to HIGH confidence in the run (observability for the promotion
  signal; `0` when no single-reviewer finding was promoted).

## Behavioral notes for callers

- **Partial runs are normal.** If an agent times out or errors, `partial` is
  `true` and the surviving agents' findings are still written. A backend
  pipeline must treat `partial=true` as success-with-fewer-sources, not
  failure.
- **Finding counts differ from other backends.** atcr's deterministic
  reconciler uses its own clustering/dedupe and confidence logic, so counts
  will not match a different reviewer backend run over the same range. This is
  expected; the pipeline must not assert count parity.
- **Review scope.** Reviewers scope to diff-touched lines. To constrain a
  review to a specific plan's work items, pass `atcr review --sprint-plan
  <path>`: the plan's markdown content is injected as a `SCOPE CONSTRAINT`
  before the diff so reviewers suppress findings unrelated to those work
  items. A missing or empty plan is ignored; an unreadable one warns and
  proceeds.
