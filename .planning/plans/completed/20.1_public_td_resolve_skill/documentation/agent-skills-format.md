# Agent Skills Format & Progressive Disclosure

**Priority: Critical**

## Overview

Agent Skills are modular capabilities that extend Claude's functionality: each Skill packages instructions, metadata, and optional resources (scripts, templates) that Claude uses automatically when relevant, organized like an onboarding guide for a new team member. The defining architectural property is progressive disclosure — Claude loads Skill content in stages as needed rather than consuming it all upfront, which is what lets a project accumulate many Skills without paying a context cost for the ones not currently in use.

Progressive disclosure resolves into three concrete loading levels. Level 1 (metadata) is the YAML frontmatter's `name` and `description`, always loaded at startup at roughly 100 tokens per Skill so Claude knows a Skill exists and when to reach for it. Level 2 (instructions) is the body of SKILL.md itself — procedural knowledge, workflows, and guidance — read from the filesystem via bash only once a request matches the Skill's description, and expected to stay under 5k tokens. Level 3 (resources and code) covers bundled files referenced from SKILL.md — additional markdown files, executable scripts, reference materials — loaded or executed only as needed, with an effectively unlimited token budget because their contents never enter context except on demand.

For this epic, both new files (`skill/CONVENTIONS.md` and `skill/debt-resolve/SKILL.md`) sit at different levels of this model. `skill/debt-resolve/SKILL.md` is itself a full Skill with its own Level 1/2 structure (frontmatter + body) matching the shape already established by `skill/SKILL.md` in this repo. `skill/CONVENTIONS.md` is a Level 3 resource — a shared reference file that both `skill/SKILL.md` and `skill/debt-resolve/SKILL.md` point to for the Prerequisites boilerplate (binary-on-PATH check, git-worktree check, `.atcr/` path-safety rules) rather than each restating it inline.

## Key Concepts

**Three Skill content types / three loading levels** — metadata (always loaded, ~100 tokens), instructions (loaded when triggered, under 5k tokens), and resources/code (loaded as needed, effectively unlimited).
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

**Filesystem-based architecture** — "Skills exist as directories containing instructions, executable code, and reference materials, organized like an onboarding guide you'd create for a new team member. This filesystem-based architecture enables progressive disclosure: Claude loads information in stages as needed, rather than consuming context upfront."
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

**Required SKILL.md frontmatter fields** — every Skill requires a SKILL.md file with YAML frontmatter containing `name` and `description` as the only required fields.
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

**`name` field constraints** — maximum 64 characters; only lowercase letters, numbers, and hyphens; no XML tags; cannot contain the reserved words "anthropic" or "claude".
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

**`description` field constraints** — must be non-empty, maximum 1024 characters, cannot contain XML tags, and "should include both what the Skill does and when Claude should use it."
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

**Claude Code supports only Custom Skills** — "Create Skills as directories with SKILL.md files. Claude discovers and uses them automatically. Custom Skills in Claude Code are filesystem-based and don't require API uploads." Sharing scope is "Personal (~/.claude/skills/) or project-based (.claude/skills/); can also be shared via Claude Code Plugins."
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

**Runtime environment** — "Skills have the same network access as any other program on the user's computer," and global package installation is discouraged: "Skills should only install packages locally in order to avoid interfering with the user's computer."
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

**Skills as folders of instructions/scripts/resources** — "Skills are folders of instructions, scripts, and resources that Claude loads dynamically to improve performance on specialized tasks," enabling Claude to "complete specific tasks in a repeatable way."
> Source: https://support.claude.com/en/articles/12512176-what-are-skills

**Progressive disclosure prevents context overload** — "Claude determines which skills are relevant and loads the information it needs to complete that task, helping to prevent context window overload." "Claude reviews available skills, loads relevant ones, and applies their instructions."
> Source: https://support.claude.com/en/articles/12512176-what-are-skills

**Custom Skills need no coding** — "Anyone can create skills by writing instructions in Markdown—no coding required for simple skills, though you can attach executable scripts to custom skills for more advanced functionality."
> Source: https://support.claude.com/en/articles/12512176-what-are-skills

**Open standard portability** — the Agent Skills specification is published as an open standard at agentskills.io, enabling skill portability across AI platforms adopting the standard.
> Source: https://support.claude.com/en/articles/12512176-what-are-skills

**Existing dispatcher shape in this repo** — `skill/SKILL.md` is the existing `/atcr <command>` dispatcher: frontmatter with `name: atcr` and a `description` naming every routed subcommand (including `atcr debt`), followed by Overview, Prerequisites, Input Format, Orchestration Steps, and a Commands routing table. Its Prerequisites section currently states the binary-on-PATH check and the git-worktree check inline — this is the content this epic extracts into `skill/CONVENTIONS.md`.
> Source: skill/SKILL.md (Prerequisites section)

