# Disagreement Radar

The disagreement radar promotes reviewer disagreement from a buried artifact to a
first-class output. It points a human reviewer at the highest-tension spots in a
change — where independent reviewers split, where a single reviewer raised
something alone, where the reconciler could not decide, and where skeptics could
not agree a finding is even real.

The radar is a deterministic projection over data the reconciler already
produces. It collects no new reviewer data and does not change clustering, dedup,
or merge behavior.

## What the radar surfaces

| Class | Source | Signal |
|-------|--------|--------|
| `severity_split` | a merged finding with a `disagreement` annotation (`reconciled/findings.json`) | reviewers assigned different severities to the same location |
| `solo_finding` | a finding raised by a single reviewer (MEDIUM confidence) | a spot only one reviewer flagged — others may have missed it |
| `gray_zone` | a pair in `reconciled/ambiguous.json` | similarity fell in the gray zone `[0.4, 0.7)`; the reconciler left it unmerged |
| `verification_disagreement` | a `verdict: unverifiable` produced by a skeptic-vote **tie** (`reconciled/verification.json`, Epic 3.0) | skeptics could not agree whether the finding is real |

Out-of-scope findings (pre-existing issues outside the reviewed change) and
refuted findings never enter the radar.

Persona tension (correctness reviewer vs. design reviewer) is **not** surfaced:
the data model carries no persona/role metadata, only model names.

## Scoring and ranking

Items are ranked highest-tension first, deterministically (the same input always
produces the same order).

- **Severity spread** is the tier distance between the highest and lowest severity
  on a location: `CRITICAL=4, HIGH=3, MEDIUM=2, LOW=1`, so `LOW vs CRITICAL` has a
  spread of 3.
- **Independence** is the count of distinct reviewers on the finding. This is a
  **v1 proxy** recorded in the handoff file as `independenceModel:
  "distinct-reviewer-count"`. The data model carries no model-strength or
  near-duplicate-model signal, so a richer independence metric is out of scope.
- **Score**: when a severity spread exists, `score = spread × independence`;
  otherwise `score = the finding's severity rank`. So a CRITICAL solo finding
  (score 4) outranks a `LOW vs MEDIUM` split (spread 1 × independence 2 = 2),
  while a `LOW vs CRITICAL` split among two reviewers (3 × 2 = 6) tops both.

Ties on score break by severity rank (desc), then file, line, kind, and problem
text — a total order, so ranking is reproducible.

The inclusion threshold is a fixed default in v1: every severity split
(spread ≥ 1), every solo finding, every gray-zone pair, and every verification
tie is surfaced. It is not configurable yet.

## Surfaces

### `atcr report --disagreements`

Renders the focused radar — a ranked list of the tension spots, each with the
model positions side by side — instead of the standard report:

```
atcr report --disagreements [id-or-path]
```

The flag is listed in `atcr report --help`. When there is no tension the view
prints `No disagreements detected.`

### Radar section in `report.md`

The standard markdown report (`atcr report`, `atcr report --format md`, and the
persisted `reconciled/report.md`) carries a `## Disagreements` section **above**
the consensus findings. The section is omitted entirely when there are no
disagreements, so a review with no tension produces byte-identical report output
to the pre-radar format.

## Handoff schema — `reconciled/disagreements.json`

`reconciled/disagreements.json` is the stable, versioned queue Epic 6.0
(Cross-Examination) consumes directly, without re-parsing `ambiguous.json` or
`findings.json`.

```json
{
  "schemaVersion": "1.0",
  "independenceModel": "distinct-reviewer-count",
  "items": [
    {
      "kind": "severity_split",
      "file": "internal/store/cache.go",
      "line": 88,
      "severity": "CRITICAL",
      "problem": "unbounded map grows without eviction",
      "score": 6,
      "spread": 3,
      "independence": 2,
      "reviewers": ["greta", "otto"],
      "disagreement": "LOW vs CRITICAL"
    }
  ]
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `schemaVersion` | string | Contract version. `"1.0"`. Bumped on any breaking change. |
| `independenceModel` | string | Names the independence proxy that produced the scores (`"distinct-reviewer-count"` in v1). |
| `items` | array | Ranked tension items, highest first. |
| `items[].kind` | string | One of `severity_split`, `solo_finding`, `gray_zone`, `verification_disagreement`. |
| `items[].file` / `line` | string / int | Location of the tension. |
| `items[].severity` | string | The finding's (max) severity. |
| `items[].problem` | string | The contested problem statement. |
| `items[].score` | number | Ranking score (see Scoring). |
| `items[].spread` | int | Severity tier distance (0 when there is no severity disagreement). |
| `items[].independence` | int | Distinct-reviewer count. |
| `items[].reviewers` | string[] | Distinct reviewers (omitted when empty). |
| `items[].disagreement` | string | `"<lo> vs <hi>"` for severity splits (omitted otherwise). |
| `items[].skeptics` | string | Comma-joined skeptics on a verification tie (omitted otherwise). |
| `items[].detail` | string | Gray-zone similarity, or combined skeptic reasoning (omitted otherwise). |
| `items[].positions` | array | Side-by-side reviewer stances (`reviewer`, `severity`, `problem`) — populated for gray-zone pairs, whose member findings are unmerged. |

### Snapshot semantics

`disagreements.json` is written at reconcile time. Because the verify stage runs
after reconcile, the **file** reflects the reconcile-time tension and does not
include the `verification_disagreement` class. The **live** radar
(`atcr report --disagreements` and the `report.md` section) reads the embedded
verification blocks from `findings.json` and surfaces verification disagreements
whenever a verified review is present. A consumer that needs the verification
tier in structured form should call the radar builder over the current
`findings.json` rather than rely on the snapshot file.

### Evolution policy

The schema is additive-only within `schemaVersion: 1.x`: new optional item fields
may be added, but existing field names and the scoring contract do not change
under `1.x`. Any breaking change increments `schemaVersion`.
