# Defending Against Inter-Agent Attacks in AI Code Review

**Status:** Draft
**Target Audience:** Security Engineers, CISOs, AI Researchers
**Objective:** Explain how ATCR prevents multi-agent cascades (Inter-agent attacks) using our Skeptic persona and context sanitization.

---

## 1. The Rise of the Multi-Agent System
- We previously argued that Single-Model Code Review isn't enough. You need a panel of agents (Reviewers, Fixers, Skeptics) to handle complex workflows.
- But introducing multiple agents introduces a terrifying new attack vector: **The Inter-Agent Attack**.

## 2. What is an Inter-Agent Attack?
- Definition: When a compromised agent feeds malicious instructions to a downstream agent in the same pipeline.
- Example: A developer opens a PR with a hidden Prompt Injection in the comments. The "Reviewer" agent reads it, gets compromised, and generates a malicious "Fix" patch. The "Skeptic" agent then reads that patch, gets compromised, and approves the malicious payload to production.
- AI Supply Chain Risks: The cascading failure of trust across your LLM orchestration layer.

## 3. How ATCR Solves This: Automated Red Teaming
- Most tools pass raw context blindly between agents. 
- ATCR treats the output of *every* agent as hostile input to the next.
- **Context Sanitization:** When the Fixer sends a patch to the Skeptic, the ATCR orchestrator aggressively sanitizes the payload, stripping executable markdown commands that could hijack the Skeptic's context window.
- **The OWASP LLM Top 10:** Our Skeptic persona acts as an Automated Red Team. It doesn't just check if the code compiles; it specifically cross-examines the Fixer's output against the OWASP Top 10, looking for prompt injections and data leakage.

## 4. Conclusion
- If you're using AI agents in your CI/CD pipeline, you cannot trust them implicitly. 
- You need a hardened orchestration layer that assumes agents *will* hallucinate or be hijacked. 
- ATCR provides that hardened, multi-agent sandbox out of the box.
