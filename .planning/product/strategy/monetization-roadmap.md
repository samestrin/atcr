# ATCR Monetization Roadmap

**Last Updated:** 2026-06-14
**Strategy:** OSS-first, services bridge, product scales
**Timeline:** 18 months to sustainable revenue

---

## Strategic Thesis

**Focus on OSS, get adoption, then monetize.** ATCR has real potential in a genuine niche: multi-model code review with deterministic reconciliation and adversarial verification. The strategy is:

1. **OSS builds adoption** — developers trust OSS, try it, contribute, advocate
2. **Services fund development** — consulting generates revenue without breaking OSS trust
3. **Product scales the business** — SaaS and enterprise licenses convert adoption to revenue
4. **Premium features create the moat** — auto-fix, analytics, certification create category leadership

**The niche is defensible:**
- Multi-model + deterministic reconciler + adversarial verification is a moat
- Most competitors are single-model or rule-based
- Blue ocean opportunities: certification, consulting, reconciler library
- The leaderboard can become the cited benchmark (standards gravity)

**The risk:** Running out of money before adoption converts to revenue. Mitigate by starting consulting early, keeping burn low, and focusing on the leaderboard as the magnet.

---

## Phase 1: Build the Magnet (0-3 months)

**Goal:** Get users, get attention, get the leaderboard cited.
**Revenue:** $0 (investment phase)
**Success Criteria:** 100+ GitHub stars, 10+ external contributors, leaderboard cited in 3+ blog posts/articles

### What to Build

| Concept | Effort | Purpose |
|---------|--------|---------|
| **Disagreement Radar** | 1.5 wks | The talk hook — "the tool that shows you where AI reviewers disagree" |
| **Local Model-Eval Leaderboard** | 2 wks | The eval magnet — `atcr leaderboard` CLI, per-run scorecard |
| **CI Integration thin slice** | 1 wk | Table stakes — GitHub Action + `--fail-on` |
| **Persona Ecosystem seed** | 1 wk | 10-15 high-quality community personas |

### Why This Order

1. **Disagreement Radar** is pure differentiation — no one else does this. It's the demo, the blog post, the Hacker News discussion.
2. **Local Leaderboard** is the unique asset — every team asks "which model should I use?" The leaderboard answers this. It's the magnet that pulls users in.
3. **CI Integration** is table stakes — without it, adoption is friction-heavy. Ship the thin slice (GitHub Action + `--fail-on`) and move on.
4. **Persona Ecosystem** widens the model×persona matrix the Leaderboard scores — more personas make the eval richer.

### Key Milestones

- **Month 1:** Disagreement Radar lands, blog post published, Hacker News discussion
- **Month 2:** Local Leaderboard lands, 50+ GitHub stars, first external contributor
- **Month 3:** CI Integration lands, 100+ GitHub stars, 10+ external contributors

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Low adoption | Medium | High | Focus on the leaderboard as the magnet; it's the unique asset |
| Disagreement is mostly noise | Medium | Medium | Rank by reviewer strength; only surface splits among trusted reviewers |
| CI Action becomes maintenance burden | Low | Medium | Keep it simple — just a Docker wrapper around `atcr` |

---

## Phase 2: Services Fund the OSS (3-6 months)

**Goal:** Generate revenue to fund development. Sell expertise, not the tool.
**Revenue:** $10k-100k (consulting + referral revenue)
**Success Criteria:** 5+ consulting clients, $30k+ revenue, 200+ GitHub stars

### What to Sell

| Service | Price | Effort | Target |
|---------|-------|--------|--------|
| **Model Selection Consulting** | $5k-20k/engagement | 1-2 wks | Teams building with LLMs, asking "which model should I use?" |
| **Enterprise Persona Development** | $10k-50k/persona | 2-3 wks | Regulated industries with domain-specific needs |
| **Strategic Partnerships (Referral Revenue)** | $6/user/month (20% commission) | Ongoing | ATCR users sign up for Synthetic.new via referral code |

### Strategic Partnerships

**Synthetic.new** is the primary strategic partnership (see `strategic-partnerships.md`):
- Flat-rate LLM access ($30/mo) with multi-model support
- Make Synthetic the "recommended" provider in ATCR defaults
- Referral revenue: $6/user/month for every ATCR user who signs up
- 1000 ATCR users × $6/month = $6k/month passive income
- This funds OSS development while you build the product

### Why Services

- **You already have the data** — the leaderboard is built; you just need to package it.
- **You already have the expertise** — you built ATCR; you're the expert.
- **It's fast** — 1-2 weeks per engagement, not months.
- **High margins** — the work is mostly automated (ATCR runs); you're selling knowledge, not labor.
- **It funds OSS** — consulting revenue keeps the lights on while you build the product.
- **Clients become users** — every consulting client is a potential OSS advocate.

