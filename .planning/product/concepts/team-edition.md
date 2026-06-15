# Concept: Self-Hosted Team Edition + Compliance Wrapper

**Status:** Conceptual
**Created:** 2026-06-11
**Priority:** Medium (high ceiling, slow)

## Problem

ATCR is a powerful single-user tool, but teams need:
- Shared configuration (provider keys, persona registry)
- Finding history across runs ("what has this package been flagged for before?")
- Audit trails for compliance (SOC 2, ISO 27001: "show me all reviews for PR #1234")
- Trend visibility ("which modules have the highest finding density?")

Without these, teams either:
- Share a single `registry.yaml` via config repo (manual, no audit trail)
- Skip compliance requirements (risk)
- Use a SaaS tool that hosts their code (privacy concern)

## Solution

A **team edition** of ATCR that adds:

### Shared Registry
- One `registry.yaml` for the team, managed via:
  - Config repo (git-based, version-controlled)
  - API endpoint (for dynamic updates)
- Provider keys stored securely (env vars, secrets manager integration)
- Persona library shared across the team

### Finding History
- Every `atcr review` run appends to a `findings-history.jsonl` in a configurable location (repo-local or team-shared)
- Query interface: "show me all HIGH findings for `internal/auth/` in the last 90 days"
- Aggregation: finding counts by package, severity, reviewer

### Trend Dashboard
- Web UI (lightweight, self-hosted) showing:
  - Finding counts over time (by severity, by package)
  - Top offender modules (highest finding density)
  - Reviewer panel performance (which personas find the most issues?)
- Exportable reports (PDF, CSV) for management review

### Audit Trail
- Immutable log of every review run:
  - Who triggered it (CI job, user)
  - What was reviewed (base/head SHAs, PR number)
  - What was found (findings summary)
  - What was fixed (follow-up PRs linked to findings)
- Compliance-ready: "prove that PR #1234 was reviewed and all CRITICAL findings were resolved"

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Shared registry | Team-wide `registry.yaml` with secure key storage | Medium |
| Finding history | JSONL append-only log, queryable by package/severity/date | Low |
| Trend dashboard | Web UI for finding trends, top offenders, panel performance | Medium |
| Audit trail | Immutable review log, compliance-ready reports | Medium |
| API layer | REST/GraphQL API for programmatic access to history, trends | Medium |

## Revenue Model

**Enterprise license** for the team features:
- OSS core: free, self-hosted, full-featured for single users
- Team edition: paid license for shared registry, history, dashboard, audit
- Pricing tiers:
  - Starter (5 users): $X/month
  - Team (20 users): $Y/month
  - Enterprise (unlimited): custom pricing, support contract

**Compliance wrapper** (add-on):
- Compliance report generator (SOC 2, HIPAA, ISO 27001)
- "Survived-a-Skeptic" certification integration (see survived-a-skeptic-cert.md)
- Audit trail hardening for regulated industries
- $5k-20k/year per org (on top of Team Edition license)

**Support contracts** for enterprise:
- Priority bug fixes
- Custom persona development (see enterprise-persona-dev.md)
- Onboarding assistance

**Why this works:**
- Regulated industries need to prove code quality.
- The compliance wrapper is a thin layer on top of the audit trail.
- Enterprises will pay for compliance; it's a legal requirement, not a nice-to-have.
- Recurring revenue (annual licenses).

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Shared registry | 1-2 weeks | Config repo integration, API for dynamic updates |
| Finding history | 1 week | JSONL writer + query CLI, index for fast lookups |
| Trend dashboard | 2-3 weeks | Web UI (React/Vue), backend API, data aggregation |
| Audit trail | 1-2 weeks | Immutable log, compliance report generator |
| API layer | 1-2 weeks | REST endpoints for history, trends, audit |
| **Total** | **6-10 weeks** | Can be phased: history first, dashboard later |

## Moat / Differentiation

- **Audit trail is nearly free** — ATCR already produces deterministic artifacts. The audit log is just persisting and querying the review directories.
- **Dashboard is a thin UI** — the data (`summary.json`, `findings.txt`) already exists. The dashboard is a view layer, not a data layer.
- **Self-hosted = privacy** — teams that can't use SaaS (regulated industries) will prefer this.
- **OSS core = trust** — the core tool is auditable, which builds trust in the team edition.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Teams don't want to pay for team features | Medium | High | Start with a generous free tier (3 users, basic history); prove value before charging |
| Dashboard becomes a maintenance burden | Medium | Medium | Keep it lightweight — no real-time updates, no complex visualization. Export > interactive |
| API surface explodes | Low | Medium | Start with read-only API (history, trends); write API later if needed |
| Enterprise sales cycle is long | High | Medium | Focus on self-serve first (Stripe billing, instant signup); enterprise sales as a secondary channel |

## Open Questions

- **Config repo vs. API?** Should the shared registry be git-based (like `.github/workflows`) or API-driven (like a secrets manager)?
- **Where does history live?** Repo-local (each repo has its own `findings-history.jsonl`) or team-shared (one history DB for all repos)?
- **Dashboard tech stack?** React + Express (full-featured) or static site generator (lightweight, export-focused)?
- **Compliance frameworks?** Which frameworks to target first (SOC 2, ISO 27001, FedRAMP)?

## Relationship to Other Concepts

- **"Survived-a-Skeptic" Certification** — the compliance wrapper integrates with the certification; enterprises that buy Team Edition will want the certification.
- **Enterprise Persona Development** — enterprises that buy Team Edition will want custom personas; the two are often sold together.
- **Review-as-a-Service API** — the SaaS alternative; offer both, let teams choose.
- **Review Intelligence Analytics** — the Team Edition finding history is the data source; Team Edition users opt-in to aggregation.
- **CI Integration** — the finding history is built on top of CI integration; build CI first, then Team Edition.

## References

- Grafana: OSS core, paid enterprise features (dashboard, alerting, auth)
- GitLab: OSS core, paid CI/CD features
- SonarQube: OSS code quality, paid enterprise features (security, compliance)
