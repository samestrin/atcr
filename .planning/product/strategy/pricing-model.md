# ATCR Pricing and Licensing Strategy

Based on the recent shift to a Two-Repo Open Core architecture and the competitive positioning against incumbents, here is the recommended pricing and licensing strategy for ATCR.

## The Core Challenge: BYO-Keys
Because ATCR's unique value proposition is privacy and running locally/in-CI, users bring their own API keys (BYO-Keys) or run local models (Ollama). 
- **CodeRabbit** charges **$24/user/mo**, but *they* pay for the LLM inference.
- **ATCR** users are providing the compute (either paying OpenAI directly or running local GPUs). Therefore, ATCR's pricing must reflect that we are providing **workflow, governance, and reconciliation**, not raw inference.

---

## 1. The Licensing Model

We will use a classic Open Core licensing split to maximize top-of-funnel adoption while capturing enterprise value.

### The Core: MIT License (Free)
- **Repo:** `samestrin/atcr`
- **Why:** Developers demand MIT/Apache for foundational CLI tools. If the core isn't permissively licensed, you won't get the viral adoption necessary to build a category leader.

### The Enterprise Repo: Commercial License
- **Repo:** `samestrin/atcr-enterprise`
- **Why:** The enterprise features (Audit Logging, Jira Integrations, Accountability Gates) are strictly B2B value-adds. Access to this repository is granted via an annual SaaS contract or a paid tier. 

---

## 2. Pricing Tiers

### Tier 1: Community (Free Forever)
- **Target:** Open-source maintainers, indie hackers, and small dev teams.
- **Features:** The multi-model Reconciler engine, standard personas (Bruce, Kai, Penny, Sasha), local Model-Eval Leaderboard, and basic GitHub PR comments.

### Tier 2: Pro / Teams ($15 / user / month)
- **Target:** Startups and mid-market teams (10-50 developers).
- **Features:** Advanced Integrations (Jira, Linear, GitLab), Custom Private Personas, and Team-level Dashboards.
- **Why it works:** At $15/mo, it's roughly 40% cheaper than CodeRabbit. Because teams bring their own keys, their total cost (ATCR + Tokens) will roughly equal CodeRabbit, but they get the absolute privacy and multi-model consensus that CodeRabbit lacks.

### Tier 3: Enterprise ($15,000+ / year Flat Rate)
- **Target:** Regulated industries (Fintech, Healthcare, Defense).
- **Features:** Epic 34.0 (Immutable Audit Logging), Epic 36.0 (Protected Paths), Epic 37.0 (Accountability Gates).
- **Why it works:** Enterprises despise per-seat tracking. A flat $15k-$30k annual fee is easily swiped on a Director's corporate card if it solves a SOC2 or FedRAMP compliance bottleneck.

---

## 3. Strategic Pushback: Local vs. Cloud Models

**Should ATCR be priced differently if a team is using Local Models (e.g., Ollama) instead of Cloud APIs?**

**Absolutely not. The price remains exactly the same.** 

The value ATCR provides is **not** the LLM. ATCR's value is the **Deterministic Reconciler**, the **Multi-Model Panel**, and the **Enterprise Workflows** (Jira integration, Audit Logs, PR gates). Whether the customer chooses to route that orchestration to a local GPU or to OpenAI is their infrastructure choice. If they use local models, they save *themselves* money on API costs, but ATCR's software value remains 100% unchanged. Do not cannibalize your own pricing because the user found a cheaper way to run the compute.

---

## 4. The Broader Competitive Landscape

We are not just positioning against CodeRabbit. The $15/mo and $15k/yr tiers also counter the rest of the market:

- **CodiumAI PR-Agent:** They are open source but operate as a heavy Python/Webhook application. ATCR wins by being a hyper-fast, pure Go binary that runs anywhere, and by enforcing **multi-model consensus** (Codium is single-model). Codium charges ~$19/user/mo for their pro tier, making our $15/mo highly competitive.
- **Gitar.ai:** They are a SaaS that focuses on auto-applying fixes silently to PRs. ATCR wins on **Zero-Trust Privacy**. Gitar requires read/write access to your entire codebase via a GitHub App. ATCR runs locally or in *your* CI, keeping your code entirely within your VPC. For security-conscious teams, Gitar is a non-starter and ATCR is the only option.

---

## 5. Revenue Math (The 18-Month Projection)

- **Pro Tier:** 50 teams of 10 developers = 500 seats @ $15/mo = **$90,000 / year ARR**.
- **Enterprise Tier:** 5 enterprise logos @ $15k/year = **$75,000 / year ARR**.
- **Consulting:** 2 Model-Selection engagements @ $10k = **$20,000**.
- **Total:** **$185,000 ARR** with a highly conservative customer base (just 55 paying companies).
