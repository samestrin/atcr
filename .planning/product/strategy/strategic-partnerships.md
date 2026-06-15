# Strategic Partnerships

**Last Updated:** 2026-06-14
**Status:** Active exploration

This document tracks potential strategic partnerships that align with ATCR's OSS-first, adoption-focused strategy.

---

## Partnership Thesis

**Complementary product partnerships** accelerate adoption by solving a user problem (low-cost multi-model access) while generating revenue (referrals) that funds OSS development.

The ideal partner:
- Provides something ATCR needs (multi-model access, low cost, flat rate)
- Benefits from ATCR's adoption (more users = more revenue)
- Aligns with OSS values (developer-friendly, transparent pricing)
- Generates referral revenue (funds OSS development)

---

## Active Partnerships

### Synthetic.new

**Status:** In communication
**Type:** LLM provider (multi-model, flat-rate)
**Integration:** Already integrated via llm-env

#### What They Offer

- **Flat-rate pricing:** Starting at $30/mo (predictable costs for users)
- **Multi-model access:** 9 models + 4 stable aliases
- **Changing model names:** Model names change, but aliases are stable
- **GitHub action:** Daily check updates model mapping (already implemented in llm-env)
- **Referral program:** You already have a referral code in llm-env quickstart

#### Why This Is a Strong Fit

**ATCR needs:**
- Multi-model access (core value is consensus across models) ✓
- Low-friction onboarding (OSS adoption depends on it) ✓
- Cost predictability (teams hate variable API costs) ✓

**Synthetic provides:**
- 9 models + 4 stable aliases (multi-model ✓)
- Flat-rate $30/mo (cost predictability ✓)
- You already have integration + referral code (low friction ✓)

#### Strategic Play

**1. Make Synthetic the "Recommended" Provider**

Build ATCR defaults around the 4 stable aliases:

```yaml
# Default registry.yaml
providers:
  - name: synthetic
    models:
      - alias: stable-1  # Maps to current best model
      - alias: stable-2
      - alias: stable-3
      - alias: stable-4
```

Quickstart becomes:
1. Sign up for synthetic (referral link)
2. Set `SYNTHETIC_API_KEY`
3. `atcr review` — done

This is **frictionless onboarding**. Compare to the current flow: "Sign up for OpenAI, Anthropic, Google, configure 3 providers, manage 3 API keys, worry about costs..."

**2. Negotiate a Formal Partnership**

Propose to synthetic.new:

- **Better rates for ATCR users** — $20/mo instead of $30/mo (or a special "ATCR tier")
- **Co-marketing** — synthetic promotes ATCR in their docs, ATCR promotes synthetic in quickstart
- **Integration support** — synthetic ensures the 4 aliases stay stable, ATCR ensures the integration works
- **Revenue share** — referral code generates recurring revenue (if not already)

This is a classic **complementary product partnership**. Synthetic needs users; ATCR needs a low-cost multi-model provider. Win-win.

**3. The Daily Model Check Is Already Solved**

You already have this in llm-env (GitHub action checks models daily, updates JSON). This solves the "model names change" problem. ATCR can use the same approach:

- Use the 4 stable aliases in the default registry
- GitHub action updates the mapping daily (alias → current model)
- Users don't care that "stable-1" is actually "claude-3-5-sonnet-20241022" today and might be something else tomorrow

**4. Don't Lock In (But Make It Easy)**

Make synthetic the **recommended** provider, not the **only** provider. Keep supporting OpenAI, Anthropic, Google, etc. This avoids vendor lock-in concerns while making synthetic the path of least resistance.

#### Revenue Angle

**Referral revenue:**
- Every ATCR user who signs up for synthetic via your referral code = recurring revenue for you
- If synthetic offers 20% referral commission, that's $6/user/month
- 1000 ATCR users × $6/month = $6k/month passive income

**This funds OSS development.** The referral revenue keeps the lights on while you build the product.

#### Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Vendor dependency | Medium | High | Keep support for other providers (OpenAI, Anthropic, Google); don't hard-code synthetic into the core |
| Synthetic changes pricing/terms | Low | High | Have a fallback plan (switch recommended provider if needed) |
| Synthetic goes out of business | Low | High | Keep support for other providers; don't build critical dependencies |
| Model aliases become unstable | Low | Medium | Use the daily GitHub action to update mapping; have a manual override |

