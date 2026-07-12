// Package skill embeds the atcr Agent Skill definition so its structure is
// verified at build time and the file can be installed programmatically. The
// skill itself is Markdown instructions for a host AI agent; there is no runtime
// Go logic here — orchestration is a sequence of atcr CLI invocations.
//
// SKILL.md is a /atcr <command> dispatcher: a routing table over the atcr CLI
// plus on-demand secondary files (host-review.md, ambiguity-adjudication.md,
// findings-format.md, CONVENTIONS.md, debt-resolve/SKILL.md) that carry the
// detailed host-review, adjudication, findings-format, shared-conventions, and
// debt-resolution instructions. Embedding the secondary files here lets tests
// verify their relocated content at build time.
package skill

import _ "embed"

// SkillMD is the embedded SKILL.md content (the /atcr dispatcher definition).
//
//go:embed SKILL.md
var SkillMD string

// HostReviewMD is the embedded host-review instructions (the +1 host review's
// adversarial personality, payload-grounding rules, and findings-file format).
//
//go:embed host-review.md
var HostReviewMD string

// AmbiguityAdjudicationMD is the embedded optional gray-zone cluster
// adjudication instructions (gatekeeper rules and the adjudication contract).
//
//go:embed ambiguity-adjudication.md
var AmbiguityAdjudicationMD string

// FindingsFormatMD is the embedded findings-format reference; it points to the
// canonical docs/findings-format.md rather than redefining the column contract.
//
//go:embed findings-format.md
var FindingsFormatMD string

// ConventionsMD is the embedded shared-conventions reference: the Prerequisites
// (atcr binary on PATH, git work tree, gh CLI) and the .atcr/ path-safety rules
// that every public skill's SKILL.md points at instead of inlining.
//
//go:embed CONVENTIONS.md
var ConventionsMD string

// DebtResolveMD is the embedded /atcr debt resolve route: the on-demand skill file
// documenting the RED→GREEN→ADVERSARIAL→REFACTOR resolution cycle over the public,
// .atcr/-scoped local TD store. It points at CONVENTIONS.md for the shared
// Prerequisites/path-safety rules rather than duplicating them.
//
//go:embed debt-resolve/SKILL.md
var DebtResolveMD string
