# `repo-state-v1` case format

This document is the complete specification for authoring a `repo-state-v1` benchmark case. It is deliberately self-contained: everything needed to write a conforming case is here, and no part of it requires reading Go source. The loader, matcher, and scorer that consume this format are epic 35.16.10's work; they implement this document rather than redefining it.

**Status:** the format is fixed as of epic 35.16.7. Cases authored against it are hand-verifiable today and become machine-runnable when 35.16.10 lands the loader.

## Why this is a new suite and not `standard-v2`

The only benchmark suite that exists today is `benchmarks/standard-v1/`: a flat directory of `.diff` files plus a `suite.json` manifest that maps each case id to one diff file and a list of expected categories. That shape encodes an assumption — **a case is a diff and nothing else** — and every consumer of it inherits that assumption.

A `repo-state-v1` case is not a diff. It is a small repository: a base tree of files as they existed before the change, a commit message asserting what the change does, and a diff applied on top of that tree. The whole point of the tier is to evaluate review behavior that depends on repository state a diff cannot carry — code the diff does not touch, and an author's claim about the change that lives in git metadata rather than in the change itself.

A base tree has no place in a flat `.diff` directory. Bolting it onto `standard-v1` would mean either a manifest whose case entries have two incompatible shapes, or a directory whose files mean different things depending on which case claims them. Both make the existing suite harder to read in exchange for reusing a manifest file. A separate suite directory with its own format costs one directory and keeps each suite's shape honest about what its cases are.

## Directory layout

A suite is a directory. Each case is a subdirectory of it, named by the case id.

```
benchmarks/repo-state-v1/
├── FORMAT.md                      this document
├── suite.json                     suite manifest (see "Suite manifest")
└── <case-id>/                     one directory per case
    ├── case.json                  case manifest (required)
    ├── README.md                  human-readable description (required)
    ├── commit-message.txt         the claim source (required)
    ├── change.diff                the change under review (required)
    └── base/                      the base tree (required)
        └── ...                    ordinary files, any depth
```

The manifest's file-reference fields — `base_tree`, `commit_message`, and `diff` — are relative to the case directory, use POSIX `/` separators, and must not escape it (no leading `/`, no `..` segment). `expected_findings[].file` is the deliberate exception: it is a repository-relative path in the **head state** — the tree after the diff applies — and resolves against the materialized head tree, not the case directory.

## The base tree

`base/` holds the repository as it exists **before** the change, with `base/` itself standing in for the repository root. A file at `base/streamer/cursor.py` is the repository file `streamer/cursor.py`.

Prune it. Include the file the change edits, the files that call into it, and whatever else a reviewer genuinely needs to adjudicate the case — nothing more. A case is worth authoring only if a reader can hold its whole tree in their head; a tree that needs its own tour has stopped being a fixture and become a repository.

The tree may be synthetic (hand-written for the case) or vendored from a real project. Vendored content carries its upstream license, so a case with vendored files must ship a `NOTICE.md` in its case directory naming the source and its license, following the precedent of `benchmarks/standard-v1/NOTICE.md`.

`base/` must not contain a `.git` directory. The loader creates the repository; the case supplies only the working tree.

## The commit message

`commit-message.txt` is the raw text of the commit message applied with the change. It is the case's **claim source** — the thing the reviewer is asked to check the diff against — so it is a plain file rather than a manifest string: real commit messages carry blank lines, bullets, and trailers, and a JSON string field invites authors to flatten all three away.

The first line is the subject. Everything after the first blank line is the body. The file is used verbatim, including trailing whitespace and trailers, so a case can exercise trailer handling by including one.

## The change

`change.diff` is a unified diff applied to `base/` to produce the head state. It must apply cleanly to `base/` with `git apply` from the repository root and must use `a/` and `b/` path prefixes, which is what `git diff` produces by default.

The diff is the only thing that changes the tree. A case that needs a file added, deleted, or renamed expresses it in the diff, not by shipping a second tree.

## `case.json`

The example below is deliberately fictional — its ids and line numbers name no real case. For a conforming case you can read end to end, see [`claim-absent-cursor-fix/case.json`](claim-absent-cursor-fix/case.json).

```json
{
  "id": "example-case",
  "format": "repo-state-v1",
  "base_tree": "base",
  "commit_message": "commit-message.txt",
  "diff": "change.diff",
  "expected_findings": [
    {
      "id": "example-finding",
      "file": "pkg/example.py",
      "line_start": 40,
      "line_end": 40,
      "line_tolerance": 3,
      "outside_diff": true,
      "category": "correctness",
      "summary": "One sentence stating the planted defect concretely enough that a third party can confirm it is really present."
    }
  ]
}
```

