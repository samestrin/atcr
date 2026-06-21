# Competitive Analysis: AI Code Review Space

This directory tracks the landscape of AI-assisted code review and defines **ATCR's counter-positioning**. Our primary differentiator is that we provide a deterministic, mathematically grounded **panel of reviewers** rather than a single omniscient bot, and we do so completely offline and locally.

## Us vs. Them

The table below highlights how ATCR compares to the leading products in the AI Code Review space.

| Feature / Dimension | **ATCR** | **Gitar.ai** | **CodeRabbit** | **CodiumAI PR-Agent** |
|--------------------|----------|--------------|----------------|-----------------------|
| **Architecture** | Local CLI Binary | GitHub App / SaaS | GitHub App / SaaS | Open Source / Orchestrator |
| **Review Strategy** | Multi-Model Consensus | Single-Model Omniscience | Single-Model Omniscience | Single-Model (Pluggable) |
| **Primary Output** | `report.md` (Audit Artifact) | Auto-commits branch fixes | PR Comments & Summaries | PR Comments & Summaries |
| **Remediation** | Offline, deterministic clustering | Validated against CI pipeline | "One-Click" PR applies | Chat-based `/improve` commands |
| **Data Privacy** | Zero-Trust (BYO-Keys) | SaaS Data Access | SaaS Data Access | Self-hosted or SaaS (Qodo) |
| **Language/Stack** | Pure Go (1 binary) | Proprietary | Proprietary | Python |
| **Integration** | CI/CD Agnostic | Deep GitHub & Jira Integrations | Deep GitHub/GitLab Integrations | Deep Git Provider Integrations |

## Competitor Deep Dives

- [Gitar.ai](gitar.md) — The "Fix-First" automated remediation bot.
- [CodeRabbit](coderabbit.md) — The popular incumbent PR reviewer.
- [CodiumAI PR-Agent](codiumai-pr-agent.md) — The open-source, Python-based orchestrator.
