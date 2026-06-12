// Package skill embeds the atcr Agent Skill definition so its structure is
// verified at build time and the file can be installed programmatically. The
// skill itself is Markdown instructions for a host AI agent; there is no runtime
// Go logic here — orchestration is a sequence of atcr CLI invocations.
package skill

import _ "embed"

// SkillMD is the embedded SKILL.md content (the Agent Skill definition).
//
//go:embed SKILL.md
var SkillMD string
