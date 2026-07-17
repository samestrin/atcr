# Competitor Deep Dive: Greptile

## Overview

[Greptile](https://www.greptile.com) is a fast-growing AI code review product that emphasizes a "global" understanding of the codebase. Instead of just looking at the diff, Greptile ingests and builds a semantic graph of the entire repository. This allows it to evaluate how changes in one part of the codebase might impact other areas, helping to catch complex logic errors that simple diff-based analysis might miss.

## Core Value Proposition

- **Full-Codebase Context:** Builds a graph index of the repo (functions, classes, dependencies) so the AI understands cross-file dependencies when reviewing a PR.
- **TREX (Runtime Validation):** Capable of running and validating code as part of its review pipeline.
- **Conversational Feedback:** Developers can @-mention the AI within the PR for follow-up questions and clarifications.
- **Local Learning Loops:** Uses reinforcement learning from team feedback (thumbs up/down, merges) to suppress nitpicks over time.
- **"Fix with your Agent":** Sends identified issues directly to AI coding agents like Cursor or Claude Code for immediate implementation.

## Architecture

Greptile operates as a **SaaS Platform** with deep integrations into GitHub/GitLab. It offers cloud deployment as well as self-hosting options (via Docker/Kubernetes) for organizations with strict security requirements.

## ATCR Counter-Positioning

While Greptile's approach to deep context is impressive, it represents a fundamentally different philosophy from ATCR. Here is how we counter them:

### 1. The "Omniscient Oracle" vs. The Heterogeneous Panel
Greptile tries to make a single review pipeline extremely smart by feeding it a massive context graph. However, relying on one perspective (even a highly contextualized one) still risks single-model bias and hallucination. 
**ATCR** does not try to be the single smartest reviewer. Instead, we fan the code out to a heterogeneous panel of models and mathematically reconcile their findings. We believe that 2 models agreeing on an issue (even with less global context) is a higher-confidence signal than 1 model guessing based on a massive context window.

### 2. Managed SaaS Index vs. Local CLI Engine
Greptile (even in its self-hosted Docker form) is a heavy, stateful service that requires indexing the entire repository and hosting webhooks. 
**ATCR** is a lightweight, stateless Go binary. It follows the UNIX philosophy: pipe a diff in, get a machine-readable `findings.json` out. It doesn't require a background indexing service or seat licenses, offering absolute data privacy by keeping the codebase on the local machine or CI runner and bringing your own API keys (BYO-Keys).

### 3. Feature Inspiration
While we counter-position against their SaaS architecture, Greptile's feature set directly validates the need for ATCR's roadmap:
- **Context:** Greptile's context graph validates ATCR Epic 45.0 (Context-Aware Pre-fetching) which will use lightweight local semantic search to achieve similar context without the heavy SaaS footprint.
- **Feedback Loops:** Greptile's nitpick suppression validates ATCR Epic 46.0 (Local Feedback Loops) using deterministic `.atcr/ignore.yaml` rules.
- **Auto-Fix:** Greptile's "Fix with your Agent" validates ATCR's existing Sprint 17.0 `--auto-fix` flow and the `fixer` persona.
