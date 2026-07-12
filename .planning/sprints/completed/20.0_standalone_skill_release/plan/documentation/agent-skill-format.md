# Agent Skill Format & Progressive Disclosure

`[CRITICAL]`

## Overview

Agent Skills are structured around a three-level progressive-disclosure model: metadata, instructions, and resources. Level 1 (metadata) is the YAML frontmatter — `name` and `description` — which Claude loads at startup and keeps in the system prompt for every installed Skill. Level 2 (instructions) is the body of SKILL.md itself, which only enters the context window when the Skill is triggered by a matching request. Level 3 (resources) covers bundled files — additional markdown guides, executable scripts, and reference material — that Claude accesses from the filesystem only when referenced, without ever loading their full contents into context up front.

> Source: [Agent Skills overview: How Skills work]

This staged loading is why the model matters for restructuring `skill/SKILL.md` into a dispatcher: Claude's VM environment is filesystem-based, so a Skill can defer the bulk of its instructions to secondary files and only pull them in via bash when a specific code path is actually exercised. A Skill that inlines everything in Level 2 pays the "Under 5k tokens" budget on every trigger, whether or not the triggered command needs that content.

> Source: [Agent Skills overview: How Skills work]

For this plan, the ~500-line budget on `skill/SKILL.md` maps directly onto keeping Level 2 lean: the dispatcher's `/atcr <command>` entry point and command-routing logic belong in SKILL.md's body, while detailed host-review, ambiguity-adjudication, and findings-format instructions move to secondary markdown files that are loaded on demand, consistent with the Level 3 "load additional files only when referenced" mechanism.

> Source: [codebase-discovery.json:architecture_notes]

## Key Concepts

- **Three loading levels and token cost.** Level 1 (metadata) is always loaded at startup at roughly 100 tokens per Skill. Level 2 (instructions) loads only when the Skill is triggered, capped at under 5k tokens. Level 3+ (resources and code) loads as needed and is described as "effectively unlimited" since bundled files are executed via bash rather than loaded into context.

  > Source: [Agent Skills overview: How Skills work]

- **SKILL.md frontmatter requirements.** Every Skill requires a SKILL.md file with YAML frontmatter containing two required fields: `name` and `description`. `name` has a maximum of 64 characters, must contain only lowercase letters, numbers, and hyphens, cannot contain XML tags, and cannot contain the reserved words "anthropic" or "claude". `description` must be non-empty, has a maximum of 1024 characters, cannot contain XML tags, and should state both what the Skill does and when Claude should use it.

  > Source: [Agent Skills overview: Skill structure]

- **Load additional files only when referenced.** Skills can bundle instructions (additional markdown files such as FORMS.md or REFERENCE.md with specialized guidance), code (executable scripts run via bash for deterministic operations without consuming context), and resources (reference materials like schemas, API docs, templates, or examples). Claude accesses these bundled files only when referenced from SKILL.md. This is the mechanism that justifies splitting the current Host Review Instructions, ambiguity-adjudication logic, and findings-format spec out of the primary dispatcher into secondary files (e.g., `skill/host-review.md`, `skill/ambiguity-adjudication.md`, `skill/findings-format.md`) loaded on demand rather than inlined in SKILL.md.

  > Source: [Agent Skills overview: How Skills work]
  > Source: [codebase-discovery.json:architecture_notes]

- **Claude Code's filesystem-based skill discovery.** In Claude Code, custom Skills are filesystem-based and don't require API uploads. They can be scoped as personal (`~/.claude/skills/`) or project-based (`.claude/skills/`), and can also be shared via Claude Code Plugins.

  > Source: [Agent Skills overview: Claude Code sharing scope]

## Code Examples

Frontmatter example from the source docs:

```yaml
---
name: pdf-processing
description: Extract text and tables from PDF files, fill forms, merge documents. Use when working with PDF files or when the user mentions PDFs, forms, or document extraction.
---
```

> Source: [Agent Skills overview: Level 1: Metadata (always loaded)]

Actual frontmatter of the atcr skill being restructured:

```yaml
---
name: atcr
description: Run a multi-reviewer code review with atcr — fan a git range out to a panel of LLM reviewer personas, add a host (+1) review, and reconcile everything into a single deduplicated, confidence-scored report. Use when asked to review a branch, a PR, or a git range.
---
```

> Source: [`skill/SKILL.md`](../../../../../skill/SKILL.md)

Directory layout example:

```text
pdf-skill/
├── SKILL.md (main instructions)
├── FORMS.md (form-filling guide)
├── REFERENCE.md (detailed API reference)
└── scripts/
    └── fill_form.py (utility script)
```

> Source: [Agent Skills overview: Level 3: Resources and code (loaded as needed)]

## Quick Reference

| Level | When Loaded | Token Cost | Content |
|---|---|---|---|
| Level 1: Metadata | Always (at startup) | ~100 tokens per Skill | `name` and `description` from YAML frontmatter |
| Level 2: Instructions | When Skill is triggered | Under 5k tokens | SKILL.md body with instructions and guidance |
| Level 3+: Resources | As needed | Effectively unlimited | Bundled files executed via bash without loading contents into context |

> Source: [Agent Skills overview: How Skills work]

## Related Documentation

- Source: [Agent Skills overview](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview)
- File this plan restructures: `skill/SKILL.md` (must be rewritten as a dispatcher within the ~500-line budget, per this plan's objective)
