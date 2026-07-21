# ATCR Product Vocabulary

This document standardizes the terminology used across ATCR's codebase, marketing materials, and epic planning to ensure consistency when discussing the product's architecture and value proposition.

## The 3-Layer AI Code Review Architecture
*Adapted from industry standards (e.g., Tessl) to explain ATCR's platform depth.*

### 1. The Reviewers (Content Generation)
- **Definition:** The agents, LLMs, and static analysis tools that actually look at diffs and generate findings, comments, or fixes.
- **ATCR Implementation:** Our multi-agent personas (`Skeptic`, `Fixer`, `Frontend QA`).
- **Context:** Single-model wrappers only operate at this layer.

### 2. The Workflow (Coordination)
- **Definition:** The intelligent engine that coordinates the Reviewers. It decides *when* to run which reviewer, how to merge their conflicting results, and whether to escalate or block a PR.
- **ATCR Implementation:** The `Reconciler v2` (Epic 13.0) and the `Multi-Tier Execution Engine` (Epic 32.1).
- **Context:** This is ATCR's primary moat. We don't just generate findings; we deterministically cluster, deduplicate, and score them using consensus mechanisms.

### 3. The Plumbing (Integrations)
- **Definition:** The scripts and API clients that move data between systems. They don't generate content or make decisions; they just push results to where developers live.
- **ATCR Implementation:** Enterprise Integrations (Epic 37.0) hooking into Jira, Slack, GitHub Actions, and GitLab CI.
- **Context:** To allow safe third-party plumbing, ATCR requires a **Skill Evaluation Framework** to score third-party integrations for validity and safety.

## Agent Personas
- **Reviewer:** Primary agent that reads a diff and generates initial findings.
- **Skeptic:** Adversarial agent that attempts to prove the Reviewer's findings are false positives.
- **Fixer:** Agent responsible for generating patches (`--auto-fix`).

## Assistant vs Agent (Industry Positioning)
- **Assistant:** A tool (like GitHub Copilot) that is advisory. It suggests code, but humans must trigger it, read the suggestion, apply it, and verify it.
- **Agent:** A tool (like ATCR) that acts autonomously. It runs in the background (or in CI), understands deep project context, catches race conditions, and auto-fixes them with explanations without human hand-holding. ATCR is firmly an **Agent**.

## Key Concepts
- **Automated Red Teaming:** The process of adversarially testing code (and AI outputs) continuously on every PR. In ATCR, the Skeptic persona performs this role, acting as an automated red team against the Auto-Fixer to ensure it doesn't bypass security filters or write vulnerable code.
- **Inter-agent Attacks:** A critical security risk in multi-agent systems where a compromised agent feeds malicious instructions or context to a downstream agent. ATCR's orchestrator must strictly sanitize context passed between the Fixer and the Skeptic.
- **Adversarial Verification:** The specific ATCR mechanism of using a Skeptic to refute a finding.
- **Zero Vendor Lock-in:** The principle that generated outputs (e.g., Playwright UI tests in Epic 44.0) must be standard open-source formats, not proprietary vendor formats.
- **Managerial Velocity Metrics:** Enterprise analytics (Epic 42.0) focusing on "Time-to-Merge" and "Pushed vs Landed Coding Time".
