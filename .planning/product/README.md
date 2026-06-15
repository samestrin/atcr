# ATCR Product

**Last Updated:** 2026-06-14

This directory contains product planning, strategy, and concepts for ATCR.

---

## Directory Structure

```
product/
├── README.md (this file — overview)
├── concepts/
│   ├── README.md (index of 12 product concepts)
│   ├── competitive-analysis.md (competitor mapping)
│   └── [12 concept files — features, revenue models, effort]
├── strategy/
│   ├── README.md (strategic overview)
│   ├── monetization-roadmap.md (4-phase plan, revenue projections)
│   └── strategic-partnerships.md (Synthetic.new, future partnerships)
├── roadmap/
│   └── README.md (engineering roadmap, Epics 1.0→5.0)
├── content/
│   └── blog/
│       └── why-single-model-code-review-isnt-enough.md (lgtmaybe marketing angle)
├── pitch/
│   └── [pitch materials — TBD]
└── planning/
    └── 1-mvp/
        └── [MVP planning — TBD]
```

---

## Quick Navigation

### I want to understand the product strategy
→ Start with [`strategy/README.md`](strategy/README.md)

### I want to see the product concepts
→ Start with [`concepts/README.md`](concepts/README.md)

### I want to see the competitive landscape
→ Read [`concepts/competitive-analysis.md`](concepts/competitive-analysis.md)

### I want to see the monetization plan
→ Read [`strategy/monetization-roadmap.md`](strategy/monetization-roadmap.md)

### I want to see the strategic partnerships
→ Read [`strategy/strategic-partnerships.md`](strategy/strategic-partnerships.md)

### I want to see the engineering roadmap
→ Read [`roadmap/README.md`](roadmap/README.md)

### I want to see marketing content
→ Read [`content/blog/why-single-model-code-review-isnt-enough.md`](content/blog/why-single-model-code-review-isnt-enough.md)

---

## Strategic Summary

**Thesis:** OSS-first, get adoption, then monetize.

**The niche:** Multi-model code review with deterministic reconciliation and adversarial verification. Most competitors are single-model or rule-based. ATCR's defensible position is the multi-model, deterministic reconciler with adversarial verification.

**The 4 phases:**
1. **Build the magnet** (0-3 months) — Disagreement Radar, Local Leaderboard, CI Integration
2. **Services fund the OSS** (3-6 months) — Consulting, Custom Personas, Strategic Partnerships
3. **Product scales the business** (6-12 months) — SaaS API, Team Edition, Public Leaderboard
4. **Premium features + enterprise** (12+ months) — Auto-fix, Analytics, Certification, Reconciler Library

**Revenue projections:**
- Conservative: $160k in 18 months
- Optimistic: $1.6M in 18 months

**The moat:** Reconciler + Leaderboard + Certification + Ecosystem. If the leaderboard becomes the cited benchmark (like SWE-bench for agents), ATCR becomes the reference. That's standards gravity.

---

## Key Documents

| Document | Purpose |
|----------|---------|
| [`strategy/monetization-roadmap.md`](strategy/monetization-roadmap.md) | 4-phase plan with timeline, revenue projections, decision points |
| [`strategy/strategic-partnerships.md`](strategy/strategic-partnerships.md) | Active partnerships (Synthetic.new), evaluation criteria |
| [`concepts/README.md`](concepts/README.md) | Index of 12 product concepts, ranked by ROI |
| [`concepts/competitive-analysis.md`](concepts/competitive-analysis.md) | Competitive landscape for each concept |
| [`roadmap/README.md`](roadmap/README.md) | Engineering roadmap (Epics 1.0→5.0) |

---

## Recent Changes

**2026-06-14 (part 2):**
- Added lgtmaybe to competitive analysis (single-model competitor with known limitations)
- Created blog post draft: "Why Single-Model Code Review Isn't Enough" (uses lgtmaybe's admission as marketing angle)
- Added Epic 9.0: File Path Validation and Correction (addresses hallucinated file paths in TD items)

**2026-06-14 (part 1):**
- Added 6 new product concepts (Model Selection Consulting, Enterprise Persona Dev, Review-as-a-Service, Finding Remediation, Survived-a-Skeptic Certification, Review Intelligence Analytics)
- Created competitive analysis document
- Created monetization roadmap with 4-phase plan
- Created strategic partnerships document (Synthetic.new)
- Reorganized folder structure (strategy/, explorations/)
- Updated existing concepts with new angles (Team Edition + Compliance, Leaderboard + Consulting, Persona Ecosystem + Enterprise Dev)

**2026-06-11:**
- Initial 6 product concepts (Disagreement Radar, Model-Eval Leaderboard, CI Integration, Persona Ecosystem, Reconciler Library, Team Edition)
