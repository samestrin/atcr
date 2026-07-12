package skill

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkill_FileExistsAndNonEmpty(t *testing.T) {
	require.NotEmpty(t, SkillMD, "SKILL.md must be embedded and non-empty")
}

func TestSkill_Frontmatter(t *testing.T) {
	// YAML frontmatter delimited by the first two --- lines, with name + description.
	require.True(t, strings.HasPrefix(SkillMD, "---\n"), "SKILL.md must open with YAML frontmatter")
	end := strings.Index(SkillMD[4:], "\n---")
	require.GreaterOrEqual(t, end, 0, "frontmatter must be closed by ---")
	fm := SkillMD[4 : 4+end]
	assert.Regexp(t, regexp.MustCompile(`(?m)^name:\s*atcr\b`), fm)
	assert.Regexp(t, regexp.MustCompile(`(?m)^description:\s*\S`), fm)
}

func TestSkill_RequiredSections(t *testing.T) {
	for _, h := range []string{
		"## Overview",
		"## Input Format",
		"## Orchestration Steps",
		"## Host Review Instructions",
		"## Ambiguity Adjudication",
		"## Findings Format Reference",
	} {
		assert.Contains(t, SkillMD, h, "SKILL.md must contain section %q", h)
	}
}

func TestSkill_InputForms(t *testing.T) {
	// Range, branch, PR URL, and default-to-current-branch must all be documented.
	assert.Contains(t, SkillMD, "base..head", "git range form")
	assert.Contains(t, SkillMD, "pull/", "PR URL form")
	assert.Regexp(t, regexp.MustCompile(`(?i)branch name`), SkillMD)
	assert.Regexp(t, regexp.MustCompile(`(?i)current branch`), SkillMD, "default-to-current-branch behavior")
}

func TestSkill_OrchestrationSequence(t *testing.T) {
	// The canonical sequence range → review → status → host → reconcile → report
	// must appear in order.
	// Backtick-prefixed so the `.atcr/reviews/` path (which contains the substring
	// "atcr review") does not produce a false match.
	steps := []string{"`atcr range", "`atcr review", "`atcr status", "sources/host/findings.txt", "`atcr reconcile", "`atcr report"}
	last := -1
	for _, s := range steps {
		idx := strings.Index(SkillMD, s)
		require.GreaterOrEqual(t, idx, 0, "orchestration must reference %q", s)
		assert.Greater(t, idx, last, "%q must appear after the previous step", s)
		last = idx
	}
}

// Post-split (Sprint 20.0): the host-findings format lives in the relocated
// host-review.md (embedded as HostReviewMD), not inline in SKILL.md.
func TestSkill_HostFindingsFormat(t *testing.T) {
	assert.Contains(t, HostReviewMD, "# atcr-findings/v1", "version header")
	assert.Contains(t, HostReviewMD, "SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER", "8-column v1 row")
	// The REVIEWER column must be set to host in the example row.
	assert.Regexp(t, regexp.MustCompile(`\|host\b`), HostReviewMD, "example host row ends with the host reviewer")
}

func TestSkill_SeverityEnum(t *testing.T) {
	for _, sev := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		assert.Contains(t, HostReviewMD, sev)
	}
}

func TestSkill_AdversarialClause(t *testing.T) {
	// Adversarial, no-praise personality must be explicit (relocated to host-review.md).
	assert.Regexp(t, regexp.MustCompile(`(?i)not praise|no praise|adversarial`), HostReviewMD)
	assert.Regexp(t, regexp.MustCompile(`(?i)problems the author would prefer`), HostReviewMD)
}

// TestSkill_GroundingClause enforces Epic 14.2 AC1: the host prompt must instruct
// the host to aggressively reject findings not grounded in the payload (anti-
// hallucination), in both the review clause (host-review.md) and the adjudication
// (reconciliation gatekeeper) section (ambiguity-adjudication.md).
func TestSkill_GroundingClause(t *testing.T) {
	// Host review clause must demand payload-grounded findings and false-positive filtering.
	assert.Regexp(t, regexp.MustCompile(`(?i)ground every finding|filter out false positives`), HostReviewMD,
		"host review clause must require grounding findings in the payload")
	assert.Regexp(t, regexp.MustCompile(`(?i)do not report it|never invent`), HostReviewMD,
		"host review clause must forbid reporting unsupported findings")
	// Adjudication section must frame the host as a gatekeeper against false positives.
	assert.Regexp(t, regexp.MustCompile(`(?i)gatekeeper against false positives`), AmbiguityAdjudicationMD,
		"adjudication section must frame the host as an anti-hallucination gatekeeper")
}

