# Concept: Review-as-a-Service API

**Status:** Conceptual
**Created:** 2026-06-14
**Priority:** High (scalable revenue)

## Problem

ATCR is a CLI tool. To use it, teams need to:
- Install the binary
- Configure provider keys
- Write a `registry.yaml`
- Run it in CI or locally
- Parse the output

This is fine for developers who want full control. But many teams just want an API call: POST a diff, get back a multi-model reconciled review. They don't want to manage infrastructure; they want a service.

## Solution

**Hosted API.** POST a diff, get back a multi-model reconciled review. No CLI, no setup, no infrastructure.

### What it looks like

```bash
curl -X POST https://api.atcr.dev/v1/review \
  -H "Authorization: Bearer $ATCR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "diff": "...",
    "base": "abc123",
    "head": "def456",
    "personas": ["bruce", "greta", "kai"],
    "fail_on": "HIGH"
  }'
```

Response:
```json
{
  "review_id": "rev_abc123",
  "status": "completed",
  "findings": [
    {
      "severity": "HIGH",
      "file": "auth.go",
      "line": 42,
      "problem": "Missing error check",
      "fix": "Add error check after database call",
      "reviewers": ["bruce", "greta"],
      "confidence": "HIGH"
    }
  ],
  "ambiguous": [...],
  "summary": {
    "total_findings": 5,
    "by_severity": {"HIGH": 1, "MEDIUM": 3, "LOW": 1},
    "cost_usd": 0.12,
    "latency_ms": 15000
  }
}
```

### Why this works

- You already have the infrastructure (ATCR CLI + reconciler + personas).
- The API is a thin wrapper around the existing CLI.
- Teams pay for convenience, not features.
- Recurring revenue (subscription or per-review).

### Pricing models

- **Per-review:** $0.10-0.50 per review (depending on diff size, number of personas).
- **Subscription:** $99/mo for 1000 reviews, $499/mo for 10,000 reviews.
- **Enterprise:** Custom pricing for high-volume users, SLA, dedicated infrastructure.

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| API wrapper | HTTP API around `atcr review` + `atcr reconcile` | 1 week |
| Authentication | API key management, rate limiting | 3 days |
| Billing | Per-review or subscription billing (Stripe) | 1 week |
| Dashboard | Basic usage dashboard (reviews run, cost, findings) | 1 week |
| Documentation | API docs, examples, SDKs (Python, JS) | 1 week |
| **Total** | | **4-6 weeks** |

## Revenue Model

**SaaS, scalable:**
- Per-review: $0.10-0.50 per review
- Subscription: $99-499/mo
- Enterprise: custom pricing ($1k-10k/mo)

**Why this is the fastest path to scalable revenue:**
- The API is a thin wrapper; most of the work is billing + dashboard.
- Recurring revenue (subscriptions) or usage-based revenue (per-review).
- Teams pay for convenience, not features.
- You're not competing with Copilot/Cursor (they're IDE tools); you're competing with manual review.

**Revenue projections:**
- 100 customers × $199/mo = $19.9k/mo ($239k/year)
- 1000 customers × $99/mo = $99k/mo ($1.19M/year)
- 10 enterprise customers × $5k/mo = $50k/mo ($600k/year)

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| API wrapper | 1 week | HTTP API around `atcr review` + `atcr reconcile` |
| Authentication | 3 days | API key management, rate limiting |
| Billing | 1 week | Stripe integration, per-review or subscription |
| Dashboard | 1 week | Basic usage dashboard (reviews run, cost, findings) |
| Documentation | 1 week | API docs, examples, SDKs (Python, JS) |
| Deployment | 3 days | Docker, CI/CD, monitoring |
| **Total** | **4-6 weeks** | |

## Moat / Differentiation

- **You have the infrastructure.** ATCR + reconciler + personas are built.
- **The API is a thin wrapper.** Most of the work is billing + dashboard, not core tech.
- **Recurring revenue.** Subscriptions or usage-based; predictable revenue.
- **You're not competing with Copilot.** They're IDE tools; you're a review API.
- **The reconciler is the moat.** Deterministic merge, confidence scoring, disagreement preservation.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| SaaS is a different business | High | High | Start small; use consulting revenue to fund SaaS development |
| API costs are high (LLM API calls) | Medium | Medium | Optimize for cost; use cheaper models for low-severity findings |
| Teams want self-hosted, not SaaS | Medium | Medium | Offer both (SaaS + Team Edition); let teams choose |
| Low adoption | Medium | High | Start with your network; offer free tier to generate case studies |
| Competitors copy the API | Low | Medium | The moat is the reconciler + personas, not the API wrapper |

## Open Questions

- **Tech stack?** Go + React (native), or a framework like Wasp?
- **Hosting?** AWS, GCP, Fly.io, Railway?
- **Pricing model?** Per-review, subscription, or both?
- **Free tier?** How many free reviews to generate case studies?
- **SLA?** What uptime guarantee can you offer?
- **Data privacy?** Do you store diffs, or process and discard?

## Why This Is the Fastest Path to Scalable Revenue

1. **You already have the infrastructure.** ATCR + reconciler + personas are built.
2. **The API is a thin wrapper.** Most of the work is billing + dashboard, not core tech.
3. **Recurring revenue.** Subscriptions or usage-based; predictable revenue.
4. **Teams pay for convenience.** They don't want to manage infrastructure; they want an API call.
5. **You're not competing with Copilot.** They're IDE tools; you're a review API.
6. **The reconciler is the moat.** Deterministic merge, confidence scoring, disagreement preservation.

**Time to first dollar:** 6-8 weeks (if you start today).

**Time to $10k MRR:** 3-6 months (if you can get 50-100 customers).

## Relationship to Other Concepts

- **Team Edition** — the self-hosted alternative. Offer both; let teams choose.
- **Model Selection Consulting** — consulting clients become SaaS customers.
- **Finding Remediation** — could be a premium tier (auto-fix add-on).

## References

- CodeRabbit, Qodo, Copilot — competitors (different approaches, same market)
- Stripe — SaaS billing infrastructure
