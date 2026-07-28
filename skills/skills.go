// Package skills embeds the atcr Agent Skill definition so its structure is
// verified at build time and the tree can be installed programmatically by
// `atcr skill export`. The skill itself is Markdown instructions for a host AI
// agent; there is no runtime Go logic here — orchestration is a sequence of atcr
// CLI invocations.
//
// The install root is plural (`skills/`) with one directory per skill, so
// `cp -r skills/* <dest>` is correct in one line and a second skill can land
// beside the first without another rename. Each skill directory's name matches
// its SKILL.md `name:` frontmatter — `skills/atcr/` for `name: atcr`.
//
// atcr/SKILL.md is a /atcr <command> dispatcher: a routing table over the atcr
// CLI plus on-demand secondary files (host-review.md, ambiguity-adjudication.md,
// findings-format.md, CONVENTIONS.md, debt-resolve.md) that carry the detailed
// host-review, adjudication, findings-format, shared-conventions, and
// debt-resolution instructions. Every secondary file is a true sibling of
// SKILL.md, so the relative references inside it resolve in an installed tree.
// Embedding them here lets tests verify their content at build time and lets
// the export command write the tree out without shipping it separately.
//
// go:embed cannot traverse `../`, so this file sits at or above every file it
// embeds — hence skills/skills.go rather than skills/atcr/skills.go.
package skills

import (
	"embed"
)

// SkillMD is the embedded SKILL.md content (the /atcr dispatcher definition).
//
//go:embed atcr/SKILL.md
var SkillMD string

// HostReviewMD is the embedded host-review instructions (the +1 host review's
// adversarial personality, payload-grounding rules, and findings-file format).
//
//go:embed atcr/host-review.md
var HostReviewMD string

// AmbiguityAdjudicationMD is the embedded optional gray-zone cluster
// adjudication instructions (gatekeeper rules and the adjudication contract).
//
//go:embed atcr/ambiguity-adjudication.md
var AmbiguityAdjudicationMD string

// FindingsFormatMD is the embedded findings-format reference; it points to the
// canonical docs/findings-format.md rather than redefining the column contract.
//
//go:embed atcr/findings-format.md
var FindingsFormatMD string

// ConventionsMD is the embedded shared-conventions reference: the Prerequisites
// (atcr binary on PATH, git work tree, gh CLI) and the .atcr/ path-safety rules
// that every public skill's SKILL.md points at instead of inlining.
//
//go:embed atcr/CONVENTIONS.md
var ConventionsMD string

// DebtResolveMD is the embedded /atcr debt resolve route: the on-demand reference
// file documenting the RED→GREEN→ADVERSARIAL→REFACTOR resolution cycle over the
// public, .atcr/-scoped local TD store. It is reached through SKILL.md's
// `atcr debt` routing row rather than invoked on its own, and points at
// CONVENTIONS.md for the shared Prerequisites/path-safety rules rather than
// duplicating them.
//
//go:embed atcr/debt-resolve.md
var DebtResolveMD string

// Tree is the whole installable skill tree, rooted one level above the per-skill
// directories so embedded paths read `atcr/SKILL.md`, `atcr/CONVENTIONS.md`, and
// so on. `atcr skill export` walks this FS to write a byte-identical copy to
// disk; the individual string vars above remain for the build-time content tests.
//
//go:embed atcr
var Tree embed.FS

// SkillDir is the directory name of the single shipped skill, matching its
// SKILL.md `name:` frontmatter. Export writes the CONTENTS of this directory to
// the destination, because a harness skill directory is itself named for the
// skill (e.g. .claude/skills/atcr/SKILL.md).
const SkillDir = "atcr"