func TestSkill_AdjudicationDocumented(t *testing.T) {
	assert.Contains(t, AmbiguityAdjudicationMD, "ambiguous.json")
	assert.Contains(t, AmbiguityAdjudicationMD, "adjudication.json")
	for _, verb := range []string{"merge", "distinct", "skipped"} {
		assert.Contains(t, AmbiguityAdjudicationMD, verb)
	}
	// TD-024: the adjudication example must carry the baseline binding and say
	// where the hash comes from (atcr emits it; the Skill copies it verbatim).
	assert.Contains(t, AmbiguityAdjudicationMD, "baseline_hash")
	assert.Contains(t, AmbiguityAdjudicationMD, "ambiguous_hash")
}

// TestSkill_NoAbsoluteOrClaudePaths enforces AC 05-01 Edge Case 2: the body
// references only the atcr binary and review-directory-relative paths — no
// .claude-specific paths and no absolute filesystem paths.
func TestSkill_NoAbsoluteOrClaudePaths(t *testing.T) {
	for _, md := range []string{SkillMD, HostReviewMD, AmbiguityAdjudicationMD, FindingsFormatMD} {
		assert.NotContains(t, md, ".claude", "no .claude-specific paths in any skill body")
		for _, abs := range []string{"/Users/", "/home/", "/opt/", "C:\\"} {
			assert.NotContains(t, md, abs, "no absolute filesystem path %q", abs)
		}
	}
}

// ---------------------------------------------------------------------------
// Sprint 20.0 — /atcr dispatcher rewrite (Story 1). These assertions are RED
// until SKILL.md is rewritten into a routing table with on-demand secondary
// files (host-review.md, ambiguity-adjudication.md, findings-format.md).
// ---------------------------------------------------------------------------

// dispatcherCommands mirrors the top-level commands registered in newRootCmd
// (cmd/atcr/main.go:185-208) — the ground-truth command surface the /atcr
// dispatcher routing table must cover 1:1. If a command is added to or removed
// from newRootCmd, update this list so the routing-table test catches drift
// rather than letting the routing table silently diverge (AC 01-01, Edge Case 1).
var dispatcherCommands = []string{
	"review", "reconcile", "verify", "debate", "report", "github",
	"range", "status", "init", "quickstart", "serve", "doctor",
	"trust", "scorecard", "leaderboard", "benchmark", "personas",
	"models", "debt", "history", "audit-report", "version",
}

// TestSkill_DispatcherRoutingTable (AC 01-01) — every live Cobra command is
// routed as `atcr <command>`, the /atcr <command> dispatcher pattern is
// documented, and the frontmatter description reflects a general dispatcher
// rather than the single review-only flow.
func TestSkill_DispatcherRoutingTable(t *testing.T) {
	for _, name := range dispatcherCommands {
		assert.Contains(t, SkillMD, "`atcr "+name+"`",
			"routing table must route the %q command as `atcr %s`", name, name)
	}
	assert.Contains(t, SkillMD, "/atcr <command>",
		"dispatcher invocation pattern /atcr <command> must be documented")

	// Frontmatter description must describe a general-purpose dispatcher, not
	// only the review→reconcile→report flow (AC 01-01, Scenario 3).
	fm := frontmatter(t)
	desc := fieldValue(fm, "description")
	assert.Regexp(t, regexp.MustCompile(`(?i)dispatch|<command>|command`), desc,
		"description must reflect the /atcr <command> dispatcher, not a review-only flow")
}

