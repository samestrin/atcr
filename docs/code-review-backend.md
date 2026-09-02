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

## Installing atcr

The caller needs the `atcr` binary on `PATH` before invoking either command below: `go install github.com/samestrin/atcr/cmd/atcr@latest` (Go 1.25+), or build from source (`go build -o atcr ./cmd/atcr`) for a pinned CI toolchain.

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
    unresolved.json              # findings with no symbol correspondence in the tracked tree
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
- `unresolved_filtered` — count of findings routed OUT of the primary stream
  into `unresolved.json` (see "Findings excluded from the primary stream"
  below). `0` on a run where every finding cited resolvable code.
- `unresolved_state` — what the content check actually did: `applied`,
  `disabled`, `unavailable`, `incomplete`, or absent. Read it BEFORE reading
  `unresolved_filtered`; see below for why a bare `0` cannot be interpreted. The
  state qualifies a `0`; it does not cap the count. `unavailable` in particular
  is compatible with a nonzero count — see its row below.

## Findings excluded from the primary stream

`findings.txt`, `findings.json` and `report.md` do **not** necessarily contain
every finding the reviewers produced. Two filters can route a finding into a
sidecar instead:

- **Consensus filter** → `ambiguous.json`, counted by `consensus_filtered`.
  Uncorroborated single-reviewer findings below the confidence bar, on a panel
  large enough for the filter to run.
- **Content resolution** → `unresolved.json`, counted by `unresolved_filtered`.
  Findings whose cited file does not exist, for which no filename-level
  correction was found, and whose described constructs appear nowhere in the
  tracked tree.

Neither filter deletes anything: a routed finding is written in full to its
sidecar. A caller that needs every finding the panel produced must read the
sidecars alongside `findings.json`; a caller that wants only findings that
correspond to real code can read `findings.json` alone.

Each record in `unresolved.json` has the same shape as a `findings.json` record,
plus one optional field:

- `unresolved_reason` — why the content check routed this finding. Absent means
  the ordinary case: the constructs its prose names appear nowhere in the tracked
  tree. `"doc_shield"` means they DO appear, but only in a file classified as
  documentation by its extension (`.md`, `.markdown`, `.mdx` prose, `.rst`,
  `.txt`, `.adoc`). That routing rests on a heuristic, so the finding is
  preserved here like any other but is not charged to the reviewer's scorecard
  denominator — see `findings_doc_shielded` in [scorecard.md](scorecard.md).

### Reading `unresolved_filtered`

A `0` here does **not** by itself mean "no finding was fabricated". At least six
conditions produce it, and most of them mean the check never adjudicated
anything. `unavailable` is the one state that does not resolve the question at
all: it covers both "no index was built", which adjudicates nothing, and a
parser failure that left the declaration set empty, where an index WAS built and
the raw-token search still ran. A `0` under `unavailable` could be either, and a
nonzero count under it is perfectly ordinary.

| `unresolved_state` | Meaning | What a `0` count means |
|---|---|---|
| `applied` | The check was in force. It does not assert an index was built — when every finding cites a file that exists, none is needed. | Nothing was routed. The healthy case. |
| `disabled` | `ATCR_DISABLE_AST_GROUPING` is set, or there was no tracked file index (the root is not a git repository, or git was unavailable). | Nothing was checked. |
| `unavailable` | No usable **declaration set** — either no index at all (over the file cap, nothing in the tracked tree readable, no root-contained file), or an index whose parser failed and left the declaration set empty. | Ambiguous, and the only row that is. With no index, nothing could be routed. With a failed parser, an index WAS built and the raw-token search ran, so a `0` means "checked and routed nothing" — and a NONZERO count under this state is expected, not a contradiction. |
| `incomplete` | The index was built but a region of the tree went unsearched, so every no-match verdict was withheld. Outranks `unavailable`: a run that both lost its parsers and went unread reports `incomplete`, because the unread region is what withheld the verdicts. Resolutions are unaffected by a hole, but that particular run has no declarations to resolve against either. | Nothing could be routed. |
| absent | Content resolution did not run for this reconcile (no repo root was resolved — the ordinary case on the MCP path). | Nothing was checked. |

`unresolved_state` renders in `report.md` unconditionally and rides the
`tier-4 content resolution` log line on every CLI path, so the distinction
survives outside `summary.json`. A caller treating a `0` as a clean bill of
health must first confirm the state is `applied`.

Re-running `atcr reconcile` over the same review directory rewrites every
`reconciled/` artifact unconditionally — including replacing a non-empty
`unresolved.json` with `[]` when the re-run routes nothing. Before overwriting,
the prior generation is copied to `reconciled.bak/` in full, sidecars
included, so an overwritten sidecar is recoverable from
`reconciled.bak/unresolved.json` until the next re-run replaces the backup.

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