**Embed harness registers every skill file as a build-time constant** — `skill/skill.go` embeds `SKILL.md`, `host-review.md`, `ambiguity-adjudication.md`, and `findings-format.md` via `//go:embed`, each as an exported `string` constant (`SkillMD`, `HostReviewMD`, `AmbiguityAdjudicationMD`, `FindingsFormatMD`), so that "Embedding the secondary files here lets tests verify their relocated content at build time." Any new file this epic adds (`CONVENTIONS.md`, `debt-resolve/SKILL.md`) must be added to this same embed set to be discoverable and testable the same way.
> Source: skill/skill.go

**Test suite validates SKILL.md structure and a dispatcher command list** — `skill/skill_test.go` defines `dispatcherCommands`, a list mirroring the live Cobra command surface (including `"debt"`), and asserts every command is routed as `` `atcr <name>` `` inside `SkillMD`; it also asserts `/atcr <command>` is documented and the frontmatter `description` reflects a general dispatcher. A separate test (`TestSkill_NoAbsoluteOrClaudePaths`) asserts none of the embedded skill bodies (`SkillMD`, `HostReviewMD`, `AmbiguityAdjudicationMD`, `FindingsFormatMD`) contain `.claude`-specific paths or absolute filesystem paths (`/Users/`, `/home/`, `/opt/`, `C:\`). New tests for this epic must add the new subcommand to an equivalent assertion and confirm `CONVENTIONS.md` is referenced, following this same pattern.
> Source: skill/skill_test.go

**On-demand secondary skill file precedent** — `skill/host-review.md` is loaded on demand by `skill/SKILL.md` step 4 ("load `host-review.md` on demand for the full instructions") rather than inlined into SKILL.md's main body; it opens with a `# Host Review Instructions` H1, states the reviewer's role and adversarial personality, then documents the exact findings-file format with a fenced example. This is the structural precedent (Level 3 resource, loaded as needed, own H1 heading, self-contained instructions) for `skill/debt-resolve/SKILL.md`, though the new file is itself a top-level Skill rather than a sub-file of the `atcr` dispatcher Skill.
> Source: skill/host-review.md

## Code Examples

Level 1 metadata example (YAML frontmatter only):

```yaml
---
name: pdf-processing
description: Extract text and tables from PDF files, fill forms, merge documents. Use when working with PDF files or when the user mentions PDFs, forms, or document extraction.
---
```
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

Level 2 instructions example (SKILL.md body, referencing a Level 3 file):

````markdown
# PDF Processing

## Quick start

Use pdfplumber to extract text from PDFs:

```python
import pdfplumber

with pdfplumber.open("document.pdf") as pdf:
    text = pdf.pages[0].extract_text()
```

For advanced form filling, see [FORMS.md](FORMS.md).
````
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

Level 3 directory layout example:

```text
pdf-skill/
├── SKILL.md (main instructions)
├── FORMS.md (form-filling guide)
├── REFERENCE.md (detailed API reference)
└── scripts/
    └── fill_form.py (utility script)
```
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

Minimal required Skill structure:

```yaml
---
name: your-skill-name
description: Brief description of what this Skill does and when to use it
---

# Your Skill Name

## Instructions
[Clear, step-by-step guidance for Claude to follow]

## Examples
[Concrete examples of using this Skill]
```
> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

## Quick Reference

| Level | When Loaded | Token Cost | Content |
|---|---|---|---|
| Level 1: Metadata | Always (at startup) | ~100 tokens per Skill | `name` and `description` from YAML frontmatter |
| Level 2: Instructions | When Skill is triggered | Under 5k tokens | SKILL.md body with instructions and guidance |
| Level 3+: Resources | As needed | Effectively unlimited | Bundled files executed via bash without loading contents into context |

> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

| Field | Constraint |
|---|---|
| `name` | Required; max 64 characters; only lowercase letters, numbers, hyphens; no XML tags; cannot contain "anthropic" or "claude" |
| `description` | Required; non-empty; max 1024 characters; no XML tags; should state both what the Skill does and when to use it |

> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

| Claude Code specifics | Detail |
|---|---|
| Supported Skill kind | Custom Skills only (directories with SKILL.md, discovered automatically, no API upload) |
| Sharing scope | Personal (`~/.claude/skills/`) or project-based (`.claude/skills/`); also shareable via Claude Code Plugins |
| Network access | Same as any other program on the user's computer |
| Package installation | Local only — global installs discouraged to avoid interfering with the user's system |

> Source: https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview

## Related Documentation

- https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview
- https://support.claude.com/en/articles/12512176-what-are-skills
- `skill/SKILL.md` — existing `/atcr <command>` dispatcher Skill; Prerequisites section to be extracted into `skill/CONVENTIONS.md`
- `skill/skill.go` — Go embed harness; new files must be added to its embed set
- `skill/skill_test.go` — structural tests including the `dispatcherCommands` list (near line 133) and the no-`.claude`/no-absolute-path assertions
