# ATCR Content Marketing Pipeline

This directory contains the drafts and outlines for ATCR's content marketing strategy. 
To maximize impact, these posts should be published in a strategic sequence that moves the audience from **Awareness** (agitating current pain points), to **Education** (explaining our unique architecture), to **Enterprise Trust** (proving our security and scalability to buyers).

## Recommended Publication Schedule

### Phase 1: The Problem (Awareness & Agitation)
*Goal: Highlight the pain developers currently feel with manual code reviews and the fundamental flaws in V1 "single-model" AI assistants.*

1. **[The Code Review Bottleneck](the-code-review-bottleneck.md)**
   - **Target:** Individual Contributors & Engineering Managers
   - **Pitch:** Why PRs sit for days and how synchronous reviews kill momentum. Sets the stage for AI intervention.
2. **[Slopfix & AI Code Bloat](slopfix-ai-code-bloat.md)**
   - **Target:** Senior Engineers
   - **Pitch:** Agitating the problem with current AI coding tools (Copilot, Cursor) that generate massive amounts of unchecked code, exacerbating the review bottleneck.
3. **[Why Single-Model Code Review Isn't Enough](why-single-model-code-review-isnt-enough.md)**
   - **Target:** Tech Leads & Architects
   - **Pitch:** The flagship post introducing the "3-Layer AI Review Architecture" (Reviewers, Workflow, Plumbing). Explains why asking a single LLM to review a diff is dangerous, setting up ATCR's multi-agent approach.

### Phase 2: The ATCR Solution (Education & Architecture)
*Goal: Explain how ATCR's unique multi-agent architecture and local tooling solve the problems highlighted in Phase 1.*

4. **[Persona Mapping: The Pillars of Code Review](persona-mapping-code-review-pillars.md)**
   - **Target:** Engineering Teams
   - **Pitch:** Deep dive into how ATCR uses specialized agents (Reviewer, Fixer, Skeptic) to mimic human team dynamics.
5. **[Stop Blocking Your Terminal: AI Review with Git Worktrees](stop-blocking-your-terminal-with-git-worktrees.md)**
   - **Target:** Developer Relations / Open Source Community
   - **Pitch:** A highly practical, tutorial-style post showing developers how to run ATCR's multi-agent consensus locally without locking up their primary Git branch.
6. **[Build Without the Build Cost](build-without-the-build-cost.md)**
   - **Target:** FinOps & DevOps
   - **Pitch:** Explaining ATCR's sandbox validation overlay and how we run deterministic checks without massive cloud compute costs.
7. **[You're Paying an LLM to Review Your Lockfiles](stop-paying-to-review-vendored-code.md)**
   - **Target:** FinOps & DevOps
   - **Pitch:** `.atcrignore` + `.gitignore` payload exclusion (Epic 26.0): drop generated code, vendored deps, and lockfiles before the byte-budget pass — cheaper reviews and sharper findings, with `--no-ignore` as the escape hatch.
8. **[Stop Picking Your AI Reviewer by Vibes: Benchmark It](benchmark-leaderboard-pick-your-reviewer.md)**
   - **Target:** Tech Leads & Platform Engineers
   - **Pitch:** Grounded in the `atcr benchmark run` leaderboard (Epic 10.x). Argues you should pick the model behind your review on measured cost-per-corroborated-finding, not vibes — with reproducible, checkpoint-resumable scoring.
9. **[How ATCR Merges Five Models' Findings Without a Vector Database](deterministic-dedup-without-embeddings.md)**
   - **Target:** Architects & Backend Engineers
   - **Pitch:** The deeper reconciler-internals piece (Epics 13.1/13.2). Deterministic finding dedup via AST-isomorphism grouping + Kuhn-Munkres matching + DBSCAN noise isolation — no embeddings, no vector store, no per-run drift.
10. **[A Marketplace for Code-Review Expertise, Without the Marketplace](persona-marketplace-community-registry.md)**
    - **Target:** Developer Relations / Open Source Community
    - **Pitch:** The community persona registry (Epics 19.6/19.9): discover-by-model search, `atcr personas submit` via a `gh` fork/PR, and two-tier curation that keeps openness and trust from defeating each other.
11. **[From Bug Found to PR Opened, With No Hands on the Keyboard — Safely](auto-fix-detection-to-pr.md)**
    - **Target:** Engineering Managers & Staff Engineers
    - **Pitch:** `atcr review --auto-fix` (Epic 17.0): apply → validate → revert-on-failure → branch/commit/PR. The safety story is ordering — no GitHub-mutating call is reachable before local validation passes.
12. **[The Fastest Way to Lose Trust in an AI Reviewer: Repeat Yourself](teach-your-reviewer-what-you-rejected.md)**
    - **Target:** Engineering Teams & Tech Leads
    - **Pitch:** `atcr debt resolve --status wontfix --reason` (Epic 24.0): a terminal dismissal that the reconcile dedup-by-id path actually enforces, so a rejected finding stops re-surfacing loop after loop.
13. **[Reproducible AI Reviews in a World Where Models Change Under You](reproducible-reviews-live-model-resolution.md)**
    - **Target:** Platform Engineers & DevOps
    - **Pitch:** Live model resolution (Epic 19.7): personas bind to a family/channel and lock to a concrete slug — reproducible by default, upgraded on purpose via `atcr personas upgrade`, and loud on drift via `atcr models check`.

### Phase 3: Enterprise Trust & Security (Selling to Buyers)
*Goal: Win over the decision-makers (CISOs, VPs of QA, Directors of Engineering) who hold the budget and are terrified of AI security risks.*

14. **[The Zero Vendor Lock-in Advantage in AI Testing](zero-vendor-lockin-testing.md)**
    - **Target:** QA Leaders & Engineering VPs
    - **Pitch:** Explains why ATCR's dynamic UI testing outputs 100% standard open-source Playwright scripts, ensuring enterprises own their IP.
15. **[Defending Against Inter-Agent Attacks in AI Code Review](inter-agent-attacks-defense.md)**
    - **Target:** CISOs & Security Engineers
    - **Pitch:** A deep dive into the terrifying new risk of multi-agent cascades, and how ATCR's "Automated Red Teaming" Skeptic and context sanitization act as an AI firewall.
16. **[Zero Data Egress: AI Code Review That Never Leaves Your Hardware](local-ollama-zero-egress-review.md)**
    - **Target:** CISOs & Regulated Industries (finance, health, defense)
    - **Pitch:** The local Ollama/llama.cpp/vLLM persona pack (Epic 27.0): `gerald`/`orson`/`liam` tuned per hardware tier, `local` provider routing, and an architectural — not contractual — zero-egress guarantee once the registry points at `localhost`.
17. **[When the Auditor Asks "Who Reviewed This PR?", Can You Answer for the AI?](audit-trail-every-ai-review-logged.md)**
    - **Target:** Compliance Leads & Engineering Directors
    - **Pitch:** The append-only audit ledger + `atcr audit-report --pr` (Epic 19.1) and version-controllable, month-sharded findings history (Epic 19.4). AI-review evidence becomes a command, not a shrug.
18. **[Your AI Findings Belong in the Security Tab, Not a Text File](sarif-native-security-dashboard.md)**
    - **Target:** Security Engineers & DevSecOps
    - **Pitch:** `atcr report --format=sarif` (Epic 25.0): schema-conformant SARIF 2.1.0 that lands reconciled findings in GitHub Code Scanning and GitLab's SAST widget, next to CodeQL, with severity mapping and no silently-dropped file-level findings.
