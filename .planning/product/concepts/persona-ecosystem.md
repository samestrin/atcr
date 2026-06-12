# Concept: Persona Ecosystem (Community-Driven)

**Status:** Conceptual  
**Created:** 2026-06-11  
**Priority:** Low  

## Problem

ATCR ships with 6 personas (bruce, greta, kai, mira, dax, otto) covering general review dimensions:
- Correctness, algorithmic correctness, design fit, production feasibility, test coverage, style

But teams have domain-specific review needs:
- **Security** — OWASP Top 10, injection, auth bypass, secrets leakage
- **Performance** — N+1 queries, memory leaks, algorithmic complexity
- **Accessibility** — WCAG compliance, ARIA attributes, keyboard navigation
- **Compliance** — regulatory requirements (HIPAA, GDPR, SOX)
- **Language-specific** — Go idioms, Rust ownership, Python type hints
- **Framework-specific** — React hooks, Django ORM, Rails conventions

Without domain-specific personas, teams either:
- Write their own persona prompts (inconsistent, not shared)
- Skip domain-specific review (risk)
- Use a SaaS tool that has built-in domain reviewers (vendor lock-in)

## Solution

A **curated collection of domain-specific personas** in a community repo:

```
github.com/atcr/personas
```

### Structure

```
personas/
  security/
    owasp.md          # OWASP Top 10 reviewer
    secrets.md        # Secrets leakage detector
  performance/
    sql.md            # SQL query performance
    memory.md         # Memory leak detector
  accessibility/
    wcag.md           # WCAG 2.1 compliance
  compliance/
    hipaa.md          # HIPAA compliance
    gdpr.md           # GDPR compliance
  language/
    go-idioms.md      # Go idioms and best practices
    rust-ownership.md # Rust borrow checker patterns
  framework/
    react-hooks.md    # React hooks best practices
    django-orm.md     # Django ORM anti-patterns
```

### Quality Bar

Each persona must have:
- **Prompt template** — the persona's instructions (markdown)
- **Test fixture** — a sample code snippet the persona should flag
- **Expected findings** — the findings the persona should produce for the fixture
- **Documentation** — what the persona reviews, when to use it, example output

### Contribution Model

- **Community submissions** — anyone can submit a persona via PR
- **Review process** — ATCR maintainers review for quality (prompt clarity, fixture coverage)
- **Testing** — CI runs the persona against its fixture; must produce expected findings
- **Versioning** — personas are versioned (v1, v2); breaking changes require a major version bump

### Installation

```bash
# Install all personas
atcr personas install --all

# Install specific persona
atcr personas install security/owasp

# List installed personas
atcr personas list
```

Installed personas land in `~/.config/atcr/personas/` and are available for use in the registry.

## Key Features

| Feature | Description | Effort |
|---------|-------------|--------|
| Persona repo | `github.com/atcr/personas` with curated collection | 1 week (initial 10-15 personas) |
| Quality bar | Test fixture + expected findings for each persona | Ongoing (per persona) |
| CLI integration | `atcr personas install/list/remove` | 1 week |
| Contribution guide | Documentation for submitting personas | 3 days |
| CI testing | Automated testing of personas against fixtures | 1 week |

## Revenue Model

**No direct revenue** — this is ecosystem building, not productization.

**Indirect benefits:**
- **Adoption** — domain-specific personas make ATCR useful for more teams
- **Community** — contributors become advocates; word-of-mouth growth
- **Enterprise licensing** — teams that love the OSS personas want the team edition (shared registry, audit trail)
- **Consulting** — custom persona development for enterprise clients

## Engineering Effort

| Component | Effort | Notes |
|-----------|--------|-------|
| Initial personas | 1 week | 10-15 high-quality personas (security, performance, accessibility, compliance, language) |
| CLI integration | 1 week | `atcr personas install/list/remove` |
| Contribution guide | 3 days | Documentation, template, examples |
| CI testing | 1 week | Automated fixture testing, expected findings validation |
| Ongoing curation | Ongoing | Review community submissions, maintain quality |
| **Total** | **3-4 weeks** (initial) | Ongoing maintenance is community-driven |

## Moat / Differentiation

- **Personas are the personality** — a rich ecosystem makes ATCR the go-to for "serious" code review
- **Community lock-in** — contributors invest time writing personas; they're unlikely to switch to a competing tool
- **Quality bar** — curated collection (not a free-for-all) builds trust
- **Domain coverage** — teams with specific needs (security, compliance) find off-the-shelf personas

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Low community participation | High | Medium | Seed the repo with 10-15 high-quality personas; document the contribution process clearly |
| Persona quality is inconsistent | Medium | High | Strict review process; automated fixture testing; reject low-quality submissions |
| Personas become stale | Medium | Medium | Version personas; deprecate old versions; automated testing catches regressions |
| Overlap with built-in personas | Low | Low | Built-in personas are generalist; community personas are domain-specific; document the difference |

## Open Questions

- **License?** CC-BY-SA (share-alike) or Apache 2.0 (permissive)? CC-BY-SA ensures contributions stay open; Apache 2.0 is more friendly to commercial use.
- **Hosting?** GitHub repo (simple, universal) or a dedicated registry (like npm, PyPI)?
- **Versioning?** Semver per persona (v1.0, v1.1) or global version (all personas v1, v2)?
- **Discovery?** How do users find personas? Searchable website? CLI search (`atcr personas search security`)?

## References

- Homebrew taps: community-contributed packages, curated by maintainers
- VS Code extensions: marketplace with quality bar (verified publishers, ratings)
- Grafana dashboards: community-contributed dashboards, shared via grafana.com
- ESLint plugins: community-contributed rules, installed via npm
