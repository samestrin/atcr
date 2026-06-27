# Technical Debt — Per-Item Files (Epic 12.1)

This directory holds one Markdown file per technical-debt item, generated from
the table in [`../README.md`](../README.md). It is the additive, human-friendly
representation introduced in Epic 12.1.

> **Source of truth:** `../README.md` (the flat table) remains authoritative and
> is what all tooling reads/writes. These files are a point-in-time snapshot and
> may drift after new rows are appended to the table. Do not hand-edit them
> expecting the change to flow back to the table — that cutover is deferred to
> the follow-on epic.

## File naming

`TD-NNNN-<slug>.md` — `NNNN` is a 4-digit sequence assigned in table order;
`<slug>` is derived from the Problem text for readability. Example:
`TD-0006-tool-gating-for-run-tests-run-script-lives-only-in.md`.

## Schema

YAML frontmatter (structured metadata) followed by a Markdown body
(free-form, multi-line text):

```markdown
---
id: TD-0006                 # stable item id (TD-NNNN)
order: 6                    # 1-based position in document order
section: '[2026-06-26] From Sprint: 11.0_executing_reviewers'  # verbatim source section header
date: "2026-06-26"          # date parsed from the section header
group: "3"                  # original table group id ("U" = ungrouped)
status: deferred            # open | resolved | deferred  (table [ ] / [x] / [/])
severity: MEDIUM            # CRITICAL | HIGH | MEDIUM | LOW
file: internal/tools/dispatch.go:146   # path:line citation
category: security
est_minutes: "120"
source: code-review
reviewers: claude           # "" when the source section has no reviewer columns
confidence: MEDIUM          # "" when the source section has no reviewer columns
has_review_cols: true       # whether the source section carried Reviewers|Confidence
---

## Problem

Free-form, may span multiple paragraphs.

## Fix

Free-form, may span multiple paragraphs.
```

`status` maps to the table checkbox: `open` → `[ ]`, `resolved` → `[x]`,
`deferred` → `[/]`.

## Regeneration

```
go run ./cmd/td-migrate migrate    # ../README.md table -> items/*.md (this dir)
go run ./cmd/td-migrate generate   # items/*.md -> table markdown on stdout
```

`migrate` writes by filename and does not prune; regenerate into a clean
directory if source Problem text (and therefore a slug) has changed.
