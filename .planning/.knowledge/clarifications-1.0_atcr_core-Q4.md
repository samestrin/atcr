---
id: mem-2026-06-11-a08eb5
question: "How should atcr's adjudication-sensibility verification (gray-zone ambiguous.json clusters) be exercised when a real review run cannot guarantee clusters in the Jaccard gray band?"
created: 2026-06-11
last_retrieved: ""
sprints: [1.0_atcr_core]
files: [internal/reconcile/dedupe.go, skill/SKILL.md, .planning/sprints/active/1.0_atcr_core/plan/acceptance-criteria/05-04-adversarial-review-and-adjudication.md]
tags: [clarifications, sprint-1.0_atcr_core, testing, reconciler, adjudication, verification]
retrievals: 0
status: active
type: clarifications
---

# How should atcr's adjudication-sensibility verification (gra

## Decision

Satisfy opportunistically on the first real review run that yields ambiguous.json clusters; do not block on it. If a real run yields an empty clusters array, fall back to a deterministic synthetic seed: hand-authored sources/*/findings.txt files with same-location problem texts crafted to land in the Jaccard 0.4-0.7 gray band, run through real `atcr reconcile`. Key insight: a purpose-built "synthetic code range" cannot guarantee gray-zone clusters because the band depends on model wording, not the diff — only seeded findings files give the guarantee. Shipping unverified carries no correctness risk: unadjudicated gray-zone clusters remain unmerged (conservative default), and adjudication is explicitly optional in the Skill.

Justification:
- Thresholds are fixed integer-arithmetic boundaries (merge >= 0.7, gray >= 0.4) in internal/reconcile/dedupe.go:16-17,147-149 — a seeded corpus is trivially constructible.
- TD FIX text is conditional ("During a real review run that yields ambiguous.json clusters...") — .planning/technical-debt/README.md:97.
- Conservative default: "False positives in CI gates are worse than false negatives" — 05-04-adversarial-review-and-adjudication.md:32; skill/SKILL.md:94; adjudication marked "(optional)" at skill/SKILL.md:92.
- Plan anticipates real-run corpora accumulating post-v1: "Tune with fixture corpora from real multi-agent runs" — plan/plan.md:134; original-requirements.md:251.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/dedupe.go
- skill/SKILL.md
- .planning/sprints/active/1.0_atcr_core/plan/acceptance-criteria/05-04-adversarial-review-and-adjudication.md