### Case fields

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `id` | string | yes | Case id. Must equal the case directory name. Lowercase, digits, and `-` only. |
| `format` | string | yes | Always the literal `"repo-state-v1"`. A case that predates a future format revision is identified by this field, not by its directory. |
| `base_tree` | string | yes | Path to the base tree directory. Conventionally `"base"`. |
| `commit_message` | string | yes | Path to the commit-message file. Conventionally `"commit-message.txt"`. |
| `diff` | string | yes | Path to the unified diff. Conventionally `"change.diff"`. |
| `expected_findings` | array | yes | One entry per defect the case plants. Must not be empty — a case with nothing to find scores nothing. |

### `expected_findings[]` fields

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `id` | string | yes | Finding id, unique within the case. Lowercase, digits, and `-` only. |
| `file` | string | yes | Repository-relative path of the line that settles the finding, **without** the `base/` prefix. |
| `line_start` | integer | yes | First line of the settling range, 1-based, in the **head** state (after the diff applies). |
| `line_end` | integer | yes | Last line of the settling range, 1-based, inclusive. Equals `line_start` for a single line. |
| `line_tolerance` | integer | no | Match tolerance in lines. Defaults to `3` when omitted. |
| `outside_diff` | boolean | yes | `true` when the settling line is **not** among the diff's added or removed lines. |
| `category` | string | yes | Expected finding category, from atcr's closed category vocabulary (for example `correctness`, `security`, `performance`, `maintainability`). |
| `summary` | string | yes | One sentence stating the defect concretely enough that a third party can confirm it is really present in the case. |

### The matching rule

A reported finding **matches** an expected finding when both hold:

1. The reported file path equals `file` exactly, compared as a repository-relative POSIX path.
2. The reported line `L` satisfies `line_start - line_tolerance <= L <= line_end + line_tolerance`.

The tolerance exists because a reviewer pointing at a defect often cites the line above or below it — the function signature rather than the offending assignment. `±3` is the same window `llm_support_td_dedupe` uses to cluster `FILE:LINE` findings across reviewers, reused here so a single convention governs "these two reports are about the same place" everywhere in the toolchain.

A reported finding matches at most one expected finding: when several are in range, the one whose range midpoint is nearest wins, and an exact `id` tie breaks alphabetically. Every expected finding is matched at most once, so N reports of the same defect score as one hit and not N.

### `outside_diff`

`outside_diff` is a property of the finding, not of the case, and it is the field the tier exists for.

A finding with `outside_diff: false` is settled by a line the diff itself contains. Any reviewer reading only added and removed lines can reach it.

A finding with `outside_diff: true` is settled by a line the diff does **not** touch. It is reachable only by a reviewer that consulted repository state beyond the diff — because the claim named a behavior the diff never changed, or because the defect is an *absence* and an absent change leaves no trace in a diff. Scoring reports these separately from in-diff findings: a run that scores well on in-diff findings and zero on `outside_diff` ones has demonstrated exactly the gap this tier measures, and collapsing the two into one number would hide it.

## Suite manifest

`suite.json` lists the cases the suite contains.

```json
{
  "suite": "repo-state-v1",
  "suite_version": "1.0.0",
  "cases": [
    { "id": "example-case", "dir": "example-case" }
  ]
}
```

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `suite` | string | yes | Always `"repo-state-v1"`. |
| `suite_version` | string | yes | Semantic version of the suite's contents. Adding a case is a minor bump; correcting a case's expected findings is a patch bump. |
| `cases[].id` | string | yes | Case id. Must equal the `id` in that case's `case.json`. |
| `cases[].dir` | string | yes | Case directory, relative to the suite directory. Conventionally equal to the id. |

## Authoring checklist

A conforming case satisfies all of these. Each is checkable by reading the case directory alone.

- [ ] The case directory name, `case.json`'s `id`, and the suite manifest entry all agree.
- [ ] `case.json` parses as JSON and carries every required field.
- [ ] `base/` contains no `.git` directory and no file outside the case's stated purpose.
- [ ] `change.diff` applies cleanly to `base/` and uses `a/`/`b/` prefixes.
- [ ] `commit-message.txt` has a subject line and, where the case is about a claim, a body that states that claim.
- [ ] Every `expected_findings[]` entry names a `file` that exists in the head state, with `line_start`/`line_end` valid in that state.
- [ ] Every `expected_findings[]` entry's `outside_diff` value is correct — verified by checking whether the cited lines appear as added or removed lines in `change.diff`.
- [ ] `README.md` states what the case plants, why it is hard, and — until 35.16.10 lands the loader — that the case is authored but not yet machine-runnable.
- [ ] A case with vendored content ships a `NOTICE.md` naming the upstream source and license.