#### Action Plan

1. **Reach out to synthetic.new** — propose a formal partnership (better rates, co-marketing, revenue share)
2. **Build ATCR defaults around the 4 stable aliases** — make synthetic the recommended provider
3. **Update the quickstart** — "Sign up for synthetic, set API key, run `atcr review`"
4. **Co-market** — synthetic promotes ATCR, ATCR promotes synthetic
5. **Document the integration** — examples, blog post, case study
6. **Track referral revenue** — this funds OSS development

#### Timeline

- **Month 1-2:** Reach out to synthetic, negotiate partnership terms
- **Month 3:** Build ATCR defaults around synthetic aliases, update quickstart
- **Month 4:** Launch co-marketing (blog post, social media)
- **Month 5-6:** Track adoption, referral revenue, adjust strategy

#### Success Criteria

- Partnership agreement signed
- Synthetic is the recommended provider in ATCR quickstart
- 100+ ATCR users sign up for synthetic via referral code by month 6
- $500+/month referral revenue by month 6 (funds OSS development)
- Co-marketing content published (blog post, social media)

---

## Evaluated But Not Pursued

### Featherless

**Type:** LLM provider (flat-rate)
**Status:** Evaluated, not pursuing
**Reason:** Limited context windows, limited concurrent connections
**Why it doesn't fit:** ATCR's multi-model approach requires multiple concurrent API calls and sufficient context window for code review. Featherless's limitations make it unsuitable for ATCR's use case.

### Chutes

**Type:** LLM provider (flat-rate)
**Status:** Evaluated, not pursuing
**Reason:** Limited context windows, limited concurrent connections
**Why it doesn't fit:** Same as Featherless. ATCR's multi-model approach requires more capacity than Chutes provides.

---

## Future Partnership Opportunities

### IDE/Editor Integrations

**Potential partners:** VS Code, JetBrains, Cursor
**Type:** Distribution channel
**Value:** ATCR as a plugin/extension, IDE-integrated code review
**Status:** Not yet explored
**Timeline:** Phase 3-4 (after SaaS API is built)

### CI/CD Platforms

**Potential partners:** GitHub Actions marketplace, GitLab, CircleCI
**Type:** Distribution channel
**Value:** Official ATCR templates, featured in marketplace
**Status:** Not yet explored (but CI Integration concept is Phase 1)
**Timeline:** Phase 1-2 (CI Integration is a prerequisite)

### Compliance/Security Platforms

**Potential partners:** Vanta, Drata, Secureframe (SOC 2 automation)
**Type:** Integration partner
**Value:** ATCR's "Survived-a-Skeptic" certification integrates with compliance automation
**Status:** Not yet explored
**Timeline:** Phase 4 (after certification is built)

### Model Vendors

**Potential partners:** OpenAI, Anthropic, Google, Mistral
**Type:** Technical partnership
**Value:** Early access to new models, co-marketing, sponsored evals
**Status:** Not yet explored
**Timeline:** Phase 3-4 (after leaderboard has traction)

---

## Partnership Evaluation Criteria

When evaluating future partnerships, use these criteria:

1. **Alignment with ATCR's needs** — Does the partner provide something ATCR needs (distribution, cost reduction, features)?
2. **Alignment with partner's needs** — Does ATCR provide something the partner needs (users, content, integration)?
3. **OSS values alignment** — Does the partner align with OSS values (developer-friendly, transparent)?
4. **Revenue potential** — Does the partnership generate revenue (referrals, co-marketing, integration fees)?
5. **Risk assessment** — What are the risks (vendor dependency, reputation, technical debt)?
6. **Effort required** — How much effort is needed to build and maintain the partnership?

Score each criterion 1-5, prioritize partnerships with the highest total score.

---

## See Also

- `monetization-roadmap.md` — the 4-phase monetization plan (partnerships are part of Phase 2)
- `../concepts/README.md` — the 12 product concepts
- `../concepts/competitive-analysis.md` — competitive landscape
- `../roadmap/README.md` — the engineering ladder (Epics 1.0→5.0)