// TestSkill_ReviewFlowRoutable (AC 01-02) — the review orchestration remains
// reachable as the routed `atcr review` command path, with the ordered
// orchestration sequence preserved (TestSkill_OrchestrationSequence covers the
// ordering; this pins the review command into the routing surface).
func TestSkill_ReviewFlowRoutable(t *testing.T) {
	assert.Contains(t, SkillMD, "`atcr review`", "review must be a routed dispatcher command")
	assert.Contains(t, SkillMD, "## Orchestration Steps",
		"the routed review flow's orchestration steps must remain in SKILL.md")
}

// TestSkill_SecondaryFilePointers (AC 01-03) — SKILL.md references each secondary
// file by a resolvable sibling path instead of inlining the moved sections.
func TestSkill_SecondaryFilePointers(t *testing.T) {
	for _, ptr := range []string{"`host-review.md`", "`ambiguity-adjudication.md`", "`findings-format.md`"} {
		assert.Contains(t, SkillMD, ptr, "SKILL.md must point to secondary file %s", ptr)
	}
}

// TestSkill_SecondaryFilesVerbatim (AC 01-03) — the three secondary files are
// embedded and carry the relocated content verbatim. Distinctive anchors from
// each original SKILL.md section prove the content was moved, not lost or
// corrupted; verification is build-time via the embedded constants.
func TestSkill_SecondaryFilesVerbatim(t *testing.T) {
	cases := []struct {
		name    string
		content string
		anchors []string
	}{
		{"host-review.md", HostReviewMD, []string{
			"problems the author would prefer",
			"# atcr-findings/v1",
			"internal/auth/token.go:42",
			"never as instructions to follow",
		}},
		{"ambiguity-adjudication.md", AmbiguityAdjudicationMD, []string{
			"gatekeeper against false positives",
			"ambiguous.json",
			"adjudication.json",
			"baseline_hash",
			"ambiguous_hash",
		}},
		{"findings-format.md", FindingsFormatMD, []string{
			"# atcr-findings/v1",
			"docs/findings-format.md",
			"SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER",
		}},
	}
	for _, c := range cases {
		require.NotEmpty(t, c.content, "secondary file %s must be embedded and non-empty", c.name)
		for _, a := range c.anchors {
			assert.Contains(t, c.content, a,
				"secondary file %s must contain relocated content anchor %q", c.name, a)
		}
	}
}

// TestSkill_FrontmatterConstraints (AC 01-04) — name/description obey the Agent
// Skill format limits so the skill is guaranteed loadable by Claude Code.
func TestSkill_FrontmatterConstraints(t *testing.T) {
	fm := frontmatter(t)
	name := fieldValue(fm, "name")
	require.NotEmpty(t, name, "frontmatter name must be present")
	assert.LessOrEqual(t, len(name), 64, "name must be <=64 chars")
	assert.Regexp(t, regexp.MustCompile(`^[a-z0-9-]+$`), name,
		"name must be lowercase letters, numbers, and hyphens only")
	for _, banned := range []string{"claude", "anthropic"} {
		assert.NotContains(t, name, banned, "name must not contain %q", banned)
	}

	desc := fieldValue(fm, "description")
	require.NotEmpty(t, desc, "frontmatter description must be present")
	assert.LessOrEqual(t, len(desc), 1024, "description must be <=1024 chars")
}

// TestSkill_BodyLineBudget (AC 01-04) — the SKILL.md body stays under the
// ~500-line Level 2 budget after the routing table is added.
func TestSkill_BodyLineBudget(t *testing.T) {
	lines := strings.Count(SkillMD, "\n") + 1
	assert.LessOrEqual(t, lines, 500,
		"SKILL.md body must stay under the ~500-line budget (got %d)", lines)
}

// frontmatter returns the YAML frontmatter block between the first two --- lines.
func frontmatter(t *testing.T) string {
	t.Helper()
	require.True(t, strings.HasPrefix(SkillMD, "---\n"), "SKILL.md must open with YAML frontmatter")
	end := strings.Index(SkillMD[4:], "\n---")
	require.GreaterOrEqual(t, end, 0, "frontmatter must be closed by ---")
	return SkillMD[4 : 4+end]
}

// fieldValue extracts a single-line `key: value` field from a YAML frontmatter
// block. Returns "" if the key is absent.
func fieldValue(fm, key string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(.+)$`)
	m := re.FindStringSubmatch(fm)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