### How to Find Clients

- **Network** — start with your network, offer one free evaluation as a case study
- **Blog posts** — write about model evaluation, share leaderboard insights
- **Conference talks** — present the leaderboard, the disagreement radar, the reconciler
- **Hacker News** — share the leaderboard results, engage in discussions

### Key Milestones

- **Month 4:** First consulting client, $5k-10k revenue, case study published
- **Month 5:** 3+ consulting clients, $20k+ revenue, blog post about model evaluation
- **Month 6:** 5+ consulting clients, $30k+ revenue, conference talk accepted

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Services don't scale | High | Medium | Use consulting to fund product development; transition to self-serve over time |
| Low demand | Medium | High | Start with your network; offer one free evaluation as a case study |
| Clients expect custom work | Medium | Medium | Standardize the engagement; say no to scope creep |

---

## Phase 3: Product Scales the Business (6-12 months)

**Goal:** Scalable revenue. Convert adoption to paying customers.
**Revenue:** $50k-500k (SaaS + enterprise)
**Success Criteria:** 50+ paying customers, $20k+ MRR, 500+ GitHub stars

### What to Build

| Product | Price | Effort | Target |
|---------|-------|--------|--------|
| **Review-as-a-Service API** | $99-499/mo | 4-6 wks | Teams that want an API call, not a CLI tool |
| **Team Edition + Compliance** | $10k-50k/year per org | 6-10 wks | Regulated industries that need self-hosted + audit trail |
| **Public Leaderboard** | Free | 3 wks | Top-of-funnel magnet — the lmarena-of-code-review |

### Why This Order

1. **Review-as-a-Service API** is the SaaS path — teams pay for convenience, not features. The API is a thin wrapper; most of the work is billing + dashboard.
2. **Team Edition + Compliance** is the enterprise play — regulated industries need self-hosted + audit trail + compliance reports. The compliance wrapper is the wedge.
3. **Public Leaderboard** is the top-of-funnel magnet — it's free, but it drives adoption of everything else.

### Key Milestones

- **Month 7:** Review-as-a-Service API beta, 5+ beta users
- **Month 8:** Review-as-a-Service API GA, 10+ paying customers, $2k+ MRR
- **Month 9:** Team Edition beta, 3+ beta users
- **Month 10:** Team Edition GA, 5+ paying customers, $5k+ MRR
- **Month 11:** Public Leaderboard launches, 500+ GitHub stars
- **Month 12:** 50+ paying customers, $20k+ MRR

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| SaaS is a different business | High | High | Start small; use consulting revenue to fund SaaS development |
| Enterprise sales cycle is long | High | Medium | Focus on self-serve first (Stripe billing, instant signup) |
| Low adoption | Medium | High | Start with your network; offer free tier to generate case studies |

---

## Phase 4: Premium Features + Enterprise (12+ months)

**Goal:** Moat + category creation. You're not just a tool; you're a platform.
**Revenue:** $100k-1M+ (platform revenue)
**Success Criteria:** 100+ paying customers, $50k+ MRR, leaderboard cited in 10+ articles/papers

### What to Build

| Feature | Price | Effort | Target |
|---------|-------|--------|--------|
| **Finding Remediation** | Premium tier | 4-6 wks | Teams that want auto-fix, not just detection |
| **Review Intelligence Analytics** | $99-499/mo | 4-6 wks | Engineering leaders who want benchmarking + trend analysis |
| **"Survived-a-Skeptic" Certification** | $500-2000/report | 2-3 wks (post-3.0) | Regulated industries that need to prove code quality |
| **Reconciler Library** | Dual license | 5-7 wks | Other tools that want to embed the reconciler |

### Why This Order

1. **Finding Remediation** is the premium tier — detection is a commodity; remediation is the value. It differentiates from Copilot/Cursor.
2. **Review Intelligence Analytics** is the data product — aggregate finding data across repos to answer "what are the most common bugs in Go codebases?"
3. **"Survived-a-Skeptic" Certification** is the compliance badge — regulated industries need to prove code quality. You're creating a new market.
4. **Reconciler Library** is the standards play — if the reconciler becomes widely adopted, ATCR is the reference implementation.

### Key Milestones

- **Month 13:** Finding Remediation beta, 5+ beta users
- **Month 14:** Finding Remediation GA, 10+ premium customers
- **Month 15:** Review Intelligence Analytics beta, 5+ beta users
- **Month 16:** "Survived-a-Skeptic" Certification launches, 3+ certifications issued
- **Month 17:** Reconciler Library extracted, 2+ external tools embedding it
- **Month 18:** 100+ paying customers, $50k+ MRR, leaderboard cited in 10+ articles/papers

### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Auto-fix is hard | High | High | Start with simple findings; expand gradually |
| Privacy concerns (analytics) | High | High | Strict anonymization; opt-in only |
| Market doesn't exist (certification) | Medium | High | Start with your network; offer free certifications |
| Low adoption (reconciler library) | High | Medium | Focus on OSS adoption first; white-label later |

---

## Revenue Projections

| Phase | Timeline | Revenue Source | Revenue Range | Cumulative |
|-------|----------|----------------|---------------|------------|
| Phase 1 | 0-3 months | $0 (investment) | $0 | $0 |
| Phase 2 | 3-6 months | Consulting + Referral Revenue | $10k-100k | $10k-100k |
| Phase 3 | 6-12 months | SaaS + Enterprise + Referral | $50k-500k | $60k-600k |
| Phase 4 | 12-18 months | Platform + Referral | $100k-1M+ | $160k-1.6M |

**Revenue sources:**
- **Consulting** — Model Selection, Enterprise Persona Development
- **Referral Revenue** — Synthetic.new partnership ($6/user/month)
- **SaaS** — Review-as-a-Service API subscriptions
- **Enterprise** — Team Edition + Compliance licenses
- **Platform** — Finding Remediation, Analytics, Certification

**Conservative scenario:** $160k in 18 months (enough to keep the lights on and fund OSS development)
**Optimistic scenario:** $1.6M in 18 months (enough to hire a team and scale)

---

## Key Metrics to Track

### Adoption Metrics
- GitHub stars (target: 500+ by month 12)
- External contributors (target: 20+ by month 12)
- Downloads / installs (target: 1000+ by month 12)
- Leaderboard submissions (target: 50+ by month 12)
- Blog posts / articles mentioning ATCR (target: 10+ by month 12)

### Revenue Metrics
- Consulting clients (target: 10+ by month 12)
- Consulting revenue (target: $50k+ by month 12)
- SaaS customers (target: 50+ by month 12)
- SaaS MRR (target: $20k+ by month 12)
- Enterprise customers (target: 5+ by month 12)
- Enterprise ARR (target: $50k+ by month 12)

### Product Metrics
- Reviews run (target: 10,000+ by month 12)
- Findings raised (target: 50,000+ by month 12)
- Findings corroborated (target: 30,000+ by month 12)
- Findings survived-skeptic (target: 10,000+ by month 18, post-3.0)

---

## Decision Points

### Month 3: Go/No-Go on Services
- **Go if:** 100+ GitHub stars, 10+ external contributors, 3+ blog posts about the leaderboard
- **No-Go if:** <50 GitHub stars, <5 external contributors, no blog posts
- **Action if No-Go:** Pivot to a different niche, or open-source the tool and move on

### Month 6: Go/No-Go on SaaS
- **Go if:** 5+ consulting clients, $30k+ revenue, 200+ GitHub stars
- **No-Go if:** <3 consulting clients, <$10k revenue, <100 GitHub stars
- **Action if No-Go:** Continue consulting, delay SaaS, or pivot to a different monetization strategy

### Month 12: Go/No-Go on Enterprise
- **Go if:** 50+ paying customers, $20k+ MRR, 500+ GitHub stars
- **No-Go if:** <20 paying customers, <$10k MRR, <300 GitHub stars
- **Action if No-Go:** Focus on SaaS, delay enterprise, or pivot to a different market

---

## The Moat

ATCR's defensible position is the **multi-model, deterministic reconciler with adversarial verification**. Most competitors are single-model or rule-based. The moat is:

1. **The reconciler** — deterministic clustering, dedupe, confidence scoring, disagreement preservation. No one else ships this.
2. **The leaderboard** — code-review-specific model evaluation, reproducible methodology, adversarial verification (post-3.0). No direct competitor.
3. **The certification** — "survived-a-skeptic" is a genuine quality signal. Creating a new market.
4. **The ecosystem** — personas, consulting, community. Network effects.

If the leaderboard becomes the cited benchmark (like SWE-bench for agents, or lmarena for chatbots), ATCR becomes the reference. That's standards gravity. Every team building with LLMs for code review will use ATCR because it's the benchmark. That's the moat.

---

## See Also

- `strategic-partnerships.md` — active and potential partnerships (Synthetic.new, future opportunities)
- `../concepts/README.md` — the 12 product concepts, ranked by ROI
- `../concepts/competitive-analysis.md` — competitive landscape for each concept
- `../roadmap/README.md` — the engineering ladder (Epics 1.0→5.0) these concepts ride on top of
